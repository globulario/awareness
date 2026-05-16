// Package fixledger tracks the status of known fixes, guardrails, and implementation
// obligations derived from incident reports and the guardrails.md document.
package fixledger

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// FixStatus is the lifecycle state of a fix case.
type FixStatus string

const (
	FixProposed   FixStatus = "PROPOSED"
	FixInProgress FixStatus = "IN_PROGRESS"
	FixPartial    FixStatus = "PARTIAL"
	FixDone       FixStatus = "DONE"
	FixRegressed  FixStatus = "REGRESSED"
	FixSuperseded FixStatus = "SUPERSEDED"
	FixUnknown    FixStatus = "UNKNOWN"
)

// FixCase represents a single tracked fix with its status, scope, and test obligations.
type FixCase struct {
	ID               string    `yaml:"id"`
	Title            string    `yaml:"title"`
	Status           FixStatus `yaml:"status"`
	Pattern          string    `yaml:"pattern"` // searchable pattern for task matching
	Category         string    `yaml:"category,omitempty"`
	TargetInvariants []string  `yaml:"target_invariants,omitempty"`
	FixedFiles       []string  `yaml:"fixed_files,omitempty"`
	RemainingFiles   []string  `yaml:"remaining_files,omitempty"`
	RequiredTests    []string  `yaml:"required_tests,omitempty"`
	DoD              string    `yaml:"dod,omitempty"`
	Summary          string    `yaml:"summary,omitempty"`
	Notes            string    `yaml:"notes,omitempty"`
}

// fixCasesFile is the top-level structure of fix_cases.yaml.
type fixCasesFile struct {
	FixCases []FixCase `yaml:"fix_cases"`
}

// LoadFixCases reads fix_cases.yaml at path and returns the list of fix cases.
// Returns an empty slice if the file does not exist.
func LoadFixCases(path string) ([]FixCase, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("LoadFixCases %s: %w", path, err)
	}
	var f fixCasesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("LoadFixCases parse %s: %w", path, err)
	}
	return f.FixCases, nil
}
