package agent

import (
	"context"
	"encoding/json"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicProvider implements Provider using the official Anthropic Go SDK.
type AnthropicProvider struct {
	client anthropic.Client
}

// NewAnthropicProvider returns a Provider backed by the Anthropic Messages API.
// apiKey is passed directly; if empty the SDK falls back to ANTHROPIC_API_KEY.
func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	opts := []option.RequestOption{}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	return &AnthropicProvider{
		client: anthropic.NewClient(opts...),
	}
}

// Complete sends a non-streaming request to the Anthropic Messages API and
// returns the response mapped to our internal types.
func (p *AnthropicProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	params := anthropic.MessageNewParams{
		Model:     req.Model,
		MaxTokens: int64(req.MaxTokens),
		Messages:  toSDKMessages(req.Messages),
	}

	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}

	if req.Temperature != 0 {
		params.Temperature = anthropic.Float(req.Temperature)
	}

	if len(req.Tools) > 0 {
		params.Tools = toSDKTools(req.Tools)
	}

	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic: messages.new: %w", err)
	}

	resp := &CompletionResponse{
		StopReason: string(msg.StopReason),
		Usage: Usage{
			InputTokens:  int(msg.Usage.InputTokens),
			OutputTokens: int(msg.Usage.OutputTokens),
		},
	}

	for _, block := range msg.Content {
		cb, err := fromSDKBlock(block)
		if err != nil {
			return nil, fmt.Errorf("anthropic: decode content block: %w", err)
		}
		if cb != nil {
			resp.Content = append(resp.Content, *cb)
		}
	}

	return resp, nil
}

// Stream implements StreamProvider. It opens a streaming request to the
// Anthropic Messages API and maps SDK SSE events to our StreamEvent types.
//
// The caller must drain or stop reading the events channel — Stream sends
// on it synchronously and will block if the channel is full.
func (p *AnthropicProvider) Stream(ctx context.Context, req *CompletionRequest, events chan<- StreamEvent) error {
	params := anthropic.MessageNewParams{
		Model:     req.Model,
		MaxTokens: int64(req.MaxTokens),
		Messages:  toSDKMessages(req.Messages),
	}

	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}

	if req.Temperature != 0 {
		params.Temperature = anthropic.Float(req.Temperature)
	}

	if len(req.Tools) > 0 {
		params.Tools = toSDKTools(req.Tools)
	}

	stream := p.client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	// blockType tracks the content block type at each index so we know
	// whether input_json_delta chunks belong to a tool_use block.
	type blockMeta struct {
		blockType string
		toolID    string
		toolName  string
		inputBuf  []byte
	}
	blocks := make(map[int64]*blockMeta)

	var stopReason string
	var usage Usage

	for stream.Next() {
		evt := stream.Current()

		switch evt.Type {
		case "content_block_start":
			cb := evt.ContentBlock
			meta := &blockMeta{blockType: cb.Type}
			if cb.Type == "tool_use" {
				meta.toolID = cb.ID
				meta.toolName = cb.Name
				events <- StreamEvent{
					Type:     "tool_use_start",
					ToolID:   cb.ID,
					ToolName: cb.Name,
				}
			}
			blocks[evt.Index] = meta

		case "content_block_delta":
			delta := evt.Delta
			meta := blocks[evt.Index]
			switch delta.Type {
			case "text_delta":
				events <- StreamEvent{
					Type: "text_delta",
					Text: delta.Text,
				}
			case "input_json_delta":
				if meta != nil && meta.blockType == "tool_use" {
					meta.inputBuf = append(meta.inputBuf, delta.PartialJSON...)
					events <- StreamEvent{
						Type:      "tool_use_delta",
						ToolID:    meta.toolID,
						ToolName:  meta.toolName,
						ToolInput: delta.PartialJSON,
					}
				}
			}

		case "content_block_stop":
			meta := blocks[evt.Index]
			if meta != nil && meta.blockType == "tool_use" {
				events <- StreamEvent{
					Type:      "tool_use_end",
					ToolID:    meta.toolID,
					ToolName:  meta.toolName,
					ToolInput: string(meta.inputBuf),
				}
			}
			delete(blocks, evt.Index)

		case "message_delta":
			if sr := string(evt.Delta.StopReason); sr != "" {
				stopReason = sr
			}
			usage.InputTokens = int(evt.Usage.InputTokens)
			usage.OutputTokens = int(evt.Usage.OutputTokens)

		case "message_stop":
			events <- StreamEvent{
				Type:       "message_done",
				StopReason: stopReason,
				Usage:      usage,
			}
		}
	}

	if err := stream.Err(); err != nil {
		return fmt.Errorf("anthropic: stream: %w", err)
	}
	return nil
}

