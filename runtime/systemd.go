package runtime

import "context"

// SystemdUnit is the status of a single systemd service unit.
type SystemdUnit struct {
	ServiceID    string
	UnitName     string
	ActiveState  string // active, inactive, failed
	SubState     string // running, exited, start-limit-hit, dead
	RestartCount int
	NodeID       string
}

// SystemdStatusSource returns current systemd unit statuses.
type SystemdStatusSource interface {
	Units(ctx context.Context) ([]SystemdUnit, error)
}

// NoopSystemdStatusSource returns no units.
type NoopSystemdStatusSource struct{}

func (NoopSystemdStatusSource) Units(_ context.Context) ([]SystemdUnit, error) { return nil, nil }
func (NoopSystemdStatusSource) SourceInfo() (string, bool)                     { return "noop", true }

// FakeSystemdStatusSource returns fixed units (for tests).
type FakeSystemdStatusSource struct {
	Data []SystemdUnit
	Err  error
}

func (f *FakeSystemdStatusSource) Units(_ context.Context) ([]SystemdUnit, error) {
	return f.Data, f.Err
}
