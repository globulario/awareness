package livecluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/globulario/awareness/graph"
	"github.com/google/uuid"
)

// RunLivePreflight combines a live cluster signal snapshot with static graph context
// to produce a combined verdict for the given task and files.
func RunLivePreflight(ctx context.Context, g *graph.Graph, st *Store, collectors []SignalCollector, req LivePreflightRequest) (*LivePreflightResult, error) {
	if req.LookbackHours == 0 {
		req.LookbackHours = 24
	}

	// Derive components and services from files if not provided.
	components := req.Components
	if len(components) == 0 && len(req.Files) > 0 {
		components = MapFilesToComponents(ctx, g, req.Files)
	}
	services := req.Services
	if len(services) == 0 {
		services = MapComponentsToServices(ctx, g, components)
	}

	// Collect live signals.
	sigReq := CollectSignalsRequest{
		SessionID:       req.SessionID,
		Task:            req.Task,
		Files:           req.Files,
		Components:      components,
		Services:        services,
		LookbackHours:   req.LookbackHours,
		RequireLiveData: req.RequireLiveData,
	}
	snap, err := CollectClusterSignals(ctx, sigReq, collectors)
	if err != nil {
		return nil, fmt.Errorf("livecluster: collect signals: %w", err)
	}

	// Persist the snapshot.
	if st != nil {
		_ = st.StoreClusterSignalSnapshot(ctx, snap)
	}

	// Check if required live data is missing.
	if req.RequireLiveData {
		unavailCount := 0
		for _, src := range snap.Sources {
			if src.Status == "unavailable" || src.Status == "timeout" {
				unavailCount++
			}
		}
		if unavailCount > 0 && len(snap.Sources) > 0 && unavailCount == len(snap.Sources) {
			r := buildResult(req, components, snap.ID, req.StaticResultID,
				"block", "critical",
				"Live data required but all signal collectors unavailable.",
				[]LivePreflightFinding{{
					Kind:     "source_unavailable",
					Severity: "critical",
					Message:  "All signal sources unavailable; cannot satisfy require_live_data.",
				}}, nil, nil)
			if st != nil {
				_ = st.StoreLivePreflightResult(ctx, r)
			}
			return r, nil
		}
	}

	// Evaluate findings.
	blockers := evaluateActiveIncidents(snap, components, services)
	blockers = append(blockers, evaluateConvergence(snap, components, services)...)
	blockers = append(blockers, evaluateServiceHealth(snap, services)...)
	blockers = append(blockers, evaluateRepeatedErrors(snap, components, services)...)

	warnings, confirmations := evaluateWarningsAndConfirmations(snap, components, services, req.RequireLiveData)

	verdict, severity := computeVerdict(blockers, warnings, snap, req.RequireLiveData)
	summary := buildPreflightSummary(verdict, blockers, warnings, confirmations, snap)

	r := buildResult(req, components, snap.ID, req.StaticResultID, verdict, severity, summary, blockers, warnings, confirmations)
	if st != nil {
		_ = st.StoreLivePreflightResult(ctx, r)
	}
	return r, nil
}

// ── evaluators ────────────────────────────────────────────────────────────────

func evaluateActiveIncidents(snap *ClusterSignalSnapshot, components, services []string) []LivePreflightFinding {
	var findings []LivePreflightFinding
	for _, inc := range snap.Incidents {
		if inc.Status != "active" && inc.Status != "investigating" {
			continue
		}
		if !touches(inc.Component, components) && !touches(inc.ServiceName, services) {
			continue
		}
		sev := "warning"
		if inc.Severity == "critical" {
			sev = "critical"
		}
		findings = append(findings, LivePreflightFinding{
			Kind:        "active_incident",
			Severity:    sev,
			Component:   inc.Component,
			ServiceName: inc.ServiceName,
			Message:     fmt.Sprintf("Active incident: %s", inc.Title),
			Evidence:    inc.Summary,
		})
	}
	return findings
}

