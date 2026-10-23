package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// InheritAgyPreferences fills missing preferences from an existing home without
// sharing credentials, skills, or writable settings files between sessions.
// Existing destination choices always win, including explicit false values.
func InheritAgyPreferences(sourceHome, destinationHome string) error {
	for _, relative := range []string{
		".gemini/antigravity-cli/settings.json",
		".gemini/antigravity-cli/cache/nux_state.json",
	} {
		source, err := os.ReadFile(filepath.Join(sourceHome, relative))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var defaults map[string]json.RawMessage
		if err := json.Unmarshal(source, &defaults); err != nil {
			return fmt.Errorf("read Antigravity preferences %s: %w", relative, err)
		}
		destination := filepath.Join(destinationHome, relative)
		current := map[string]json.RawMessage{}
		data, err := os.ReadFile(destination)
		if err == nil {
			if err := json.Unmarshal(data, &current); err != nil {
				return fmt.Errorf("read session preferences %s: %w", relative, err)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if current == nil {
			current = map[string]json.RawMessage{}
		}
		changed := false
		for key, value := range defaults {
			if _, exists := current[key]; !exists {
				current[key] = value
				changed = true
			}
		}
		if !changed {
			continue
		}
		data, err = json.MarshalIndent(current, "", "  ")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(destination, data, 0600); err != nil {
			return err
		}
	}
	return nil
}
