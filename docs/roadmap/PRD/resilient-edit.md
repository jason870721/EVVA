# PRD — Resilient Edit (whitespace/indentation-tolerant match fallback) — Implementation Plan

> **Audience:** engineers working on the `fs` tool family.
> **Status:** proposed; ready to build.
> **Target release (proposed):** a within-wave **`v1.8.x` patch** — this sharpens
> an existing tool (`edit`), it is *not* a new roadmap wave, so it bumps **Z**,
> not Y. No wave→minor row. (Operator confirms at release time.)
> **Reference source — DIVERGENCE (read §2.4):** evva's `edit` matching is a
> faithful port of `ref/src/tools/FileEditTool/utils.ts` (`findActualString`:
> exact → curly-quote normalize → fail). ref has **no** whitespace/indentation
> -tolerant or anchor matching; its only extra fallback is `desanitizeMatchString`
> (Anthropic API-token sanitization), which evva also skipped. So this feature is
> a **deliberate divergence beyond ref**, permitted by CLAUDE.md ("Diverge only
> with a clear reason" — §3). Prior art for the pattern: aider's flexible
> search/replace, opencode / Cline replacer chains, oh-my-pi's "Hashline".
> **Live-source verification (2026-06-23, on `dev`):** the match seam
> (`pkg/tools/fs/edit.go:179` `findActualString(mem.content, in.OldString)`), the
> resolver (`edit.go:346` exact→curly), the ambiguity guard (`edit.go:192`
> `strings.Count > 1`), the not-found hint (`edit.go:611` `buildNotFoundHint`),
> the `new_string` post-processing (`edit.go:209-213`
> `stripTrailingWhitespacePerLine` + `preserveQuoteStyle`), the substitution
> (`edit.go:219` `applyEditToFile`), the diff/offset path (`edit.go:229`
> `findAllOffsets` + `diff.go:228` `lineNumberOf`), and the freshness contract
> (`tracker.go` `ReadTracker.CanEdit`, a separate concern) were all read.

---

## 1. TL;DR — what this phase actually is

The `edit` tool resolves `old_string` against the file with **exact match**, then
a **curly-quote-normalized** retry, then gives up:

```
edit: old_string not found in <path>.
<hint>
Re-read the file and copy the exact text — including whitespace — that you want to replace.
```

The single most common real-world `edit` failure is not "wrong text" — it is
**whitespace drift**: the model reproduces a block with the wrong indentation
(tabs vs spaces, 2- vs 4-space, a leading-indent offset), or the file was
reformatted since the model last read it. The content is right; the leading/
trailing whitespace is off. Today every such case costs a full **re-read → retry**
round-trip (latency + tokens), and sometimes loops.

**Resilient Edit adds one more fallback layer, fired only after exact + curly
both miss:** a **line-trimmed unique match** — compare `old_string`'s lines to
the file's lines ignoring each line's leading/trailing horizontal whitespace; if
exactly **one** contiguous run of file lines matches, that run *is* the target.
The tool then **reconciles indentation** (re-indents `new_string` to the file's
actual indentation) and applies the edit verbatim through the unchanged
downstream path.

Two non-negotiable safety properties keep this from becoming a guess-machine:

1. **Exact always wins.** The fallback never runs when exact/curly already found
   a match, so no successful edit changes behavior.
2. **Unique or nothing.** A fallback that matches **>1** distinct file region is
   treated as *ambiguous* and rejected with "add more context" — identical to
   today's `count > 1` guard. We never silently pick a region.

It is a **silent safety net**, deliberately *not* advertised in the tool
description (§5.6), so the model keeps trying to be byte-exact and only benefits
when it slips.

---

## 2. Inventory — what already exists (do not re-build)

### 2.1 The match seam — `findActualString` (`pkg/tools/fs/edit.go:346`)
```go
actualOld, found := findActualString(mem.content, in.OldString)   // edit.go:179
```
`findActualString` returns the **file's verbatim substring** that the model
meant (so downstream `strings.Count` / `strings.Replace` / offset math all
operate on real file bytes). It tries (1) exact `strings.Contains`, then (2)
curly→straight normalized match (mapping the normalized offset back to a real
byte offset via `normalizedByteOffsetToReal`). On miss it returns `("", false)`.
**This is the one function the new layer slots into** — its contract (return a
verbatim file substring or `false`) is exactly what the fallback must honor.

### 2.2 The downstream path is match-agnostic (do not touch)
Once `actualOld` is a verbatim file substring, everything after it is reusable
as-is: the ambiguity guard (`edit.go:192` `strings.Count(content, actualOld)`),
`new_string` post-processing (`edit.go:209-213`), `applyEditToFile`
(`edit.go:219`, with its empty-`new_string` trailing-newline cleanup), the TOCTOU
re-stat (`edit.go:244`), the encoding-preserving write, the diff
(`buildEditDiff` → `findAllOffsets` + `lineNumberOf`), and the tracker re-record.
The fallback's *only* job is to produce a better `actualOld`.

### 2.3 The freshness contract is a separate concern — `ReadTracker` (`tracker.go`)
`ReadTracker.CanEdit` (`edit.go:166`) rejects edits to a file never read, or one
whose bytes drifted on disk since the last read. That is about *staleness*, not
*matching*, and is **unchanged** by this feature. The fallback runs only after
`CanEdit` has already passed, i.e. the in-memory `mem.content` is current.

### 2.4 ref parity + the gap (`ref/src/tools/FileEditTool/utils.ts`)
- `findActualString` (ref:73) — exact → curly. evva's is a line-for-line port.
- `normalizeFileEditInput` (ref:581) + `desanitizeMatchString` (ref:557) — a
  *third* ref fallback that swaps Anthropic API-sanitized tokens (`<fnr>` →
  `<function_results>`, etc.) before matching. **evva never ported this.** It is
  cheap, ref-blessed, and orthogonal to whitespace — fold it in as **Strategy 0.5**
  (§4 Task 2) while we are in this code.
- **No whitespace/indentation/anchor tolerance anywhere in ref.** That is the
  net-new part and the divergence this PRD authorizes.

---

## 3. Goal & acceptance criteria

**Goal:** an `edit` whose `old_string` is correct in content but off in
leading/trailing whitespace (indentation drift, tabs↔spaces, post-format) lands
**first try**, correctly indented, without a re-read — while exact matches and
the existing ambiguity/safety behavior are byte-for-byte unchanged.

Acceptance:

1. **Exact unchanged.** Any edit that succeeds today (exact or curly) takes the
   identical path and produces the identical result. The fallback is unreachable
   when `findActualString` already matched. (Regression-pinned by the existing
   `edit_test.go` suite passing untouched.)
2. **Line-trimmed match lands.** An `old_string` differing from the file only by
   per-line leading/trailing horizontal whitespace, matching exactly one
   contiguous file line-run, is found and applied.
3. **Indentation reconciled.** When the matched file block is indented
   differently from the model's `old_string`, `new_string` is re-indented by the
   same consistent delta so the result matches the file's indentation — not the
   model's.
4. **Unique or rejected.** A line-trimmed signature matching ≥2 distinct file
   regions is rejected with an "ambiguous — add more context" error; never
   silently resolved.
5. **No new false positives.** Blank-only `old_string`, single-blank-line
   signatures, and all-whitespace matches do not trigger the fallback (they would
   match everywhere). Markdown (`.md/.mdx`) trailing-space semantics preserved
   (the `new_string` strip already skips markdown — §2.2 path unchanged).
6. **`replace_all` coherent.** With `replace_all=true` and only the fallback
   matching, the resolved verbatim `actualOld` is replaced everywhere it occurs
   (same semantics as today, just a better-resolved needle).
7. **Observable.** When the fallback fires, the success summary notes it
   (`edited <path> … (whitespace-tolerant match)`) and `logger.Debug` records the
   strategy, so we can see in logs/telemetry how often exact missed.
8. **De-sanitize fallback ported** (Strategy 0.5) — `<fnr>`/`<n>`/… tokens
   resolve, mirroring ref.
9. `go build ./...` clean; `go test ./...` green; new table tests for each
   strategy, the ambiguity rejection, the indent reconciliation, the
   exact-still-wins guard, CRLF, and markdown.

---

## 4. Work breakdown (ordered)

### Task 1 — Extract a strategy-returning resolver (`pkg/tools/fs/match.go`, new)
Introduce a small matcher that the tool calls instead of `findActualString`
directly, preserving the existing return contract plus a strategy tag:

```go
type matchStrategy int
const (
    matchExact matchStrategy = iota   // strings.Contains
    matchCurly                        // quote-normalized
    matchDesanitize                   // ref token de-sanitize (Task 2)
    matchLineTrimmed                  // whitespace-tolerant (Task 3)
)

// resolveOldString returns the file's verbatim substring to replace.
//   found=false  → not matched by any strategy (caller emits the not-found hint)
//   ambiguous=true → a fallback matched >1 region (caller emits "add context")
func resolveOldString(content, old string) (actualOld string, strat matchStrategy, found, ambiguous bool)
```

`matchExact`/`matchCurly` delegate to the existing `findActualString` body
(move it in, keep behavior). `edit.go:179` becomes a call to `resolveOldString`;
the `!found` branch is unchanged; a new `ambiguous` branch emits the
ambiguity-style error.

### Task 2 — Strategy 0.5: de-sanitize (port ref `desanitizeMatchString`)
Port ref's `DESANITIZATIONS` map (`utils.ts:531`) and apply it as a normalize-
then-`strings.Contains` retry between curly and line-trimmed. Like curly, it must
map back to a **verbatim file substring** (the de-sanitization rewrites the
*search*, not the file — so once the de-sanitized search matches the real file
content, the matched region is already verbatim). Cheap, ref-blessed, closes a
known parity gap.

### Task 3 — Strategy 1: line-trimmed unique match (the core, `match.go`)
Algorithm (content is already LF-normalized by `readFileWithEncoding`):
1. Split `old` into logical lines on `\n`. Build `trimmedOld[i] = trimHoriz(line)`
   (`strings.Trim(line, " \t")`). **Bail** (no fallback) if every trimmed line is
   empty, or the signature is a single empty line (§5.1 — these match everywhere).
2. Build a line index over `content`: for each logical line, its `[startByte,
   endByte)` (end excludes the `\n`). Compute `trimHoriz` per file line lazily.
3. Slide a window of `len(trimmedOld)` lines over the file lines; a position
   matches when `trimHoriz(fileLine[k]) == trimmedOld[k]` for all `k`. Collect
   **all** matching start positions.
4. `len==0` → fall through (not found). `len>=2` → `ambiguous=true`. `len==1` →
   `actualOld = content[firstLine.startByte : lastLine.endByte]` (a verbatim
   substring; `strings.Contains(content, actualOld)` holds by construction).

### Task 4 — Indentation reconciliation (`match.go` + wire into `edit.go:209`)
When Strategy 1 matched, the file block and the model's `old` may have a
**consistent leading-whitespace delta** (e.g. file lines all carry two extra
leading spaces). Reconcile `new_string` so the result adopts the *file's*
indentation:
1. For each matched line pair, compute `filePrefix` = file line's leading
   horizontal ws, `oldPrefix` = model `old` line's leading horizontal ws.
2. If the relationship is **consistent** — every non-blank pair satisfies
   `filePrefix == delta + oldPrefix` for one fixed `delta` string (add) **or**
   `oldPrefix == removed + filePrefix` (strip) — apply that `delta` to every
   `new_string` line (add the prefix, or strip it). Blank `new_string` lines stay
   blank.
3. If **inconsistent** (mixed re-indent, or `new_string` already matches file
   indent), skip reconciliation and use `new_string` as given — never guess a
   per-line indent. (Conservative: a wrong-but-present edit beats a mangled one;
   tests pin this.)
This hooks in just before `preserveQuoteStyle` at `edit.go:213`, gated on
`strat == matchLineTrimmed` and non-markdown.

### Task 5 — Summary + logging (`edit.go`)
`editSummary` gains a strategy note suffix when `strat == matchLineTrimmed`
(`… (whitespace-tolerant match)`). `logger.Debug("edit.match", "strategy", …)`
on every dispatch. The diff/metadata payload is unchanged.

### Task 6 — Tests (`pkg/tools/fs/match_test.go` + extend `edit_test.go`)
Table tests per §7. Crucial cases: indent-drift (spaces↔tabs, ±N indent), the
exact-still-wins guard, ambiguous rejection, blank/whitespace-only no-trigger,
`replace_all` + fallback, CRLF file (terminator-preserving), `.md` two-space
break preserved, and a reconciliation-skip-on-inconsistent case.

### Task 7 — Docs + changelog
- `CHANGELOG.md` `[Unreleased]` → `### Changed`: "edit gains a whitespace/
  indentation-tolerant match fallback (after exact + curly), with indentation
  reconciliation; unique-or-reject, exact unchanged."
- User-guide: one line under the `edit` tool — the net behavior (edits survive
  indentation drift), not the algorithm (per the "document shipped features" rule).
- Tool **description unchanged** (§5.6).

---

## 5. Design decisions & risks (read before coding)

### 5.1 — The danger is over-matching; uniqueness + content-bearing signatures are the guard
A trimmed-line matcher is one bad heuristic away from replacing the wrong block.
Three rules contain it: (a) it runs **only** after exact+curly miss; (b) it must
match **exactly one** region or it rejects (no first-wins); (c) signatures that
are empty / all-blank / a single short line are refused outright — they would
match in many places and carry no anchoring content. A multi-line signature with
real tokens matching exactly once is overwhelmingly the intended block.

### 5.2 — Reconcile indentation, or "success" produces mis-indented code
Finding the block is half the job. If we splice the model's (wrongly-indented)
`new_string` into a differently-indented file region, we "succeed" but corrupt
formatting — worse than failing, because it looks done. The consistent-delta
reconciliation (Task 4) is what makes the feature *useful*, not just lenient. It
is deliberately conservative: only a single consistent add/strip delta is
applied; anything ambiguous falls back to verbatim `new_string`.

### 5.3 — `actualOld` MUST stay a verbatim file substring
Every downstream consumer (`strings.Count`, `applyEditToFile`'s `strings.Replace`,
`findAllOffsets`, the diff) assumes `actualOld` occurs literally in `content`.
The line-trimmed strategy therefore returns the **file's** bytes for the matched
span, never the model's trimmed reconstruction. This is the same contract curly-
matching already honors (`edit.go:362-374`) — we are extending it, not bending it.

### 5.4 — Interaction with the empty-`old_string` and create paths
The fallback is reached only on the normal edit path (non-empty `old_string`,
existing non-empty file). The create / empty-file / empty-`old_string` branches
(`edit.go:123,155`) return before the resolver and are untouched.

### 5.5 — CRLF and encoding
`mem.content` is LF-normalized; the matcher operates on LF content and returns LF
substrings, and the existing encoding-preserving write (`writeFileWithEncoding`
with `mem.lf == endCRLF`) re-applies the file's real line endings. The matcher
adds no CRLF awareness — it inherits the tool's existing normalization. A test
pins a CRLF file matching via the fallback.

### 5.6 — Keep it silent: do NOT advertise fuzzy matching in the description
If the tool description says "we tolerate whitespace," the model gets sloppy and
sends approximate `old_string`s on purpose, eroding precision and inviting the
over-match risk. The description stays "exact string replacements." The fallback
is a recovery net the model shouldn't plan around. (The success-summary note is
for *humans/logs*, after the fact.)

