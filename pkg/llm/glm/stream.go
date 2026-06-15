package glm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/johnny1110/evva/pkg/llm"
	"github.com/johnny1110/evva/pkg/tools"
)

// Stream is the chunked variant of Complete. It opens an SSE connection to
// z.ai's Anthropic-compatible /v1/messages endpoint with "stream": true, parses
// each event, emits text/thinking deltas through sink as they arrive, and
// returns the fully assembled Response when the server emits message_stop.
//
// The stream protocol is the Anthropic per-content-block shape:
//   - message_start carries the request id, model, and initial usage stats.
//   - content_block_start opens a block of type text | thinking | tool_use.
//   - content_block_delta carries incremental data (text/thinking/signature/
//     input_json deltas).
//   - content_block_stop closes the block.
//   - message_delta updates the final usage.output_tokens.
//   - message_stop is the terminator; ping events are keepalives we ignore;
//     error events abort with the server's reason.
//
// The thinking signature is opaque and never streamed to the chunk sink — it
// ships back verbatim on the next assistant turn (llm.Message.ThinkingSignature).
func (c *Client) Stream(ctx context.Context, messages []llm.Message, toolSet []tools.Tool, sink llm.ChunkSink) (llm.Response, error) {
	if c.apiKey == "" {
		return llm.Response{}, fmt.Errorf("glm: missing API key (type in /config to setup)")
	}
	if sink == nil {
		sink = llm.DiscardChunks
	}

	body := c.buildRequestBody(messages, toolSet)
	body.Stream = true

	payload, err := json.Marshal(body)
	if err != nil {
		return llm.Response{}, fmt.Errorf("glm: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+messagesPath, bytes.NewReader(payload))
	if err != nil {
		return llm.Response{}, fmt.Errorf("glm: build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "text/event-stream")
	c.setAuth(req)

	resp, err := c.params.HTTP().Do(req)
	if err != nil {
		return llm.Response{}, fmt.Errorf("glm: http: %w", llm.NormalizeErr(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(resp.Body)
		return llm.Response{}, fmt.Errorf("glm: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	return c.consumeStream(ctx, resp.Body, sink)
}

// --- SSE wire types -------------------------------------------------------

// sseEnvelope is the discriminated-union shape emitted in each data: line. The
// "type" field selects which sub-field carries meaningful data.
type sseEnvelope struct {
	Type         string           `json:"type"`
	Index        int              `json:"index,omitempty"`
	Message      *sseMessage      `json:"message,omitempty"`
	ContentBlock *sseContentBlock `json:"content_block,omitempty"`
	Delta        *sseDelta        `json:"delta,omitempty"`
	Usage        *sseUsage        `json:"usage,omitempty"`
	Error        *sseError        `json:"error,omitempty"`
}

type sseMessage struct {
	ID    string    `json:"id"`
	Model string    `json:"model"`
	Usage *sseUsage `json:"usage,omitempty"`
}

type sseContentBlock struct {
	Type      string          `json:"type"` // "text" | "thinking" | "tool_use"
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
}

type sseDelta struct {
	// Inner delta type:
	//   "text_delta", "thinking_delta", "signature_delta", "input_json_delta"
	Type        string `json:"type,omitempty"`
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	// Top-level message_delta also reuses this struct for stop info.
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
}

type sseUsage struct {
	InputTokens              int `json:"input_tokens,omitempty"`
	OutputTokens             int `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

type sseError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// streamingBlock accumulates state for one content block across its deltas.
// At content_block_stop time we promote its contents into the assembled
// Response. We track per-index since the index is stable for a block's life.
type streamingBlock struct {
	kind      string // "text" | "thinking" | "tool_use"
	text      strings.Builder
	thinking  strings.Builder
	signature strings.Builder
	toolID    string
	toolName  string
	toolInput strings.Builder
}

// consumeStream is the SSE parsing loop, factored out for testability with a
// synthetic io.Reader.
func (c *Client) consumeStream(ctx context.Context, body io.Reader, sink llm.ChunkSink) (llm.Response, error) {
	scanner := bufio.NewScanner(body)
	// A frame can carry a full thinking block in a single line; 1 MB headroom
	// handles long messages comfortably.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var (
		out       llm.Response
		blocks    = map[int]*streamingBlock{}
		blockKeys []int // insertion order so tool calls stay stable
	)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				return llm.Response{}, llm.ErrInterrupted
			}
			return llm.Response{}, err
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			// "event: <type>" lines are redundant (the JSON has its own "type"
			// tag), and blank lines separate frames — ignore both.
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}

		var env sseEnvelope
		if err := json.Unmarshal([]byte(payload), &env); err != nil {
			return llm.Response{}, fmt.Errorf("glm: decode stream chunk: %w", err)
		}
		if env.Error != nil {
			return llm.Response{}, fmt.Errorf("glm: %s: %s", env.Error.Type, env.Error.Message)
		}

		switch env.Type {
		case "ping":
			// keepalive — no payload, no action.
			continue
		case "message_start":
			if env.Message != nil && env.Message.Usage != nil {
				out.Usage.InputTokens = env.Message.Usage.InputTokens
				out.Usage.OutputTokens = env.Message.Usage.OutputTokens
				out.Usage.CacheReadTokens = env.Message.Usage.CacheReadInputTokens
				out.Usage.CacheCreationTokens = env.Message.Usage.CacheCreationInputTokens
			}
		case "content_block_start":
			if env.ContentBlock == nil {
				continue
			}
			b := &streamingBlock{kind: env.ContentBlock.Type}
			switch env.ContentBlock.Type {
			case "tool_use":
				b.toolID = env.ContentBlock.ID
				b.toolName = env.ContentBlock.Name
			case "text":
				if env.ContentBlock.Text != "" {
					// Some servers seed the initial text in content_block_start;
					// emit it as a chunk too so the UI doesn't miss the head.
					b.text.WriteString(env.ContentBlock.Text)
					sink.OnChunk(llm.Chunk{Kind: llm.ChunkText, Delta: env.ContentBlock.Text})
				}
			case "thinking":
				if env.ContentBlock.Thinking != "" {
					b.thinking.WriteString(env.ContentBlock.Thinking)
					sink.OnChunk(llm.Chunk{Kind: llm.ChunkThinking, Delta: env.ContentBlock.Thinking})
				}
				if env.ContentBlock.Signature != "" {
					b.signature.WriteString(env.ContentBlock.Signature)
				}
			}
			if _, exists := blocks[env.Index]; !exists {
				blockKeys = append(blockKeys, env.Index)
			}
			blocks[env.Index] = b
		case "content_block_delta":
			b, ok := blocks[env.Index]
			if !ok || env.Delta == nil {
				continue
			}
			switch env.Delta.Type {
			case "text_delta":
				if env.Delta.Text != "" {
					b.text.WriteString(env.Delta.Text)
					sink.OnChunk(llm.Chunk{Kind: llm.ChunkText, Delta: env.Delta.Text})
				}
			case "thinking_delta":
				if env.Delta.Thinking != "" {
					b.thinking.WriteString(env.Delta.Thinking)
					sink.OnChunk(llm.Chunk{Kind: llm.ChunkThinking, Delta: env.Delta.Thinking})
				}
			case "signature_delta":
				if env.Delta.Signature != "" {
					b.signature.WriteString(env.Delta.Signature)
				}
			case "input_json_delta":
				if env.Delta.PartialJSON != "" {
					b.toolInput.WriteString(env.Delta.PartialJSON)
				}
			}
		case "content_block_stop":
			// Block already accumulated; final assembly happens after the loop.
		case "message_delta":
			// Usually carries final output_tokens; sometimes other usage fields
			// are revised here too.
			if env.Usage != nil {
				if env.Usage.OutputTokens > 0 {
					out.Usage.OutputTokens = env.Usage.OutputTokens
				}
				if env.Usage.InputTokens > 0 {
					out.Usage.InputTokens = env.Usage.InputTokens
				}
				if env.Usage.CacheReadInputTokens > 0 {
					out.Usage.CacheReadTokens = env.Usage.CacheReadInputTokens
				}
				if env.Usage.CacheCreationInputTokens > 0 {
					out.Usage.CacheCreationTokens = env.Usage.CacheCreationInputTokens
				}
			}
		case "message_stop":
			// terminator — assemble and return below.
		default:
			// Unknown event type — silently ignore for forward compat.
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return llm.Response{}, llm.ErrInterrupted
		}
		return llm.Response{}, fmt.Errorf("glm: stream: %w", llm.NormalizeErr(err))
	}

	// Assemble the final Response from per-index block state. Text from every
	// text block concatenates; thinking concatenates with the last signature
	// winning (matches buffered Complete); tool calls preserve arrival order.
	var (
		text     strings.Builder
		thinking strings.Builder
	)
	for _, idx := range blockKeys {
		b := blocks[idx]
		switch b.kind {
		case "text":
			text.WriteString(b.text.String())
		case "thinking":
			thinking.WriteString(b.thinking.String())
			if sig := b.signature.String(); sig != "" {
				out.ThinkingSignature = sig
			}
		case "tool_use":
			args := b.toolInput.String()
			if args == "" {
				args = "{}"
			}
			out.ToolCalls = append(out.ToolCalls, &tools.Call{
				ID:    b.toolID,
				Name:  b.toolName,
				Input: json.RawMessage(args),
			})
		}
	}
	out.Content = text.String()
	out.Thinking = thinking.String()
	return out, nil
}
