package rotation

import (
	"testing"
	"time"
)

func TestRotationEngine_Policies(t *testing.T) {
	ledger := NewCooldownLedger()
	eng := NewEngine(ledger)

	accounts := []AccountStatus{
		{ID: "acc-1", Email: "primary@example.com", RemainingQuotaPct: 80.0},
		{ID: "acc-2", Email: "secondary@example.com", RemainingQuotaPct: 95.0},
		{ID: "acc-3", Email: "fallback@example.com", RemainingQuotaPct: 40.0},
	}

	// 1. Sticky policy: should stay on current if eligible
	selected, err := eng.SelectNext(accounts, PolicySticky, "acc-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.ID != "acc-1" {
		t.Errorf("expected acc-1 for sticky, got %s", selected.ID)
	}

	// 2. Balanced policy: should pick acc-2 (highest 95%)
	selected, err = eng.SelectNext(accounts, PolicyBalanced, "acc-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.ID != "acc-2" {
		t.Errorf("expected acc-2 for balanced, got %s", selected.ID)
	}

	// 3. Failover: mark acc-2 in cooldown and select next best
	next, err := eng.Failover(accounts, "acc-2", 30*time.Minute, PolicyBalanced)
	if err != nil {
		t.Fatalf("failover error: %v", err)
	}
	if next.ID != "acc-1" {
		t.Errorf("expected acc-1 after acc-2 failover, got %s", next.ID)
	}
	if ledger.IsEligible("acc-2") {
		t.Errorf("acc-2 should be in cooldown")
	}

	// 4. Round-robin policy
	rrSelected, err := eng.SelectNext(accounts, PolicyRoundRobin, "acc-1")
	if err != nil {
		t.Fatalf("round-robin error: %v", err)
	}
	// acc-2 is in cooldown, so next should be acc-3
	if rrSelected.ID != "acc-3" {
		t.Errorf("expected acc-3 for round robin (skipping cooldown acc-2), got %s", rrSelected.ID)
	}
}

func TestAccountProfiles(t *testing.T) {
	tmpFile := t.TempDir() + "/profiles.yaml"
	cfg, err := LoadAccountProfiles(tmpFile)
	if err != nil {
		t.Fatalf("LoadAccountProfiles failed: %v", err)
	}

	if cfg.ActiveProfile != "default" {
		t.Errorf("expected active profile default, got %s", cfg.ActiveProfile)
	}

	// Assign account to work profile
	if err := cfg.AssignAccount("work", "work-dev@example.com"); err != nil {
		t.Fatalf("AssignAccount failed: %v", err)
	}
	if err := cfg.Save(tmpFile); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Reload
	reloaded, err := LoadAccountProfiles(tmpFile)
	if err != nil {
		t.Fatalf("reloaded failed: %v", err)
	}
	workProf := reloaded.Profiles["work"]
	if len(workProf.Accounts) != 1 || workProf.Accounts[0] != "work-dev@example.com" {
		t.Errorf("expected work-dev@example.com in work profile, got %v", workProf.Accounts)
	}

	allAccounts := []AccountStatus{
		{Email: "work-dev@example.com", RemainingQuotaPct: 100},
		{Email: "personal@example.com", RemainingQuotaPct: 80},
	}
	filtered := reloaded.FilterAccounts("work", allAccounts)
	if len(filtered) != 1 || filtered[0].Email != "work-dev@example.com" {
		t.Errorf("expected only work-dev filtered, got %v", filtered)
	}
}
