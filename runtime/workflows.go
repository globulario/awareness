package runtime

import (
	"context"
	"strings"
	"time"
)

// WorkflowReceipt is a summary of a recent workflow execution.
type WorkflowReceipt struct {
	WorkflowID   string
	WorkflowType string
	Status       string // PENDING, RUNNING, SUCCEEDED, FAILED, TIMED_OUT
	StartedAt    time.Time
	FinishedAt   *time.Time
	ErrorMsg     string
	ServiceID    string
}

// WorkflowSource returns recent workflow receipts.
type WorkflowSource interface {
	RecentReceipts(ctx context.Context, since time.Duration) ([]WorkflowReceipt, error)
}

// NoopWorkflowSource returns no receipts and never errors.
type NoopWorkflowSource struct{}

func (NoopWorkflowSource) RecentReceipts(_ context.Context, _ time.Duration) ([]WorkflowReceipt, error) {
	return nil, nil
}
func (NoopWorkflowSource) SourceInfo() (string, bool) { return "noop", true }

// FakeWorkflowSource returns fixed receipts (for tests).
type FakeWorkflowSource struct {
	Data []WorkflowReceipt
	Err  error
}

func (f *FakeWorkflowSource) RecentReceipts(_ context.Context, _ time.Duration) ([]WorkflowReceipt, error) {
	return f.Data, f.Err
}

// matchWorkflowToFailureMode returns the failure mode ID that best matches
// a failed workflow receipt. Matching is keyword-based against WorkflowType.
func matchWorkflowToFailureMode(r WorkflowReceipt, knownFMs []string) string {
	if r.Status != "FAILED" && r.Status != "TIMED_OUT" {
		return ""
	}
	lower := strings.ToLower(r.WorkflowType + " " + r.ErrorMsg)
	for _, id := range knownFMs {
		parts := strings.Split(id, ".")
		if len(parts) > 0 {
			segment := parts[len(parts)-1]
			// Try both underscore form (e.g. "metadata_conflict") and space form.
			if strings.Contains(lower, segment) ||
				strings.Contains(lower, strings.ReplaceAll(segment, "_", " ")) {
				return id
			}
		}
	}
	return ""
}
