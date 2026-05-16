package runtime

import "context"

// ServiceStatus is the runtime status of a single service instance.
type ServiceStatus struct {
	ServiceID    string
	NodeID       string
	Version      string
	State        string // RUNNING, STOPPED, FAILED, STARTING, START_LIMIT_HIT
	RestartCount int
	LastError    string
}

// ServiceStatusSource returns current service statuses.
type ServiceStatusSource interface {
	Services(ctx context.Context) ([]ServiceStatus, error)
}

// NoopServiceStatusSource returns no services and never errors.
type NoopServiceStatusSource struct{}

func (NoopServiceStatusSource) Services(_ context.Context) ([]ServiceStatus, error) {
	return nil, nil
}
func (NoopServiceStatusSource) SourceInfo() (string, bool) { return "noop", true }

// FakeServiceStatusSource returns fixed statuses (for tests).
type FakeServiceStatusSource struct {
	Data []ServiceStatus
	Err  error
}

func (f *FakeServiceStatusSource) Services(_ context.Context) ([]ServiceStatus, error) {
	return f.Data, f.Err
}
