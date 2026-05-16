// Package meta hosts agent-meta tools: Agent (spawn sub-agent), ToolSearch
// (load deferred-tool schemas), Skill (invoke a user-installed skill), and
// ScheduleWakeup (self-pace /loop iterations).
//
// Stubs today. When implemented these will need agent-side hooks (sub-agent
// runner, schema registry, skill loader, scheduler) supplied via constructor
// injection from the toolset Builders.
package meta

import "github.com/johnny1110/evva/internal/tools"

// Names lists every tool name this package contributes.
func Names() []tools.ToolName {
	return []tools.ToolName{tools.AGENT, tools.TOOL_SEARCH, tools.SKILL, tools.SCHEDULE_WAKEUP}
}

// The real AGENT, TOOL_SEARCH, and SCHEDULE_WAKEUP tools live in their
// own files (agent.go / toolsearch.go / wakeup.go) — each needs an
// agent-layer hook (spawner, lookup, wakeup queue). Skill remains a
// stub singleton here.

var (
	Skill tools.Tool = tools.NewStub(
		tools.SKILL,
		"Execute a skill within the main conversation.\n\n"+
			"When users reference a \"slash command\" or \"/<something>\", they are referring to a skill. "+
			"Use this tool to invoke it.\n\n"+
			"- Set `skill` to the exact name from the available-skills list (no leading slash). "+
			"Plugin-namespaced skills use `plugin:skill`.\n"+
			"- Set `args` to pass optional arguments.\n"+
			"- Only invoke a skill that appears in the available-skills list or that the user explicitly typed as /<name>.\n"+
			"- Do not invoke a skill that is already running.\n"+
			"- Do not use this tool for built-in CLI commands like /help or /clear.",
		`{
			"type":"object",
			"additionalProperties":false,
			"required":["skill"],
			"properties":{
				"skill":{"type":"string","description":"The name of a skill from the available-skills list. Do not guess names."},
				"args":{"type":"string","description":"Optional arguments for the skill"}
			}
		}`,
	)
)
