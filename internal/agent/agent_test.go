package agent

import (
	"testing"
)

// TestParentID_ReturnsParentIDForSubagent is a regression test for the
// HIGH-1 bug discovered during the whole-project review: the old code
// returned a.ID when a.Parent != nil, which meant every subagent
// reported its own ID as its ParentID — silently breaking event routing
// for async subagents and any downstream consumer that read ParentID.
//
// Constructs a hand-rolled Agent so we don't need to spin up an LLM
// client or toolset just to assert a one-line accessor.
func TestParentID_ReturnsParentIDForSubagent(t *testing.T) {
	parent := &Agent{ID: "parent-007"}
	child := &Agent{ID: "child-042", Parent: parent}

	if got := child.ParentID(); got != "parent-007" {
		t.Errorf("ParentID(): got %q, want %q (the bug was returning child.ID)", got, "parent-007")
	}
}

// TestParentID_ReturnsEmptyForRootAgent locks down the other branch:
// a root agent (no parent) returns empty string. Without this companion
// test a "fix" that returns Parent.ID unconditionally would nil-panic.
func TestParentID_ReturnsEmptyForRootAgent(t *testing.T) {
	root := &Agent{ID: "root-001"}
	if got := root.ParentID(); got != "" {
		t.Errorf("ParentID() on root: got %q, want empty", got)
	}
}

// TestIsSubagent_FollowsParentField sanity-checks that IsSubagent uses
// the same field, so the two accessors stay in sync.
func TestIsSubagent_FollowsParentField(t *testing.T) {
	root := &Agent{}
	child := &Agent{Parent: root}
	if root.IsSubagent() {
		t.Error("root.IsSubagent() returned true")
	}
	if !child.IsSubagent() {
		t.Error("child.IsSubagent() returned false")
	}
}
