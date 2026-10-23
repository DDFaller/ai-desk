package voice

import (
	"strings"
	"testing"
	"time"

	"github.com/falluba/ai-desk/pkg/deck"
)

func TestVoice_SanitizeForSpeech(t *testing.T) {
	// Test 1: ANSI escape stripping
	rawANSI := "\x1b[32m✓ Created routine\x1b[0m \x1b[1m\"Daily Review\"\x1b[0m in \x1b[36mstate.db\x1b[0m"
	cleaned := SanitizeForSpeech(rawANSI)
	if strings.Contains(cleaned, "\x1b") {
		t.Errorf("expected ANSI escapes stripped, got: %q", cleaned)
	}
	if !strings.Contains(cleaned, "Created routine \"Daily Review\" in state.db") {
		t.Errorf("expected clean message, got: %q", cleaned)
	}

	// Test 2: Markdown code fence compression
	rawMarkdown := "I've implemented the auth check in task.go.\n```go\nfunc CheckAuth() bool {\n    return true\n}\n```\nAll 5 unit tests are passing."
	cleanedMd := SanitizeForSpeech(rawMarkdown)
	if strings.Contains(cleanedMd, "func CheckAuth") {
		t.Errorf("expected code block to be condensed, got: %q", cleanedMd)
	}
	if !strings.Contains(cleanedMd, "I have updated the code block") {
		t.Errorf("expected speech cue 'I have updated the code block', got: %q", cleanedMd)
	}
	if !strings.Contains(cleanedMd, "All 5 unit tests are passing") {
		t.Errorf("expected closing sentence preserved, got: %q", cleanedMd)
	}

	// Test 3: Inline backticks stripped
	rawBackticks := "Updated `pkg/deck/task.go` and `pkg/voice/bridge.go`."
	cleanedBt := SanitizeForSpeech(rawBackticks)
	if strings.Contains(cleanedBt, "`") {
		t.Errorf("expected backticks stripped, got: %q", cleanedBt)
	}
	if !strings.Contains(cleanedBt, "Updated pkg/deck/task.go and pkg/voice/bridge.go") {
		t.Errorf("expected backtick contents preserved, got: %q", cleanedBt)
	}

	// Test 4: Spinners and prompt headers stripped
	rawSpinners := "⠋ Thinking...\n> Running git status\n│ On branch main\nEverything is up to date."
	cleanedSpin := SanitizeForSpeech(rawSpinners)
	if strings.Contains(cleanedSpin, "⠋") || strings.Contains(cleanedSpin, "│") {
		t.Errorf("expected spinners and borders stripped, got: %q", cleanedSpin)
	}
	if !strings.Contains(cleanedSpin, "Everything is up to date") {
		t.Errorf("expected natural sentence preserved, got: %q", cleanedSpin)
	}
}

func TestVoice_VoiceAgentBridgeLifecycle(t *testing.T) {
	inst := &deck.InstanceRecord{
		ID:          "inst-voice-test",
		Title:       "Payment Service",
		Tool:        "claude",
		TmuxSession: "agentdeck_payment_test",
	}

	bridge, err := NewVoiceAgentBridge(nil, inst)
	if err != nil {
		t.Fatalf("unexpected error creating voice bridge: %v", err)
	}

	if bridge.Target().ID != "inst-voice-test" {
		t.Errorf("expected target ID 'inst-voice-test', got %s", bridge.Target().ID)
	}
	if bridge.Target().Title != "Payment Service" {
		t.Errorf("expected target Title 'Payment Service', got %s", bridge.Target().Title)
	}

	// Test turn history
	turn := VoiceTurn{
		Speaker:    "user",
		Text:       "Check API health",
		SpokenText: "Check API health",
		Timestamp:  time.Now(),
		LatencyMS:  110,
	}
	bridge.history = append(bridge.history, turn)

	hist := bridge.History()
	if len(hist) != 1 {
		t.Fatalf("expected 1 turn in history, got %d", len(hist))
	}
	if hist[0].Speaker != "user" || hist[0].Text != "Check API health" {
		t.Errorf("unexpected turn data: %+v", hist[0])
	}
}
