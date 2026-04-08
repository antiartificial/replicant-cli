package tools

import "github.com/antiartificial/replicant/internal/permission"

// Tool is the interface all replicant tools implement.
type Tool interface {
	// Name returns the tool's identifier used in replicant definitions.
	Name() string

	// Description returns a human-readable description for the LLM.
	Description() string

	// Parameters returns the JSON Schema for the tool's input.
	Parameters() map[string]any

	// Run executes the tool with the given JSON arguments and returns the result.
	Run(args string) (string, error)

	// Risk returns the risk level for permission checks.
	Risk() permission.RiskLevel
}
