package cluster_controllerpb

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ApplyClusterNetworkRequest struct {
	Object *ClusterNetwork
}

type ApplyServiceDesiredVersionRequest struct {
	Object *ServiceDesiredVersion
}

type DeleteServiceDesiredVersionRequest struct {
	Name string
}

type GetClusterNetworkRequest struct{}

type ListServiceDesiredVersionsRequest struct{}

type ListServiceDesiredVersionsResponse struct {
	Items []*ServiceDesiredVersion
}

// ServiceRelease CRUD request/response types.

type ApplyServiceReleaseRequest struct {
	Object *ServiceRelease
}

type GetServiceReleaseRequest struct {
	Name string
}

type ListServiceReleasesRequest struct{}

type ListServiceReleasesResponse struct {
	Items []*ServiceRelease
}

type DeleteServiceReleaseRequest struct {
	Name string
}

// ApplicationRelease CRUD request/response types.

type ApplyApplicationReleaseRequest struct {
	Object *ApplicationRelease
}

type GetApplicationReleaseRequest struct {
	Name string
}

type ListApplicationReleasesRequest struct{}

type ListApplicationReleasesResponse struct {
	Items []*ApplicationRelease
}

type DeleteApplicationReleaseRequest struct {
	Name string
}

// InfrastructureRelease CRUD request/response types.

type ApplyInfrastructureReleaseRequest struct {
	Object *InfrastructureRelease
}

type GetInfrastructureReleaseRequest struct {
	Name string
}

type ListInfrastructureReleasesRequest struct{}

type ListInfrastructureReleasesResponse struct {
	Items []*InfrastructureRelease
}

type DeleteInfrastructureReleaseRequest struct {
	Name string
}

type WatchRequest struct {
	Type                string
	Prefix              string
	FromResourceVersion string
	IncludeExisting     bool
}

func (x *ApplyClusterNetworkRequest) GetObject() *ClusterNetwork {
	if x != nil {
		return x.Object
	}
	return nil
}

func (x *ApplyServiceDesiredVersionRequest) GetObject() *ServiceDesiredVersion {
	if x != nil {
		return x.Object
	}
	return nil
}

func (x *DeleteServiceDesiredVersionRequest) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *ApplyServiceReleaseRequest) GetObject() *ServiceRelease {
	if x != nil {
		return x.Object
	}
	return nil
}

func (x *GetServiceReleaseRequest) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *DeleteServiceReleaseRequest) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *ApplyApplicationReleaseRequest) GetObject() *ApplicationRelease {
	if x != nil {
		return x.Object
	}
	return nil
}

func (x *GetApplicationReleaseRequest) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *DeleteApplicationReleaseRequest) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *ApplyInfrastructureReleaseRequest) GetObject() *InfrastructureRelease {
	if x != nil {
		return x.Object
	}
	return nil
}

func (x *GetInfrastructureReleaseRequest) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *DeleteInfrastructureReleaseRequest) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *WatchRequest) GetType() string {
	if x != nil {
		return x.Type
	}
	return ""
}

func (x *WatchRequest) GetPrefix() string {
	if x != nil {
		return x.Prefix
	}
	return ""
}

func (x *WatchRequest) GetFromResourceVersion() string {
	if x != nil {
		return x.FromResourceVersion
	}
	return ""
}

func (x *WatchRequest) GetIncludeExisting() bool {
	if x != nil {
		return x.IncludeExisting
	}
	return false
}

