# PRD — Checkpoint & Rewind (time-travel undo) — Implementation Plan

> **Audience:** engineers comfortable across `internal/agent`, `internal/session`,
> `pkg/tools/fs`, and the `pkg/ui/bubbletea` app.
> **Status:** proposed; ready to build **after** Resilient Edit
> (`docs/roadmap/PRD/resilient-edit.md`) lands.
> **Target release (proposed):** a **new roadmap wave → new minor (v1.9
> candidate)**. It is a substantial new capability, not a within-wave fix.
> **Do NOT** add the wave→minor row until the operator confirms the wave at
> implementation time (per CLAUDE.md the row is claimed at planning, but the
> *wave itself* is the operator's call — propose, don't presume).
> **Reference source:** ref ships file-edit checkpointing in the Agent SDK
> (`rewind file changes with checkpointing`); the public Claude Code behavior is
> the UX model (snapshot before edits; `/rewind` or Esc-Esc; restore code /
> conversation / both; **bash-modified files are not tracked**). evva can do
> *better* than that last limitation via git (§5.4). There is no single
> `ref/src/...` module to port verbatim — this is an evva-native assembly over
> existing seams.
> **Live-source verification (2026-06-23, on `dev`):** the conversation-state
> shape (`internal/session/snapshot.go` `Snapshot`/`SessionState{Messages
> []llm.Message, Usage, LastTurnInputTokens, MicroCompacted, FullCompactCount}`;
> `Save`/`Load`/`List` in `store.go`), the live-state swap seam
> (`Controller.Messages()`/`ClearSession()`/`ResumeSession(id)` in `pkg/ui/ui.go:82`;
> impls `internal/agent/agent.go:1026,1548`; adapter `pkg/agent/agent.go:352,357`),
> the file-mutation sites that already hold before-content
> (`pkg/tools/fs/edit.go:218` `before := mem.content`; `write.go` overwrite path
> via `buildOverwriteDiff(path, before, after)`), the TUI slash+overlay pattern
> (`pkg/ui/bubbletea/app/root.go` `handleSubmit` switch :732; `focus.Push` +
> `relayout`; template `components/overlays/resume.go`), the Esc plumbing
> (`root.go:74-77` interrupt/`interrupted`), and the evva-owned dir family
> (`pkg/permission/types.go:24,30` `PlanDirSegment=.evva/plans`,
> `WorktreeDirSegment=.evva/worktrees`) were all read.

---

## 1. TL;DR — what this phase actually is

