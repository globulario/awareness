// Package metrics indexes metric_queries.yaml and metric_thresholds.yaml from
// the awareness knowledge directory, then links metric warning rules to failure
// modes and invariants already in the graph.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
)

// ─── YAML structures ────────────────────────────────────────────────────────

type metricQueryFile struct {
	Queries []yamlMetricQuery `yaml:"queries"`
}

type yamlMetricQuery struct {
	ID      string `yaml:"id"`
	Query   string `yaml:"query"`
	Service string `yaml:"service"`
	Unit    string `yaml:"unit"`
}

// metricThresholdFile mirrors the nested map structure of metric_thresholds.yaml.
// Top-level keys are service names (e.g. "etcd", "default").
// Each value is a map from metric name to warn/critical levels.
type metricThresholdFile struct {
	Thresholds map[string]map[string]yamlThresholdLevel `yaml:"thresholds"`
}

type yamlThresholdLevel struct {
	Warn     float64 `yaml:"warn"`
	Critical float64 `yaml:"critical"`
}

// ─── Warning rule mapping ────────────────────────────────────────────────────

type metricWarningMapping struct {
	// QueryID matches the id field in metric_queries.yaml.
	QueryID string
	// ThresholdKey is "<service>:<metric>" matching the thresholds block.
	ThresholdKey string
	// FailureModes is a list of failure mode IDs (without "failure_mode:" prefix).
	FailureModes []string
	// Invariants is a list of invariant IDs (without "invariant:" prefix).
	Invariants []string
	// DecisionRules is a list of decision rule IDs (without "decision_rule:" prefix)
	// that this metric warning should trigger as evidence.
	DecisionRules []string
	// Explanation is a human-readable sentence for the warning rule.
	Explanation string
}

// metricWarningMappings encodes the known relationships between metric queries,
// thresholds, failure modes, and invariants. Add new entries here to extend
// coverage — no YAML file changes required.
var metricWarningMappings = []metricWarningMapping{
	{
		QueryID:       "etcd_fsync_latency_ms",
		ThresholdKey:  "etcd:fsync_latency_ms",
		FailureModes:  []string{"etcd.leader_instability", "service.endpoint.etcd_address_reachability"},
		Invariants:    []string{"service.endpoint.etcd_address_reachability"},
		DecisionRules: []string{"leader_only_reconcilers_must_gate_on_leadership"},
		Explanation:   "High etcd fsync latency risks leader election instability and workflow dispatch timeouts",
	},
	{
		QueryID:       "etcd_disk_percent",
		ThresholdKey:  "etcd:disk_percent",
		FailureModes:  []string{"etcd.nospace_alarm", "control_plane.convergence_blocked"},
		Invariants:    []string{"service.endpoint.etcd_address_reachability"},
		DecisionRules: []string{"service_notify_ready_before_etcd_write"},
		Explanation:   "etcd disk full triggers NOSPACE alarm, blocks all writes, halts convergence",
	},
	{
		QueryID:      "workflow_failed_runs_15m",
		ThresholdKey: "workflow:failed_runs_15m",
		FailureModes: []string{"workflow.convergence_blocked"},
		Invariants:   []string{"reconcile.global_work_must_not_starve_completion"},
		Explanation:  "Workflow failures indicate convergence risk and may threaten reconcile fairness",
	},
	{
		QueryID:       "minio_offline_disks",
		ThresholdKey:  "minio:offline_disks",
		FailureModes:  []string{"objectstore.availability_degraded"},
		Invariants:    []string{"objectstore.topology_contract"},
		DecisionRules: []string{"minio_topology_requires_three_storage_nodes"},
		Explanation:   "Offline MinIO disks risk artifact availability and violate topology contract",
	},
	{
		QueryID:      "scylla_disk_percent",
		ThresholdKey: "scylla:disk_percent",
		FailureModes: []string{"scylla.disk_full_degrades_workflow_backend"},
		Invariants:   []string{"scylla.critical_keyspace_replication_policy"},
		Explanation:  "ScyllaDB disk pressure risks workflow/AI memory backend availability",
	},
}

// ─── Public entry point ──────────────────────────────────────────────────────

// Extract indexes metric_queries.yaml and metric_thresholds.yaml from
// docsAwarenessDir/knowledge/ and links the resulting graph nodes to failure
// modes and invariants already present in g.
//
// If docsAwarenessDir is empty or the YAML files do not exist, Extract returns
// nil — the caller is expected to skip metric indexing gracefully.
func Extract(ctx context.Context, g *graph.Graph, docsAwarenessDir string) error {
	if docsAwarenessDir == "" {
		return nil
	}

	knowledgeDir := filepath.Join(docsAwarenessDir, "knowledge")

	queries, err := loadMetricQueries(filepath.Join(knowledgeDir, "metric_queries.yaml"))
	if err != nil {
		return fmt.Errorf("metrics.Extract: load queries: %w", err)
	}

	thresholds, err := loadMetricThresholds(filepath.Join(knowledgeDir, "metric_thresholds.yaml"))
	if err != nil {
		return fmt.Errorf("metrics.Extract: load thresholds: %w", err)
	}

	// Step 1: index metric query nodes.
	if err := indexQueries(ctx, g, queries); err != nil {
		return fmt.Errorf("metrics.Extract: index queries: %w", err)
	}

	// Step 2: index threshold nodes.
	if err := indexThresholds(ctx, g, thresholds); err != nil {
		return fmt.Errorf("metrics.Extract: index thresholds: %w", err)
	}

	// Step 3: link queries to service nodes.
	if err := linkQueriesToServices(ctx, g, queries); err != nil {
		return fmt.Errorf("metrics.Extract: link queries to services: %w", err)
	}

	// Step 4: create warning rules and link to failure modes / invariants.
	if err := createWarningRules(ctx, g, thresholds); err != nil {
		return fmt.Errorf("metrics.Extract: create warning rules: %w", err)
	}

	return nil
}