### 5.7 — Risks
- **Wrong-region replacement.** Mitigated by uniqueness + content-bearing
  signature rules (§5.1); residual risk is lower than today's status quo where
  the model, after a not-found, sometimes re-sends a slightly-wrong larger block.
- **Indent reconciliation mangles a deliberately ragged block.** Mitigated by the
  consistent-delta-only rule (§5.2) — ragged/inconsistent → verbatim `new_string`.
- **Performance on huge files.** The line index is O(lines); the slide is
  O(fileLines × oldLines) worst case. `MaxEditFileSize` (1 GiB) already bounds
  input; for normal source files this is negligible. If ever hot, cap the
  fallback to files under N lines (not needed for v1).

---

## 6. Out of scope (revisit later)
- **Block-anchor matching** (match on first+last line only, ignore the middle —
  oh-my-pi/aider "anchor"). Higher recall, materially higher wrong-region risk;
  only worth it behind stricter gates. Defer to a possible v2 once line-trimmed
  telemetry shows residual misses.
- **Fuzzy/similarity matching** (Levenshtein/token-ratio). Explicitly rejected —
  unbounded false-positive surface; the whole point is *whitespace* tolerance,
  not *content* tolerance.
- **A config kill-switch** (`edit_fuzzy_match`). Default-on, unflagged for v1
  (the safety rules make it strictly additive). Add a knob only if a real case
  demands disabling it.
