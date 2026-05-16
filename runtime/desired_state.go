package runtime

import "context"

// DesiredStateRecord is a single desired-state entry from the controller.
type DesiredStateRecord struct {
	ServiceID string
	Version   string
	BuildID   string
	Phase     string
	Profiles  []string
}

// StateSource returns desired and installed state records.
type StateSource interface {
	DesiredState(ctx context.Context) ([]DesiredStateRecord, error)
	InstalledState(ctx context.Context) ([]InstalledStateRecord, error)
}

// NoopStateSource returns empty state and never errors.
type NoopStateSource struct{}

func (NoopStateSource) DesiredState(_ context.Context) ([]DesiredStateRecord, error) {
	return nil, nil
}

func (NoopStateSource) InstalledState(_ context.Context) ([]InstalledStateRecord, error) {
	return nil, nil
}

func (NoopStateSource) SourceInfo() (string, bool) { return "noop", true }

// FakeStateSource returns fixed state (for tests).
type FakeStateSource struct {
	DesiredData   []DesiredStateRecord
	InstalledData []InstalledStateRecord
	Err           error
}

func (f *FakeStateSource) DesiredState(_ context.Context) ([]DesiredStateRecord, error) {
	return f.DesiredData, f.Err
}

func (f *FakeStateSource) InstalledState(_ context.Context) ([]InstalledStateRecord, error) {
	return f.InstalledData, f.Err
}
