package dream

import (
	"strings"
	"testing"
)

func TestBuildPrompt(t *testing.T) {
	out := BuildPrompt("/home/u/memory", "/home/u/sessions", 7)

	// The 4 phases, the no-promotion guard, the interpolated paths/count, and
	// the evva grep-TOOL form are all present.
	for _, want := range []string{
		"# Dream: Memory Consolidation",
		"## Phase 1 — Orient",
		"## Phase 2 — Gather recent signal",
		"## Phase 3 — Consolidate",
		"## Phase 4 — Prune and index",
		"/home/u/memory",
		"/home/u/sessions",
		"MEMORY.md",
		"7 new since",
		`glob="*.json"`,
		"`grep` tool",
		"Do NOT promote anything into EVVA.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing %q", want)
		}
	}

	// The dream agent has no shell, so the prompt must NOT teach a shell grep
	// or reference ref's JSONL transcripts.
	for _, bad := range []string{"grep -rn", "--include", ".jsonl"} {
		if strings.Contains(out, bad) {
			t.Errorf("prompt leaks a shell-grep form %q (the dream agent has no bash)", bad)
		}
	}
}