type ResourcesServiceServer interface {
	ApplyClusterNetwork(context.Context, *ApplyClusterNetworkRequest) (*ClusterNetwork, error)
	GetClusterNetwork(context.Context, *GetClusterNetworkRequest) (*ClusterNetwork, error)
	ApplyServiceDesiredVersion(context.Context, *ApplyServiceDesiredVersionRequest) (*ServiceDesiredVersion, error)
	DeleteServiceDesiredVersion(context.Context, *DeleteServiceDesiredVersionRequest) (*emptypb.Empty, error)
	ListServiceDesiredVersions(context.Context, *ListServiceDesiredVersionsRequest) (*ListServiceDesiredVersionsResponse, error)
	Watch(*WatchRequest, ResourcesService_WatchServer) error
	// Service Lifecycle v1
	ApplyServiceRelease(context.Context, *ApplyServiceReleaseRequest) (*ServiceRelease, error)
	GetServiceRelease(context.Context, *GetServiceReleaseRequest) (*ServiceRelease, error)
	ListServiceReleases(context.Context, *ListServiceReleasesRequest) (*ListServiceReleasesResponse, error)
	DeleteServiceRelease(context.Context, *DeleteServiceReleaseRequest) (*emptypb.Empty, error)
	// Application Lifecycle v1
	ApplyApplicationRelease(context.Context, *ApplyApplicationReleaseRequest) (*ApplicationRelease, error)
	GetApplicationRelease(context.Context, *GetApplicationReleaseRequest) (*ApplicationRelease, error)
	ListApplicationReleases(context.Context, *ListApplicationReleasesRequest) (*ListApplicationReleasesResponse, error)
	DeleteApplicationRelease(context.Context, *DeleteApplicationReleaseRequest) (*emptypb.Empty, error)
	// Infrastructure Lifecycle v1
	ApplyInfrastructureRelease(context.Context, *ApplyInfrastructureReleaseRequest) (*InfrastructureRelease, error)
	GetInfrastructureRelease(context.Context, *GetInfrastructureReleaseRequest) (*InfrastructureRelease, error)
	ListInfrastructureReleases(context.Context, *ListInfrastructureReleasesRequest) (*ListInfrastructureReleasesResponse, error)
	DeleteInfrastructureRelease(context.Context, *DeleteInfrastructureReleaseRequest) (*emptypb.Empty, error)
	// State Alignment
	RepairStateAlignment(context.Context, *RepairStateAlignmentRequest) (*StateAlignmentReport, error)
}

type UnimplementedResourcesServiceServer struct{}

