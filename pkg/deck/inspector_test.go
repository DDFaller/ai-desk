package deck

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseCodexRolloutTokensWithRateLimits(t *testing.T) {
	tmpDir := t.TempDir()
	rolloutFile := filepath.Join(tmpDir, "rollout-test.jsonl")

	content := `{"timestamp":"2026-10-23T14:31:15.477Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":836814,"cached_input_tokens":797056,"output_tokens":19765,"reasoning_output_tokens":1441,"total_tokens":856579},"model_context_window":258400},"rate_limits":{"limit_id":"codex","primary":{"used_percent":99.0,"window_minutes":10080,"resets_at":1793307636},"credits":{"has_credits":false,"balance":"0"},"plan_type":"prolite"}}}
{"timestamp":"2026-10-23T14:31:16.397Z","type":"event_msg","payload":{"type":"task_complete","error":{"message":"You've hit your usage limit. Try again at Oct 29th, 2026.","codex_error_info":"usage_limit_exceeded"}}}`

	if err := os.WriteFile(rolloutFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test rollout: %v", err)
	}

	usage, ok := parseCodexRolloutTokens(rolloutFile)
	if !ok {
		t.Fatalf("expected parseCodexRolloutTokens to return ok=true")
	}

	if usage.TotalTokens != 856579 {
		t.Errorf("expected TotalTokens 856579, got %d", usage.TotalTokens)
	}
	if usage.RateLimitUsedPct != 99.0 {
		t.Errorf("expected RateLimitUsedPct 99.0, got %f", usage.RateLimitUsedPct)
	}
	if usage.PlanType != "prolite" {
		t.Errorf("expected PlanType prolite, got %s", usage.PlanType)
	}
	expectedReset := time.Unix(1793307636, 0)
	if !usage.RateLimitResetAt.Equal(expectedReset) {
		t.Errorf("expected ResetAt %v, got %v", expectedReset, usage.RateLimitResetAt)
	}
	if !usage.Exhausted {
		t.Errorf("expected Exhausted to be true")
	}
	if usage.ExhaustionReason != "usage_limit_exceeded" {
		t.Errorf("expected ExhaustionReason usage_limit_exceeded, got %s", usage.ExhaustionReason)
	}
}

func TestExtractTokenMetricsAgy(t *testing.T) {
	inst := &InstanceRecord{
		ID:      "inst-agy-test",
		Title:   "AGY Stream Task",
		Tool:    "agy",
		Account: "danielmcf15@gmail.com",
	}

	usage, ok := extractTokenMetrics(inst)
	if !ok {
		t.Skip("skipping agy vault test if accounts.json is not present")
	}

	if usage.PlanType != "Google Antigravity" {
		t.Errorf("expected PlanType 'Google Antigravity', got %s", usage.PlanType)
	}
	if usage.AccountEmail != "danielmcf15@gmail.com" {
		t.Errorf("expected AccountEmail 'danielmcf15@gmail.com', got %s", usage.AccountEmail)
	}
	if usage.RateLimitUsedPct <= 0.0 || usage.RateLimitUsedPct > 100.0 {
		t.Errorf("unexpected RateLimitUsedPct: %f", usage.RateLimitUsedPct)
	}
}

func TestResolveInstanceAccount(t *testing.T) {
	// 1. Explicit email
	inst1 := &InstanceRecord{
		ID:      "test-1",
		Title:   "MoraakyAI",
		Tool:    "agy",
		Account: "danielmcf15@gmail.com",
	}
	res1 := ResolveInstanceAccount(inst1)
	if res1.Email != "danielmcf15@gmail.com" {
		t.Errorf("expected email 'danielmcf15@gmail.com', got %s", res1.Email)
	}
	if res1.DisplayName != "danielmcf15@gmail.com" {
		t.Errorf("expected displayName 'danielmcf15@gmail.com', got %s", res1.DisplayName)
	}
	if res1.IsDefault {
		t.Errorf("expected isDefault false")
	}

	// 2. Default account on AGY tool
	inst2 := &InstanceRecord{
		ID:      "test-2",
		Title:   "job-applies",
		Tool:    "agy",
		Account: "default",
	}
	res2 := ResolveInstanceAccount(inst2)
	if res2.Email == "" {
		t.Errorf("expected non-empty resolved email for agy default")
	}
	if !res2.IsDefault {
		t.Errorf("expected isDefault true for default account")
	}

	// 3. Codex session with empty account
	inst3 := &InstanceRecord{
		ID:      "test-3",
		Title:   "codex-worker",
		Tool:    "codex",
		Account: "",
	}
	res3 := ResolveInstanceAccount(inst3)
	if res3.DisplayName != "default" {
		t.Errorf("expected displayName 'default', got %s", res3.DisplayName)
	}
	if !res3.IsDefault {
		t.Errorf("expected isDefault true for empty account")
	}

	// 4. InvalidateInspectionCache evicts cached inspections and tokens
	InvalidateInspectionCache("test-1")
	InvalidateInspectionCache("")
}

func TestQuotaDetectionDoesNotRotateForQuotedTextOrOtherErrors(t *testing.T) {
	for _, line := range []string{
		`{"type":"user","message":"usage_limit_exceeded"}`,
		`{"type":"event_msg","payload":{"type":"task_complete","error":{"codex_error_info":"network_error","message":"network unavailable"}}}`,
	} {
		if reason := quotaEvent([]byte(line)); reason != "" {
			t.Fatalf("false quota signal %q", reason)
		}
	}
	line := `{"type":"system","subtype":"api_error","error":{"status":429,"type":"rate_limit_error"}}`
	if quotaEvent([]byte(line)) == "" {
		t.Fatal("missed provider rate limit")
	}
}
