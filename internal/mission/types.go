package mission

import "time"

type MissionStatus string

const (
	StatusPlanning  MissionStatus = "planning"
	StatusRunning   MissionStatus = "running"
	StatusPaused    MissionStatus = "paused"
	StatusCompleted MissionStatus = "completed"
	StatusFailed    MissionStatus = "failed"
)

type MilestoneStatus string

const (
	MilestoneStatusPending   MilestoneStatus = "pending"
	MilestoneStatusRunning   MilestoneStatus = "running"
	MilestoneStatusCompleted MilestoneStatus = "completed"
	MilestoneStatusFailed    MilestoneStatus = "failed"
)

type FeatureStatus string

const (
	FeatureStatusPending   FeatureStatus = "pending"
	FeatureStatusRunning   FeatureStatus = "running"
	FeatureStatusCompleted FeatureStatus = "completed"
	FeatureStatusFailed    FeatureStatus = "failed"
	FeatureStatusFixing    FeatureStatus = "fixing"
)

// Mission represents a top-level objective broken down into milestones.
type Mission struct {
	ID         string        `json:"id"`
	Objective  string        `json:"objective"`
	Milestones []*Milestone  `json:"milestones"`
	Status     MissionStatus `json:"status"`
	CurrentIdx int           `json:"current_milestone_idx"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
	Dir        string        `json:"dir"` // working directory
}

// Milestone is a discrete phase of a mission, containing features and an
// optional validation contract that must pass before advancing.
type Milestone struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Features    []*Feature          `json:"features"`
	Validation  *ValidationContract `json:"validation,omitempty"`
	Status      MilestoneStatus     `json:"status"`
}

// Feature is a single unit of work assigned to a child agent.
type Feature struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Criteria    []string      `json:"acceptance_criteria"`
	Instruction string        `json:"instruction"`         // system prompt for worker
	WorkerModel string        `json:"worker_model,omitempty"`
	Tools       []string      `json:"tools,omitempty"`
	Status      FeatureStatus `json:"status"`
	Result      string        `json:"result,omitempty"`
	Error       string        `json:"error,omitempty"`
	Attempts    int           `json:"attempts"`
	MaxAttempts int           `json:"max_attempts"` // default 3
}

// ValidationContract describes how to verify a milestone's completion.
type ValidationContract struct {
	Checks []ValidationCheck `json:"checks"`
}

// ValidationCheck is a single shell-command-based verification step.
type ValidationCheck struct {
	Name     string `json:"name"`
	Command  string `json:"command"`
	Expected string `json:"expected,omitempty"` // substring match on output
	MustPass bool   `json:"must_pass"`
}
