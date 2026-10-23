package providers

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ProviderConfig represents one configured model provider.
type ProviderConfig struct {
	Name      string            `yaml:"-"`
	Type      string            `yaml:"type"` // e.g. openai-compatible, ollama, custom
	BaseURL   string            `yaml:"base_url"`
	APIKey    string            `yaml:"api_key,omitempty"`
	APIKeys   []string          `yaml:"api_keys,omitempty"` // multi-key pool for rotation!
	APIKeyRef string            `yaml:"api_key_ref,omitempty"`
	Models    []string          `yaml:"models,omitempty"`
	Headers   map[string]string `yaml:"headers,omitempty"`
}

// Registry stores all configured custom providers.
type Registry struct {
	Providers map[string]ProviderConfig `yaml:"providers"`
	path      string
}

// DefaultConfigPath returns the canonical path ~/.ai-desk/providers.yaml.
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ai-desk", "providers.yaml")
}

// Load loads providers from the specified YAML file path.
func Load(path string) (*Registry, error) {
	if path == "" {
		path = DefaultConfigPath()
	}
	r := &Registry{
		Providers: make(map[string]ProviderConfig),
		path:      path,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, fmt.Errorf("read providers file: %w", err)
	}

	if err := yaml.Unmarshal(data, r); err != nil {
		return nil, fmt.Errorf("parse providers YAML: %w", err)
	}
	for name, p := range r.Providers {
		p.Name = name
		r.Providers[name] = p
	}
	return r, nil
}

// Save persists the registry back to its YAML file.
func (r *Registry) Save() error {
	if r.path == "" {
		r.path = DefaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal providers YAML: %w", err)
	}
	return os.WriteFile(r.path, data, 0600)
}

// Get returns the provider by name, if found.
func (r *Registry) Get(name string) (ProviderConfig, bool) {
	p, ok := r.Providers[name]
	return p, ok
}

// ListNames returns sorted names of all configured providers.
func (r *Registry) ListNames() []string {
	names := make([]string, 0, len(r.Providers))
	for name := range r.Providers {
		names = append(names, name)
	}
	return names
}

// ActiveKey returns the first available API key, or the single APIKey.
func (p ProviderConfig) ActiveKey(keyIndex int) string {
	if len(p.APIKeys) > 0 {
		return p.APIKeys[keyIndex%len(p.APIKeys)]
	}
	return p.APIKey
}
