package graph_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/graph"
)

func TestExperienceStoreLifecycle(t *testing.T) {
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	e, err := g.CreateExperience(context.Background(), graph.ExperienceEntry{
		ID:           "exp.workflow.defer.test",
		Kind:         "debugging_experience",
		Domain:       "workflow",
		Capability:   "workflow.defer",
		Status:       "unproven",
		Summary:      "workflow defer status path",
		GoalOriginal: "stop workflow retry hammer",
		StrategyID:   "trace_typed_error_propagation",
	})
	if err != nil {
		t.Fatalf("CreateExperience: %v", err)
	}
	if e.ID == "" {
		t.Fatal("expected id")
	}

	if _, err := g.AddExperienceAttempt(context.Background(), graph.ExperienceAttempt{
		ExperienceID: e.ID,
		StrategyID:   "trace_typed_error_propagation",
		Action:       "inspect aggregation",
		Outcome:      "found dropped type",
		Status:       "success",
	}); err != nil {
		t.Fatalf("AddExperienceAttempt: %v", err)
	}

	if _, err := g.AddExperienceObservation(context.Background(), graph.ExperienceObservation{
		ExperienceID: e.ID,
		Type:         "test",
		Summary:      "defer test failed before and passed after",
		Confidence:   0.9,
	}); err != nil {
		t.Fatalf("AddExperienceObservation: %v", err)
	}

	if err := g.CloseExperience(context.Background(), e.ID, "success", "trace typed errors", "check typed errors first", nil); err != nil {
		t.Fatalf("CloseExperience: %v", err)
	}

	rec, err := g.GetExperience(context.Background(), e.ID)
	if err != nil {
		t.Fatalf("GetExperience: %v", err)
	}
	if rec == nil || rec.Experience.Status != "success" {
		t.Fatalf("unexpected record: %+v", rec)
	}

	hits, err := g.SearchSimilarExperiences(context.Background(), graph.ExperienceSearchQuery{Goal: "workflow retry hammer", Domain: "workflow", Limit: 3})
	if err != nil {
		t.Fatalf("SearchSimilarExperiences: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one hit")
	}
}

