package runtime

import (
	"context"
	"fmt"
	"time"

	workflowpb "github.com/globulario/awareness/workflowpb"
	"google.golang.org/grpc"
)

// GrpcWorkflowSource pulls recent workflow runs from the workflow service.
// It fetches both failed and active/blocked runs within the lookback window.
type GrpcWorkflowSource struct {
	cfg       GrpcSourceConfig
	transport string
	conn      *grpc.ClientConn
	client    workflowpb.WorkflowServiceClient
}

// NewGrpcWorkflowSource dials the workflow service using the provided config.
func NewGrpcWorkflowSource(cfg GrpcSourceConfig) (*GrpcWorkflowSource, error) {
	if cfg.Addr == "" {
		return nil, fmt.Errorf("workflow source: addr is empty")
	}
	opts, transport, err := cfg.dialOptions()
	if err != nil {
		return nil, fmt.Errorf("workflow source: dial options: %w", err)
	}
	conn, err := grpc.NewClient(cfg.Addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("workflow source: dial %s: %w", cfg.Addr, err)
	}
	return &GrpcWorkflowSource{
		cfg:       cfg,
		transport: transport,
		conn:      conn,
		client:    workflowpb.NewWorkflowServiceClient(conn),
	}, nil
}

// Close releases the gRPC connection.
func (s *GrpcWorkflowSource) Close() { _ = s.conn.Close() }

// SourceInfo implements sourceIdentifier.
func (s *GrpcWorkflowSource) SourceInfo() (string, bool) { return "workflow.grpc", false }

// Transport implements transportReporter.
func (s *GrpcWorkflowSource) Transport() string { return s.transport }

// RecentReceipts fetches failed and active workflow runs within the lookback window.
func (s *GrpcWorkflowSource) RecentReceipts(ctx context.Context, since time.Duration) ([]WorkflowReceipt, error) {
	cutoff := time.Now().Add(-since)
	seen := make(map[string]bool)
	var out []WorkflowReceipt

	// Fetch failed runs.
	failed, err := s.client.ListRuns(ctx, &workflowpb.ListRunsRequest{
		FailedOnly: true,
		Limit:      50,
	})
	if err != nil {
		return nil, fmt.Errorf("ListRuns (failed): %w", err)
	}
	for _, run := range failed.GetRuns() {
		if r := workflowRunToReceipt(run, cutoff); r != nil && !seen[r.WorkflowID] {
			seen[r.WorkflowID] = true
			out = append(out, *r)
		}
	}

	// Fetch active/blocked runs.
	active, err := s.client.ListRuns(ctx, &workflowpb.ListRunsRequest{
		ActiveOnly: true,
		Limit:      50,
	})
	if err != nil {
		// Don't fail entirely — failed runs already collected.
		out = append(out, WorkflowReceipt{
			WorkflowID:   "list-active-error",
			WorkflowType: "error",
			Status:       "ERROR",
			ErrorMsg:     err.Error(),
			StartedAt:    time.Now(),
		})
		return out, nil
	}
	for _, run := range active.GetRuns() {
		if r := workflowRunToReceipt(run, cutoff); r != nil && !seen[r.WorkflowID] {
			seen[r.WorkflowID] = true
			out = append(out, *r)
		}
	}

	return out, nil
}

func workflowRunToReceipt(run *workflowpb.WorkflowRun, cutoff time.Time) *WorkflowReceipt {
	if run == nil {
		return nil
	}
	startedAt := time.Time{}
	if ts := run.GetStartedAt(); ts != nil {
		startedAt = ts.AsTime()
	}
	if !startedAt.IsZero() && startedAt.Before(cutoff) {
		return nil
	}
	status := runStatusString(run.GetStatus())
	var finishedAt *time.Time
	if ts := run.GetFinishedAt(); ts != nil {
		t := ts.AsTime()
		finishedAt = &t
	}
	// Extract a service ID from the workflow context if available.
	serviceID := ""
	if wctx := run.GetContext(); wctx != nil {
		serviceID = wctx.GetComponentName()
	}
	return &WorkflowReceipt{
		WorkflowID:   run.GetId(),
		WorkflowType: run.GetWorkflowName(),
		Status:       status,
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		ErrorMsg:     run.GetErrorMessage(),
		ServiceID:    serviceID,
	}
}

func runStatusString(s workflowpb.RunStatus) string {
	switch s {
	case workflowpb.RunStatus_RUN_STATUS_PENDING:
		return "PENDING"
	case workflowpb.RunStatus_RUN_STATUS_EXECUTING, workflowpb.RunStatus_RUN_STATUS_RETRYING:
		return "RUNNING"
	case workflowpb.RunStatus_RUN_STATUS_BLOCKED:
		return "BLOCKED"
	case workflowpb.RunStatus_RUN_STATUS_SUCCEEDED:
		return "SUCCEEDED"
	case workflowpb.RunStatus_RUN_STATUS_FAILED:
		return "FAILED"
	case workflowpb.RunStatus_RUN_STATUS_CANCELED:
		return "CANCELED"
	default:
		return "UNKNOWN"
	}
}
