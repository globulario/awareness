package livecluster

import (
	"context"
	"errors"
	"testing"
	"time"

	cluster_doctorpb "github.com/globulario/awareness/cluster_doctorpb"
	"google.golang.org/grpc"
)

func TestFindingToIncident_OnlyErrorAndCritical(t *testing.T) {
	cases := []struct {
		name string
		sev  cluster_doctorpb.Severity
		want bool
	}{
		{"info dropped", cluster_doctorpb.Severity_SEVERITY_INFO, false},
		{"warn dropped", cluster_doctorpb.Severity_SEVERITY_WARN, false},
		{"error surfaced", cluster_doctorpb.Severity_SEVERITY_ERROR, true},
		{"critical surfaced", cluster_doctorpb.Severity_SEVERITY_CRITICAL, true},
		{"unknown dropped", cluster_doctorpb.Severity_SEVERITY_UNKNOWN, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &cluster_doctorpb.Finding{
				FindingId: "F1",
				Severity:  tc.sev,
				Summary:   "thing broken",
				EntityRef: "globule-nuc/workflow.service",
			}
			inc := findingToIncident(f)
			if (inc != nil) != tc.want {
				t.Fatalf("want incident=%v, got %v", tc.want, inc)
			}
		})
	}
}

func TestFindingToIncident_EntityRefSplit(t *testing.T) {
	f := &cluster_doctorpb.Finding{
		FindingId: "F2",
		Severity:  cluster_doctorpb.Severity_SEVERITY_CRITICAL,
		Summary:   "scylla session unavailable",
		EntityRef: "globule-nuc/workflow.service",
		Category:  "workflow",
	}
	inc := findingToIncident(f)
	if inc == nil {
		t.Fatal("expected incident")
	}
	if inc.NodeID != "globule-nuc" {
		t.Errorf("NodeID=%q, want globule-nuc", inc.NodeID)
	}
	if inc.ServiceName != "workflow" {
		t.Errorf("ServiceName=%q, want workflow", inc.ServiceName)
	}
	if inc.Component != "workflow" {
		t.Errorf("Component=%q, want workflow", inc.Component)
	}
	if inc.Severity != "critical" {
		t.Errorf("Severity=%q, want critical", inc.Severity)
	}
	if inc.Source != "doctor" {
		t.Errorf("Source=%q, want doctor", inc.Source)
	}
}

func TestFindingToIncident_ClusterEntityRef(t *testing.T) {
	// "cluster" is the ambient scope — should not be parsed as a node.
	f := &cluster_doctorpb.Finding{
		FindingId: "F3",
		Severity:  cluster_doctorpb.Severity_SEVERITY_ERROR,
		Summary:   "dns reload stuck",
		EntityRef: "cluster",
	}
	inc := findingToIncident(f)
	if inc == nil {
		t.Fatal("expected incident")
	}
	if inc.NodeID != "" || inc.ServiceName != "" {
		t.Errorf("cluster-scoped should leave node/service empty, got node=%q svc=%q", inc.NodeID, inc.ServiceName)
	}
}

func TestDriftItemToServiceState_Categories(t *testing.T) {
	cases := []struct {
		name       string
		cat        cluster_doctorpb.DriftCategory
		wantHealth string
		wantOK     bool
	}{
		{"stopped → unhealthy", cluster_doctorpb.DriftCategory_UNIT_STOPPED, "unhealthy", true},
		{"missing → unreachable", cluster_doctorpb.DriftCategory_MISSING_UNIT_FILE, "unreachable", true},
		{"disabled → degraded", cluster_doctorpb.DriftCategory_UNIT_DISABLED, "degraded", true},
		{"endpoint_missing → unreachable", cluster_doctorpb.DriftCategory_ENDPOINT_MISSING, "unreachable", true},
		{"version_mismatch → no service state", cluster_doctorpb.DriftCategory_VERSION_MISMATCH, "", false},
		{"inventory_incomplete → no service state", cluster_doctorpb.DriftCategory_INVENTORY_INCOMPLETE, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := &cluster_doctorpb.DriftItem{
				NodeId:    "globule-nuc",
				EntityRef: "globule-nuc/workflow.service",
				Category:  tc.cat,
				Desired:   "running",
				Actual:    "stopped",
			}
			state, ok := driftItemToServiceState(item)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if state.Health != tc.wantHealth {
				t.Errorf("Health=%q, want %q", state.Health, tc.wantHealth)
			}
			if state.ServiceName != "workflow" {
				t.Errorf("ServiceName=%q, want workflow", state.ServiceName)
			}
			if state.NodeID != "globule-nuc" {
				t.Errorf("NodeID=%q, want globule-nuc", state.NodeID)
			}
		})
	}
}

