package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/falluba/ai-desk/pkg/deck"
	"strings"
	"testing"
)

func TestAgentViewFitsNarrowTerminals(t *testing.T) {
	for _, size := range [][2]int{{40, 12}, {80, 24}, {140, 40}} {
		m := Model{width: size[0], height: size[1], showAgentManager: true, agents: []deck.AgentRecord{{Name: strings.Repeat("Long name ", 30), Team: "Product"}}}
		view := m.View()
		if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
			t.Fatalf("view overflows %v: %dx%d", size, lipgloss.Width(view), lipgloss.Height(view))
		}
	}
}

func TestAgentTreeDoesNotMoveStoredSession(t *testing.T) {
	a := deck.AgentRecord{ID: "agent-a", Name: "Builder", Team: "Product"}
	inst := deck.InstanceRecord{ID: "session", GroupPath: "legacy", ToolData: `{"agent_id":"agent-a"}`}
	roots := deck.BuildAgentTree(nil, []deck.InstanceRecord{inst}, []deck.AgentRecord{a})
	found := false
	for _, item := range deck.FlattenVisibleTree(roots) {
		if item.GroupNode != nil && item.GroupNode.Agent != nil {
			found = true
		}
	}
	if !found || inst.GroupPath != "legacy" {
		t.Fatal("agent grouping lost or mutated persistent group")
	}
}
