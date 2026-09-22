package workflow

import "testing"

func inputTestGraph() Graph {
	return Graph{
		Nodes: []Node{
			{ID: "start", Type: "start", Config: map[string]any{"fields": []any{
				map[string]any{"key": "topic", "type": "text", "required": true, "max_length": float64(20)},
				map[string]any{"key": "count", "type": "integer"},
				map[string]any{"key": "enabled", "type": "boolean"},
			}}},
			{ID: "end", Type: "end"},
		},
		Edges: []Edge{{ID: "start-end", Source: "start", Target: "end"}},
	}
}

func TestValidateRunInputRequiresDeclaredFieldsAndTypes(t *testing.T) {
	graph := inputTestGraph()
	if err := validateRunInput(graph, `{"topic":"整理进展","count":2,"enabled":true}`); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	for name, input := range map[string]string{
		"missing required": `{"count":2}`,
		"wrong type":       `{"topic":"整理进展","count":2.5}`,
		"unknown field":    `{"topic":"整理进展","extra":"x"}`,
		"too long":         `{"topic":"这是一个超过二十个字符的输入内容，用于测试限制"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateRunInput(graph, input); err == nil {
				t.Fatal("invalid input was accepted")
			}
		})
	}
}

func TestValidateDraftInputFieldSchema(t *testing.T) {
	graph := inputTestGraph()
	graph.Nodes[0].Config["fields"] = []any{map[string]any{"key": "topic", "type": "unsupported"}}
	if err := Validate(graph, false); err == nil {
		t.Fatal("unsupported input type should be rejected even for a draft")
	}
}
