package workflow

import (
	"encoding/json"
	"testing"
)

func TestUpgradeV1SerialGraph(t *testing.T) {
	graph := Graph{
		Nodes: []Node{
			{ID: "start", Type: "start", Label: "Start"},
			{ID: "agent", Type: "agent", Label: "Draft", AgentID: "agent-1", Instructions: "write"},
			{ID: "end", Type: "end", Label: "End"},
		},
		Edges: []Edge{{ID: "s-a", Source: "start", Target: "agent"}, {ID: "a-e", Source: "agent", Target: "end"}},
	}
	result := UpgradeV1Graph(graph)
	if !result.AutoConvertible || len(result.Issues) != 0 {
		t.Fatalf("upgrade result = %#v", result)
	}
	if result.Graph.SchemaVersion != CurrentGraphSchemaVersion || result.Graph.Defaults.MaxRetries != 0 {
		t.Fatalf("converted graph = %#v", result.Graph)
	}
	converted := result.Graph.Nodes[1]
	if converted.Assignee == nil || converted.Assignee.Type != "agent" || converted.Assignee.ID != "agent-1" {
		t.Fatalf("assignee = %#v", converted.Assignee)
	}
	if len(converted.Outputs) != 1 || converted.Outputs[0].Key != "text" || converted.Config["effect_policy"] != "unknown" {
		t.Fatalf("converted node = %#v", converted)
	}
	if result.Graph.Edges[0].Kind != "flow" {
		t.Fatalf("edge kind = %q", result.Graph.Edges[0].Kind)
	}
}

func TestUpgradeV1BranchRequiresExplicitControls(t *testing.T) {
	graph := Graph{
		Nodes: []Node{{ID: "start", Type: "start"}, {ID: "left", Type: "agent", AgentID: "a", Instructions: "left"}, {ID: "right", Type: "agent", AgentID: "b", Instructions: "right"}, {ID: "end", Type: "end"}},
		Edges: []Edge{{ID: "s-l", Source: "start", Target: "left"}, {ID: "s-r", Source: "start", Target: "right"}, {ID: "l-e", Source: "left", Target: "end"}, {ID: "r-e", Source: "right", Target: "end"}},
	}
	result := UpgradeV1Graph(graph)
	if result.AutoConvertible {
		t.Fatal("irregular v1 branch must require repair")
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Code == "v1_branch_requires_repair" || issue.Code == "v1_merge_requires_repair" {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %#v", result.Issues)
	}
}

func TestNodePreservesUnknownFields(t *testing.T) {
	var graph Graph
	if err := json.Unmarshal([]byte(`{"schema_version":2,"nodes":[{"id":"future","type":"loop_v3","label":"Future","position":{"x":1,"y":2},"future_config":{"mode":"bounded"}}],"edges":[]}`), &graph); err != nil {
		t.Fatal(err)
	}
	if IsSupportedNodeType(graph.Nodes[0].Type) {
		t.Fatal("future node unexpectedly supported")
	}
	encoded, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	nodes := raw["nodes"].([]any)
	node := nodes[0].(map[string]any)
	if node["future_config"].(map[string]any)["mode"] != "bounded" {
		t.Fatalf("unknown field was lost: %s", encoded)
	}
	if err := Validate(graph, false); err != nil {
		t.Fatalf("unknown node should remain editable as a draft: %v", err)
	}
	if err := Validate(graph, true); err == nil {
		t.Fatal("unknown node must not be runnable")
	}
}

func TestUpgradeUnknownNodeRequiresRepair(t *testing.T) {
	result := UpgradeV1Graph(Graph{
		Nodes: []Node{{ID: "future", Type: "loop_v3"}},
	})
	if result.AutoConvertible {
		t.Fatal("unknown legacy node must not be auto-converted")
	}
	for _, issue := range result.Issues {
		if issue.Code == "unsupported_node_type" && issue.NodeID == "future" {
			return
		}
	}
	t.Fatalf("issues = %#v", result.Issues)
}
