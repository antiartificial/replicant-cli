package mission

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CheckResult holds the outcome of a single validation check.
type CheckResult struct {
	Check   ValidationCheck
	Passed  bool
	Output  string
	Error   string
}

// RunValidation executes all checks in a ValidationContract and returns
// results for every check, even when some fail. An error is only returned
// for unexpected infrastructure failures (e.g. context cancelled).
func RunValidation(ctx context.Context, dir string, contract *ValidationContract) ([]CheckResult, error) {
	if contract == nil {
		return nil, nil
	}

	results := make([]CheckResult, 0, len(contract.Checks))

	for _, check := range contract.Checks {
		res, err := runCheck(ctx, dir, check)
		if err != nil {
			// Context cancellation is fatal — abort remaining checks.
			return results, err
		}
		results = append(results, res)
	}

	return results, nil
}

// runCheck executes a single ValidationCheck via the shell.
func runCheck(ctx context.Context, dir string, check ValidationCheck) (CheckResult, error) {
	res := CheckResult{Check: check}

	//nolint:gosec // command is defined by the mission config, not user input
	cmd := exec.CommandContext(ctx, "sh", "-c", check.Command)
	cmd.Dir = dir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	runErr := cmd.Run()
	output := out.String()
	res.Output = output

	if ctx.Err() != nil {
		return res, ctx.Err()
	}

	if runErr != nil {
		res.Error = fmt.Sprintf("command failed: %v", runErr)
		res.Passed = false
		return res, nil
	}

	// Success: check substring match when Expected is set.
	if check.Expected != "" {
		res.Passed = strings.Contains(output, check.Expected)
		if !res.Passed {
			res.Error = fmt.Sprintf("expected %q not found in output", check.Expected)
		}
	} else {
		// No expected substring — exit 0 is sufficient.
		res.Passed = true
	}

	return res, nil
}