// ─── Loaders ────────────────────────────────────────────────────────────────

func loadMetricQueries(path string) ([]yamlMetricQuery, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f metricQueryFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return f.Queries, nil
}

func loadMetricThresholds(path string) (map[string]map[string]yamlThresholdLevel, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f metricThresholdFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return f.Thresholds, nil
}

// ─── Step 1: index queries ───────────────────────────────────────────────────

func indexQueries(ctx context.Context, g *graph.Graph, queries []yamlMetricQuery) error {
	for _, q := range queries {
		nodeID := "metric_query:" + q.ID
		summary := fmt.Sprintf("query for %s service — %s", q.Service, q.Unit)
		if err := g.AddNode(ctx, graph.Node{
			ID:      nodeID,
			Type:    graph.NodeTypeMetricQuery,
			Name:    q.ID,
			Summary: summary,
			Metadata: map[string]any{
				"query":   q.Query,
				"service": q.Service,
				"unit":    q.Unit,
			},
		}); err != nil {
			return fmt.Errorf("add metric_query node %s: %w", q.ID, err)
		}
	}
	return nil
}

// ─── Step 2: index thresholds ────────────────────────────────────────────────

func indexThresholds(ctx context.Context, g *graph.Graph, thresholds map[string]map[string]yamlThresholdLevel) error {
	for svc, metrics := range thresholds {
		for metric, level := range metrics {
			nodeID := fmt.Sprintf("metric_threshold:%s:%s", svc, metric)
			if err := g.AddNode(ctx, graph.Node{
				ID:   nodeID,
				Type: graph.NodeTypeMetricThreshold,
				Name: svc + "." + metric,
				Metadata: map[string]any{
					"warn":     level.Warn,
					"critical": level.Critical,
					"service":  svc,
					"metric":   metric,
				},
			}); err != nil {
				return fmt.Errorf("add metric_threshold node %s.%s: %w", svc, metric, err)
			}
		}
	}
	return nil
}

// ─── Step 3: link queries to services ────────────────────────────────────────

func linkQueriesToServices(ctx context.Context, g *graph.Graph, queries []yamlMetricQuery) error {
	for _, q := range queries {
		if q.Service == "" {
			continue
		}
		queryNodeID := "metric_query:" + q.ID

		// Try both node ID patterns for globular services.
		svcNodeID := resolveServiceNodeID(ctx, g, q.Service)
		if svcNodeID != "" {
			if err := g.AddEdge(ctx, graph.Edge{
				Src:  queryNodeID,
				Kind: graph.EdgeMetricQueryObservesService,
				Dst:  svcNodeID,
			}); err != nil {
				return fmt.Errorf("link query %s to service %s: %w", q.ID, svcNodeID, err)
			}
		} else {
			// Service node not yet in graph; record service name in metadata
			// so future enrichment passes can complete the linkage.
			if err := g.AddNode(ctx, graph.Node{
				ID:   queryNodeID,
				Type: graph.NodeTypeMetricQuery,
				Name: q.ID,
				Metadata: map[string]any{
					"query":        q.Query,
					"service":      q.Service,
					"unit":         q.Unit,
					"service_name": q.Service,
				},
			}); err != nil {
				return fmt.Errorf("update metric_query node %s with service_name: %w", q.ID, err)
			}
		}
	}
	return nil
}

// resolveServiceNodeID tries both "globular_service:{name}" and
// "globular_service:{name}-server" patterns and returns the first that exists.
// Returns "" when neither exists.
func resolveServiceNodeID(ctx context.Context, g *graph.Graph, svcName string) string {
	candidates := []string{
		"globular_service:" + svcName,
		"globular_service:" + svcName + "-server",
	}
	for _, id := range candidates {
		n, err := g.FindNode(ctx, id)
		if err == nil && n != nil {
			return id
		}
	}
	return ""
}

// ─── Step 4: warning rules ───────────────────────────────────────────────────

func createWarningRules(ctx context.Context, g *graph.Graph, thresholds map[string]map[string]yamlThresholdLevel) error {
	for _, m := range metricWarningMappings {
		if err := createWarningRule(ctx, g, m, thresholds); err != nil {
			return fmt.Errorf("warning rule %s: %w", m.QueryID, err)
		}
	}
	return nil
}

