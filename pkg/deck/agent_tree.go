package deck

// BuildAgentTree adds virtual agent nodes without moving persisted sessions or groups.
func BuildAgentTree(groups []GroupRecord, instances []InstanceRecord, agents []AgentRecord) []*GroupNode {
	groups = append([]GroupRecord{}, groups...)
	instances = append([]InstanceRecord{}, instances...)
	paths := map[string]*AgentRecord{}
	byID := map[string]string{}
	for _, agent := range agents {
		copy := agent
		path := agent.Team + "/@" + agent.ID
		paths[path] = &copy
		byID[agent.ID] = path
		groups = append(groups, GroupRecord{Path: path, Name: agent.Name, Expanded: true})
	}
	for i := range instances {
		cfg, _ := ReadSessionConfig(&instances[i])
		if path, ok := byID[cfg.AgentID]; ok {
			instances[i].GroupPath = path
		}
	}
	roots := BuildGroupTree(groups, instances)
	var visit func([]*GroupNode)
	visit = func(nodes []*GroupNode) {
		for _, node := range nodes {
			node.Agent = paths[node.Path]
			visit(node.Children)
		}
	}
	visit(roots)
	return roots
}
