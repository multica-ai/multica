package workflow

import "testing"

func TestSimulateRunAppliesFailureRoute(t *testing.T) {
	run := simulationFailureRun(map[string]any{"on_failure": "route"}, []Edge{
		{ID: "start-agent", Source: "start", Target: "agent"},
		{ID: "agent-fallback", Source: "agent", Target: "fallback", SourcePort: "failure"},
		{ID: "fallback-end", Source: "fallback", Target: "end"},
	})
	result := simulateRun(run, map[string]SimulationFixture{
		"agent":    {Action: "rework"},
		"fallback": {},
	})

	if result.Status != "succeeded" {
		t.Fatalf("simulation status = %q, want succeeded", result.Status)
	}
	if state := simulationNode(result, "agent"); state.Status != "failed" || state.ReasonCode != "failure_routed" {
		t.Fatalf("agent state = %#v, want routed failure", state)
	}
	if state := simulationNode(result, "fallback"); state.Status != "succeeded" {
		t.Fatalf("fallback state = %#v, want succeeded", state)
	}
}

func TestSimulateRunModelsFailureTakeoverAsHumanWait(t *testing.T) {
	run := simulationFailureRun(map[string]any{"on_failure": "takeover"}, []Edge{
		{ID: "start-agent", Source: "start", Target: "agent"},
		{ID: "agent-end", Source: "agent", Target: "end"},
	})
	result := simulateRun(run, map[string]SimulationFixture{
		"agent": {Action: "rework"},
	})

	if result.Status != "waiting" {
		t.Fatalf("simulation status = %q, want waiting", result.Status)
	}
	if state := simulationNode(result, "agent"); state.Status != "waiting_human" || state.ReasonCode != "awaiting_takeover" {
		t.Fatalf("agent state = %#v, want takeover wait", state)
	}
}

func simulationFailureRun(agentConfig map[string]any, edges []Edge) Run {
	graph := Graph{
		SchemaVersion: 2,
		Nodes: []Node{
			{ID: "start", Type: "start"},
			{ID: "agent", Type: "agent", Config: agentConfig},
			{ID: "fallback", Type: "agent"},
			{ID: "end", Type: "end"},
		},
		Edges: edges,
	}
	return Run{
		Graph:       graph,
		Input:       "{}",
		RootScopeID: "scope",
		Nodes: []NodeRun{
			{NodeID: "start", Status: "succeeded", Output: "{}"},
			{NodeID: "agent", Status: "pending"},
			{NodeID: "fallback", Status: "pending"},
			{NodeID: "end", Status: "pending"},
		},
	}
}

func simulationNode(run Run, nodeID string) NodeRun {
	for _, state := range run.Nodes {
		if state.NodeID == nodeID {
			return state
		}
	}
	return NodeRun{}
}
