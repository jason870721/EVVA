# PRD — Auto-Dream (Background Memory Consolidation) — Implementation Plan

> **Audience:** senior engineers implementing this phase.
> **Status:** proposed; ready to build.
> **Target release:** **v1.8.0** — claims a new roadmap wave (*auto-memory
> consolidation*). Append the wave→minor row to `CLAUDE.md` + `EVVA.md` at
> planning time (Task 8). One roadmap wave = one minor (CLAUDE.md).
> **Roadmap source:** the deferred **background half** of the memory roadmap.
> `docs/roadmap/PRD/memory-typed-directory.md` scoped itself to *"the
> foreground pieces"* and named a deferred *"`/dream` / background-consolidation
> memory phase."* This is that phase.
> **Reference source (port):** `ref/src/services/autoDream/` — `autoDream.ts`
> (engine + gate), `consolidationPrompt.ts` (the 4-phase dream prompt),
> `consolidationLock.ts` (lock + timestamp + session scan), `config.ts`
> (enabled gate); `ref/src/tasks/DreamTask/DreamTask.ts` (UI surfacing).
> **Reference source (NOT ported here — see §6):** `ref/src/services/extractMemories/`
> and `ref/src/services/SessionMemory/`. ref ships autoDream *independently* of
> these (`config.ts:1-2`); so do we.
> **Live-source verification (2026-06-17, on `dev`):** the trigger seam
> (`internal/agent/loop.go:235` `hooks.EventStop`/`FireStop`; `done()` →
> `IDLE`+`KindRunEnd` at `loop.go:306`), the background-agent machinery
> (`agent.New(Config)` + `ctl.Run(ctx, prompt)`, used by the swarm at
> `internal/swarm/space.go:422` + `scheduler.go:423`), the session list API
> (`internal/session/store.go` `List`/`ListEntry.MTime`/`SessionsDir`), the
> memory paths + fence (`internal/memdir/memdirpaths.go` `MemoryDir`/
> `IsInMemoryDir`; `internal/agent/options.go:243` `WithMemoryDir`;
> `permission.IsAutoMemPath`), and the recall model-selection precedent
> (`internal/agent/memory_recall.go` `recallTarget`/`recallClient`) were all read.

---

## 1. TL;DR — what this phase actually is

evva's memory has two of its three moving parts:

- **Recall** (`internal/memdir/recall`) — each turn, a cheap side-query pulls
  the few relevant memories into context.
- **Inline save** — the model writes memory files *during* a conversation,
  driven by the typed-memory prompt.
- **Consolidation — MISSING.** Nothing ever goes back over the accumulated
  store to merge near-duplicates, prune stale facts, fix drifted claims, and
  keep `MEMORY.md` under its truncation budget. Inline save is greedy and
  local; over many sessions the store bloats and rots.

The `remember` bundled skill (shipped separately) is the **manual** version of
consolidation — the user runs `/remember`, it *proposes*, the user approves.
**Dream is the automatic, background version**: when the user has been idle and
enough new activity has accumulated, evva forks a **fenced background agent**
that runs a 4-phase consolidation pass (Orient → Gather → Consolidate → Prune)
over the global memory directory, then reports a one-line summary. It is
gated (time + session count + lock) so it fires at most ~once/day, and it can
only write inside the memory directory.

Dream and `remember` are the two halves of memory hygiene: **dream keeps the
store tight automatically and within-memory; `remember` promotes durable
conventions up to `EVVA.md` with the user in the loop.**

