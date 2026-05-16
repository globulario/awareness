package runtime

import (
	"fmt"
	"strings"
	"time"
)

// StateDelta captures a mismatch between desired and installed state.
type StateDelta struct {
	ServiceID        string
	NodeID           string
	DesiredVersion   string
	InstalledVersion string
	DesiredBuildID   string
	InstalledBuildID string
	DeltaType        string // VERSION_MISMATCH, BUILD_ID_MISMATCH, MISSING_INSTALLED
}

// RuntimeSnapshot is a point-in-time read-only view of cluster state.
// All fields are populated by the RuntimeBridge.Snapshot() call.
type RuntimeSnapshot struct {
	ID         string
	CapturedAt time.Time
	NodeID     string
	ClusterID  string

	// Source evidence (populated from sources).
	DoctorFindings    []DoctorFinding
	RecentEvents      []RuntimeEvent
	WorkflowReceipts  []WorkflowReceipt
	DesiredState      []DesiredStateRecord
	InstalledState    []InstalledStateRecord
	RuntimeServices   []ServiceStatus
	RepositoryStatus  []RepositoryStatus
	ObjectstoreStatus []ObjectstoreStatus
	XDSStatus         []XDSStatus
	SystemdUnits      []SystemdUnit
	Metrics           []MetricSample
	SourceHealth      []SourceHealth

	// Computed by Match().
	StateDelta          []StateDelta
	MatchedInvariants   []string
	MatchedFailureModes []string

	// Operational metadata.
	// SourceWarnings holds warnings from source collection (set by the bridge before Match).
	// Warnings is the combined view: SourceWarnings + warnings derived during Match.
	// Match always recomputes Warnings from SourceWarnings + fresh derived warnings,
	// ensuring idempotent behaviour.
	SourceWarnings []string
	Warnings       []string
	Errors         []string
}

