package project

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ValidationError collects all profile validation failures.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return "profile validation failed:\n  - " + strings.Join(e.Errors, "\n  - ")
}

// Validate checks a ProjectProfile for required fields and internal
// consistency. Returns nil when the profile is valid.
func Validate(p *ProjectProfile) error {
	var errs []string

	if p.Name == "" {
		errs = append(errs, "name is required")
	}
	if p.Root == "" {
		errs = append(errs, "root path is empty")
	}
	if p.Runtime.Enabled {
		switch p.Runtime.Adapter {
		case "null", "globular", "":
		default:
			// Unknown adapter — allowed (future adapters).
		}
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// IsValidationError returns true when err is a *ValidationError.
func IsValidationError(err error) bool {
	var v *ValidationError
	return errors.As(err, &v)
}

// DoctorReport summarises the health of a resolved profile.
// All checks are static (filesystem + config); no runtime adapter is called.
type DoctorReport struct {
	Project       string
	Kind          string
	Root          string
	ConfigPath    string
	RuntimeStatus string
	GraphCache    string
	Checks        []DoctorCheck
	OK            bool
}

// DoctorCheck is one line in the profile doctor output.
type DoctorCheck struct {
	Name   string
	Status string // "ok" | "warn" | "error"
	Detail string
}

// Doctor runs static health checks on a ProjectProfile and returns a report.
func Doctor(p *ProjectProfile) *DoctorReport {
	r := &DoctorReport{
		Project:    p.Name,
		Kind:       string(p.Kind),
		Root:       p.Root,
		ConfigPath: p.ConfigPath,
	}

	if p.IsRuntimeEnabled() {
		r.RuntimeStatus = fmt.Sprintf("adapter:%s", p.Runtime.Adapter)
	} else {
		r.RuntimeStatus = "disabled"
	}
	r.GraphCache = p.Graph.CacheDir

	checkDir := func(label, path string, required bool) {
		if path == "" {
			if required {
				r.Checks = append(r.Checks, DoctorCheck{label, "warn", "not configured"})
			}
			return
		}
		if dirExists(path) {
			r.Checks = append(r.Checks, DoctorCheck{label, "ok", path})
		} else {
			status := "warn"
			if required {
				status = "error"
			}
			r.Checks = append(r.Checks, DoctorCheck{label, status, fmt.Sprintf("missing: %s", path)})
		}
	}
	checkFile := func(label, path string) {
		if path == "" {
			return
		}
		if fileExists(path) {
			r.Checks = append(r.Checks, DoctorCheck{label, "ok", path})
		} else {
			r.Checks = append(r.Checks, DoctorCheck{label, "warn", fmt.Sprintf("missing: %s", path)})
		}
	}

	checkDir("awareness.root", p.Awareness.Root, false)
	for _, inv := range p.Awareness.Invariants {
		checkFile("invariants", inv)
	}
	for _, fm := range p.Awareness.FailureModes {
		checkFile("failure_modes", fm)
	}
	for _, ff := range p.Awareness.ForbiddenFixes {
		checkFile("forbidden_fixes", ff)
	}
	checkDir("decisions_dir", p.Awareness.DecisionsDir, false)
	checkDir("proposals_dir", p.Awareness.ProposalsDir, false)
	checkDir("graph.cache_dir", p.Graph.CacheDir, false)

	for _, sr := range p.SourceRoots {
		checkDir("source_root", sr, false)
	}

	r.OK = true
	for _, c := range r.Checks {
		if c.Status == "error" {
			r.OK = false
			break
		}
	}
	return r
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
