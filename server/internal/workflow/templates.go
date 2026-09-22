package workflow

import "fmt"

type TemplateBinding struct {
	Key      string `json:"key"`
	Kind     string `json:"kind"`
	NodeID   string `json:"node_id"`
	Required bool   `json:"required"`
}

type Template struct {
	ID          string            `json:"id"`
	Version     int               `json:"version"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Graph       Graph             `json:"graph"`
	Bindings    []TemplateBinding `json:"bindings"`
}

func templateGraph(nodes []Node, edges []Edge) Graph {
	graph := DefaultGraph()
	graph.Nodes = nodes
	graph.Edges = edges
	return graph
}

func builtInTemplates() []Template {
	return []Template{
		{
			ID:          "blank",
			Version:     1,
			Name:        "空白流程",
			Description: "从 Start 到 End 创建一个可编辑的工作流。",
			Graph:       DefaultGraph(),
			Bindings:    []TemplateBinding{},
		},
		{
			ID:          "content-review",
			Version:     1,
			Name:        "内容生成与人工审核",
			Description: "智能体生成草稿，成员审核通过或退回修改。",
			Graph: templateGraph(
				[]Node{
					{ID: "start", Type: "start", Label: "开始", Position: Position{X: 80, Y: 180}},
					{ID: "draft", Type: "agent", Label: "生成草稿", Position: Position{X: 360, Y: 180}, Instructions: "根据输入生成草稿。", Outputs: []OutputField{{Key: "draft", Label: "草稿", Type: "text", Required: true}}},
					{ID: "review", Type: "human_review", Label: "人工审核", Position: Position{X: 660, Y: 180}, Config: map[string]any{"decisions": []string{"approve", "rework"}}, Outputs: []OutputField{{Key: "final_text", Label: "最终内容", Type: "text", Required: true}}},
					{ID: "end", Type: "end", Label: "结束", Position: Position{X: 980, Y: 180}},
				},
				[]Edge{{ID: "e-start-draft", Source: "start", Target: "draft"}, {ID: "e-draft-review", Source: "draft", Target: "review"}, {ID: "e-review-end", Source: "review", Target: "end"}},
			),
			Bindings: []TemplateBinding{
				{Key: "draft.agent", Kind: "agent", NodeID: "draft", Required: true},
				{Key: "review.member", Kind: "member", NodeID: "review", Required: true},
			},
		},
		{
			ID:          "parallel-research",
			Version:     1,
			Name:        "并行资料分析与汇总",
			Description: "两路智能体并行分析，汇合后生成汇总。",
			Graph: templateGraph(
				[]Node{
					{ID: "start", Type: "start", Label: "开始", Position: Position{X: 80, Y: 180}},
					{ID: "research-a", Type: "agent", Label: "分析资料 A", Position: Position{X: 340, Y: 80}, Instructions: "分析输入中的第一类资料。"},
					{ID: "research-b", Type: "agent", Label: "分析资料 B", Position: Position{X: 340, Y: 300}, Instructions: "分析输入中的第二类资料。"},
					{ID: "merge", Type: "merge", Label: "等待全部完成", Position: Position{X: 620, Y: 180}},
					{ID: "summary", Type: "agent", Label: "生成汇总", Position: Position{X: 860, Y: 180}, Instructions: "汇总上游分析结果。"},
					{ID: "end", Type: "end", Label: "结束", Position: Position{X: 1140, Y: 180}},
				},
				[]Edge{{ID: "e-start-a", Source: "start", Target: "research-a", Kind: "flow"}, {ID: "e-start-b", Source: "start", Target: "research-b", Kind: "flow"}, {ID: "e-a-merge", Source: "research-a", Target: "merge", Kind: "flow"}, {ID: "e-b-merge", Source: "research-b", Target: "merge", Kind: "flow"}, {ID: "e-merge-summary", Source: "merge", Target: "summary", Kind: "flow"}, {ID: "e-summary-end", Source: "summary", Target: "end", Kind: "flow"}},
			),
			Bindings: []TemplateBinding{
				{Key: "research-a.agent", Kind: "agent", NodeID: "research-a", Required: true},
				{Key: "research-b.agent", Kind: "agent", NodeID: "research-b", Required: true},
				{Key: "summary.agent", Kind: "agent", NodeID: "summary", Required: true},
			},
		},
	}
}

func ListTemplates() []Template {
	templates := builtInTemplates()
	return templates
}

func GetTemplate(id string) (Template, error) {
	for _, item := range builtInTemplates() {
		if item.ID == id {
			return item, nil
		}
	}
	return Template{}, &Error{Status: 404, Message: fmt.Sprintf("workflow template %q not found", id), Code: "template_not_found"}
}