func evaluateConvergence(snap *ClusterSignalSnapshot, components, services []string) []LivePreflightFinding {
	var findings []LivePreflightFinding
	// Use services as fallback for matching when components is empty.
	matchList := components
	if len(matchList) == 0 {
		matchList = services
	}
	for _, c := range snap.Convergence {
		if !touches(c.Component, matchList) {
			continue
		}
		if c.ConvergenceStatus == "stuck" || c.ConvergenceStatus == "diverged" || c.ConvergenceStatus == "blocked" {
			evidence := c.BlockedReason
			if c.RetryCount > 0 {
				evidence = fmt.Sprintf("retry_count=%d age=%ds %s", c.RetryCount, c.AgeSeconds, c.BlockedReason)
			}
			findings = append(findings, LivePreflightFinding{
				Kind:      "convergence",
				Severity:  "critical",
				Component: c.Component,
				Message:   fmt.Sprintf("Convergence %s for %s", c.ConvergenceStatus, c.Component),
				Evidence:  evidence,
			})
		}
	}
	return findings
}

func evaluateServiceHealth(snap *ClusterSignalSnapshot, services []string) []LivePreflightFinding {
	var findings []LivePreflightFinding
	for _, svc := range snap.Services {
		if !touches(svc.ServiceName, services) {
			continue
		}
		switch svc.Health {
		case "unhealthy", "unreachable":
			evidence := svc.LastError
			if evidence == "" {
				evidence = fmt.Sprintf("status=%s readiness=%s", svc.Status, svc.Readiness)
			}
			findings = append(findings, LivePreflightFinding{
				Kind:        "service_health",
				Severity:    "critical",
				ServiceName: svc.ServiceName,
				Message:     fmt.Sprintf("%s is %s", svc.ServiceName, svc.Health),
				Evidence:    evidence,
			})
		}
	}
	return findings
}

func evaluateRepeatedErrors(snap *ClusterSignalSnapshot, components, services []string) []LivePreflightFinding {
	var findings []LivePreflightFinding
	for _, e := range snap.Errors {
		if e.Severity != "critical" {
			continue
		}
		if !touches(e.Component, components) && !touches(e.ServiceName, services) {
			continue
		}
		if e.Count >= 10 {
			findings = append(findings, LivePreflightFinding{
				Kind:        "recent_errors",
				Severity:    "critical",
				Component:   e.Component,
				ServiceName: e.ServiceName,
				Message:     fmt.Sprintf("%d repeated critical errors: %s", e.Count, e.Signature),
				Evidence:    fmt.Sprintf("first=%s last=%s sample=%s", tsStr(e.FirstSeen), tsStr(e.LastSeen), e.Sample),
			})
		}
	}
	return findings
}

func evaluateWarningsAndConfirmations(snap *ClusterSignalSnapshot, components, services []string, requireLive bool) (warnings, confirmations []LivePreflightFinding) {
	// Source unavailability → warning.
	for _, src := range snap.Sources {
		if src.Status == "unavailable" || src.Status == "timeout" {
			warnings = append(warnings, LivePreflightFinding{
				Kind:     "source_unavailable",
				Severity: "warning",
				Message:  fmt.Sprintf("Signal source %q unavailable (%s)", src.Name, src.Status),
				Evidence: src.Message,
			})
		}
	}

	// Degraded services → warning.
	for _, svc := range snap.Services {
		if !touches(svc.ServiceName, services) {
			continue
		}
		if svc.Health == "degraded" {
			warnings = append(warnings, LivePreflightFinding{
				Kind:        "service_health",
				Severity:    "warning",
				ServiceName: svc.ServiceName,
				Message:     fmt.Sprintf("%s is degraded", svc.ServiceName),
				Evidence:    svc.LastError,
			})
		}
		if svc.Health == "healthy" {
			confirmations = append(confirmations, LivePreflightFinding{
				Kind:        "service_health",
				Severity:    "info",
				ServiceName: svc.ServiceName,
				Message:     fmt.Sprintf("%s is healthy", svc.ServiceName),
			})
		}
	}

	// Pending (non-stuck) convergence → warning.
	for _, c := range snap.Convergence {
		if !touches(c.Component, components) {
			continue
		}
		if c.ConvergenceStatus == "pending" || c.ConvergenceStatus == "in_progress" {
			warnings = append(warnings, LivePreflightFinding{
				Kind:      "convergence",
				Severity:  "warning",
				Component: c.Component,
				Message:   fmt.Sprintf("Convergence %s for %s", c.ConvergenceStatus, c.Component),
			})
		}
		if c.ConvergenceStatus == "converged" {
			confirmations = append(confirmations, LivePreflightFinding{
				Kind:      "convergence",
				Severity:  "info",
				Component: c.Component,
				Message:   fmt.Sprintf("%s is converged", c.Component),
			})
		}
	}

	_ = requireLive
	return
}

