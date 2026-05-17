package runtime

import (
	"context"
	"time"
)

// ─── Minimal domain types used by both the Adapter interface and BridgeSnapshotter ───

// DoctorFindingCompat is the subset of DoctorFinding fields needed by
// the standalone preflight package. Concrete adapter implementations
// (in the services module) must populate these fields.
type DoctorFindingCompat struct {
	FindingID  string
	Severity   string
	Title      string
	Suppressed bool
}

// ServiceStatusCompat is the subset of ServiceStatus fields needed by
// the standalone preflight package.
type ServiceStatusCompat struct {
	ServiceID string
	NodeID    string
	State     string
}

// WorkflowReceiptCompat is the subset of WorkflowReceipt fields needed by
// the standalone preflight package.
type WorkflowReceiptCompat struct {
	WorkflowType string
	Status       string
	ErrorMsg     string
}

// StateDeltaCompat is the subset of StateDelta fields needed by
// the standalone preflight package.
type StateDeltaCompat struct {
	ServiceID        string
	DeltaType        string
	DesiredVersion   string
	InstalledVersion string
}

// RepositoryStatusCompat is the subset of RepositoryStatus fields needed by
// the standalone Adapter interface.
type RepositoryStatusCompat struct {
	Mode      string
	NodeID    string
	Reachable bool
	LastError string
}

// ─── BridgeSnapshotter ────────────────────────────────────────────────────────

// BridgeSnapshot is a minimal runtime snapshot returned by BridgeSnapshotter.
// It contains only the fields that the standalone preflight package needs.
// The concrete RuntimeBridge in the services module populates these fields
// from its full RuntimeSnapshot.
type BridgeSnapshot struct {
	CapturedAt          time.Time
	MatchedInvariants   []string
	MatchedFailureModes []string
	Warnings            []string
	DoctorFindings      []DoctorFindingCompat
	ServiceStatuses     []ServiceStatusCompat
	WorkflowReceipts    []WorkflowReceiptCompat
	StateDeltas         []StateDeltaCompat
	RepositoryStatuses  []RepositoryStatusCompat
}

// BridgeSnapshotter is the interface that a RuntimeBridge must implement for
// use with the standalone preflight package. The concrete type lives in the
// services module (services/golang/awareness/runtime.RuntimeBridge).
//
// The graph argument is passed as interface{} to avoid importing the graph
// package here; callers must type-assert to *graph.Graph.
type BridgeSnapshotter interface {
	BridgeSnapshot(ctx context.Context, since time.Duration, g interface{}) (*BridgeSnapshot, error)
}

// ─── Adapter interface ────────────────────────────────────────────────────────

// RuntimeSignals is the adapter-agnostic signal envelope returned by
// Adapter.CollectSignals. Fields are populated only when the adapter is
// enabled and sources are reachable; NullAdapter always returns a zero value.
type RuntimeSignals struct {
	DoctorFindings   []DoctorFindingCompat
	ServiceStatuses  []ServiceStatusCompat
	RepositoryStatus []RepositoryStatusCompat
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

// ─── NullBridgeSnapshotter ────────────────────────────────────────────────────

// NullBridgeSnapshotter is the default BridgeSnapshotter for use when no
// live cluster is available. It satisfies the interface with an empty,
// non-error result.
type NullBridgeSnapshotter struct{}

func (NullBridgeSnapshotter) BridgeSnapshot(_ context.Context, _ time.Duration, _ interface{}) (*BridgeSnapshot, error) {
	return &BridgeSnapshot{CapturedAt: time.Now()}, nil
}
