package ui

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/falluba/ai-desk/pkg/deck"
)

func TestScrollLatencyOverSections(t *testing.T) {
	// Build 100 tree items across multiple groups/sections
	var items []deck.TreeItem
	for g := 1; g <= 10; g++ {
		groupName := fmt.Sprintf("Section-%d", g)
		groupNode := &deck.GroupNode{
			Name:       groupName,
			Path:       groupName,
			TotalTasks: 9,
		}
		items = append(items, deck.TreeItem{
			IsGroup:   true,
			GroupNode: groupNode,
			Depth:     0,
		})
		for s := 1; s <= 9; s++ {
			items = append(items, deck.TreeItem{
				IsGroup: false,
				Session: &deck.InstanceRecord{
					ID:        fmt.Sprintf("sess-%d-%d", g, s),
					Title:     fmt.Sprintf("Task %d.%d", g, s),
					Tool:      "agy",
					Status:    "running",
					CreatedAt: time.Now().Unix(),
				},
				Depth: 1,
			})
		}
	}

	m := Model{
		width:           120,
		height:          40,
		focus:           FocusSelection,
		treeItems:       items,
		selectedTreeIdx: 0,
	}

	start := time.Now()
	totalScrollEvents := 100

	downKey := tea.KeyMsg{Type: tea.KeyDown}
	for i := 0; i < totalScrollEvents; i++ {
		mUpdated, _ := m.Update(downKey)
		m = mUpdated.(Model)
	}

	elapsed := time.Since(start)
	perItemLatency := elapsed / time.Duration(totalScrollEvents)

	t.Logf("100 section scroll events took: %v (average %.2f µs per scroll)", elapsed, float64(perItemLatency.Microseconds()))

	// Latency per scroll event must be under 2ms (imperceptible / 60-120fps smooth)
	if perItemLatency > 2*time.Millisecond {
		t.Fatalf("scrolling is too slow: %v per scroll event (exceeds 2ms threshold)", perItemLatency)
	}
}
