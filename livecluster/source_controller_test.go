package livecluster

import (
	"context"
	"errors"
	"testing"

	cluster_controllerpb "github.com/globulario/awareness/cluster_controllerpb"
	"google.golang.org/grpc"
)

func TestControllerCollector_HashMismatchProducesConvergence(t *testing.T) {
	stub := &stubControllerClient{
		resp: &cluster_controllerpb.GetClusterHealthV1Response{
			Nodes: []*cluster_controllerpb.NodeHealth{
				{
					NodeId:              "globule-nuc",
					DesiredServicesHash: "abc",
					AppliedServicesHash: "abc", // converged
				},
				{
					NodeId:              "globule-dell",
					DesiredServicesHash: "abc",
					AppliedServicesHash: "xyz", // drifted
				},
			},
		},
	}
	c := NewControllerCollector("controller", nil)
	res, err := c.collectWith(context.Background(), stub)
	if err != nil {
		t.Fatalf("collectWith: %v", err)
	}
	if res.Source.Status != "ok" {
		t.Errorf("Source.Status=%q, want ok", res.Source.Status)
	}
	if len(res.Convergence) != 1 {
		t.Fatalf("Convergence=%d, want 1 (only dell is drifted)", len(res.Convergence))
	}
	if res.Convergence[0].Component != "node:globule-dell" {
		t.Errorf("Component=%q, want node:globule-dell", res.Convergence[0].Component)
	}
	if res.Convergence[0].ConvergenceStatus != "in_progress" {
		t.Errorf("ConvergenceStatus=%q, want in_progress", res.Convergence[0].ConvergenceStatus)
	}
}

func TestControllerCollector_NodeLastErrorSurfacesAsServiceState(t *testing.T) {
	stub := &stubControllerClient{
		resp: &cluster_controllerpb.GetClusterHealthV1Response{
			Nodes: []*cluster_controllerpb.NodeHealth{
				{
					NodeId:    "globule-dell",
					LastError: "apply failed: checksum mismatch on workflow-service",
				},
			},
		},
	}
	c := NewControllerCollector("controller", nil)
	res, err := c.collectWith(context.Background(), stub)
	if err != nil {
		t.Fatalf("collectWith: %v", err)
	}
	if len(res.Services) != 1 {
		t.Fatalf("Services=%d, want 1", len(res.Services))
	}
	s := res.Services[0]
	if s.ServiceName != "node-agent" {
		t.Errorf("ServiceName=%q, want node-agent", s.ServiceName)
	}
	if s.NodeID != "globule-dell" {
		t.Errorf("NodeID=%q, want globule-dell", s.NodeID)
	}
	if s.Health != "degraded" {
		t.Errorf("Health=%q, want degraded", s.Health)
	}
	if s.LastError == "" {
		t.Error("LastError should propagate from controller")
	}
}

func TestControllerCollector_ServiceSummaryStuckVsInProgress(t *testing.T) {
	stub := &stubControllerClient{
		resp: &cluster_controllerpb.GetClusterHealthV1Response{
			Services: []*cluster_controllerpb.ServiceSummary{
				{
					ServiceName:    "fully-converged",
					NodesAtDesired: 3,
					NodesTotal:     3,
					Upgrading:      0,
				},
				{
					ServiceName:    "still-rolling",
					NodesAtDesired: 1,
					NodesTotal:     3,
					Upgrading:      2,
				},
				{
					ServiceName:    "stuck-mid-rollout",
					NodesAtDesired: 1,
					NodesTotal:     3,
					Upgrading:      0,
				},
			},
		},
	}
	c := NewControllerCollector("controller", nil)
	res, err := c.collectWith(context.Background(), stub)
	if err != nil {
		t.Fatalf("collectWith: %v", err)
	}
	byName := map[string]string{}
	for _, cv := range res.Convergence {
		byName[cv.Component] = cv.ConvergenceStatus
	}
	if _, ok := byName["fully-converged"]; ok {
		t.Error("fully-converged should not produce a convergence entry")
	}
	if byName["still-rolling"] != "in_progress" {
		t.Errorf("still-rolling status=%q, want in_progress", byName["still-rolling"])
	}
	if byName["stuck-mid-rollout"] != "stuck" {
		t.Errorf("stuck-mid-rollout status=%q, want stuck", byName["stuck-mid-rollout"])
	}
}

func TestControllerCollector_RPCError(t *testing.T) {
	stub := &stubControllerClient{err: errors.New("controller unavailable")}
	c := NewControllerCollector("controller", nil)
	res, err := c.collectWith(context.Background(), stub)
	if err != nil {
		t.Fatalf("collectWith should not return error, got %v", err)
	}
	if res.Source.Status != "unavailable" {
		t.Errorf("Source.Status=%q, want unavailable", res.Source.Status)
	}
}

func TestControllerCollector_DialError(t *testing.T) {
	c := NewControllerCollector("controller", func(ctx context.Context) (cluster_controllerpb.ClusterControllerServiceClient, func(), error) {
		return nil, nil, errors.New("dial: connection refused")
	})
	res, err := c.Collect(context.Background(), CollectSignalsRequest{})
	if err != nil {
		t.Fatalf("Collect should not error on dial failure, got %v", err)
	}
	if res.Source.Status != "unavailable" {
		t.Errorf("Source.Status=%q, want unavailable", res.Source.Status)
	}
}

func TestControllerCollector_UnconfiguredFactory(t *testing.T) {
	c := NewControllerCollector("controller", nil)
	if c.Available(context.Background()) {
		t.Error("Available should be false when factory is nil")
	}
	res, err := c.Collect(context.Background(), CollectSignalsRequest{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if res.Source.Status != "not_configured" {
		t.Errorf("Source.Status=%q, want not_configured", res.Source.Status)
	}
}

// stubControllerClient implements controllerHealthClient for tests.
type stubControllerClient struct {
	resp *cluster_controllerpb.GetClusterHealthV1Response
	err  error
}

func (s *stubControllerClient) GetClusterHealthV1(_ context.Context, _ *cluster_controllerpb.GetClusterHealthV1Request, _ ...grpc.CallOption) (*cluster_controllerpb.GetClusterHealthV1Response, error) {
	return s.resp, s.err
}
