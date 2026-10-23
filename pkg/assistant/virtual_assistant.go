package assistant

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/falluba/ai-desk/pkg/deck"
	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/voice"
)

// AssistantConfig configures resident virtual assistant behavior.
type AssistantConfig struct {
	VoiceEnabled    bool          `json:"voice_enabled"`
	AutoFailover    bool          `json:"auto_failover"`
	ProactiveAlerts bool          `json:"proactive_alerts"`
	PollInterval    time.Duration `json:"poll_interval"`
	VoiceName       string        `json:"voice_name"`
}

// DefaultAssistantConfig returns standard virtual assistant defaults.
func DefaultAssistantConfig() AssistantConfig {
	return AssistantConfig{
		VoiceEnabled:    true,
		AutoFailover:    true,
		ProactiveAlerts: true,
		PollInterval:    8 * time.Second,
		VoiceName:       "alba",
	}
}

// VirtualAssistant represents the resident autonomous copilot inspired by Grok Bot.
type VirtualAssistant struct {
	sdb            *deck.StateDB
	virt           *harness.Virtualizer
	config         AssistantConfig
	mu             sync.Mutex
	lastStatuses   map[string]string
	lastExhaustion map[string]bool
	recentEvents   []string
}

// NewVirtualAssistant instantiates a new resident virtual assistant.
func NewVirtualAssistant(sdb *deck.StateDB, virt *harness.Virtualizer, cfg AssistantConfig) *VirtualAssistant {
	if virt == nil {
		virt = harness.NewVirtualizer("", "")
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 8 * time.Second
	}
	if cfg.VoiceName == "" {
		cfg.VoiceName = "alba"
	}
	return &VirtualAssistant{
		sdb:            sdb,
		virt:           virt,
		config:         cfg,
		lastStatuses:   make(map[string]string),
		lastExhaustion: make(map[string]bool),
		recentEvents:   make([]string, 0, 50),
	}
}

// Speak speaks text using the local neural voice if voice is enabled.
func (a *VirtualAssistant) Speak(text string) {
	if !a.config.VoiceEnabled {
		return
	}
	go func() {
		_, _ = voice.Speak(text, a.config.VoiceName, "")
	}()
}

// MonitorTick checks all active sessions once for status changes and quota exhaustion.
func (a *VirtualAssistant) MonitorTick() ([]string, error) {
	if a.sdb == nil {
		return nil, fmt.Errorf("state.db connection is nil")
	}

	instances, err := a.sdb.LoadInstances()
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	var alerts []string

	for _, inst := range instances {
		insp, err := a.sdb.InspectSessionRecord(&inst)
		if err != nil || insp == nil {
			continue
		}

		sessionKey := inst.ID
		prevStatus := a.lastStatuses[sessionKey]
		currentStatus := inst.Status
		a.lastStatuses[sessionKey] = currentStatus

		// 1. Quota Exhaustion & Auto-Failover Detection
		isExhausted := insp.TokensUsed.Exhausted
		wasExhausted := a.lastExhaustion[sessionKey]
		a.lastExhaustion[sessionKey] = isExhausted

		if isExhausted && !wasExhausted {
			reason := insp.TokensUsed.ExhaustionReason
			if reason == "" {
				reason = "Rate limit or quota reached"
			}

			if a.config.AutoFailover {
				updated, fErr := deck.AutoFailoverSession(a.sdb, a.virt, inst.ID, reason)
				if fErr == nil && updated != nil {
					msg := fmt.Sprintf("Alert: Account quota depleted for %q. Auto-switched to %s.", inst.Title, updated.Account)
					alerts = append(alerts, msg)
					a.recentEvents = append(a.recentEvents, msg)
					a.Speak(fmt.Sprintf("Quota limit reached on task %s. Auto-switched account to %s.", inst.Title, updated.Account))
				} else {
					msg := fmt.Sprintf("Warning: Quota exhausted on task %q (%s). Auto-failover failed: %v", inst.Title, inst.Account, fErr)
					alerts = append(alerts, msg)
					a.recentEvents = append(a.recentEvents, msg)
				}
			} else {
				msg := fmt.Sprintf("Warning: Quota exhausted on task %q (%s). Manual switch required.", inst.Title, inst.Account)
				alerts = append(alerts, msg)
				a.recentEvents = append(a.recentEvents, msg)
			}
		}

		// 2. Proactive Status Transition Notifications
		if a.config.ProactiveAlerts && prevStatus != "" && prevStatus != currentStatus {
			if currentStatus == "completed" || currentStatus == "waiting" {
				msg := fmt.Sprintf("Notice: Task %q is now %s.", inst.Title, currentStatus)
				alerts = append(alerts, msg)
				a.recentEvents = append(a.recentEvents, msg)
				a.Speak(fmt.Sprintf("Task %s is now %s.", inst.Title, currentStatus))
			} else if currentStatus == "error" {
				msg := fmt.Sprintf("Alert: Task %q reported an error.", inst.Title)
				alerts = append(alerts, msg)
				a.recentEvents = append(a.recentEvents, msg)
				a.Speak(fmt.Sprintf("Task %s encountered an error.", inst.Title))
			}
		}
	}

	return alerts, nil
}

