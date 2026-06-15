package overlays

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	config "github.com/johnny1110/evva/pkg/config"
	"github.com/johnny1110/evva/pkg/ui/bubbletea/theme"
)

// newGroupConfig builds a Config with one scalar row and one drill-in group
// (two nested rows), bypassing NewConfig so the test needs no controller.
func newGroupConfig() *Config {
	noop := func(string) error { return nil }
	return &Config{fields: []ConfigField{
		{Label: "display_thinking", Kind: cfgKindBool, Get: func() string { return "false" }, Apply: noop},
		{Label: "llm-provider", Kind: cfgKindGroup, Children: []ConfigField{
			{Label: "anthropic.api_key", Kind: cfgKindSecret, Get: func() string { return "" }, Apply: noop},
			{Label: "anthropic.api_url", Kind: cfgKindString, Get: func() string { return "" }, Apply: noop},
		}},
	}}
}

// TestConfigGroupDrillIn — Enter on a group row opens its children as the
// active list; Esc backs out to the parent (restoring the cursor) instead of
// closing; a second Esc at the top level closes.
func TestConfigGroupDrillIn(t *testing.T) {
	c := newGroupConfig()

	// Move onto the group row.
	if close, _ := c.Update(tea.KeyMsg{Type: tea.KeyDown}); close {
		t.Fatal("Down should not close")
	}
	if c.sel != 1 {
		t.Fatalf("expected sel=1 (group row), got %d", c.sel)
	}

	// Enter drills in.
	if close, _ := c.Update(tea.KeyMsg{Type: tea.KeyEnter}); close {
		t.Fatal("Enter on group should not close")
	}
	if c.groupFields == nil {
		t.Fatal("Enter on group should drill into its children")
	}
	if got := c.current(); len(got) != 2 || got[0].Label != "anthropic.api_key" {
		t.Fatalf("current() should be the group children, got %v", labels(got))
	}
	if c.sel != 0 {
		t.Fatalf("cursor should reset to 0 inside group, got %d", c.sel)
	}

	// Esc backs out without closing, restoring the parent cursor.
	if close, _ := c.Update(tea.KeyMsg{Type: tea.KeyEsc}); close {
		t.Fatal("Esc inside group should back out, not close")
	}
	if c.groupFields != nil {
		t.Fatal("Esc should leave the group")
	}
	if c.sel != 1 {
		t.Fatalf("parent cursor should be restored to 1, got %d", c.sel)
	}

	// Esc at the top level closes.
	if close, _ := c.Update(tea.KeyMsg{Type: tea.KeyEsc}); !close {
		t.Fatal("Esc at top level should close")
	}
}

// TestConfigGroupViewBreadcrumb — the group sub-view shows the breadcrumb
// header and lists the nested provider rows, not the parent scalar.
func TestConfigGroupViewBreadcrumb(t *testing.T) {
	c := newGroupConfig()
	c.sel = 1
	c.Update(tea.KeyMsg{Type: tea.KeyEnter}) // drill in

	out := c.View(80, theme.Default())
	if !strings.Contains(out, "/CONFIG ▸ llm-provider") {
		t.Errorf("group view should show breadcrumb header: %q", out)
	}
	if !strings.Contains(out, "anthropic.api_key") {
		t.Errorf("group view should list nested provider rows: %q", out)
	}
	if !strings.Contains(out, "[Esc] back") {
		t.Errorf("group footer should hint Esc backs out: %q", out)
	}
}

// TestBuildProviderFieldsGroupsAllProviders — the provider group is built from
// the registry, includes GLM, and pins Ollama's lone api_url row last.
func TestBuildProviderFieldsGroupsAllProviders(t *testing.T) {
	fields := buildProviderFields(&config.Config{})
	ls := labels(fields)
	joined := strings.Join(ls, ",")
	for _, want := range []string{"anthropic.api_key", "glm.api_key", "glm.api_url", "openai.api_url"} {
		if !strings.Contains(joined, want) {
			t.Errorf("provider fields missing %q; got %v", want, ls)
		}
	}
	// Ollama is key-less: only its api_url, and it must be the final row.
	if joined := strings.Join(ls, ","); strings.Contains(joined, "ollama.api_key") {
		t.Errorf("ollama should have no api_key row; got %v", ls)
	}
	if ls[len(ls)-1] != "ollama.api_url" {
		t.Errorf("ollama.api_url should be the last row; got %v", ls)
	}
}

func labels(fields []ConfigField) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = f.Label
	}
	return out
}
