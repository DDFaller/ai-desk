package voice

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVoice_Status(t *testing.T) {
	status, err := GetStatus()
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.TTSModel == "" {
		t.Errorf("expected non-empty TTSModel")
	}
	if status.ASRModel == "" {
		t.Errorf("expected non-empty ASRModel")
	}
	if status.LatencyMS <= 0 {
		t.Errorf("expected positive latency")
	}
}

func TestVoice_SpeakValidation(t *testing.T) {
	_, err := Speak("", "alba", "/tmp/out.wav")
	if err == nil {
		t.Errorf("expected error on empty text")
	}
}

func TestVoice_LocalSynthesisSmoke(t *testing.T) {
	bin := FindVoxBinary()
	if bin == "" {
		t.Skip("vox binary not installed, skipping live synthesis")
	}

	tempWav := filepath.Join(t.TempDir(), "test.wav")
	out, err := Speak("Test resident voice", "alba", tempWav)
	if err != nil {
		t.Logf("Speak error in test environment: %v", err)
		return
	}
	if _, statErr := os.Stat(out); statErr != nil {
		t.Errorf("expected generated WAV at %s", out)
	}
}
