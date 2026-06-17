package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/johnny1110/evva/pkg/event"
	"github.com/johnny1110/evva/pkg/permission"
	"github.com/johnny1110/evva/pkg/tools"
)

// The dream agent's broker auto-denies any approval that reaches it — i.e. an
// attempt to write outside the memory dir — so a background pass never hangs on
// a prompt no one is watching.
func TestAutoDenyBroker(t *testing.T) {
	d, err := autoDenyBroker{}.Request(context.Background(), permission.ApprovalRequest{})
	if err != nil {
		t.Fatalf("Request err = %v", err)
	}
	if d.Behavior != permission.BehaviorDeny {
		t.Errorf("Request behavior = %v, want Deny", d.Behavior)
	}
}

// dreamSink captures the summary (last text block) and the basenames of
// written/edited files — deduped, sorted — while ignoring reads and other tools.
func TestDreamSink(t *testing.T) {
	s := newDreamSink()
	emitText := func(txt string) {
		s.Emit(event.Event{Kind: event.KindText, Text: &event.TextPayload{Text: txt}})
	}
	emitTool := func(name, path string) {
		in, _ := json.Marshal(map[string]string{"file_path": path})
		s.Emit(event.Event{Kind: event.KindToolUseStart, ToolUseStart: &event.ToolUseStartPayload{Name: name, Input: in}})
	}

	emitText("first draft")
	emitTool(string(tools.READ_FILE), "/mem/should_not_count.md") // reads never count
	emitTool(string(tools.GREP), "/mem/whatever")                 // ditto
	emitTool(string(tools.WRITE_FILE), "/mem/b.md")
	emitTool(string(tools.EDIT_FILE), "/mem/a.md")
	emitTool(string(tools.EDIT_FILE), "/mem/a.md") // dup → counted once
	emitText("final summary")                      // last text block wins

	summary, files := s.result()
	if summary != "final summary" {
		t.Errorf("summary = %q, want %q", summary, "final summary")
	}
	if len(files) != 2 || files[0] != "a.md" || files[1] != "b.md" {
		t.Errorf("files = %v, want [a.md b.md] (sorted, deduped, reads excluded)", files)
	}
}
