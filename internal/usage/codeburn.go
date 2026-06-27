package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
)

// ErrCodeburnMissing is returned when the codeburn CLI is not on PATH.
var ErrCodeburnMissing = errors.New("codeburn not found on PATH")

// CodeburnStat is one period's totals from codeburn.
type CodeburnStat struct {
	Cost    float64 `json:"cost"`
	Savings float64 `json:"savings"`
	Calls   int     `json:"calls"`
}

// CodeburnReport is `codeburn status --format json`. codeburn already reads
// every tool's session files and prices them, so for the long tail of tools
// council does not launch we relay its machine-wide numbers rather than
// re-deriving them (and can't attribute them to council panes).
type CodeburnReport struct {
	Currency string       `json:"currency"`
	Today    CodeburnStat `json:"today"`
	Month    CodeburnStat `json:"month"`
}

// CodeburnAvailable reports whether the codeburn CLI can be run.
func CodeburnAvailable() bool {
	_, err := exec.LookPath("codeburn")
	return err == nil
}

// RunCodeburn shells out to `codeburn status --format json`. Returns
// ErrCodeburnMissing when codeburn isn't installed.
func RunCodeburn(ctx context.Context) (CodeburnReport, error) {
	if !CodeburnAvailable() {
		return CodeburnReport{}, ErrCodeburnMissing
	}
	cmd := exec.CommandContext(ctx, "codeburn", "status", "--format", "json")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return CodeburnReport{}, err
	}
	return ParseCodeburnStatus(out.Bytes())
}

// ParseCodeburnStatus decodes codeburn's status JSON (split out so it is
// testable without the codeburn binary installed).
func ParseCodeburnStatus(data []byte) (CodeburnReport, error) {
	var r CodeburnReport
	if err := json.Unmarshal(data, &r); err != nil {
		return CodeburnReport{}, err
	}
	if r.Currency == "" {
		return CodeburnReport{}, errors.New("codeburn status: missing currency (unexpected output)")
	}
	return r, nil
}
