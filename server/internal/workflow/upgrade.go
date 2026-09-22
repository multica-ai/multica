package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
)

// GraphUpgradeResult is the reviewable output of the v1 -> v2 conversion. An
// upgrade can be applied even when Issues is non-empty: the resulting draft is
// then explicitly marked unpublishable until the user repairs those issues.
type GraphUpgradeResult struct {
	SourceSchemaVersion int                `json:"source_schema_version"`
	TargetSchemaVersion int                `json:"target_schema_version"`
	Graph               Graph              `json:"graph"`
	AutoConvertible     bool               `json:"auto_convertible"`
	Issues              []ValidationDetail `json:"issues"`
	Warnings            []ValidationDetail `json:"warnings"`
}

// UpgradeV1Graph converts the stable v1 fields without guessing at semantic
// branching. Serial graphs are converted automatically. Branching that is not
// already expressed with explicit parallel/merge controls is retained in the
// preview and reported as a repair item, so a user can fix it without losing
// the original topology.
func UpgradeV1Graph(source Graph) GraphUpgradeResult {
	result := GraphUpgradeResult{
		SourceSchemaVersion: graphVersion(source),
		TargetSchemaVersion: CurrentGraphSchemaVersion,
		Issues:              []ValidationDetail{},
		Warnings:            []ValidationDetail{},
	}
	result.Graph = cloneGraph(source)
	result.Graph.SchemaVersion = CurrentGraphSchemaVersion

	if result.SourceSchemaVersion >= CurrentGraphSchemaVersion {
		result.AutoConvertible = true
		result.Warnings = append(result.Warnings, detail("already_v2", "workflow is already using schema v2"))
		return result
	}

	// v1 retries were not durable enough to replay safely. The migration keeps
	// the old prompt and references but starts the v2 retry budget at zero.
	result.Graph.Defaults.MaxRetries = 0
	for i := range result.Graph.Nodes {
		node := &result.Graph.Nodes[i]
		if !IsSupportedNodeType(node.Type) {
			result.Issues = append(result.Issues, nodeDetail("unsupported_node_type", node.ID, "type", "legacy node type must be replaced or supported before publishing"))
			continue
		}
		if node.Type == "agent" || node.Type == "human_task" || node.Type == "human_review" {
			if node.Assignee == nil && node.AgentID != "" {
				node.Assignee = &Assignee{Type: "agent", ID: node.AgentID}
			}
			if node.Config == nil {
				node.Config = map[string]any{}
			}
			if _, exists := node.Config["effect_policy"]; !exists {
				node.Config["effect_policy"] = "unknown"
			}
			node.Config["max_retries"] = 0
			node.MaxRetries = 0
			if node.Instructions == "" {
				if prompt, ok := node.Config["prompt"].(string); ok {
					node.Instructions = prompt
				}
			}
			if len(node.Outputs) == 0 {
				node.Outputs = []OutputField{{Key: "text", Label: "Text", Type: "text"}}
			}
		}
	}
	for i := range result.Graph.Edges {
		if result.Graph.Edges[i].Kind == "" {
			result.Graph.Edges[i].Kind = "flow"
		}
	}

	startIDs := nodeIDsByType(result.Graph, "start")
	endIDs := nodeIDsByType(result.Graph, "end")
	if len(startIDs) == 0 {
		result.Graph.Nodes = append(result.Graph.Nodes, Node{ID: "start", Type: "start", Label: "Start", Position: Position{X: 80, Y: 160}})
		startIDs = []string{"start"}
		result.Warnings = append(result.Warnings, detail("start_added", "a fixed Start boundary was added during upgrade"))
	}
	if len(endIDs) == 0 {
		result.Graph.Nodes = append(result.Graph.Nodes, Node{ID: "end", Type: "end", Label: "End", Position: Position{X: 720, Y: 160}})
		endIDs = []string{"end"}
		result.Warnings = append(result.Warnings, detail("end_added", "a fixed End boundary was added during upgrade"))
	}
	if len(startIDs) != 1 {
		result.Issues = append(result.Issues, detail("multiple_start_nodes", "upgrade requires exactly one Start boundary"))
	}
	if len(endIDs) != 1 {
		result.Issues = append(result.Issues, detail("multiple_end_nodes", "upgrade requires exactly one End boundary"))
	}
	if len(startIDs) == 1 && len(endIDs) == 1 {
		connectBoundary(&result.Graph, startIDs[0], endIDs[0], &result)
	}

	result.Warnings = append(result.Warnings, detail("effect_policy_unknown", "legacy executable nodes use effect_policy=unknown until reviewed"))
	result.Warnings = append(result.Warnings, detail("retry_budget_reset", "legacy automatic retries were reset to zero"))

	// Fan-out/fan-in is safe to preserve only when the old graph already names
	// its control nodes. We do not synthesize a parallel semantic from arbitrary
	// cross edges because that can change which outputs are required at merge.
	hasParallel, hasMerge := false, false
	for _, node := range result.Graph.Nodes {
		hasParallel = hasParallel || node.Type == "parallel"
		hasMerge = hasMerge || node.Type == "merge"
	}
	incoming, outgoing := graphDegrees(result.Graph)
	for _, node := range result.Graph.Nodes {
		if outgoing[node.ID] > 1 && !(hasParallel && hasMerge) {
			result.Issues = append(result.Issues, nodeDetail("v1_branch_requires_repair", node.ID, "edges", "legacy fan-out is not a structured parallel step; add parallel and merge controls"))
		}
		if incoming[node.ID] > 1 && !(hasParallel && hasMerge) {
			result.Issues = append(result.Issues, nodeDetail("v1_merge_requires_repair", node.ID, "edges", "legacy fan-in is not a structured merge step; add an explicit merge"))
		}
	}

	if err := Validate(result.Graph, false); err != nil {
		result.Issues = append(result.Issues, validationDetails(err)...)
	}
	result.Issues = uniqueValidationDetails(result.Issues)
	result.Warnings = uniqueValidationDetails(result.Warnings)
	result.AutoConvertible = len(result.Issues) == 0
	return result
}