func (UnimplementedResourcesServiceServer) ApplyClusterNetwork(context.Context, *ApplyClusterNetworkRequest) (*ClusterNetwork, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ApplyClusterNetwork not implemented")
}
func (UnimplementedResourcesServiceServer) GetClusterNetwork(context.Context, *GetClusterNetworkRequest) (*ClusterNetwork, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetClusterNetwork not implemented")
}
func (UnimplementedResourcesServiceServer) ApplyServiceDesiredVersion(context.Context, *ApplyServiceDesiredVersionRequest) (*ServiceDesiredVersion, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ApplyServiceDesiredVersion not implemented")
}
func (UnimplementedResourcesServiceServer) DeleteServiceDesiredVersion(context.Context, *DeleteServiceDesiredVersionRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteServiceDesiredVersion not implemented")
}
func (UnimplementedResourcesServiceServer) ListServiceDesiredVersions(context.Context, *ListServiceDesiredVersionsRequest) (*ListServiceDesiredVersionsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListServiceDesiredVersions not implemented")
}
func (UnimplementedResourcesServiceServer) Watch(*WatchRequest, ResourcesService_WatchServer) error {
	return status.Errorf(codes.Unimplemented, "method Watch not implemented")
}
func (UnimplementedResourcesServiceServer) ApplyServiceRelease(context.Context, *ApplyServiceReleaseRequest) (*ServiceRelease, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ApplyServiceRelease not implemented")
}
func (UnimplementedResourcesServiceServer) GetServiceRelease(context.Context, *GetServiceReleaseRequest) (*ServiceRelease, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetServiceRelease not implemented")
}
func (UnimplementedResourcesServiceServer) ListServiceReleases(context.Context, *ListServiceReleasesRequest) (*ListServiceReleasesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListServiceReleases not implemented")
}
func (UnimplementedResourcesServiceServer) DeleteServiceRelease(context.Context, *DeleteServiceReleaseRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteServiceRelease not implemented")
}
func (UnimplementedResourcesServiceServer) ApplyApplicationRelease(context.Context, *ApplyApplicationReleaseRequest) (*ApplicationRelease, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ApplyApplicationRelease not implemented")
}
func (UnimplementedResourcesServiceServer) GetApplicationRelease(context.Context, *GetApplicationReleaseRequest) (*ApplicationRelease, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetApplicationRelease not implemented")
}
func (UnimplementedResourcesServiceServer) ListApplicationReleases(context.Context, *ListApplicationReleasesRequest) (*ListApplicationReleasesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListApplicationReleases not implemented")
}
func (UnimplementedResourcesServiceServer) DeleteApplicationRelease(context.Context, *DeleteApplicationReleaseRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteApplicationRelease not implemented")
}
func (UnimplementedResourcesServiceServer) ApplyInfrastructureRelease(context.Context, *ApplyInfrastructureReleaseRequest) (*InfrastructureRelease, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ApplyInfrastructureRelease not implemented")
}
func (UnimplementedResourcesServiceServer) GetInfrastructureRelease(context.Context, *GetInfrastructureReleaseRequest) (*InfrastructureRelease, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetInfrastructureRelease not implemented")
}
func (UnimplementedResourcesServiceServer) ListInfrastructureReleases(context.Context, *ListInfrastructureReleasesRequest) (*ListInfrastructureReleasesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListInfrastructureReleases not implemented")
}
func (UnimplementedResourcesServiceServer) DeleteInfrastructureRelease(context.Context, *DeleteInfrastructureReleaseRequest) (*emptypb.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteInfrastructureRelease not implemented")
}
func (UnimplementedResourcesServiceServer) RepairStateAlignment(context.Context, *RepairStateAlignmentRequest) (*StateAlignmentReport, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RepairStateAlignment not implemented")
}

type ResourcesService_WatchServer interface {
	Send(*WatchEvent) error
	grpc.ServerStream
}

func RegisterResourcesServiceServer(s *grpc.Server, srv ResourcesServiceServer) {
	s.RegisterService(&grpc.ServiceDesc{
		ServiceName: "cluster_controller.ResourcesService",
		HandlerType: (*ResourcesServiceServer)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "ApplyClusterNetwork",
				Handler:    _ResourcesService_ApplyClusterNetwork_Handler,
			},
			{
				MethodName: "GetClusterNetwork",
				Handler:    _ResourcesService_GetClusterNetwork_Handler,
			},
			{
				MethodName: "ApplyServiceDesiredVersion",
				Handler:    _ResourcesService_ApplyServiceDesiredVersion_Handler,
			},
			{
				MethodName: "DeleteServiceDesiredVersion",
				Handler:    _ResourcesService_DeleteServiceDesiredVersion_Handler,
			},
			{
				MethodName: "ListServiceDesiredVersions",
				Handler:    _ResourcesService_ListServiceDesiredVersions_Handler,
			},
			{
				MethodName: "ApplyServiceRelease",
				Handler:    _ResourcesService_ApplyServiceRelease_Handler,
			},
			{
				MethodName: "GetServiceRelease",
				Handler:    _ResourcesService_GetServiceRelease_Handler,
			},
			{
				MethodName: "ListServiceReleases",
				Handler:    _ResourcesService_ListServiceReleases_Handler,
			},
			{
				MethodName: "DeleteServiceRelease",
				Handler:    _ResourcesService_DeleteServiceRelease_Handler,
			},
			{
				MethodName: "ApplyApplicationRelease",
				Handler:    _ResourcesService_ApplyApplicationRelease_Handler,
			},
			{
				MethodName: "GetApplicationRelease",
				Handler:    _ResourcesService_GetApplicationRelease_Handler,
			},
			{
				MethodName: "ListApplicationReleases",
				Handler:    _ResourcesService_ListApplicationReleases_Handler,
			},
			{
				MethodName: "DeleteApplicationRelease",
				Handler:    _ResourcesService_DeleteApplicationRelease_Handler,
			},
			{
				MethodName: "ApplyInfrastructureRelease",
				Handler:    _ResourcesService_ApplyInfrastructureRelease_Handler,
			},
			{
				MethodName: "GetInfrastructureRelease",
				Handler:    _ResourcesService_GetInfrastructureRelease_Handler,
			},
			{
				MethodName: "ListInfrastructureReleases",
				Handler:    _ResourcesService_ListInfrastructureReleases_Handler,
			},
			{
				MethodName: "DeleteInfrastructureRelease",
				Handler:    _ResourcesService_DeleteInfrastructureRelease_Handler,
			},
			{
				MethodName: "RepairStateAlignment",
				Handler:    _ResourcesService_RepairStateAlignment_Handler,
			},
		},
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "Watch",
				Handler:       _ResourcesService_Watch_Handler,
				ServerStreams: true,
			},
		},
	}, srv)
}

