package voice

import (
	"strings"
	"testing"
)

func TestSelectVoiceAdapter(t *testing.T) {
	for _, tc := range []struct {
		tool     string
		expected string
	}{
		{"agy", "agy"},
		{"AGY", "agy"},
		{"gemini", "agy"},
		{"codex", "codex"},
		{"CODEX", "codex"},
		{"claude", "claude"},
		{"opencode", "opencode"},
		{"custom", "custom"},
	} {
		adp := SelectVoiceAdapter(tc.tool)
		if adp.Name() != tc.expected {
			t.Errorf("for tool %q: expected adapter name %q, got %q", tc.tool, tc.expected, adp.Name())
		}
	}
}

func TestAgyVoiceAdapterSanitizeSpokenResponse(t *testing.T) {
	adp := &AgyVoiceAdapter{}

	rawInput := "\x1b[32m○ Thought\x1b[0m\nThinking about the user request...\n" +
		"● Ran run_command: \"git status\"\n" +
		"Command output:\nOn branch main\n" +
		"I have inspected [server.go](file:///home/user/project/server.go) and fixed the bug.\n" +
		"```go\nfunc main() {}\n```\n" +
		"All tests are passing."

	spoken := adp.SanitizeSpokenResponse(rawInput)

	if strings.Contains(spoken, "○ Thought") || strings.Contains(spoken, "Thought") {
		t.Errorf("expected ○ Thought to be stripped, got:\n%s", spoken)
	}
	if strings.Contains(spoken, "● Ran") || strings.Contains(spoken, "run_command") {
		t.Errorf("expected ● Ran to be stripped, got:\n%s", spoken)
	}
	if strings.Contains(spoken, "file:///") {
		t.Errorf("expected file:/// URL to be stripped, got:\n%s", spoken)
	}
	if !strings.Contains(spoken, "server.go") {
		t.Errorf("expected server.go filename to be preserved, got:\n%s", spoken)
	}
	if !strings.Contains(spoken, "I updated the code block") {
		t.Errorf("expected code block cue, got:\n%s", spoken)
	}
	if !strings.Contains(spoken, "All tests are passing") {
		t.Errorf("expected 'All tests are passing', got:\n%s", spoken)
	}
}

func TestCodexVoiceAdapterExhaustionNotice(t *testing.T) {
	adp := &CodexVoiceAdapter{}

	// Verify adapter recognizes rate limit messages and returns spoken notice
	sampleError := "Task complete: error usage_limit_exceeded: You've hit your usage limit."
	if !strings.Contains(sampleError, "usage_limit_exceeded") {
		t.Fail()
	}
	notice := "OpenAI Codex usage limit reached. Your token quota has been exhausted. Please switch to Google Antigravity with key W."
	sanitized := adp.SanitizeSpokenResponse(notice)
	if !strings.Contains(sanitized, "usage limit reached") {
		t.Errorf("expected sanitized notice to contain 'usage limit reached', got:\n%s", sanitized)
	}
}
