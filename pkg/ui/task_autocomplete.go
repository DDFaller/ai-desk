package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/falluba/ai-desk/pkg/deck"
)

// CompletionCycler manages the cycle state for autocompleted matches.
type CompletionCycler struct {
	matches []string
	index   int
}

func (c *CompletionCycler) IsActive() bool {
	return len(c.matches) > 0
}

func (c *CompletionCycler) Reset() {
	c.matches = nil
	c.index = -1
}

func (c *CompletionCycler) SetMatches(matches []string) {
	c.matches = matches
	c.index = -1
}

func (c *CompletionCycler) Next() string {
	if len(c.matches) == 0 {
		return ""
	}
	c.index = (c.index + 1) % len(c.matches)
	return c.matches[c.index]
}

func (c *CompletionCycler) Prev() string {
	if len(c.matches) == 0 {
		return ""
	}
	c.index--
	if c.index < 0 {
		c.index = len(c.matches) - 1
	}
	return c.matches[c.index]
}

func (c *CompletionCycler) Current() string {
	if c.index >= 0 && c.index < len(c.matches) {
		return c.matches[c.index]
	}
	return ""
}

func (c *CompletionCycler) Matches() []string {
	return c.matches
}

func (c *CompletionCycler) CurrentIndex() int {
	return c.index
}

// GetDirectoryCompletions scans local filesystem directories matching the given input prefix.
// Handles '~' home prefix expansion and conversion, relative and absolute paths.
func GetDirectoryCompletions(input string) ([]string, error) {
	if input == "" {
		home, _ := os.UserHomeDir()
		return []string{home, "."}, nil
	}

	originalInput := input
	expandedInput := input
	if strings.HasPrefix(input, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		expandedInput = filepath.Join(home, strings.TrimPrefix(input, "~"))
	}

	var dir, prefix string
	if strings.HasSuffix(originalInput, string(os.PathSeparator)) {
		dir = expandedInput
		prefix = ""
	} else if info, err := os.Stat(expandedInput); err == nil && info.IsDir() {
		// Exact directory match without trailing slash - list contents
		dir = expandedInput
		prefix = ""
	} else {
		dir = filepath.Dir(expandedInput)
		prefix = filepath.Base(expandedInput)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}

	var matches []string
	home, _ := os.UserHomeDir()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			// Skip hidden directories unless prefix explicitly starts with dot
			if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
				continue
			}

			fullPath := filepath.Join(dir, name)

			// Convert back to tilde prefix if requested originally
			if strings.HasPrefix(originalInput, "~") && home != "" {
				rel, err := filepath.Rel(home, fullPath)
				if err == nil && !strings.HasPrefix(rel, "..") {
					fullPath = "~" + string(os.PathSeparator) + rel
				}
			}
			matches = append(matches, fullPath)
		}
	}

	sort.Strings(matches)
	return matches, nil
}

// GetGroupCompletions returns existing group paths that match the prefix.
func GetGroupCompletions(input string, knownGroups []string) []string {
	cleanInput := strings.TrimSpace(strings.ToLower(input))
	var matches []string
	seen := make(map[string]bool)

	for _, g := range knownGroups {
		g = strings.Trim(g, "/")
		if g == "" || seen[g] {
			continue
		}
		if cleanInput == "" || strings.HasPrefix(strings.ToLower(g), cleanInput) || strings.Contains(strings.ToLower(g), cleanInput) {
			seen[g] = true
			matches = append(matches, g)
		}
	}
	sort.Strings(matches)
	return matches
}

// GetRecentProjectPaths returns distinct project paths from instances, ordered by recency.
func GetRecentProjectPaths(instances []deck.InstanceRecord) []string {
	var paths []string
	seen := make(map[string]bool)

	// Add current working directory first
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		paths = append(paths, cwd)
		seen[cwd] = true
	}

	// Sort instances by LastAccessed descending
	sorted := make([]deck.InstanceRecord, len(instances))
	copy(sorted, instances)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].LastAccessed > sorted[j].LastAccessed
	})

	for _, inst := range sorted {
		p := strings.TrimSpace(inst.ProjectPath)
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	return paths
}

// TextInputState provides cursor-aware text manipulation and editing.
type TextInputState struct {
	Value  string
	Cursor int
}

func (t *TextInputState) Set(val string) {
	t.Value = val
	t.Cursor = len([]rune(val))
}

func (t *TextInputState) Insert(r rune) {
	runes := []rune(t.Value)
	if t.Cursor < 0 {
		t.Cursor = 0
	}
	if t.Cursor > len(runes) {
		t.Cursor = len(runes)
	}
	runes = append(runes[:t.Cursor], append([]rune{r}, runes[t.Cursor:]...)...)
	t.Value = string(runes)
	t.Cursor++
}

func (t *TextInputState) InsertString(s string) {
	for _, r := range s {
		t.Insert(r)
	}
}

func (t *TextInputState) Backspace() {
	runes := []rune(t.Value)
	if t.Cursor > 0 && len(runes) > 0 {
		runes = append(runes[:t.Cursor-1], runes[t.Cursor:]...)
		t.Value = string(runes)
		t.Cursor--
	}
}

func (t *TextInputState) Delete() {
	runes := []rune(t.Value)
	if t.Cursor >= 0 && t.Cursor < len(runes) {
		runes = append(runes[:t.Cursor], runes[t.Cursor+1:]...)
		t.Value = string(runes)
	}
}

func (t *TextInputState) Left() {
	if t.Cursor > 0 {
		t.Cursor--
	}
}

func (t *TextInputState) Right() {
	runes := []rune(t.Value)
	if t.Cursor < len(runes) {
		t.Cursor++
	}
}

func (t *TextInputState) Home() {
	t.Cursor = 0
}

func (t *TextInputState) End() {
	t.Cursor = len([]rune(t.Value))
}

func (t *TextInputState) Clear() {
	t.Value = ""
	t.Cursor = 0
}

// DeleteWordBackwardPath deletes backward to the previous '/' or space (agent-deck #896).
func (t *TextInputState) DeleteWordBackwardPath() {
	runes := []rune(t.Value)
	if t.Cursor <= 0 || len(runes) == 0 {
		return
	}
	pos := t.Cursor - 1

	// Skip trailing slashes or spaces first
	for pos > 0 && (runes[pos] == '/' || runes[pos] == ' ' || runes[pos] == '\\') {
		pos--
	}
	// Scan back until next slash or space
	for pos > 0 && runes[pos] != '/' && runes[pos] != ' ' && runes[pos] != '\\' {
		pos--
	}
	if pos > 0 && (runes[pos] == '/' || runes[pos] == ' ' || runes[pos] == '\\') {
		pos++
	}

	newRunes := append(runes[:pos], runes[t.Cursor:]...)
	t.Value = string(newRunes)
	t.Cursor = pos
}