func TestExperiencePromoteLessonCandidate(t *testing.T) {
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	_, err = g.CreateExperience(context.Background(), graph.ExperienceEntry{
		ID:           "exp.workflow.defer.promote",
		Kind:         "debugging_experience",
		Domain:       "workflow",
		Capability:   "workflow.defer",
		Status:       "success",
		Summary:      "avoid timeout-only fix",
		GoalOriginal: "stop retry hammer",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = g.AddExperienceAttempt(context.Background(), graph.ExperienceAttempt{
		ExperienceID: "exp.workflow.defer.promote",
		Action:       "test promote path",
		Status:       "success",
	})
	_, _ = g.AddExperienceObservation(context.Background(), graph.ExperienceObservation{
		ExperienceID: "exp.workflow.defer.promote",
		Type:         "test",
		Summary:      "promotion readiness evidence",
		Confidence:   0.9,
	})
	_ = g.CloseExperience(context.Background(), "exp.workflow.defer.promote", "success", "avoid timeout-only fix", "check persistent state first", &graph.ExperienceScorecard{
		Success:       1.0,
		ReuseValue:    0.8,
		Specificity:   0.8,
		RiskReduction: 0.8,
		Confidence:    0.9,
	})
	res, err := g.PromoteExperienceLesson(context.Background(), "exp.workflow.defer.promote", "forbidden_fix", "Do not increase timeout only")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "candidate_recorded" {
		t.Fatalf("unexpected status: %s", res.Status)
	}
	n, err := g.FindNode(context.Background(), res.CandidateID)
	if err != nil || n == nil {
		t.Fatalf("candidate node missing: %v", err)
	}
}

func TestSeedWorkflowDeferExperienceIdempotent(t *testing.T) {
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	first, err := g.SeedWorkflowDeferExperience(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.SeedWorkflowDeferExperience(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != graph.SeedWorkflowDeferExperienceID || second.ID != first.ID {
		t.Fatalf("unexpected IDs: first=%s second=%s", first.ID, second.ID)
	}
	rec, err := g.GetExperience(context.Background(), graph.SeedWorkflowDeferExperienceID)
	if err != nil || rec == nil {
		t.Fatalf("seeded experience not retrievable: %v", err)
	}
	if len(rec.Attempts) < 2 || len(rec.Observations) < 2 {
		t.Fatalf("expected seeded attempts/observations, got attempts=%d obs=%d", len(rec.Attempts), len(rec.Observations))
	}
}

func TestCloseExperienceDerivesEvidenceStrength(t *testing.T) {
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	_, err = g.CreateExperience(context.Background(), graph.ExperienceEntry{
		ID:           "exp.workflow.evidence.strength",
		Kind:         "debugging_experience",
		Domain:       "workflow",
		Capability:   "workflow.defer",
		Status:       "partial",
		Summary:      "evidence strength derivation",
		GoalOriginal: "check evidence strength",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = g.AddExperienceAttempt(context.Background(), graph.ExperienceAttempt{
		ExperienceID: "exp.workflow.evidence.strength",
		Action:       "run test",
		Status:       "success",
	})
	_, _ = g.AddExperienceObservation(context.Background(), graph.ExperienceObservation{
		ExperienceID: "exp.workflow.evidence.strength",
		Type:         "test",
		Summary:      "unit tests passed",
		Confidence:   0.9,
	})
	_, _ = g.AddExperienceObservation(context.Background(), graph.ExperienceObservation{
		ExperienceID: "exp.workflow.evidence.strength",
		Type:         "operator_note",
		Summary:      "operator confirmed runtime behavior",
		Confidence:   0.6,
	})
	err = g.CloseExperience(context.Background(), "exp.workflow.evidence.strength", "success", "use tests first", "check tests first", &graph.ExperienceScorecard{
		Success:       1.0,
		ReuseValue:    0.7,
		Specificity:   0.7,
		RiskReduction: 0.8,
		Confidence:    0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := g.GetExperience(context.Background(), "exp.workflow.evidence.strength")
	if err != nil || rec == nil || rec.Scorecard == nil {
		t.Fatalf("missing scorecard: %v", err)
	}
	if rec.Scorecard.EvidenceStrength <= 0 {
		t.Fatalf("expected derived evidence_strength > 0, got %f", rec.Scorecard.EvidenceStrength)
	}
}

func TestLinkExperienceArtifacts(t *testing.T) {
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	_, err = g.CreateExperience(context.Background(), graph.ExperienceEntry{
		ID:           "exp.link.test",
		Kind:         "debugging_experience",
		Domain:       "workflow",
		Capability:   "workflow.defer",
		Status:       "partial",
		Summary:      "link test",
		GoalOriginal: "test links",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = g.LinkExperienceArtifacts(context.Background(), "exp.link.test", graph.ExperienceLinkInput{
		ClosureEntryID:       "CLOSE-1",
		InvariantIDs:         []string{"convergence.no_infinite_retry"},
		ForbiddenFixIDs:      []string{"retry_hammer.increase_timeout_only"},
		AvoidedForbiddenFixs: []string{"retry_hammer.increase_timeout_only"},
		TouchedFiles:         []string{"golang/workflow/engine/engine.go"},
		ChangedSymbols:       []string{"engine.compileSubSteps"},
	})
	if err != nil {
		t.Fatal(err)
	}
	edges, err := g.OutgoingEdges(context.Background(), "experience:exp.link.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) == 0 {
		t.Fatal("expected outgoing edges")
	}
}

func TestSearchSimilarExperiencesUsesArtifactOverlap(t *testing.T) {
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	_, err = g.CreateExperience(context.Background(), graph.ExperienceEntry{
		ID:           "exp.rank.match",
		Kind:         "debugging_experience",
		Domain:       "workflow",
		Capability:   "workflow.defer",
		Status:       "success",
		Summary:      "workflow defer fix",
		GoalOriginal: "workflow retry loop after failed package install",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = g.LinkExperienceArtifacts(context.Background(), "exp.rank.match", graph.ExperienceLinkInput{
		TouchedFiles:    []string{"golang/workflow/engine/engine.go"},
		ChangedSymbols:  []string{"engine.compileSubSteps"},
		InvariantIDs:    []string{"convergence.no_infinite_retry"},
		ForbiddenFixIDs: []string{"retry_hammer.increase_timeout_only"},
	})
	_, _ = g.AddExperienceAttempt(context.Background(), graph.ExperienceAttempt{
		ExperienceID: "exp.rank.match",
		Action:       "inspect compileSubSteps defer propagation",
		Status:       "success",
	})
	_, _ = g.AddExperienceAttempt(context.Background(), graph.ExperienceAttempt{
		ExperienceID: "exp.rank.match",
		Action:       "increase retry interval only",
		Status:       "failed",
	})
	_, _ = g.AddExperienceObservation(context.Background(), graph.ExperienceObservation{
		ExperienceID: "exp.rank.match",
		Type:         "test",
		Summary:      "workflow defer test passed",
		Confidence:   0.9,
	})

	_, err = g.CreateExperience(context.Background(), graph.ExperienceEntry{
		ID:           "exp.rank.nomatch",
		Kind:         "debugging_experience",
		Domain:       "frontend",
		Capability:   "media.youtube_import",
		Status:       "success",
		Summary:      "frontend media import",
		GoalOriginal: "add youtube import",
	})
	if err != nil {
		t.Fatal(err)
	}

	hits, err := g.SearchSimilarExperiences(context.Background(), graph.ExperienceSearchQuery{
		Goal:            "workflow retry loop after failed package install",
		Domain:          "workflow",
		Capability:      "workflow.defer",
		Files:           []string{"golang/workflow/engine/engine.go"},
		Symbols:         []string{"engine.compileSubSteps"},
		InvariantIDs:    []string{"convergence.no_infinite_retry"},
		ForbiddenFixIDs: []string{"retry_hammer.increase_timeout_only"},
		Limit:           2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits")
	}
	if hits[0].ExperienceID != "exp.rank.match" {
		t.Fatalf("unexpected top hit: %+v", hits[0])
	}
	if len(hits[0].Reasons) == 0 {
		t.Fatalf("expected reasons on top hit")
	}
	if len(hits[0].WorkedPaths) == 0 && len(hits[0].FailedPaths) == 0 {
		t.Fatalf("expected worked/failed paths in hit: %+v", hits[0])
	}
}

func TestPromotionReadinessAndContradictionGovernance(t *testing.T) {
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	_, err = g.CreateExperience(context.Background(), graph.ExperienceEntry{
		ID:           "exp.gov.base",
		Kind:         "debugging_experience",
		Domain:       "workflow",
		Capability:   "workflow.defer",
		Status:       "success",
		Summary:      "base experience",
		GoalOriginal: "stop retry hammer",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = g.AddExperienceAttempt(context.Background(), graph.ExperienceAttempt{
		ExperienceID: "exp.gov.base",
		Action:       "inspect typed errors",
		Status:       "success",
	})
	_, _ = g.AddExperienceObservation(context.Background(), graph.ExperienceObservation{
		ExperienceID: "exp.gov.base",
		Type:         "test",
		Summary:      "tests passed",
		Confidence:   0.9,
	})
	if err := g.CloseExperience(context.Background(), "exp.gov.base", "success", "trace typed errors", "check typed errors first", &graph.ExperienceScorecard{Success: 1, ReuseValue: 0.8, Specificity: 0.8, RiskReduction: 0.8, Confidence: 0.9}); err != nil {
		t.Fatal(err)
	}
	ready, err := g.EvaluatePromotionReadiness(context.Background(), "exp.gov.base")
	if err != nil {
		t.Fatal(err)
	}
	if !ready.Ready {
		t.Fatalf("expected ready, got %+v", ready)
	}

	_, err = g.CreateExperience(context.Background(), graph.ExperienceEntry{
		ID:           "exp.gov.contradictor",
		Kind:         "debugging_experience",
		Domain:       "workflow",
		Capability:   "workflow.defer",
		Status:       "success",
		Summary:      "contradictor",
		GoalOriginal: "same goal",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.LinkExperienceRelation(context.Background(), "exp.gov.base", "contradicted_by", "exp.gov.contradictor"); err != nil {
		t.Fatal(err)
	}
	ready2, err := g.EvaluatePromotionReadiness(context.Background(), "exp.gov.base")
	if err != nil {
		t.Fatal(err)
	}
	if ready2.Ready {
		t.Fatalf("expected not ready after contradiction: %+v", ready2)
	}
	if _, err := g.PromoteExperienceLesson(context.Background(), "exp.gov.base", "forbidden_fix", "candidate"); err == nil {
		t.Fatal("expected promotion to be blocked when contradicted")
	}
}
