// Package toolset bundles registered tool names by category.
//
// Importing this package transitively imports every tool family package, so
// each package's init() runs and registers its tools with the central
// internal/tools registry. By the time Active/Deferred/All return, every
// tool name they list resolves to a registered tools.Group.
//
// Why this package exists separately from internal/tools:
// internal/tools is a *leaf* — it defines the Tool interface, ToolName type,
// and Registry. It must NOT import the per-family packages, or we'd get
// `tools -> tools/fs -> tools`, which Go rejects as an import cycle. Letting
// the aggregator live one level up keeps the dependency edges pointing only
// downward toward leaves.
package toolset

import (
	"slices"

	"github.com/johnny1110/evva/internal/tools"
	"github.com/johnny1110/evva/internal/tools/cron"
	"github.com/johnny1110/evva/internal/tools/fs"
	"github.com/johnny1110/evva/internal/tools/meta"
	"github.com/johnny1110/evva/internal/tools/mode"
	"github.com/johnny1110/evva/internal/tools/monitor"
	"github.com/johnny1110/evva/internal/tools/notebook"
	"github.com/johnny1110/evva/internal/tools/shell"
	"github.com/johnny1110/evva/internal/tools/task"
	"github.com/johnny1110/evva/internal/tools/ux"
	"github.com/johnny1110/evva/internal/tools/web"
)

// Active lists every tool the model sees in every Complete call: fs, shell, meta.
func Active() []tools.ToolName {
	return slices.Concat(fs.Names(), shell.Names(), meta.Names())
}

// Deferred lists every tool whose schema is fetched on demand via TOOL_SEARCH.
func Deferred() []tools.ToolName {
	return slices.Concat(
		task.Names(),
		monitor.Names(),
		mode.Names(),
		notebook.Names(),
		ux.Names(),
		cron.Names(),
		web.Names(),
	)
}

// All returns every registered tool name (Active first, then Deferred).
func All() []tools.ToolName { return slices.Concat(Active(), Deferred()) }

// ReadOnly is the minimal tool set the Explore profile uses — no writes.
func ReadOnly() []tools.ToolName {
	return []tools.ToolName{tools.READ_FILE, tools.WEB_SEARCH, tools.GREP, tools.LS}
}
