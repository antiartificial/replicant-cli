package permission

// RiskLevel classifies tool operations by their reversibility and impact.
type RiskLevel int

const (
	// RiskNone — safe, read-only operations (read_file, glob, grep).
	RiskNone RiskLevel = iota

	// RiskLow — reversible writes (edit_file, write_file).
	RiskLow

	// RiskHigh — potentially destructive or externally visible (execute, github).
	RiskHigh
)

func (r RiskLevel) String() string {
	switch r {
	case RiskNone:
		return "none"
	case RiskLow:
		return "low"
	case RiskHigh:
		return "high"
	default:
		return "unknown"
	}
}

// NeedsConfirmation returns true if a tool at this risk level requires
// user approval before execution.
func (r RiskLevel) NeedsConfirmation(autonomy AutonomyLevel) bool {
	switch autonomy {
	case AutonomyFull:
		return false
	case AutonomyHigh:
		return r >= RiskHigh
	case AutonomyNormal:
		return r >= RiskLow
	default: // AutonomyOff
		return r > RiskNone
	}
}

// AutonomyLevel controls how much the replicant can do without asking.
type AutonomyLevel int

const (
	AutonomyOff    AutonomyLevel = iota // confirm everything except reads
	AutonomyNormal                      // auto-approve reads + edits, confirm shell
	AutonomyHigh                        // auto-approve most, confirm destructive
	AutonomyFull                        // never ask (yolo mode)
)
