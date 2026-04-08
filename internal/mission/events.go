package mission

import "fmt"

// EventType identifies the kind of progress event emitted by the engine.
type EventType int

const (
	EventMissionStarted EventType = iota
	EventMilestoneStarted
	EventFeatureStarted
	EventFeatureCompleted
	EventFeatureFailed
	EventValidationStarted
	EventValidationResult
	EventMilestoneCompleted
	EventMilestoneFailed
	EventMissionCompleted
	EventMissionFailed
)

// Event carries a progress update from the mission engine.
type Event struct {
	Type        EventType
	MissionID   string
	MilestoneID string
	FeatureID   string
	Message     string
	IsError     bool
}

// String returns a human-readable representation of the event.
func (e Event) String() string {
	prefix := ""
	if e.IsError {
		prefix = "ERROR "
	}
	switch e.Type {
	case EventMissionStarted:
		return fmt.Sprintf("%smission started: %s", prefix, e.Message)
	case EventMilestoneStarted:
		return fmt.Sprintf("%s  milestone started: %s", prefix, e.Message)
	case EventFeatureStarted:
		return fmt.Sprintf("%s    feature started: %s", prefix, e.Message)
	case EventFeatureCompleted:
		return fmt.Sprintf("%s    feature completed: %s", prefix, e.Message)
	case EventFeatureFailed:
		return fmt.Sprintf("%s    feature FAILED: %s", prefix, e.Message)
	case EventValidationStarted:
		return fmt.Sprintf("%s  validation: %s", prefix, e.Message)
	case EventValidationResult:
		return fmt.Sprintf("%s    check: %s", prefix, e.Message)
	case EventMilestoneCompleted:
		return fmt.Sprintf("%s  milestone completed: %s", prefix, e.Message)
	case EventMilestoneFailed:
		return fmt.Sprintf("%s  milestone FAILED: %s", prefix, e.Message)
	case EventMissionCompleted:
		return fmt.Sprintf("%smission completed: %s", prefix, e.Message)
	case EventMissionFailed:
		return fmt.Sprintf("%smission FAILED: %s", prefix, e.Message)
	default:
		return fmt.Sprintf("%s%s", prefix, e.Message)
	}
}
