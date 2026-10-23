package routines

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/falluba/ai-desk/pkg/deck"
)

// Scheduler coordinates timed evaluation of active recurring routines.
type Scheduler struct {
	sdb      *deck.StateDB
	interval time.Duration
	stopChan chan struct{}
	running  bool
	mu       sync.Mutex
}

// NewScheduler creates a new routines scheduler.
func NewScheduler(sdb *deck.StateDB, interval time.Duration) *Scheduler {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	return &Scheduler{
		sdb:      sdb,
		interval: interval,
		stopChan: make(chan struct{}),
	}
}

// Start initiates the background evaluation loop.
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	ticker := time.NewTicker(s.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-s.stopChan:
				return
			case t := <-ticker.C:
				_ = s.CheckAndRunDue(t)
			}
		}
	}()
}

// Stop terminates the scheduler loop.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	close(s.stopChan)
	s.running = false
}

// CheckAndRunDue identifies and triggers all active routines whose schedule is due.
func (s *Scheduler) CheckAndRunDue(now time.Time) error {
	if s.sdb == nil {
		return fmt.Errorf("state.db connection is nil")
	}

	routines, err := LoadRoutines(s.sdb)
	if err != nil {
		return err
	}

	nowEpoch := now.Unix()
	for _, r := range routines {
		if r.Status != "active" {
			continue
		}

		if r.NextRunAt <= nowEpoch {
			go s.executeRoutine(r, now)
		}
	}
	return nil
}

// TriggerNow manually fires a specific routine immediately out-of-schedule.
func (s *Scheduler) TriggerNow(identifier string) (*RoutineRecord, error) {
	if s.sdb == nil {
		return nil, fmt.Errorf("state.db connection is nil")
	}

	r, err := FindRoutine(s.sdb, identifier)
	if err != nil {
		return nil, err
	}

	go s.executeRoutine(*r, time.Now())
	return r, nil
}

// TriggerSync synchronously executes a routine and waits for completion.
func (s *Scheduler) TriggerSync(identifier string) (*RoutineRecord, error) {
	if s.sdb == nil {
		return nil, fmt.Errorf("state.db connection is nil")
	}

	r, err := FindRoutine(s.sdb, identifier)
	if err != nil {
		return nil, err
	}

	s.executeRoutine(*r, time.Now())
	return FindRoutine(s.sdb, r.ID)
}

func (s *Scheduler) executeRoutine(r RoutineRecord, triggerTime time.Time) {
	sched, err := ParseCron(r.CronExpr, time.Local)
	nextRunEpoch := int64(0)
	if err == nil {
		if next, ok := sched.Next(triggerTime); ok {
			nextRunEpoch = next.Unix()
		}
	}

	cmdStr := strings.TrimSpace(r.Command)
	if cmdStr == "" {
		_ = RecordRoutineRun(s.sdb, r.ID, "skipped", "no command specified", nextRunEpoch)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	out, cErr := cmd.CombinedOutput()

	outStr := strings.TrimSpace(string(out))
	if len(outStr) > 2000 {
		outStr = outStr[:1990] + "..."
	}

	status := "success"
	if cErr != nil {
		status = fmt.Sprintf("error: %v", cErr)
	}

	_ = RecordRoutineRun(s.sdb, r.ID, status, outStr, nextRunEpoch)
}
