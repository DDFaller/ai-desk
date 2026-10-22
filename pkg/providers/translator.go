package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Translator projects provider configurations into harness-specific configuration schemas.
type Translator struct{}

func NewTranslator() *Translator {
	return &Translator{}
}

// InjectOpenCodeConfig writes a providers.json into OpenCode's virtualized sandbox directory.
func (t *Translator) InjectOpenCodeConfig(sandboxDir string, p ProviderConfig, keyIndex int) error {
	apiKey := p.ActiveKey(keyIndex)
	cfg := map[string]any{
		"providers": map[string]any{
			p.Name: map[string]any{
				"type":    p.Type,
				"baseURL": p.BaseURL,
				"apiKey":  apiKey,
				"models":  p.Models,
			},
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal opencode config: %w", err)
	}
	return os.WriteFile(filepath.Join(sandboxDir, "providers.json"), data, 0600)
}

// EnvOverrides returns environment variables tailored to the harness for a given provider.
func (t *Translator) EnvOverrides(tool string, p ProviderConfig, keyIndex int) map[string]string {
	env := make(map[string]string)
	apiKey := p.ActiveKey(keyIndex)

	switch tool {
	case "claude":
		if p.BaseURL != "" {
			env["ANTHROPIC_BASE_URL"] = p.BaseURL
		}
		if apiKey != "" {
			env["ANTHROPIC_API_KEY"] = apiKey
		}
	case "codex":
		if p.BaseURL != "" {
			env["OPENAI_BASE_URL"] = p.BaseURL
		}
		if apiKey != "" {
			env["OPENAI_API_KEY"] = apiKey
		}
	case "agy", "gemini":
		if p.BaseURL != "" {
			env["GEMINI_BASE_URL"] = p.BaseURL
			env["AI_GATEWAY_URL"] = p.BaseURL
		}
		if apiKey != "" {
			env["GEMINI_API_KEY"] = apiKey
		}
	}
	return env
}
