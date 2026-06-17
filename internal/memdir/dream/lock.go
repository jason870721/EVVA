// Package dream is the background memory-consolidation ("dream") subsystem:
// a gated, fenced pass that merges, prunes, and re-indexes the global memory
// store. This package holds the stdlib-leaning primitives — the lock+timestamp,
// the fire gate, and the consolidation prompt; the agent orchestration that
// runs the fenced subagent lives in internal/agent (it needs agent.New).
//
// Ported from ref/src/services/autoDream/ (consolidationLock.ts, autoDream.ts
// gate order, consolidationPrompt.ts). See docs/roadmap/PRD/auto-dream.md.
package dream

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// lockFile sits inside the memory dir, so it keys on the same global store the
// memory does and is writable wherever the memory dir is. Its mtime IS the
// last-consolidated timestamp; its body is the holder PID (a two-writer
// tie-break, not a liveness probe — see TryAcquire).
const lockFile = ".consolidate-lock"

// staleAfter bounds how long a claimed lock blocks a fresh trigger. It must be
// comfortably longer than a dream run (minutes) and shorter than the time-gate
// (AutoDreamMinHours, ≥24h): long enough that an in-flight dream's lock is not
// reclaimed mid-run, short enough that a crash without rollback doesn't wedge
// consolidation for a full day on top of the time-gate. A hard crash still
// can't double-fire (the bumped mtime also fails the time-gate for ≥minHours).
const staleAfter = time.Hour

func lockPath(memDir string) string { return filepath.Join(memDir, lockFile) }

// ReadLastConsolidatedAt returns the lock file's mtime — the moment the last
// consolidation claimed the lock — or the zero Time when dream has never run
// (no file). One stat; this is the cheap first gate. Empty memDir ⇒ zero.
func ReadLastConsolidatedAt(memDir string) time.Time {
	if memDir == "" {
		return time.Time{}
	}
	fi, err := os.Stat(lockPath(memDir))
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// TryAcquire claims the consolidation lock by stamping the lock file's mtime to
// now. It returns the PRIOR mtime (pass to Rollback on a failed run) and
// ok=true on success. ok=false when the lock was claimed within staleAfter —
// i.e. another trigger (this process or another) is already consolidating. A
// lock older than staleAfter is reclaimed (a crashed holder).
//
// The PID body breaks a two-writer race: if two triggers both see a
// stale/absent lock and both write, the last writer's PID wins and the loser
// bails on re-read. It is NOT a liveness probe (that would need
// platform-specific process checks); staleAfter is the crash backstop instead.
func TryAcquire(memDir string) (prior time.Time, ok bool) {
	if memDir == "" {
		return time.Time{}, false
	}
	path := lockPath(memDir)
	if fi, err := os.Stat(path); err == nil {
		prior = fi.ModTime()
		if time.Since(prior) < staleAfter {
			return prior, false // freshly held — another consolidation is live
		}
	}
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		return prior, false
	}
	pid := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(path, []byte(pid), 0o644); err != nil {
		return prior, false
	}
	now := time.Now()
	_ = os.Chtimes(path, now, now)
	if b, err := os.ReadFile(path); err != nil || strings.TrimSpace(string(b)) != pid {
		return prior, false // lost the race to a concurrent writer
	}
	return prior, true
}

// Rollback rewinds the lock after a failed, cancelled, or killed run so the
// time-gate re-opens at the next idle instead of waiting a full AutoDreamMinHours.
// A zero prior (the lock did not exist before this acquire) removes the file,
// restoring the "never ran" state. Best-effort: a rollback error only delays the
// next trigger to the time-gate, it never corrupts state.
func Rollback(memDir string, prior time.Time) {
	if memDir == "" {
		return
	}
	path := lockPath(memDir)
	if prior.IsZero() {
		_ = os.Remove(path)
		return
	}
	_ = os.WriteFile(path, nil, 0o644)
	_ = os.Chtimes(path, prior, prior)
}
