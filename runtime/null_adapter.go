package runtime

import (
	"context"

	"github.com/globulario/awareness/project"
)

// NullAdapter is the default Adapter for projects that do not require live
// runtime integration (runtime.enabled=false in .awareness.yaml).
//
// All methods return empty, non-error results. Cadence and any other project
// that does not have a Globular cluster uses NullAdapter.
type NullAdapter struct{}

var _ Adapter = NullAdapter{}

func (NullAdapter) Name() string  { return "null" }
func (NullAdapter) Enabled() bool { return false }

func (NullAdapter) Doctor(_ context.Context, _ *project.ProjectProfile) (*DoctorReport, error) {
	return &DoctorReport{
		Adapter:  "null",
		Enabled:  false,
		Status:   "runtime_disabled",
		Findings: nil,
	}, nil
}

func (NullAdapter) CollectFacts(_ context.Context, _ *project.ProjectProfile) ([]Fact, error) {
	return nil, nil
}

func (NullAdapter) CollectEvidence(_ context.Context, _ *project.ProjectProfile, _ EvidenceQuery) ([]Evidence, error) {
	return nil, nil
}
