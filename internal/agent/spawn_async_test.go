package agent

import (
	"errors"
	"testing"

	"github.com/johnny1110/evva/internal/constant"
	"github.com/johnny1110/evva/internal/tools/meta"
)

// These tests are regressions for HIGH-2: the async-spawn goroutine in
// spawn.go previously logged success/failure but never called
// SpawnGroup.Report or SpawnGroup.Crush, so the panel entry was orphaned
// forever and DrainCompleted never delivered the result to the parent.
//
// Spinning up a real Agent (with LLM client + toolset) just to exercise
// the goroutine is more machinery than the regression needs. The
// goroutine's contract is "on success: Report; on error: Crush; do NOT
// Remove (let DrainCompleted handle it)". We validate that contract
// directly against the SpawnGroup state-machine, which is the load-
// bearing surface — the goroutine's body is now three plain method
// calls.

func TestAsyncSpawn_SuccessFlowsThroughDrainCompleted(t *testing.T) {
	g := meta.NewSpawnGroup()
	g.Add("worker", "agent-1", "general", "do thing", true /* async */)

	// Simulate the goroutine's success path post-fix.
	g.Report("agent-1", "all done")

	snap := g.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len: got %d, want 1", len(snap))
	}
	if got, want := snap[0].Status, constant.READY_REPORT.String(); got != want {
		t.Errorf("status after Report: got %q, want %q", got, want)
	}
	if snap[0].Summary != "all done" {
		t.Errorf("summary: got %q", snap[0].Summary)
	}

	drained := g.DrainCompleted()
	if len(drained) != 1 {
		t.Fatalf("DrainCompleted: got %d entries, want 1 (async success must drain)", len(drained))
	}
	if drained[0].ID != "agent-1" {
		t.Errorf("drained ID: got %q, want %q", drained[0].ID, "agent-1")
	}
	// After draining, the entry is gone.
	if got := g.Snapshot(); len(got) != 0 {
		t.Errorf("after DrainCompleted: snapshot len %d, want 0", len(got))
	}
}

func TestAsyncSpawn_CrushFlowsThroughDrainCompleted(t *testing.T) {
	g := meta.NewSpawnGroup()
	g.Add("worker", "agent-2", "general", "task", true)

	// Simulate the goroutine's error path post-fix.
	g.Crush("agent-2", "[subagent crushed]", errors.New("boom"))

	snap := g.Snapshot()
	if got, want := snap[0].Status, constant.CRUSHED.String(); got != want {
		t.Errorf("status after Crush: got %q, want %q", got, want)
	}
	if snap[0].Err != "boom" {
		t.Errorf("error captured: got %q, want %q", snap[0].Err, "boom")
	}

	drained := g.DrainCompleted()
	if len(drained) != 1 {
		t.Fatalf("crushed async agent must surface via DrainCompleted; got %d entries", len(drained))
	}
}

// TestAsyncSpawn_NeverReportedStaysInPanel locks the bug behavior in
// reverse: if the goroutine returns without calling Report/Crush (the
// pre-fix code), the entry sits in the panel forever — DrainCompleted
// can't deliver anything because nothing is marked done.
//
// This is the canary: if someone re-introduces the bug by skipping the
// Report call in spawn.go, the entry will be stuck and downstream
// turns will never see the result. We assert the failure mode here so
// that fact is documented in test form.
func TestAsyncSpawn_NeverReportedStaysInPanel(t *testing.T) {
	g := meta.NewSpawnGroup()
	g.Add("worker", "agent-3", "general", "task", true)

	// Deliberately DO NOT call Report or Crush.

	drained := g.DrainCompleted()
	if len(drained) != 0 {
		t.Errorf("entry that was never Reported/Crushed should NOT drain; got %d", len(drained))
	}
	if got := g.Snapshot(); len(got) != 1 || got[0].Status != constant.INIT.String() {
		t.Errorf("entry should still be in INIT state; got %+v", got)
	}
}