func cloneGraph(source Graph) Graph {
	encoded, err := json.Marshal(source)
	if err != nil {
		return source
	}
	var cloned Graph
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return source
	}
	return cloned
}

func nodeIDsByType(graph Graph, nodeType string) []string {
	ids := make([]string, 0)
	for _, node := range graph.Nodes {
		if node.Type == nodeType {
			ids = append(ids, node.ID)
		}
	}
	return ids
}

func graphDegrees(graph Graph) (map[string]int, map[string]int) {
	incoming, outgoing := map[string]int{}, map[string]int{}
	for _, edge := range graph.Edges {
		if edge.Kind == "rework" || edge.Kind == "compensation" {
			continue
		}
		outgoing[edge.Source]++
		incoming[edge.Target]++
	}
	return incoming, outgoing
}

func connectBoundary(graph *Graph, startID, endID string, result *GraphUpgradeResult) {
	incoming, outgoing := graphDegrees(*graph)
	roots, sinks := make([]string, 0), make([]string, 0)
	for _, node := range graph.Nodes {
		if node.ID != startID && node.ID != endID && incoming[node.ID] == 0 {
			roots = append(roots, node.ID)
		}
		if node.ID != startID && node.ID != endID && outgoing[node.ID] == 0 {
			sinks = append(sinks, node.ID)
		}
	}
	if len(roots) == 0 && len(sinks) == 0 {
		if len(graph.Nodes) == 2 {
			appendUpgradeEdge(graph, startID, endID)
		}
		return
	}
	for _, root := range roots {
		if root != endID {
			appendUpgradeEdge(graph, startID, root)
		}
	}
	for _, sink := range sinks {
		if sink != startID {
			appendUpgradeEdge(graph, sink, endID)
		}
	}
	if len(roots) > 1 || len(sinks) > 1 {
		result.Issues = append(result.Issues, detail("v1_boundary_branch_requires_repair", "multiple legacy roots or sinks were connected to fixed boundaries; add explicit controls"))
	}
}

func appendUpgradeEdge(graph *Graph, source, target string) {
	for _, edge := range graph.Edges {
		if edge.Source == source && edge.Target == target && edge.Kind != "rework" && edge.Kind != "compensation" {
			return
		}
	}
	graph.Edges = append(graph.Edges, Edge{ID: fmt.Sprintf("upgrade-%s-%s", source, target), Source: source, Target: target, Kind: "flow"})
}

func validationDetails(err error) []ValidationDetail {
	var typed *Error
	if asError(err, &typed) && typed != nil && len(typed.Details) > 0 {
		return append([]ValidationDetail(nil), typed.Details...)
	}
	return []ValidationDetail{detail("upgrade_validation_failed", err.Error())}
}

func asError(err error, target **Error) bool {
	if typed, ok := err.(*Error); ok {
		*target = typed
		return true
	}
	return false
}

func uniqueValidationDetails(details []ValidationDetail) []ValidationDetail {
	seen := make(map[string]bool, len(details))
	result := make([]ValidationDetail, 0, len(details))
	for _, item := range details {
		key := fmt.Sprintf("%s\x00%s\x00%s\x00%s", item.Code, item.NodeID, item.EdgeID, item.Field)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].NodeID != result[j].NodeID {
			return result[i].NodeID < result[j].NodeID
		}
		return result[i].Code < result[j].Code
	})
	return result
}
