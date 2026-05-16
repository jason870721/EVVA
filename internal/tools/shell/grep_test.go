package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Phase 1 analysis — GrepTool.Execute code paths:
//   - decode input
//   - empty pattern rejected
//   - invalid regex rejected
//   - non-absolute path rejected
//   - default mode = "content" (path:line:text rows)
//   - mode = "files_with_matches" → unique paths
//   - mode = "count" → "path:count" rows
//   - case_insensitive prefixes (?i)
//   - glob filter narrows by filename
//   - head_limit caps output rows
//   - vendored / .git dirs auto-skipped
//   - binary files (containing NUL) skipped
//   - no-match → "(no matches)" placeholder

// writeGrepFixture creates a directory tree with seeded text files so
// each grep test gets an isolated, hermetic search space. Returns the
// absolute root path.
func writeGrepFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	mustWrite := func(rel, content string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}

	mustWrite("alpha.go", "package alpha\n\nfunc Foo() {}\nfunc Bar() {}\n")
	mustWrite("beta.go", "package beta\n\nfunc Foo() {}\n")
	mustWrite("notes.md", "hello FOO world\nplain line\n")
	mustWrite("nested/inner.go", "package inner\n\nfunc Bar() {}\n")
	// Should be skipped automatically by skipDirs.
	mustWrite(".git/config", "func Foo() {}\n")
	mustWrite("node_modules/lib.js", "function Foo() {}\n")
	// A binary file with an embedded NUL — must be skipped by the
	// binary sniff.
	mustWrite("blob.bin", "\x00\x01func Foo()\x02")

	return root
}

func TestGrep_RejectsEmptyPattern(t *testing.T) {
	tool := &GrepTool{}
	res, _ := tool.Execute(context.Background(), json.RawMessage(`{"pattern":""}`))
	if !res.IsError || !strings.Contains(res.Content, "required") {
		t.Errorf("expected 'required'; got %q", res.Content)
	}
}

func TestGrep_RejectsInvalidRegex(t *testing.T) {
	tool := &GrepTool{}
	res, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"pattern":"[unclosed"}`))
	if !res.IsError || !strings.Contains(res.Content, "bad pattern") {
		t.Errorf("expected 'bad pattern'; got %q", res.Content)
	}
}

func TestGrep_RejectsRelativePath(t *testing.T) {
	tool := &GrepTool{}
	res, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"pattern":"x","path":"relative/dir"}`))
	if !res.IsError || !strings.Contains(res.Content, "absolute") {
		t.Errorf("expected absolute-path rejection; got %q", res.Content)
	}
}

func TestGrep_ContentMode_DefaultsToPathLineText(t *testing.T) {
	root := writeGrepFixture(t)
	tool := &GrepTool{}

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"pattern":"func Foo","path":%q}`, root)))

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "alpha.go:3:func Foo()") {
		t.Errorf("expected alpha.go hit; got %q", res.Content)
	}
	if !strings.Contains(res.Content, "beta.go:3:func Foo()") {
		t.Errorf("expected beta.go hit; got %q", res.Content)
	}
	// Vendored dirs must be skipped.
	if strings.Contains(res.Content, ".git/config") {
		t.Error(".git contents leaked into result")
	}
	if strings.Contains(res.Content, "node_modules") {
		t.Error("node_modules contents leaked into result")
	}
	// Binary file must be skipped.
	if strings.Contains(res.Content, "blob.bin") {
		t.Error("binary file should be skipped by NUL sniff")
	}
}

func TestGrep_FilesWithMatchesMode(t *testing.T) {
	root := writeGrepFixture(t)
	tool := &GrepTool{}

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"pattern":"func Foo","path":%q,"output_mode":"files_with_matches"}`, root)))

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	wantContains := []string{"alpha.go", "beta.go"}
	for _, w := range wantContains {
		if !strings.Contains(res.Content, w) {
			t.Errorf("missing %q in files_with_matches output: %s", w, res.Content)
		}
	}
	// In this mode the body should NOT contain "func Foo" text — only paths.
	if strings.Contains(res.Content, "func Foo") {
		t.Errorf("files_with_matches leaked match content: %s", res.Content)
	}
}

func TestGrep_CountMode(t *testing.T) {
	root := writeGrepFixture(t)
	tool := &GrepTool{}

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"pattern":"func ","path":%q,"output_mode":"count"}`, root)))

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	// alpha.go has 2 matches (Foo + Bar), beta.go has 1, inner.go has 1.
	if !strings.Contains(res.Content, "alpha.go:2") {
		t.Errorf("expected alpha.go:2 in count output; got %s", res.Content)
	}
	if !strings.Contains(res.Content, "beta.go:1") {
		t.Errorf("expected beta.go:1 in count output; got %s", res.Content)
	}
}

func TestGrep_CaseInsensitive(t *testing.T) {
	root := writeGrepFixture(t)
	tool := &GrepTool{}

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"pattern":"foo","path":%q,"case_insensitive":true}`, root)))

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	// notes.md has "FOO" (uppercase); only case-insensitive search finds it.
	if !strings.Contains(res.Content, "notes.md") {
		t.Errorf("case_insensitive should match FOO in notes.md; got %q", res.Content)
	}
}

func TestGrep_GlobFiltersByFilename(t *testing.T) {
	root := writeGrepFixture(t)
	tool := &GrepTool{}

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"pattern":"hello","path":%q,"glob":"*.md"}`, root)))

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "notes.md") {
		t.Errorf("expected notes.md hit; got %q", res.Content)
	}
}

func TestGrep_GlobExcludesNonMatching(t *testing.T) {
	root := writeGrepFixture(t)
	tool := &GrepTool{}

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"pattern":"func Foo","path":%q,"glob":"*.md"}`, root)))

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	// glob=*.md → .go files not searched → no matches.
	if !strings.Contains(res.Content, "(no matches)") {
		t.Errorf("expected '(no matches)' since *.md files don't contain 'func Foo'; got %q", res.Content)
	}
}

func TestGrep_HeadLimitCapsOutput(t *testing.T) {
	root := writeGrepFixture(t)
	tool := &GrepTool{}

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"pattern":"func ","path":%q,"head_limit":1}`, root)))

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	lines := strings.Split(strings.TrimRight(res.Content, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line under head_limit=1; got %d:\n%s", len(lines), res.Content)
	}
}

func TestGrep_NoMatchesPlaceholder(t *testing.T) {
	root := writeGrepFixture(t)
	tool := &GrepTool{}

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"pattern":"nonexistent-symbol","path":%q}`, root)))

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "(no matches)") {
		t.Errorf("expected '(no matches)' placeholder; got %q", res.Content)
	}
}

func TestGrep_SingleFilePath(t *testing.T) {
	root := writeGrepFixture(t)
	tool := &GrepTool{}

	res, _ := tool.Execute(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"pattern":"Bar","path":%q}`, filepath.Join(root, "alpha.go"))))

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "alpha.go:4:func Bar()") {
		t.Errorf("expected alpha.go:4 hit; got %q", res.Content)
	}
}

func TestGrep_DecodeError(t *testing.T) {
	tool := &GrepTool{}
	res, _ := tool.Execute(context.Background(), json.RawMessage(`{nope`))
	if !res.IsError || !strings.Contains(res.Content, "decode") {
		t.Errorf("expected decode error; got %q", res.Content)
	}
}