func _ResourcesService_ApplyClusterNetwork_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ApplyClusterNetworkRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).ApplyClusterNetwork(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/clustercontroller.ResourcesService/ApplyClusterNetwork",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).ApplyClusterNetwork(ctx, req.(*ApplyClusterNetworkRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResourcesService_GetClusterNetwork_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetClusterNetworkRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).GetClusterNetwork(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/clustercontroller.ResourcesService/GetClusterNetwork",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).GetClusterNetwork(ctx, req.(*GetClusterNetworkRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResourcesService_ApplyServiceDesiredVersion_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ApplyServiceDesiredVersionRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).ApplyServiceDesiredVersion(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/clustercontroller.ResourcesService/ApplyServiceDesiredVersion",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).ApplyServiceDesiredVersion(ctx, req.(*ApplyServiceDesiredVersionRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResourcesService_DeleteServiceDesiredVersion_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteServiceDesiredVersionRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).DeleteServiceDesiredVersion(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/clustercontroller.ResourcesService/DeleteServiceDesiredVersion",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).DeleteServiceDesiredVersion(ctx, req.(*DeleteServiceDesiredVersionRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResourcesService_ListServiceDesiredVersions_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListServiceDesiredVersionsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).ListServiceDesiredVersions(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/clustercontroller.ResourcesService/ListServiceDesiredVersions",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).ListServiceDesiredVersions(ctx, req.(*ListServiceDesiredVersionsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResourcesService_Watch_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(WatchRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(ResourcesServiceServer).Watch(m, &resourcesServiceWatchServer{stream})
}

type resourcesServiceWatchServer struct {
	grpc.ServerStream
}

func (x *resourcesServiceWatchServer) Send(m *WatchEvent) error {
	return x.ServerStream.SendMsg(m)
}

// ResourcesServiceClient is the client API for ResourcesService.
type ResourcesServiceClient interface {
	ApplyClusterNetwork(ctx context.Context, in *ApplyClusterNetworkRequest, opts ...grpc.CallOption) (*ClusterNetwork, error)
	GetClusterNetwork(ctx context.Context, in *GetClusterNetworkRequest, opts ...grpc.CallOption) (*ClusterNetwork, error)
	ApplyServiceDesiredVersion(ctx context.Context, in *ApplyServiceDesiredVersionRequest, opts ...grpc.CallOption) (*ServiceDesiredVersion, error)
	DeleteServiceDesiredVersion(ctx context.Context, in *DeleteServiceDesiredVersionRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	ListServiceDesiredVersions(ctx context.Context, in *ListServiceDesiredVersionsRequest, opts ...grpc.CallOption) (*ListServiceDesiredVersionsResponse, error)
	Watch(ctx context.Context, in *WatchRequest, opts ...grpc.CallOption) (ResourcesService_WatchClient, error)
	// Service Lifecycle v1
	ApplyServiceRelease(ctx context.Context, in *ApplyServiceReleaseRequest, opts ...grpc.CallOption) (*ServiceRelease, error)
	GetServiceRelease(ctx context.Context, in *GetServiceReleaseRequest, opts ...grpc.CallOption) (*ServiceRelease, error)
	ListServiceReleases(ctx context.Context, in *ListServiceReleasesRequest, opts ...grpc.CallOption) (*ListServiceReleasesResponse, error)
	DeleteServiceRelease(ctx context.Context, in *DeleteServiceReleaseRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	// Application Lifecycle v1
	ApplyApplicationRelease(ctx context.Context, in *ApplyApplicationReleaseRequest, opts ...grpc.CallOption) (*ApplicationRelease, error)
	GetApplicationRelease(ctx context.Context, in *GetApplicationReleaseRequest, opts ...grpc.CallOption) (*ApplicationRelease, error)
	ListApplicationReleases(ctx context.Context, in *ListApplicationReleasesRequest, opts ...grpc.CallOption) (*ListApplicationReleasesResponse, error)
	DeleteApplicationRelease(ctx context.Context, in *DeleteApplicationReleaseRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	// Infrastructure Lifecycle v1
	ApplyInfrastructureRelease(ctx context.Context, in *ApplyInfrastructureReleaseRequest, opts ...grpc.CallOption) (*InfrastructureRelease, error)
	GetInfrastructureRelease(ctx context.Context, in *GetInfrastructureReleaseRequest, opts ...grpc.CallOption) (*InfrastructureRelease, error)
	ListInfrastructureReleases(ctx context.Context, in *ListInfrastructureReleasesRequest, opts ...grpc.CallOption) (*ListInfrastructureReleasesResponse, error)
	DeleteInfrastructureRelease(ctx context.Context, in *DeleteInfrastructureReleaseRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	// State Alignment
	RepairStateAlignment(ctx context.Context, in *RepairStateAlignmentRequest, opts ...grpc.CallOption) (*StateAlignmentReport, error)
}

type resourcesServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewResourcesServiceClient(cc grpc.ClientConnInterface) ResourcesServiceClient {
	return &resourcesServiceClient{cc}
}

func (c *resourcesServiceClient) ApplyClusterNetwork(ctx context.Context, in *ApplyClusterNetworkRequest, opts ...grpc.CallOption) (*ClusterNetwork, error) {
	out := new(ClusterNetwork)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/ApplyClusterNetwork", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resourcesServiceClient) GetClusterNetwork(ctx context.Context, in *GetClusterNetworkRequest, opts ...grpc.CallOption) (*ClusterNetwork, error) {
	out := new(ClusterNetwork)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/GetClusterNetwork", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resourcesServiceClient) ApplyServiceDesiredVersion(ctx context.Context, in *ApplyServiceDesiredVersionRequest, opts ...grpc.CallOption) (*ServiceDesiredVersion, error) {
	out := new(ServiceDesiredVersion)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/ApplyServiceDesiredVersion", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resourcesServiceClient) DeleteServiceDesiredVersion(ctx context.Context, in *DeleteServiceDesiredVersionRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/DeleteServiceDesiredVersion", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resourcesServiceClient) ListServiceDesiredVersions(ctx context.Context, in *ListServiceDesiredVersionsRequest, opts ...grpc.CallOption) (*ListServiceDesiredVersionsResponse, error) {
	out := new(ListServiceDesiredVersionsResponse)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/ListServiceDesiredVersions", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resourcesServiceClient) Watch(ctx context.Context, in *WatchRequest, opts ...grpc.CallOption) (ResourcesService_WatchClient, error) {
	stream, err := c.cc.NewStream(ctx, &grpc.StreamDesc{
		StreamName:    "Watch",
		ServerStreams: true,
	}, "/clustercontroller.ResourcesService/Watch", opts...)
	if err != nil {
		return nil, err
	}
	x := &resourcesServiceWatchClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type ResourcesService_WatchClient interface {
	Recv() (*WatchEvent, error)
	grpc.ClientStream
}

type resourcesServiceWatchClient struct {
	grpc.ClientStream
}

func (x *resourcesServiceWatchClient) Recv() (*WatchEvent, error) {
	m := new(WatchEvent)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// ── ServiceRelease server-side handlers ──────────────────────────────────────

func _ResourcesService_ApplyServiceRelease_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ApplyServiceReleaseRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).ApplyServiceRelease(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/clustercontroller.ResourcesService/ApplyServiceRelease"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).ApplyServiceRelease(ctx, req.(*ApplyServiceReleaseRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResourcesService_GetServiceRelease_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetServiceReleaseRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).GetServiceRelease(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/clustercontroller.ResourcesService/GetServiceRelease"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).GetServiceRelease(ctx, req.(*GetServiceReleaseRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResourcesService_ListServiceReleases_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListServiceReleasesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).ListServiceReleases(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/clustercontroller.ResourcesService/ListServiceReleases"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).ListServiceReleases(ctx, req.(*ListServiceReleasesRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResourcesService_DeleteServiceRelease_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteServiceReleaseRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).DeleteServiceRelease(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/clustercontroller.ResourcesService/DeleteServiceRelease"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).DeleteServiceRelease(ctx, req.(*DeleteServiceReleaseRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// ── ServiceRelease client-side methods ───────────────────────────────────────

func (c *resourcesServiceClient) ApplyServiceRelease(ctx context.Context, in *ApplyServiceReleaseRequest, opts ...grpc.CallOption) (*ServiceRelease, error) {
	out := new(ServiceRelease)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/ApplyServiceRelease", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resourcesServiceClient) GetServiceRelease(ctx context.Context, in *GetServiceReleaseRequest, opts ...grpc.CallOption) (*ServiceRelease, error) {
	out := new(ServiceRelease)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/GetServiceRelease", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resourcesServiceClient) ListServiceReleases(ctx context.Context, in *ListServiceReleasesRequest, opts ...grpc.CallOption) (*ListServiceReleasesResponse, error) {
	out := new(ListServiceReleasesResponse)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/ListServiceReleases", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resourcesServiceClient) DeleteServiceRelease(ctx context.Context, in *DeleteServiceReleaseRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/DeleteServiceRelease", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ── ApplicationRelease server-side handlers ──────────────────────────────────

func _ResourcesService_ApplyApplicationRelease_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ApplyApplicationReleaseRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).ApplyApplicationRelease(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/clustercontroller.ResourcesService/ApplyApplicationRelease"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).ApplyApplicationRelease(ctx, req.(*ApplyApplicationReleaseRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResourcesService_GetApplicationRelease_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetApplicationReleaseRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).GetApplicationRelease(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/clustercontroller.ResourcesService/GetApplicationRelease"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).GetApplicationRelease(ctx, req.(*GetApplicationReleaseRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResourcesService_ListApplicationReleases_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListApplicationReleasesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).ListApplicationReleases(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/clustercontroller.ResourcesService/ListApplicationReleases"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).ListApplicationReleases(ctx, req.(*ListApplicationReleasesRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResourcesService_DeleteApplicationRelease_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteApplicationReleaseRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).DeleteApplicationRelease(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/clustercontroller.ResourcesService/DeleteApplicationRelease"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).DeleteApplicationRelease(ctx, req.(*DeleteApplicationReleaseRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// ── ApplicationRelease client-side methods ───────────────────────────────────

func (c *resourcesServiceClient) ApplyApplicationRelease(ctx context.Context, in *ApplyApplicationReleaseRequest, opts ...grpc.CallOption) (*ApplicationRelease, error) {
	out := new(ApplicationRelease)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/ApplyApplicationRelease", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resourcesServiceClient) GetApplicationRelease(ctx context.Context, in *GetApplicationReleaseRequest, opts ...grpc.CallOption) (*ApplicationRelease, error) {
	out := new(ApplicationRelease)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/GetApplicationRelease", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resourcesServiceClient) ListApplicationReleases(ctx context.Context, in *ListApplicationReleasesRequest, opts ...grpc.CallOption) (*ListApplicationReleasesResponse, error) {
	out := new(ListApplicationReleasesResponse)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/ListApplicationReleases", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resourcesServiceClient) DeleteApplicationRelease(ctx context.Context, in *DeleteApplicationReleaseRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/DeleteApplicationRelease", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ── InfrastructureRelease server-side handlers ───────────────────────────────

func _ResourcesService_ApplyInfrastructureRelease_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ApplyInfrastructureReleaseRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).ApplyInfrastructureRelease(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/clustercontroller.ResourcesService/ApplyInfrastructureRelease"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).ApplyInfrastructureRelease(ctx, req.(*ApplyInfrastructureReleaseRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResourcesService_GetInfrastructureRelease_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetInfrastructureReleaseRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).GetInfrastructureRelease(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/clustercontroller.ResourcesService/GetInfrastructureRelease"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).GetInfrastructureRelease(ctx, req.(*GetInfrastructureReleaseRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResourcesService_ListInfrastructureReleases_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListInfrastructureReleasesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).ListInfrastructureReleases(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/clustercontroller.ResourcesService/ListInfrastructureReleases"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).ListInfrastructureReleases(ctx, req.(*ListInfrastructureReleasesRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ResourcesService_DeleteInfrastructureRelease_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteInfrastructureReleaseRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).DeleteInfrastructureRelease(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/clustercontroller.ResourcesService/DeleteInfrastructureRelease"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).DeleteInfrastructureRelease(ctx, req.(*DeleteInfrastructureReleaseRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// ── InfrastructureRelease client-side methods ────────────────────────────────

func (c *resourcesServiceClient) ApplyInfrastructureRelease(ctx context.Context, in *ApplyInfrastructureReleaseRequest, opts ...grpc.CallOption) (*InfrastructureRelease, error) {
	out := new(InfrastructureRelease)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/ApplyInfrastructureRelease", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resourcesServiceClient) GetInfrastructureRelease(ctx context.Context, in *GetInfrastructureReleaseRequest, opts ...grpc.CallOption) (*InfrastructureRelease, error) {
	out := new(InfrastructureRelease)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/GetInfrastructureRelease", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resourcesServiceClient) ListInfrastructureReleases(ctx context.Context, in *ListInfrastructureReleasesRequest, opts ...grpc.CallOption) (*ListInfrastructureReleasesResponse, error) {
	out := new(ListInfrastructureReleasesResponse)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/ListInfrastructureReleases", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *resourcesServiceClient) DeleteInfrastructureRelease(ctx context.Context, in *DeleteInfrastructureReleaseRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/DeleteInfrastructureRelease", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ── RepairStateAlignment server-side handler ─────────────────────────────────

func _ResourcesService_RepairStateAlignment_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RepairStateAlignmentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ResourcesServiceServer).RepairStateAlignment(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/clustercontroller.ResourcesService/RepairStateAlignment"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ResourcesServiceServer).RepairStateAlignment(ctx, req.(*RepairStateAlignmentRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// ── RepairStateAlignment client-side method ──────────────────────────────────

func (c *resourcesServiceClient) RepairStateAlignment(ctx context.Context, in *RepairStateAlignmentRequest, opts ...grpc.CallOption) (*StateAlignmentReport, error) {
	out := new(StateAlignmentReport)
	err := c.cc.Invoke(ctx, "/clustercontroller.ResourcesService/RepairStateAlignment", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}
