package runtime

import (
	"context"
	"time"
)

// RuntimeEvent is a single event observed in the cluster.
type RuntimeEvent struct {
	EventType string
	ServiceID string
	NodeID    string
	Timestamp time.Time
	Message   string
	Severity  string
}

// EventSource returns recent runtime events.
type EventSource interface {
	RecentEvents(ctx context.Context, since time.Duration) ([]RuntimeEvent, error)
}

// NoopEventSource returns no events and never errors.
type NoopEventSource struct{}

func (NoopEventSource) RecentEvents(_ context.Context, _ time.Duration) ([]RuntimeEvent, error) {
	return nil, nil
}
func (NoopEventSource) SourceInfo() (string, bool) { return "noop", true }

// FakeEventSource returns fixed events (for tests).
type FakeEventSource struct {
	Data []RuntimeEvent
	Err  error
}

func (f *FakeEventSource) RecentEvents(_ context.Context, _ time.Duration) ([]RuntimeEvent, error) {
	return f.Data, f.Err
}
