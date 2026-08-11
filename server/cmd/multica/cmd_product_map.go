package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// productMapCmd seeds the workspace product map (SY-20). Idempotent: upserts
// product nodes by (workspace_id, slug), refs by (product_id, ref_type,
// ref_id), editors by (product_id, user_id). Safe to re-run.
//
// Design decisions locked with the owner (2026-08-11):
//   - status_source is per-product configurable. Multica ships through its own
//     code repository (GitLab CI from main), so its live status source is
//     'code_repo' — evidence is the default-branch head. 院务系统's authority
//     is PMO 上线状态 ('pmo'); without real PMO data its status stays
//     pending_confirmation ("待确认") — Issue `done` alone never means live.
//   - 院务系统's Multica projects/issues are not in this workspace yet, so the
//     seed creates the node body only; traceability refs are backfilled when
//     the projects land.
var productMapCmd = &cobra.Command{
	Use:   "product-map-seed",
	Short: "Seed the workspace product map with 院务系统 + Multica nodes (idempotent)",
	RunE: func(cmd *cobra.Command, args []string) error {
		workspaceID, err := cmd.Flags().GetString("workspace-id")
		if err != nil || workspaceID == "" {
			return fmt.Errorf("--workspace-id is required")
		}
		editorUserID, err := cmd.Flags().GetString("editor-user-id")
		if err != nil || editorUserID == "" {
			return fmt.Errorf("--editor-user-id is required (凯撒/沙磊 user id)")
		}
		multicaProjectID, _ := cmd.Flags().GetString("multica-project-id")
		multicaIssueIDs, _ := cmd.Flags().GetStringSlice("multica-issue-id")

		dbURL := os.Getenv("DATABASE_URL")
		if dbURL == "" {
			dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		pool, err := pgxpool.New(ctx, dbURL)
		if err != nil {
			return fmt.Errorf("connect to database: %w", err)
		}
		defer pool.Close()
		if err := pool.Ping(ctx); err != nil {
			return fmt.Errorf("ping database: %w", err)
		}

		q := db.New(pool)
		wsUUID := util.MustParseUUID(workspaceID)
		editorUUID := util.MustParseUUID(editorUserID)

		// Multica node — status_source=code_repo. Evidence carries the repo +
		// default branch; the head SHA is filled by the operator/agent before
		// running (or left as the repo URL alone, which still qualifies as
		// code-repo evidence of the product's home).
		multicaEvidence := map[string]any{
			"source":         "code_repo",
			"repo_url":       "https://gitlab.sy.soyoung.com/fe/wasai/multica.git",
			"default_branch": "main",
			"checked_at":     time.Now().UTC().Format(time.RFC3339),
		}
		multicaNode, err := q.UpsertProductNode(ctx, db.UpsertProductNodeParams{
			WorkspaceID:  wsUUID,
			ParentID:     pgtypeUUIDNil(),
			Name:         "Multica",
			Slug:         "multica",
			Description:  "Multica 自身：AI 编码工作区平台，前端（Web + Desktop）与 Go 服务端 monorepo。",
			SortOrder:    1,
			Status:       "pending_confirmation",
			StatusSource: "code_repo",
			Evidence:     mustJSON(multicaEvidence),
		})
		if err != nil {
			return fmt.Errorf("upsert Multica node: %w", err)
		}
		slog.Info("seeded Multica node", "id", util.UUIDToString(multicaNode.ID))

		// 院务系统 node — status_source=pmo; no PMO data yet → 待确认.
		yuanwuNode, err := q.UpsertProductNode(ctx, db.UpsertProductNodeParams{
			WorkspaceID:  wsUUID,
			ParentID:     pgtypeUUIDNil(),
			Name:         "院务系统",
			Slug:         "yuanwu",
			Description:  "院务系统：试点产品（验收主试点）。项目与需求进入本工作区后回填追溯链接。",
			SortOrder:    2,
			Status:       "pending_confirmation",
			StatusSource: "pmo",
			Evidence:     mustJSON(map[string]any{"source": "pmo", "note": "PMO 上线状态待接入；Issue done 不作为上线依据"}),
		})
		if err != nil {
			return fmt.Errorf("upsert 院务系统 node: %w", err)
		}
		slog.Info("seeded 院务系统 node", "id", util.UUIDToString(yuanwuNode.ID))

		// 凯撒（沙磊）as first editor on both products.
		for _, node := range []db.ProductNode{multicaNode, yuanwuNode} {
			if _, err := q.UpsertProductEditor(ctx, db.UpsertProductEditorParams{
				ProductID: node.ID,
				UserID:    editorUUID,
			}); err != nil {
				return fmt.Errorf("register editor for %s: %w", node.Slug, err)
			}
		}
		slog.Info("registered editor", "user_id", editorUserID)

		// Multica traceability: project + issues, when provided.
		if multicaProjectID != "" {
			if _, err := q.UpsertProductRef(ctx, db.UpsertProductRefParams{
				ProductID: multicaNode.ID,
				RefType:   "project",
				RefID:     util.MustParseUUID(multicaProjectID),
			}); err != nil {
				return fmt.Errorf("link Multica project: %w", err)
			}
		}
		for _, id := range multicaIssueIDs {
			if _, err := q.UpsertProductRef(ctx, db.UpsertProductRefParams{
				ProductID: multicaNode.ID,
				RefType:   "issue",
				RefID:     util.MustParseUUID(id),
			}); err != nil {
				return fmt.Errorf("link Multica issue %s: %w", id, err)
			}
		}

		fmt.Printf("product map seeded: Multica=%s 院务系统=%s\n",
			util.UUIDToString(multicaNode.ID), util.UUIDToString(yuanwuNode.ID))
		return nil
	},
}

func init() {
	productMapCmd.Flags().String("workspace-id", "", "workspace UUID to seed into")
	productMapCmd.Flags().String("editor-user-id", "", "first product editor user UUID (凯撒/沙磊)")
	productMapCmd.Flags().String("multica-project-id", "", "Multica project UUID to link (optional)")
	productMapCmd.Flags().StringSlice("multica-issue-id", nil, "Multica issue UUIDs to link (repeatable, optional)")
	rootCmd.AddCommand(productMapCmd)
}

func pgtypeUUIDNil() pgtype.UUID {
	return pgtype.UUID{}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
