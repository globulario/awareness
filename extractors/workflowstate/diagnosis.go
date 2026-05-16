package workflowstate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
	workflowpb "github.com/globulario/awareness/workflowpb"
)

// diagResults holds the count of nodes emitted during diagnosis.
type diagResults struct {
	nodesEmitted int
}

// failureModeEntry mirrors the top-level item in failure_modes.yaml.
type failureModeEntry struct {
	ID                string   `yaml:"id"`
	Title             string   `yaml:"title"`
	Severity          string   `yaml:"severity"`
	Symptoms          []string `yaml:"symptoms"`
	RelatedInvariants []string `yaml:"related_invariants"`
	RelatedServices   []string `yaml:"related_services"`
	ForbiddenFixes    []string `yaml:"forbidden_fixes"`
}

type failureModeFile struct {
	FailureModes []failureModeEntry `yaml:"failure_modes"`
}

// diagnoseRuns matches failed runs against known failure modes and invariants,
// emitting workflow_failure_pattern nodes and linking them to failure_mode/invariant nodes.
func diagnoseRuns(ctx context.Context, g *graph.Graph, runs []*workflowpb.WorkflowRun, docsAwarenessDir string, now time.Time) diagResults {
	results := diagResults{}
	if len(runs) == 0 {
		return results
	}

	fms := loadFailureModes(docsAwarenessDir)

	for _, run := range runs {
		if run.GetStatus() != workflowpb.RunStatus_RUN_STATUS_FAILED {
			continue
		}
		matched := matchFailureModes(run, fms)
		if len(matched) == 0 {
			continue
		}

		patternID := fmt.Sprintf("workflow_failure_pattern:%s", run.GetId()[:8])
		expiresAt := now.Add(ttlFailedRun * time.Second)
		meta := liveNodeMeta("workflow_failure_pattern", now, expiresAt, ttlFailedRun, "medium")
		meta["workflow_name"] = run.GetWorkflowName()
		meta["run_id"] = run.GetId()
		meta["error_message"] = run.GetErrorMessage()
		meta["failure_class"] = run.GetFailureClass().String()
		meta["matched_failure_modes"] = matchedIDs(matched)
		meta["threatened_invariants"] = collectInvariants(matched)

		summary := fmt.Sprintf("workflow=%s failed; matched failure_modes: %s", run.GetWorkflowName(), strings.Join(matchedIDs(matched), ", "))

		if err := g.AddNode(ctx, graph.Node{
			ID:       patternID,
			Type:     graph.NodeTypeFailureMode,
			Name:     "workflow failure: " + run.GetWorkflowName(),
			Summary:  summary,
			Metadata: meta,
		}); err != nil {
			continue
		}
		results.nodesEmitted++

		runID := "workflow_run:" + run.GetId()
		// Link run → failure pattern.
		_ = g.AddEdge(ctx, graph.Edge{
			Src: runID, Kind: graph.EdgeWorkflowFailureIndicates, Dst: patternID, Phase: "live",
		})

		// Link pattern → matched failure mode nodes. P1-1: stamp with
		// last_observed_at + observation_source so coverage classifies
		// this as an ACTIVE detector. These edges are emitted at
		// observation time (a failed run hit the pattern right now),
		// so they're conceptually always-active. Stamping makes that
		// explicit and lets the lifecycle window decay them later if
		// the same pattern stops firing.
		for _, fm := range matched {
			fmID := "failure_mode:" + fm.ID
			_ = g.AddEdge(ctx, graph.Edge{
				Src: patternID, Kind: graph.EdgeWorkflowFailureIndicates, Dst: fmID, Phase: "live",
				Metadata: map[string]any{
					"last_observed_at":   now.Unix(),
					"observation_source": "workflow",
				},
			})
			// Link run → invariants at risk.
			for _, inv := range fm.RelatedInvariants {
				invID := "invariant:" + inv
				_ = g.AddEdge(ctx, graph.Edge{
					Src: runID, Kind: graph.EdgeWorkflowFailureRisksInvariant, Dst: invID, Phase: "live",
				})
			}
		}

		// Emit forbidden action reminder for deterministic failures with high retry count.
		if run.GetRetryCount() > 3 {
			fbID := fmt.Sprintf("workflow_forbidden_retry:%s", run.GetId()[:8])
			_ = g.AddNode(ctx, graph.Node{
				ID:      fbID,
				Type:    graph.NodeTypeForbiddenFix,
				Name:    "blind retry forbidden: " + run.GetWorkflowName(),
				Summary: fmt.Sprintf("workflow %s has retried %d times — blind retry without terminal classification is forbidden", run.GetWorkflowName(), run.GetRetryCount()),
				Metadata: map[string]any{
					"source_tier": sourceTier,
					"collector":   collectorID,
					"run_id":      run.GetId(),
					"retry_count": run.GetRetryCount(),
				},
			})
			_ = g.AddEdge(ctx, graph.Edge{
				Src: patternID, Kind: graph.EdgeViolates, Dst: fbID, Phase: "live",
			})
			results.nodesEmitted++
		}
	}

	return results
}

