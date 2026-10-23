package deck

import (
	"sort"
	"strings"
)

// GroupNode represents a node in the hierarchical group tree.
type GroupNode struct {
	Agent      *AgentRecord
	Path       string
	Name       string
	Depth      int
	Expanded   bool
	Children   []*GroupNode
	Sessions   []InstanceRecord
	TotalTasks int // Sessions in this group + all nested subgroups
}

// TreeItem represents a flattened visible row for TUI tree display.
type TreeItem struct {
	IsGroup   bool
	GroupNode *GroupNode
	Session   *InstanceRecord
	Depth     int
}

// BuildGroupTree constructs a forest of root GroupNodes from raw groups and instances.
func BuildGroupTree(groups []GroupRecord, instances []InstanceRecord) []*GroupNode {
	nodeMap := make(map[string]*GroupNode)

	// 1. Initialize nodes from groups table
	for _, g := range groups {
		cleanPath := strings.Trim(g.Path, "/")
		if cleanPath == "" {
			continue
		}
		ensureGroupAncestry(nodeMap, cleanPath)
		parts := strings.Split(cleanPath, "/")
		name := g.Name
		if name == "" {
			name = parts[len(parts)-1]
		}
		nodeMap[cleanPath] = &GroupNode{
			Path:     cleanPath,
			Name:     name,
			Depth:    len(parts) - 1,
			Expanded: g.Expanded,
			Children: make([]*GroupNode, 0),
			Sessions: make([]InstanceRecord, 0),
		}
	}

	// 2. Associate instances to groups, creating implied groups if needed
	for _, inst := range instances {
		grpPath := strings.Trim(inst.GroupPath, "/")
		if grpPath == "" {
			grpPath = "Uncategorized"
		}

		ensureGroupAncestry(nodeMap, grpPath)
		node := nodeMap[grpPath]
		node.Sessions = append(node.Sessions, inst)
	}

	// 3. Link parent-child relationships
	var roots []*GroupNode
	for path, node := range nodeMap {
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash == -1 {
			// Root group
			roots = append(roots, node)
		} else {
			parentPath := path[:lastSlash]
			ensureGroupAncestry(nodeMap, parentPath)
			parent := nodeMap[parentPath]
			parent.Children = append(parent.Children, node)
		}
	}

	// 4. Sort children and calculate TotalTasks recursively
	sortGroupNodes(roots)
	for _, root := range roots {
		calculateTaskCounts(root)
	}

	return roots
}

func ensureGroupAncestry(nodeMap map[string]*GroupNode, path string) {
	parts := strings.Split(path, "/")
	current := ""
	for i, seg := range parts {
		if current == "" {
			current = seg
		} else {
			current = current + "/" + seg
		}
		if _, exists := nodeMap[current]; !exists {
			nodeMap[current] = &GroupNode{
				Path:     current,
				Name:     seg,
				Depth:    i,
				Expanded: true,
				Children: make([]*GroupNode, 0),
				Sessions: make([]InstanceRecord, 0),
			}
		}
	}
}

func sortGroupNodes(nodes []*GroupNode) {
	sort.Slice(nodes, func(i, j int) bool {
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
	for _, n := range nodes {
		sort.Slice(n.Sessions, func(i, j int) bool {
			return strings.ToLower(n.Sessions[i].Title) < strings.ToLower(n.Sessions[j].Title)
		})
		sortGroupNodes(n.Children)
	}
}

func calculateTaskCounts(n *GroupNode) int {
	total := len(n.Sessions)
	for _, child := range n.Children {
		total += calculateTaskCounts(child)
	}
	n.TotalTasks = total
	return total
}

// FlattenVisibleTree produces a slice of TreeItems taking `Expanded` state into account.
func FlattenVisibleTree(roots []*GroupNode) []TreeItem {
	var items []TreeItem
	for _, root := range roots {
		flattenNode(root, &items)
	}
	return items
}

func flattenNode(n *GroupNode, out *[]TreeItem) {
	// Add the group itself
	*out = append(*out, TreeItem{
		IsGroup:   true,
		GroupNode: n,
		Depth:     n.Depth,
	})

	if !n.Expanded {
		return
	}

	// Add sub-groups
	for _, child := range n.Children {
		flattenNode(child, out)
	}

	// Add direct sessions
	for i := range n.Sessions {
		*out = append(*out, TreeItem{
			IsGroup: false,
			Session: &n.Sessions[i],
			Depth:   n.Depth + 1,
		})
	}
}
