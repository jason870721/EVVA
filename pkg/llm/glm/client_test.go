package glm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/johnny1110/evva/pkg/llm"
	"github.com/johnny1110/evva/pkg/tools"
)

// TestComplete_BearerAuth is GLM's key divergence from the copied Anthropic
// engine: z.ai authenticates with "Authorization: Bearer <key>", NOT the
// x-api-key header native Anthropic uses. It also confirms the request hits
// <apiURL>/v1/messages and that Name() reports "glm".
func TestComplete_BearerAuth(t *testing.T) {
	var gotAuth, gotXAPIKey, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotXAPIKey = r.Header.Get("X-Api-Key")
		gotPath = r.URL.Path
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2}}`))
	}))
	defer srv.Close()

	c := New(llm.APIConfig{ApiURL: srv.URL, ApiSecret: "z-key-123"}, "")
	if c.Name() != "glm" {
		t.Errorf("Name(): got %q, want glm", c.Name())
	}
	if c.Model() != DefaultModel {
		t.Errorf("Model(): got %q, want %q (default)", c.Model(), DefaultModel)
	}

	resp, err := c.Complete(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "ping"}}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotAuth != "Bearer z-key-123" {
		t.Errorf("Authorization header: got %q, want %q", gotAuth, "Bearer z-key-123")
	}
	if gotXAPIKey != "" {
		t.Errorf("x-api-key must NOT be set for z.ai; got %q", gotXAPIKey)
	}
	if gotPath != messagesPath {
		t.Errorf("request path: got %q, want %q", gotPath, messagesPath)
	}
	if resp.Content != "hi" {
		t.Errorf("Content: got %q, want hi", resp.Content)
	}
	if resp.Usage.InputTokens != 5 || resp.Usage.OutputTokens != 2 {
		t.Errorf("Usage: got in=%d out=%d, want 5/2", resp.Usage.InputTokens, resp.Usage.OutputTokens)
	}
}

// TestComplete_EffortOutputConfig verifies evva effort is forwarded as
// output_config.effort (which z.ai buckets into GLM's High/Max tiers).
func TestComplete_EffortOutputConfig(t *testing.T) {
	var body apiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"content":[],"stop_reason":"end_turn"}`))
	}))
	defer srv.Close()

	c := New(llm.APIConfig{ApiURL: srv.URL, ApiSecret: "k"}, "glm-5.2", llm.WithEffort(3))
	if _, err := c.Complete(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "x"}}, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if body.OutputConfig == nil {
		t.Fatal("output_config not sent")
	}
	if body.OutputConfig.Effort != "xhigh" {
		t.Errorf("effort: got %q, want xhigh (evva high)", body.OutputConfig.Effort)
	}
	if body.Model != "glm-5.2" {
		t.Errorf("model: got %q, want glm-5.2", body.Model)
	}
}

func TestGLMEffort(t *testing.T) {
	// evva effort → output_config.effort. z.ai collapses these onto GLM's two
	// tiers (low/medium → High, high/ultra → Max); the wire value is identical
	// to native Anthropic.
	tests := []struct {
		level int
		want  string
	}{
		{0, ""},
		{1, "medium"},
		{2, "high"},
		{3, "xhigh"},
		{4, "max"},
		{5, ""},
	}
	for _, tt := range tests {
		if got := glmEffort(tt.level); got != tt.want {
			t.Errorf("glmEffort(%d) = %q, want %q", tt.level, got, tt.want)
		}
	}
}

// TestToAPIMessages_ImageContentBlocks locks the copied image path: a read-file
// image block is forwarded as an Anthropic base64 image source. This is what
// wires the read tool to GLM's image processing with no extra code.
func TestToAPIMessages_ImageContentBlocks(t *testing.T) {
	msgs := []llm.Message{{
		Role: llm.RoleTool,
		ToolResults: []*llm.ToolResult{{
			ID: "toolu_001",
			ContentBlocks: []tools.ContentBlock{
				{Type: tools.ContentBlockText, Text: "Image analysis result:"},
				{Type: tools.ContentBlockImage, Image: &tools.ImageBlock{
					MIMEType:     "image/png",
					Base64Data:   "iVBORw0KGgo=",
					OriginalSize: 1234,
				}},
			},
		}},
	}}

	out := toAPIMessages(msgs)
	if len(out) != 1 || out[0].Role != "user" {
		t.Fatalf("expected 1 user apiMessage; got %d role=%q", len(out), out[0].Role)
	}
	tr := out[0].Content[0]
	if tr.Type != "tool_result" || tr.ToolUseID != "toolu_001" {
		t.Errorf("tool_result meta: type=%s id=%s", tr.Type, tr.ToolUseID)
	}
	items, ok := tr.Content.([]blockContentItem)
	if !ok || len(items) != 2 {
		t.Fatalf("expected []blockContentItem len 2; got %T len=%d", tr.Content, len(items))
	}
	if items[0].Type != "text" || items[0].Text != "Image analysis result:" {
		t.Errorf("text item: type=%s text=%q", items[0].Type, items[0].Text)
	}
	if items[1].Type != "image" || items[1].Source == nil {
		t.Fatalf("image item: type=%s source=%v", items[1].Type, items[1].Source)
	}
	if items[1].Source.Type != "base64" || items[1].Source.MediaType != "image/png" || items[1].Source.Data != "iVBORw0KGgo=" {
		t.Errorf("image source: %+v", items[1].Source)
	}
}

func TestToAPIMessages_ErrorStaysTextOnly(t *testing.T) {
	// is_error tool_results must carry only text — image blocks are dropped.
	msgs := []llm.Message{{
		Role: llm.RoleTool,
		ToolResults: []*llm.ToolResult{{
			ID:      "toolu_003",
			Content: "read failed",
			IsError: true,
			ContentBlocks: []tools.ContentBlock{
				{Type: tools.ContentBlockImage, Image: &tools.ImageBlock{MIMEType: "image/png", Base64Data: "AAAA"}},
			},
		}},
	}}

	tr := toAPIMessages(msgs)[0].Content[0]
	content, ok := tr.Content.(string)
	if !ok || content != "read failed" {
		t.Errorf("error tool_result: got %T %v, want string 'read failed'", tr.Content, tr.Content)
	}
}
