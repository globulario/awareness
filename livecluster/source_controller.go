package livecluster

import (
	"context"
	"fmt"
	"time"

	cluster_controllerpb "github.com/globulario/awareness/cluster_controllerpb"
	"google.golang.org/grpc"
)

// controllerHealthClient is the minimal subset of
// cluster_controllerpb.ClusterControllerServiceClient that the
// controller-backed collector uses. Tests stub against this narrow
// interface instead of the full ~40-method client.
type controllerHealthClient interface {
	GetClusterHealthV1(ctx context.Context, in *cluster_controllerpb.GetClusterHealthV1Request, opts ...grpc.CallOption) (*cluster_controllerpb.GetClusterHealthV1Response, error)
}

// ControllerClientFactory dials the cluster controller and returns a client
// plus a release callback. MCP and the CLI supply their own factories so
// this package stays transport-agnostic.
type ControllerClientFactory func(ctx context.Context) (client cluster_controllerpb.ClusterControllerServiceClient, release func(), err error)

// ControllerCollector adapts the controller's authoritative cluster view
// (GetClusterHealthV1) into live signals:
//
//   - Each node with a non-empty LastError surfaces as a degraded service
//     state for "node-agent" on that node.
//   - Each node whose desired_services_hash != applied_services_hash adds
//     a RuntimeConvergenceState("in_progress") for the node.
//   - Each ServiceSummary with NodesAtDesired < NodesTotal adds a
//     RuntimeConvergenceState — "stuck" when Upgrading=0, otherwise
//     "in_progress".
//
// The controller is the only source of truth for desired/applied hashes;
// the doctor's drift report sees the same data but with delay, so this
// collector is preferred for convergence signals.
type ControllerCollector struct {
	name    string
	factory ControllerClientFactory
}

// NewControllerCollector wires a controller-backed SignalCollector.
func NewControllerCollector(name string, factory ControllerClientFactory) *ControllerCollector {
	if name == "" {
		name = "controller"
	}
	return &ControllerCollector{name: name, factory: factory}
}

func (c *ControllerCollector) Name() string { return c.name }

func (c *ControllerCollector) Available(_ context.Context) bool {
	return c != nil && c.factory != nil
}

func (c *ControllerCollector) Collect(ctx context.Context, _ CollectSignalsRequest) (*SignalSourceResult, error) {
	if c == nil || c.factory == nil {
		return notConfigured(c.name), nil
	}

	client, release, err := c.factory(ctx)
	if err != nil {
		return unavailableSource(c.name, err), nil
	}
	if release != nil {
		defer release()
	}
	return c.collectWith(ctx, client)
}

// collectWith runs the controller RPC against a narrow client interface
// so tests can stub without implementing the full client.
func (c *ControllerCollector) collectWith(ctx context.Context, client controllerHealthClient) (*SignalSourceResult, error) {
	resp, err := client.GetClusterHealthV1(ctx, &cluster_controllerpb.GetClusterHealthV1Request{})
	if err != nil {
		return unavailableSource(c.name, err), nil
	}

	res := &SignalSourceResult{
		Source: SignalSourceStatus{
			Name:        c.name,
			Status:      "ok",
			CollectedAt: time.Now().Unix(),
		},
	}

	for _, n := range resp.GetNodes() {
		// Per-node convergence based on hash equality.
		desired := n.GetDesiredServicesHash()
		applied := n.GetAppliedServicesHash()
		if desired != "" && desired != applied {
			res.Convergence = append(res.Convergence, RuntimeConvergenceState{
				Component:         "node:" + n.GetNodeId(),
				DesiredState:      desired,
				InstalledState:    applied,
				RuntimeState:      applied,
				ConvergenceStatus: "in_progress",
				BlockedReason:     "desired_services_hash != applied_services_hash",
				RelatedKey:        n.GetNodeId(),
			})
		}

		// LastError on a node implies the node-agent's last apply failed.
		if msg := n.GetLastError(); msg != "" {
			res.Services = append(res.Services, ServiceLiveState{
				ServiceName: "node-agent",
				Component:   "node_agent",
				NodeID:      n.GetNodeId(),
				Status:      "running",
				Health:      "degraded",
				Readiness:   "not_ready",
				LastError:   msg,
			})
		}
	}

	for _, s := range resp.GetServices() {
		if s.GetNodesTotal() == 0 {
			continue
		}
		if s.GetNodesAtDesired() >= s.GetNodesTotal() {
			continue
		}
		status := "in_progress"
		if s.GetUpgrading() == 0 {
			status = "stuck"
		}
		res.Convergence = append(res.Convergence, RuntimeConvergenceState{
			Component:         s.GetServiceName(),
			DesiredState:      s.GetDesiredVersion(),
			ConvergenceStatus: status,
			BlockedReason: fmt.Sprintf(
				"%d/%d nodes at desired version (%d upgrading)",
				s.GetNodesAtDesired(), s.GetNodesTotal(), s.GetUpgrading(),
			),
		})
	}

	return res, nil
}
