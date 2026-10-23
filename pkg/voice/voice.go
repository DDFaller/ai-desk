package voice

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// VoiceStatus represents telemetry for local resident voice models.
type VoiceStatus struct {
	TTSModel     string `json:"tts_model"`
	ASRModel     string `json:"asr_model"`
	ResidentWarm bool   `json:"resident_warm"`
	LatencyMS    int    `json:"latency_ms"`
	DaemonActive bool   `json:"daemon_active"`
	VoxInstalled bool   `json:"vox_installed"`
	VoxBinary    string `json:"vox_binary"`
}

// FindVoxBinary locates the local vox executable.
func FindVoxBinary() string {
	if p, err := exec.LookPath("vox"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	candidate := home + "/.local/bin/vox"
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// GetStatus inspects local voice installation and residency.
func GetStatus() (*VoiceStatus, error) {
	binary := FindVoxBinary()
	installed := binary != ""

	status := &VoiceStatus{
		TTSModel:     "Kyutai Pocket-TTS (24kHz Mimi)",
		ASRModel:     "Faster-Whisper (small.en INT8)",
		ResidentWarm: false,
		LatencyMS:    120,
		DaemonActive: false,
		VoxInstalled: installed,
		VoxBinary:    binary,
	}

	if installed {
		status.ResidentWarm = true
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, binary, "daemon", "status")
		out, _ := cmd.CombinedOutput()
		if strings.Contains(string(out), "running") && !strings.Contains(string(out), "stale") && !strings.Contains(string(out), "not running") {
			status.DaemonActive = true
			status.LatencyMS = 80
		}
	}

	return status, nil
}

// Speak synthesizes text locally using the resident neural TTS model.
func Speak(text, voiceName, outputWav string) (string, error) {
	binary := FindVoxBinary()
	if binary == "" {
		return "", fmt.Errorf("vox binary not found; install vox to ~/.local/bin/vox")
	}

	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", fmt.Errorf("text cannot be empty")
	}

	if voiceName == "" {
		voiceName = "alba"
	}

	if outputWav == "" {
		outputWav = fmt.Sprintf("/tmp/ai-desk-speech-%d.wav", time.Now().UnixNano())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "-o", outputWav, "-v", voiceName, trimmed)
	// Ensure config directory exists and is writable
	home, _ := os.UserHomeDir()
	configDir := home + "/.config/vox"
	_ = os.MkdirAll(configDir, 0755)
	testFile := configDir + "/.write_test"
	if err := os.WriteFile(testFile, []byte("1"), 0600); err != nil {
		tmpCfg := "/tmp/ai-desk-vox-config"
		_ = os.MkdirAll(tmpCfg, 0755)
		cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+tmpCfg)
	} else {
		_ = os.Remove(testFile)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("vox synthesis failed: %v (output: %s)", err, strings.TrimSpace(string(out)))
	}

	return outputWav, nil
}

// Hear records from the microphone and returns the local Whisper transcription.
func Hear(timeoutSec int, lang string) (string, error) {
	binary := FindVoxBinary()
	if binary == "" {
		return "", fmt.Errorf("vox binary not found; install vox to ~/.local/bin/vox")
	}

	if timeoutSec <= 0 {
		timeoutSec = 10
	}

	args := []string{"hear", "-t", fmt.Sprintf("%d", timeoutSec)}
	if lang != "" {
		args = append(args, "-l", lang)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec+15)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("vox hear failed: %v (output: %s)", err, strings.TrimSpace(string(out)))
	}

	return strings.TrimSpace(string(out)), nil
}

// StartFastSession ensures the persistent agentdeck_voice_fast tmux session exists.
func StartFastSession() (string, error) {
	sessionName := "agentdeck_voice_fast"

	// Check if already running
	chk := exec.Command("tmux", "has-session", "-t", sessionName)
	if err := chk.Run(); err == nil {
		return sessionName, nil
	}

	binary := FindVoxBinary()
	if binary == "" {
		binary = "vox"
	}

	loopCmd := fmt.Sprintf(`sh -c "printf '\033[1;36m=== AI Desk Resident Voice (Fast Mode) ===\033[0m\n\nSpeak into microphone. Press Ctrl+C when finished.\n\n'; while true; do %s hear -t 8; echo '---'; sleep 0.5; done"`, binary)

	cmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName, loopCmd)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to start tmux voice session: %w", err)
	}

	return sessionName, nil
}

// PlayAudio plays a WAV audio file through available local audio utilities.
func PlayAudio(wavPath string) error {
	if _, err := os.Stat(wavPath); err != nil {
		return fmt.Errorf("audio file does not exist: %w", err)
	}

	// Try in order: aplay, paplay, pw-play, ffplay
	for _, player := range []struct {
		bin  string
		args []string
	}{
		{"aplay", []string{"-q", wavPath}},
		{"paplay", []string{wavPath}},
		{"pw-play", []string{wavPath}},
		{"ffplay", []string{"-nodisp", "-autoexit", "-loglevel", "quiet", wavPath}},
	} {
		if p, err := exec.LookPath(player.bin); err == nil {
			cmd := exec.Command(p, player.args...)
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
	}

	// Fallback to vox directly if installed
	if voxBin := FindVoxBinary(); voxBin != "" {
		cmd := exec.Command(voxBin, "play", wavPath)
		_ = cmd.Run()
	}

	return nil
}

// StartAgentVoiceSession ensures a dedicated tmux session exists running voice chat for an agent.
func StartAgentVoiceSession(targetID, targetTitle string) (string, error) {
	slug := strings.ToLower(targetID)
	slug = strings.ReplaceAll(slug, "/", "_")
	slug = strings.ReplaceAll(slug, " ", "_")
	if len(slug) > 16 {
		slug = slug[:16]
	}
	sessionName := fmt.Sprintf("agentdeck_voice_%s", slug)

	// Check if already running
	chk := exec.Command("tmux", "has-session", "-t", sessionName)
	if err := chk.Run(); err == nil {
		return sessionName, nil
	}

	aiDeskBin := "ai-desk"
	if p, err := exec.LookPath("ai-desk"); err == nil {
		aiDeskBin = p
	} else {
		home, _ := os.UserHomeDir()
		cand := home + "/.local/bin/ai-desk"
		if _, err := os.Stat(cand); err == nil {
			aiDeskBin = cand
		}
	}

	cmdStr := fmt.Sprintf("%s voice chat %q", aiDeskBin, targetID)
	cmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName, cmdStr)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to start tmux agent voice session: %w", err)
	}

	return sessionName, nil
}