This phase ports `ref/src/services/autoDream/` faithfully, adapted to evva's
**one global memory store** (vs ref's per-git-root key) and evva's tool set
(no shell for the dream agent — see §5.3).

---

## 2. Inventory — what already exists (do not re-build)

### 2.1 The memory layer — `internal/memdir` (global store)
`MemoryDir(appHome)` = `<appHome>/memory` (ONE global store; evva diverges
from ref's per-project key — memory-typed-directory PRD §5.2). `MemoryIndexFile`
= `MEMORY.md`. `EnsureMemoryDir`, `IsInMemoryDir(appHome, absPath)` (containment
check), `MemoryIndexPath`. The typed-memory prompt (`autoMemoryGuidanceSection`
in `internal/agent/sysprompt/main_agent.go`) is the source of truth for *what*
to save and *how* — the dream prompt defers to it rather than re-stating it.

### 2.2 Per-turn recall + the dedicated background client — `internal/agent/memory_recall.go`
`recallTarget()` picks a cheap per-provider model (Sonnet / deepseek-flash /
gpt-5.4-mini / glm-4.6 / ollama-mirror) at `medium` effort; `recallClient()`
builds a **dedicated** `llm.Client` via `buildLLMClient`. Dream reuses this
exact model-selection precedent (§5.6) — a background consolidation pass wants
the same "cheap, always-credentialed" client, not the main loop's flagship.

### 2.3 The trigger seam — the Stop hook
`internal/agent/loop.go:235`: `if !a.IsSubagent() && a.hookDispatcher.Has(hooks.EventStop)`
fires `FireStop`. The main agent reaches this when a turn produces no more tool
calls — i.e. it is about to go idle. `done()` (`loop.go:306`) then sets
`status = IDLE` and emits `KindRunEnd`. **This is ref's `stopHooks` seam.**
Dream's gate check fires here (§5.2), main-agent-only.

### 2.4 The background-agent machinery — `agent.New` + `Run`
The swarm already runs full agent loops programmatically: `internal/swarm/space.go:422`
builds members via `agent.New(agent.Config{...})` against per-agent config
clones, and `internal/swarm/scheduler.go:423` drives them with `ctl.Run(ctx, prompt)`.
Dream is *one* such background agent: build a fenced agent, `Run` it once with
the consolidation prompt. **No new agent-runtime primitive is needed.**

### 2.5 The write fence — `WithMemoryDir` (RP-25) + `permission.IsAutoMemPath`
`internal/agent/options.go:243` `WithMemoryDir(dir)` re-homes an agent's
writable auto-memory and (RP-25) installs a deny-level fence: writes/edits
outside `dir` are refused *above* bypass. `permission.IsAutoMemPath` is the
carve-out predicate. This is exactly the trust boundary the dream agent runs
behind (§5.3).

### 2.6 Session transcripts on disk — `session.List` / `SessionsDir`
`internal/session/store.go`: sessions persist at
`<appHome>/sessions/<workdir-slug>/<session-id>.json`. `List(appHome, workdirSlug)`
returns `[]ListEntry{Snapshot, MTime int64}` sorted newest-first. The
**session-gate** counts snapshots with `MTime > lastConsolidatedAt`. Memory is
global but sessions are per-workdir-slug — the gate must scan **all** slugs
(§5.1); add `session.CountTouchedSince`.

### 2.7 Config precedent — `EnableAutoMemory` / `MemoryRecallModel`
`pkg/config/config.go:138-149` + `file_config.go:46-61`: typed field +
`yaml:"..."` tag + `Get*/Set*` accessor, three times over
(`enable_auto_memory`, `enable_memory_recall`, `memory_recall_model`). Dream's
knobs (Task 1) mirror this shape exactly — and dream is additionally gated on
`GetEnableAutoMemory()` (no memory system → nothing to consolidate).

### 2.8 The `remember` skill — the manual sibling
`internal/skills/bundled/content/remember/SKILL.md` already does the
user-in-the-loop version (review → propose → approve, including promotion to
`EVVA.md`). Dream must NOT duplicate its cross-layer promotion role — dream
stays **within** the memory dir (§5.9 of the remember design: promotion needs a
human). The two are complementary, not overlapping.

### 2.9 Reference (`ref/src/services/autoDream/`)
- `autoDream.ts` — `initAutoDream()` closure; gate order time → scan-throttle →
  sessions → lock; `runForkedAgent` with `createAutoMemCanUseTool`; progress
  watcher; inline "Improved N" completion message; failure rolls back the lock.
- `consolidationPrompt.ts` — `buildConsolidationPrompt(memoryRoot, transcriptDir, extra)`,
  the 4-phase prompt. Ports near-verbatim (§5.3 swaps shell-grep → grep tool).
- `consolidationLock.ts` — `readLastConsolidatedAt`, `listSessionsTouchedSince`,
  `tryAcquireConsolidationLock`, `rollbackConsolidationLock`. Single lock file
  whose **mtime doubles as `lastConsolidatedAt`**; acquire bumps it to now,
  failure rolls it back so the time-gate re-opens.
- `config.ts` — `isAutoDreamEnabled()` (user setting overrides default);
  `DEFAULTS = { minHours: 24, minSessions: 5 }`.
- `tasks/DreamTask/DreamTask.ts` — UI surfacing (footer pill + detail). evva v1
  ships the minimal surface (§5.10 / Task 7); the full pill is a later polish.

---

## 3. Goal & acceptance criteria

**Goal:** when enabled, evva automatically keeps the global memory store tight —
merging duplicates, pruning stale entries, fixing drifted claims, and bounding
`MEMORY.md` — without the user asking, at a cost bounded to ~once/day.

Acceptance:

1. **Off by default, one switch on.** `enable_auto_dream` defaults `false`.
   With it `true` (and `enable_auto_memory` `true`), dream fires per the gate;
   with either `false`, dream never runs and advertises nothing.
2. **Gate holds.** Dream fires only when *all* of: enabled; `≥ min_hours`
   since the last consolidation; `≥ min_sessions` sessions touched since then
   (across all workdir slugs, excluding the current one); lock acquired.
   Cheapest checks first; a per-turn miss costs ≤ 1 stat (+ a ≤10-min-throttled
   dir scan).
3. **Fenced.** The dream agent can only write/edit inside `<appHome>/memory`;
   any write elsewhere is denied. It has no shell, no web, no subagent spawn,
   and no per-turn recall.
4. **Non-blocking.** Dream runs in the background; the user can immediately
   start a new turn. A concurrent dream is impossible (lock).
5. **Visible + summarized.** The user sees a "dreaming" indication while it
   runs and a one-line "Improved N memories" system message when it finishes
   (skipped if it changed nothing).
6. **Crash/kill-safe.** A failed, cancelled, or killed dream rolls the lock
   mtime back so the time-gate re-opens; it never leaves a half-claimed lock.
7. **Independent of extractMemories.** Ships without the auto-*save* subsystem;
   operates on whatever inline-save + `remember` produced.
8. `go build ./...` clean; `go test ./...` green; new unit tests for the gate,
   lock round-trip, session counting, and prompt content.

---

## 4. Work breakdown (ordered)

### Task 1 — Config knobs (`pkg/config`)
Mirror the `MemoryRecall` trio. Add to `Config` + `FileConfig` + accessors:

| Field | YAML | Default | Meaning |
|---|---|---|---|
| `EnableAutoDream bool` | `enable_auto_dream` | `false` | master switch |
| `AutoDreamMinHours int` | `auto_dream_min_hours` | `24` | time gate |
| `AutoDreamMinSessions int` | `auto_dream_min_sessions` | `5` | activity gate |
| `AutoDreamModel string` | `auto_dream_model` | `""` | optional model override (else recall-style per-provider default) |

`Get*`/`Set*` with the same validation shape as the recall knobs (negatives
normalize to the default; `Set*` persists). Surface `enable_auto_dream` in the
`/config` tool's supported settings.

### Task 2 — Lock + timestamp + session count (`internal/memdir/dream`)
New **stdlib-leaning** sub-package (it may import `internal/session` +
`pkg/config`, like `recall` imports `pkg/llm` — the parent `memdir` stays
stdlib-only). Port `consolidationLock.ts`:
- Lock dir `<memoryDir>/.dream/`; marker file `consolidation.lock`.
- `ReadLastConsolidatedAt(memDir) time.Time` — the marker's mtime (zero/missing
  ⇒ epoch, so first run's time-gate is open).
- `TryAcquire(memDir) (prior time.Time, ok bool)` — atomically bump the
  marker's mtime to now iff not held; returns the prior mtime for rollback.
- `Rollback(memDir, prior time.Time)` — restore the prior mtime (failure path).
- `session.CountTouchedSince(appHome string, since time.Time, excludeID string) (int, error)`
  (new, in `internal/session`) — scan **all** `<appHome>/sessions/<slug>/`
  dirs, count `*.json` with mtime > since, minus the current session.

### Task 3 — The gate (`internal/memdir/dream`)
`GateOpen(cfg, appHome, currentSessionID, now) (Decision, prior)` running the
order from §2.9, with the **scan-throttle** (skip the session scan when the last
scan was < 10 min ago and the time-gate keeps re-passing — ref's
`SESSION_SCAN_INTERVAL_MS`). Pure + table-testable; the lock acquire is the
final, side-effecting step (caller owns rollback on run failure).

### Task 4 — The consolidation prompt (`internal/memdir/dream/prompt.go`)
Port `buildConsolidationPrompt(memoryRoot, transcriptRoot, sessionIDs)`
verbatim except: (a) `MEMORY.md` / `MemoryDir` substituted; (b) the transcript
grep example uses evva's **`grep` tool** form, not a shell `grep` (§5.3); (c)
the index budget cites evva's 200-line `MEMORY.md` truncation. Phases 1-4
unchanged. Pure string builder; pin its load-bearing lines with a content test.

### Task 5 — The fenced dream agent (`internal/agent/dream.go`)
`buildDreamAgent(ctx) (ui.Controller, error)`:
- `agent.New(agent.Config{...})` against a **config clone** with
  `EnableMemoryRecall=false` (§5.4) and the dream model/effort (§5.6).
- A **minimal tool set**: `read`, `glob`, `grep`, `tree`, `edit`, `write` —
  **no `bash`, no web, no `agent`** (§5.3).
- `WithMemoryDir(memDir)` so the permission **auto-allow carve-out** targets
  the global memory dir (a write/edit confined to it passes without a prompt),
  plus the fence design in §5.3 (NOT bypass).
- A dedicated `event.Sink` capturing assistant text + edited/written paths for
  the progress watcher + completion summary.

### Task 6 — Trigger wiring (`internal/agent/dream.go` + `loop.go`)
`(a *Agent) maybeFireDream()` called once when the **main agent** goes idle
(end of `done()`'s main-agent branch, or just after `runLoop` returns).
Main-agent-only, gated on `EnableAutoDream && EnableAutoMemory`. It runs the
cheap gate synchronously; if open + lock acquired, it launches the dream agent
in a **goroutine** with a background context (agent-lifetime, cancelled on
shutdown), so the user is never blocked. On run failure/cancel → `Rollback`.

### Task 7 — UI surface (events + completion message)
Minimal v1: a `KindDreaming` / status signal so the TUI can show a 🌙 indicator
while it runs, and an inline system message `🌙 Consolidated memory — improved N
file(s): a.md, b.md` on completion (skipped when nothing changed), reusing the
same surface as a normal background completion. The full background-task pill
(`DreamTask`) is **out of scope for v1** (§6).

### Task 8 — Docs + version + changelog + wave map
- Append the wave→minor row to `CLAUDE.md` **and** `EVVA.md`:
  `| v1.8 | Auto-memory consolidation (dream) — background fenced consolidation agent; PRD: docs/roadmap/PRD/auto-dream.md |`.
- User-guide: document `enable_auto_dream` + the gate + the fence + cost note
  (the "document shipped features in the same change" rule).
- `CHANGELOG.md` `[Unreleased]` → `### Added`.

---

## 5. Design decisions & risks (read before coding)

### 5.1 — Global memory, per-workdir sessions → session-gate scans all slugs
evva's memory is one global store but sessions are per-workdir. The activity
gate is a proxy for "enough new happened since the last dream"; with global
memory that means activity *anywhere*, so `CountTouchedSince` scans every slug
under `<appHome>/sessions/`. (Simpler current-slug-only counting is the
fallback if cross-slug I/O ever bites; it is not the default because a
multi-project user would under-trigger.)

### 5.2 — Background goroutine, fired at idle — not blocking, not a cron
Dream fires from the **Stop/idle seam** (§2.3), exactly like ref's stopHooks —
not from `cron`/`alarm` (those are model-facing and wall-clock; dream is a
runtime housekeeping pass gated on *relative* time + activity). It runs in a
goroutine so the user keeps working; the lock makes a second concurrent dream
impossible.

### 5.3 — The fence is the trust boundary; the dream agent gets no shell
An autonomous agent writing to disk unsupervised is only safe behind a hard
fence. **Verified against `pkg/permission/decision.go` (2026-06-17), the fence
is NOT bypass mode** — `ModeBypass` short-circuits to *allow* (decision.go:46),
and the only deny that pierces bypass is the RP-25 *sibling* fence, which
guards OTHER swarm members' dirs under `<workdir>/agents/` — it does not confine
a solo agent whose memDir is `<appHome>/memory`. So the fence is three layers:

1. **No shell, no escape tools.** The dream agent's tool set is `read`, `glob`,
   `grep`, `tree`, `edit`, `write` — **no `bash`** (no redirection / `rm` / `mv`),
   no web, no `agent`. (Diverges from ref's read-only-bash + `createAutoMemCanUseTool`
   gate; tool-level exclusion is simpler and strictly safer.)
2. **`ModeDefault` + the memDir auto-allow carve-out** (`Decide` step 5, active
   in default): a write/edit *confined to* `WithMemoryDir(memDir)` is allowed
   without a prompt; anything else falls through to "ask".
3. **An auto-deny permission broker** installed only on the dream agent: an
   "ask" (i.e. a write/edit OUTSIDE memDir) is auto-DENIED, never surfaced to a
   user who isn't there. Reads are not gated, so exploration is unaffected.

Net: the dream agent reads freely, writes only inside the memory dir, and any
attempt to write elsewhere is cleanly denied (not allowed, not hung). The
transcript-grep prompt line uses the `grep` tool, not a shell grep.

### 5.4 — Recall disabled inside the dream agent
The dream agent is a *root* agent, so `runMemoryRecall` would otherwise fire on
its prompt (recall is gated only on `!IsSubagent()`). Build its config clone
with `EnableMemoryRecall=false`: dream reads memory directly (Phase 1 `ls`/read),
it must not also recurse through the recall side-query.

### 5.5 — Lock = a single file whose mtime is the timestamp (port ref)
One file, two jobs: its mtime is `lastConsolidatedAt`, and "bump mtime to now"
*is* the acquire. Failure rolls the mtime back so the time-gate re-opens at the
next idle; the scan-throttle is the backoff against hot-looping. This avoids a
separate lock + timestamp + their skew. Cross-process safe (mtime compare +
atomic rename); within-process the goroutine that bumped it owns the run.

### 5.6 — Model selection mirrors recall
Reuse the `recallTarget` precedent: an explicit `AutoDreamModel` (credentialed)
wins; else the cheap per-provider default at `medium` effort. A consolidation
pass is judgment-light gardening, not flagship reasoning — and it must run on a
provider that's already credentialed, never depend on a second key.

### 5.7 — Cost: a dream is a full agentic run, but rare
Unlike recall (one cheap completion), a dream is a multi-iteration agent loop
(ls → read → grep → edit → write) — real tokens. The gate bounds it to
~once/`min_hours` after `≥min_sessions`, so amortized cost is low; the cost is
opt-in (`enable_auto_dream`). The dream agent has its own session, so its usage
is metered separately from the user's — surface it in its completion message
rather than the main `/cost` total.

### 5.8 — Concurrency: dream vs. the user's next turn touching memory
After dream launches, the user may start a turn. Reads (recall) are safe.
A model-driven memory *write* in the user's turn could race a dream edit of the
same file (lost update). The window is tiny (dream ≤ once/day; mid-turn memory
writes are rare) and v1 accepts it. A memory-dir write mutex is the stronger fix
and is deferred — note it, don't build it.

### 5.9 — Dream consolidates within memory; promotion stays manual (`remember`)
Dream must not promote memories into `EVVA.md` — cross-layer promotion is a
human-judgment call the `remember` skill already owns (user approves each).
Dream's Phase 3/4 stay inside the memory dir (merge/prune/reindex). This keeps
the autonomous agent's blast radius inside the fence and avoids two systems
editing project instructions.

### 5.10 — Risks
- **Bad consolidation (deletes a good memory).** Mitigation: the prompt says
  "merge/update at the source, convert dates, delete only *contradicted* facts";
  the fence limits damage to the memory dir; memory is recoverable from git if
  the user versions `<appHome>`. Residual risk accepted for v1; a dry-run /
  proposal mode is a possible later knob.
- **Hot-loop if the gate is mis-evaluated.** Mitigation: scan-throttle +
  mtime-as-timestamp means a passed time-gate that fails the session-gate does
  not re-scan for 10 min, and a fired dream advances the mtime.
- **Background agent outliving shutdown.** Mitigation: the dream goroutine binds
  a cancellable agent-lifetime context cancelled on agent `Close`.

---

## 6. Out of scope (revisit later)
- **extractMemories / SessionMemory** — auto-*save* (writing new memories from a
  transcript). Dream consolidates what exists; inline-save + `remember` feed it.
- **The full `DreamTask` UI pill** (footer pill + live turn/files detail dialog).
  v1 ships a status indicator + completion message only.
- **Per-member / swarm dreaming.** RP-25 gave members their own memory dirs;
  dreaming them is a natural follow-on but is solo-first here.
- **A memory-dir write mutex** (§5.8) and a **dream dry-run/proposal mode** (§5.10).
- **Assistant/daily-log (KAIROS) mode** — ref's append-only-log variant; evva
  has no daily-log layout.

---

## 7. Verification checklist (PR gate)
- [ ] `enable_auto_dream` defaults false; dream never fires with it (or
      `enable_auto_memory`) off; no prompt/tool advertises it when off.
- [ ] Gate unit tests: time-gate, session-gate (cross-slug, excludes current),
      scan-throttle, lock acquire/rollback round-trip.
- [ ] Fence test: the dream agent's config denies a write outside the memory
      dir and has no `bash`/web/`agent` tool.
- [ ] Recall is off inside the dream agent.
- [ ] Completion message lists changed files; emitted only when ≥1 file changed.
- [ ] Failure/cancel rolls the lock mtime back (time-gate re-opens).
- [ ] Prompt content test pins the 4 phases + the `grep`-tool (not shell) line.
- [ ] `go build ./...`, `go test ./...`, `gofmt`, `go vet` clean.
- [ ] Wave→minor row added to CLAUDE.md + EVVA.md; user-guide + CHANGELOG updated.

## 8. File-by-file change list (cheat sheet)
| File | Change |
|---|---|
| `pkg/config/config.go` | +4 fields, +`Get/Set` accessors (Task 1) |
| `pkg/config/file_config.go` | +4 `yaml:` fields + merge (Task 1) |
| `internal/tools/config/...` (supported settings) | advertise `enable_auto_dream` |
| `internal/memdir/dream/lock.go` | lock + timestamp (Task 2) |
| `internal/memdir/dream/gate.go` | gate order + scan-throttle (Task 3) |
| `internal/memdir/dream/prompt.go` | consolidation prompt (Task 4) |
| `internal/session/store.go` | +`CountTouchedSince` (Task 2) |
| `internal/agent/dream.go` | build+run fenced agent, `maybeFireDream`, surface (Tasks 5-7) |
| `internal/agent/loop.go` | call `maybeFireDream` at main-agent idle (Task 6) |
| `internal/agent/options.go` | (reuse `WithMemoryDir`; add a dream-config option if needed) |
| `CLAUDE.md` / `EVVA.md` | wave→minor row (Task 8) |
| `docs/user-guide/...` | document the feature (Task 8) |
| `CHANGELOG.md` | `[Unreleased]` Added (Task 8) |
| `*_test.go` | gate, lock, session-count, fence, prompt-content (Task 7 checklist) |
