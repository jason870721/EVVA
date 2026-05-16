package fs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Phase 1 analysis — WriteTool.Execute code paths:
//   - decode input
//   - resolvePath errors
//   - new-file path (no read-guard required)
//   - overwrite path requires prior read via tracker
//   - approver decline w/o feedback → IsError
//   - approver decline w/ feedback → non-error result carrying feedback
//   - approver approves → file mutated, Metadata holds FileDiff
//   - auto-mkdir for missing parents
//   - empty content writes empty file

// fakeApprover implements Approver with a programmable Decision.
type fakeApprover struct {
	dec  Decision
	err  error
	seen *FileDiff
}

func (f *fakeApprover) Approve(_ context.Context, diff *FileDiff) (Decision, error) {
	f.seen = diff
	return f.dec, f.err
}

func TestWrite_NewFileSkipsReadGuard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	tool := NewWrite(NewReadTracker(), nil) // nil approver = auto-approve

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"file_path":"`+path+`","content":"hello"}`))

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content: got %q, want %q", string(got), "hello")
	}
	if !strings.Contains(res.Content, "created") {
		t.Errorf("expected 'created' summary, got %q", res.Content)
	}
}

func TestWrite_OverwriteBlockedWithoutPriorRead(t *testing.T) {
	path := writeTempFile(t, "old")
	tool := NewWrite(NewReadTracker(), nil)

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"file_path":"`+path+`","content":"new"}`))

	if !res.IsError {
		t.Fatal("expected guard to block overwrite without prior read")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "old" {
		t.Errorf("file should NOT have been modified; got %q", string(got))
	}
}

func TestWrite_OverwriteAllowedAfterRead(t *testing.T) {
	path := writeTempFile(t, "old")
	tr := NewReadTracker()
	tr.MarkRead(path)
	tool := NewWrite(tr, nil)

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"file_path":"`+path+`","content":"new"}`))

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("file content: got %q, want %q", string(got), "new")
	}
	if !strings.Contains(res.Content, "overwrote") {
		t.Errorf("expected 'overwrote' summary; got %q", res.Content)
	}
	if _, ok := res.Metadata.(*FileDiff); !ok {
		t.Error("expected Metadata to carry *FileDiff for overwrite")
	}
}

func TestWrite_ApproverDeclineNoFeedback_IsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	approver := &fakeApprover{dec: Decision{Approved: false}}
	tool := NewWrite(NewReadTracker(), approver)

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"file_path":"`+path+`","content":"v"}`))

	if !res.IsError {
		t.Fatal("expected IsError when approver declines without feedback")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("file was created despite decline")
	}
	if approver.seen == nil {
		t.Error("approver was not consulted")
	}
}

func TestWrite_ApproverDeclineWithFeedback_IsNonError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	approver := &fakeApprover{dec: Decision{Approved: false, Feedback: "use yaml instead"}}
	tool := NewWrite(NewReadTracker(), approver)

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"file_path":"`+path+`","content":"v"}`))

	if res.IsError {
		t.Errorf("decline-with-feedback should be a non-error result; got IsError, content=%q", res.Content)
	}
	if !strings.Contains(res.Content, "use yaml instead") {
		t.Errorf("feedback text missing from content: %q", res.Content)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("file was created despite decline")
	}
}

func TestWrite_ApproverErrorSurfacedAsIsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	approver := &fakeApprover{err: errors.New("approver broke")}
	tool := NewWrite(NewReadTracker(), approver)

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"file_path":"`+path+`","content":"v"}`))

	if !res.IsError || !strings.Contains(res.Content, "approver broke") {
		t.Errorf("expected approver error surfaced; got isErr=%v content=%q", res.IsError, res.Content)
	}
}

func TestWrite_AutoMkdirsMissingParents(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "a", "b", "c", "f.txt")
	tool := NewWrite(NewReadTracker(), nil)

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"file_path":"`+deep+`","content":"x"}`))

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if _, err := os.Stat(deep); err != nil {
		t.Errorf("deep path not created: %v", err)
	}
}

func TestWrite_EmptyContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	tool := NewWrite(NewReadTracker(), nil)

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"file_path":"`+path+`","content":""}`))

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("empty content size: got %d, want 0", info.Size())
	}
}

func TestWrite_DecodeError(t *testing.T) {
	tool := NewWrite(NewReadTracker(), nil)
	res, _ := tool.Execute(context.Background(), json.RawMessage(`{not json`))
	if !res.IsError || !strings.Contains(res.Content, "decode") {
		t.Errorf("expected decode error; got isErr=%v content=%q", res.IsError, res.Content)
	}
}
