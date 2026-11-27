package handler

type trajectoryGraphNode struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Title     string         `json:"title"`
	Status    string         `json:"status"`
	IssueID   string         `json:"issue_id"`
	RunID     string         `json:"run_id,omitempty"`
	CreatedAt string         `json:"created_at"`
	Details   map[string]any `json:"details"`
}

type trajectoryGraphEdge struct {
	ID         string `json:"id"`
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"`
	EvidenceID string `json:"evidence_id,omitempty"`
}

type trajectoryGraphFinding struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	NodeID   string `json:"node_id"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type trajectoryGraph struct {
	RootIssueID string                   `json:"root_issue_id"`
	Nodes       []trajectoryGraphNode    `json:"nodes"`
	Edges       []trajectoryGraphEdge    `json:"edges"`
	Findings    []trajectoryGraphFinding `json:"findings"`
	Evidence    []any                    `json:"evidence"`
	Coverage    struct {
		Complete  bool     `json:"complete"`
		Truncated bool     `json:"truncated"`
		Warnings  []string `json:"warnings"`
	} `json:"coverage"`
}

func trajectoryGraphText(details map[string]any, key string) string {
	value, _ := details[key].(string)
	return value
}

func projectTrajectoryGraph(root string, nodes []trajectoryGraphNode, truncated bool) trajectoryGraph {
	graph := trajectoryGraph{RootIssueID: root, Nodes: nodes, Edges: []trajectoryGraphEdge{}, Findings: []trajectoryGraphFinding{}, Evidence: []any{}}
	graph.Coverage.Complete = !truncated
	graph.Coverage.Truncated = truncated
	graph.Coverage.Warnings = []string{}
	present := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		present[node.ID] = true
	}
	seen := map[string]bool{}
	add := func(from, to, kind string) {
		id := kind + ":" + from + ":" + to
		if present[from] && present[to] && !seen[id] {
			seen[id] = true
			graph.Edges = append(graph.Edges, trajectoryGraphEdge{ID: id, From: from, To: to, Kind: kind})
		}
	}
	for _, node := range nodes {
		switch node.Kind {
		case "issue":
			add("issue:"+trajectoryGraphText(node.Details, "parent_id"), node.ID, "parent")
			add("issue:"+trajectoryGraphText(node.Details, "creation_issue_id"), node.ID, "created_from")
		case "run":
			add("issue:"+node.IssueID, node.ID, "contains")
			for _, relation := range [][2]string{{"retry_of", "retry"}, {"rerun_of", "rerun"}, {"delegated_from", "delegation"}, {"parent_run", "delegation"}} {
				add("run:"+trajectoryGraphText(node.Details, relation[0]), node.ID, relation[1])
			}
			if node.Details["issue_snapshot"] == nil {
				graph.Findings = append(graph.Findings, trajectoryGraphFinding{ID: "missing_input_snapshot:" + node.ID, Code: "missing_input_snapshot", NodeID: node.ID, Severity: "gap", Message: "No native input snapshot is stored for this Run. Its historical Issue input is unknown."})
				graph.Coverage.Complete = false
			}
		case "comment", "artifact":
			add("issue:"+node.IssueID, node.ID, "contains")
			if node.RunID != "" {
				add("run:"+node.RunID, node.ID, "produces")
			}
			if node.Kind == "artifact" {
				add("comment:"+trajectoryGraphText(node.Details, "comment_id"), node.ID, "attachment")
			}
		}
	}
	if truncated {
		graph.Coverage.Warnings = append(graph.Coverage.Warnings, "Graph limits reached (200 issues, 8 reference hops, 1000 runs, 2000 records per native record type, 6 MiB of records). Narrow the task filters to inspect more.")
	}
	return graph
}
