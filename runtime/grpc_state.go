package runtime

import (
	"context"
	"fmt"

	cluster_controllerpb "github.com/globulario/awareness/cluster_controllerpb"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// GrpcStateSource pulls desired and installed state from the cluster_controller.
// DesiredState: calls GetDesiredState (authoritative).
// InstalledState: derived from GetClusterHealthV1 per-node installed_versions map.
type GrpcStateSource struct {
	cfg       GrpcSourceConfig
	transport string
	conn      *grpc.ClientConn
	client    cluster_controllerpb.ClusterControllerServiceClient
}

// NewGrpcStateSource dials the cluster_controller service using the provided config.
func NewGrpcStateSource(cfg GrpcSourceConfig) (*GrpcStateSource, error) {
	if cfg.Addr == "" {
		return nil, fmt.Errorf("state source: addr is empty")
	}
	opts, transport, err := cfg.dialOptions()
	if err != nil {
		return nil, fmt.Errorf("state source: dial options: %w", err)
	}
	conn, err := grpc.NewClient(cfg.Addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("state source: dial %s: %w", cfg.Addr, err)
	}
	return &GrpcStateSource{
		cfg:       cfg,
		transport: transport,
		conn:      conn,
		client:    cluster_controllerpb.NewClusterControllerServiceClient(conn),
	}, nil
}

// Close releases the gRPC connection.
func (s *GrpcStateSource) Close() { _ = s.conn.Close() }

// SourceInfo implements sourceIdentifier.
func (s *GrpcStateSource) SourceInfo() (string, bool) { return "cluster_controller.grpc", false }

// Transport implements transportReporter.
func (s *GrpcStateSource) Transport() string { return s.transport }

// DesiredState calls GetDesiredState and returns the list of desired services.
func (s *GrpcStateSource) DesiredState(ctx context.Context) ([]DesiredStateRecord, error) {
	resp, err := s.client.GetDesiredState(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("GetDesiredState: %w", err)
	}
	out := make([]DesiredStateRecord, 0, len(resp.GetServices()))
	for _, svc := range resp.GetServices() {
		out = append(out, DesiredStateRecord{
			ServiceID: svc.GetServiceId(),
			Version:   svc.GetVersion(),
			Phase:     svc.GetStatus(),
		})
	}
	return out, nil
}

// InstalledState derives installed state from GetClusterHealthV1 per-node data.
func (s *GrpcStateSource) InstalledState(ctx context.Context) ([]InstalledStateRecord, error) {
	resp, err := s.client.GetClusterHealthV1(ctx, &cluster_controllerpb.GetClusterHealthV1Request{})
	if err != nil {
		return nil, fmt.Errorf("GetClusterHealthV1: %w", err)
	}
	var out []InstalledStateRecord
	for _, node := range resp.GetNodes() {
		for svcName, version := range node.GetInstalledVersions() {
			buildID := node.GetInstalledBuildIds()[svcName]
			out = append(out, InstalledStateRecord{
				ServiceID: svcName,
				Version:   version,
				BuildID:   buildID,
				NodeID:    node.GetNodeId(),
				Status:    "INSTALLED",
			})
		}
	}
	return out, nil
}
