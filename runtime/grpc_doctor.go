package runtime

import (
	"context"
	"fmt"
	"strings"

	cluster_doctorpb "github.com/globulario/awareness/cluster_doctorpb"
	"google.golang.org/grpc"
)

// GrpcDoctorSource pulls live doctor findings from the cluster_doctor service.
// It is read-only: it only calls GetClusterReport with HEAL_MODE_OBSERVE.
type GrpcDoctorSource struct {
	cfg       GrpcSourceConfig
	transport string // "insecure", "tls", or "mtls"
	conn      *grpc.ClientConn
	client    cluster_doctorpb.ClusterDoctorServiceClient
}

// NewGrpcDoctorSource dials the cluster_doctor service using the provided config.
// Returns an error if the dial fails; the source is unusable in that case.
func NewGrpcDoctorSource(cfg GrpcSourceConfig) (*GrpcDoctorSource, error) {
	if cfg.Addr == "" {
		return nil, fmt.Errorf("doctor source: addr is empty")
	}
	opts, transport, err := cfg.dialOptions()
	if err != nil {
		return nil, fmt.Errorf("doctor source: dial options: %w", err)
	}
	conn, err := grpc.NewClient(cfg.Addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("doctor source: dial %s: %w", cfg.Addr, err)
	}
	return &GrpcDoctorSource{
		cfg:       cfg,
		transport: transport,
		conn:      conn,
		client:    cluster_doctorpb.NewClusterDoctorServiceClient(conn),
	}, nil
}

// Close releases the gRPC connection.
func (s *GrpcDoctorSource) Close() { _ = s.conn.Close() }

// SourceInfo implements sourceIdentifier.
func (s *GrpcDoctorSource) SourceInfo() (string, bool) { return "cluster_doctor.grpc", false }

// Transport implements transportReporter.
func (s *GrpcDoctorSource) Transport() string { return s.transport }

// Findings calls GetClusterReport in observe mode (no mutations) and maps results.
func (s *GrpcDoctorSource) Findings(ctx context.Context) ([]DoctorFinding, error) {
	resp, err := s.client.GetClusterReport(ctx, &cluster_doctorpb.ClusterReportRequest{
		// HealMode 0 = HEAL_MODE_OBSERVE — read-only.
	})
	if err != nil {
		return nil, fmt.Errorf("GetClusterReport: %w", err)
	}
	out := make([]DoctorFinding, 0, len(resp.GetFindings()))
	for _, f := range resp.GetFindings() {
		out = append(out, DoctorFinding{
			FindingID:    f.GetFindingId(),
			Severity:     severityString(f.GetSeverity()),
			Title:        f.GetSummary(),
			Description:  evidenceText(f.GetEvidence()),
			InvariantRef: f.GetInvariantId(),
			ServiceRef:   f.GetEntityRef(),
		})
	}
	return out, nil
}

func severityString(s cluster_doctorpb.Severity) string {
	switch s {
	case cluster_doctorpb.Severity_SEVERITY_CRITICAL:
		return "critical"
	case cluster_doctorpb.Severity_SEVERITY_ERROR:
		return "high"
	case cluster_doctorpb.Severity_SEVERITY_WARN:
		return "medium"
	case cluster_doctorpb.Severity_SEVERITY_INFO:
		return "low"
	default:
		return "unknown"
	}
}

// evidenceText converts Evidence key_values map to a human-readable string.
// Evidence.key_values is map[string]string in the proto.
func evidenceText(ev []*cluster_doctorpb.Evidence) string {
	if len(ev) == 0 {
		return ""
	}
	parts := make([]string, 0)
	for _, e := range ev {
		for k, v := range e.GetKeyValues() {
			if v != "" {
				parts = append(parts, k+": "+v)
			}
		}
	}
	return strings.Join(parts, "; ")
}
