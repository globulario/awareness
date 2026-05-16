package runtime

import "context"

// NullAdapter is the default RuntimeAdapter for projects that do not require
// live runtime integration (runtime.enabled=false in .awareness.yaml).
//
// All methods return empty, non-error results. Cadence and any other
// project that does not have a Globular cluster uses NullAdapter.
type NullAdapter struct{}

var _ RuntimeAdapter = NullAdapter{}

func (NullAdapter) Name() string    { return "null" }
func (NullAdapter) Enabled() bool   { return false }

func (NullAdapter) CollectFacts(_ context.Context) ([]RuntimeFact, error) {
	return nil, nil
}

func (NullAdapter) Health(_ context.Context) ([]RuntimeFinding, error) {
	return nil, nil
}

func (NullAdapter) ResolveRuntimeObject(_ context.Context, _ RuntimeRef) (*RuntimeObject, error) {
	return nil, nil
}
