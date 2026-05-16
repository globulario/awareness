package learning

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ProposalStatus values — mirrors graph.ProposalStatus*.
const (
	StatusDraft       = "DRAFT"
	StatusValidated   = "VALIDATED"
	StatusNeedsReview = "NEEDS_REVIEW"
	StatusApproved    = "APPROVED"
	StatusRejected    = "REJECTED"
	StatusPromoted    = "PROMOTED"
	StatusSuperseded  = "SUPERSEDED"
)

// ProposalSpec is the top-level structure of a proposal YAML file.
type ProposalSpec struct {
	Proposal            ProposalHeader                       `yaml:"proposal"`
	FailureModes        []ProposedFailureMode                `yaml:"failure_modes,omitempty"`
	Invariants          []ProposedInvariant                  `yaml:"invariants,omitempty"`
	ForbiddenFixes      []ProposedForbiddenFix               `yaml:"forbidden_fixes,omitempty"`
	ScanRules           []ProposedScanRule                   `yaml:"scan_rules,omitempty"`
	MetricThresholds    map[string]map[string]ThresholdEntry `yaml:"metric_thresholds,omitempty"`
	RequiredTests       []string                             `yaml:"required_tests,omitempty"`
	ContextAliases      map[string][]string                  `yaml:"context_aliases,omitempty"`
	ServiceDependencies []ProposedDependency                 `yaml:"service_dependencies,omitempty"`
	ManualRepairs       []string                             `yaml:"manual_repairs,omitempty"`
	Evidence            ProposalEvidence                     `yaml:"evidence"`
	LearnSource         string                               `yaml:"learn_source,omitempty"` // "learn_from_fix" or "incident"
}

// ProposedScanRule is a static analysis rule proposed by the learning system.
type ProposedScanRule struct {
	ID              string   `yaml:"id"`
	Description     string   `yaml:"description"`
	Language        string   `yaml:"language"`
	Severity        string   `yaml:"severity"`
	Patterns        []string `yaml:"patterns,omitempty"`
	KnowledgeID     string   `yaml:"knowledge_id,omitempty"`
	SafeAlternative string   `yaml:"safe_alternative,omitempty"`
	Allowlist       []string `yaml:"allowlist,omitempty"`
}

// ThresholdEntry is a metric alert threshold.
type ThresholdEntry struct {
	Warn     float64 `yaml:"warn"`
	Critical float64 `yaml:"critical"`
	Reason   string  `yaml:"reason,omitempty"`
}

// ProposalHeader identifies the proposal and links it to the source incident.
type ProposalHeader struct {
	ID             string `yaml:"id"`
	SourceIncident string `yaml:"source_incident"`
	Status         string `yaml:"status"`
	CreatedAt      string `yaml:"created_at,omitempty"`
	ApprovedBy     string `yaml:"approved_by,omitempty"`
	ApprovedAt     string `yaml:"approved_at,omitempty"`
}

// ProposedFailureMode is a failure mode proposed by the awareness learning system.
type ProposedFailureMode struct {
	ID                string            `yaml:"id"`
	Title             string            `yaml:"title"`
	Severity          string            `yaml:"severity"`
	Symptoms          []string          `yaml:"symptoms"`
	RootCause         string            `yaml:"root_cause"`
	ArchitectureFix   string            `yaml:"architecture_fix"`
	RelatedInvariants []string          `yaml:"related_invariants,omitempty"`
	RelatedServices   []string          `yaml:"related_services,omitempty"`
	ForbiddenFixes    []string          `yaml:"forbidden_fixes,omitempty"`
	RequiredTests     []string          `yaml:"required_tests,omitempty"`
	Protects          *ProposedProtects `yaml:"protects,omitempty"`
}

// ProposedInvariant is an invariant proposed by the awareness learning system.
type ProposedInvariant struct {
	ID             string            `yaml:"id"`
	Title          string            `yaml:"title"`
	Severity       string            `yaml:"severity"`
	Summary        string            `yaml:"summary"`
	Protects       *ProposedProtects `yaml:"protects,omitempty"`
	ForbiddenFixes []string          `yaml:"forbidden_fixes,omitempty"`
	RequiredTests  []string          `yaml:"required_tests,omitempty"`
}

// ProposedProtects declares what state or code an invariant protects.
type ProposedProtects struct {
	State    []string `yaml:"state,omitempty"`
	Files    []string `yaml:"files,omitempty"`
	Symbols  []string `yaml:"symbols,omitempty"`
	Services []string `yaml:"services,omitempty"`
}

// ProposedForbiddenFix is a forbidden fix proposed by the awareness learning system.
type ProposedForbiddenFix struct {
	ID                string   `yaml:"id"`
	Title             string   `yaml:"title,omitempty"`
	Summary           string   `yaml:"summary,omitempty"`
	RelatedInvariants []string `yaml:"related_invariants,omitempty"`
}

// ProposedDependency is a service dependency edge proposed by the learning system.
type ProposedDependency struct {
	Service  string `yaml:"service"`
	Phase    string `yaml:"phase"`
	Required bool   `yaml:"required"`
	Reason   string `yaml:"reason,omitempty"`
}

// ProposalEvidence links a proposal to its source incident evidence.
type ProposalEvidence struct {
	SourceIncident string                 `yaml:"source_incident"`
	Symptoms       []string               `yaml:"symptoms,omitempty"`
	StateDeltas    []string               `yaml:"state_deltas,omitempty"`
	ManualRepairs  []string               `yaml:"manual_repairs,omitempty"`
	FalsePositive  *FalsePositiveFeedback `yaml:"false_positive,omitempty"`
}

