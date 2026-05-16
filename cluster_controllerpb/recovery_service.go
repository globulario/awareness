package cluster_controllerpb

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ── Request / Response types ─────────────────────────────────────────────────

// StartNodeFullReseedRecoveryRequest triggers the node.recover.full_reseed
// workflow. Must be operator-initiated (admin RBAC).
type StartNodeFullReseedRecoveryRequest struct {
	NodeID              string `json:"node_id"`
	Reason              string `json:"reason"`
	ExactReplayRequired bool   `json:"exact_replay_required,omitempty"`
	Force               bool   `json:"force,omitempty"`
	DryRun              bool   `json:"dry_run,omitempty"`
	// Optional: reuse an existing snapshot instead of capturing a new one.
	SnapshotID string `json:"snapshot_id,omitempty"`
	Note       string `json:"note,omitempty"`
}

// StartNodeFullReseedRecoveryResponse returns the workflow run ID and planned order.
type StartNodeFullReseedRecoveryResponse struct {
	WorkflowID       string                    `json:"workflow_id,omitempty"`
	SnapshotID       string                    `json:"snapshot_id,omitempty"`
	State            string                    `json:"state,omitempty"`
	Warnings         []string                  `json:"warnings,omitempty"`
	PlannedArtifacts []PlannedRecoveryArtifact `json:"planned_artifacts,omitempty"`
}

// GetNodeRecoveryStatusRequest queries the current recovery state.
type GetNodeRecoveryStatusRequest struct {
	NodeID string `json:"node_id"`
}

// GetNodeRecoveryStatusResponse returns the full recovery picture for a node.
type GetNodeRecoveryStatusResponse struct {
	Recovery *NodeRecoveryState           `json:"recovery,omitempty"`
	Snapshot *NodeRecoverySnapshot        `json:"snapshot,omitempty"`
	Results  []NodeRecoveryArtifactResult `json:"results,omitempty"`
}

// CreateNodeRecoverySnapshotRequest creates a standalone snapshot without
// starting a recovery workflow. Useful for pre-maintenance captures.
type CreateNodeRecoverySnapshotRequest struct {
	NodeID string `json:"node_id"`
	Reason string `json:"reason,omitempty"`
}

// CreateNodeRecoverySnapshotResponse returns the created snapshot.
type CreateNodeRecoverySnapshotResponse struct {
	SnapshotID string                `json:"snapshot_id"`
	Snapshot   *NodeRecoverySnapshot `json:"snapshot,omitempty"`
}

// AckNodeReprovisionedRequest is called by the operator (or installer callback)
// to signal that the physical/virtual machine has been wiped and the OS
// reinstalled. The workflow resumes from AWAIT_REPROVISION.
type AckNodeReprovisionedRequest struct {
	WorkflowID string `json:"workflow_id"`
	NodeID     string `json:"node_id"`
	Note       string `json:"note,omitempty"`
}

// ── Service interface ─────────────────────────────────────────────────────────

// NodeRecoveryServiceServer is implemented by the cluster controller server.
type NodeRecoveryServiceServer interface {
	StartNodeFullReseedRecovery(context.Context, *StartNodeFullReseedRecoveryRequest) (*StartNodeFullReseedRecoveryResponse, error)
	GetNodeRecoveryStatus(context.Context, *GetNodeRecoveryStatusRequest) (*GetNodeRecoveryStatusResponse, error)
	CreateNodeRecoverySnapshot(context.Context, *CreateNodeRecoverySnapshotRequest) (*CreateNodeRecoverySnapshotResponse, error)
	AckNodeReprovisioned(context.Context, *AckNodeReprovisionedRequest) (*emptypb.Empty, error)
}

// UnimplementedNodeRecoveryServiceServer returns Unimplemented for all methods.
type UnimplementedNodeRecoveryServiceServer struct{}

func (UnimplementedNodeRecoveryServiceServer) StartNodeFullReseedRecovery(_ context.Context, _ *StartNodeFullReseedRecoveryRequest) (*StartNodeFullReseedRecoveryResponse, error) {
	return nil, status.Error(codes.Unimplemented, "StartNodeFullReseedRecovery not implemented")
}

func (UnimplementedNodeRecoveryServiceServer) GetNodeRecoveryStatus(_ context.Context, _ *GetNodeRecoveryStatusRequest) (*GetNodeRecoveryStatusResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetNodeRecoveryStatus not implemented")
}

func (UnimplementedNodeRecoveryServiceServer) CreateNodeRecoverySnapshot(_ context.Context, _ *CreateNodeRecoverySnapshotRequest) (*CreateNodeRecoverySnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "CreateNodeRecoverySnapshot not implemented")
}

func (UnimplementedNodeRecoveryServiceServer) AckNodeReprovisioned(_ context.Context, _ *AckNodeReprovisionedRequest) (*emptypb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "AckNodeReprovisioned not implemented")
}

// ── gRPC service descriptor (hand-written, follows resources_service.go pattern) ──

const NodeRecoveryServiceName = "cluster_controller.NodeRecoveryService"

// RegisterNodeRecoveryServiceServer registers NodeRecoveryService with the gRPC server.
func RegisterNodeRecoveryServiceServer(s *grpc.Server, srv NodeRecoveryServiceServer) {
	s.RegisterService(&nodeRecoveryServiceDesc, srv)
}

