package livecluster

import (
	"context"
	"time"
)

// MockCollector is a test double for SignalCollector.
// Callers configure exactly what data it returns.
type MockCollector struct {
	name     string
	status   string // ok | degraded | unavailable | timeout
	services []ServiceLiveState
	errors   []RecentErrorSignature
	conv     []RuntimeConvergenceState
	incidents []ActiveClusterIncident
	sleep    time.Duration
}

// NewMockCollector returns a healthy mock collector with no signals.
func NewMockCollector(name string) *MockCollector {
	return &MockCollector{name: name, status: "ok"}
}

// WithStatus overrides the source status (ok | degraded | unavailable | timeout).
func (m *MockCollector) WithStatus(status string) *MockCollector {
	m.status = status
	return m
}

// WithServices attaches service states.
func (m *MockCollector) WithServices(svcs ...ServiceLiveState) *MockCollector {
	m.services = append(m.services, svcs...)
	return m
}

// WithErrors attaches error signatures.
func (m *MockCollector) WithErrors(errs ...RecentErrorSignature) *MockCollector {
	m.errors = append(m.errors, errs...)
	return m
}

// WithConvergence attaches convergence states.
func (m *MockCollector) WithConvergence(states ...RuntimeConvergenceState) *MockCollector {
	m.conv = append(m.conv, states...)
	return m
}

// WithIncidents attaches active incidents.
func (m *MockCollector) WithIncidents(incs ...ActiveClusterIncident) *MockCollector {
	m.incidents = append(m.incidents, incs...)
	return m
}

// WithSleep makes the collector sleep (for timeout tests).
func (m *MockCollector) WithSleep(d time.Duration) *MockCollector {
	m.sleep = d
	return m
}

func (m *MockCollector) Name() string { return m.name }

func (m *MockCollector) Available(_ context.Context) bool {
	return m.status == "ok" || m.status == "degraded"
}

func (m *MockCollector) Collect(ctx context.Context, req CollectSignalsRequest) (*SignalSourceResult, error) {
	if m.sleep > 0 {
		select {
		case <-time.After(m.sleep):
		case <-ctx.Done():
			return &SignalSourceResult{
				Source: SignalSourceStatus{
					Name:        m.name,
					Status:      "timeout",
					CollectedAt: time.Now().Unix(),
				},
			}, ctx.Err()
		}
	}
	return &SignalSourceResult{
		Source: SignalSourceStatus{
			Name:        m.name,
			Status:      m.status,
			CollectedAt: time.Now().Unix(),
		},
		Services:    m.services,
		Errors:      m.errors,
		Convergence: m.conv,
		Incidents:   m.incidents,
	}, nil
}

// HealthyCluster returns a set of mock collectors simulating a fully healthy cluster.
func HealthyCluster(services ...string) []SignalCollector {
	svcs := make([]ServiceLiveState, 0, len(services))
	for _, s := range services {
		svcs = append(svcs, ServiceLiveState{
			ServiceName: s,
			Status:      "running",
			Health:      "healthy",
			Readiness:   "ready",
		})
	}
	return []SignalCollector{
		NewMockCollector("health").WithServices(svcs...),
		NewMockCollector("convergence").WithConvergence(),
		NewMockCollector("incidents"),
	}
}
