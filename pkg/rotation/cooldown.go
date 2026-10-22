package rotation

import (
	"sync"
	"time"
)

// CooldownRecord stores when an account or provider key was placed in cooldown.
type CooldownRecord struct {
	AccountID string    `json:"account_id"`
	Reason    string    `json:"reason"`
	Until     time.Time `json:"until"`
}

// CooldownLedger manages rate-limit cooldown timers thread-safely.
type CooldownLedger struct {
	mu      sync.RWMutex
	records map[string]CooldownRecord
}

func NewCooldownLedger() *CooldownLedger {
	return &CooldownLedger{
		records: make(map[string]CooldownRecord),
	}
}

// RecordCooldown puts an account/key in cooldown for the specified duration.
func (c *CooldownLedger) RecordCooldown(accountID string, duration time.Duration, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records[accountID] = CooldownRecord{
		AccountID: accountID,
		Reason:    reason,
		Until:     time.Now().Add(duration),
	}
}

// IsEligible reports whether an account is free of active cooldowns.
func (c *CooldownLedger) IsEligible(accountID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rec, exists := c.records[accountID]
	if !exists {
		return true
	}
	return time.Now().After(rec.Until)
}

// ResetCooldown clears an active cooldown immediately.
func (c *CooldownLedger) ResetCooldown(accountID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.records, accountID)
}

// ActiveCooldowns returns all accounts currently in cooldown.
func (c *CooldownLedger) ActiveCooldowns() map[string]CooldownRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()
	active := make(map[string]CooldownRecord)
	now := time.Now()
	for k, v := range c.records {
		if now.Before(v.Until) {
			active[k] = v
		}
	}
	return active
}
