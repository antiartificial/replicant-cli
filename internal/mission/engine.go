package mission

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/antiartificial/replicant/internal/agent"
	"github.com/antiartificial/replicant/internal/permission"
)

// defaultFeatureTools is the tool set given to feature worker agents when the
// feature does not specify an explicit list.
var defaultFeatureTools = []string{
	"read_file",
	"write_file",
	"edit_file",
	"list_dir",
	"execute",
	"glob",
	"grep",
}

// ToolRegistry is the interface the engine uses to resolve and execute tools.
// It is satisfied by *tools.Registry; defined here to avoid an import cycle.
type ToolRegistry interface {
	// Resolve returns the tools corresponding to the given names.
	Resolve(names []string) []ToolEntry
	// Get returns a single tool by name.
	Get(name string) (ToolEntry, bool)
}

// ToolEntry is the minimal interface the engine needs from a tool.
type ToolEntry interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Run(args string) (string, error)
	Risk() permission.RiskLevel
}

// Engine drives the full mission lifecycle: planning, feature execution,
// validation, and retry loops.
type Engine struct {
	toolRegistry    ToolRegistry
	providerFactory func(model string) (agent.Provider, string, error)
	defaultModel    string
	store           *Store
}

// NewEngine constructs an Engine.
func NewEngine(
	toolReg ToolRegistry,
	providerFactory func(model string) (agent.Provider, string, error),
	defaultModel string,
	store *Store,
) *Engine {
	return &Engine{
		toolRegistry:    toolReg,
		providerFactory: providerFactory,
		defaultModel:    defaultModel,
		store:           store,
	}
}

// Run executes a mission from its current state through to completion (or
// failure). Progress strings are sent to the progress channel as they occur;
// the caller is responsible for draining it. The channel is never closed by
// Run.
func (e *Engine) Run(ctx context.Context, m *Mission, progress chan<- string) error {
	m.Status = StatusRunning
	m.UpdatedAt = time.Now()
	_ = e.store.Save(m)

	send := func(ev Event) {
		if progress != nil {
			progress <- ev.String()
		}
	}

	send(Event{
		Type:      EventMissionStarted,
		MissionID: m.ID,
		Message:   m.Objective,
	})

	for i := m.CurrentIdx; i < len(m.Milestones); i++ {
		ms := m.Milestones[i]

		if err := e.runMilestone(ctx, m, ms, send); err != nil {
			m.Status = StatusFailed
			m.UpdatedAt = time.Now()
			_ = e.store.Save(m)

			send(Event{
				Type:      EventMissionFailed,
				MissionID: m.ID,
				Message:   fmt.Sprintf("%s: %v", m.Objective, err),
				IsError:   true,
			})
			return err
		}

		// Advance the checkpoint.
		m.CurrentIdx = i + 1
		m.UpdatedAt = time.Now()
		_ = e.store.Save(m)
	}

	m.Status = StatusCompleted
	m.UpdatedAt = time.Now()
	_ = e.store.Save(m)

	send(Event{
		Type:      EventMissionCompleted,
		MissionID: m.ID,
		Message:   m.Objective,
	})
	return nil
}

