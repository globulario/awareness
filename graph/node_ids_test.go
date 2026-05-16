package graph

import "testing"

func TestFailureModeNodeID_AddsPrefix(t *testing.T) {
	if got := FailureModeNodeID("etcd.leader_instability"); got != "failure_mode:etcd.leader_instability" {
		t.Errorf("FailureModeNodeID = %q, want %q", got, "failure_mode:etcd.leader_instability")
	}
}

func TestFailureModeNodeID_Idempotent(t *testing.T) {
	already := "failure_mode:etcd.leader_instability"
	if got := FailureModeNodeID(already); got != already {
		t.Errorf("FailureModeNodeID idempotent broke: %q -> %q", already, got)
	}
}

func TestFailureModeNodeID_Empty(t *testing.T) {
	if got := FailureModeNodeID(""); got != "" {
		t.Errorf("FailureModeNodeID(empty) = %q, want empty", got)
	}
}

func TestFailureModeIDFromNode_StripsPrefix(t *testing.T) {
	if got := FailureModeIDFromNode("failure_mode:etcd.leader_instability"); got != "etcd.leader_instability" {
		t.Errorf("FailureModeIDFromNode = %q, want %q", got, "etcd.leader_instability")
	}
}

func TestFailureModeIDFromNode_NoPrefixUnchanged(t *testing.T) {
	bare := "etcd.leader_instability"
	if got := FailureModeIDFromNode(bare); got != bare {
		t.Errorf("FailureModeIDFromNode mangled unprefixed id: %q -> %q", bare, got)
	}
}

func TestIsFailureModeNode(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"failure_mode:x", true},
		{"failure_mode:", true},
		{"invariant:x", false},
		{"", false},
		{"x", false},
	}
	for _, c := range cases {
		if got := IsFailureModeNode(c.in); got != c.want {
			t.Errorf("IsFailureModeNode(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, id := range []string{"etcd.leader_instability", "workflow.resume_poisoning", "x"} {
		if got := FailureModeIDFromNode(FailureModeNodeID(id)); got != id {
			t.Errorf("round-trip failed for %q -> %q", id, got)
		}
	}
}
