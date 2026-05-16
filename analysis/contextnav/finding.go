package contextnav

// finding.go — Phase 10 entry point for the `awareness.finding_context`
// / `globular awareness finding-context` surface. Lets callers ask
// "given this exact finding id, give me the full decision trace" without
// running a full preflight. Useful when the agent already knows which
// failure_mode / invariant / forbidden_fix it wants to dig into.
//
// The function is a thin wrapper around Build with a single
// pre-populated finding list. Owner inference, pivots, falsifiers, next
// actions, and cross-cutting evidence all flow through the normal Build
// path so the output is shape-compatible with whatever preflight.Run
// would have produced for the same finding.

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/awareness/graph"
)

// FindingContextOptions tunes a single-finding trace build. Most fields
// mirror the BuildInputs counterparts; the ones unique to this path are
// at the top.
type FindingContextOptions struct {
	// Kind is one of "failure_mode", "invariant", "forbidden_fix".
	// Required.
	Kind string
	// ID is the bare finding id (no prefix). Required.
	ID string

	Graph *graph.Graph
	Task  string
	Files []string

	// IncludeRuntime gates runtime-flavoured pivots and evidence. When
	// false, Build's pivot inference skips runtime nodes — matches the
	// "agent doesn't want stale runtime hints" path.
	IncludeRuntime bool

	// Trust* and Experiences/Metrics/FixCases let the caller supply
	// cross-cutting evidence when they already have it (e.g. from a
	// recent preflight). Empty values disable the corresponding
	// EvidenceRef appender.
	TrustVerdict    string
	TrustConfidence string
	TrustFreshness  string
	TrustReason     string
	FixCases        []FixCaseRef
	FixLedgerGaps   []string
	Experiences     []ExperienceRef
	MetricWarnings  []string
}

// ParseFindingID splits a prefixed finding id like "failure_mode:X" into
// (kind, bare-id). Returns ("", "", error) when the input is not in the
// expected shape.
func ParseFindingID(prefixed string) (string, string, error) {
	prefixed = strings.TrimSpace(prefixed)
	if prefixed == "" {
		return "", "", fmt.Errorf("empty finding id")
	}
	parts := strings.SplitN(prefixed, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("finding id %q is not in the form <kind>:<id>", prefixed)
	}
	kind := strings.ToLower(parts[0])
	switch kind {
	case "failure_mode", "invariant", "forbidden_fix":
		return kind, parts[1], nil
	}
	return "", "", fmt.Errorf("unsupported finding kind %q (want failure_mode|invariant|forbidden_fix)", kind)
}

// BuildForFinding produces a single DecisionTrace for an explicit
// finding id, running owner inference + pivots + falsifiers + next
// actions + cross-cutting evidence through the same Build path that
// preflight uses. Returns an error only when opts.Kind / opts.ID are
// missing or invalid — a non-existent finding still gets a trace shell
// so callers can render "no graph anchor for this id" via the trace's
// own warnings.
func BuildForFinding(ctx context.Context, opts FindingContextOptions) (DecisionTrace, error) {
	if opts.Kind == "" || opts.ID == "" {
		return DecisionTrace{}, fmt.Errorf("BuildForFinding: Kind and ID are required")
	}
	in := BuildInputs{
		Confidence:          ConfidenceMedium,
		GraphFreshnessKnown: false,
		Graph:               opts.Graph,
		Ctx:                 ctx,
		Task:                opts.Task,
		Files:               append([]string(nil), opts.Files...),
		LiveOverlayStatus:   "",
		TrustVerdict:        opts.TrustVerdict,
		TrustConfidence:     opts.TrustConfidence,
		TrustFreshness:      opts.TrustFreshness,
		TrustReason:         opts.TrustReason,
		FixCases:            append([]FixCaseRef(nil), opts.FixCases...),
		FixLedgerGaps:       append([]string(nil), opts.FixLedgerGaps...),
		Experiences:         append([]ExperienceRef(nil), opts.Experiences...),
		MetricWarnings:      append([]string(nil), opts.MetricWarnings...),
	}
	if opts.IncludeRuntime {
		// Mark runtime as freshly observed for THIS finding so pivots
		// surface runtime nodes even when no preflight overlay was
		// attached. Caller takes responsibility for the include-runtime
		// claim.
		in.LiveOverlayStatus = "fresh"
	}
	switch opts.Kind {
	case "failure_mode":
		in.FailureModes = []string{opts.ID}
	case "invariant":
		in.Invariants = []string{opts.ID}
	case "forbidden_fix":
		in.ForbiddenFixes = []string{opts.ID}
	default:
		return DecisionTrace{}, fmt.Errorf("BuildForFinding: unsupported Kind %q", opts.Kind)
	}
	traces := Build(in)
	if len(traces) == 0 {
		// Should not happen — Build emits one trace per finding — but
		// guard so callers always get a usable record.
		return DecisionTrace{
			FindingID:   opts.ID,
			FindingType: FindingType(opts.Kind),
			Confidence:  ConfidenceUnknown,
			Warnings:    []string{"BuildForFinding: no trace produced (internal)"},
		}, nil
	}
	return traces[0], nil
}