func createWarningRule(ctx context.Context, g *graph.Graph, m metricWarningMapping, thresholds map[string]map[string]yamlThresholdLevel) error {
	ruleID := "metric_warning_rule:" + m.QueryID

	// Resolve the threshold node ID from the ThresholdKey "<service>:<metric>".
	thresholdNodeID := "metric_threshold:" + m.ThresholdKey

	// Count how many linked nodes exist vs. are missing.
	linkedFMs := 0
	missingFMs := 0
	linkedInvs := 0
	missingInvs := 0

	// Resolve warn/critical values from loaded thresholds (best-effort).
	var warn, critical float64
	svc, metric := splitThresholdKey(m.ThresholdKey)
	if svc != "" && metric != "" && thresholds != nil {
		if svcThresholds, ok := thresholds[svc]; ok {
			if level, ok := svcThresholds[metric]; ok {
				warn = level.Warn
				critical = level.Critical
			}
		}
	}

	// Create the warning rule node.
	if err := g.AddNode(ctx, graph.Node{
		ID:      ruleID,
		Type:    graph.NodeTypeMetricWarningRule,
		Name:    "metric_warning:" + m.QueryID,
		Summary: m.Explanation,
		Metadata: map[string]any{
			"query_id":      m.QueryID,
			"threshold_key": m.ThresholdKey,
			"warn":          warn,
			"critical":      critical,
			"explanation":   m.Explanation,
		},
	}); err != nil {
		return fmt.Errorf("add metric_warning_rule node: %w", err)
	}

	// Link warning rule → threshold (if threshold node exists).
	tn, err := g.FindNode(ctx, thresholdNodeID)
	if err == nil && tn != nil {
		// Also emit EdgeMetricThresholdAppliesToService from threshold to any linked service.
		// We link the threshold to its canonical service node if resolvable.
		if svc != "" {
			svcNodeID := resolveServiceNodeID(ctx, g, svc)
			if svcNodeID != "" {
				if err := g.AddEdge(ctx, graph.Edge{
					Src:  thresholdNodeID,
					Kind: graph.EdgeMetricThresholdAppliesToService,
					Dst:  svcNodeID,
				}); err != nil {
					return fmt.Errorf("link threshold %s to service %s: %w", thresholdNodeID, svcNodeID, err)
				}
			}
		}
	}

	// Link warning rule → failure modes (best-effort).
	for _, fmID := range m.FailureModes {
		fmNodeID := "failure_mode:" + fmID
		n, err := g.FindNode(ctx, fmNodeID)
		if err != nil || n == nil {
			missingFMs++
			continue
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  ruleID,
			Kind: graph.EdgeMetricWarningIndicatesFailureMode,
			Dst:  fmNodeID,
		}); err != nil {
			return fmt.Errorf("link warning rule to failure mode %s: %w", fmID, err)
		}
		linkedFMs++
	}

	// Link warning rule → invariants (best-effort).
	for _, invID := range m.Invariants {
		invNodeID := "invariant:" + invID
		n, err := g.FindNode(ctx, invNodeID)
		if err != nil || n == nil {
			missingInvs++
			continue
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  ruleID,
			Kind: graph.EdgeMetricWarningRisksInvariant,
			Dst:  invNodeID,
		}); err != nil {
			return fmt.Errorf("link warning rule to invariant %s: %w", invID, err)
		}
		linkedInvs++
	}

	// Link warning rule → decision rules (best-effort).
	linkedDRs := 0
	missingDRs := 0
	for _, drID := range m.DecisionRules {
		drNodeID := "decision_rule:" + drID
		n, err := g.FindNode(ctx, drNodeID)
		if err != nil || n == nil {
			missingDRs++
			continue
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  ruleID,
			Kind: graph.EdgeMetricWarningTriggerRule,
			Dst:  drNodeID,
		}); err != nil {
			return fmt.Errorf("link warning rule to decision rule %s: %w", drID, err)
		}
		linkedDRs++
	}

	// Update node metadata with linkage stats so coverage reports can surface gaps.
	if missingFMs > 0 || missingInvs > 0 || missingDRs > 0 {
		if err := g.AddNode(ctx, graph.Node{
			ID:      ruleID,
			Type:    graph.NodeTypeMetricWarningRule,
			Name:    "metric_warning:" + m.QueryID,
			Summary: m.Explanation,
			Metadata: map[string]any{
				"query_id":       m.QueryID,
				"threshold_key":  m.ThresholdKey,
				"warn":           warn,
				"critical":       critical,
				"explanation":    m.Explanation,
				"linked_fms":     linkedFMs,
				"missing_fms":    missingFMs,
				"linked_invs":    linkedInvs,
				"missing_invs":   missingInvs,
				"linked_drs":     linkedDRs,
				"missing_drs":    missingDRs,
			},
		}); err != nil {
			return fmt.Errorf("update metric_warning_rule metadata: %w", err)
		}
	}

	return nil
}

// splitThresholdKey splits "<service>:<metric>" into (service, metric).
// Returns ("", "") on malformed input.
func splitThresholdKey(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			return key[:i], key[i+1:]
		}
	}
	return "", ""
}
