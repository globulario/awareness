package learning_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/learning"
)

const fixtureEnvoy = "testdata/incidents/envoy_desired_hash_restart_storm.yaml"

func TestIncidentBundleLoadsFromYAML(t *testing.T) {
	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}

	if b.IncidentID == "" {
		t.Error("incident_id is empty")
	}
	if b.Severity != "critical" {
		t.Errorf("expected severity critical, got %q", b.Severity)
	}
	if len(b.Symptoms) == 0 {
		t.Error("symptoms must not be empty")
	}
	if len(b.ObservedServices) == 0 {
		t.Error("observed_services must not be empty")
	}
	if b.SuspectedRootCause == "" {
		t.Error("suspected_root_cause must not be empty")
	}
}

func TestIncidentBundleHasManualRepairs(t *testing.T) {
	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}
	if len(b.ManualRepairs) == 0 {
		t.Error("manual_repairs must not be empty in the fixture")
	}
}

func TestIncidentBundleHasProposedSection(t *testing.T) {
	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}
	if b.Proposed == nil {
		t.Fatal("fixture must include a proposed section for testing")
	}
	if len(b.Proposed.FailureModes) == 0 {
		t.Error("proposed.failure_modes must not be empty")
	}
	if len(b.Proposed.Invariants) == 0 {
		t.Error("proposed.invariants must not be empty")
	}
}

func TestIncidentBundleMissingFile(t *testing.T) {
	_, err := learning.LoadIncidentBundle("testdata/incidents/nonexistent.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestRecordIncidentInGraph(t *testing.T) {
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer g.Close()

	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}

	if err := learning.RecordIncidentInGraph(ctx, g, b); err != nil {
		t.Fatalf("RecordIncidentInGraph: %v", err)
	}

	// Incident node must exist.
	n, err := g.FindNode(ctx, "incident:"+b.IncidentID)
	if err != nil {
		t.Fatalf("FindNode: %v", err)
	}
	if n == nil {
		t.Error("incident node not created in graph")
	}

	// Incident record must be stored.
	inc, err := g.FindIncident(ctx, b.IncidentID)
	if err != nil {
		t.Fatalf("FindIncident: %v", err)
	}
	if inc == nil {
		t.Error("incident record not stored in graph DB")
	}
}
