package workflow

import (
	"encoding/json"
	"testing"
)

func testNode(id, kind string) Node {
	return Node{ID: id, Type: kind, Label: id, Position: Position{0, 0}, AgentID: "agent-1", Instructions: "run"}
}

func TestValidateRunnableGraphs(t *testing.T) {
	tests := []struct {
		name    string
		graph   Graph
		wantErr bool
	}{
		{
			name: "serial",
			graph: Graph{
				Nodes: []Node{testNode("start", "start"), testNode("agent", "agent"), testNode("end", "end")},
				Edges: []Edge{{ID: "s-a", Source: "start", Target: "agent"}, {ID: "a-e", Source: "agent", Target: "end"}},
			},
		},
		{
			name: "parallel merge",
			graph: Graph{
				Nodes: []Node{testNode("start", "start"), testNode("left", "agent"), testNode("right", "agent"), testNode("end", "end")},
				Edges: []Edge{
					{ID: "s-l", Source: "start", Target: "left"},
					{ID: "s-r", Source: "start", Target: "right"},
					{ID: "l-e", Source: "left", Target: "end"},
					{ID: "r-e", Source: "right", Target: "end"},
				},
			},
		},
		{
			name: "cycle",
			graph: Graph{
				Nodes: []Node{testNode("start", "start"), testNode("agent", "agent"), testNode("end", "end")},
				Edges: []Edge{{ID: "s-a", Source: "start", Target: "agent"}, {ID: "a-s", Source: "agent", Target: "start"}, {ID: "a-e", Source: "agent", Target: "end"}},
			},
			wantErr: true,
		},
		{
			name: "isolated node",
			graph: Graph{
				Nodes: []Node{testNode("start", "start"), testNode("agent", "agent"), testNode("orphan", "agent"), testNode("end", "end")},
				Edges: []Edge{{ID: "s-a", Source: "start", Target: "agent"}, {ID: "a-e", Source: "agent", Target: "end"}},
			},
			wantErr: true,
		},
		{
			name: "invalid ancestor reference",
			graph: Graph{
				Nodes: []Node{
					testNode("start", "start"),
					{ID: "a", Type: "agent", Label: "a", AgentID: "agent-1", Instructions: "run", InputRefs: []string{"b"}},
					testNode("b", "agent"),
					testNode("end", "end"),
				},
				Edges: []Edge{{ID: "s-a", Source: "start", Target: "a"}, {ID: "a-e", Source: "a", Target: "end"}, {ID: "s-b", Source: "start", Target: "b"}, {ID: "b-e", Source: "b", Target: "end"}},
			},
			wantErr: true,
		},
		{
			name: "duplicate edge connection",
			graph: Graph{
				Nodes: []Node{testNode("start", "start"), testNode("agent", "agent"), testNode("end", "end")},
				Edges: []Edge{
					{ID: "s-a-1", Source: "start", Target: "agent"},
					{ID: "s-a-2", Source: "start", Target: "agent"},
					{ID: "a-e", Source: "agent", Target: "end"},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.graph, true)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateDraftAllowsIncompleteGraph(t *testing.T) {
	graph := Graph{Nodes: []Node{testNode("start", "start"), testNode("end", "end")}}
	if err := Validate(graph, false); err != nil {
		t.Fatalf("draft should allow an incomplete graph: %v", err)
	}
}

func TestValidateV2AllowsPureHumanWorkflow(t *testing.T) {
	graph := Graph{
		SchemaVersion: 2,
		Nodes: []Node{
			{ID: "start", Type: "start", Label: "Start"},
			{ID: "collect", Type: "human_task", Label: "Collect", Assignee: &Assignee{Type: "member", ID: "member-1"}},
			{ID: "review", Type: "human_review", Label: "Review", Assignee: &Assignee{Type: "member", ID: "member-2"}},
			{ID: "end", Type: "end", Label: "End"},
		},
		Edges: []Edge{
			{ID: "s-c", Source: "start", Target: "collect"},
			{ID: "c-r", Source: "collect", Target: "review"},
			{ID: "r-e", Source: "review", Target: "end"},
		},
	}
	if err := Validate(graph, true); err != nil {
		t.Fatalf("pure human workflow should be runnable: %v", err)
	}
	plan, err := Compile(graph)
	if err != nil || plan.StartNodeID != "start" || plan.EndNodeID != "end" {
		t.Fatalf("compiled plan = %#v, err = %v", plan, err)
	}
}

func TestValidateV2ReturnsStructuredErrors(t *testing.T) {
	graph := Graph{
		SchemaVersion: 2,
		Nodes: []Node{
			{ID: "start", Type: "start", Label: "Start"},
			{ID: "agent", Type: "agent", Label: "Agent"},
			{ID: "end", Type: "end", Label: "End"},
		},
		Edges: []Edge{{ID: "s-a", Source: "start", Target: "agent"}, {ID: "a-e", Source: "agent", Target: "end"}},
	}
	err := Validate(graph, true)
	workflowErr, ok := err.(*Error)
	if !ok || workflowErr.Code != "workflow_validation_failed" {
		t.Fatalf("expected structured validation error, got %#v", err)
	}
	if len(workflowErr.Details) != 2 {
		t.Fatalf("details = %#v, want agent and instructions errors", workflowErr.Details)
	}
}

func TestV2ConditionRequiresDefault(t *testing.T) {
	graph := Graph{
		SchemaVersion: 2,
		Nodes: []Node{
			{ID: "start", Type: "start", Label: "Start"},
			{ID: "condition", Type: "condition", Label: "Condition"},
			{ID: "end", Type: "end", Label: "End"},
		},
		Edges: []Edge{{ID: "s-c", Source: "start", Target: "condition"}, {ID: "c-e", Source: "condition", Target: "end", SourcePort: "branch-1"}},
	}
	if err := Validate(graph, true); err == nil {
		t.Fatal("condition without a default branch should be rejected")
	}
}

func TestWorkflowDefaultsAcceptNestedRetryShape(t *testing.T) {
	var graph Graph
	if err := json.Unmarshal([]byte(`{"schema_version":2,"defaults":{"retry":{"max_retries":3,"initial_delay_seconds":10,"backoff_multiplier":2,"max_delay_seconds":100},"execution_timeout_seconds":60},"nodes":[],"edges":[]}`), &graph); err != nil {
		t.Fatal(err)
	}
	if graph.Defaults.MaxRetries != 3 || graph.Defaults.InitialDelaySeconds != 10 || graph.Defaults.ExecutionTimeoutSeconds != 60 {
		t.Fatalf("defaults = %#v", graph.Defaults)
	}
}

func TestGraphJSONEmitsArraysForNilCollections(t *testing.T) {
	encoded, err := json.Marshal(Graph{})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["nodes"].([]any); !ok {
		t.Fatalf("nodes = %s, want JSON array", encoded)
	}
	if _, ok := raw["edges"].([]any); !ok {
		t.Fatalf("edges = %s, want JSON array", encoded)
	}
}

func TestV2ReworkEdgeRequiresDeclaredScope(t *testing.T) {
	graph := Graph{
		SchemaVersion: 2,
		Nodes: []Node{
			{ID: "start", Type: "start", Label: "Start"},
			{ID: "draft", Type: "agent", Label: "Draft", AgentID: "agent-1", Instructions: "draft"},
			{ID: "review", Type: "human_review", Label: "Review", Assignee: &Assignee{Type: "member", ID: "member-1"}},
			{ID: "end", Type: "end", Label: "End"},
		},
		Edges: []Edge{
			{ID: "s-d", Source: "start", Target: "draft"},
			{ID: "d-r", Source: "draft", Target: "review"},
			{ID: "r-e", Source: "review", Target: "end"},
			{ID: "r-d", Source: "review", Target: "draft", Kind: "rework", ScopeID: "revision"},
		},
	}
	if err := Validate(graph, true); err == nil {
		t.Fatal("rework edge without a declared scope should be rejected")
	}
	graph.Scopes = []Scope{{ID: "revision", Type: "rework", EntryNodeID: "draft", ExitNodeID: "review", MaxReworks: 3}}
	if err := Validate(graph, true); err != nil {
		t.Fatalf("declared rework scope should validate: %v", err)
	}
}
