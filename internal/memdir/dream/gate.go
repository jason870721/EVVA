package dream

import (
	"fmt"
	"time"

	"github.com/johnny1110/evva/internal/session"
)

// ScanThrottle bounds how often the session-gate re-scans the sessions tree.
// When the time-gate passes but the session-gate won't (not enough new
// activity), the lock mtime doesn't advance, so the time-gate keeps passing
// every idle. Without a throttle that would re-walk every workdir slug on each
// idle. Ported from ref's SESSION_SCAN_INTERVAL_MS.
const ScanThrottle = 10 * time.Minute

// GateConfig is the slice of runtime config the gate needs, passed as a plain
// struct so this package stays free of pkg/config and the gate is table-
// testable without constructing a full Config. The agent layer adapts the live
// config into this.
type GateConfig struct {
	Enabled     bool
	MinHours    int // ≤0 → 24
	MinSessions int // ≤0 → 5
}

// GateInput bundles everything GateOpen reads. Now and LastScanAt are passed in
// (not read from the clock) so tests are deterministic; LastScanAt is the
// caller-held, in-memory scan-throttle cursor.
type GateInput struct {
	Cfg              GateConfig
	AppHome          string
	MemDir           string
	CurrentSessionID string
	Now              time.Time
	LastScanAt       time.Time
}

// Decision is the gate outcome. When Fire is true the lock has ALREADY been
// acquired and Prior carries its pre-acquire mtime — the caller MUST run the
// dream and, on any failure/cancel, call Rollback(MemDir, Prior) so the
// time-gate re-opens. Reason is for debug/telemetry on every path.
type Decision struct {
	Fire     bool
	Prior    time.Time
	Sessions int
	Reason   string
}

// GateOpen evaluates the fire gate in cheapest-first order — enabled → time →
// scan-throttle → sessions → lock — and acquires the lock as its final,
// side-effecting step iff every gate passes. It returns the Decision plus the
// (possibly advanced) scan cursor for the caller to store. Pure up to the
// CountTouchedSince scan + the TryAcquire at the end.
func GateOpen(in GateInput) (Decision, time.Time, error) {
	scanAt := in.LastScanAt

	if !in.Cfg.Enabled {
		return Decision{Reason: "disabled"}, scanAt, nil
	}
	if in.MemDir == "" {
		return Decision{Reason: "no memory dir"}, scanAt, nil
	}

	// 1. Time gate — one stat.
	last := ReadLastConsolidatedAt(in.MemDir)
	minHours := in.Cfg.MinHours
	if minHours <= 0 {
		minHours = 24
	}
	if in.Now.Sub(last) < time.Duration(minHours)*time.Hour {
		return Decision{Reason: "time-gate not reached"}, scanAt, nil
	}

	// 2. Scan throttle — avoid re-walking the sessions tree every idle while
	// the time-gate keeps passing but activity stays below MinSessions.
	if !in.LastScanAt.IsZero() && in.Now.Sub(in.LastScanAt) < ScanThrottle {
		return Decision{Reason: "scan-throttled"}, scanAt, nil
	}
	scanAt = in.Now

	// 3. Session gate — count activity across all workdir slugs since `last`.
	minSessions := in.Cfg.MinSessions
	if minSessions <= 0 {
		minSessions = 5
	}
	n, err := session.CountTouchedSince(in.AppHome, last, in.CurrentSessionID)
	if err != nil {
		return Decision{Reason: "session-scan error"}, scanAt, err
	}
	if n < minSessions {
		return Decision{Sessions: n, Reason: fmt.Sprintf("session-gate (%d/%d)", n, minSessions)}, scanAt, nil
	}

	// 4. Lock — the final, side-effecting claim.
	prior, ok := TryAcquire(in.MemDir)
	if !ok {
		return Decision{Sessions: n, Reason: "lock held"}, scanAt, nil
	}
	return Decision{
		Fire:     true,
		Prior:    prior,
		Sessions: n,
		Reason:   fmt.Sprintf("fire — %d sessions, %.0fh since last", n, in.Now.Sub(last).Hours()),
	}, scanAt, nil
}
