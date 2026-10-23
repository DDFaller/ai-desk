package voice

import (
	"context"
	"crypto/md5"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/falluba/ai-desk/pkg/deck"
)

// VoiceTurn represents a single conversational turn between the user and an agent.
type VoiceTurn struct {
	Speaker    string    `json:"speaker"` // "user" or "agent"
	Text       string    `json:"text"`
	SpokenText string    `json:"spoken_text,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	LatencyMS  int       `json:"latency_ms"`
}

// VoiceAgentBridge coordinates the bidirectional voice conversation with a live agent task.
type VoiceAgentBridge struct {
	sdb       *deck.StateDB
	target    *deck.InstanceRecord
	adapter   HarnessVoiceAdapter
	history   []VoiceTurn
	voiceName string
	active    bool
	mu        sync.Mutex
}

// NewVoiceAgentBridge initializes a voice bridge attached to a target agent instance.
func NewVoiceAgentBridge(sdb *deck.StateDB, target *deck.InstanceRecord) (*VoiceAgentBridge, error) {
	if target == nil {
		return nil, fmt.Errorf("target agent instance cannot be nil")
	}

	adapter := SelectVoiceAdapter(target.Tool)

	return &VoiceAgentBridge{
		sdb:       sdb,
		target:    target,
		adapter:   adapter,
		history:   make([]VoiceTurn, 0),
		voiceName: "alba",
		active:    true,
	}, nil
}

// Target returns the paired agent instance record.
func (b *VoiceAgentBridge) Target() *deck.InstanceRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.target
}

// History returns conversational turns recorded during the session.
func (b *VoiceAgentBridge) History() []VoiceTurn {
	b.mu.Lock()
	defer b.mu.Unlock()
	copied := make([]VoiceTurn, len(b.history))
	copy(copied, b.history)
	return copied
}

// Adapter returns the active harness voice adapter.
func (b *VoiceAgentBridge) Adapter() HarnessVoiceAdapter {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.adapter
}

// SendVoicePrompt injects the user's transcribed speech into the agent's tmux session.
func (b *VoiceAgentBridge) SendVoicePrompt(prompt string) error {
	sessionName := b.target.TmuxSession
	if sessionName == "" {
		sessionName = b.target.ID
	}

	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return fmt.Errorf("prompt cannot be empty")
	}

	if b.adapter != nil {
		return b.adapter.InjectPrompt(sessionName, trimmed)
	}

	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, trimmed, "Enter")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to send keys to tmux session %q: %w", sessionName, err)
	}

	return nil
}

// CapturePane returns the recent terminal output lines from the agent's tmux pane.
func (b *VoiceAgentBridge) CapturePane(lines int) ([]string, error) {
	sessionName := b.target.TmuxSession
	if sessionName == "" {
		sessionName = b.target.ID
	}

	return CapturePaneLines(sessionName, lines)
}

// WaitForAgentTurn polls the target pane until the agent completes its turn,
// returning the newly generated output lines.
func (b *VoiceAgentBridge) WaitForAgentTurn(ctx context.Context, initialBuf []string, timeout time.Duration) (string, error) {
	sessionName := b.target.TmuxSession
	if sessionName == "" {
		sessionName = b.target.ID
	}

	if b.adapter != nil {
		return b.adapter.WaitForTurn(ctx, sessionName, initialBuf, timeout)
	}

	if timeout <= 0 {
		timeout = 90 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()

	initialLen := len(initialBuf)
	var lastHash string
	quiescentCount := 0
	sawNewContent := false

	for {
		select {
		case <-ctx.Done():
			current, _ := b.CapturePane(initialLen + 80)
			return b.extractDiff(initialBuf, current), nil
		case <-ticker.C:
			current, err := b.CapturePane(initialLen + 80)
			if err != nil {
				continue
			}

			if len(current) > initialLen {
				sawNewContent = true
			}

			h := hashLines(current)
			if h == lastHash && sawNewContent {
				quiescentCount++
				if quiescentCount >= 4 {
					diff := b.extractDiff(initialBuf, current)
					if strings.TrimSpace(diff) != "" {
						return diff, nil
					}
				}
			} else {
				lastHash = h
				quiescentCount = 0
			}
		}
	}
}

func (b *VoiceAgentBridge) extractDiff(before, after []string) string {
	if len(after) <= len(before) {
		return ""
	}

	diffLines := after[len(before):]
	return strings.Join(diffLines, "\n")
}

func hashLines(lines []string) string {
	combined := strings.Join(lines, "\n")
	return fmt.Sprintf("%x", md5.Sum([]byte(combined)))
}

// SanitizeForSpeech cleans raw terminal / agent text into crisp, natural spoken English:
// 1. Strips ANSI color and cursor escapes
// 2. Compresses markdown code blocks into concise spoken cues
// 3. Removes terminal spinners and prompt borders
// 4. Caps long tool outputs to ~2-3 key sentences
func SanitizeForSpeech(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	// 1. Strip ANSI escapes
	text := ansiRegex.ReplaceAllString(raw, "")

	// 2. Strip URLs to avoid reading out long web addresses
	text = urlRegex.ReplaceAllString(text, "web link")

	// 3. Compress Markdown code blocks
	text = codeFenceRegex.ReplaceAllString(text, " I have updated the code block. ")

	// 4. Remove inline backticks
	text = strings.ReplaceAll(text, "`", "")

	// 5. Clean line by line
	lines := strings.Split(text, "\n")
	var cleanLines []string

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}

		// Skip echoed command prompts or spinners
		if strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "❯") || strings.HasPrefix(trimmed, "$") || strings.HasPrefix(trimmed, "?") {
			continue
		}
		if strings.HasPrefix(trimmed, "Running:") || strings.HasPrefix(trimmed, "Executing:") {
			continue
		}

		// Remove spinner artifacts
		cleaned := spinnerChars.ReplaceAllString(trimmed, "")
		cleaned = multipleSpaces.ReplaceAllString(cleaned, " ")
		cleaned = strings.TrimSpace(cleaned)

		if cleaned != "" {
			cleanLines = append(cleanLines, cleaned)
		}
	}

	joined := strings.Join(cleanLines, " ")
	joined = multipleSpaces.ReplaceAllString(joined, " ")
	joined = strings.TrimSpace(joined)

	// If excessively long (e.g. over 350 chars), take the first 2-3 sentences
	if len(joined) > 350 {
		sentences := strings.SplitAfter(joined, ". ")
		if len(sentences) >= 2 {
			joined = strings.TrimSpace(sentences[0] + sentences[1])
		} else {
			joined = joined[:340] + "..."
		}
	}

	return joined
}

