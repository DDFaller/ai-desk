package ui

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// SessionItem represents a running tmux agent session.
type SessionItem struct {
	ID        string
	Title     string
	Windows   int
	Attached  bool
	CreatedAt time.Time
}

// FetchTmuxSessions queries tmux for all active sessions.
func FetchTmuxSessions() []SessionItem {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}|#{session_windows}|#{session_attached}|#{session_created}")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	sessions := make([]SessionItem, 0, len(lines))

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}

		rawName := parts[0]
		windows, _ := strconv.Atoi(parts[1])
		attached := parts[2] == "1"
		createdSec, _ := strconv.ParseInt(parts[3], 10, 64)

		title := rawName
		if strings.HasPrefix(rawName, "agentdeck_") {
			trimmed := strings.TrimPrefix(rawName, "agentdeck_")
			// Remove the random suffix if present (e.g. _db626627)
			if lastIdx := strings.LastIndex(trimmed, "_"); lastIdx > 0 {
				title = trimmed[:lastIdx]
			} else {
				title = trimmed
			}
		}

		sessions = append(sessions, SessionItem{
			ID:        rawName,
			Title:     title,
			Windows:   windows,
			Attached:  attached,
			CreatedAt: time.Unix(createdSec, 0),
		})
	}
	return sessions
}
