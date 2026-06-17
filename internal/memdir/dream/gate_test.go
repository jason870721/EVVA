package dream

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedSessions(t *testing.T, appHome string, n int, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(appHome, "sessions", "proj") // SessionsSubdir layout
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		p := filepath.Join(dir, fmt.Sprintf("s%d.json", i))
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
}

// setLast plants a lock file at a chosen mtime to control lastConsolidatedAt.
func setLast(t *testing.T, memDir string, at time.Time) {
	t.Helper()
	p := lockPath(memDir)
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestGateOpen(t *testing.T) {
	now := time.Now()
	cfg := GateConfig{Enabled: true, MinHours: 24, MinSessions: 5}

	t.Run("disabled never fires", func(t *testing.T) {
		appHome, memDir := t.TempDir(), t.TempDir()
		d, _, err := GateOpen(GateInput{Cfg: GateConfig{Enabled: false}, AppHome: appHome, MemDir: memDir, Now: now})
		if err != nil || d.Fire {
			t.Fatalf("disabled should not fire: %+v err=%v", d, err)
		}
	})

	t.Run("time-gate blocks when recently consolidated", func(t *testing.T) {
		appHome, memDir := t.TempDir(), t.TempDir()
		setLast(t, memDir, now.Add(-1*time.Hour)) // 1h < 24h
		seedSessions(t, appHome, 10, now)
		d, _, _ := GateOpen(GateInput{Cfg: cfg, AppHome: appHome, MemDir: memDir, Now: now})
		if d.Fire {
			t.Errorf("should not fire within minHours: %s", d.Reason)
		}
	})

	t.Run("session-gate blocks when too few", func(t *testing.T) {
		appHome, memDir := t.TempDir(), t.TempDir()
		setLast(t, memDir, now.Add(-48*time.Hour)) // time-gate open
		seedSessions(t, appHome, 3, now)           // < 5
		d, _, _ := GateOpen(GateInput{Cfg: cfg, AppHome: appHome, MemDir: memDir, Now: now})
		if d.Fire {
			t.Errorf("should not fire below MinSessions: %s", d.Reason)
		}
		if d.Sessions != 3 {
			t.Errorf("Sessions = %d, want 3", d.Sessions)
		}
	})

	t.Run("scan-throttle blocks a fresh re-scan", func(t *testing.T) {
		appHome, memDir := t.TempDir(), t.TempDir()
		setLast(t, memDir, now.Add(-48*time.Hour))
		seedSessions(t, appHome, 10, now)
		d, _, _ := GateOpen(GateInput{Cfg: cfg, AppHome: appHome, MemDir: memDir, Now: now, LastScanAt: now.Add(-1 * time.Minute)})
		if d.Fire {
			t.Errorf("should not fire while scan-throttled: %s", d.Reason)
		}
	})

	t.Run("fires and acquires the lock when all gates pass", func(t *testing.T) {
		appHome, memDir := t.TempDir(), t.TempDir()
		setLast(t, memDir, now.Add(-48*time.Hour))
		seedSessions(t, appHome, 7, now)
		d, scanAt, err := GateOpen(GateInput{Cfg: cfg, AppHome: appHome, MemDir: memDir, Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if !d.Fire {
			t.Fatalf("should fire: %s", d.Reason)
		}
		if d.Sessions != 7 {
			t.Errorf("Sessions = %d, want 7", d.Sessions)
		}
		if !scanAt.Equal(now) {
			t.Errorf("scan cursor should advance to now")
		}
		// The fire bumped the lock mtime to ~now, so an immediate re-gate is
		// blocked by the time-gate — proving the lock+timestamp double duty.
		if d2, _, _ := GateOpen(GateInput{Cfg: cfg, AppHome: appHome, MemDir: memDir, Now: now}); d2.Fire {
			t.Error("second gate immediately after a fire should not fire again")
		}
	})
}