evva has **no undo**. A wrong turn — a bad multi-file refactor, a misguided
rewrite, a prompt that sent the agent down the wrong path — can only be unwound
by hand (git, if you're lucky) and the conversation can't be wound back at all.
Every 2026 peer (Claude Code, Gemini CLI) ships **checkpoint + rewind**: state
is snapshotted before edits, and `/rewind` (or Esc-Esc on an empty prompt)
restores **code**, **conversation**, or **both** to a prior point. It is the
safety net that makes ambitious autonomous / `bypass`-mode runs feel safe.

This phase adds that. A **checkpoint** is taken at each **user-turn boundary**
and captures: (a) **before-images** of every file the agent's `fs` tools touch
during the turn, and (b) the **conversation cut-point** (the message-history
length at turn start). `/rewind` lists checkpoints; selecting one restores code
(rewrite the before-images back), conversation (truncate history to the
cut-point and swap it into the live agent), or both.

It reuses three things evva already has: the **before-content** the `edit`/`write`
tools already compute, the **`session.Snapshot` ↔ live-agent swap** that
`ResumeSession` already performs, and the **slash-command + overlay** TUI pattern.
Storage lives under **`.evva/checkpoints/`**, joining the `.evva/plans` +
`.evva/worktrees` family.

---

## 2. Inventory — what already exists (do not re-build)

### 2.1 Before-content is already in hand at every mutation
- `edit.go:218` — `before := mem.content` is the pre-edit file content; the tool
  also already builds a `FileDiff` (`buildEditDiff`). The checkpoint hook needs
  `(path, before)` — both are right there.
- `write.go` — the overwrite path reads prior content for `buildOverwriteDiff(path,
  before, after)`; the create path knows the file is new (before = ∅ → "delete on
  rewind"). Same hook shape.
- Net: a tool-result side-channel (`Metadata` already carries `*FileDiff`) or a
  small `CheckpointSink` interface lets the agent capture `(path, beforeBytes,
  existedBefore)` without re-reading disk.

### 2.2 Conversation state + the live-swap seam
- `session.Snapshot`/`SessionState` (`snapshot.go:30,47`) is the persisted shape:
  `Messages []llm.Message` + usage/compaction counters. `FromSnapshot`/`ToSnapshot`
  copy slices defensively.
- `Controller.ResumeSession(id)` (`ui.go:250`; `agent.go:1548`) **"swaps the live
  agent's state with the session … the TUI re-renders from Session().Messages."**
  Rewind-conversation is the same move with a **truncated** message slice — the
  primitive already exists; we add a sibling that swaps in a cut-down history
  instead of a whole loaded snapshot.
- `Controller.Messages()` (`ui.go:94`) + `ClearSession()` (`ui.go:236`) round out
  the surface; the transcript reset on `/clear` (`root.go:751` `transcript.Reset()`)
  is the template for re-rendering after a rewind.

### 2.3 The TUI slash + overlay pattern
- `root.go` `handleSubmit` (:732) is a `switch text` over `/clear`, `/config`,
  `/resume`, …; each `Reset`s input/slash, then `a.focus.Push(overlays.NewX(a.controller))`
  + `a.relayout()`. `/rewind` is one more `case`.
- `components/overlays/resume.go` is the **direct template**: a list overlay backed
  by the controller, Esc to dismiss, Enter to act. The rewind overlay is a
  resume-overlay variant whose rows are checkpoints and whose action is a
  three-way restore (code / chat / both).
- Esc plumbing (`root.go:74-77`): `interrupted` already captures "user pressed
  Esc"; the empty-prompt **Esc-Esc** opener keys off the same path (§4 Task 5).

### 2.4 The evva-owned dir family + permission carve-outs
`pkg/permission/types.go:24,30` defines `PlanDirSegment = .evva/plans` and
`WorktreeDirSegment = .evva/worktrees`, with `pathWithin` containment helpers
(`IsPlanFilePath`, etc.). `.evva/checkpoints/` joins this family — single-source a
`CheckpointDirSegment` const next to them. Checkpoint storage is evva-owned
runtime state, like plans and worktrees (and like them, a candidate for
`.gitignore` guidance in the user-guide).

### 2.5 Session persistence already does atomic writes + versioned schema
`store.go` `writeAtomic` (temp+rename) and `SnapshotVersion` + skip-on-newer
(`store.go:89,149`) are the durability + forward-compat patterns to mirror for
the checkpoint index file.

---

## 3. Goal & acceptance criteria

**Goal:** from the TUI, the user can rewind to any prior user-turn in the current
session and restore the **files**, the **conversation**, or **both** — recovering
cleanly from a bad run without leaving the terminal.

Acceptance:

1. **A checkpoint per user turn.** At each user-turn start, a checkpoint records
   the conversation cut-point; as the turn runs, the first mutation of each file
   captures that file's before-image (once per file per turn). A turn that
   touches no files still produces a (conversation-only) checkpoint.
2. **`/rewind` lists checkpoints**, newest first, each showing the turn's
   first-user-prompt preview + age + a "N files" count (mirroring the resume
   picker's metadata, `ui.go:264` `MessageCount`).
3. **Three restore modes.** Selecting a checkpoint offers **code** (rewrite each
   captured before-image; delete files that didn't exist before), **conversation**
   (truncate history to the cut-point, swap into the live agent, re-render),
   or **both**. A confirm step precedes any code restore (it overwrites the
   working tree).
4. **Esc-Esc opens rewind** when the prompt is empty and no run is active
   (matching Claude Code/Gemini muscle memory); when a run *is* active, Esc keeps
   its current interrupt meaning (§5.5).
5. **Safe + bounded.** Restores never touch files outside the workdir; checkpoint
   storage is pruned by a retention policy (count or age) so a long session can't
   grow `.evva/checkpoints/` unbounded.
6. **Honest about the gap.** Files modified by **bash** (not the `fs` tools) are
   **not** captured by the per-edit hook (documented limitation, as in Claude
   Code) — **unless** the optional git-assisted snapshot (§5.4) is enabled, which
   covers tracked-file bash edits too.
7. **No behavior change when unused.** Capturing before-images is cheap (bytes
   already in memory) and off the hot path; a session that never rewinds pays only
   the storage write + retention sweep.
8. `go build ./...`; `go test ./...` green; unit tests for capture, cut-point
   truncation, three-way restore, retention, and the create→delete-on-rewind case.

---

## 4. Work breakdown (ordered)

### Task 1 — Checkpoint store (`internal/checkpoint`, new)
A small package mirroring `internal/session`'s shape:
- On-disk layout `<workdir>/.evva/checkpoints/<session-id>/<turn-seq>/`: a
  `meta.json` (turn seq, timestamp, first-user-prompt preview, conversation
  cut-length, list of captured files + whether each existed before) and the
  before-image blobs (content-addressed by hash to dedupe re-touched files).
- `Begin(sessionID, turnSeq, cutLen, prompt) *Checkpoint`, `CaptureFile(path,
  before []byte, existed bool)` (idempotent per path per checkpoint),
  `List(sessionID)`, `Load(id)`, `Prune(policy)`, atomic writes (reuse the
  `writeAtomic` pattern). Versioned index like `SnapshotVersion`.
- `pkg/permission`: add `CheckpointDirSegment = .evva/checkpoints` next to the
  plan/worktree segments.

### Task 2 — Capture hook in the agent loop (`internal/agent`)
- At user-turn start (where a new user message enters the loop), `Begin` a
  checkpoint with `cutLen = len(session.Messages)` *before* the user message is
  appended (so rewind lands the conversation just *before* that turn).
- Thread a `CheckpointSink` (interface, nil-safe) into the `fs` tool construction
  so `edit`/`write`/(create) report `(path, before, existed)` on first mutation.
  Capture is **first-touch-per-file-per-turn** (later edits in the same turn don't
  overwrite the earliest before-image). Subagents/swarm members do **not** capture
  (solo main-agent only for v1 — §5.6).

### Task 3 — Restore engine (`internal/checkpoint` + controller method)
- **Code restore:** for each captured file, write the before-image back
  (encoding/line-endings preserved via the `fs` write helpers); files that did
  not exist before are deleted. Refuse paths outside the workdir. Best-effort,
  collecting per-file errors into a summary.
- **Conversation restore:** truncate live `session.Messages` to `cutLen` and swap
  it in. Add `Controller.RewindConversation(cutLen int) error` (sibling of
  `ResumeSession`) on the interface + `internal/agent` impl + `pkg/agent` adapter;
  it must also reset the compaction counters / usage consistently (reuse
  `SetCompactState`/`SetUsage` paths that `FromSnapshot` uses).
- **Both:** code then conversation.

### Task 4 — `/rewind` overlay (`pkg/ui/bubbletea/components/overlays/rewind.go`)
Clone `resume.go`'s structure: list rows from `Controller.Checkpoints()` (new
read-only accessor), Esc dismiss, Enter → a small mode selector (code / chat /
both) → confirm for code → invoke the restore controller method → reset the
transcript and re-render (mirror `/clear`'s `transcript.Reset()` + status refresh).
Add the `case "/rewind":` to `root.go:732`.

### Task 5 — Esc-Esc opener (`pkg/ui/bubbletea/app/root.go`)
When the prompt input is empty and no run is active, a double-Esc within a short
window opens the rewind overlay (reuse the existing post-event window pattern the
codebase already uses for the mouse/arrow dedup). When a run is active, Esc
retains its interrupt semantics untouched (§5.5).

### Task 6 — Retention + config
- `RewindMaxCheckpoints int` (default e.g. 50) and/or `RewindRetentionHours` —
  pruned on `Begin` and on session close. Mirror the `pkg/config` field + YAML +
  `Get/Set` accessor shape used by the dream/recall knobs.
- Checkpoints are **session-scoped** and cleaned with the session (or by
  retention); `/clear` starts a fresh checkpoint namespace.

### Task 7 — Docs + changelog + wave map (at implementation time, operator-confirmed)
- Append the wave→minor row to `CLAUDE.md` + `EVVA.md` (e.g. `| v1.9 | Checkpoint
  & Rewind — per-turn file/conversation snapshots + /rewind |`) — **only once the
  operator confirms the wave**.
- User-guide: `/rewind`, Esc-Esc, the three restore modes, the bash-tracking
  limitation + the git-assisted option, the retention knob, and a `.gitignore`
  note for `.evva/checkpoints/`.
- `CHANGELOG.md` `[Unreleased]` `### Added`.

---

## 5. Design decisions & risks (read before coding)

### 5.1 — Granularity: per-user-turn, not per-edit
Claude Code checkpoints per file edit; evva checkpoints per **user turn**. A turn
is the unit a user actually thinks in ("undo what that instruction did"), it keeps
the picker short, and it aligns the conversation cut-point with the file
before-images naturally (one cut-len per turn). Per-file before-images *within*
the turn still give file-level fidelity on restore.

### 5.2 — Conversation truncation must agree with compaction
The live session carries `MicroCompacted`/`FullCompactCount` and a usage tally.
Truncating `Messages` to a prior `cutLen` taken *after* a compaction would be
incoherent (the cut-len indexes a since-rewritten history). v1 rule: **store the
cut-point as a stable marker** (e.g. the message count *and* a monotonic turn
seq), and if a compaction happened between the checkpoint and now, **disable
conversation-rewind for checkpoints older than the last compaction** (code
restore still allowed), surfaced in the overlay. Getting this wrong silently
corrupts context — treat it as the central correctness risk.

### 5.3 — Code restore overwrites the working tree → confirm + scope
A code restore is destructive (it rewrites files to old content, deletes
since-created ones). It must: prompt a confirm in the TUI; refuse any path that
escapes the workdir (`pathWithin`); and never touch files it didn't capture. It
does **not** stage/commit — it edits the working tree, like the agent itself
does. Pairs naturally with the user keeping the repo under git.

### 5.4 — The bash gap, and beating it with git
The per-edit hook only sees `fs`-tool mutations; `bash` (`sed`, `>`, `mv`, build
artifacts) is invisible — the documented Claude Code limitation. **Optional
stronger mode:** when the workdir is a git repo, also snapshot a lightweight ref
(e.g. a `git stash create`-style dangling commit, or recording `HEAD` + a
`git diff` blob) at turn start, so code-restore can reset *tracked* files
regardless of who changed them. Gate behind a config flag; keep the fs-hook
restore as the always-on baseline so non-git workdirs still get undo of agent
edits. (Design the store so a checkpoint can hold *either/both* a git-ref and
fs before-images.)

### 5.5 — Esc-Esc must not steal the interrupt
Esc during an active run means "interrupt" (`root.go:74-77`) — load-bearing. The
rewind opener triggers **only** when the prompt is empty *and* no run is active,
matching peer behavior. If in doubt, ship `/rewind` first and add Esc-Esc behind
the same guard once the interrupt interaction is proven untouched (the existing
double-key dedup window is the reference implementation).

### 5.6 — Solo main-agent only for v1
Subagents and swarm members don't checkpoint in v1 (they run in worktrees /
their own loops; their isolation already bounds blast radius — see
`internal/tools/mode/worktree.go`). Checkpoint is a main-session affordance.
Swarm/subagent rewind is a deliberate later wave.

### 5.7 — Risks
- **Conversation/compaction incoherence** (§5.2) — the headline risk; mitigated by
  gating conversation-rewind across compaction boundaries.
- **Storage growth** — bounded by retention (Task 6) + content-addressed dedupe of
  re-touched files.
- **Restore clobbers concurrent user/external edits** — the confirm step + the
  workdir-scope check; document that code-restore overwrites current contents.
- **Partial restore on error** — collect per-file errors, report a summary, never
  half-apply silently.

---

## 6. Out of scope (revisit later)
- **Branching / multiple timelines** (rewind, edit, keep both forks). v1 is linear
  undo.
- **Cross-session rewind** (rewinding into a `/resume`d older session). Checkpoints
  are scoped to the current session id.
- **bash file-watching via fanotify/FSEvents.** The git-assisted mode (§5.4) is the
  pragmatic substitute; OS-level FS watching is its own project.
- **Swarm / subagent checkpointing** (§5.6).
- **A non-TUI `evva rewind` CLI subcommand.** TUI-first; CLI can follow.

## 7. Verification checklist (PR gate)
- [ ] A user turn produces one checkpoint; first-touch-per-file before-images
      captured once; a no-file turn still checkpoints (conversation-only).
- [ ] `/rewind` lists newest-first with prompt preview + age + file count.
- [ ] Code restore rewrites before-images, deletes since-created files, preserves
      encoding/line endings, refuses out-of-workdir paths.
- [ ] Conversation restore truncates to the cut-point and re-renders; **disabled**
      for checkpoints older than the last compaction (§5.2), with a clear reason
      shown.
- [ ] "Both" applies code then conversation; partial errors summarized.
- [ ] Esc-Esc opens rewind only on empty prompt + no active run; interrupt
      behavior during a run unchanged.
- [ ] Retention prunes by count/age; `.evva/checkpoints/` stays bounded.
- [ ] Capture is main-agent only (subagents/members don't checkpoint).
- [ ] `go build ./...`, `go test ./...`, `gofmt`, `go vet` clean.

## 8. File-by-file change list (cheat sheet)
| File | Change |
|---|---|
| `internal/checkpoint/store.go` | **new** — store, capture, list, restore, retention (Tasks 1,3,6) |
| `pkg/permission/types.go` | `+ CheckpointDirSegment = .evva/checkpoints` + containment helper (Task 1) |
| `internal/agent/*` (loop + tool wiring) | turn-start `Begin`; thread `CheckpointSink` into `fs` tools; `RewindConversation` impl (Tasks 2,3) |
| `pkg/tools/fs/edit.go`, `write.go` | report `(path, before, existed)` to a nil-safe sink on first mutation (Task 2) |
| `pkg/ui/ui.go` + `pkg/agent/agent.go` | `Checkpoints()` accessor + `RewindConversation(cutLen)` on the Controller interface + adapter (Tasks 3,4) |
| `pkg/ui/bubbletea/components/overlays/rewind.go` | **new** — checkpoint picker + restore-mode selector (Task 4) |
| `pkg/ui/bubbletea/app/root.go` | `case "/rewind"`; Esc-Esc opener guard (Tasks 4,5) |
| `pkg/config/config.go` + `file_config.go` | retention knob(s) + optional git-assist flag (Tasks 6, §5.4) |
| `CLAUDE.md` / `EVVA.md` | wave→minor row (Task 7, operator-confirmed) |
| `docs/user-guide/...`, `CHANGELOG.md` | feature docs + `[Unreleased]` Added (Task 7) |
| `*_test.go` | capture, truncation+compaction gate, three-way restore, retention, create→delete (Task 7 checklist) |