- **`write`-tool / notebook matching.** This is `edit`-only.

## 7. Verification checklist (PR gate)
- [ ] Existing `edit_test.go` passes unmodified (exact path unchanged).
- [ ] Indent-drift (spaces↔tabs, ±N leading indent) matches and reconciles.
- [ ] Ambiguous trimmed signature (≥2 regions) → rejected, not applied.
- [ ] Blank-only / single-empty-line / all-whitespace `old_string` → no fallback.
- [ ] `replace_all=true` + fallback replaces all verbatim occurrences.
- [ ] CRLF file matches via fallback and writes back CRLF.
- [ ] `.md` file keeps two-trailing-space hard breaks.
- [ ] Inconsistent re-indent → `new_string` used verbatim (reconciliation skipped).
- [ ] De-sanitize (Strategy 0.5) resolves `<fnr>`-style tokens.
- [ ] Success summary notes the fallback; `logger.Debug` records strategy.
- [ ] `go build ./...`, `go test ./...`, `gofmt`, `go vet` clean.

## 8. File-by-file change list (cheat sheet)
| File | Change |
|---|---|
| `pkg/tools/fs/match.go` | **new** — `resolveOldString` (moves exact/curly in), de-sanitize, line-trimmed matcher, indent reconciliation (Tasks 1-4) |
| `pkg/tools/fs/edit.go` | call `resolveOldString` at :179; add `ambiguous` branch; wire reconciliation before :213; strategy note in `editSummary`; debug log (Tasks 1,4,5) |
| `pkg/tools/fs/match_test.go` | **new** — strategy + reconciliation table tests (Task 6) |
| `pkg/tools/fs/edit_test.go` | a few integration cases through `Execute` (Task 6) |
| `CHANGELOG.md` | `[Unreleased]` `### Changed` (Task 7) |
| `docs/user-guide/...` | one-line behavior note (Task 7) |
