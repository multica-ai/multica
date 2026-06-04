package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Sync is the GitHub Projects v2 connector for one workspace. It owns the
// inbound poll loop (project items -> Multica issues) and is the data
// surface the outbound Patcher uses to push local changes back.
type Sync struct {
	cfg     Config
	store   *Store
	client  *Client
	issues  *service.IssueService
	queries *db.Queries

	inst   Installation
	schema ProjectSchema
	log    *slog.Logger
}

// New builds a Sync. It does not touch the network or DB; call Bootstrap
// to resolve the board schema and persist the installation.
func New(cfg Config, pool *pgxpool.Pool, queries *db.Queries, issues *service.IssueService) *Sync {
	return &Sync{
		cfg:     cfg,
		store:   NewStore(pool),
		client:  NewClient(cfg.Token),
		issues:  issues,
		queries: queries,
		log:     slog.With("component", "github-sync", "project", fmt.Sprintf("%s/#%d", cfg.Org, cfg.ProjectNumber)),
	}
}

// Bootstrap resolves the board schema and upserts the installation row.
func (s *Sync) Bootstrap(ctx context.Context) error {
	schema, err := s.client.FetchProjectSchema(ctx, s.cfg.Org, s.cfg.ProjectNumber)
	if err != nil {
		return fmt.Errorf("fetch project schema: %w", err)
	}
	s.schema = schema
	fieldMap, _ := json.Marshal(schema)
	inst, err := s.store.EnsureInstallation(ctx, s.cfg.WorkspaceID, s.cfg.Org, s.cfg.ProjectNumber, schema.ProjectNodeID, fieldMap)
	if err != nil {
		return err
	}
	s.inst = inst
	s.log.Info("github installation ready", "installation_id", inst.ID, "board", schema.Title, "workspace_id", s.cfg.WorkspaceID)
	return nil
}

// Run does an initial import, then polls on cfg.PollInterval until ctx is
// cancelled. Each cycle is best-effort; transient errors are logged and
// retried on the next tick.
func (s *Sync) Run(ctx context.Context) {
	if err := s.Bootstrap(ctx); err != nil {
		s.log.Error("github sync bootstrap failed; connector idle", "error", err)
		return
	}
	if n, err := s.ImportAll(ctx); err != nil {
		s.log.Error("initial import failed", "error", err)
	} else {
		s.log.Info("initial import complete", "items", n)
	}
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.log.Info("github sync stopping")
			return
		case <-ticker.C:
			if _, err := s.ImportAll(ctx); err != nil {
				s.log.Warn("poll import failed", "error", err)
			}
		}
	}
}

// ImportAll pulls every board item and reconciles it into the Multica
// workspace. Returns the number of items processed.
func (s *Sync) ImportAll(ctx context.Context) (int, error) {
	items, err := s.client.FetchItems(ctx, s.inst.ProjectNodeID)
	if err != nil {
		_ = s.store.MarkSynced(ctx, s.inst.ID, err.Error())
		return 0, err
	}

	creatorID, err := s.store.AnyMemberUserID(ctx, s.cfg.WorkspaceID)
	if err != nil {
		return 0, err
	}

	// key: "repo#number" -> multica issue id, for parent linking (pass 2).
	byNumber := map[string]string{}

	for _, it := range items {
		if it.Title == "" {
			continue // skip untitled draft rows
		}
		issueID, err := s.upsertIssue(ctx, it, creatorID)
		if err != nil {
			s.log.Warn("upsert issue failed", "item", it.ItemID, "title", it.Title, "error", err)
			continue
		}
		if it.Repo != "" && it.Number > 0 {
			byNumber[fmt.Sprintf("%s#%d", it.Repo, it.Number)] = issueID
		}
	}

	// Pass 2: link parents (Intents -> tasks per parent-features-and-sub-tasks.md).
	linked := 0
	for _, it := range items {
		if it.ParentNumber == 0 || it.Repo == "" {
			continue
		}
		parentID, ok := byNumber[fmt.Sprintf("%s#%d", it.Repo, it.ParentNumber)]
		if !ok {
			continue // parent not on this board
		}
		childID := byNumber[fmt.Sprintf("%s#%d", it.Repo, it.Number)]
		if childID == "" || childID == parentID {
			continue
		}
		if err := s.store.SetIssueParent(ctx, childID, parentID, s.cfg.WorkspaceID); err != nil {
			s.log.Warn("link parent failed", "child", childID, "parent", parentID, "error", err)
			continue
		}
		linked++
	}

	_ = s.store.MarkSynced(ctx, s.inst.ID, "")
	s.log.Debug("import cycle done", "items", len(items), "parents_linked", linked)
	return len(items), nil
}