func TestDriftItemToServiceState_EmptyServiceDropped(t *testing.T) {
	// EntityRef "cluster" splits to empty service → should be dropped.
	item := &cluster_doctorpb.DriftItem{
		NodeId:    "globule-nuc",
		EntityRef: "",
		Category:  cluster_doctorpb.DriftCategory_UNIT_STOPPED,
	}
	if _, ok := driftItemToServiceState(item); ok {
		t.Error("empty service should be dropped")
	}
}

func TestDriftItemToConvergence_Categories(t *testing.T) {
	cases := []struct {
		name   string
		cat    cluster_doctorpb.DriftCategory
		wantOK bool
	}{
		{"version → diverged", cluster_doctorpb.DriftCategory_VERSION_MISMATCH, true},
		{"hash → diverged", cluster_doctorpb.DriftCategory_STATE_HASH_MISMATCH, true},
		{"unit_stopped not convergence", cluster_doctorpb.DriftCategory_UNIT_STOPPED, false},
		{"unknown not convergence", cluster_doctorpb.DriftCategory_DRIFT_UNKNOWN, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := &cluster_doctorpb.DriftItem{
				NodeId:    "globule-nuc",
				EntityRef: "globule-nuc/workflow.service",
				Category:  tc.cat,
				Desired:   "1.2.30",
				Actual:    "1.2.29",
			}
			cv, ok := driftItemToConvergence(item)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v, want %v", ok, tc.wantOK)
			}
			if ok && cv.ConvergenceStatus != "diverged" {
				t.Errorf("ConvergenceStatus=%q, want diverged", cv.ConvergenceStatus)
			}
		})
	}
}

func TestSplitEntityRef(t *testing.T) {
	cases := []struct {
		ref      string
		wantNode string
		wantSvc  string
	}{
		{"globule-nuc/workflow.service", "globule-nuc", "workflow"},
		{"workflow", "", "workflow"},
		{"workflow.service", "", "workflow"},
		{"cluster", "", ""},
		{"", "", ""},
		{"globule-nuc", "globule-nuc", ""}, // hostname-shaped lone token
	}
	for _, tc := range cases {
		_, svc, node := splitEntityRef(tc.ref, "")
		if node != tc.wantNode || svc != tc.wantSvc {
			t.Errorf("splitEntityRef(%q) = (svc=%q, node=%q), want (svc=%q, node=%q)",
				tc.ref, svc, node, tc.wantSvc, tc.wantNode)
		}
	}
}

func TestDoctorCollector_UnconfiguredFactory(t *testing.T) {
	c := NewDoctorCollector("doctor", nil)
	if c.Available(context.Background()) {
		t.Error("Available should be false when factory is nil")
	}
	res, err := c.Collect(context.Background(), CollectSignalsRequest{})
	if err != nil {
		t.Fatalf("Collect should not error on nil factory, got %v", err)
	}
	if res.Source.Status != "not_configured" {
		t.Errorf("Source.Status=%q, want not_configured", res.Source.Status)
	}
}

func TestDoctorCollector_DialError(t *testing.T) {
	c := NewDoctorCollector("doctor", func(ctx context.Context) (cluster_doctorpb.ClusterDoctorServiceClient, func(), error) {
		return nil, nil, errors.New("dial: connection refused")
	})
	res, err := c.Collect(context.Background(), CollectSignalsRequest{})
	if err != nil {
		t.Fatalf("Collect should not error on dial failure, got %v", err)
	}
	if res.Source.Status != "unavailable" {
		t.Errorf("Source.Status=%q, want unavailable", res.Source.Status)
	}
	if res.Source.Message == "" {
		t.Error("Source.Message should describe the dial failure")
	}
}

