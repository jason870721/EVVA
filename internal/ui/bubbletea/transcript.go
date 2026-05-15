package bubbletea

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/johnny1110/evva/internal/agent/event"
	"github.com/johnny1110/evva/internal/tools/fs"
)

// transcript accumulates a scrollback buffer of human-readable lines from
// the agent's event stream. It is intentionally view-only: foldEvent
// appends pre-styled strings; the parent model is responsible for
// rendering them inside a viewport.
//
// Each entry is one logical "block" (a user prompt, an assistant text
// turn, a single tool call + result, an error banner, ...). Blocks may
// contain internal newlines.
//
// Streaming chunks (KindTextChunk / KindThinkingChunk) accumulate into a
// single in-flight block per kind — textInflightIdx / thinkingInflightIdx
// track which scrollback entry to append into. Both reset to -1 on
// KindTurnEnd so the next assistant turn starts fresh blocks. They also
// reset whenever a buffered KindText / KindThinking arrives, so a single
// turn never produces both a streamed block and a buffered duplicate.
type transcript struct {
	blocks              []string
	textInflightIdx     int
	thinkingInflightIdx int
	// rawText holds the unstyled accumulator for the in-flight text block.
	// Lipgloss re-styles the full string every chunk; without keeping the
	// raw text around we'd recurse styling on already-styled output.
	rawText     string
	rawThinking string
}

// String returns the entire scrollback as one newline-joined buffer.
func (t *transcript) String() string {
	return strings.Join(t.blocks, "\n\n")
}

// appendUserPrompt records a prompt the user just submitted.
func (t *transcript) appendUserPrompt(text string) {
	t.resetInflight()
	t.blocks = append(t.blocks, styles.UserPrompt.Render("> "+text))
}

// resetInflight closes the active streamed text/thinking blocks so the next
// chunk starts a fresh entry. Called whenever a non-chunk event interrupts
// the stream (turn end, tool call, error, user prompt, buffered text).
func (t *transcript) resetInflight() {
	t.textInflightIdx = -1
	t.thinkingInflightIdx = -1
	t.rawText = ""
	t.rawThinking = ""
}

// foldEvent translates one agent event into a transcript entry (or
// updates an in-flight one). Returns true if the transcript changed and
// the viewport should re-render.
func (t *transcript) foldEvent(e event.Event) bool {
	switch e.Kind {
	case event.KindThinking:
		t.resetInflight()
		if e.Thinking != nil && e.Thinking.Text != "" {
			t.blocks = append(t.blocks, styles.Thinking.Render("· "+truncate(e.Thinking.Text, 800)))
			return true
		}
	case event.KindText:
		t.resetInflight()
		if e.Text != nil && e.Text.Text != "" {
			t.blocks = append(t.blocks, styles.Assistant.Render(e.Text.Text))
			return true
		}
	case event.KindThinkingChunk:
		if e.Thinking == nil || e.Thinking.Text == "" {
			return false
		}
		t.rawThinking += e.Thinking.Text
		rendered := styles.Thinking.Render("· " + truncate(t.rawThinking, 800))
		if t.thinkingInflightIdx >= 0 && t.thinkingInflightIdx < len(t.blocks) {
			t.blocks[t.thinkingInflightIdx] = rendered
		} else {
			t.blocks = append(t.blocks, rendered)
			t.thinkingInflightIdx = len(t.blocks) - 1
		}
		return true
	case event.KindTextChunk:
		if e.Text == nil || e.Text.Text == "" {
			return false
		}
		t.rawText += e.Text.Text
		rendered := styles.Assistant.Render(t.rawText)
		if t.textInflightIdx >= 0 && t.textInflightIdx < len(t.blocks) {
			t.blocks[t.textInflightIdx] = rendered
		} else {
			t.blocks = append(t.blocks, rendered)
			t.textInflightIdx = len(t.blocks) - 1
		}
		return true
	case event.KindToolUseStart:
		if e.ToolUseStart != nil {
			label := fmt.Sprintf("→ %s %s", e.ToolUseStart.Name, compactInput(e.ToolUseStart.Input))
			t.blocks = append(t.blocks, styles.ToolCall.Render(label))
			return true
		}
	case event.KindToolUseResult:
		if e.ToolUseResult != nil {
			var b strings.Builder
			if e.ToolUseResult.IsError {
				b.WriteString(styles.ToolErr.Render("✗ " + truncate(e.ToolUseResult.Content, 800)))
			} else {
				b.WriteString(styles.ToolOK.Render("✓ " + truncate(e.ToolUseResult.Content, 800)))
			}
			if diff, ok := e.ToolUseResult.Metadata.(*fs.FileDiff); ok && diff != nil {
				b.WriteByte('\n')
				b.WriteString(renderFileDiff(diff))
			}
			t.blocks = append(t.blocks, b.String())
			return true
		}
	case event.KindError:
		if e.Error != nil {
			t.blocks = append(t.blocks, styles.ErrorBanner.Render(fmt.Sprintf("[error:%s] %v", e.Error.Stage, e.Error.Err)))
			return true
		}
	case event.KindRunCancelled:
		t.blocks = append(t.blocks, styles.DimText.Render("[cancelled]"))
		return true
	case event.KindIterLimit:
		if e.IterLimit != nil {
			t.blocks = append(t.blocks, styles.DimText.Render(fmt.Sprintf("[iter-limit] reached %d iterations — press Enter to continue", e.IterLimit.Reached)))
			return true
		}
	}
	return false
}

func compactInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	s := truncate(string(raw), 160)
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
