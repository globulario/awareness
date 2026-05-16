package preflight_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/preflight"
)

func TestPreflightIncludesExperienceHints(t *testing.T) {
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	_, err = g.CreateExperience(context.Background(), graph.ExperienceEntry{
		ID:             "exp.workflow.defer.seed",
		Kind:           "debugging_experience",
		Domain:         "workflow",
		Capability:     "workflow.defer",
		Status:         "success",
		Summary:        "preserve deferred status",
		GoalOriginal:   "workflow retry loop after failed package install",
		GoalNormalized: "workflow retry loop after failed package install",
		StrategyID:     "trace_typed_error_propagation",
		Lesson:         "trace typed errors across boundaries",
		NextTimeHint:   "inspect persistent workflow status before timing changes",
	})
	if err != nil {
		t.Fatalf("CreateExperience: %v", err)
	}

	r, err := preflight.Run(context.Background(), preflight.Options{Task: "workflow retry loop after failed package install"}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(r.ExperienceHints) == 0 {
		t.Fatalf("expected experience hints, got none")
	}
}

func TestPreflightIncludesSeededWorkflowDeferHint(t *testing.T) {
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if _, err := g.SeedWorkflowDeferExperience(context.Background()); err != nil {
		t.Fatalf("SeedWorkflowDeferExperience: %v", err)
	}
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task: "workflow retry loop after failed package install",
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := false
	for _, h := range r.ExperienceHints {
		if h.ExperienceID == graph.SeedWorkflowDeferExperienceID {
			if h.Verdict == "" {
				t.Fatalf("expected verdict for seeded hint")
			}
			if h.FinalScore <= 0 {
				t.Fatalf("expected final score > 0 for seeded hint, got %f", h.FinalScore)
			}
			if len(h.Reasons) == 0 {
				t.Fatalf("expected ranking reasons for seeded hint")
			}
			if len(h.WorkedPaths) == 0 {
				t.Fatalf("expected worked paths for seeded hint")
			}
			if len(h.EvidenceTypes) == 0 {
				t.Fatalf("expected evidence types for seeded hint")
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected seeded hint %q in %+v", graph.SeedWorkflowDeferExperienceID, r.ExperienceHints)
	}
}