func TestDoctorCollector_BothRPCsFail(t *testing.T) {
	stub := &stubDoctorClient{
		reportErr: errors.New("report unavailable"),
		driftErr:  errors.New("drift unavailable"),
	}
	c := NewDoctorCollector("doctor", nil)
	res := c.collectWith(context.Background(), stub)
	if res.Source.Status != "unavailable" {
		t.Errorf("Source.Status=%q, want unavailable when both RPCs fail", res.Source.Status)
	}
}

func TestDoctorCollector_PartialDegrade(t *testing.T) {
	stub := &stubDoctorClient{
		reportResp: &cluster_doctorpb.ClusterReport{
			Findings: []*cluster_doctorpb.Finding{
				{
					FindingId: "F1",
					Severity:  cluster_doctorpb.Severity_SEVERITY_CRITICAL,
					Summary:   "scylla raft stalled",
					EntityRef: "globule-nuc/scylla.service",
				},
			},
		},
		driftErr: errors.New("drift unavailable"),
	}
	c := NewDoctorCollector("doctor", nil)
	res := c.collectWith(context.Background(), stub)
	if res.Source.Status != "degraded" {
		t.Errorf("Source.Status=%q, want degraded", res.Source.Status)
	}
	if len(res.Incidents) != 1 {
		t.Fatalf("Incidents=%d, want 1", len(res.Incidents))
	}
	if res.Incidents[0].ServiceName != "scylla" {
		t.Errorf("ServiceName=%q, want scylla", res.Incidents[0].ServiceName)
	}
}

func TestDoctorCollector_DriftMappedToBothStateAndConvergence(t *testing.T) {
	// One drift item per category — verify both legs of mapping populate.
	stub := &stubDoctorClient{
		reportResp: &cluster_doctorpb.ClusterReport{},
		driftResp: &cluster_doctorpb.DriftReport{
			Items: []*cluster_doctorpb.DriftItem{
				{
					NodeId:    "globule-nuc",
					EntityRef: "globule-nuc/workflow.service",
					Category:  cluster_doctorpb.DriftCategory_UNIT_STOPPED,
					Desired:   "running",
					Actual:    "inactive",
				},
				{
					NodeId:    "globule-nuc",
					EntityRef: "globule-nuc/repository.service",
					Category:  cluster_doctorpb.DriftCategory_VERSION_MISMATCH,
					Desired:   "1.2.30",
					Actual:    "1.2.29",
				},
			},
		},
	}
	c := NewDoctorCollector("doctor", nil)
	res := c.collectWith(context.Background(), stub)
	if res.Source.Status != "ok" {
		t.Errorf("Source.Status=%q, want ok", res.Source.Status)
	}
	if len(res.Services) != 1 {
		t.Errorf("Services=%d, want 1 (unit_stopped should produce one)", len(res.Services))
	}
	if len(res.Convergence) != 1 {
		t.Errorf("Convergence=%d, want 1 (version_mismatch should produce one)", len(res.Convergence))
	}
}

func TestDoctorCollector_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stub := &stubDoctorClient{honorCtx: true}
	c := NewDoctorCollector("doctor", nil)
	start := time.Now()
	res := c.collectWith(ctx, stub)
	if res == nil {
		t.Fatal("expected non-nil result")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("collectWith took %v with cancelled ctx — should return immediately", elapsed)
	}
}

// stubDoctorClient implements doctorReportClient for tests.
type stubDoctorClient struct {
	reportResp *cluster_doctorpb.ClusterReport
	reportErr  error
	driftResp  *cluster_doctorpb.DriftReport
	driftErr   error
	honorCtx   bool
}

func (s *stubDoctorClient) GetClusterReport(ctx context.Context, _ *cluster_doctorpb.ClusterReportRequest, _ ...grpc.CallOption) (*cluster_doctorpb.ClusterReport, error) {
	if s.honorCtx {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return s.reportResp, s.reportErr
}

func (s *stubDoctorClient) GetDriftReport(ctx context.Context, _ *cluster_doctorpb.DriftReportRequest, _ ...grpc.CallOption) (*cluster_doctorpb.DriftReport, error) {
	if s.honorCtx {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return s.driftResp, s.driftErr
}
