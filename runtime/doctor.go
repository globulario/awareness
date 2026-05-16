// Package runtime provides a read-only observation layer that connects the
// awareness graph to live cluster runtime evidence.
//
// HARD CONSTRAINT: This package is READ-ONLY. It must never call RPCs that
// mutate desired state, dispatch workflows, or modify installed state.
package runtime

import (
	"context"
	"strings"
)

// DoctorFinding is a single finding from the cluster doctor.
type DoctorFinding struct {
	FindingID    string
	Severity     string // critical, high, medium, low
	Title        string
	Description  string
	InvariantRef string // optional reference to known invariant ID
	ServiceRef   string // optional service ID
	Suppressed   bool
}

// DoctorSource returns current doctor findings.
type DoctorSource interface {
	Findings(ctx context.Context) ([]DoctorFinding, error)
}

// NoopDoctorSource returns no findings and never errors.
type NoopDoctorSource struct{}

func (NoopDoctorSource) Findings(_ context.Context) ([]DoctorFinding, error) { return nil, nil }
func (NoopDoctorSource) SourceInfo() (string, bool)                           { return "noop", true }

// FakeDoctorSource returns a fixed set of findings (for tests).
type FakeDoctorSource struct {
	Data []DoctorFinding
	Err  error
}

func (f *FakeDoctorSource) Findings(_ context.Context) ([]DoctorFinding, error) {
	return f.Data, f.Err
}

// matchFindingToInvariant returns the invariant ID that best matches a finding,
// or "" if no match. It first checks InvariantRef, then keyword-matches the title.
func matchFindingToInvariant(f DoctorFinding, knownInvariants []string) string {
	if f.InvariantRef != "" {
		for _, id := range knownInvariants {
			if id == f.InvariantRef {
				return id
			}
		}
	}
	lower := strings.ToLower(f.Title + " " + f.Description)
	for _, id := range knownInvariants {
		// Match against the last segment of the invariant ID (e.g. "desired_hash_consistency").
		// Try both underscore form and space form for resilience.
		parts := strings.Split(id, ".")
		if len(parts) > 0 {
			segment := parts[len(parts)-1]
			if strings.Contains(lower, segment) ||
				strings.Contains(lower, strings.ReplaceAll(segment, "_", " ")) {
				return id
			}
		}
	}
	return ""
}
