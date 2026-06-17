package config

import "testing"

// The auto-dream knobs default to opt-in-off with seeded thresholds, and
// survive a persist + reload round-trip.
func TestAutoDreamConfigDefaultsAndRoundTrip(t *testing.T) {
	home := t.TempDir()
	wd := t.TempDir()

	cfg, err := Load(LoadOptions{AppName: "alpha", AppHome: home, WorkDir: wd})
	if err != nil {
		t.Fatal(err)
	}

	// Defaults: opt-in off, thresholds seeded to 24h / 5 sessions, no model pin.
	if cfg.GetEnableAutoDream() {
		t.Error("EnableAutoDream should default to false (opt-in)")
	}
	if got := cfg.GetAutoDreamMinHours(); got != 24 {
		t.Errorf("AutoDreamMinHours default = %d, want 24", got)
	}
	if got := cfg.GetAutoDreamMinSessions(); got != 5 {
		t.Errorf("AutoDreamMinSessions default = %d, want 5", got)
	}
	if got := cfg.GetAutoDreamModel(); got != "" {
		t.Errorf("AutoDreamModel default = %q, want empty", got)
	}

	// Persist a toggle + a model pin, then reload from the same home.
	if err := cfg.SetEnableAutoDream(true); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetAutoDreamModel("glm-4.6"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(LoadOptions{AppName: "alpha", AppHome: home, WorkDir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.GetEnableAutoDream() {
		t.Error("EnableAutoDream should persist as true across reload")
	}
	if got := reloaded.GetAutoDreamModel(); got != "glm-4.6" {
		t.Errorf("AutoDreamModel round-trip = %q, want glm-4.6", got)
	}
	// Thresholds remain at their defaults after a round-trip.
	if got := reloaded.GetAutoDreamMinHours(); got != 24 {
		t.Errorf("AutoDreamMinHours after reload = %d, want 24", got)
	}
	if got := reloaded.GetAutoDreamMinSessions(); got != 5 {
		t.Errorf("AutoDreamMinSessions after reload = %d, want 5", got)
	}
}

// A non-positive threshold in YAML normalizes to the default rather than
// disabling the gate (a 0 floor would let dream fire every idle).
func TestAutoDreamThresholdsNormalizeNonPositive(t *testing.T) {
	home := t.TempDir()
	wd := t.TempDir()
	cfg, err := Load(LoadOptions{AppName: "alpha", AppHome: home, WorkDir: wd})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a hand-edited config.yml with explicit zeros by round-tripping
	// through the file layer: zeros are omitempty, so they vanish and load
	// re-seeds the defaults.
	cfg.AutoDreamMinHours = 0
	cfg.AutoDreamMinSessions = 0
	if err := cfg.SaveFile(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(LoadOptions{AppName: "alpha", AppHome: home, WorkDir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.GetAutoDreamMinHours(); got != 24 {
		t.Errorf("zero min_hours should normalize to 24, got %d", got)
	}
	if got := reloaded.GetAutoDreamMinSessions(); got != 5 {
		t.Errorf("zero min_sessions should normalize to 5, got %d", got)
	}
}
