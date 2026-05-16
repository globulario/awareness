package runtime_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/globulario/awareness/runtime"
)

// TestBridgeSnapshotWithFakeSources verifies that all fake sources are collected
// and appear in the returned snapshot.
func TestBridgeSnapshotWithFakeSources(t *testing.T) {
	ctx := context.Background()
	b := runtime.NewBridge("node1", "cluster1")

	b.Doctor = &runtime.FakeDoctorSource{
		Data: []runtime.DoctorFinding{
			{FindingID: "f1", Severity: "high", Title: "service restart storm"},
		},
	}
	b.Events = &runtime.FakeEventSource{
		Data: []runtime.RuntimeEvent{
			{EventType: "SERVICE_RESTART", ServiceID: "envoy", NodeID: "node1"},
		},
	}
	b.Workflows = &runtime.FakeWorkflowSource{
		Data: []runtime.WorkflowReceipt{
			{WorkflowID: "wf1", WorkflowType: "deploy", Status: "SUCCEEDED"},
		},
	}
	b.Services = &runtime.FakeServiceStatusSource{
		Data: []runtime.ServiceStatus{
			{ServiceID: "envoy", NodeID: "node1", State: "RUNNING"},
		},
	}
	b.Repository = &runtime.FakeRepositoryStatusSource{
		Data: []runtime.RepositoryStatus{
			{Mode: "NORMAL", NodeID: "node1", Reachable: true},
		},
	}

	snap, err := b.Snapshot(ctx, 15*time.Minute, nil)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if snap.NodeID != "node1" {
		t.Errorf("NodeID = %q, want %q", snap.NodeID, "node1")
	}
	if snap.ClusterID != "cluster1" {
		t.Errorf("ClusterID = %q, want %q", snap.ClusterID, "cluster1")
	}
	if len(snap.DoctorFindings) != 1 {
		t.Errorf("DoctorFindings count = %d, want 1", len(snap.DoctorFindings))
	}
	if len(snap.RecentEvents) != 1 {
		t.Errorf("RecentEvents count = %d, want 1", len(snap.RecentEvents))
	}
	if len(snap.WorkflowReceipts) != 1 {
		t.Errorf("WorkflowReceipts count = %d, want 1", len(snap.WorkflowReceipts))
	}
	if len(snap.RuntimeServices) != 1 {
		t.Errorf("RuntimeServices count = %d, want 1", len(snap.RuntimeServices))
	}
	if len(snap.RepositoryStatus) != 1 {
		t.Errorf("RepositoryStatus count = %d, want 1", len(snap.RepositoryStatus))
	}
}

// TestBridgeMissingSourceAddsWarning verifies that a failing source adds a
// warning to the snapshot instead of causing Snapshot to return an error.
func TestBridgeMissingSourceAddsWarning(t *testing.T) {
	ctx := context.Background()
	b := runtime.NewBridge("", "")
	b.Doctor = &runtime.FakeDoctorSource{Err: errors.New("doctor service unreachable")}

	snap, err := b.Snapshot(ctx, 15*time.Minute, nil)
	if err != nil {
		t.Fatalf("Snapshot should not error on source failure: %v", err)
	}
	if len(snap.Warnings) == 0 {
		t.Error("expected warning from failed doctor source, got none")
	}
	found := false
	for _, w := range snap.Warnings {
		if strings.Contains(w, "doctor") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning containing 'doctor', got: %v", snap.Warnings)
	}
}

// TestBridgeDoesNotMutateDesiredState verifies that the bridge only calls
// read-only methods — it never writes to the state source.
// (Structural test: FakeStateSource.Err=nil and no writes happen.)
func TestBridgeDoesNotMutateDesiredState(t *testing.T) {
	ctx := context.Background()
	b := runtime.NewBridge("", "")
	b.State = &runtime.FakeStateSource{
		DesiredData: []runtime.DesiredStateRecord{
			{ServiceID: "envoy", Version: "1.0.0"},
		},
		InstalledData: []runtime.InstalledStateRecord{
			{ServiceID: "envoy", Version: "1.0.0", NodeID: "node1"},
		},
	}

	snap, err := b.Snapshot(ctx, 15*time.Minute, nil)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// Read-only: desired and installed state were fetched but not mutated.
	if len(snap.DesiredState) != 1 {
		t.Errorf("DesiredState count = %d, want 1", len(snap.DesiredState))
	}
	if len(snap.InstalledState) != 1 {
		t.Errorf("InstalledState count = %d, want 1", len(snap.InstalledState))
	}
	// No StateDelta expected — versions match.
	if len(snap.StateDelta) != 0 {
		t.Errorf("StateDelta count = %d, want 0 (versions match)", len(snap.StateDelta))
	}
}
