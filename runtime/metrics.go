package runtime

import "context"

// MetricSample is a compact, source-agnostic metric value captured during a
// runtime snapshot. It lets Prometheus or another collector feed dynamic risk
// into preflight without coupling awareness to one storage backend.
type MetricSample struct {
	Name      string
	NodeID    string
	ServiceID string
	Value     float64
	Unit      string
	Labels    map[string]string
}

// MetricsSource returns current or recent operational metrics.
// Implementations should be read-only. Errors are converted to snapshot warnings.
type MetricsSource interface {
	Samples(ctx context.Context) ([]MetricSample, error)
}

// NoopMetricsSource returns no samples and never errors.
type NoopMetricsSource struct{}

func (NoopMetricsSource) Samples(_ context.Context) ([]MetricSample, error) { return nil, nil }
func (NoopMetricsSource) SourceInfo() (string, bool)                         { return "noop", true }

// FakeMetricsSource returns fixed samples for tests.
type FakeMetricsSource struct {
	Data []MetricSample
	Err  error
}

func (f *FakeMetricsSource) Samples(_ context.Context) ([]MetricSample, error) {
	return f.Data, f.Err
}
