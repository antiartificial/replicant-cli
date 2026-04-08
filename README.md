# replicant

A multi-provider AI agent harness in Go with an interactive terminal UI. Define agent personas as markdown files, point them at a codebase, and let them work.

Inspired by the agentic coding tool pattern — where an LLM loops over tool calls until an objective is complete — but built as a native CLI with full control over the provider, the prompt, the tools, and the permission model.

## What it does

```
you type a message
    → replicant sends it to the LLM with your tools
    → LLM responds (streamed to your terminal in real time)
    → if the LLM calls a tool: execute it, feed the result back, loop
    → when the LLM is done: you see the result, type the next message
```

That's the whole loop. The rest is configuration.

## Quick start

```bash
# Build
make build

# Run with default replicant (deckard)
ANTHROPIC_API_KEY=sk-... ./replicant

# Pick a different persona
./replicant -r roy        # architect/planner (Opus)
./replicant -r rachael    # autonomous coder (Sonnet, 100 turns)
./replicant -r zhora      # debugger (Sonnet)
./replicant -r pris       # fast validator (Haiku)

# Use a different provider
./replicant -m openai/gpt-4o
./replicant -m xai/grok-3
```

## Replicants

Agent personas are markdown files with YAML frontmatter. Drop them in `./replicants/` or `~/.replicant/replicants/`.

```markdown
---
name: deckard
description: General-purpose coding replicant
model: anthropic/claude-sonnet-4-20250514
tools: [read_file, edit_file, execute, glob, grep]
max_turns: 50
---

You are Deckard, a coding replicant. You read, search, edit,
and execute with precision.

When given a task:
1. Understand the objective
2. Plan your approach
3. Execute incrementally
4. Validate
```

The frontmatter configures the model, tools, and loop limits. The markdown body is the system prompt. That's it — no DSL, no config files, no YAML hell.

### Built-in roster

| Name | Model | Role |
|------|-------|------|
| **deckard** | Sonnet | General-purpose coding agent |
| **roy** | Opus | Architecture, planning, trade-off analysis |
| **rachael** | Sonnet | Autonomous objective completion (100 turns) |
| **zhora** | Sonnet | Debugging and root cause investigation |
| **pris** | Haiku | Fast validation, code review, yes/no checks |

## Providers

Replicant talks to LLMs through a `Provider` interface. Three implementations ship:

- **Anthropic** — streaming via SSE, tool use, prompt caching aware
- **OpenAI** — standard chat completions with function calling
- **xAI** — OpenAI-compatible endpoint for Grok models

The model string determines the provider: `anthropic/claude-sonnet-4-20250514`, `openai/gpt-4o`, `xai/grok-3`. Bare model names (no prefix) default to Anthropic.

## Tools

| Tool | Risk | What it does |
|------|------|-------------|
| `read_file` | none | Read file contents with line numbers |
| `edit_file` | low | Search-and-replace in files |
| `execute` | high | Run shell commands with timeout |
| `glob` | none | Find files by pattern |
| `grep` | none | Search file contents (uses ripgrep) |

Tools declare their own risk level. The permission system gates execution based on your autonomy setting:

```bash
REPLICANT_AUTONOMY=normal  # auto-approve reads, confirm edits + shell (default)
REPLICANT_AUTONOMY=high    # auto-approve most, confirm destructive ops
REPLICANT_AUTONOMY=full    # never ask
REPLICANT_AUTONOMY=off     # confirm everything
```

When a tool needs confirmation, the TUI prompts inline — press `y` to allow, `n` to deny.

## Sessions

Every conversation is logged as append-only JSONL in `~/.replicant/sessions/`. Each line is a typed entry: `session_start`, `message`, `tool_call`, or `tool_result`. Sessions are never modified after writing — useful for debugging, replay, and auditing what the agent did.

## Project layout

```
cmd/replicant/main.go           Entry point, wires config → provider → agent → TUI
internal/
  agent/
    provider.go                 Provider interface, message types, factory
    anthropic.go                Anthropic SDK with SSE streaming
    openai.go                   OpenAI/xAI SDK with function calling
    agent.go                    ReAct loop (tool_use → execute → tool_result → repeat)
    session.go                  JSONL session persistence
  config/config.go              Environment-based configuration
  permission/permission.go      Risk levels and autonomy gating
  replicant/
    definition.go               Replicant definition struct
    loader.go                   Markdown + YAML frontmatter parser
    registry.go                 Discovers .md files from standard dirs
  tools/
    tool.go                     Tool interface
    registry.go                 Tool name → implementation lookup
    readfile.go                 read_file implementation
    editfile.go                 edit_file (search-and-replace)
    execute.go                  Shell execution with timeout
    glob.go                     Recursive file pattern matching
    grep.go                     Content search (ripgrep or grep fallback)
  tui/
    app.go                      Root bubbletea model, agent bridge
    conversation.go             Scrollable message viewport
    input.go                    Multi-line text input
    statusbar.go                Model, tokens, session timer
    spinner.go                  Thinking indicator
    theme.go                    Blade Runner color palette
    messages.go                 Bubbletea message types
replicants/                     Agent persona definitions (.md)
Makefile                        Build, run, test, docker
Dockerfile                      Multi-stage build, ships with ripgrep
```

## What this brings to the table

**Native binary, no runtime.** Single `go build` produces a 17MB binary. No Node, no Python, no Docker required to run.

**Markdown-defined agents.** The system prompt, model selection, tool access, and loop limits are all in one readable file. Fork a replicant by copying a markdown file and editing it.

**Provider-agnostic loop.** The ReAct loop doesn't know or care which LLM is behind the `Provider` interface. Swap Anthropic for OpenAI by changing a prefix. Add a new provider by implementing one method.

**Streaming-first.** Anthropic responses stream token-by-token to the TUI via SSE. You see the agent think in real time, not after a long wait.

**Permission model with teeth.** Tools declare risk. Autonomy levels gate execution. The TUI blocks on confirmation for dangerous operations. No silent `rm -rf`.

**Auditable sessions.** Every tool call, every result, every message — logged to JSONL. If the agent broke something, you can trace exactly what it did and why.

**Composable by design.** The agent loop, tools, providers, and TUI are separate packages with clean interfaces. Use the agent loop without the TUI. Add tools without touching the loop. Swap the TUI for a web interface.

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ANTHROPIC_API_KEY` | — | Anthropic API key |
| `OPENAI_API_KEY` | — | OpenAI / xAI API key |
| `REPLICANT_MODEL` | `claude-sonnet-4-20250514` | Default model |
| `REPLICANT_AUTONOMY` | `normal` | Permission level |

## Docker

```bash
make docker
docker run --rm -it -e ANTHROPIC_API_KEY -v $(pwd):/work -w /work replicant:latest
```

## License

MIT
