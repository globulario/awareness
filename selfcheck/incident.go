package selfcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// IncidentBundle is a minimal incident YAML written by self-check.
// It mirrors the structure in learning.IncidentBundle but is kept
// local to avoid a circular import. The file can be loaded by
// `globular awareness propose-from-incident` after human review.
type IncidentBundle struct {
	IncidentID         string   `yaml:"incident_id"`
	Title              string   `yaml:"title"`
	Status             string   `yaml:"status"`
	Severity           string   `yaml:"severity"`
	GeneratedAt        string   `yaml:"generated_at"`
	Symptoms           []string `yaml:"symptoms"`
	SuspectedRootCause string   `yaml:"suspected_root_cause"`
	FalseSilences      []string `yaml:"false_silences,omitempty"`
	MCPIssues          []string `yaml:"mcp_issues,omitempty"`
	StaleRefs          []string `yaml:"stale_refs,omitempty"`
	RecommendedFixes   []string `yaml:"recommended_fixes,omitempty"`
	// Note: no proposed.* block — self-check does not generate proposals.
	// Run `globular awareness propose-from-incident <id>` after human review.
}

// CreateIncidentBundle writes a self-check incident bundle to docsDir/incidents/.
// It returns the path of the written file.
//
// Safety contract: this function NEVER generates a proposal, NEVER sets status
// to APPROVED or PROMOTED, and NEVER writes to invariants.yaml or failure_modes.yaml.
func CreateIncidentBundle(r *Report, docsDir string) (string, error) {
	if docsDir == "" {
		return "", fmt.Errorf("docsDir is required to create an incident bundle")
	}

	incidentsDir := filepath.Join(docsDir, "incidents")
	if err := os.MkdirAll(incidentsDir, 0o755); err != nil {
		return "", fmt.Errorf("create incidents dir: %w", err)
	}

	ts := r.GeneratedAt.UTC().Format("20060102T150405")
	id := fmt.Sprintf("selfcheck_%s", ts)
	path := filepath.Join(incidentsDir, id+".yaml")

	severity := "low"
	if len(r.FalseSilences) > 0 {
		severity = "medium"
	}
	if len(r.MCPIssues) > 0 {
		severity = "high"
	}

	symptoms := buildSymptoms(r)

	bundle := IncidentBundle{
		IncidentID:  id,
		Title:       fmt.Sprintf("Awareness self-check found %d issues on %s", countFails(r), r.GeneratedAt.Format("2006-01-02")),
		Status:      "open",
		Severity:    severity,
		GeneratedAt: r.GeneratedAt.UTC().Format(time.RFC3339),
		Symptoms:    symptoms,
		SuspectedRootCause: buildRootCause(r),
		FalseSilences:    r.FalseSilences,
		MCPIssues:        r.MCPIssues,
		StaleRefs:        r.StaleRefs,
		RecommendedFixes: r.RecommendedFixes,
	}

	data, err := yaml.Marshal(bundle)
	if err != nil {
		return "", fmt.Errorf("marshal incident bundle: %w", err)
	}

	header := fmt.Sprintf("# Self-check incident — generated %s\n# Review findings before running: globular awareness propose-from-incident %s\n# DO NOT auto-approve or auto-promote this bundle.\n",
		r.GeneratedAt.Format("2006-01-02T15:04:05Z"), id)

	if err := os.WriteFile(path, append([]byte(header), data...), 0o644); err != nil {
		return "", fmt.Errorf("write incident bundle: %w", err)
	}

	return path, nil
}

func countFails(r *Report) int {
	n := 0
	for _, cr := range r.Checks {
		if cr.Status == StatusFail {
			n++
		}
	}
	return n
}

func buildSymptoms(r *Report) []string {
	var out []string
	for _, cr := range r.Checks {
		if cr.Status == StatusFail || cr.Status == StatusWeak {
			out = append(out, fmt.Sprintf("[%s] %s: %s", cr.Status, cr.Name, cr.Detail))
		}
	}
	return out
}

func buildRootCause(r *Report) string {
	var parts []string
	if len(r.FalseSilences) > 0 {
		parts = append(parts, fmt.Sprintf("%d false silences — awareness graph may be missing context aliases or keyword coverage", len(r.FalseSilences)))
	}
	if len(r.MCPIssues) > 0 {
		parts = append(parts, "MCP tool registration exposes promotion (must be CLI-only)")
	}
	if len(r.StaleRefs) > 0 {
		parts = append(parts, fmt.Sprintf("%d stale graph references — run 'globular awareness build'", len(r.StaleRefs)))
	}
	if len(parts) == 0 {
		return "unknown — no specific root cause identified"
	}
	return strings.Join(parts, "; ")
}
