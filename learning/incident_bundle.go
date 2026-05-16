package learning

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
)

// IncidentBundle is structured evidence collected from a runtime incident.
// V1 loads bundles from YAML fixture files; future versions will collect
// evidence from live cluster APIs.
type IncidentBundle struct {
	IncidentID         string    `yaml:"incident_id"`
	Title              string    `yaml:"title"`
	Status             string    `yaml:"status"`
	Severity           string    `yaml:"severity"`
	TimeRange          TimeRange `yaml:"time_range"`
	Symptoms           []string  `yaml:"symptoms"`
	ObservedServices   []string  `yaml:"observed_services"`
	StateDeltas        []string  `yaml:"state_deltas"`
	WorkflowReceipts   []string  `yaml:"workflow_receipts"`
	DoctorFindings     []string  `yaml:"doctor_findings"`
	RuntimeEvents      []string  `yaml:"runtime_events"`
	ManualRepairs      []string  `yaml:"manual_repairs"`
	SuspectedRootCause string    `yaml:"suspected_root_cause"`
	RelatedFiles       []string  `yaml:"related_files"`
	RelatedSymbols     []string  `yaml:"related_symbols"`
	Evidence           []string  `yaml:"evidence"`

	// Proposed is optional pre-reasoned content (populated by AI or filled in
	// fixture YAML for V1 testing). GenerateProposalFromBundle uses this when present.
	Proposed *BundleProposedContent `yaml:"proposed,omitempty"`
}

// TimeRange holds incident start/end in RFC3339 format.
type TimeRange struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// BundleProposedContent carries pre-reasoned proposal data embedded in the bundle.
// This allows test fixtures to encode the expected proposal without AI inference.
type BundleProposedContent struct {
	FailureModes   []ProposedFailureMode  `yaml:"failure_modes,omitempty"`
	Invariants     []ProposedInvariant    `yaml:"invariants,omitempty"`
	ForbiddenFixes []ProposedForbiddenFix `yaml:"forbidden_fixes,omitempty"`
	RequiredTests  []string               `yaml:"required_tests,omitempty"`
	ContextAliases map[string][]string    `yaml:"context_aliases,omitempty"`
}

// LoadIncidentBundle reads an incident bundle YAML from path.
func LoadIncidentBundle(path string) (*IncidentBundle, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("incident bundle not found: %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("read incident bundle %s: %w", path, err)
	}
	var b IncidentBundle
	if err := yaml.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parse incident bundle %s: %w", path, err)
	}
	if b.IncidentID == "" {
		return nil, fmt.Errorf("incident bundle %s: incident_id is required", path)
	}
	return &b, nil
}

// SaveIncidentBundle serialises a bundle to YAML at path.
func SaveIncidentBundle(path string, b *IncidentBundle) error {
	data, err := yaml.Marshal(b)
	if err != nil {
		return fmt.Errorf("marshal incident bundle: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write incident bundle %s: %w", path, err)
	}
	return nil
}

// RecordIncidentInGraph stores the incident bundle as an incident node and
// evidence nodes in g. Call after loading the bundle; safe to call repeatedly.
func RecordIncidentInGraph(ctx context.Context, g *graph.Graph, b *IncidentBundle) error {
	now := time.Now().Unix()

	// Upsert the incident record.
	inc := graph.IncidentRecord{
		ID:        b.IncidentID,
		Title:     b.Title,
		Severity:  b.Severity,
		Status:    b.Status,
		Summary:   b.SuspectedRootCause,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := g.UpsertIncident(ctx, inc); err != nil {
		return err
	}

	incNodeID := "incident:" + b.IncidentID

	// Incident node in the graph.
	if err := g.AddNode(ctx, graph.Node{
		ID:      incNodeID,
		Type:    graph.NodeTypeIncident,
		Name:    b.IncidentID,
		Summary: b.Title,
	}); err != nil {
		return err
	}

	// Observed service edges.
	for _, svc := range b.ObservedServices {
		svcID := "service:" + svc
		_ = g.AddNode(ctx, graph.Node{ID: svcID, Type: graph.NodeTypeGlobularService, Name: svc})
		_ = g.AddEdge(ctx, graph.Edge{Src: incNodeID, Kind: graph.EdgeObservedDuring, Dst: svcID})
	}

	// Manual repair nodes.
	for i, repair := range b.ManualRepairs {
		repairID := fmt.Sprintf("manual_repair:%s.%d", b.IncidentID, i)
		_ = g.AddNode(ctx, graph.Node{
			ID:      repairID,
			Type:    graph.NodeTypeManualRepair,
			Name:    repairID,
			Summary: repair,
		})
		_ = g.AddEdge(ctx, graph.Edge{Src: incNodeID, Kind: graph.EdgeSupportedBy, Dst: repairID})
	}

	return nil
}
