package preflight

import (
	"testing"
)

func TestPreflight_ClassifyGenericFindings(t *testing.T) {
	tests := []struct {
		name     string
		task     string
		wantAny  []TaskClass
		wantNone []TaskClass
	}{
		{
			name:    "local code change default",
			task:    "update the README",
			wantAny: []TaskClass{ClassLocalCodeChange},
		},
		{
			name:    "restart storm",
			task:    "service is in restart storm, start-limit-hit",
			wantAny: []TaskClass{ClassRestartStorm, ClassConvergenceRisk},
		},
		{
			name:    "state mismatch",
			task:    "checksum mismatch between installed and desired",
			wantAny: []TaskClass{ClassStateMismatch, ClassConvergenceRisk},
		},
		{
			name:    "architecture sensitive",
			task:    "fix the retry loop in the reconciler",
			wantAny: []TaskClass{ClassArchitectureSensitive},
		},
		{
			name:    "runtime incident",
			task:    "service panicked on startup",
			wantAny: []TaskClass{ClassRuntimeIncident},
		},
		{
			name:    "dependency cycle",
			task:    "there is a deadlock in the workflow engine",
			wantAny: []TaskClass{ClassDependencyCycle},
		},
		{
			name:    "regression",
			task:    "this is a regression from last week",
			wantAny: []TaskClass{ClassConvergenceRisk},
		},
		{
			name:     "no spurious classes for plain task",
			task:     "add a helper function",
			wantNone: []TaskClass{ClassRestartStorm, ClassStateMismatch, ClassRuntimeIncident},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyTask(tt.task)
			for _, want := range tt.wantAny {
				if !HasClass(got, want) {
					t.Errorf("ClassifyTask(%q) missing %s; got %v", tt.task, want, got)
				}
			}
			for _, notWant := range tt.wantNone {
				if HasClass(got, notWant) {
					t.Errorf("ClassifyTask(%q) unexpectedly has %s; got %v", tt.task, notWant, got)
				}
			}
		})
	}
}

func TestPreflight_HasClass(t *testing.T) {
	classes := []TaskClass{ClassLocalCodeChange, ClassArchitectureSensitive}

	if !HasClass(classes, ClassLocalCodeChange) {
		t.Error("HasClass should find ClassLocalCodeChange")
	}
	if !HasClass(classes, ClassArchitectureSensitive) {
		t.Error("HasClass should find ClassArchitectureSensitive")
	}
	if HasClass(classes, ClassRuntimeIncident) {
		t.Error("HasClass should not find ClassRuntimeIncident")
	}
	if HasClass(nil, ClassLocalCodeChange) {
		t.Error("HasClass on nil should return false")
	}
}

func TestPreflight_AppendClass(t *testing.T) {
	var classes []TaskClass
	classes = AppendClass(classes, ClassLocalCodeChange)
	classes = AppendClass(classes, ClassLocalCodeChange) // duplicate
	classes = AppendClass(classes, ClassRuntimeIncident)

	if len(classes) != 2 {
		t.Errorf("AppendClass: want 2 classes, got %d: %v", len(classes), classes)
	}
	if !HasClass(classes, ClassLocalCodeChange) {
		t.Error("AppendClass: missing ClassLocalCodeChange")
	}
	if !HasClass(classes, ClassRuntimeIncident) {
		t.Error("AppendClass: missing ClassRuntimeIncident")
	}
}
