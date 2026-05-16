package learning

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
)

// PromoteOptions controls optional behaviour during promotion.
type PromoteOptions struct {
	// AllowUnapproved permits promotion of proposals that have not yet
	// reached APPROVED status. Use only in developer / test mode.
	AllowUnapproved bool
}

// PromotionResult summarises what was written during promotion.
type PromotionResult struct {
	InvariantsAdded     []string
	FailureModesAdded   []string
	ForbiddenFixesAdded []string
	AliasesAdded        int
	GraphRebuildNeeded  bool
}

// PromoteProposal promotes an approved proposal into the docs/awareness directory.
//
// It must be called with a ProposalValidationResult that has status PASS.
// By default, the proposal must have status APPROVED. Pass PromoteOptions{AllowUnapproved: true}
// to bypass the status check (developer/test mode only).
//
// It writes into the approved YAML files (invariants.yaml, failure_modes.yaml,
// forbidden_fixes.yaml, context_aliases.yaml) as append-only merges.
// Existing entries are preserved unless extended.
//
// The graph status is updated BEFORE any YAML files are written so that if
// the graph update fails, no YAML mutations occur.
// The caller is responsible for triggering a graph rebuild.
func PromoteProposal(ctx context.Context, p *ProposalSpec, vr *ProposalValidationResult, docsAwarenessDir string, g *graph.Graph, opts ...PromoteOptions) (*PromotionResult, error) {
	if vr == nil || vr.Status != ValidationPass {
		return nil, fmt.Errorf("promote: proposal %q has not been validated (run validate-proposal first)", p.Proposal.ID)
	}

	var o PromoteOptions
	if len(opts) > 0 {
		o = opts[0]
	}

	// Status gate: require APPROVED unless caller explicitly bypasses it.
	if !o.AllowUnapproved && p.Proposal.Status != StatusApproved {
		return nil, fmt.Errorf("proposal has status %q; promotion requires APPROVED status (use --allow-unapproved to override in developer mode)", p.Proposal.Status)
	}
	if !o.AllowUnapproved && strings.TrimSpace(p.Proposal.ApprovedBy) == "" {
		return nil, fmt.Errorf("proposal %q is APPROVED but missing approved_by reviewer identity", p.Proposal.ID)
	}

	result := &PromotionResult{GraphRebuildNeeded: true}

	// Graph status update FIRST — before any YAML writes.
	// If this fails, no YAML is modified.
	if g != nil {
		if err := g.UpdateProposalStatus(ctx, p.Proposal.ID, graph.ProposalStatusPromoted); err != nil {
			return nil, fmt.Errorf("promote: update graph status: %w", err)
		}
	}

	// Promote invariants.
	if len(p.Invariants) > 0 {
		added, err := mergeInvariants(filepath.Join(docsAwarenessDir, "invariants.yaml"), p.Invariants)
		if err != nil {
			return nil, err
		}
		result.InvariantsAdded = added
	}

	// Promote failure modes.
	if len(p.FailureModes) > 0 {
		added, err := mergeFailureModes(filepath.Join(docsAwarenessDir, "failure_modes.yaml"), p.FailureModes)
		if err != nil {
			return nil, err
		}
		result.FailureModesAdded = added
	}

	// Promote forbidden fixes.
	if len(p.ForbiddenFixes) > 0 {
		added, err := mergeForbiddenFixes(filepath.Join(docsAwarenessDir, "forbidden_fixes.yaml"), p.ForbiddenFixes)
		if err != nil {
			return nil, err
		}
		result.ForbiddenFixesAdded = added
	}

	// Promote context aliases.
	if len(p.ContextAliases) > 0 {
		n, err := mergeContextAliases(filepath.Join(docsAwarenessDir, "context_aliases.yaml"), p.ContextAliases)
		if err != nil {
			return nil, err
		}
		result.AliasesAdded = n
	}

	// Write a .promoted marker file into the proposals directory.
	markerPath := filepath.Join(docsAwarenessDir, "proposals",
		sanitiseID(p.Proposal.ID)+".promoted")
	_ = os.WriteFile(markerPath, []byte(fmt.Sprintf(
		"promoted_at: %s\nproposal_id: %s\nsource_incident: %s\napproved_by: %s\napproved_at: %s\n",
		time.Now().UTC().Format(time.RFC3339),
		p.Proposal.ID,
		p.Proposal.SourceIncident,
		p.Proposal.ApprovedBy,
		p.Proposal.ApprovedAt,
	)), 0o644)

	return result, nil
}

// ---- YAML file merge helpers ----

// invariantFile mirrors the top-level structure of invariants.yaml.
type invariantFile struct {
	Invariants []rawInvariant `yaml:"invariants"`
}

type rawInvariant struct {
	ID                  string       `yaml:"id"`
	Title               string       `yaml:"title"`
	Severity            string       `yaml:"severity"`
	Status              string       `yaml:"status,omitempty"`
	Summary             string       `yaml:"summary"`
	Protects            *rawProtects `yaml:"protects,omitempty"`
	ForbiddenFixes      []string     `yaml:"forbidden_fixes,omitempty"`
	RequiredTests       []string     `yaml:"required_tests,omitempty"`
	RelatedFailureModes []string     `yaml:"related_failure_modes,omitempty"`
}

type rawProtects struct {
	State    []string `yaml:"state,omitempty"`
	Files    []string `yaml:"files,omitempty"`
	Symbols  []string `yaml:"symbols,omitempty"`
	Services []string `yaml:"services,omitempty"`
}

