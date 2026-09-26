package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Default-assignee routing for issue creation (RIC-1024).
//
// Core-development, architecture, and security/complex work defaults to the
// workspace's "core squad" instead of a single agent; pure ops, scheduled
// inspection, and light tasks stay unassigned so the single-agent fast track
// keeps working. The routing only fires when the caller did not name an
// assignee — an explicit `--assignee` / `--assignee-id` always wins.

// coreSquadFallbackName is the conventional name resolved when no explicit
// setting is present. It matches the squad provisioned for the P-G-E
// (Planner-Generator-Evaluator) development flow.
const coreSquadFallbackName = "pge-core-squad"

// coreTaskMarkers, strongOpsMarkers and softOpsMarkers are conservative
// substring sets used to classify a title+description. Matching is
// lowercase-normalized so Chinese and English phrasing both work, and the
// three tiers order the precedence:
//
//   - strongOpsMarkers always win: a task that literally performs inspection /
//     monitoring / backup / cleanup / health / report is operational no matter
//     what subject it mentions, so it never funnels into the squad.
//   - coreTaskMarkers classify development, architecture/refactor, security,
//     backend/frontend and complexity-significant work as core.
//   - softOpsMarkers (deploy, ops, sync, cron, channel) only classify when no
//     core marker is present — "deploy the new API" is a core task whose deploy
//     verb is incidental, while "deploy to production" alone is ops.
var (
	coreTaskMarkers = []string{
		// English
		"feature", "architecture", "architect", "refactor", "refactoring",
		"security", "implement", "implementation", "backend", "frontend", "api",
		"database", "migration", "protocol", "sdk", "vulnerability", "ssrf",
		// Chinese
		"核心", "研发", "开发", "架构", "重构", "安全", "实现", "复杂", "功能",
	}
	strongOpsMarkers = []string{
		// English — pure operational activity, subject-independent.
		"inspection", "inspect", "monitor", "monitoring", "backup", "cleanup",
		"health check", "restart", "rollback", "report",
		// Chinese
		"巡检", "监控", "备份", "清理", "健康检查", "报告", "通知",
	}
	softOpsMarkers = []string{
		// English — operational activity that a core context can override.
		"deploy", "ops", "operation", "sync", "cron", "channel",
		// Chinese
		"部署", "运维", "同步",
	}
)

// IsCoreTask reports whether the title+description read as core-development,
// architecture, security, or complexity-significant work that should default
// to a squad. Strong ops markers always win (inspection / monitoring / backup
// / cleanup / health / report stay on the single-agent fast track even when a
// core word appears alongside — "security inspection" is an inspection, not a
// fix). Soft ops markers (deploy, ops, sync) classify only when no core marker
// is present, so "deploy the new API" is core while "deploy to production" is
// ops. Anything unclassified stays fast track rather than guessed.
func IsCoreTask(title, description string) bool {
	haystack := strings.ToLower(title + " " + description)
	for _, m := range strongOpsMarkers {
		if strings.Contains(haystack, m) {
			return false
		}
	}
	core := false
	for _, m := range coreTaskMarkers {
		if strings.Contains(haystack, m) {
			core = true
			break
		}
	}
	if core {
		return true
	}
	for _, m := range softOpsMarkers {
		if strings.Contains(haystack, m) {
			return false
		}
	}
	return false
}

// workspaceSettingsCoreSquadID reads the configured core squad id out of the
// workspace settings JSONB. The empty string means the workspace has not
// opted in via settings.
func workspaceSettingsCoreSquadID(settings []byte) string {
	if len(settings) == 0 {
		return ""
	}
	var s struct {
		DefaultCoreSquadID string `json:"default_core_squad_id"`
	}
	if err := json.Unmarshal(settings, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s.DefaultCoreSquadID)
}

// ResolveCoreSquadID returns the workspace's core squad id when one can be
// determined: the settings value if present and existing, else a squad named
// pge-core-squad. The boolean reports whether a squad was resolved.
func ResolveCoreSquadID(ctx context.Context, q interface {
	ListSquads(ctx context.Context, workspaceID pgtype.UUID) ([]db.Squad, error)
	GetSquadInWorkspace(ctx context.Context, arg db.GetSquadInWorkspaceParams) (db.Squad, error)
}, workspaceID pgtype.UUID, settings []byte) (pgtype.UUID, bool) {
	if configured := workspaceSettingsCoreSquadID(settings); configured != "" {
		if id, err := util.ParseUUID(configured); err == nil {
			if _, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
				ID:          id,
				WorkspaceID: workspaceID,
			}); err == nil {
				return id, true
			}
		}
	}
	squads, err := q.ListSquads(ctx, workspaceID)
	if err != nil {
		return pgtype.UUID{}, false
	}
	for _, s := range squads {
		if strings.EqualFold(strings.TrimSpace(s.Name), coreSquadFallbackName) {
			return s.ID, true
		}
	}
	return pgtype.UUID{}, false
}
