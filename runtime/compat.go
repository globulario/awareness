package runtime

import "context"

// RuntimeSignals is the adapter-agnostic signal envelope returned by
// Adapter.CollectSignals. Fields are populated only when the adapter is
// enabled and sources are reachable; NullAdapter always returns a zero value.
type RuntimeSignals struct {
	DoctorFindings   []DoctorFinding
	ServiceStatuses  []ServiceStatus
	RepositoryStatus []RepositoryStatus
}

// Adapter is the lightweight standalone runtime interface used by the
// awareness CLI and awareness-mcp server. It allows the preflight command
// to optionally collect live signals without depending on a Globular cluster.
type Adapter interface {
	// Name returns the adapter identifier (e.g. "null", "globular").
	Name() string
	// Enabled reports whether the adapter can provide live data.
	Enabled() bool
	// CollectSignals collects live runtime signals. Returns an empty signals
	// struct and nil error when the adapter is disabled or unavailable.
	CollectSignals(ctx context.Context, prof interface{}, opts SignalOptions) (*RuntimeSignals, error)
	// Doctor returns a lightweight runtime health report for the adapter.
	Doctor(ctx context.Context, prof interface{}) (AdapterDoctorReport, error)
	// CollectFacts collects structured evidence facts for preflight analysis.
	// Returns nil, nil when the adapter is disabled.
	CollectFacts(ctx context.Context, prof interface{}) ([]interface{}, error)
}

// SignalOptions configures signal collection. Reserved for future use.
type SignalOptions struct{}

// AdapterDoctorReport is a lightweight runtime health report returned by
// Adapter.Doctor. For NullAdapter this always reflects runtime_disabled.
type AdapterDoctorReport struct {
	Status   string   `json:"status"`
	Findings []string `json:"findings,omitempty"`
}

// NullAdapter is the default adapter for projects that do not require live
// runtime integration. It satisfies the Adapter interface with empty,
// non-error results.
type NullAdapter struct{}

func (NullAdapter) Name() string { return "null" }
func (NullAdapter) Enabled() bool { return false }
func (NullAdapter) CollectSignals(_ context.Context, _ interface{}, _ SignalOptions) (*RuntimeSignals, error) {
	return &RuntimeSignals{}, nil
}
func (NullAdapter) Doctor(_ context.Context, _ interface{}) (AdapterDoctorReport, error) {
	return AdapterDoctorReport{Status: "runtime_disabled"}, nil
}
func (NullAdapter) CollectFacts(_ context.Context, _ interface{}) ([]interface{}, error) {
	return nil, nil
}

// New returns a runtime adapter by name. Currently only "null" is supported
// in the standalone module; cluster adapters require Globular services.
// When an unknown adapter name is requested, New falls back to NullAdapter
// and returns an error so callers can log a warning.
func New(adapterName string) (Adapter, error) {
	return NullAdapter{}, nil
}
