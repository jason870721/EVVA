// Package llmfactory wires LLM provider constants to concrete provider clients.
//
// Why this lives outside internal/llm:
// internal/llm is a *leaf* — it defines the Client interface and shared types
// (Message, Response, Option). Each provider package imports internal/llm to
// implement that interface. If the factory also lived in internal/llm, llm
// would have to import its own provider subpackages, creating an import
// cycle that Go rejects at compile time. Putting the factory one level up
// keeps every edge pointing downward toward the leaf.
//
// Adding a new provider:
//  1. Create internal/llm/<name>/ implementing llm.Client.
//  2. Add a case to Of below.
//  3. Register the provider constant in internal/constant.
package llmfactory

import (
	"fmt"

	config "github.com/johnny1110/evva/configs"
	"github.com/johnny1110/evva/internal/constant"
	"github.com/johnny1110/evva/internal/llm"
	"github.com/johnny1110/evva/internal/llm/claude"
	"github.com/johnny1110/evva/internal/llm/deepseek"
	"github.com/johnny1110/evva/internal/llm/ollama"
)

// Of constructs the concrete llm.Client for the requested provider, looking
// up its API config (URL + secret) from the loaded application config.
//
// Returns an error when the provider has no API key configured or when the
// provider value is unknown. constant.LLMProvider is keyed by Name so the
// switch is over the string (the struct contains a slice and is not directly
// comparable).
func Of(provider constant.LLMProvider, model constant.Model, opts []llm.Option) (llm.Client, error) {
	cfg := config.Get()
	api, ok := cfg.LLMProviderConfig[provider.Name]
	if !ok {
		return nil, fmt.Errorf("provider: [%s] API_KEY not set", provider.Name)
	}

	switch provider.Name {
	case constant.ANTHROPIC.Name:
		return claude.New(api, string(model), opts...), nil
	case constant.DEEPSEEK.Name:
		return deepseek.New(api, string(model), opts...), nil
	case constant.OLLAMA.Name:
		return ollama.New(api, string(model), opts...), nil
	default:
		return nil, fmt.Errorf("unknown provider %q (want anthropic | deepseek | ollama)", provider.Name)
	}
}
