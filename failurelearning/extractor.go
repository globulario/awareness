package failurelearning

import (
	"context"
	"strings"

	"github.com/globulario/awareness/failuregraph"
	"github.com/globulario/awareness/incidentpattern"
	"github.com/globulario/awareness/sessionoracle"
)

// ExtractFromRequest builds a FailureLearningExtract directly from a ProposeRequest.
// Used when data is supplied inline — closure or direct MCP call.
func ExtractFromRequest(req ProposeRequest) FailureLearningExtract {
	ext := FailureLearningExtract{
		RawErrors:         req.RawErrors,
		Symptoms:          req.Symptoms,
		RootCauses:        req.RootCauses,
		Resolutions:       req.Resolutions,
		WrongFixes:        req.WrongFixes,
		RegressionTests:   req.Tests,
		RelatedFiles:      req.Files,
		RelatedComponents: req.Components,
		RelatedInvariants: req.Invariants,
		RelatedIncidents:  req.Incidents,
		SemanticAtoms:     req.SemanticAtoms,
		LiveSignals:       req.LiveSignals,
		ClosureEvidence:   req.ClosureEvidence,
	}

	// Normalize raw errors while extracting.
	for _, raw := range req.RawErrors {
		norm := failuregraph.NormalizeErrorSignature(raw)
		ext.NormalizedErrors = append(ext.NormalizedErrors, norm)
	}
	return ext
}

// ExtractFromIncident queries the incidentpattern store for the given incidentID
// and builds an extract. Returns error if incident not found.
func ExtractFromIncident(ctx context.Context, incidentID string, ip *incidentpattern.Store, fg *failuregraph.Store) (*FailureLearningExtract, error) {
	pattern, err := ip.LoadPatternByIncident(ctx, incidentID)
	if err != nil {
		return nil, err
	}
	if pattern == nil {
		return nil, nil
	}

	ext := &FailureLearningExtract{}

	// RootCauses from RootCause field (split by newlines, filter empty)
	if pattern.RootCause != "" {
		for _, line := range strings.Split(pattern.RootCause, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				ext.RootCauses = append(ext.RootCauses, line)
			}
		}
	}

	// Resolutions from Lesson field
	if pattern.Lesson != "" {
		for _, line := range strings.Split(pattern.Lesson, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				ext.Resolutions = append(ext.Resolutions, line)
			}
		}
	}

	// WrongFixes from FailedFixes descriptions
	for _, ff := range pattern.FailedFixes {
		if ff.Description != "" {
			ext.WrongFixes = append(ext.WrongFixes, ff.Description)
		}
	}

	// RelatedFiles from PatternFiles
	for _, pf := range pattern.Files {
		if pf.Path != "" {
			ext.RelatedFiles = append(ext.RelatedFiles, pf.Path)
		}
	}

	// RelatedInvariants from PatternInvariants
	for _, pi := range pattern.Invariants {
		if pi.InvariantID != "" {
			ext.RelatedInvariants = append(ext.RelatedInvariants, pi.InvariantID)
		}
	}

	ext.RelatedIncidents = []string{incidentID}

	// Normalize any summary text as a synthetic raw error for matching
	if pattern.Summary != "" {
		ext.RawErrors = append(ext.RawErrors, pattern.Summary)
		ext.NormalizedErrors = append(ext.NormalizedErrors, failuregraph.NormalizeErrorSignature(pattern.Summary))
	}

	return ext, nil
}

// ExtractFromSession queries the sessionoracle for the given sessionID and builds
// an extract from decisions, test results, warnings, and unfinished work.
func ExtractFromSession(ctx context.Context, sessionID string, oracle *sessionoracle.Oracle, fg *failuregraph.Store) (*FailureLearningExtract, error) {
	session, err := oracle.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, nil
	}

	ext := &FailureLearningExtract{}

	// Decisions → root causes and related files/invariants/incidents
	decisions, err := oracle.ListDecisions(ctx, sessionID)
	if err == nil {
		for _, d := range decisions {
			if d.Rationale != "" {
				ext.RootCauses = append(ext.RootCauses, d.Rationale)
			}
			ext.RelatedFiles = append(ext.RelatedFiles, d.RelatedFiles...)
			ext.RelatedInvariants = append(ext.RelatedInvariants, d.RelatedInvariants...)
			ext.RelatedIncidents = append(ext.RelatedIncidents, d.RelatedIncidents...)
		}
	}

	// Test results → regression tests (failures become wrong-fix evidence)
	tests, err := oracle.ListTestResults(ctx, sessionID)
	if err == nil {
		for _, t := range tests {
			if t.Summary != "" {
				ext.RegressionTests = append(ext.RegressionTests, t.Summary)
			}
		}
	}

	// Warnings → symptoms / raw errors
	warnings, err := oracle.ListWarnings(ctx, sessionID)
	if err == nil {
		for _, w := range warnings {
			if w.Message != "" {
				ext.Symptoms = append(ext.Symptoms, w.Message)
				ext.RawErrors = append(ext.RawErrors, w.Message)
				ext.NormalizedErrors = append(ext.NormalizedErrors, failuregraph.NormalizeErrorSignature(w.Message))
			}
		}
	}

	// Unfinished work → surfaced as extra context
	unfinished, err := oracle.ListUnfinishedWork(ctx, sessionID)
	if err == nil {
		for _, uw := range unfinished {
			if uw.Description != "" {
				ext.ClosureEvidence = append(ext.ClosureEvidence, uw.Description)
			}
			ext.RelatedFiles = append(ext.RelatedFiles, uw.RelatedFiles...)
			ext.RelatedIncidents = append(ext.RelatedIncidents, uw.RelatedIncidents...)
		}
	}

	// Session objective as a loose raw error for matching
	if session.Objective != "" {
		ext.RawErrors = append(ext.RawErrors, session.Objective)
		ext.NormalizedErrors = append(ext.NormalizedErrors, failuregraph.NormalizeErrorSignature(session.Objective))
	}

	return ext, nil
}
