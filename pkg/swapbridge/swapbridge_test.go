package swapbridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSwapBridge_LoadAccounts(t *testing.T) {
	tempDir := t.TempDir()
	accPath := filepath.Join(tempDir, "accounts.json")

	sampleJSON := `{
  "test@example.com": {
    "email": "test@example.com",
    "name": "Test User",
    "quota_snapshot": {
      "groups": [
        {
          "id": "gemini",
          "buckets": [
            {
              "id": "gemini-5h",
              "remaining_fraction": 0.85,
              "reset_at": "2026-10-22T18:00:00Z"
            }
          ]
        }
      ]
    }
  }
}`
	if err := os.WriteFile(accPath, []byte(sampleJSON), 0644); err != nil {
		t.Fatal(err)
	}

	sb := NewSwapBridge(tempDir)
	accounts, err := sb.LoadAccounts()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	if accounts[0].Email != "test@example.com" {
		t.Errorf("expected test@example.com, got %s", accounts[0].Email)
	}
	if accounts[0].RemainingQuotaPct != 85.0 {
		t.Errorf("expected 85%% remaining, got %f", accounts[0].RemainingQuotaPct)
	}
}
