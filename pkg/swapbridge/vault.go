package swapbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/falluba/ai-desk/pkg/rotation"
)

type QuotaBucket struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	RemainingFraction float64 `json:"remaining_fraction"`
	ResetAt           string  `json:"reset_at"`
	Window            string  `json:"window"`
}

type QuotaGroup struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Buckets []QuotaBucket `json:"buckets"`
}

type QuotaSnapshot struct {
	Groups []QuotaGroup `json:"groups"`
}

type SavedAccount struct {
	Email           string        `json:"email"`
	Name            string        `json:"name"`
	AccessExpiresAt string        `json:"access_expires_at"`
	QuotaSnapshot   QuotaSnapshot `json:"quota_snapshot"`
}

type SwapBridge struct {
	geminiDir string
}

func NewSwapBridge(geminiDir string) *SwapBridge {
	if geminiDir == "" {
		home, _ := os.UserHomeDir()
		geminiDir = filepath.Join(home, ".gemini", "agy-swap")
	}
	return &SwapBridge{geminiDir: geminiDir}
}

// LoadAccounts reads ~/.gemini/agy-swap/accounts.json and converts to rotation.AccountStatus list.
func (sb *SwapBridge) LoadAccounts() ([]rotation.AccountStatus, error) {
	accPath := filepath.Join(sb.geminiDir, "accounts.json")
	data, err := os.ReadFile(accPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read accounts.json: %w", err)
	}

	var raw map[string]SavedAccount
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse accounts.json: %w", err)
	}

	statuses := make([]rotation.AccountStatus, 0, len(raw))
	for email, acc := range raw {
		// Calculate lowest remaining fraction across groups
		lowestPct := 100.0
		if len(acc.QuotaSnapshot.Groups) > 0 {
			for _, g := range acc.QuotaSnapshot.Groups {
				for _, b := range g.Buckets {
					pct := b.RemainingFraction * 100.0
					if pct < lowestPct {
						lowestPct = pct
					}
				}
			}
		} else {
			lowestPct = 100.0
		}

		statuses = append(statuses, rotation.AccountStatus{
			ID:                email,
			Email:             email,
			Provider:          "google-antigravity",
			RemainingQuotaPct: lowestPct,
		})
	}
	return statuses, nil
}

// ReadAccountToken reads the token data if present in accounts.json or vault.json.
func (sb *SwapBridge) ReadAccountToken(email string) (string, error) {
	vaultPath := filepath.Join(sb.geminiDir, "vault.json")
	if data, err := os.ReadFile(vaultPath); err == nil {
		var vault map[string]any
		if json.Unmarshal(data, &vault) == nil {
			if entry, ok := vault[email]; ok {
				encoded, _ := json.Marshal(entry)
				return string(encoded), nil
			}
		}
	}
	// Fallback to accounts.json
	accPath := filepath.Join(sb.geminiDir, "accounts.json")
	if data, err := os.ReadFile(accPath); err == nil {
		var raw map[string]any
		if json.Unmarshal(data, &raw) == nil {
			if entry, ok := raw[email]; ok {
				encoded, _ := json.Marshal(entry)
				return string(encoded), nil
			}
		}
	}
	return "", fmt.Errorf("token for %s not found in vault", email)
}

// ResetTime parses an ISO8601 reset_at timestamp into time.Time.
func ParseResetTime(ts string) time.Time {
	t, _ := time.Parse(time.RFC3339, ts)
	return t
}