// RunInteractiveLoop starts the interactive bidirectional voice chat console with the agent.
func (b *VoiceAgentBridge) RunInteractiveLoop(ctx context.Context) error {
	sessionName := b.target.TmuxSession
	if sessionName == "" {
		sessionName = b.target.ID
	}

	adapterName := "generic"
	if b.adapter != nil {
		adapterName = b.adapter.Name()
	}
	fmt.Printf("\n\033[1;36m=== AI Desk Voice Agent Bridge ===\033[0m\n")
	fmt.Printf("  • Target Session: \033[1m%s\033[0m \033[90m[%s]\033[0m (\033[36m%s\033[0m) [Adapter: \033[35m%s\033[0m]\n", b.target.Title, b.target.Tool, sessionName, adapterName)
	fmt.Printf("  • Models:         \033[32mFaster-Whisper (ASR) ⇄ Kyutai Pocket-TTS (24kHz Mimi)\033[0m\n")
	fmt.Printf("  • Status:         \033[1;32m● PAIRED & LISTENING\033[0m\n")
	fmt.Printf("  • Exit:           Press Ctrl+C to disconnect voice bridge\n\n")

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		fmt.Println("\033[1;33m[🎙 Listening...] Speak into microphone:\033[0m")
		startTurn := time.Now()
		transcript, err := Hear(8, "")
		if err != nil {
			fmt.Printf("\033[90m[Audio input note: %v]\033[0m\n", err)
			time.Sleep(1 * time.Second)
			continue
		}

		trimmedPrompt := strings.TrimSpace(transcript)
		if trimmedPrompt == "" || trimmedPrompt == "." || trimmedPrompt == "..." {
			// No audible speech detected, loop quietly
			time.Sleep(300 * time.Millisecond)
			continue
		}

		fmt.Printf("\n\033[1;36m🎙 You:\033[0m %s\n", trimmedPrompt)

		// 1. Capture pane buffer before sending prompt
		initialBuf, _ := b.CapturePane(100)

		// 2. Record user turn
		b.mu.Lock()
		b.history = append(b.history, VoiceTurn{
			Speaker:   "user",
			Text:      trimmedPrompt,
			Timestamp: time.Now(),
		})
		b.mu.Unlock()

		// 3. Inject prompt into agent's tmux session
		if err := b.SendVoicePrompt(trimmedPrompt); err != nil {
			fmt.Printf("\033[31m[Error injecting prompt into tmux: %v]\033[0m\n", err)
			continue
		}

		fmt.Println("\033[90m[⚙ Agent thinking & executing in tmux...]\033[0m")

		// 4. Wait for agent turn to complete
		agentRawOutput, err := b.WaitForAgentTurn(ctx, initialBuf, 90*time.Second)
		if err != nil {
			fmt.Printf("\033[33m[Notice waiting for turn: %v]\033[0m\n", err)
		}

		var spokenResponse string
		if b.adapter != nil {
			spokenResponse = b.adapter.SanitizeSpokenResponse(agentRawOutput)
		} else {
			spokenResponse = SanitizeForSpeech(agentRawOutput)
		}
		if spokenResponse == "" {
			spokenResponse = "The agent completed the command."
		}

		latency := int(time.Since(startTurn).Milliseconds())
		fmt.Printf("\033[1;35m🤖 Agent:\033[0m %s\n", spokenResponse)

		// 5. Record agent turn
		b.mu.Lock()
		b.history = append(b.history, VoiceTurn{
			Speaker:    "agent",
			Text:       agentRawOutput,
			SpokenText: spokenResponse,
			Timestamp:  time.Now(),
			LatencyMS:  latency,
		})
		b.mu.Unlock()

		// 6. Synthesize speech locally with Kyutai Pocket-TTS
		fmt.Println("\033[90m[🔊 Synthesizing speech with Pocket-TTS 24kHz...]\033[0m")
		wavPath, sErr := Speak(spokenResponse, b.voiceName, "")
		if sErr != nil {
			fmt.Printf("\033[31m[TTS synthesis error: %v]\033[0m\n", sErr)
		} else {
			_ = PlayAudio(wavPath)
		}

		fmt.Println("\033[90m─────────────────────────────────────────────────────────────\033[0m")
		time.Sleep(600 * time.Millisecond)
	}
}
