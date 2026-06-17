package dream

import (
	"os"
	"testing"
	"time"
)

// ReadLastConsolidatedAt is zero with no lock, and ~now after an acquire.
func TestReadLastConsolidatedAt(t *testing.T) {
	dir := t.TempDir()
	if got := ReadLastConsolidatedAt(dir); !got.IsZero() {
		t.Errorf("absent lock: got %v, want zero", got)
	}
	if _, ok := TryAcquire(dir); !ok {
		t.Fatal("first TryAcquire should succeed")
	}
	at := ReadLastConsolidatedAt(dir)
	if at.IsZero() || time.Since(at) > time.Minute {
		t.Errorf("post-acquire mtime = %v, want ~now", at)
	}
}

// First acquire succeeds with a zero prior; an immediate second acquire is
// blocked (the within-window double-fire guard).
func TestTryAcquireBlocksWhenFresh(t *testing.T) {
	dir := t.TempDir()
	prior1, ok1 := TryAcquire(dir)
	if !ok1 || !prior1.IsZero() {
		t.Fatalf("first acquire: ok=%v prior=%v, want ok=true prior=zero", ok1, prior1)
	}
	if _, ok2 := TryAcquire(dir); ok2 {
		t.Error("second immediate acquire should be blocked (lock fresh)")
	}
}

// A lock older than staleAfter is reclaimable (a crashed holder).
func TestTryAcquireReclaimsStale(t *testing.T) {
	dir := t.TempDir()
	if _, ok := TryAcquire(dir); !ok {
		t.Fatal("first acquire should succeed")
	}
	old := time.Now().Add(-2 * staleAfter)
	if err := os.Chtimes(lockPath(dir), old, old); err != nil {
		t.Fatal(err)
	}
	prior, ok := TryAcquire(dir)
	if !ok {
		t.Fatal("stale lock should be reclaimable")
	}
	if time.Since(prior) < staleAfter {
		t.Errorf("reclaim prior = %v, want the backdated (stale) mtime", prior)
	}
}

// Rollback(zero) removes the lock; Rollback(prior) rewinds the mtime so the
// time-gate re-opens.
func TestRollback(t *testing.T) {
	dir := t.TempDir()

	if _, ok := TryAcquire(dir); !ok {
		t.Fatal("acquire")
	}
	Rollback(dir, time.Time{})
	if _, err := os.Stat(lockPath(dir)); !os.IsNotExist(err) {
		t.Error("Rollback(zero) should remove the lock file")
	}

	past := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.WriteFile(lockPath(dir), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(lockPath(dir), past, past); err != nil {
		t.Fatal(err)
	}
	prior, ok := TryAcquire(dir) // past is stale ⇒ reclaim, prior == past
	if !ok {
		t.Fatal("reclaim stale")
	}
	Rollback(dir, prior)
	if got := ReadLastConsolidatedAt(dir); got.Unix() != past.Unix() {
		t.Errorf("Rollback rewound mtime to %v, want %v", got, past)
	}
}
