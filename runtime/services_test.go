package runtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/globulario/awareness/runtime"
)

// TestServiceStartLimitHitInSnapshot verifies that when a service has
// START_LIMIT_HIT state, a runtime snapshot collected from a bridge
// containing that service properly includes it.
func TestServiceStartLimitHitInSnapshot(t *testing.T) {
	ctx := context.Background()
	b := runtime.NewBridge("node1", "cluster1")
	b.Services = &runtime.FakeServiceStatusSource{
		Data: []runtime.ServiceStatus{
			{
				ServiceID:    "envoy",
				NodeID:       "node1",
				Version:      "1.0.5",
				State:        "START_LIMIT_HIT",
				RestartCount: 5,
				LastError:    "systemd start-limit exceeded",
			},
		},
	}

	snap, err := b.Snapshot(ctx, 15*time.Minute, nil)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if len(snap.RuntimeServices) != 1 {
		t.Fatalf("RuntimeServices count = %d, want 1", len(snap.RuntimeServices))
	}
	svc := snap.RuntimeServices[0]
	if svc.State != "START_LIMIT_HIT" {
		t.Errorf("State = %q, want START_LIMIT_HIT", svc.State)
	}
	if svc.RestartCount != 5 {
		t.Errorf("RestartCount = %d, want 5", svc.RestartCount)
	}
}

// TestMultipleServiceStatusesCollected verifies that multiple service statuses
// are all collected into the snapshot.
func TestMultipleServiceStatusesCollected(t *testing.T) {
	ctx := context.Background()
	b := runtime.NewBridge("", "")
	b.Services = &runtime.FakeServiceStatusSource{
		Data: []runtime.ServiceStatus{
			{ServiceID: "envoy", State: "RUNNING"},
			{ServiceID: "controller", State: "RUNNING"},
			{ServiceID: "node-agent", State: "FAILED"},
		},
	}

	snap, err := b.Snapshot(ctx, 15*time.Minute, nil)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.RuntimeServices) != 3 {
		t.Errorf("RuntimeServices count = %d, want 3", len(snap.RuntimeServices))
	}
}

// TestServiceStatusSourceError verifies that a service-status source error
// adds a warning instead of failing Snapshot.
func TestServiceStatusSourceError(t *testing.T) {
	ctx := context.Background()
	b := runtime.NewBridge("", "")
	b.Services = &runtime.FakeServiceStatusSource{
		Err: context.DeadlineExceeded,
	}

	snap, err := b.Snapshot(ctx, 15*time.Minute, nil)
	if err != nil {
		t.Fatalf("Snapshot should not fail on source error: %v", err)
	}
	if len(snap.Warnings) == 0 {
		t.Error("expected warning from service-status source error")
	}
}
