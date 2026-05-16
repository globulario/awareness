package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeSourceWithInfo is a fake source that implements sourceIdentifier.
type fakeSourceWithInfo struct {
	backend string
	isNoop  bool
}

func (f *fakeSourceWithInfo) SourceInfo() (string, bool) { return f.backend, f.isNoop }

func TestSourceHealthFor_Noop(t *testing.T) {
	noop := NoopDoctorSource{}
	h := sourceHealthFor(SourceDoctor, noop, nil)

	if !h.EmptyDueToNoop {
		t.Error("expected EmptyDueToNoop=true for noop source")
	}
	if h.Healthy {
		t.Error("expected Healthy=false for noop source")
	}
	if h.Backend != "noop" {
		t.Errorf("expected backend=noop, got %q", h.Backend)
	}
	if h.Source != SourceDoctor {
		t.Errorf("expected source=doctor, got %q", h.Source)
	}
	if h.CollectedAt == "" {
		t.Error("CollectedAt should be set")
	}
}

func TestSourceHealthFor_RealSource_Success(t *testing.T) {
	fake := &fakeSourceWithInfo{backend: "cluster_doctor.grpc", isNoop: false}
	h := sourceHealthFor(SourceDoctor, fake, nil)

	if h.EmptyDueToNoop {
		t.Error("expected EmptyDueToNoop=false for real source")
	}
	if !h.Healthy {
		t.Error("expected Healthy=true on success")
	}
	if h.LastError != "" {
		t.Errorf("expected no error, got %q", h.LastError)
	}
	if h.Backend != "cluster_doctor.grpc" {
		t.Errorf("expected backend=cluster_doctor.grpc, got %q", h.Backend)
	}
}

func TestSourceHealthFor_RealSource_Error(t *testing.T) {
	fake := &fakeSourceWithInfo{backend: "cluster_doctor.grpc", isNoop: false}
	err := errors.New("connection refused")
	h := sourceHealthFor(SourceDoctor, fake, err)

	if h.EmptyDueToNoop {
		t.Error("expected EmptyDueToNoop=false for real source with error")
	}
	if h.Healthy {
		t.Error("expected Healthy=false on error")
	}
	if h.LastError != "connection refused" {
		t.Errorf("expected LastError=connection refused, got %q", h.LastError)
	}
}

func TestSourceHealthFor_UnknownSource(t *testing.T) {
	// A source that does not implement sourceIdentifier.
	type unknownSrc struct{}
	h := sourceHealthFor(SourceMetrics, unknownSrc{}, nil)

	if h.EmptyDueToNoop {
		t.Error("unknown source should not be classified as noop")
	}
	if !h.Healthy {
		t.Error("should be healthy when no error and no noop")
	}
	if h.Backend != "unknown" {
		t.Errorf("expected backend=unknown, got %q", h.Backend)
	}
}

func TestAllNoopSourcesReportNoop(t *testing.T) {
	ctx := context.Background()
	b := NewBridge("test-node", "")
	snap, err := b.Snapshot(ctx, 15*time.Minute, nil)
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}

	if len(snap.SourceHealth) == 0 {
		t.Fatal("expected SourceHealth to be populated")
	}

	noopCount := 0
	for _, sh := range snap.SourceHealth {
		if sh.EmptyDueToNoop {
			noopCount++
		}
	}
	// All 10 sources should be noop when using NewBridge defaults.
	if noopCount != 10 {
		t.Errorf("expected 10 noop sources, got %d", noopCount)
	}
}

func TestFakeSourceWithError_DoesNotBlockOthers(t *testing.T) {
	ctx := context.Background()
	b := NewBridge("test-node", "")
	b.Doctor = &FakeDoctorSource{Err: errors.New("doctor down")}
	// All other sources remain noop.

	snap, err := b.Snapshot(ctx, 15*time.Minute, nil)
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}

	// Find doctor source health.
	var doctorHealth *SourceHealth
	for i := range snap.SourceHealth {
		if snap.SourceHealth[i].Source == SourceDoctor {
			doctorHealth = &snap.SourceHealth[i]
			break
		}
	}
	if doctorHealth == nil {
		t.Fatal("doctor source health not found")
	}
	if doctorHealth.Healthy {
		t.Error("expected doctor to be unhealthy")
	}
	if doctorHealth.LastError == "" {
		t.Error("expected LastError to be set")
	}

	// Verify other sources are still present.
	if len(snap.SourceHealth) != 10 {
		t.Errorf("expected 10 source health records, got %d", len(snap.SourceHealth))
	}
}

func TestNewHealthySource(t *testing.T) {
	h := newHealthySource(SourceMetrics, "prometheus.http")
	if !h.Healthy {
		t.Error("expected Healthy=true")
	}
	if h.EmptyDueToNoop {
		t.Error("expected EmptyDueToNoop=false")
	}
	if h.Backend != "prometheus.http" {
		t.Errorf("wrong backend: %q", h.Backend)
	}
}

func TestNewErrSource(t *testing.T) {
	h := newErrSource(SourceWorkflows, "workflow.grpc", errors.New("timeout"))
	if h.Healthy {
		t.Error("expected Healthy=false")
	}
	if h.LastError != "timeout" {
		t.Errorf("expected LastError=timeout, got %q", h.LastError)
	}
}

func TestNewNoopSource(t *testing.T) {
	h := newNoopSource(SourceEvents)
	if !h.EmptyDueToNoop {
		t.Error("expected EmptyDueToNoop=true")
	}
	if h.Healthy {
		t.Error("expected Healthy=false")
	}
	if h.Backend != "noop" {
		t.Errorf("expected backend=noop, got %q", h.Backend)
	}
}
