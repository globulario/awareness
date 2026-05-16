package runtime

import "time"

// SourceKind identifies a runtime evidence source.
type SourceKind string

const (
	SourceDoctor      SourceKind = "doctor"
	SourceState       SourceKind = "state"
	SourceServices    SourceKind = "services"
	SourceWorkflows   SourceKind = "workflows"
	SourceMetrics     SourceKind = "metrics"
	SourceEvents      SourceKind = "events"
	SourceRepository  SourceKind = "repository"
	SourceObjectstore SourceKind = "objectstore"
	SourceXDS         SourceKind = "xds"
	SourceSystemd     SourceKind = "systemd"
)

// SourceHealth reports the collection status of a single evidence source.
// It distinguishes three empty-result causes:
//   - EmptyDueToNoop=true: never collected real data (noop source)
//   - Healthy=false, LastError set: tried and failed
//   - Healthy=true: data was collected (may still be empty if cluster has no findings)
type SourceHealth struct {
	Source         SourceKind `json:"source"`
	Backend        string     `json:"backend"`                    // "cluster_doctor.grpc", "prometheus.http", "noop", etc.
	Transport      string     `json:"transport,omitempty"`        // "insecure", "tls", "mtls"
	Auth           string     `json:"auth,omitempty"`             // "none", "service_token"
	Healthy        bool       `json:"healthy"`
	EmptyDueToNoop bool       `json:"empty_due_to_noop"`
	LastError      string     `json:"last_error,omitempty"`
	CollectedAt    string     `json:"collected_at"`
	Warnings       []string   `json:"warnings,omitempty"` // e.g. insecure transport warning
}

// withTransport returns a copy of SourceHealth with Transport and Auth set.
// If transport is "insecure", a production-safety warning is appended.
func (sh SourceHealth) withTransport(transport, auth string) SourceHealth {
	sh.Transport = transport
	sh.Auth = auth
	if transport == "insecure" {
		sh.Warnings = append(append([]string(nil), sh.Warnings...), "insecure runtime source transport enabled — not safe for production")
	}
	return sh
}

// sourceIdentifier is an optional interface that sources implement to report
// their backend name and whether they are a no-op.
type sourceIdentifier interface {
	SourceInfo() (backend string, isNoop bool)
}

// transportReporter is an optional interface for sources that can report
// which transport they are using.
type transportReporter interface {
	Transport() string
}

func newHealthySource(kind SourceKind, backend string) SourceHealth {
	return SourceHealth{
		Source:      kind,
		Backend:     backend,
		Healthy:     true,
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func newNoopSource(kind SourceKind) SourceHealth {
	return SourceHealth{
		Source:         kind,
		Backend:        "noop",
		Healthy:        false,
		EmptyDueToNoop: true,
		CollectedAt:    time.Now().UTC().Format(time.RFC3339),
	}
}

func newErrSource(kind SourceKind, backend string, err error) SourceHealth {
	msg := "unknown error"
	if err != nil {
		msg = err.Error()
	}
	return SourceHealth{
		Source:      kind,
		Backend:     backend,
		Healthy:     false,
		LastError:   msg,
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// sourceHealthFor builds a SourceHealth record after a collection attempt.
// src is the source interface value; err is nil on success.
func sourceHealthFor(kind SourceKind, src interface{}, err error) SourceHealth {
	backend := "unknown"
	isNoop := false
	if si, ok := src.(sourceIdentifier); ok {
		backend, isNoop = si.SourceInfo()
	}
	if isNoop {
		return newNoopSource(kind)
	}

	var sh SourceHealth
	if err != nil {
		sh = newErrSource(kind, backend, err)
	} else {
		sh = newHealthySource(kind, backend)
	}

	// Carry transport info if the source implements transportReporter.
	if tr, ok := src.(transportReporter); ok {
		sh = sh.withTransport(tr.Transport(), "none")
	}

	return sh
}
