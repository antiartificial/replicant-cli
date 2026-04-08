package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/antiartificial/replicant/internal/mission"
	"github.com/antiartificial/replicant/internal/permission"
	"github.com/google/uuid"
)

// RegistryAdapter wraps a *Registry so it satisfies mission.ToolRegistry.
// The mission package defines its own ToolEntry / ToolRegistry interfaces to
// avoid an import cycle with tools; this adapter bridges them.
type RegistryAdapter struct {
	r *Registry
}

// NewRegistryAdapter wraps reg so it can be passed to mission.NewEngine.
func NewRegistryAdapter(reg *Registry) *RegistryAdapter {
	return &RegistryAdapter{r: reg}
}

func (a *RegistryAdapter) Resolve(names []string) []mission.ToolEntry {
	raw := a.r.Resolve(names)
	out := make([]mission.ToolEntry, len(raw))
	for i, t := range raw {
		out[i] = t
	}
	return out
}

func (a *RegistryAdapter) Get(name string) (mission.ToolEntry, bool) {
	t, ok := a.r.Get(name)
	return t, ok
}

const missionToolTimeout = 60 * time.Minute

// MissionTool exposes mission planning and execution to the orchestrator agent.
// It implements Tool, ContextTool, StreamingTool, and TimeoutTool.
type MissionTool struct {
	engine *mission.Engine
	store  *mission.Store
}

// NewMissionTool constructs a MissionTool backed by the given engine and store.
func NewMissionTool(eng *mission.Engine, store *mission.Store) *MissionTool {
	return &MissionTool{engine: eng, store: store}
}

func (t *MissionTool) Name() string { return "mission" }

func (t *MissionTool) Description() string {
	return "Plan, run, and inspect long-running missions made up of milestones and features. " +
		"Use action=plan to create a mission from a structured definition, action=run to execute it " +
		"(streams progress until completion), action=status to inspect a mission's current state, " +
		"and action=list to see all missions."
}

func (t *MissionTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"plan", "run", "status", "list"},
				"description": "The operation to perform: plan | run | status | list",
			},
			"mission": map[string]any{
				"type":        "object",
				"description": "Full mission structure (required for action=plan).",
				"properties": map[string]any{
					"objective": map[string]any{"type": "string"},
					"dir":       map[string]any{"type": "string", "description": "Working directory for validation commands."},
					"milestones": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name":        map[string]any{"type": "string"},
								"description": map[string]any{"type": "string"},
								"features": map[string]any{
									"type": "array",
									"items": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"name":                map[string]any{"type": "string"},
											"description":         map[string]any{"type": "string"},
											"instruction":         map[string]any{"type": "string"},
											"acceptance_criteria": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
											"worker_model":        map[string]any{"type": "string"},
											"tools":               map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
											"max_attempts":        map[string]any{"type": "integer"},
										},
									},
								},
								"validation": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"checks": map[string]any{
											"type": "array",
											"items": map[string]any{
												"type": "object",
												"properties": map[string]any{
													"name":      map[string]any{"type": "string"},
													"command":   map[string]any{"type": "string"},
													"expected":  map[string]any{"type": "string"},
													"must_pass": map[string]any{"type": "boolean"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			"id": map[string]any{
				"type":        "string",
				"description": "Mission ID (required for action=run and action=status).",
			},
		},
		"required": []string{"action"},
	}
}

func (t *MissionTool) Risk() permission.RiskLevel { return permission.RiskLow }

func (t *MissionTool) Timeout() time.Duration { return missionToolTimeout }

// Run executes without a parent context (satisfies Tool interface).
func (t *MissionTool) Run(args string) (string, error) {
	return t.RunWithContext(context.Background(), args)
}

// RunWithContext dispatches non-streaming actions (plan, status, list).
// For run, it falls through to RunStreaming with a nil channel.
func (t *MissionTool) RunWithContext(ctx context.Context, args string) (string, error) {
	var input missionInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return "", fmt.Errorf("mission: invalid args: %w", err)
	}

	switch input.Action {
	case "plan":
		return t.doPlan(input)
	case "status":
		return t.doStatus(input)
	case "list":
		return t.doList()
	case "run":
		// RunStreaming with nil output is valid — progress is discarded.
		return t.RunStreaming(ctx, args, nil)
	default:
		return "", fmt.Errorf("mission: unknown action %q (valid: plan, run, status, list)", input.Action)
	}
}

// RunStreaming streams mission progress while running. For other actions it
// delegates to RunWithContext.
func (t *MissionTool) RunStreaming(ctx context.Context, args string, output chan<- string) (string, error) {
	var input missionInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return "", fmt.Errorf("mission: invalid args: %w", err)
	}

	if input.Action != "run" {
		return t.RunWithContext(ctx, args)
	}

	return t.doRun(ctx, input, output)
}

// missionInput is the decoded JSON input for the mission tool.
type missionInput struct {
	Action  string           `json:"action"`
	Mission *missionInputDef `json:"mission,omitempty"`
	ID      string           `json:"id,omitempty"`
}

// missionInputDef mirrors the Mission/Milestone/Feature structure for JSON
// ingestion. IDs are assigned by the tool so callers don't need to supply them.
type missionInputDef struct {
	Objective  string                `json:"objective"`
	Dir        string                `json:"dir"`
	Milestones []milestoneInputDef   `json:"milestones"`
}

type milestoneInputDef struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Features    []featureInputDef   `json:"features"`
	Validation  *validationInputDef `json:"validation,omitempty"`
}

