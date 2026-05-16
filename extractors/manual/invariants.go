package manual

import (
	"context"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
)

// invariantFile is the top-level structure of invariants.yaml and convergence_rules.yaml.
type invariantFile struct {
	Invariants []yamlInvariant `yaml:"invariants"`
}

// yamlImplementedBy is one entry in implemented_by[].
type yamlImplementedBy struct {
	File          string   `yaml:"file"`
	Functions     []string `yaml:"functions"`      // optional — Go function names (list)
	Function      string   `yaml:"function"`       // optional — single Go function name (alias)
	Trust         string   `yaml:"trust"`          // trust level: strict_verified/verified/declared/inferred
	ReadsAuthority []string `yaml:"reads_authority"` // etcd keys or config paths this fn reads
	WritesState   []string `yaml:"writes_state"`   // etcd keys or state artifacts this fn writes
	GuardsAction  []string `yaml:"guards_action"`  // actions/RPCs this fn gates via txn/guard
}

// yamlInvAuthority is one entry in authority[] of an invariant.
// Supports two styles:
//
//	source: /key  (generic — kind must be specified separately)
//	file: path    (shorthand for kind=config_file)
//	etcd: /key    (shorthand for kind=etcd_key)
type yamlInvAuthority struct {
	Source     string `yaml:"source"`     // etcd key path, config file, or proto field
	File       string `yaml:"file"`       // shorthand: config file path (kind=config_file)
	Etcd       string `yaml:"etcd"`       // shorthand: etcd key path (kind=etcd_key)
	Kind       string `yaml:"kind"`       // etcd_key / config_file / proto_field / runtime_state
	Confidence string `yaml:"confidence"` // high / medium / low
}

type yamlInvariant struct {
	ID       string `yaml:"id"`
	Title    string `yaml:"title"`
	Severity string `yaml:"severity"`
	Status   string `yaml:"status"`
	Summary  string `yaml:"summary"`
	Protects struct {
		State           []string `yaml:"state"`
		Files           []string `yaml:"files"`           // → implements + partially_implements (backward compat)
		EnforcesFiles   []string `yaml:"enforces_files"`  // → enforces (validation/blocking)
		ConfiguresFiles []string `yaml:"configures_files"` // → configures (data/config/YAML)
		ObservesFiles   []string `yaml:"observes_files"`  // → observes (detection/reporting)
		MayAffectFiles  []string `yaml:"may_affect_files"` // → may_affect (weak indirect)
		Symbols         []string `yaml:"symbols"`
		SystemdUnits    []string `yaml:"systemd_units"`
	} `yaml:"protects"`
	ForbiddenFixes      []string            `yaml:"forbidden_fixes"`
	RequiredTests       []string            `yaml:"required_tests"`
	RelatedFailureModes []string            `yaml:"related_failure_modes"`
	// Invariant implementation graph fields (new schema).
	ImplementedBy    []yamlImplementedBy `yaml:"implemented_by"`
	Authority        []yamlInvAuthority  `yaml:"authority"`
	VerifiedBy       []string            `yaml:"verified_by"`       // test function names with direct proof
	ViolatedBy       []string            `yaml:"violated_by"`       // failure mode IDs that violate this
	DecisionGuidance []string            `yaml:"decision_guidance"` // agent-readable guidance sentences
}

// LoadInvariants loads invariants.yaml or convergence_rules.yaml into g.
// Missing files are silently skipped.
func LoadInvariants(ctx context.Context, g *graph.Graph, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("LoadInvariants: read %s: %w", path, err)
	}

	var f invariantFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("LoadInvariants: parse %s: %w", path, err)
	}

	for _, inv := range f.Invariants {
		if err := loadInvariant(ctx, g, inv, path); err != nil {
			return fmt.Errorf("LoadInvariants %s: %w", inv.ID, err)
		}
	}
	return nil
}

