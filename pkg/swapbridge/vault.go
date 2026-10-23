package swapbridge

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/falluba/ai-desk/pkg/configpaths"
	"os"
	"path/filepath"
	"strings"
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
		home, _ := configpaths.HomeDir()
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
// Supports prefixed keys ("account:<email>") and "go-keyring-base64:" encoding.
func (sb *SwapBridge) ReadAccountToken(email string) (string, error) {
	candidates := []string{
		email,
		"account:" + email,
		"account:" + strings.ToLower(email),
		strings.ToLower(email),
	}

	var decodeValue func(val any) string
	decodeValue = func(val any) string {
		if val == nil {
			return ""
		}
		if s, ok := val.(string); ok {
			if strings.HasPrefix(s, "go-keyring-base64:") {
				b64 := strings.TrimPrefix(s, "go-keyring-base64:")
				if raw, err := base64.StdEncoding.DecodeString(b64); err == nil {
					return string(raw)
				}
			}
			return s
		}
		if m, ok := val.(map[string]any); ok {
			if td, exists := m["token_data"]; exists && td != nil {
				return decodeValue(td)
			}
			if enc, err := json.Marshal(m); err == nil {
				return string(enc)
			}
		}
		return ""
	}

	// 1. Try vault.json
	vaultPath := filepath.Join(sb.geminiDir, "vault.json")
	if data, err := os.ReadFile(vaultPath); err == nil {
		var vault map[string]any
		if json.Unmarshal(data, &vault) == nil {
			for _, k := range candidates {
				if entry, ok := vault[k]; ok {
					if res := decodeValue(entry); res != "" {
						return res, nil
					}
				}
			}
		}
	}

	// 2. Fallback to accounts.json
	accPath := filepath.Join(sb.geminiDir, "accounts.json")
	if data, err := os.ReadFile(accPath); err == nil {
		var raw map[string]any
		if json.Unmarshal(data, &raw) == nil {
			for _, k := range candidates {
				if entry, ok := raw[k]; ok {
					if res := decodeValue(entry); res != "" {
						return res, nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("token for %s not found in vault", email)
}

// GetAccountQuota returns the live quota status for a specific email or account ID.
func (sb *SwapBridge) GetAccountQuota(email string) (*rotation.AccountStatus, error) {
	accounts, err := sb.LoadAccounts()
	if err != nil {
		return nil, err
	}
	normalized := strings.ToLower(strings.TrimSpace(email))
	for _, acc := range accounts {
		if strings.ToLower(acc.Email) == normalized || strings.ToLower(acc.ID) == normalized {
			return &acc, nil
		}
	}
	return nil, fmt.Errorf("account %s not found in swapbridge", email)
}

// ResetTime parses an ISO8601 reset_at timestamp into time.Time.
func ParseResetTime(ts string) time.Time {
	t, _ := time.Parse(time.RFC3339, ts)
	return t
}
