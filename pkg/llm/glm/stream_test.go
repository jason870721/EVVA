package glm

import (
	"context"
	"strings"
	"testing"

	"github.com/johnny1110/evva/pkg/llm"
)

// TestConsumeStream feeds a canned SSE byte stream (Anthropic wire shape, which
// z.ai mirrors) through consumeStream and asserts chunk ordering, opaque
// signature accumulation, tool-input assembly, and usage merging.
func TestConsumeStream(t *testing.T) {
	body := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","model":"glm-5.2","usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":3}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"reflecting "}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"on this"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"abc"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"123"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hello "}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"there"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":1}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_1","name":"echo","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"msg\""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":":\"hi\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":2}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":42}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	var chunks []llm.Chunk
	sink := llm.ChunkFunc(func(c llm.Chunk) { chunks = append(chunks, c) })

	c := &Client{}
	resp, err := c.consumeStream(context.Background(), strings.NewReader(body), sink)
	if err != nil {
		t.Fatalf("consumeStream: %v", err)
	}

	wantChunks := []llm.Chunk{
		{Kind: llm.ChunkThinking, Delta: "reflecting "},
		{Kind: llm.ChunkThinking, Delta: "on this"},
		{Kind: llm.ChunkText, Delta: "Hello "},
		{Kind: llm.ChunkText, Delta: "there"},
	}
	if len(chunks) != len(wantChunks) {
		t.Fatalf("chunk count: got %d, want %d (%v)", len(chunks), len(wantChunks), chunks)
	}
	for i, w := range wantChunks {
		if chunks[i] != w {
			t.Errorf("chunk[%d]: got %+v, want %+v", i, chunks[i], w)
		}
	}

	if resp.Content != "Hello there" {
		t.Errorf("Content: got %q, want %q", resp.Content, "Hello there")
	}
	if resp.Thinking != "reflecting on this" {
		t.Errorf("Thinking: got %q, want %q", resp.Thinking, "reflecting on this")
	}
	if resp.ThinkingSignature != "abc123" {
		t.Errorf("ThinkingSignature: got %q, want abc123", resp.ThinkingSignature)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls: got %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "toolu_1" || tc.Name != "echo" || string(tc.Input) != `{"msg":"hi"}` {
		t.Errorf("tool call: id=%q name=%q args=%q", tc.ID, tc.Name, string(tc.Input))
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 42 || resp.Usage.CacheReadTokens != 3 {
		t.Errorf("Usage: in=%d out=%d cacheRead=%d, want 10/42/3",
			resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.CacheReadTokens)
	}
}

func TestConsumeStreamError(t *testing.T) {
	body := strings.Join([]string{
		`event: error`,
		`data: {"type":"error","error":{"type":"overloaded_error","message":"server busy"}}`,
		``,
	}, "\n")

	c := &Client{}
	_, err := c.consumeStream(context.Background(), strings.NewReader(body), llm.DiscardChunks)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "overloaded_error") || !strings.Contains(got, "server busy") {
		t.Errorf("err: got %q, want overloaded_error + server busy", got)
	}
}

func TestConsumeStreamCancel(t *testing.T) {
	body := strings.Join([]string{
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
	}, "\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &Client{}
	if _, err := c.consumeStream(ctx, strings.NewReader(body), llm.DiscardChunks); err != llm.ErrInterrupted {
		t.Fatalf("err: got %v, want llm.ErrInterrupted", err)
	}
}