func loadInvariant(ctx context.Context, g *graph.Graph, inv yamlInvariant, path string) error {
	nodeID := "invariant:" + inv.ID

	if err := g.AddNode(ctx, graph.Node{
		ID:      nodeID,
		Type:    graph.NodeTypeInvariant,
		Name:    inv.ID,
		Summary: inv.Summary,
	}); err != nil {
		return err
	}

	if err := g.UpsertInvariant(ctx, graph.Invariant{
		ID:       inv.ID,
		Title:    inv.Title,
		Summary:  inv.Summary,
		Severity: inv.Severity,
		Status:   inv.Status,
	}); err != nil {
		return err
	}

	// Protected etcd keys → etcd_key nodes + protects edges.
	for _, state := range inv.Protects.State {
		stateID := "etcd_key:" + state
		if err := g.AddNode(ctx, graph.Node{
			ID:   stateID,
			Type: graph.NodeTypeEtcdKey,
			Name: state,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeProtects, Dst: stateID}); err != nil {
			return err
		}
	}

	// Protected files → source_file nodes + protects edges.
	// Also create the reverse implements edge so impact path traversal (which
	// follows outgoing edges from the changed file) can reach this invariant.
	for _, file := range inv.Protects.Files {
		fileID := "source_file:" + file
		if err := g.AddNode(ctx, graph.Node{
			ID:   fileID,
			Type: graph.NodeTypeSourceFile,
			Name: file,
			Path: file,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeProtects, Dst: fileID}); err != nil {
			return err
		}
		// Reverse edge: file → implements → invariant (enables BFS from changed file).
		if err := g.AddEdge(ctx, graph.Edge{Src: fileID, Kind: graph.EdgeImplements, Dst: nodeID}); err != nil {
			return err
		}
	}

	// enforces_files → source_file nodes + protects + reverse enforces edges.
	for _, file := range inv.Protects.EnforcesFiles {
		fileID := "source_file:" + file
		if err := g.AddNode(ctx, graph.Node{
			ID:   fileID,
			Type: graph.NodeTypeSourceFile,
			Name: file,
			Path: file,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeProtects, Dst: fileID}); err != nil {
			return err
		}
		// Reverse: file → enforces → invariant (more precise than implements).
		if err := g.AddEdge(ctx, graph.Edge{Src: fileID, Kind: graph.EdgeEnforces, Dst: nodeID}); err != nil {
			return err
		}
	}

	// configures_files → source_file nodes + protects + reverse configures edges.
	for _, file := range inv.Protects.ConfiguresFiles {
		fileID := "source_file:" + file
		if err := g.AddNode(ctx, graph.Node{
			ID:   fileID,
			Type: graph.NodeTypeSourceFile,
			Name: file,
			Path: file,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeProtects, Dst: fileID}); err != nil {
			return err
		}
		// Reverse: file → configures → invariant.
		if err := g.AddEdge(ctx, graph.Edge{Src: fileID, Kind: graph.EdgeConfigures, Dst: nodeID}); err != nil {
			return err
		}
	}

	// observes_files → source_file nodes + protects + reverse observes edges.
	for _, file := range inv.Protects.ObservesFiles {
		fileID := "source_file:" + file
		if err := g.AddNode(ctx, graph.Node{
			ID:   fileID,
			Type: graph.NodeTypeSourceFile,
			Name: file,
			Path: file,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeProtects, Dst: fileID}); err != nil {
			return err
		}
		// Reverse: file → observes → invariant (detection/reporting).
		if err := g.AddEdge(ctx, graph.Edge{Src: fileID, Kind: graph.EdgeObserves, Dst: nodeID}); err != nil {
			return err
		}
	}

	// may_affect_files → source_file nodes + protects + reverse may_affect edges.
	for _, file := range inv.Protects.MayAffectFiles {
		fileID := "source_file:" + file
		if err := g.AddNode(ctx, graph.Node{
			ID:   fileID,
			Type: graph.NodeTypeSourceFile,
			Name: file,
			Path: file,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeProtects, Dst: fileID}); err != nil {
			return err
		}
		// Reverse: file → may_affect → invariant (weak indirect link).
		if err := g.AddEdge(ctx, graph.Edge{Src: fileID, Kind: graph.EdgeMayAffect, Dst: nodeID}); err != nil {
			return err
		}
	}

	// Protected symbols → symbol nodes + protects edges.
	for _, sym := range inv.Protects.Symbols {
		symID := "symbol:" + sym
		if err := g.AddNode(ctx, graph.Node{
			ID:   symID,
			Type: graph.NodeTypeSymbol,
			Name: sym,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeProtects, Dst: symID}); err != nil {
			return err
		}
	}

	// Protected systemd units → systemd_unit nodes + protects edges.
	for _, unit := range inv.Protects.SystemdUnits {
		unitID := "systemd_unit:" + unit
		if err := g.AddNode(ctx, graph.Node{
			ID:   unitID,
			Type: graph.NodeTypeSystemdUnit,
			Name: unit,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeProtects, Dst: unitID}); err != nil {
			return err
		}
	}

	// Forbidden fixes → forbidden_fix nodes + forbids edges + reverse blocks_forbidden_action.
	for _, fix := range inv.ForbiddenFixes {
		fixID := "forbidden_fix:" + fix
		if err := g.AddNode(ctx, graph.Node{
			ID:   fixID,
			Type: graph.NodeTypeForbiddenFix,
			Name: fix,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeForbids, Dst: fixID}); err != nil {
			return err
		}
		// Reverse: forbidden_fix → blocks_forbidden_action → invariant.
		if err := g.AddEdge(ctx, graph.Edge{
			Src:      fixID,
			Kind:     graph.EdgeBlocksForbiddenAction,
			Dst:      nodeID,
			Metadata: map[string]any{"source_file": path, "trust_level": "declared"},
		}); err != nil {
			return err
		}
	}

	// Required tests → test nodes + tested_by edges + reverse verifies.
	for _, test := range inv.RequiredTests {
		testID := "test:" + test
		if err := g.AddNode(ctx, graph.Node{
			ID:   testID,
			Type: graph.NodeTypeTest,
			Name: test,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeTestedBy, Dst: testID}); err != nil {
			return err
		}
		// Reverse: test → verifies → invariant (test proves invariant holds).
		if err := g.AddEdge(ctx, graph.Edge{
			Src:      testID,
			Kind:     graph.EdgeVerifies,
			Dst:      nodeID,
			Metadata: map[string]any{"source_file": path, "trust_level": "declared"},
		}); err != nil {
			return err
		}
	}

	// Related failure modes → best-effort edges (failure mode nodes may not exist yet).
	for _, fmID := range inv.RelatedFailureModes {
		fmNodeID := "failure_mode:" + fmID
		// EnsureNode preserves the canonical failure_modes loader's metadata.
		if err := g.EnsureNode(ctx, graph.Node{
			ID:   fmNodeID,
			Type: graph.NodeTypeFailureMode,
			Name: fmID,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeAffects, Dst: fmNodeID}); err != nil {
			return err
		}
		// Reverse: failure_mode → violates → invariant.
		if err := g.AddEdge(ctx, graph.Edge{
			Src:      fmNodeID,
			Kind:     graph.EdgeViolates,
			Dst:      nodeID,
			Metadata: map[string]any{"source_file": path, "trust_level": "declared"},
		}); err != nil {
			return err
		}
	}

	// ── New invariant implementation graph edges ─────────────────────────────

	// protects.files also emit partially_implements (backward-compat, trust=declared).
	for _, file := range inv.Protects.Files {
		fileID := "source_file:" + file
		if err := g.AddEdge(ctx, graph.Edge{
			Src:      fileID,
			Kind:     graph.EdgePartiallyImplements,
			Dst:      nodeID,
			Metadata: map[string]any{"source_file": path, "trust_level": "declared", "weight_reason": "protects.files backward compat"},
		}); err != nil {
			return err
		}
	}

	// implemented_by[] → full implementation evidence with reads_authority/writes_state/guards_action.
	for _, impl := range inv.ImplementedBy {
		fileID := "source_file:" + impl.File
		if err := g.AddNode(ctx, graph.Node{
			ID:   fileID,
			Type: graph.NodeTypeSourceFile,
			Name: impl.File,
			Path: impl.File,
		}); err != nil {
			return err
		}
		trust := impl.Trust
		if trust == "" {
			trust = "declared"
		}
		implMeta := map[string]any{
			"source_file": path,
			"trust_level": trust,
		}
		// Collect all function names (Functions list takes precedence over Function scalar).
		fns := impl.Functions
		if len(fns) == 0 && impl.Function != "" {
			fns = []string{impl.Function}
		}
		if len(fns) > 0 {
			implMeta["functions"] = fns
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:      fileID,
			Kind:     graph.EdgeImplements,
			Dst:      nodeID,
			Metadata: implMeta,
		}); err != nil {
			return err
		}
		// reads_authority sub-edges.
		for _, auth := range impl.ReadsAuthority {
			authID := "authority:" + auth
			if err := g.AddNode(ctx, graph.Node{ID: authID, Type: "authority_source", Name: auth}); err != nil {
				return err
			}
			if err := g.AddEdge(ctx, graph.Edge{
				Src:      fileID,
				Kind:     graph.EdgeReadsAuthority,
				Dst:      authID,
				Metadata: map[string]any{"source_file": path, "trust_level": trust},
			}); err != nil {
				return err
			}
		}
		// writes_state sub-edges.
		for _, ws := range impl.WritesState {
			wsID := "state:" + ws
			if err := g.AddNode(ctx, graph.Node{ID: wsID, Type: "state_artifact", Name: ws}); err != nil {
				return err
			}
			if err := g.AddEdge(ctx, graph.Edge{
				Src:      fileID,
				Kind:     graph.EdgeWritesState,
				Dst:      wsID,
				Metadata: map[string]any{"source_file": path, "trust_level": trust},
			}); err != nil {
				return err
			}
		}
		// guards_action sub-edges.
		for _, ga := range impl.GuardsAction {
			gaID := "action:" + ga
			if err := g.AddNode(ctx, graph.Node{ID: gaID, Type: "guarded_action", Name: ga}); err != nil {
				return err
			}
			if err := g.AddEdge(ctx, graph.Edge{
				Src:      fileID,
				Kind:     graph.EdgeGuardsAction,
				Dst:      gaID,
				Metadata: map[string]any{"source_file": path, "trust_level": trust},
			}); err != nil {
				return err
			}
		}
	}

	// authority[] → authority_source nodes + reads_authority edges from the invariant node.
	for _, auth := range inv.Authority {
		// Resolve source and kind from shorthand fields (file: / etcd:) or generic (source: + kind:).
		src := auth.Source
		kind := auth.Kind
		if auth.File != "" {
			src = auth.File
			if kind == "" {
				kind = "config_file"
			}
		} else if auth.Etcd != "" {
			src = auth.Etcd
			if kind == "" {
				kind = "etcd_key"
			}
		}
		if src == "" {
			continue // skip malformed entry
		}
		if kind == "" {
			kind = "etcd_key"
		}
		authID := "authority:" + src
		conf := auth.Confidence
		if conf == "" {
			conf = "medium"
		}
		if err := g.AddNode(ctx, graph.Node{
			ID:      authID,
			Type:    "authority_source",
			Name:    auth.Source,
			Metadata: map[string]any{"kind": kind, "confidence": conf},
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:      nodeID,
			Kind:     graph.EdgeReadsAuthority,
			Dst:      authID,
			Metadata: map[string]any{"source_file": path, "kind": kind, "confidence": conf},
		}); err != nil {
			return err
		}
	}

	// verified_by[] → test → verifies → invariant (higher trust than required_tests).
	for _, test := range inv.VerifiedBy {
		testID := "test:" + test
		if err := g.AddNode(ctx, graph.Node{
			ID:   testID,
			Type: graph.NodeTypeTest,
			Name: test,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:      testID,
			Kind:     graph.EdgeVerifies,
			Dst:      nodeID,
			Metadata: map[string]any{"source_file": path, "trust_level": "verified"},
		}); err != nil {
			return err
		}
	}

	// violated_by[] → failure_mode → violates → invariant (declarative override).
	for _, fmID := range inv.ViolatedBy {
		fmNodeID := "failure_mode:" + fmID
		if err := g.EnsureNode(ctx, graph.Node{
			ID:   fmNodeID,
			Type: graph.NodeTypeFailureMode,
			Name: fmID,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:      fmNodeID,
			Kind:     graph.EdgeViolates,
			Dst:      nodeID,
			Metadata: map[string]any{"source_file": path, "trust_level": "declared"},
		}); err != nil {
			return err
		}
	}

	// decision_guidance[] → stored in invariant node metadata (no new graph edge needed).
	if len(inv.DecisionGuidance) > 0 {
		if err := g.AddNode(ctx, graph.Node{
			ID:       nodeID,
			Type:     graph.NodeTypeInvariant,
			Name:     inv.ID,
			Summary:  inv.Summary,
			Metadata: map[string]any{"decision_guidance": inv.DecisionGuidance},
		}); err != nil {
			return err
		}
	}

	return nil
}