// runMilestone drives a single milestone through its feature execution and
// validation loop.
func (e *Engine) runMilestone(ctx context.Context, m *Mission, ms *Milestone, send func(Event)) error {
	ms.Status = MilestoneStatusRunning
	_ = e.store.Save(m)

	send(Event{
		Type:        EventMilestoneStarted,
		MissionID:   m.ID,
		MilestoneID: ms.ID,
		Message:     ms.Name,
	})

	// Retry loop: run pending/fixing features, validate, repeat if needed.
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Gather features that need execution.
		var toRun []*Feature
		for _, f := range ms.Features {
			switch f.Status {
			case FeatureStatusPending, FeatureStatusFixing, "":
				toRun = append(toRun, f)
			}
		}

		if len(toRun) > 0 {
			e.runFeaturesParallel(ctx, m, ms, toRun, send)
		}

		// Check for hard failures (exhausted retries).
		var hardFailed []*Feature
		for _, f := range ms.Features {
			if f.Status == FeatureStatusFailed {
				hardFailed = append(hardFailed, f)
			}
		}
		if len(hardFailed) > 0 {
			names := make([]string, len(hardFailed))
			for i, f := range hardFailed {
				names[i] = f.Name
			}
			ms.Status = MilestoneStatusFailed
			_ = e.store.Save(m)

			send(Event{
				Type:        EventMilestoneFailed,
				MissionID:   m.ID,
				MilestoneID: ms.ID,
				Message:     fmt.Sprintf("%s: features failed: %s", ms.Name, strings.Join(names, ", ")),
				IsError:     true,
			})
			return fmt.Errorf("milestone %q: features failed: %s", ms.Name, strings.Join(names, ", "))
		}

		// Run validation if present.
		if ms.Validation != nil && len(ms.Validation.Checks) > 0 {
			send(Event{
				Type:        EventValidationStarted,
				MissionID:   m.ID,
				MilestoneID: ms.ID,
				Message:     ms.Name,
			})

			dir := m.Dir
			if dir == "" {
				dir = "."
			}

			results, err := RunValidation(ctx, dir, ms.Validation)
			if err != nil {
				return fmt.Errorf("validation: %w", err)
			}

			// Report each check.
			allPassed := true
			var failedChecks []CheckResult
			for _, r := range results {
				status := "PASS"
				if !r.Passed {
					status = "FAIL"
					allPassed = false
					failedChecks = append(failedChecks, r)
				}
				msg := fmt.Sprintf("[%s] %s", status, r.Check.Name)
				if r.Error != "" {
					msg += ": " + r.Error
				}
				send(Event{
					Type:        EventValidationResult,
					MissionID:   m.ID,
					MilestoneID: ms.ID,
					Message:     msg,
					IsError:     !r.Passed && r.Check.MustPass,
				})
			}

			if !allPassed {
				// Try to fix features related to failed checks.
				anyFixing := e.scheduleRetries(ms, failedChecks)
				if !anyFixing {
					// No features can be retried — milestone fails.
					ms.Status = MilestoneStatusFailed
					_ = e.store.Save(m)

					send(Event{
						Type:        EventMilestoneFailed,
						MissionID:   m.ID,
						MilestoneID: ms.ID,
						Message:     fmt.Sprintf("%s: validation failed and no features can retry", ms.Name),
						IsError:     true,
					})
					return fmt.Errorf("milestone %q: validation failed", ms.Name)
				}
				// Loop back to re-run the fixing features.
				continue
			}
		}

		// All features completed and validation passed (or absent).
		break
	}

	ms.Status = MilestoneStatusCompleted
	_ = e.store.Save(m)

	send(Event{
		Type:        EventMilestoneCompleted,
		MissionID:   m.ID,
		MilestoneID: ms.ID,
		Message:     ms.Name,
	})
	return nil
}

// runFeaturesParallel executes a slice of features concurrently and waits for
// all of them to finish.
func (e *Engine) runFeaturesParallel(ctx context.Context, m *Mission, ms *Milestone, features []*Feature, send func(Event)) {
	var wg sync.WaitGroup
	for _, f := range features {
		wg.Add(1)
		go func(feat *Feature) {
			defer wg.Done()
			e.runFeature(ctx, m, feat, send)
			_ = e.store.Save(m)
		}(f)
	}
	wg.Wait()
}

// runFeature executes a single feature using a child agent, following the same
// pattern as spawn.go's RunWithContext.
func (e *Engine) runFeature(ctx context.Context, m *Mission, f *Feature, send func(Event)) {
	f.Status = FeatureStatusRunning
	f.Attempts++

	send(Event{
		Type:      EventFeatureStarted,
		MissionID: m.ID,
		FeatureID: f.ID,
		Message:   fmt.Sprintf("%s (attempt %d)", f.Name, f.Attempts),
	})

	result, err := e.executeFeatureAgent(ctx, m, f)
	if err != nil {
		f.Error = err.Error()

		maxAttempts := f.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 3
		}

		if f.Attempts >= maxAttempts {
			f.Status = FeatureStatusFailed
			send(Event{
				Type:      EventFeatureFailed,
				MissionID: m.ID,
				FeatureID: f.ID,
				Message:   fmt.Sprintf("%s: %v (out of retries)", f.Name, err),
				IsError:   true,
			})
		} else {
			// Will be retried by the milestone loop.
			f.Status = FeatureStatusFixing
			send(Event{
				Type:      EventFeatureFailed,
				MissionID: m.ID,
				FeatureID: f.ID,
				Message:   fmt.Sprintf("%s: %v (will retry)", f.Name, err),
				IsError:   true,
			})
		}
		return
	}

	f.Result = result
	f.Status = FeatureStatusCompleted
	send(Event{
		Type:      EventFeatureCompleted,
		MissionID: m.ID,
		FeatureID: f.ID,
		Message:   f.Name,
	})
}

