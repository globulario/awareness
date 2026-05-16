package fixledger

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Guardrail represents a high-level architectural invariant that may require
// one or more fix cases to be fully implemented.
type Guardrail struct {
	ID               string    `yaml:"id"`
	Title            string    `yaml:"title"`
	Priority         string    `yaml:"priority"` // P0/P1/P2
	Status           FixStatus `yaml:"status"`
	Category         string    `yaml:"category"`
	TargetInvariants []string  `yaml:"target_invariants,omitempty"`
	RequiredFixes    []string  `yaml:"required_fixes,omitempty"`
	Summary          string    `yaml:"summary,omitempty"`
}

// guardrailsFile is the top-level structure of guardrails.yaml.
type guardrailsFile struct {
	Guardrails []Guardrail `yaml:"guardrails"`
}

// LoadGuardrails reads guardrails.yaml at path and returns the list of guardrails.
// Returns an empty slice if the file does not exist.
func LoadGuardrails(path string) ([]Guardrail, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("LoadGuardrails %s: %w", path, err)
	}
	var f guardrailsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("LoadGuardrails parse %s: %w", path, err)
	}
	return f.Guardrails, nil
}