var nodeRecoveryServiceDesc = grpc.ServiceDesc{
	ServiceName: NodeRecoveryServiceName,
	HandlerType: (*NodeRecoveryServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "StartNodeFullReseedRecovery",
			Handler:    _NodeRecoveryService_StartNodeFullReseedRecovery_Handler,
		},
		{
			MethodName: "GetNodeRecoveryStatus",
			Handler:    _NodeRecoveryService_GetNodeRecoveryStatus_Handler,
		},
		{
			MethodName: "CreateNodeRecoverySnapshot",
			Handler:    _NodeRecoveryService_CreateNodeRecoverySnapshot_Handler,
		},
		{
			MethodName: "AckNodeReprovisioned",
			Handler:    _NodeRecoveryService_AckNodeReprovisioned_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "cluster_controller.proto",
}

func _NodeRecoveryService_StartNodeFullReseedRecovery_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StartNodeFullReseedRecoveryRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NodeRecoveryServiceServer).StartNodeFullReseedRecovery(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + NodeRecoveryServiceName + "/StartNodeFullReseedRecovery"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NodeRecoveryServiceServer).StartNodeFullReseedRecovery(ctx, req.(*StartNodeFullReseedRecoveryRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _NodeRecoveryService_GetNodeRecoveryStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetNodeRecoveryStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NodeRecoveryServiceServer).GetNodeRecoveryStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + NodeRecoveryServiceName + "/GetNodeRecoveryStatus"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NodeRecoveryServiceServer).GetNodeRecoveryStatus(ctx, req.(*GetNodeRecoveryStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _NodeRecoveryService_CreateNodeRecoverySnapshot_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateNodeRecoverySnapshotRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NodeRecoveryServiceServer).CreateNodeRecoverySnapshot(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + NodeRecoveryServiceName + "/CreateNodeRecoverySnapshot"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NodeRecoveryServiceServer).CreateNodeRecoverySnapshot(ctx, req.(*CreateNodeRecoverySnapshotRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _NodeRecoveryService_AckNodeReprovisioned_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AckNodeReprovisionedRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NodeRecoveryServiceServer).AckNodeReprovisioned(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + NodeRecoveryServiceName + "/AckNodeReprovisioned"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NodeRecoveryServiceServer).AckNodeReprovisioned(ctx, req.(*AckNodeReprovisionedRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// ── Client ────────────────────────────────────────────────────────────────────

// NodeRecoveryServiceClient is the client API for NodeRecoveryService.
type NodeRecoveryServiceClient interface {
	StartNodeFullReseedRecovery(ctx context.Context, in *StartNodeFullReseedRecoveryRequest, opts ...grpc.CallOption) (*StartNodeFullReseedRecoveryResponse, error)
	GetNodeRecoveryStatus(ctx context.Context, in *GetNodeRecoveryStatusRequest, opts ...grpc.CallOption) (*GetNodeRecoveryStatusResponse, error)
	CreateNodeRecoverySnapshot(ctx context.Context, in *CreateNodeRecoverySnapshotRequest, opts ...grpc.CallOption) (*CreateNodeRecoverySnapshotResponse, error)
	AckNodeReprovisioned(ctx context.Context, in *AckNodeReprovisionedRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
}

type nodeRecoveryServiceClient struct {
	cc grpc.ClientConnInterface
}

// NewNodeRecoveryServiceClient creates a new NodeRecoveryServiceClient.
func NewNodeRecoveryServiceClient(cc grpc.ClientConnInterface) NodeRecoveryServiceClient {
	return &nodeRecoveryServiceClient{cc}
}

func (c *nodeRecoveryServiceClient) StartNodeFullReseedRecovery(ctx context.Context, in *StartNodeFullReseedRecoveryRequest, opts ...grpc.CallOption) (*StartNodeFullReseedRecoveryResponse, error) {
	out := new(StartNodeFullReseedRecoveryResponse)
	err := c.cc.Invoke(ctx, "/"+NodeRecoveryServiceName+"/StartNodeFullReseedRecovery", in, out, opts...)
	return out, err
}

func (c *nodeRecoveryServiceClient) GetNodeRecoveryStatus(ctx context.Context, in *GetNodeRecoveryStatusRequest, opts ...grpc.CallOption) (*GetNodeRecoveryStatusResponse, error) {
	out := new(GetNodeRecoveryStatusResponse)
	err := c.cc.Invoke(ctx, "/"+NodeRecoveryServiceName+"/GetNodeRecoveryStatus", in, out, opts...)
	return out, err
}

func (c *nodeRecoveryServiceClient) CreateNodeRecoverySnapshot(ctx context.Context, in *CreateNodeRecoverySnapshotRequest, opts ...grpc.CallOption) (*CreateNodeRecoverySnapshotResponse, error) {
	out := new(CreateNodeRecoverySnapshotResponse)
	err := c.cc.Invoke(ctx, "/"+NodeRecoveryServiceName+"/CreateNodeRecoverySnapshot", in, out, opts...)
	return out, err
}

func (c *nodeRecoveryServiceClient) AckNodeReprovisioned(ctx context.Context, in *AckNodeReprovisionedRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/"+NodeRecoveryServiceName+"/AckNodeReprovisioned", in, out, opts...)
	return out, err
}
