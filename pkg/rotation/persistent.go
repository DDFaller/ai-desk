package rotation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// FailoverPersisted shares cooldowns across the TUI, monitor, shim and restarts.
func FailoverPersisted(candidates []AccountStatus, failedID string, duration time.Duration, policy Policy, provider string) (AccountStatus, error) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".ai-desk")
	if p := os.Getenv("AI_DESK_RUNTIME_DIR"); p != "" {
		dir = p
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return AccountStatus{}, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "cooldowns.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return AccountStatus{}, err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return AccountStatus{}, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	path := filepath.Join(dir, "cooldowns.json")
	records := map[string]CooldownRecord{}
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, &records); err != nil {
			return AccountStatus{}, fmt.Errorf("read cooldowns: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return AccountStatus{}, err
	}
	ledger := NewCooldownLedger()
	prefix := provider + ":"
	for _, candidate := range candidates {
		if record, ok := records[prefix+candidate.ID]; ok {
			ledger.records[candidate.ID] = record
		}
	}
	engine := NewEngine(ledger)
	chosen, selectionErr := engine.Failover(candidates, failedID, duration, policy)
	for key, record := range records {
		if time.Now().After(record.Until) {
			delete(records, key)
		}
	}
	for key, record := range ledger.ActiveCooldowns() {
		records[prefix+key] = record
	}
	data, err = json.Marshal(records)
	if err != nil {
		return AccountStatus{}, err
	}
	tmp, err := os.CreateTemp(dir, "cooldowns-*.tmp")
	if err != nil {
		return AccountStatus{}, err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return AccountStatus{}, err
	}
	if err = tmp.Close(); err != nil {
		return AccountStatus{}, err
	}
	if err = os.Rename(name, path); err != nil {
		return AccountStatus{}, err
	}
	return chosen, selectionErr
}
