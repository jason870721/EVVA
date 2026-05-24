package agent

import (
	"fmt"

	agent_impl "github.com/johnny1110/evva/internal/agent"
)

// NewWithProfile is the flexible public constructor a downstream app uses to
// build an agent against its own profile and option set. Unlike New (which
// loads the bundled "evva" persona, memdir, and skill catalog) this
// constructor wires only what the caller supplies — no skill catalog, no
// memory snapshot, no agent registry by default.
//
// Approval / question handling follows the agent's defaults. Pass
// agent.WithSink to surface approval + question events to an interactive UI
// (resolve them via Agent.RespondPermission / RespondQuestion), or
// agent.WithPermissionBroker to plug in a custom allow/deny policy. With
// neither, the agent auto-denies so an async request never parks the caller
// forever.
//
// Example:
//
//	prof, _ := agent.NewProfile("custom", "you are helpful",
//	    []tools.ToolName{tools.READ_FILE, tools.BASH},
//	    "anthropic", constant.CLAUDE_SONNET_4_6,
//	    agent.ProfileOptions{})
//
//	ag, _ := agent.NewWithProfile(prof,
//	    agent.WithConfig(cfg),
//	    agent.WithSink(mySink),
//	    agent.WithMaxIterations(20),
//	)
//	resp, _ := ag.Run(ctx, "...")
func NewWithProfile(profile Profile, opts ...Option) (Agent, error) {
	inner, err := agent_impl.New(nil, profile, opts...)
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	return &agentAdapter{inner: inner}, nil
}
