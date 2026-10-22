package rtk

import (
	"testing"
)

func TestRTK_ClientLifecycle(t *testing.T) {
	client := NewClient()
	if !client.IsInstalled() {
		t.Skip("rtk binary not installed, skipping live tests")
	}

	ver, err := client.Version()
	if err != nil {
		t.Fatalf("expected version without error, got %v", err)
	}
	if ver == "" {
		t.Fatalf("expected non-empty version string")
	}
	t.Logf("Detected RTK version: %s (path: %s)", ver, client.BinPath())
}
