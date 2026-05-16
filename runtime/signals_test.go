package runtime_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/globulario/awareness/runtime"
)

func TestRuntimeSignals_NullAdapterDisabledShape(t *testing.T) {
	sig := runtime.RuntimeSignals{}
	if sig.ID != "" {
		t.Error("expected empty ID for zero-value RuntimeSignals")
	}
	if len(sig.DoctorFindings) != 0 {
		t.Error("expected no DoctorFindings in zero-value")
	}
	if len(sig.RepositoryStatus) != 0 {
		t.Error("expected no RepositoryStatus in zero-value")
	}
}

func TestRuntimeSignals_TypedFacts_JSONRoundTrip(t *testing.T) {
	sig := runtime.RuntimeSignals{
		ID:         "test-1",
		CapturedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		RepositoryStatus: []runtime.RepositoryStatusFact{
			{Name: "repo-1", Node: "node-1", Mode: "DEGRADED", Degraded: true, Reason: "test"},
		},
		ObjectstoreStatus: []runtime.ObjectstoreStatusFact{
			{Name: "minio-1", Node: "node-1", Topology: "DISTRIBUTED", Healthy: true},
		},
		XDSStatus: []runtime.XDSStatusFact{
			{Name: "xds-1", Node: "node-1", Healthy: true},
		},
		SystemdUnits: []runtime.SystemdUnitFact{
			{Unit: "foo.service", Node: "node-1", ActiveState: "active", SubState: "running"},
		},
	}
	data, err := json.Marshal(sig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got runtime.RuntimeSignals
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != sig.ID {
		t.Errorf("ID: got %q, want %q", got.ID, sig.ID)
	}
	if len(got.RepositoryStatus) != 1 || got.RepositoryStatus[0].Mode != "DEGRADED" {
		t.Errorf("RepositoryStatus round-trip failed: %+v", got.RepositoryStatus)
	}
	if len(got.ObjectstoreStatus) != 1 || !got.ObjectstoreStatus[0].Healthy {
		t.Errorf("ObjectstoreStatus round-trip failed: %+v", got.ObjectstoreStatus)
	}
}
