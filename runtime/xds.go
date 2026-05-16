package runtime

import "context"

// XDSStatus is the operational status of the xDS control plane.
type XDSStatus struct {
	NodeID            string
	AppliedGeneration int64
	PendingGeneration int64
	LastError         string
}

// XDSStatusSource returns current xDS status.
type XDSStatusSource interface {
	Status(ctx context.Context) ([]XDSStatus, error)
}

// NoopXDSStatusSource returns no status.
type NoopXDSStatusSource struct{}

func (NoopXDSStatusSource) Status(_ context.Context) ([]XDSStatus, error) { return nil, nil }
func (NoopXDSStatusSource) SourceInfo() (string, bool)                     { return "noop", true }

// FakeXDSStatusSource returns fixed status (for tests).
type FakeXDSStatusSource struct {
	Data []XDSStatus
	Err  error
}

func (f *FakeXDSStatusSource) Status(_ context.Context) ([]XDSStatus, error) {
	return f.Data, f.Err
}