// computeVerdict determines the final verdict.
func computeVerdict(blockers, warnings []LivePreflightFinding, snap *ClusterSignalSnapshot, requireLive bool) (verdict, severity string) {
	if len(blockers) > 0 {
		return "block", "critical"
	}

	// If require_live and no sources collected anything, return unknown.
	if requireLive && len(snap.Sources) == 0 {
		return "unknown", "warning"
	}

	if len(warnings) > 0 {
		return "allow_with_warnings", "warning"
	}
	return "allow", "info"
}

func buildPreflightSummary(verdict string, blockers, warnings, confirmations []LivePreflightFinding, snap *ClusterSignalSnapshot) string {
	var parts []string
	switch verdict {
	case "block":
		parts = append(parts, fmt.Sprintf("BLOCKED: %d blocker(s) on live cluster.", len(blockers)))
		for _, b := range blockers {
			parts = append(parts, "  • "+b.Message)
		}
	case "allow_with_warnings":
		parts = append(parts, fmt.Sprintf("ALLOW WITH WARNINGS: %d warning(s).", len(warnings)))
		if len(confirmations) > 0 {
			parts = append(parts, fmt.Sprintf("  %d signal(s) confirmed healthy.", len(confirmations)))
		}
	case "allow":
		parts = append(parts, fmt.Sprintf("ALLOW: %d signal(s) confirmed, no blockers.", len(confirmations)))
	case "unknown":
		parts = append(parts, "UNKNOWN: live cluster signals could not be collected.")
	}
	parts = append(parts, "Snapshot: "+snap.Summary)
	return strings.Join(parts, " ")
}

func buildResult(req LivePreflightRequest, components []string, snapID, staticID, verdict, severity, summary string,
	blockers, warnings, confirmations []LivePreflightFinding) *LivePreflightResult {
	return &LivePreflightResult{
		ID:               "LPF-" + uuid.New().String()[:8],
		SessionID:        req.SessionID,
		Task:             req.Task,
		Files:            req.Files,
		Components:       components,
		StaticResultID:   staticID,
		SignalSnapshotID: snapID,
		Verdict:          verdict,
		Severity:         severity,
		Summary:          summary,
		Blockers:         blockers,
		Warnings:         warnings,
		Confirmations:    confirmations,
	}
}

// touches returns true if target matches any element in the list (case-insensitive, partial).
func touches(target string, list []string) bool {
	if target == "" {
		return false
	}
	tl := strings.ToLower(target)
	for _, item := range list {
		il := strings.ToLower(item)
		if tl == il || strings.Contains(tl, il) || strings.Contains(il, tl) {
			return true
		}
	}
	return false
}

func tsStr(unix int64) string {
	if unix == 0 {
		return "?"
	}
	return time.Unix(unix, 0).UTC().Format("15:04:05")
}
