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

	quota, qErr := sb.GetAccountQuota("test@example.com")
	if qErr != nil {
		t.Fatalf("expected to find quota: %v", qErr)
	}
	if quota.RemainingQuotaPct != 85.0 {
		t.Errorf("expected 85%% remaining in GetAccountQuota, got %f", quota.RemainingQuotaPct)
	}
}

func TestSwapBridge_ReadAccountToken_VaultWithPrefixAndBase64(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "vault.json")

	// Raw token JSON: {"access_token":"ya29.secret123"}
	// Base64 encoded: eyJhY2Nlc3NfdG9rZW4iOiJ5YTI5LnNlY3JldDEyMyJ9
	vaultJSON := `{
  "account:user@example.com": "go-keyring-base64:eyJhY2Nlc3NfdG9rZW4iOiJ5YTI5LnNlY3JldDEyMyJ9"
}`
	if err := os.WriteFile(vaultPath, []byte(vaultJSON), 0644); err != nil {
		t.Fatal(err)
	}

	sb := NewSwapBridge(tempDir)
	token, err := sb.ReadAccountToken("user@example.com")
	if err != nil {
		t.Fatalf("unexpected error reading token: %v", err)
	}
	expected := `{"access_token":"ya29.secret123"}`
	if token != expected {
		t.Errorf("got %q, want %q", token, expected)
	}
}

func TestSwapBridge_RealVaultSmoke(t *testing.T) {
	home, _ := os.UserHomeDir()
	vaultPath := filepath.Join(home, ".gemini", "agy-swap", "vault.json")
	if _, err := os.Stat(vaultPath); err != nil {
		t.Skip("skipping real vault test (file not found)")
	}

	sb := NewSwapBridge("")
	accounts, err := sb.LoadAccounts()
	if err != nil {
		t.Fatalf("load accounts error: %v", err)
	}
	for _, acc := range accounts {
		tok, tokErr := sb.ReadAccountToken(acc.Email)
		if tokErr != nil {
			t.Errorf("expected to read token for %s: %v", acc.Email, tokErr)
		} else if len(tok) < 10 {
			t.Errorf("token for %s too short: %q", acc.Email, tok)
		}
	}
}