// executeFeatureAgent runs the child agent for a feature and returns its text
// output. This mirrors the pattern in spawn.go's RunWithContext exactly.
func (e *Engine) executeFeatureAgent(ctx context.Context, m *Mission, f *Feature) (string, error) {
	// Resolve model.
	model := f.WorkerModel
	if model == "" {
		model = e.defaultModel
	}
	prov, bareModel, err := e.providerFactory(model)
	if err != nil {
		return "", fmt.Errorf("feature %q: provider for %q: %w", f.Name, model, err)
	}

	// Resolve tools.
	toolNames := f.Tools
	if len(toolNames) == 0 {
		toolNames = defaultFeatureTools
	}

	resolvedTools := e.toolRegistry.Resolve(toolNames)
	toolDefs := make([]agent.ToolDef, len(resolvedTools))
	for i, te := range resolvedTools {
		toolDefs[i] = agent.ToolDef{
			Name:        te.Name(),
			Description: te.Description(),
			InputSchema: te.Parameters(),
		}
	}

	toolRunner := func(name string, runArgs string) (string, error) {
		te, ok := e.toolRegistry.Get(name)
		if !ok {
			return "", fmt.Errorf("unknown tool: %s", name)
		}
		return te.Run(runArgs)
	}

	// Child agents auto-approve everything below RiskHigh.
	childPermFn := func(name, _ string) (bool, error) {
		te, ok := e.toolRegistry.Get(name)
		if !ok {
			return false, nil
		}
		return te.Risk() < permission.RiskHigh, nil
	}

	// Build the system prompt: feature instruction + acceptance criteria.
	systemPrompt := f.Instruction
	if len(f.Criteria) > 0 {
		var sb strings.Builder
		sb.WriteString(systemPrompt)
		sb.WriteString("\n\nAcceptance criteria:\n")
		for _, c := range f.Criteria {
			sb.WriteString("- ")
			sb.WriteString(c)
			sb.WriteString("\n")
		}
		if m.Dir != "" {
			sb.WriteString("\nWorking directory: ")
			sb.WriteString(m.Dir)
		}
		systemPrompt = sb.String()
	}

	childAgent := agent.NewAgent(prov,
		agent.WithSystemPrompt(systemPrompt),
		agent.WithModel(bareModel),
		agent.WithTools(toolDefs),
		agent.WithToolRunner(toolRunner),
		agent.WithMaxTurns(30),
		agent.WithPermissionFn(childPermFn),
	)

	events := make(chan agent.Event, 128)

	go func() {
		childAgent.Run(ctx, f.Description, nil, events)
		close(events)
	}()

	var textParts []string
	var runErr error

	for ev := range events {
		switch ev.Type {
		case agent.EventText:
			textParts = append(textParts, ev.Text)
		case agent.EventError:
			runErr = ev.Error
		}
	}

	if runErr != nil {
		partial := strings.Join(textParts, "")
		if partial != "" {
			return fmt.Sprintf("[feature %q error: %v]\n\n%s", f.Name, runErr, partial), nil
		}
		return "", fmt.Errorf("feature %q: %w", f.Name, runErr)
	}

	result := strings.Join(textParts, "")
	if result == "" {
		return fmt.Sprintf("(feature %q completed with no text output)", f.Name), nil
	}
	return result, nil
}

// scheduleRetries marks features for retry based on failed validation checks.
// Features are matched by name substring against the check name. If no match
// is found, all incomplete features are scheduled for retry. Returns true if
// at least one feature can still be retried.
func (e *Engine) scheduleRetries(ms *Milestone, failedChecks []CheckResult) bool {
	maxAttempts := 3 // default
	anyFixing := false

	// Build a set of feature names to retry derived from failed check names.
	// Strategy: if a feature name appears in a check name (or vice-versa),
	// consider it related. Fallback: retry all non-failed features.
	relatedFeatures := make(map[string]bool)
	for _, r := range failedChecks {
		checkNameLower := strings.ToLower(r.Check.Name)
		for _, f := range ms.Features {
			if strings.Contains(checkNameLower, strings.ToLower(f.Name)) ||
				strings.Contains(strings.ToLower(f.Name), checkNameLower) {
				relatedFeatures[f.ID] = true
			}
		}
	}

	// If nothing matched by name, fall back to all completed features.
	if len(relatedFeatures) == 0 {
		for _, f := range ms.Features {
			if f.Status == FeatureStatusCompleted {
				relatedFeatures[f.ID] = true
			}
		}
	}

	for _, f := range ms.Features {
		if !relatedFeatures[f.ID] {
			continue
		}
		ma := f.MaxAttempts
		if ma <= 0 {
			ma = maxAttempts
		}
		if f.Attempts < ma {
			f.Status = FeatureStatusFixing
			anyFixing = true
		} else {
			f.Status = FeatureStatusFailed
		}
	}

	return anyFixing
}
