package rtk

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SavingsSummary represents the summary object returned by `rtk gain -f json`.
type SavingsSummary struct {
	TotalCommands     int     `json:"total_commands"`
	TotalInput        int     `json:"total_input"`
	TotalOutput       int     `json:"total_output"`
	TotalSaved        int     `json:"total_saved"`
	AvgSavingsPct     float64 `json:"avg_savings_pct"`
	TotalTimeMs       int     `json:"total_time_ms"`
	AvgTimeMs         int     `json:"avg_time_ms"`
	OversizedExcluded int     `json:"oversized_excluded"`
}

// GainResponse represents the top-level JSON from `rtk gain -f json`.
type GainResponse struct {
	Summary SavingsSummary `json:"summary"`
}

// Client wraps operations for RTK (Rust Token Killer).
type Client struct {
	binPath string
}

// NewClient creates an RTK client, resolving the rtk binary path.
func NewClient() *Client {
	bin, err := exec.LookPath("rtk")
	if err != nil {
		home, _ := os.UserHomeDir()
		localBin := filepath.Join(home, ".local", "bin", "rtk")
		if _, err := os.Stat(localBin); err == nil {
			bin = localBin
		}
	}
	return &Client{binPath: bin}
}

// IsInstalled returns true if the rtk binary is available.
func (c *Client) IsInstalled() bool {
	return c.binPath != ""
}

// BinPath returns the resolved path to the rtk binary.
func (c *Client) BinPath() string {
	return c.binPath
}

// Version returns the version string of the installed rtk binary.
func (c *Client) Version() (string, error) {
	if !c.IsInstalled() {
		return "", fmt.Errorf("rtk binary not found in PATH or ~/.local/bin")
	}
	out, err := exec.Command(c.binPath, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("get rtk version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GetSavings queries RTK global token savings via `rtk gain -f json`.
func (c *Client) GetSavings() (*SavingsSummary, error) {
	if !c.IsInstalled() {
		return nil, fmt.Errorf("rtk binary not found")
	}
	cmd := exec.Command(c.binPath, "gain", "-f", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("query rtk gain: %w", err)
	}

	var resp GainResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse rtk gain json: %w", err)
	}
	return &resp.Summary, nil
}

// InitHarnessHooks configures global RTK hooks for Claude, Gemini, Codex, and OpenCode.
func (c *Client) InitHarnessHooks() error {
	if !c.IsInstalled() {
		return fmt.Errorf("rtk binary not found")
	}

	// 1. Claude Code
	_ = exec.Command(c.binPath, "init", "-g", "--auto-patch", "--no-trust-filters").Run()

	// 2. Gemini / Antigravity
	_ = exec.Command(c.binPath, "init", "-g", "--gemini", "--auto-patch").Run()
	_ = exec.Command(c.binPath, "init", "--agent", "antigravity").Run()

	// 3. Codex
	_ = exec.Command(c.binPath, "init", "-g", "--codex").Run()

	// 4. OpenCode
	_ = exec.Command(c.binPath, "init", "-g", "--opencode").Run()

	return nil
}
