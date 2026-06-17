package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeSessionFile(t *testing.T, appHome, slug, id string, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(appHome, SessionsSubdir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, id+sessionFileSuffix)
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// CountTouchedSince spans every workdir slug (memory is global), honors the
// `since` cutoff, and excludes the current session.
func TestCountTouchedSince(t *testing.T) {
	home := t.TempDir()
	since := time.Now().Add(-1 * time.Hour)
	old := since.Add(-2 * time.Hour)      // before the cutoff
	recent := since.Add(30 * time.Minute) // after the cutoff

	writeSessionFile(t, home, "proj-a", "s1", recent)
	writeSessionFile(t, home, "proj-a", "s2", old)
	writeSessionFile(t, home, "proj-b", "s3", recent)
	writeSessionFile(t, home, "proj-b", "cur", recent) // current — excluded

	n, err := CountTouchedSince(home, since, "cur")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 { // s1 + s3; s2 too old; cur excluded
		t.Errorf("CountTouchedSince = %d, want 2 (s1+s3 across both slugs)", n)
	}
}

// A missing sessions root is the normal "no prior sessions" state: 0, no error.
func TestCountTouchedSinceMissingRoot(t *testing.T) {
	n, err := CountTouchedSince(t.TempDir(), time.Now(), "")
	if err != nil {
		t.Fatalf("missing root should be 0/nil, got err %v", err)
	}
	if n != 0 {
		t.Errorf("missing root count = %d, want 0", n)
	}
}