// toSDKMessages converts our Message slice to the SDK's MessageParam slice.
func toSDKMessages(msgs []Message) []anthropic.MessageParam {
	out := make([]anthropic.MessageParam, 0, len(msgs))
	for _, m := range msgs {
		blocks := toSDKContentBlocks(m.Content)
		switch m.Role {
		case "assistant":
			out = append(out, anthropic.NewAssistantMessage(blocks...))
		default: // "user"
			out = append(out, anthropic.NewUserMessage(blocks...))
		}
	}
	return out
}

// toSDKContentBlocks converts our ContentBlock slice to the SDK union type.
func toSDKContentBlocks(blocks []ContentBlock) []anthropic.ContentBlockParamUnion {
	out := make([]anthropic.ContentBlockParamUnion, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "text":
			out = append(out, anthropic.NewTextBlock(b.Text))

		case "tool_use":
			// Input is stored as raw JSON; decode to any for the SDK.
			var input any
			if len(b.Input) > 0 {
				if err := json.Unmarshal(b.Input, &input); err != nil {
					input = map[string]any{}
				}
			}
			out = append(out, anthropic.NewToolUseBlock(b.ID, input, b.Name))

		case "tool_result":
			isErr := b.IsError
			out = append(out, anthropic.NewToolResultBlock(b.ToolUseID, b.Content, isErr))
		}
	}
	return out
}

// toSDKTools converts our ToolDef slice to the SDK's ToolUnionParam slice.
func toSDKTools(tools []ToolDef) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		schema := anthropic.ToolInputSchemaParam{}
		if props, ok := t.InputSchema["properties"]; ok {
			schema.Properties = props
		}
		if req, ok := t.InputSchema["required"]; ok {
			if reqSlice, ok := req.([]string); ok {
				schema.Required = reqSlice
			} else if reqAny, ok := req.([]any); ok {
				// JSON unmarshalled required arrays come as []any.
				strs := make([]string, 0, len(reqAny))
				for _, v := range reqAny {
					if s, ok := v.(string); ok {
						strs = append(strs, s)
					}
				}
				schema.Required = strs
			}
		}

		tp := anthropic.ToolParam{
			Name:        t.Name,
			InputSchema: schema,
		}
		if t.Description != "" {
			tp.Description = anthropic.String(t.Description)
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &tp})
	}
	return out
}

// fromSDKBlock converts a single SDK ContentBlockUnion to our ContentBlock.
// Returns nil for unrecognised block types (thinking, etc.) which we silently skip.
func fromSDKBlock(b anthropic.ContentBlockUnion) (*ContentBlock, error) {
	switch b.Type {
	case "text":
		return &ContentBlock{
			Type: "text",
			Text: b.Text,
		}, nil

	case "tool_use":
		// b.Input is already json.RawMessage in the SDK struct.
		return &ContentBlock{
			Type:  "tool_use",
			ID:    b.ID,
			Name:  b.Name,
			Input: json.RawMessage(b.Input),
		}, nil

	default:
		// Skip thinking, redacted_thinking, server_tool_use, etc.
		return nil, nil
	}
}
