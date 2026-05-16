package preflight_test

import (
	"testing"

	"github.com/globulario/awareness/preflight"
)

func TestClassifyDesiredHashIsMismatch(t *testing.T) {
	classes := preflight.ClassifyTask("desired_hash mismatch between controller and node-agent")
	assertHasClass(t, classes, preflight.ClassStateMismatch)
	assertHasClass(t, classes, preflight.ClassConvergenceRisk)
}

func TestClassifyRestartStorm(t *testing.T) {
	classes := preflight.ClassifyTask("envoy restart storm, start-limit-hit after SIGTERM")
	assertHasClass(t, classes, preflight.ClassRestartStorm)
	assertHasClass(t, classes, preflight.ClassConvergenceRisk)
}

func TestClassifyRetryLoop(t *testing.T) {
	classes := preflight.ClassifyTask("infinite retry loop in workflow step")
	assertHasClass(t, classes, preflight.ClassRetryLoop)
	assertHasClass(t, classes, preflight.ClassArchitectureSensitive)
}

func TestClassifyArchitectureSensitiveKeywords(t *testing.T) {
	cases := []string{
		"convergence proof is broken",
		"desired state does not match installed state",
		"leader failover during bootstrap",
		"build_id is wrong after deploy",
		"checksum mismatch on artifact",
	}
	for _, task := range cases {
		classes := preflight.ClassifyTask(task)
		assertHasClass(t, classes, preflight.ClassArchitectureSensitive)
	}
}

func TestClassifyRegressionRunsDidWeFix(t *testing.T) {
	classes := preflight.ClassifyTask("same bug again in convergence")
	assertHasClass(t, classes, preflight.ClassConvergenceRisk)
}

func TestClassifyLocalCodeChangeWhenNoKeywords(t *testing.T) {
	classes := preflight.ClassifyTask("rename variable in helper utility")
	assertHasClass(t, classes, preflight.ClassLocalCodeChange)
}

func TestClassifyRuntimeIncident(t *testing.T) {
	classes := preflight.ClassifyTask("cluster crash after OOM")
	assertHasClass(t, classes, preflight.ClassRuntimeIncident)
}

func TestClassifyPackageAdmission(t *testing.T) {
	classes := preflight.ClassifyTask("package install of envoy failed admission check")
	assertHasClass(t, classes, preflight.ClassPackageAdmission)
}

func assertHasClass(t *testing.T, classes []preflight.TaskClass, want preflight.TaskClass) {
	t.Helper()
	for _, c := range classes {
		if c == want {
			return
		}
	}
	t.Errorf("expected class %q in %v", want, classes)
}