func mergeInvariants(path string, proposed []ProposedInvariant) ([]string, error) {
	var f invariantFile
	if err := loadYAMLFile(path, &f); err != nil {
		return nil, err
	}

	existing := make(map[string]bool)
	for _, inv := range f.Invariants {
		existing[inv.ID] = true
	}

	var added []string
	for _, inv := range proposed {
		if existing[inv.ID] {
			continue
		}
		ri := rawInvariant{
			ID:             inv.ID,
			Title:          inv.Title,
			Severity:       inv.Severity,
			Status:         "active",
			Summary:        inv.Summary,
			ForbiddenFixes: inv.ForbiddenFixes,
			RequiredTests:  inv.RequiredTests,
		}
		if inv.Protects != nil {
			ri.Protects = &rawProtects{
				State:    inv.Protects.State,
				Files:    inv.Protects.Files,
				Symbols:  inv.Protects.Symbols,
				Services: inv.Protects.Services,
			}
		}
		f.Invariants = append(f.Invariants, ri)
		existing[inv.ID] = true
		added = append(added, inv.ID)
	}

	if len(added) == 0 {
		return nil, nil
	}
	return added, saveYAMLFile(path, f)
}

// failureModesFile mirrors the top-level structure of failure_modes.yaml.
type failureModesFile struct {
	FailureModes []rawFailureMode `yaml:"failure_modes"`
}

type rawFailureMode struct {
	ID                string   `yaml:"id"`
	Title             string   `yaml:"title"`
	Severity          string   `yaml:"severity,omitempty"`
	Symptoms          []string `yaml:"symptoms"`
	RootCause         string   `yaml:"root_cause"`
	ArchitectureFix   string   `yaml:"architecture_fix"`
	ForbiddenFixes    []string `yaml:"forbidden_fixes,omitempty"`
	RelatedInvariants []string `yaml:"related_invariants,omitempty"`
	RelatedServices   []string `yaml:"related_services,omitempty"`
	RequiredTests     []string `yaml:"required_tests,omitempty"`
}

func mergeFailureModes(path string, proposed []ProposedFailureMode) ([]string, error) {
	var f failureModesFile
	if err := loadYAMLFile(path, &f); err != nil {
		return nil, err
	}

	existing := make(map[string]bool)
	for _, fm := range f.FailureModes {
		existing[fm.ID] = true
	}

	var added []string
	for _, fm := range proposed {
		if existing[fm.ID] {
			continue
		}
		f.FailureModes = append(f.FailureModes, rawFailureMode{
			ID:                fm.ID,
			Title:             fm.Title,
			Severity:          fm.Severity,
			Symptoms:          fm.Symptoms,
			RootCause:         fm.RootCause,
			ArchitectureFix:   fm.ArchitectureFix,
			ForbiddenFixes:    fm.ForbiddenFixes,
			RelatedInvariants: fm.RelatedInvariants,
			RelatedServices:   fm.RelatedServices,
			RequiredTests:     fm.RequiredTests,
		})
		existing[fm.ID] = true
		added = append(added, fm.ID)
	}

	if len(added) == 0 {
		return nil, nil
	}
	return added, saveYAMLFile(path, f)
}

// forbiddenFixesFile mirrors the top-level structure of forbidden_fixes.yaml.
type forbiddenFixesFile struct {
	ForbiddenFixes []rawForbiddenFix `yaml:"forbidden_fixes"`
}

type rawForbiddenFix struct {
	ID                string   `yaml:"id"`
	Summary           string   `yaml:"summary,omitempty"`
	RelatedInvariants []string `yaml:"related_invariants,omitempty"`
}

func mergeForbiddenFixes(path string, proposed []ProposedForbiddenFix) ([]string, error) {
	var f forbiddenFixesFile
	if err := loadYAMLFile(path, &f); err != nil {
		return nil, err
	}

	existing := make(map[string]bool)
	for _, ff := range f.ForbiddenFixes {
		existing[ff.ID] = true
	}

	var added []string
	for _, ff := range proposed {
		if existing[ff.ID] {
			continue
		}
		summary := ff.Summary
		if summary == "" {
			summary = ff.Title
		}
		f.ForbiddenFixes = append(f.ForbiddenFixes, rawForbiddenFix{
			ID:                ff.ID,
			Summary:           summary,
			RelatedInvariants: ff.RelatedInvariants,
		})
		existing[ff.ID] = true
		added = append(added, ff.ID)
	}

	if len(added) == 0 {
		return nil, nil
	}
	return added, saveYAMLFile(path, f)
}

// contextAliasesFile mirrors the top-level structure of context_aliases.yaml.
type contextAliasesFile struct {
	Aliases map[string][]string `yaml:"aliases"`
}

func mergeContextAliases(path string, proposed map[string][]string) (int, error) {
	var f contextAliasesFile
	if err := loadYAMLFile(path, &f); err != nil {
		return 0, err
	}
	if f.Aliases == nil {
		f.Aliases = make(map[string][]string)
	}

	n := 0
	for targetID, aliases := range proposed {
		existing := make(map[string]bool)
		for _, a := range f.Aliases[targetID] {
			existing[strings.ToLower(a)] = true
		}
		for _, alias := range aliases {
			if !existing[strings.ToLower(alias)] {
				f.Aliases[targetID] = append(f.Aliases[targetID], alias)
				existing[strings.ToLower(alias)] = true
				n++
			}
		}
	}

	if n == 0 {
		return 0, nil
	}
	return n, saveYAMLFile(path, f)
}

// ---- file I/O helpers ----

func loadYAMLFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// File doesn't exist yet — start with an empty structure.
		return nil
	}
	if err != nil {
		return fmt.Errorf("load %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func saveYAMLFile(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
