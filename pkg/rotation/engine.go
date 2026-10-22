package rotation

import (
	"errors"
	"fmt"
	"time"
)

// Policy defines how accounts or providers are selected.
type Policy string

const (
	PolicySticky     Policy = "sticky"
	PolicyBalanced   Policy = "balanced"
	PolicyRoundRobin Policy = "round-robin"
)

// AccountStatus represents the quota and availability of an account or key.
type AccountStatus struct {
	ID                string  `json:"id"`
	Email             string  `json:"email,omitempty"`
	Provider          string  `json:"provider"`
	RemainingQuotaPct float64 `json:"remaining_quota_pct"`
	InCooldown        bool    `json:"in_cooldown"`
}

// Engine coordinates account selection, policy ranking, and rate-limit failovers.
type Engine struct {
	ledger *CooldownLedger
}

func NewEngine(ledger *CooldownLedger) *Engine {
	if ledger == nil {
		ledger = NewCooldownLedger()
	}
	return &Engine{ledger: ledger}
}

// Ledger returns the underlying cooldown ledger.
func (e *Engine) Ledger() *CooldownLedger {
	return e.ledger
}

// SelectNext selects the next eligible account from the candidates list.
func (e *Engine) SelectNext(candidates []AccountStatus, policy Policy, currentID string) (AccountStatus, error) {
	if len(candidates) == 0 {
		return AccountStatus{}, errors.New("no candidates provided")
	}

	// Filter by eligibility
	eligible := make([]AccountStatus, 0, len(candidates))
	for _, c := range candidates {
		if e.ledger.IsEligible(c.ID) && c.RemainingQuotaPct > 0 {
			c.InCooldown = false
			eligible = append(eligible, c)
		}
	}

	if len(eligible) == 0 {
		return AccountStatus{}, fmt.Errorf("all %d accounts are exhausted or in cooldown", len(candidates))
	}

	// Sticky policy: maintain current account if still eligible
	if policy == PolicySticky && currentID != "" {
		for _, c := range eligible {
			if c.ID == currentID {
				return c, nil
			}
		}
	}

	// Balanced policy: favor highest remaining quota percentage
	if policy == PolicyBalanced || policy == PolicySticky {
		best := eligible[0]
		for _, c := range eligible[1:] {
			if c.RemainingQuotaPct > best.RemainingQuotaPct {
				best = c
			}
		}
		return best, nil
	}

	// Round-robin: find index of current and pick the next eligible one
	if policy == PolicyRoundRobin {
		currIdx := -1
		for i, c := range candidates {
			if c.ID == currentID {
				currIdx = i
				break
			}
		}
		for i := 1; i <= len(candidates); i++ {
			nextIdx := (currIdx + i) % len(candidates)
			cand := candidates[nextIdx]
			if e.ledger.IsEligible(cand.ID) && cand.RemainingQuotaPct > 0 {
				return cand, nil
			}
		}
	}

	return eligible[0], nil
}

// Failover marks an account exhausted and selects the next healthy account.
func (e *Engine) Failover(candidates []AccountStatus, failedID string, cooldownDuration time.Duration, policy Policy) (AccountStatus, error) {
	if cooldownDuration <= 0 {
		cooldownDuration = 15 * time.Minute
	}
	e.ledger.RecordCooldown(failedID, cooldownDuration, "HTTP 429 rate limit or quota exhaustion")
	return e.SelectNext(candidates, policy, failedID)
}