type featureInputDef struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Instruction string   `json:"instruction"`
	Criteria    []string `json:"acceptance_criteria"`
	WorkerModel string   `json:"worker_model,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	MaxAttempts int      `json:"max_attempts,omitempty"`
}

type validationInputDef struct {
	Checks []validationCheckInputDef `json:"checks"`
}

type validationCheckInputDef struct {
	Name     string `json:"name"`
	Command  string `json:"command"`
	Expected string `json:"expected,omitempty"`
	MustPass bool   `json:"must_pass"`
}

// doPlan creates a mission from the provided definition and saves it.
func (t *MissionTool) doPlan(input missionInput) (string, error) {
	if input.Mission == nil {
		return "", fmt.Errorf("mission plan: mission definition required")
	}
	if input.Mission.Objective == "" {
		return "", fmt.Errorf("mission plan: objective is required")
	}

	m := &mission.Mission{
		ID:        uuid.New().String(),
		Objective: input.Mission.Objective,
		Dir:       input.Mission.Dir,
		Status:    mission.StatusPlanning,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	for _, md := range input.Mission.Milestones {
		ms := &mission.Milestone{
			ID:          uuid.New().String(),
			Name:        md.Name,
			Description: md.Description,
			Status:      mission.MilestoneStatusPending,
		}

		for _, fd := range md.Features {
			maxAttempts := fd.MaxAttempts
			if maxAttempts <= 0 {
				maxAttempts = 3
			}
			f := &mission.Feature{
				ID:          uuid.New().String(),
				Name:        fd.Name,
				Description: fd.Description,
				Instruction: fd.Instruction,
				Criteria:    fd.Criteria,
				WorkerModel: fd.WorkerModel,
				Tools:       fd.Tools,
				MaxAttempts: maxAttempts,
				Status:      mission.FeatureStatusPending,
			}
			ms.Features = append(ms.Features, f)
		}

		if md.Validation != nil {
			vc := &mission.ValidationContract{}
			for _, cd := range md.Validation.Checks {
				vc.Checks = append(vc.Checks, mission.ValidationCheck{
					Name:     cd.Name,
					Command:  cd.Command,
					Expected: cd.Expected,
					MustPass: cd.MustPass,
				})
			}
			ms.Validation = vc
		}

		m.Milestones = append(m.Milestones, ms)
	}

	if err := t.store.Save(m); err != nil {
		return "", fmt.Errorf("mission plan: save: %w", err)
	}

	return fmt.Sprintf("mission created: %s\nobjective: %s\nmilestones: %d\nuse action=run with id=%s to start",
		m.ID, m.Objective, len(m.Milestones), m.ID), nil
}

// doRun loads and executes a mission, streaming progress to output.
func (t *MissionTool) doRun(ctx context.Context, input missionInput, output chan<- string) (string, error) {
	if input.ID == "" {
		return "", fmt.Errorf("mission run: id is required")
	}

	m, err := t.store.Load(input.ID)
	if err != nil {
		return "", fmt.Errorf("mission run: %w", err)
	}

	if err := t.engine.Run(ctx, m, output); err != nil {
		return fmt.Sprintf("mission %s failed: %v\nstatus: %s\nmilestone: %d/%d",
			m.ID, err, m.Status, m.CurrentIdx, len(m.Milestones)), nil
	}

	// Build a summary of completed features.
	var sb strings.Builder
	fmt.Fprintf(&sb, "mission completed: %s\n", m.Objective)
	fmt.Fprintf(&sb, "id: %s\n", m.ID)
	fmt.Fprintf(&sb, "milestones: %d\n", len(m.Milestones))
	for _, ms := range m.Milestones {
		fmt.Fprintf(&sb, "  [%s] %s (%d features)\n", ms.Status, ms.Name, len(ms.Features))
	}
	return sb.String(), nil
}

// doStatus returns a formatted snapshot of a mission's state.
func (t *MissionTool) doStatus(input missionInput) (string, error) {
	if input.ID == "" {
		return "", fmt.Errorf("mission status: id is required")
	}

	m, err := t.store.Load(input.ID)
	if err != nil {
		return "", fmt.Errorf("mission status: %w", err)
	}

	return formatMission(m), nil
}

// doList returns a summary of all stored missions.
func (t *MissionTool) doList() (string, error) {
	missions, err := t.store.List()
	if err != nil {
		return "", fmt.Errorf("mission list: %w", err)
	}
	if len(missions) == 0 {
		return "no missions found", nil
	}

	var sb strings.Builder
	for _, m := range missions {
		fmt.Fprintf(&sb, "[%s] %s — %s (%d milestones)\n",
			m.Status, m.ID, m.Objective, len(m.Milestones))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// formatMission renders a mission as human-readable text.
func formatMission(m *mission.Mission) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "mission: %s\n", m.ID)
	fmt.Fprintf(&sb, "objective: %s\n", m.Objective)
	fmt.Fprintf(&sb, "status: %s\n", m.Status)
	fmt.Fprintf(&sb, "progress: milestone %d/%d\n", m.CurrentIdx, len(m.Milestones))
	fmt.Fprintf(&sb, "updated: %s\n", m.UpdatedAt.Local().Format("2006-01-02 15:04:05"))
	for i, ms := range m.Milestones {
		marker := " "
		if i < m.CurrentIdx {
			marker = "✓"
		} else if i == m.CurrentIdx {
			marker = "→"
		}
		fmt.Fprintf(&sb, "  %s [%s] milestone: %s\n", marker, ms.Status, ms.Name)
		for _, f := range ms.Features {
			fmt.Fprintf(&sb, "       [%s] feature: %s (attempts: %d)\n", f.Status, f.Name, f.Attempts)
			if f.Error != "" {
				fmt.Fprintf(&sb, "              error: %s\n", f.Error)
			}
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