// upsertIssue creates a new Multica issue for an unseen item, or reconciles
// status + metadata for one already bound. Returns the Multica issue id.
func (s *Sync) upsertIssue(ctx context.Context, it Item, creatorUserID string) (string, error) {
	wsUUID, err := util.ParseUUID(s.cfg.WorkspaceID)
	if err != nil {
		return "", err
	}
	mappedStatus := MapStatusToMultica(it.Fields["Status"])
	mappedPriority := MapPriorityToMultica(it.Fields["Priority"])

	existing, err := s.store.GetBindingByItem(ctx, s.inst.ID, it.ItemID)
	if err != nil {
		return "", err
	}

	if existing != nil {
		// Reconcile remote -> local. Loop guard: if the remote status only
		// echoes what we last pushed out, leave the local side alone.
		issueUUID, err := util.ParseUUID(existing.MulticaIssueID)
		if err != nil {
			return "", err
		}
		remoteStatus := it.Fields["Status"]
		if remoteStatus != existing.LastPushedStatus {
			cur, err := s.queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueUUID, WorkspaceID: wsUUID})
			if err == nil && cur.Status != mappedStatus {
				if _, err := s.queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
					ID: issueUUID, Status: mappedStatus, WorkspaceID: wsUUID,
				}); err != nil {
					s.log.Warn("status reconcile failed", "issue", existing.MulticaIssueID, "error", err)
				}
			}
		}
		s.writeMetadata(ctx, issueUUID, wsUUID, it)
		// Refresh binding identity (repo/number/content may have populated
		// after a draft was converted to a real issue).
		_ = s.store.UpsertBinding(ctx, mergeBinding(*existing, s.inst, it, remoteStatus))
		return existing.MulticaIssueID, nil
	}

	// New item -> create a Multica issue. OriginType is intentionally left
	// unset: the issue.origin_type CHECK only permits autopilot/quick_create/
	// lark_chat, and the GitHub linkage lives in github_issue_binding +
	// metadata instead.
	creatorUUID, err := util.ParseUUID(creatorUserID)
	if err != nil {
		return "", err
	}
	res, err := s.issues.Create(ctx, service.IssueCreateParams{
		WorkspaceID:    wsUUID,
		Title:          it.Title,
		Description:    pgtype.Text{String: it.Body, Valid: it.Body != ""},
		Status:         mappedStatus,
		Priority:       mappedPriority,
		CreatorType:    "member",
		CreatorID:      creatorUUID,
		AllowDuplicate: true,
	}, service.IssueCreateOpts{Platform: "github"})
	if err != nil {
		return "", fmt.Errorf("create issue: %w", err)
	}
	issue := res.Issue
	s.writeMetadata(ctx, issue.ID, wsUUID, it)

	b := Binding{
		InstallationID:    s.inst.ID,
		WorkspaceID:       s.cfg.WorkspaceID,
		MulticaIssueID:    uuidString(issue.ID),
		GitHubItemID:      it.ItemID,
		GitHubContentID:   it.ContentID,
		GitHubRepo:        it.Repo,
		GitHubIssueNumber: it.Number,
		RemoteStatus:      it.Fields["Status"],
	}
	if err := s.store.UpsertBinding(ctx, b); err != nil {
		return "", fmt.Errorf("record binding: %w", err)
	}
	return b.MulticaIssueID, nil
}

// writeMetadata stamps the GitHub + workflow taxonomy onto the issue.
func (s *Sync) writeMetadata(ctx context.Context, issueID, wsID pgtype.UUID, it Item) {
	for k, v := range MetadataFor(it) {
		val, _ := json.Marshal(v) // metadata values are JSON-encoded primitives
		if _, err := s.queries.SetIssueMetadataKey(ctx, db.SetIssueMetadataKeyParams{
			Key: k, Value: val, ID: issueID, WorkspaceID: wsID,
		}); err != nil {
			s.log.Debug("set metadata failed", "key", k, "error", err)
		}
	}
}

func mergeBinding(b Binding, inst Installation, it Item, remoteStatus string) Binding {
	b.InstallationID = inst.ID
	b.GitHubItemID = it.ItemID
	if it.ContentID != "" {
		b.GitHubContentID = it.ContentID
	}
	if it.Repo != "" {
		b.GitHubRepo = it.Repo
	}
	if it.Number > 0 {
		b.GitHubIssueNumber = it.Number
	}
	b.RemoteStatus = remoteStatus
	return b
}

func uuidString(u pgtype.UUID) string {
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