// FalsePositiveFeedback captures structured feedback when a finding is judged
// to be a false positive and should influence learning proposals.
type FalsePositiveFeedback struct {
	FindingID        string `yaml:"finding_id"`
	WhyFalsePositive string `yaml:"why_false_positive"`
	EvidenceLink     string `yaml:"evidence_link"`
	Owner            string `yaml:"owner"`
	Reviewer         string `yaml:"reviewer"`
}

// LoadProposalFromFile reads a proposal YAML from path.
func LoadProposalFromFile(path string) (*ProposalSpec, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("proposal file not found: %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("read proposal %s: %w", path, err)
	}
	var p ProposalSpec
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse proposal %s: %w", path, err)
	}
	return &p, nil
}

// SaveProposal serialises a proposal to YAML at path.
func SaveProposal(path string, p *ProposalSpec) error {
	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal proposal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write proposal %s: %w", path, err)
	}
	return nil
}

// GenerateProposalFromBundle creates a draft ProposalSpec from an incident bundle.
//
// If the bundle has a Proposed section (pre-reasoned content from AI or a test fixture),
// that content is used directly. Otherwise a skeleton proposal is generated from
// the raw bundle evidence.
//
// The returned proposal always has status DRAFT. It must be validated and reviewed
// before promotion.
func GenerateProposalFromBundle(b *IncidentBundle) *ProposalSpec {
	proposalID := "proposal." + sanitiseID(b.IncidentID)

	p := &ProposalSpec{
		Proposal: ProposalHeader{
			ID:             proposalID,
			SourceIncident: b.IncidentID,
			Status:         StatusDraft,
			CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		},
		Evidence: ProposalEvidence{
			SourceIncident: b.IncidentID,
			Symptoms:       b.Symptoms,
			StateDeltas:    b.StateDeltas,
			ManualRepairs:  b.ManualRepairs,
		},
		ManualRepairs: b.ManualRepairs,
	}

	if b.Proposed != nil {
		// Use pre-reasoned content from the bundle (AI or fixture).
		p.FailureModes = b.Proposed.FailureModes
		p.Invariants = b.Proposed.Invariants
		p.ForbiddenFixes = b.Proposed.ForbiddenFixes
		p.RequiredTests = b.Proposed.RequiredTests
		p.ContextAliases = b.Proposed.ContextAliases
		return p
	}

	// Skeleton generation: derive a single failure mode from raw evidence.
	fm := ProposedFailureMode{
		ID:              "failure_mode." + sanitiseID(b.IncidentID),
		Title:           b.Title,
		Severity:        b.Severity,
		Symptoms:        b.Symptoms,
		RootCause:       b.SuspectedRootCause,
		ArchitectureFix: "",
		RelatedServices: b.ObservedServices,
	}
	p.FailureModes = []ProposedFailureMode{fm}

	return p
}

// sanitiseID converts a string to a valid proposal ID component by replacing
// non-alphanumeric runs with underscores and lowercasing.
func sanitiseID(s string) string {
	s = strings.ToLower(s)
	var buf strings.Builder
	lastWasUnderscore := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' {
			buf.WriteRune(r)
			lastWasUnderscore = false
		} else {
			if !lastWasUnderscore {
				buf.WriteRune('_')
				lastWasUnderscore = true
			}
		}
	}
	return strings.Trim(buf.String(), "_")
}

// AllProposedServiceIDs returns the union of services referenced by all
// failure modes and invariants in the proposal.
func (p *ProposalSpec) AllProposedServiceIDs() []string {
	seen := make(map[string]bool)
	var out []string
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, fm := range p.FailureModes {
		for _, svc := range fm.RelatedServices {
			add(svc)
		}
		if fm.Protects != nil {
			for _, svc := range fm.Protects.Services {
				add(svc)
			}
		}
	}
	for _, inv := range p.Invariants {
		if inv.Protects != nil {
			for _, svc := range inv.Protects.Services {
				add(svc)
			}
		}
	}
	for _, dep := range p.ServiceDependencies {
		add(dep.Service)
	}
	return out
}

// AllProposedInvariantIDs returns the union of invariant IDs declared in the proposal.
func (p *ProposalSpec) AllProposedInvariantIDs() []string {
	seen := make(map[string]bool)
	var out []string
	for _, inv := range p.Invariants {
		if !seen[inv.ID] {
			seen[inv.ID] = true
			out = append(out, inv.ID)
		}
	}
	return out
}

// ApproveProposal sets a proposal's status to APPROVED in-place.
// This is the programmatic equivalent of the approve-proposal CLI command.
func ApproveProposal(p *ProposalSpec) {
	p.Proposal.Status = StatusApproved
	if strings.TrimSpace(p.Proposal.ApprovedBy) == "" {
		p.Proposal.ApprovedBy = "human-review"
	}
	if strings.TrimSpace(p.Proposal.ApprovedAt) == "" {
		p.Proposal.ApprovedAt = time.Now().UTC().Format(time.RFC3339)
	}
}

// AllReferencedInvariantIDs returns all invariant IDs referenced by failure modes.
func (p *ProposalSpec) AllReferencedInvariantIDs() []string {
	seen := make(map[string]bool)
	var out []string
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, fm := range p.FailureModes {
		for _, id := range fm.RelatedInvariants {
			add(id)
		}
	}
	for _, inv := range p.Invariants {
		add(inv.ID)
	}
	return out
}
