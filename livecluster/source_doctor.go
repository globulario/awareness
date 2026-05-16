package livecluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	cluster_doctorpb "github.com/globulario/awareness/cluster_doctorpb"
	"google.golang.org/grpc"
)

// doctorReportClient is the minimal subset of cluster_doctorpb.ClusterDoctorServiceClient
// that the doctor-backed collector calls. Tests inject stubs against this
// narrow interface instead of the full ~10-method client.
type doctorReportClient interface {
	GetClusterReport(ctx context.Context, in *cluster_doctorpb.ClusterReportRequest, opts ...grpc.CallOption) (*cluster_doctorpb.ClusterReport, error)
	GetDriftReport(ctx context.Context, in *cluster_doctorpb.DriftReportRequest, opts ...grpc.CallOption) (*cluster_doctorpb.DriftReport, error)
}

// DoctorClientFactory dials cluster-doctor and returns a client plus a
// release callback that the collector invokes when it is done. Both MCP
// and the CLI provide their own factory so the livecluster package stays
// transport-agnostic.
type DoctorClientFactory func(ctx context.Context) (client cluster_doctorpb.ClusterDoctorServiceClient, release func(), err error)

// DoctorCollector adapts cluster-doctor reports into live cluster signals:
//
//   - Findings with severity ERROR or CRITICAL become ActiveClusterIncidents.
//   - DriftItems become ServiceLiveState entries (UNIT_STOPPED etc.) and/or
//     RuntimeConvergenceState entries (VERSION_MISMATCH, STATE_HASH_MISMATCH).
//
// Transport failures degrade the source to "unavailable" — they never error
// the snapshot, so a downed doctor does not block static-only preflight.
type DoctorCollector struct {
	name    string
	factory DoctorClientFactory
}

// NewDoctorCollector wires a doctor-backed SignalCollector. Name is the
// stable source identifier surfaced in SignalSourceStatus.
func NewDoctorCollector(name string, factory DoctorClientFactory) *DoctorCollector {
	if name == "" {
		name = "doctor"
	}
	return &DoctorCollector{name: name, factory: factory}
}

func (d *DoctorCollector) Name() string { return d.name }

func (d *DoctorCollector) Available(_ context.Context) bool {
	return d != nil && d.factory != nil
}

func (d *DoctorCollector) Collect(ctx context.Context, _ CollectSignalsRequest) (*SignalSourceResult, error) {
	if d == nil || d.factory == nil {
		return notConfigured(d.name), nil
	}

	client, release, err := d.factory(ctx)
	if err != nil {
		return unavailableSource(d.name, err), nil
	}
	if release != nil {
		defer release()
	}
	return d.collectWith(ctx, client), nil
}

// collectWith executes the doctor RPCs against a narrow client interface
// so tests can stub without implementing the full ClusterDoctorServiceClient.
func (d *DoctorCollector) collectWith(ctx context.Context, client doctorReportClient) *SignalSourceResult {
	res := &SignalSourceResult{
		Source: SignalSourceStatus{
			Name:        d.name,
			Status:      "ok",
			CollectedAt: time.Now().Unix(),
		},
	}

	report, rErr := client.GetClusterReport(ctx, &cluster_doctorpb.ClusterReportRequest{
		Freshness: cluster_doctorpb.FreshnessMode_FRESHNESS_CACHED,
	})
	if rErr != nil {
		res.Source.Status = "degraded"
		res.Source.Message = "GetClusterReport: " + rErr.Error()
	} else {
		for _, f := range report.GetFindings() {
			inc := findingToIncident(f)
			if inc != nil {
				res.Incidents = append(res.Incidents, *inc)
			}
		}
	}

	drift, dErr := client.GetDriftReport(ctx, &cluster_doctorpb.DriftReportRequest{
		Freshness: cluster_doctorpb.FreshnessMode_FRESHNESS_CACHED,
	})
	if dErr != nil {
		if res.Source.Status == "ok" {
			res.Source.Status = "degraded"
		}
		res.Source.Message = appendErr(res.Source.Message, "GetDriftReport: "+dErr.Error())
	} else {
		for _, item := range drift.GetItems() {
			if svc, ok := driftItemToServiceState(item); ok {
				res.Services = append(res.Services, svc)
			}
			if cv, ok := driftItemToConvergence(item); ok {
				res.Convergence = append(res.Convergence, cv)
			}
		}
	}

	if rErr != nil && dErr != nil {
		res.Source.Status = "unavailable"
	}
	return res
}

// findingToIncident maps a doctor Finding to an ActiveClusterIncident.
// Only ERROR and CRITICAL findings surface as incidents; lower severities
// are noise at the preflight layer.
func findingToIncident(f *cluster_doctorpb.Finding) *ActiveClusterIncident {
	if f == nil {
		return nil
	}
	sev := f.GetSeverity()
	if sev != cluster_doctorpb.Severity_SEVERITY_ERROR && sev != cluster_doctorpb.Severity_SEVERITY_CRITICAL {
		return nil
	}
	component, service, node := splitEntityRef(f.GetEntityRef(), f.GetCategory())
	severity := "warning"
	if sev == cluster_doctorpb.Severity_SEVERITY_CRITICAL {
		severity = "critical"
	} else if sev == cluster_doctorpb.Severity_SEVERITY_ERROR {
		severity = "critical"
	}
	now := time.Now().Unix()
	return &ActiveClusterIncident{
		IncidentID:  f.GetFindingId(),
		Source:      "doctor",
		Title:       firstNonEmpty(f.GetSummary(), f.GetCategory()),
		Severity:    severity,
		Status:      "active",
		Component:   component,
		ServiceName: service,
		NodeID:      node,
		Summary:     f.GetSummary(),
		StartedAt:   now,
		UpdatedAt:   now,
	}
}

