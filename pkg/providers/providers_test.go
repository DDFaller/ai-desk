package providers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProviders_LoadAndTranslate(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "providers.yaml")

	sampleYAML := `
providers:
  ollama:
    type: openai-compatible
    base_url: http://localhost:11434/v1
    models: ["qwen2.5-coder:32b", "deepseek-r1:14b"]
  openrouter:
    type: openai-compatible
    base_url: https://openrouter.ai/api/v1
    api_keys: ["key-alpha", "key-beta"]
    models: ["anthropic/claude-3.5-sonnet"]
`
	if err := os.WriteFile(configPath, []byte(sampleYAML), 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}
	if len(reg.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(reg.Providers))
	}

	// Verify Ollama
	ollama, ok := reg.Get("ollama")
	if !ok {
		t.Fatal("ollama provider not found")
	}
	if ollama.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("unexpected base URL: %s", ollama.BaseURL)
	}

	// Verify OpenRouter multi-key rotation
	openrouter, ok := reg.Get("openrouter")
	if !ok {
		t.Fatal("openrouter provider not found")
	}
	if openrouter.ActiveKey(0) != "key-alpha" {
		t.Errorf("expected key-alpha, got %s", openrouter.ActiveKey(0))
	}
	if openrouter.ActiveKey(1) != "key-beta" {
		t.Errorf("expected key-beta, got %s", openrouter.ActiveKey(1))
	}

	// Test translator
	trans := NewTranslator()
	claudeEnv := trans.EnvOverrides("claude", openrouter, 0)
	if claudeEnv["ANTHROPIC_BASE_URL"] != "https://openrouter.ai/api/v1" {
		t.Errorf("unexpected ANTHROPIC_BASE_URL: %s", claudeEnv["ANTHROPIC_BASE_URL"])
	}
	if claudeEnv["ANTHROPIC_API_KEY"] != "key-alpha" {
		t.Errorf("unexpected ANTHROPIC_API_KEY: %s", claudeEnv["ANTHROPIC_API_KEY"])
	}

	// Test OpenCode JSON injection
	opencodeSandbox := filepath.Join(tempDir, "opencode_sandbox")
	_ = os.MkdirAll(opencodeSandbox, 0755)
	if err := trans.InjectOpenCodeConfig(opencodeSandbox, ollama, 0); err != nil {
		t.Fatalf("inject opencode config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(opencodeSandbox, "providers.json")); err != nil {
		t.Fatalf("expected providers.json in opencode sandbox")
	}
}