// GenerateBriefing synthesizes a concise, Grok-bot-style situational report.
func (a *VirtualAssistant) GenerateBriefing() string {
	var sb strings.Builder

	sb.WriteString("\n\033[1;36m=== AI-Desk Resident Virtual Assistant ===\033[0m\n")
	sb.WriteString("Status: \033[1;32m● Active Copilot\033[0m (Inspired by xAI Grok Bot)\n\n")

	if a.sdb == nil {
		sb.WriteString("Database connection unavailable.\n")
		return sb.String()
	}

	instances, err := a.sdb.LoadInstances()
	if err != nil {
		sb.WriteString(fmt.Sprintf("Error querying sessions: %v\n", err))
		return sb.String()
	}

	runningCount := 0
	exhaustedCount := 0
	for _, inst := range instances {
		if inst.Status == "running" {
			runningCount++
		}
		if cached := deck.GetCachedInspection(inst.ID); cached != nil && cached.TokensUsed.Exhausted {
			exhaustedCount++
		}
	}

	sb.WriteString(fmt.Sprintf("  • Active Workloads: %d sessions (%d running)\n", len(instances), runningCount))
	if exhaustedCount > 0 {
		sb.WriteString(fmt.Sprintf("  • Quota Status:     \033[1;31m⚠ %d session(s) hit quota limits\033[0m\n", exhaustedCount))
	} else {
		sb.WriteString("  • Quota Status:     \033[1;32m✓ All accounts healthy\033[0m\n")
	}

	voiceStatus, _ := voice.GetStatus()
	if voiceStatus != nil && voiceStatus.VoxInstalled {
		sb.WriteString(fmt.Sprintf("  • Voice Interface:  \033[1;32m%s\033[0m (%s)\n", voiceStatus.TTSModel, voiceStatus.ASRModel))
	} else {
		sb.WriteString("  • Voice Interface:  Standby (Install vox for neural voice)\n")
	}

	sb.WriteString("\n\033[1mRecent Assistant Events:\033[0m\n")
	a.mu.Lock()
	if len(a.recentEvents) == 0 {
		sb.WriteString("  No critical events recorded. System running smoothly.\n")
	} else {
		start := len(a.recentEvents) - 5
		if start < 0 {
			start = 0
		}
		for _, ev := range a.recentEvents[start:] {
			sb.WriteString(fmt.Sprintf("  • %s\n", ev))
		}
	}
	a.mu.Unlock()

	return sb.String()
}

// StartDaemon runs the background monitoring loop until the context is canceled.
func (a *VirtualAssistant) StartDaemon(ctx context.Context) {
	ticker := time.NewTicker(a.config.PollInterval)
	defer ticker.Stop()

	a.Speak("AI Desk virtual assistant online. Monitoring active tasks and quota.")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = a.MonitorTick()
		}
	}
}
