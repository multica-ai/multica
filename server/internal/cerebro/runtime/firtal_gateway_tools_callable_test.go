package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestFirtalGatewayCallableToolFamilies(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping gateway tool integration test")
	}

	ctx := context.Background()
	queries := db.New(runtimeAccountTestPool)
	cerebro := cerebrodb.New(runtimeAccountTestPool)

	var agentID pgtype.UUID
	if err := runtimeAccountTestPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, 'gateway-tool-family-test-agent', '', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb, '[]'::jsonb)
		RETURNING id
	`, runtimeAccountTestWSID, runtimeAccountTestRuntimeID, runtimeAccountTestUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = runtimeAccountTestPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	number, err := queries.IncrementIssueCounter(ctx, runtimeAccountTestWSID)
	if err != nil {
		t.Fatalf("increment issue counter: %v", err)
	}
	issue, err := queries.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: runtimeAccountTestWSID,
		Title:       "Gateway tool family test issue",
		Status:      "todo",
		Priority:    "medium",
		CreatorType: "agent",
		CreatorID:   agentID,
		Number:      number,
		Position:    0,
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = runtimeAccountTestPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issue.ID)
		_, _ = runtimeAccountTestPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	var artifactID pgtype.UUID
	if err := runtimeAccountTestPool.QueryRow(ctx, `
		INSERT INTO artifact (
			id, workspace_id, kind, format, title, body, metadata,
			author_type, author_id
		)
		VALUES (gen_random_uuid(), $1, 'note', 'md', 'Gateway artifact probe',
		        'Regression artifact body', '{}'::jsonb, 'agent', $2)
		RETURNING id
	`, runtimeAccountTestWSID, agentID).Scan(&artifactID); err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	t.Cleanup(func() {
		_ = queries.DeleteArtifact(context.Background(), db.DeleteArtifactParams{ID: artifactID, WorkspaceID: runtimeAccountTestWSID})
	})

	group, err := cerebro.CreateCerebroGroup(ctx, cerebrodb.CreateCerebroGroupParams{
		WorkspaceID: runtimeAccountTestWSID,
		Name:        "Gateway Tool Family Test Group",
		Description: pgtype.Text{String: "gateway tool family test", Valid: true},
		CreatedBy:   runtimeAccountTestUserID,
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	t.Cleanup(func() {
		_ = cerebro.DeleteCerebroGroup(context.Background(), group.ID)
	})

	grant, err := cerebro.CreateCerebroWorkspaceGrant(ctx, cerebrodb.CreateCerebroWorkspaceGrantParams{
		WorkspaceID:     runtimeAccountTestWSID,
		SubjectType:     "agent",
		SubjectID:       agentID,
		ResourcePattern: "workspace:*",
		Capability:      "read",
		GrantedByType:   pgtype.Text{String: "agent", Valid: true},
		GrantedByID:     agentID,
	})
	if err != nil {
		t.Fatalf("create grant: %v", err)
	}
	t.Cleanup(func() {
		_ = cerebro.DeleteCerebroWorkspaceGrant(context.Background(), cerebrodb.DeleteCerebroWorkspaceGrantParams{ID: grant.ID, WorkspaceID: runtimeAccountTestWSID})
	})

	credential, err := cerebro.CreateCerebroCredential(ctx, cerebrodb.CreateCerebroCredentialParams{
		WorkspaceID:    runtimeAccountTestWSID,
		Type:           "api_key",
		Name:           "Gateway Tool Family Test Credential",
		Description:    "gateway tool family test",
		EncryptedValue: []byte("ciphertext"),
		ValueHint:      "test...",
		Metadata:       []byte(`{}`),
		CreatedByType:  "agent",
		CreatedByID:    agentID,
	})
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	t.Cleanup(func() {
		_ = cerebro.DeleteCerebroCredential(context.Background(), credential.ID)
	})

	tctx := ToolContext{AgentID: agentID, WorkspaceID: runtimeAccountTestWSID}
	reg := NewDefaultRegistry(nil, queries, tctx, cerebro)
	t.Cleanup(func() {
		_, _ = runtimeAccountTestPool.Exec(context.Background(), `DELETE FROM project WHERE workspace_id = $1 AND title = 'Gateway Tool Family Test Project'`, runtimeAccountTestWSID)
	})

	cases := []struct {
		family string
		tool   string
		args   map[string]any
		want   string
	}{
		{family: "issue/comment", tool: "add_comment", args: map[string]any{"issue_id": util.UUIDToString(issue.ID), "content": "gateway family regression"}, want: "issue_id"},
		{family: "project", tool: "create_project", args: map[string]any{"title": "Gateway Tool Family Test Project"}, want: "Gateway Tool Family Test Project"},
		{family: "artifact", tool: "search_artifacts", args: map[string]any{"q": "Gateway artifact probe"}, want: "Gateway artifact probe"},
		{family: "group", tool: "list_groups", args: map[string]any{}, want: "Gateway Tool Family Test Group"},
		{family: "grant", tool: "get_grant", args: map[string]any{"grant_id": util.UUIDToString(grant.ID)}, want: "workspace:*"},
		{family: "credential", tool: "credential_list", args: map[string]any{}, want: "Gateway Tool Family Test Credential"},
		{family: "session/runtime", tool: "list_runtimes", args: map[string]any{}, want: runtimeAccountTestRuntimeN},
	}

	for _, tc := range cases {
		t.Run(tc.family+"/"+tc.tool, func(t *testing.T) {
			result, err := reg.Call(ctx, tc.tool, tc.args)
			if err != nil {
				t.Fatalf("%s call failed: %v", tc.tool, err)
			}
			if !json.Valid([]byte(result)) {
				t.Fatalf("%s returned invalid JSON: %s", tc.tool, result)
			}
			if !strings.Contains(result, tc.want) {
				t.Fatalf("%s result did not contain %q: %s", tc.tool, tc.want, result)
			}
		})
	}
}