// MatchWithThresholds is like Match but uses the provided thresholds for metric evaluation.
// If thresholds is nil, built-in defaults are used (same as Match).
func (s *RuntimeSnapshot) MatchWithThresholds(knownInvariants, knownFMs []string, thresholds *MetricThresholds) *RuntimeSnapshot {
	if thresholds == nil {
		thresholds = &MetricThresholds{}
	}
	out := *s
	// Reset computed fields so we don't double-append on successive calls.
	out.StateDelta = nil
	out.MatchedInvariants = nil
	out.MatchedFailureModes = nil
	out.Errors = append([]string(nil), s.Errors...)
	// Derived warnings start fresh; SourceWarnings are the stable base.
	var derivedWarnings []string

	invSet := make(map[string]bool)
	fmSet := make(map[string]bool)

	addInv := func(id string) {
		if id != "" && !invSet[id] {
			invSet[id] = true
			out.MatchedInvariants = append(out.MatchedInvariants, id)
		}
	}
	addFM := func(id string) {
		if id != "" && !fmSet[id] {
			fmSet[id] = true
			out.MatchedFailureModes = append(out.MatchedFailureModes, id)
		}
	}

	// Doctor findings → invariants.
	for _, f := range s.DoctorFindings {
		if !f.Suppressed {
			addInv(matchFindingToInvariant(f, knownInvariants))
		}
	}

	// Workflow failures → failure modes.
	for _, r := range s.WorkflowReceipts {
		addFM(matchWorkflowToFailureMode(r, knownFMs))
	}

	// Desired/installed mismatch → StateDelta.
	installed := make(map[string]InstalledStateRecord)
	for _, ins := range s.InstalledState {
		installed[ins.ServiceID+"/"+ins.NodeID] = ins
	}
	for _, des := range s.DesiredState {
		key := des.ServiceID + "/" + s.NodeID
		ins, ok := installed[key]
		if !ok {
			// Try any node.
			for _, i := range s.InstalledState {
				if i.ServiceID == des.ServiceID {
					ins = i
					ok = true
					break
				}
			}
		}
		if !ok {
			out.StateDelta = append(out.StateDelta, StateDelta{
				ServiceID:      des.ServiceID,
				NodeID:         s.NodeID,
				DesiredVersion: des.Version,
				DeltaType:      "MISSING_INSTALLED",
			})
			continue
		}
		if des.Version != "" && ins.Version != "" && des.Version != ins.Version {
			out.StateDelta = append(out.StateDelta, StateDelta{
				ServiceID:        des.ServiceID,
				NodeID:           ins.NodeID,
				DesiredVersion:   des.Version,
				InstalledVersion: ins.Version,
				DesiredBuildID:   des.BuildID,
				InstalledBuildID: ins.BuildID,
				DeltaType:        "VERSION_MISMATCH",
			})
		} else if des.BuildID != "" && ins.BuildID != "" && des.BuildID != ins.BuildID {
			out.StateDelta = append(out.StateDelta, StateDelta{
				ServiceID:        des.ServiceID,
				NodeID:           ins.NodeID,
				DesiredVersion:   des.Version,
				InstalledVersion: ins.Version,
				DesiredBuildID:   des.BuildID,
				InstalledBuildID: ins.BuildID,
				DeltaType:        "BUILD_ID_MISMATCH",
			})
		}
	}

	// Systemd start-limit-hit → service.restart_singleflight invariant.
	for _, u := range s.SystemdUnits {
		if strings.Contains(strings.ToLower(u.SubState), "start-limit") {
			addInv(findInvariantByPattern("restart_singleflight", knownInvariants))
			derivedWarnings = append(derivedWarnings, fmt.Sprintf("systemd start-limit-hit: %s on node %s", u.UnitName, u.NodeID))
		}
	}

	// Repository DEGRADED/READ_ONLY/LOCAL_ONLY → repository.metadata_first invariant.
	for _, rs := range s.RepositoryStatus {
		switch rs.Mode {
		case "DEGRADED", "READ_ONLY", "LOCAL_ONLY":
			addInv(findInvariantByPattern("metadata_first", knownInvariants))
			derivedWarnings = append(derivedWarnings, fmt.Sprintf("repository %s on node %s: %s", rs.Mode, rs.NodeID, rs.LastError))
		}
	}

	// Objectstore topology mismatch → objectstore.topology_contract invariant.
	for _, os := range s.ObjectstoreStatus {
		if !os.TopologyMatch {
			addInv(findInvariantByPattern("topology_contract", knownInvariants))
			derivedWarnings = append(derivedWarnings, fmt.Sprintf("objectstore topology mismatch: got %d nodes, expected %d", os.NodeCount, os.ExpectedCount))
		}
	}

	// xDS no applied generation.
	for _, x := range s.XDSStatus {
		if x.AppliedGeneration == 0 && x.PendingGeneration > 0 {
			derivedWarnings = append(derivedWarnings, fmt.Sprintf("xDS node %s has pending generation %d but no applied generation", x.NodeID, x.PendingGeneration))
		}
	}

	// Dynamic metric risk using the provided thresholds.
	for _, m := range s.Metrics {
		if w, _ := thresholds.Evaluate(m); w != "" {
			derivedWarnings = append(derivedWarnings, w)
			continue
		}
		// Error signal metrics (any name containing "error" with value > 0).
		if strings.Contains(strings.ToLower(m.Name), "error") && m.Value > 0 {
			derivedWarnings = append(derivedWarnings, fmt.Sprintf("metric error signal: %s=%.2f%s node=%s service=%s", m.Name, m.Value, m.Unit, m.NodeID, m.ServiceID))
		}
	}

	// Combine SourceWarnings (stable) with freshly derived warnings.
	out.SourceWarnings = append([]string(nil), s.SourceWarnings...)
	out.Warnings = append(append([]string(nil), out.SourceWarnings...), derivedWarnings...)

	return &out
}

// Match performs invariant and failure-mode matching against the snapshot evidence.
// knownInvariants and knownFMs are IDs loaded from the graph.
// This method is pure — it returns a new snapshot with Match fields populated.
// Calling Match multiple times on the result is idempotent: Warnings is always
// recomputed as SourceWarnings + newly derived warnings.
func (s *RuntimeSnapshot) Match(knownInvariants, knownFMs []string) *RuntimeSnapshot {
	return s.MatchWithThresholds(knownInvariants, knownFMs, nil)
}

// findInvariantByPattern returns the first invariant ID containing pattern.
func findInvariantByPattern(pattern string, knownInvariants []string) string {
	for _, id := range knownInvariants {
		if strings.Contains(id, pattern) {
			return id
		}
	}
	return ""
}