// matchFailureModes returns failure modes that match the run's error text/context.
func matchFailureModes(run *workflowpb.WorkflowRun, fms []failureModeEntry) []failureModeEntry {
	var matched []failureModeEntry
	errMsg := strings.ToLower(run.GetErrorMessage())
	wfName := strings.ToLower(run.GetWorkflowName())
	component := ""
	if run.GetContext() != nil {
		component = strings.ToLower(run.GetContext().GetComponentName())
	}
	fc := run.GetFailureClass()

	for _, fm := range fms {
		if matchesEntry(fm, errMsg, wfName, component, fc) {
			matched = append(matched, fm)
		}
	}
	return matched
}

// matchesEntry checks if a failure mode entry matches the run properties.
func matchesEntry(fm failureModeEntry, errMsg, wfName, component string, fc workflowpb.FailureClass) bool {
	// Service name match.
	for _, svc := range fm.RelatedServices {
		if strings.Contains(wfName, strings.ToLower(svc)) ||
			strings.Contains(component, strings.ToLower(svc)) {
			return true
		}
	}

	// Failure class to failure mode keyword mapping.
	switch fc {
	case workflowpb.FailureClass_FAILURE_CLASS_NETWORK:
		if strings.Contains(strings.ToLower(fm.ID), "network") ||
			strings.Contains(strings.ToLower(fm.ID), "endpoint") ||
			strings.Contains(strings.ToLower(fm.ID), "dns") {
			return true
		}
	case workflowpb.FailureClass_FAILURE_CLASS_SYSTEMD:
		if strings.Contains(strings.ToLower(fm.ID), "systemd") ||
			strings.Contains(strings.ToLower(fm.ID), "runtime") ||
			strings.Contains(strings.ToLower(fm.ID), "service") {
			return true
		}
	case workflowpb.FailureClass_FAILURE_CLASS_REPOSITORY:
		if strings.Contains(strings.ToLower(fm.ID), "repository") ||
			strings.Contains(strings.ToLower(fm.ID), "artifact") ||
			strings.Contains(strings.ToLower(fm.ID), "checksum") {
			return true
		}
	case workflowpb.FailureClass_FAILURE_CLASS_VALIDATION:
		if strings.Contains(strings.ToLower(fm.ID), "convergence") ||
			strings.Contains(strings.ToLower(fm.ID), "validation") ||
			strings.Contains(strings.ToLower(fm.ID), "verify") {
			return true
		}
	}

	// Error message substring match against symptom keywords.
	for _, sym := range fm.Symptoms {
		words := strings.Fields(strings.ToLower(sym))
		for _, w := range words {
			if len(w) > 5 && strings.Contains(errMsg, w) {
				return true
			}
		}
	}
	return false
}

// loadFailureModes reads failure_modes.yaml from docsAwarenessDir.
func loadFailureModes(docsAwarenessDir string) []failureModeEntry {
	if docsAwarenessDir == "" {
		return nil
	}
	path := filepath.Join(docsAwarenessDir, "failure_modes.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f failureModeFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil
	}
	return f.FailureModes
}

func matchedIDs(fms []failureModeEntry) []string {
	ids := make([]string, 0, len(fms))
	for _, fm := range fms {
		ids = append(ids, fm.ID)
	}
	return ids
}

func collectInvariants(fms []failureModeEntry) []string {
	seen := map[string]bool{}
	var out []string
	for _, fm := range fms {
		for _, inv := range fm.RelatedInvariants {
			if !seen[inv] {
				seen[inv] = true
				out = append(out, inv)
			}
		}
	}
	return out
}
