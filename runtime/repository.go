package runtime

import "context"

// RepositoryStatus is the operational status of the repository service.
type RepositoryStatus struct {
	Mode      string // NORMAL, DEGRADED, READ_ONLY, LOCAL_ONLY
	NodeID    string
	Reachable bool
	LastError string
}

// RepositoryStatusSource returns current repository status.
type RepositoryStatusSource interface {
	Status(ctx context.Context) ([]RepositoryStatus, error)
}

// NoopRepositoryStatusSource returns no status.
type NoopRepositoryStatusSource struct{}

func (NoopRepositoryStatusSource) Status(_ context.Context) ([]RepositoryStatus, error) {
	return nil, nil
}
func (NoopRepositoryStatusSource) SourceInfo() (string, bool) { return "noop", true }

// FakeRepositoryStatusSource returns fixed status (for tests).
type FakeRepositoryStatusSource struct {
	Data []RepositoryStatus
	Err  error
}

func (f *FakeRepositoryStatusSource) Status(_ context.Context) ([]RepositoryStatus, error) {
	return f.Data, f.Err
}
