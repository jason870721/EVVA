// Package web hosts web tools: web_search (Tavily-backed) and web_fetch
// (HTTP GET + readable-text extraction).
//
// Both stateless — package-level singletons. They read configuration
// (TAVILY_API_KEY, FETCH_MAX_BYTES) lazily from configs.Get() inside
// Execute, so a host that rotates env mid-session picks it up.
package web

import "github.com/johnny1110/evva/internal/tools"

// Names lists every tool name this package contributes.
func Names() []tools.ToolName {
	return []tools.ToolName{tools.WEB_FETCH, tools.WEB_SEARCH}
}

var (
	Fetch  tools.Tool = &FetchTool{}
	Search tools.Tool = &SearchTool{}
)
