package glm

import "github.com/johnny1110/evva/pkg/llm"

// ProviderName is the registry key under which this client registers. It
// matches the Name field of constant.GLM. Registered into
// pkg/llm.DefaultRegistry() by pkg/llm/builtins.
const ProviderName = "glm"

// Factory adapts New into a llm.ClientFactory. Downstream apps that want to
// register GLM on a non-default registry can call this directly.
func Factory(cfg llm.APIConfig, model string, opts ...llm.Option) (llm.Client, error) {
	return New(cfg, model, opts...), nil
}