// driftItemToServiceState maps a unit-level drift category to a service
// health observation. Returns (state, true) when the category implies a
// runtime health signal; otherwise (zero, false).
func driftItemToServiceState(item *cluster_doctorpb.DriftItem) (ServiceLiveState, bool) {
	if item == nil {
		return ServiceLiveState{}, false
	}
	node, _, service := splitNodeUnit(item.GetEntityRef(), item.GetNodeId())
	state := ServiceLiveState{
		ServiceName: service,
		NodeID:      node,
		LastError:   item.GetActual(),
	}
	switch item.GetCategory() {
	case cluster_doctorpb.DriftCategory_UNIT_STOPPED:
		state.Status = "stopped"
		state.Health = "unhealthy"
		state.Readiness = "not_ready"
	case cluster_doctorpb.DriftCategory_MISSING_UNIT_FILE:
		state.Status = "stopped"
		state.Health = "unreachable"
		state.Readiness = "not_ready"
	case cluster_doctorpb.DriftCategory_UNIT_DISABLED:
		state.Status = "stopped"
		state.Health = "degraded"
		state.Readiness = "not_ready"
	case cluster_doctorpb.DriftCategory_ENDPOINT_MISSING:
		state.Health = "unreachable"
		state.Readiness = "not_ready"
	default:
		return ServiceLiveState{}, false
	}
	if state.ServiceName == "" {
		return ServiceLiveState{}, false
	}
	return state, true
}

// driftItemToConvergence maps hash/version mismatches to a RuntimeConvergenceState.
func driftItemToConvergence(item *cluster_doctorpb.DriftItem) (RuntimeConvergenceState, bool) {
	if item == nil {
		return RuntimeConvergenceState{}, false
	}
	switch item.GetCategory() {
	case cluster_doctorpb.DriftCategory_VERSION_MISMATCH,
		cluster_doctorpb.DriftCategory_STATE_HASH_MISMATCH:
		// fall through
	default:
		return RuntimeConvergenceState{}, false
	}
	_, _, service := splitNodeUnit(item.GetEntityRef(), item.GetNodeId())
	cv := RuntimeConvergenceState{
		Component:         firstNonEmpty(service, item.GetEntityRef()),
		DesiredState:      item.GetDesired(),
		InstalledState:    item.GetActual(),
		RuntimeState:      item.GetActual(),
		ConvergenceStatus: "diverged",
		BlockedReason:     fmt.Sprintf("doctor drift: %s", driftCategoryShort(item.GetCategory())),
		RelatedKey:        item.GetEntityRef(),
	}
	if cv.Component == "" {
		return RuntimeConvergenceState{}, false
	}
	return cv, true
}

// splitEntityRef best-effort splits "node/service" or service-only refs into
// (component, service, node). category is a hint when the ref is ambiguous.
func splitEntityRef(ref, category string) (component, service, node string) {
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == "cluster" {
		return "", "", ""
	}
	if i := strings.Index(ref, "/"); i > 0 {
		node = ref[:i]
		service = strings.TrimSuffix(ref[i+1:], ".service")
		component = service
		return
	}
	// Lone token — heuristic: if it looks like a hostname (contains "-" or "."),
	// assume node; otherwise assume service.
	if strings.ContainsAny(ref, ".-") && !strings.HasSuffix(ref, ".service") {
		node = ref
		return
	}
	service = strings.TrimSuffix(ref, ".service")
	component = service
	_ = category
	return
}

// splitNodeUnit splits drift entity_ref "<node>/<unit>.service" into parts,
// falling back to node_id when the ref is service-only.
func splitNodeUnit(ref, nodeID string) (node, unit, service string) {
	ref = strings.TrimSpace(ref)
	node = nodeID
	if i := strings.Index(ref, "/"); i > 0 {
		if node == "" {
			node = ref[:i]
		}
		unit = ref[i+1:]
	} else {
		unit = ref
	}
	service = strings.TrimSuffix(unit, ".service")
	return
}

func driftCategoryShort(c cluster_doctorpb.DriftCategory) string {
	switch c {
	case cluster_doctorpb.DriftCategory_MISSING_UNIT_FILE:
		return "missing_unit_file"
	case cluster_doctorpb.DriftCategory_UNIT_STOPPED:
		return "unit_stopped"
	case cluster_doctorpb.DriftCategory_UNIT_DISABLED:
		return "unit_disabled"
	case cluster_doctorpb.DriftCategory_VERSION_MISMATCH:
		return "version_mismatch"
	case cluster_doctorpb.DriftCategory_STATE_HASH_MISMATCH:
		return "state_hash_mismatch"
	case cluster_doctorpb.DriftCategory_ENDPOINT_MISSING:
		return "endpoint_missing"
	case cluster_doctorpb.DriftCategory_INVENTORY_INCOMPLETE:
		return "inventory_incomplete"
	}
	return "unknown"
}

func notConfigured(name string) *SignalSourceResult {
	return &SignalSourceResult{
		Source: SignalSourceStatus{
			Name:        name,
			Status:      "not_configured",
			CollectedAt: time.Now().Unix(),
			Message:     "collector factory unset",
		},
	}
}

func unavailableSource(name string, err error) *SignalSourceResult {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return &SignalSourceResult{
		Source: SignalSourceStatus{
			Name:        name,
			Status:      "unavailable",
			CollectedAt: time.Now().Unix(),
			Message:     msg,
		},
	}
}

func appendErr(existing, more string) string {
	if existing == "" {
		return more
	}
	return existing + "; " + more
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
