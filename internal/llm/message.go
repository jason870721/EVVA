package llm

import "github.com/johnny1110/evva/internal/tools"

// Role labels who emitted a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one turn of the conversation passed to and from the LLM.
// ToolCall is set when an assistant message represents a tool invocation request;
// ToolID pairs the assistant's request with the subsequent tool reply so providers
// that demand explicit pairing (Anthropic, OpenAI-style) can reconstruct it.
//
// Thinking is provider-specific reasoning text (currently only DeepSeek's
// reasoning_content). It is display-only — the TUI may render it, but clients
// MUST NOT echo it back in subsequent requests, since DeepSeek rejects that.
type Message struct {
	Role     Role
	Content  string
	Thinking string
	ToolCall *tools.Call
	ToolID   string
}

// Response is what the LLM returns on each completion turn.
// ToolCall is non-nil when the model wants to invoke a tool instead of replying;
// ToolID is the provider-issued identifier the agent must echo back with the result.
// Thinking carries any provider-specific reasoning trace; empty for providers
// that don't expose one. See Message.Thinking for the round-trip caveat.
type Response struct {
	Content  string
	Thinking string
	ToolCall *tools.Call
	ToolID   string
}
