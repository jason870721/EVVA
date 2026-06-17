package dream

import (
	"fmt"
	"strings"

	"github.com/johnny1110/evva/internal/memdir"
)

// maxIndexLines mirrors the MEMORY.md truncation budget the typed-memory prompt
// advertises (lines past this are dropped on load); the dream keeps the index
// under it. Port of ref's MAX_ENTRYPOINT_LINES.
const maxIndexLines = 200

// BuildPrompt renders the 4-phase consolidation prompt for the dream agent.
// Ported from ref/src/services/autoDream/consolidationPrompt.ts, adapted to
// evva (PRD §5.3): the index file is MEMORY.md, the transcript store is the
// global per-workdir sessions tree (JSON, not JSONL), transcript search uses
// evva's `grep` TOOL (the dream agent has no shell), and a Phase-4 guard keeps
// the dream inside the memory dir — promotion to EVVA.md stays the user-approved
// `/remember` step (§5.9). memDir is the global memory dir; transcriptRoot is
// <appHome>/sessions; sessionCount is the gate's activity count (context only).
func BuildPrompt(memDir, transcriptRoot string, sessionCount int) string {
	idx := memdir.MemoryIndexFile
	lines := []string{
		"# Dream: Memory Consolidation",
		"",
		"You are performing a dream — a reflective pass over your memory files. Synthesize what you've learned recently into durable, well-organized memories so that future sessions can orient quickly.",
		"",
		"Memory directory: " + memDir,
		"This directory already exists — write to it directly. Do not run mkdir or check for its existence.",
		"",
		fmt.Sprintf("Session transcripts: %s (JSON files, one subdirectory per project — %d new since the last consolidation). Search them narrowly with the `grep` tool; never read whole transcript files.", transcriptRoot, sessionCount),
		"",
		"---",
		"",
		"## Phase 1 — Orient",
		"",
		fmt.Sprintf("- Glob `%s/*.md` to see what memory already exists", memDir),
		fmt.Sprintf("- Read `%s` to understand the current index", idx),
		"- Skim existing topic files so you improve them rather than creating duplicates",
		"",
		"## Phase 2 — Gather recent signal",
		"",
		"Look for new information worth persisting, in rough priority order:",
		"",
		"1. **Existing memories that drifted** — facts that contradict what the codebase shows now",
		fmt.Sprintf("2. **Transcript search** — when you need specific context, use the `grep` tool with a NARROW pattern: pattern=\"<narrow term>\" path=\"%s\" glob=\"*.json\"", transcriptRoot),
		"",
		"Don't exhaustively read transcripts. Look only for things you already suspect matter.",
		"",
		"## Phase 3 — Consolidate",
		"",
		"For each thing worth remembering, write or update a memory file at the top level of the memory directory. Use the memory file format and type conventions from your system prompt's auto-memory section — it is the source of truth for what to save, how to structure it, and what NOT to save.",
		"",
		"Focus on:",
		"- Merging new signal into existing topic files rather than creating near-duplicates",
		"- Converting relative dates (\"yesterday\", \"last week\") to absolute dates so they stay interpretable",
		"- Deleting contradicted facts — if today's state disproves an old memory, fix it at the source",
		"",
		"## Phase 4 — Prune and index",
		"",
		fmt.Sprintf("Update `%s` so it stays under %d lines and reads as an index, not a dump — each entry one line under ~150 characters: `- [Title](file.md) — one-line hook`. Never write memory content directly into it.", idx, maxIndexLines),
		"",
		"- Remove pointers to memories that are now stale, wrong, or superseded",
		"- Demote verbose entries: if an index line runs past ~200 chars, shorten it and move the detail into the topic file",
		"- Add pointers to newly important memories",
		"- Resolve contradictions — if two files disagree, fix the wrong one",
		"",
		"Do NOT promote anything into EVVA.md or any project file — consolidation stays inside the memory directory; promoting a memory to project instructions is the separate, user-approved `/remember` step.",
		"",
		"---",
		"",
		"Return a brief summary of what you consolidated, updated, or pruned. If nothing changed (memories are already tight), say so.",
	}
	return strings.Join(lines, "\n")
}
