package detsteps

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/dettools"
)

func TestRootDettoolSourcesRun(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
	}{
		{
			name: "pipeline_state_parse.go",
			input: map[string]any{
				"description": "```json\n{\"stage\":\"implementing\",\"tasks\":[]}\n```",
			},
		},
		{
			name: "review_scope_partition.go",
			input: map[string]any{
				"files": []any{"src/app.py", "Service/Foo.csproj", ".github/workflows/ci.yml", "README.md"},
			},
		},
		{
			name: "ado_payload_normalize.go",
			input: map[string]any{
				"work_item": map[string]any{
					"id": float64(123),
					"fields": map[string]any{
						"System.WorkItemType":                      "Task",
						"System.Title":                             "Implement thing",
						"System.Description":                       "<p>Hello</p>",
						"Microsoft.VSTS.Common.AcceptanceCriteria": "<ul><li>Works</li></ul>",
						"System.State":                             "Active",
						"System.AreaPath":                          "Area",
						"System.IterationPath":                     "Iteration",
					},
				},
			},
		},
		{
			name: "coding_watchdog_analyze.go",
			input: map[string]any{
				"master_issue_id": "M-1",
				"state": map[string]any{
					"tasks": []any{
						map[string]any{"task_issue_id": "T-1", "status": "planned"},
					},
				},
				"task_comments": map[string]any{
					"T-1": []any{
						map[string]any{
							"content":    "## Implementation Plan\nPlan body",
							"created_at": "2026-06-09T10:00:00Z",
						},
					},
				},
				"master_comments": []any{},
				"agent_ids":       map[string]any{"implementer": "agent-impl"},
				"now":             "2026-06-09T10:06:00Z",
			},
		},
		{
			name: "coding_comment_extract.go",
			input: map[string]any{
				"comments": []any{
					map[string]any{"content": "## Implementation Plan\n```json coding-team-artifact\n{\"artifact_type\":\"implementation_plan\",\"artifact_version\":1,\"task_issue_id\":\"T-1\",\"master_issue_id\":\"M-1\",\"language\":\"csharp\",\"owning_project\":\"src/App\",\"owning_project_justification\":\"Existing owner\",\"files_to_create\":[],\"files_to_modify\":[\"src/App/Foo.cs\"],\"acceptance_criteria_coverage\":[{\"criterion\":\"Works\",\"planned_coverage\":\"Unit test\"}],\"key_decisions\":[\"Use existing service\"]}\n```"},
				},
			},
		},
		{
			name: "coding_plan_validate.go",
			input: map[string]any{
				"acceptance_criteria": []any{"Works"},
				"plan": map[string]any{
					"artifact_type":                "implementation_plan",
					"artifact_version":             float64(1),
					"task_issue_id":                "T-1",
					"master_issue_id":              "M-1",
					"language":                     "csharp",
					"owning_project":               "src/App",
					"owning_project_justification": "Existing owner",
					"files_to_create":              []any{},
					"files_to_modify":              []any{"src/App/Foo.cs"},
					"acceptance_criteria_coverage": []any{map[string]any{"criterion": "Works", "planned_coverage": "Unit test"}},
					"key_decisions":                []any{"Use existing service"},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := readRootDettoolSource(t, tc.name)
			res := Run(context.Background(), source, tc.input, Options{Timeout: 3 * time.Second})
			if res.Status != dettools.StatusOK {
				t.Fatalf("status = %q, code = %q, summary = %q", res.Status, res.ErrorCode, res.Summary)
			}
			if res.MachineData == nil {
				t.Fatal("machine data is nil")
			}
		})
	}
}

func readRootDettoolSource(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "dettools", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
