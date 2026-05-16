package runtime

import "context"

// ObjectstoreStatus is the operational status of the object store (MinIO).
type ObjectstoreStatus struct {
	TopologyMatch bool
	NodeCount     int
	ExpectedCount int
	Mode          string // DISTRIBUTED, STANDALONE, DEGRADED
	NodeID        string
	LastError     string
}

// ObjectstoreStatusSource returns current object store status.
type ObjectstoreStatusSource interface {
	Status(ctx context.Context) ([]ObjectstoreStatus, error)
}

// NoopObjectstoreStatusSource returns no status.
type NoopObjectstoreStatusSource struct{}

func (NoopObjectstoreStatusSource) Status(_ context.Context) ([]ObjectstoreStatus, error) {
	return nil, nil
}
func (NoopObjectstoreStatusSource) SourceInfo() (string, bool) { return "noop", true }

// FakeObjectstoreStatusSource returns fixed status (for tests).
type FakeObjectstoreStatusSource struct {
	Data []ObjectstoreStatus
	Err  error
}

func (f *FakeObjectstoreStatusSource) Status(_ context.Context) ([]ObjectstoreStatus, error) {
	return f.Data, f.Err
}
