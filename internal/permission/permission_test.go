package permission

import "testing"

// ---------------------------------------------------------------------------
// RiskLevel.String()
// ---------------------------------------------------------------------------

func TestRiskLevel_String(t *testing.T) {
	tests := []struct {
		level RiskLevel
		want  string
	}{
		{RiskNone, "none"},
		{RiskLow, "low"},
		{RiskHigh, "high"},
		{RiskLevel(99), "unknown"},
	}
	for _, tt := range tests {
		got := tt.level.String()
		if got != tt.want {
			t.Errorf("RiskLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// NeedsConfirmation
// ---------------------------------------------------------------------------

// TestNeedsConfirmation verifies all combinations of RiskLevel × AutonomyLevel.
func TestRiskLevel_NeedsConfirmation(t *testing.T) {
	type tc struct {
		risk     RiskLevel
		autonomy AutonomyLevel
		want     bool
	}

	tests := []tc{
		// AutonomyFull — never needs confirmation.
		{RiskNone, AutonomyFull, false},
		{RiskLow, AutonomyFull, false},
		{RiskHigh, AutonomyFull, false},

		// AutonomyHigh — only RiskHigh needs confirmation.
		{RiskNone, AutonomyHigh, false},
		{RiskLow, AutonomyHigh, false},
		{RiskHigh, AutonomyHigh, true},

		// AutonomyNormal — RiskLow and above need confirmation.
		{RiskNone, AutonomyNormal, false},
		{RiskLow, AutonomyNormal, true},
		{RiskHigh, AutonomyNormal, true},

		// AutonomyOff — anything above RiskNone needs confirmation.
		{RiskNone, AutonomyOff, false},
		{RiskLow, AutonomyOff, true},
		{RiskHigh, AutonomyOff, true},
	}

	for _, tt := range tests {
		got := tt.risk.NeedsConfirmation(tt.autonomy)
		if got != tt.want {
			t.Errorf("RiskLevel(%v).NeedsConfirmation(AutonomyLevel(%d)) = %v, want %v",
				tt.risk, tt.autonomy, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// AutonomyLevel constants — smoke test ordering is as documented.
// ---------------------------------------------------------------------------

func TestAutonomyLevel_Order(t *testing.T) {
	// The constants must be in ascending order: Off < Normal < High < Full.
	if !(AutonomyOff < AutonomyNormal) {
		t.Error("expected AutonomyOff < AutonomyNormal")
	}
	if !(AutonomyNormal < AutonomyHigh) {
		t.Error("expected AutonomyNormal < AutonomyHigh")
	}
	if !(AutonomyHigh < AutonomyFull) {
		t.Error("expected AutonomyHigh < AutonomyFull")
	}
}

// ---------------------------------------------------------------------------
// RiskLevel constants — ordering.
// ---------------------------------------------------------------------------

func TestRiskLevel_Order(t *testing.T) {
	if !(RiskNone < RiskLow) {
		t.Error("expected RiskNone < RiskLow")
	}
	if !(RiskLow < RiskHigh) {
		t.Error("expected RiskLow < RiskHigh")
	}
}

// ---------------------------------------------------------------------------
// NeedsConfirmation — exhaustive boundary tests.
// ---------------------------------------------------------------------------

func TestNeedsConfirmation_Boundaries(t *testing.T) {
	// A hypothetical "unknown" autonomy (default iota = 0 == AutonomyOff).
	// Any risk > RiskNone with an unrecognised autonomy goes to the default branch.
	unknown := AutonomyLevel(99)

	// RiskNone should never need confirmation regardless of autonomy.
	if RiskNone.NeedsConfirmation(unknown) {
		t.Error("RiskNone should never need confirmation, even for unknown autonomy")
	}

	// RiskHigh with unknown autonomy should need confirmation (default branch: r > RiskNone).
	if !RiskHigh.NeedsConfirmation(unknown) {
		t.Error("RiskHigh should need confirmation for unknown autonomy level")
	}
}
