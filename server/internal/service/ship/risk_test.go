package ship

import (
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
)

// TestClassifyPullRequest exercises every tier of the rule-based
// classifier. The cases are intentionally narrow — one trigger per
// case — so a regression points at the responsible rule.
func TestClassifyPullRequest(t *testing.T) {
	tests := []struct {
		name        string
		inputs      RiskInputs
		wantLevel   db.RiskLevel
		wantReason  string // substring match against any reason
		wantNoBump  bool   // true if the level should remain medium with no reasons
	}{
		{
			name: "critical_dockerfile",
			inputs: RiskInputs{
				Title: "bump base image",
				Files: []gh.PullRequestFile{
					{Filename: "Dockerfile", Status: "modified"},
				},
			},
			wantLevel:  db.RiskLevelCritical,
			wantReason: "infra/k8s/dockerfile",
		},
		{
			name: "critical_destructive_migration",
			inputs: RiskInputs{
				Title: "drop legacy users column",
				Files: []gh.PullRequestFile{
					{
						Filename: "server/migrations/099_drop_legacy.up.sql",
						Status:   "added",
						Patch:    "+ALTER TABLE \"user\" DROP COLUMN legacy_blob;",
					},
				},
			},
			wantLevel:  db.RiskLevelCritical,
			wantReason: "DROP/DELETE",
		},
		{
			name: "critical_k8s_workflow",
			inputs: RiskInputs{
				Title: "switch CI runner",
				Files: []gh.PullRequestFile{
					{Filename: ".github/workflows/ci.yml", Status: "modified"},
				},
			},
			wantLevel:  db.RiskLevelCritical,
			wantReason: "infra",
		},
		{
			name: "high_migration_only",
			inputs: RiskInputs{
				Title: "add risk_level column",
				Files: []gh.PullRequestFile{
					{
						Filename: "server/migrations/083_ship_hub_phase_5.up.sql",
						Status:   "added",
						Patch:    "+ADD COLUMN risk_level risk_level NOT NULL DEFAULT 'medium';",
					},
				},
			},
			wantLevel:  db.RiskLevelHigh,
			wantReason: "migration file",
		},
		{
			name: "high_auth_handler",
			inputs: RiskInputs{
				Title: "fix login retry",
				Files: []gh.PullRequestFile{
					{Filename: "server/internal/handler/auth.go", Status: "modified"},
				},
			},
			wantLevel:  db.RiskLevelHigh,
			wantReason: "auth handler",
		},
		{
			name: "high_member_handler",
			inputs: RiskInputs{
				Title: "tighten role checks",
				Files: []gh.PullRequestFile{
					{Filename: "server/internal/handler/member.go", Status: "modified"},
				},
			},
			wantLevel:  db.RiskLevelHigh,
			wantReason: "member handler",
		},
		{
			name: "high_agent_runtime",
			inputs: RiskInputs{
				Title: "tweak runtime sweeper",
				Files: []gh.PullRequestFile{
					{Filename: "server/cmd/server/runtime_sweeper.go", Status: "modified"},
				},
			},
			wantLevel:  db.RiskLevelHigh,
			wantReason: "agent runtime",
		},
		{
			name: "high_breaking_title",
			inputs: RiskInputs{
				Title: "[breaking] rename foo to bar",
				Files: []gh.PullRequestFile{
					{Filename: "packages/core/foo.ts", Status: "modified"},
				},
			},
			wantLevel:  db.RiskLevelHigh,
			wantReason: "breaking",
		},
		{
			name: "low_docs_only",
			inputs: RiskInputs{
				Title: "fix typo",
				Files: []gh.PullRequestFile{
					{Filename: "docs/intro.md", Status: "modified"},
				},
			},
			wantLevel:  db.RiskLevelLow,
			wantReason: "tests / docs only",
		},
		{
			name: "low_tests_only",
			inputs: RiskInputs{
				Title: "add coverage for queries",
				Files: []gh.PullRequestFile{
					{Filename: "packages/core/ship/queries.test.ts", Status: "modified"},
					{Filename: "server/internal/handler/ship_test.go", Status: "modified"},
				},
			},
			wantLevel:  db.RiskLevelLow,
			wantReason: "tests / docs only",
		},
		{
			name: "medium_default_handler_change",
			inputs: RiskInputs{
				Title: "add new field to issue",
				Files: []gh.PullRequestFile{
					{Filename: "server/internal/handler/issue.go", Status: "modified"},
				},
			},
			wantLevel:  db.RiskLevelMedium,
			wantNoBump: true,
		},
		{
			name: "no_files_falls_back_to_title",
			inputs: RiskInputs{
				Title: "huge migration follow-up",
			},
			wantLevel:  db.RiskLevelHigh,
			wantReason: "migration",
		},
		{
			name: "destructive_in_non_added_line_skipped",
			inputs: RiskInputs{
				Title: "modify existing migration",
				// The DROP appears as context (no leading +), so the
				// classifier must NOT flag it.
				Files: []gh.PullRequestFile{
					{
						Filename: "server/migrations/079_init.up.sql",
						Status:   "modified",
						Patch:    " DROP TABLE old_thing;\n+ALTER TABLE new ADD COLUMN x int;",
					},
				},
			},
			wantLevel:  db.RiskLevelHigh, // still high (migration file), but not critical
			wantReason: "migration file",
		},
		{
			name: "highest_wins_with_mixed_triggers",
			inputs: RiskInputs{
				Title: "deploy migration + auth tweak",
				Files: []gh.PullRequestFile{
					{Filename: "Dockerfile", Status: "modified"},
					{Filename: "server/migrations/100_x.up.sql", Status: "added", Patch: "+CREATE TABLE x ();"},
					{Filename: "server/internal/handler/auth.go", Status: "modified"},
				},
			},
			wantLevel: db.RiskLevelCritical,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyPullRequest(tt.inputs)
			if result.Level != tt.wantLevel {
				t.Fatalf("level: got %s reasons=%v, want %s", result.Level, result.Reasons, tt.wantLevel)
			}
			if tt.wantNoBump {
				if len(result.Reasons) != 0 {
					t.Fatalf("expected no reasons, got %v", result.Reasons)
				}
				return
			}
			if tt.wantReason != "" {
				ok := false
				for _, r := range result.Reasons {
					if strings.Contains(r, tt.wantReason) {
						ok = true
						break
					}
				}
				if !ok {
					t.Fatalf("expected reason containing %q, got %v", tt.wantReason, result.Reasons)
				}
			}
		})
	}
}

// TestClassifyResult_ToJSON pins the wire shape — empty results emit
// "[]" not "null" so the downstream JSONB column doesn't store null
// (which would force the frontend to handle yet another shape).
func TestClassifyResult_ToJSON(t *testing.T) {
	cases := []struct {
		name string
		in   ClassifyResult
		want string
	}{
		{"empty", ClassifyResult{Level: db.RiskLevelMedium}, "[]"},
		{"one_reason", ClassifyResult{Level: db.RiskLevelHigh, Reasons: []string{"hi"}}, `["hi"]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := string(c.in.ToJSON())
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
