package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Publish policy (spec 3.3). A policy says what happens to an agent's draft:
// it waits for a person (manual, the default), it publishes once a critic from
// another squad attests it (auto_after_review), or it publishes at once (auto).
// Policies are keyed by knowledge base or by kind; a kind's policy wins over its
// base's. Member saves are not governed by policy: a member publishes by asking.
// System writers (decision records, brief archives) are auto by design.

const (
	PublishModeManual      = "manual"
	PublishModeAfterReview = "auto_after_review"
	PublishModeAuto        = "auto"
	PublishScopeBase       = "base"
	PublishScopeKind       = "kind"
	// KnowledgeVerdictOK and KnowledgeVerdictChanges are a critic's two verdicts.
	KnowledgeVerdictOK      = "ok"
	KnowledgeVerdictChanges = "changes"
)

var (
	// ErrKnowledgeNotCritic means a review did not come from an agent of a squad
	// other than the author's, so it cannot publish a document.
	ErrKnowledgeNotCritic = errors.New("a review must come from an agent in another squad")
	// ErrKnowledgePolicyNotFound means no policy row exists for the scope.
	ErrKnowledgePolicyNotFound = errors.New("knowledge publish policy not found")
)

// validPublishMode reports whether mode is one of the three policy modes.
func validPublishMode(mode string) bool {
	switch mode {
	case PublishModeManual, PublishModeAfterReview, PublishModeAuto:
		return true
	}
	return false
}

// PublishPolicyView is one policy row as the API reports it.
type PublishPolicyView struct {
	Scope     string    `json:"scope"`
	Key       string    `json:"key"`
	Mode      string    `json:"mode"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AutoPublishEvent records one publish Multica did without a person pressing
// publish. Every automatic publish produces one (spec 3.3).
type AutoPublishEvent struct {
	WorkspaceID   pgtype.UUID
	LinkID        pgtype.UUID
	KnowledgeBase string
	Kind          string
	Mode          string
	// ActorType is GovernanceActorPolicy for a policy-governed draft and
	// GovernanceActorSystem for a system writer.
	ActorType     string
	AuthorSquadID pgtype.UUID
	CriticSquadID pgtype.UUID
	OccurredAt    time.Time
}

// GovernanceRecorder receives the automatic publishes. When none is wired the
// governance log records them (NewAutoPublishGovernance). A failing recorder is
// logged and never fails the publish.
type GovernanceRecorder interface {
	RecordAutoPublish(ctx context.Context, ev AutoPublishEvent) error
}

// NoopGovernance records nothing. Tests use it where the log is not under test.
type NoopGovernance struct{}

// RecordAutoPublish implements GovernanceRecorder.
func (NoopGovernance) RecordAutoPublish(context.Context, AutoPublishEvent) error { return nil }

func (s *KnowledgeService) recordAutoPublish(ctx context.Context, ev AutoPublishEvent) {
	rec := s.Governance
	if rec == nil {
		rec = NewAutoPublishGovernance(s.Queries)
	}
	ev.OccurredAt = time.Now()
	if err := rec.RecordAutoPublish(ctx, ev); err != nil {
		slog.Warn("knowledge: governance record of an automatic publish failed", "link_id", util.UUIDToString(ev.LinkID), "error", err)
	}
}

// squadMayAutoPublish reports whether a squad's documents may be published
// without a person: the squad must be at autonomy level L2 or above (spec 4.3).
// A draft with no squad, or from a squad that no longer exists, waits for a
// person.
func (s *KnowledgeService) squadMayAutoPublish(ctx context.Context, ws, squad pgtype.UUID) (bool, error) {
	if !squad.Valid {
		return false, nil
	}
	level, err := s.Queries.GetSquadAutonomyLevel(ctx, db.GetSquadAutonomyLevelParams{ID: squad, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load squad autonomy level: %w", err)
	}
	return AutonomyAllowsAutoPublish(int(level)), nil
}

// autoPublishPaused reports whether an agent's automatic publish is refused
// because the company is paused (spec 4.5). The draft then waits for a person,
// as a manual draft does. Quiet hours do not refuse publishing.
func (s *KnowledgeService) autoPublishPaused(ctx context.Context, ws pgtype.UUID) (bool, error) {
	return CompanyIsPaused(ctx, s.Queries, ws)
}

// ListPublishPolicies returns the workspace's policy rows.
func (s *KnowledgeService) ListPublishPolicies(ctx context.Context, workspaceID pgtype.UUID) ([]PublishPolicyView, error) {
	rows, err := s.Queries.ListKnowledgePublishPolicies(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list publish policies: %w", err)
	}
	out := make([]PublishPolicyView, 0, len(rows))
	for _, r := range rows {
		out = append(out, PublishPolicyView{Scope: r.Scope, Key: r.ScopeKey, Mode: r.Mode, UpdatedAt: r.UpdatedAt.Time})
	}
	return out, nil
}

// SetPublishPolicy saves the policy of a base or a kind. The scope must exist:
// a base must be one of the workspace's knowledge bases, a kind one of its
// kinds.
func (s *KnowledgeService) SetPublishPolicy(ctx context.Context, workspaceID, actorID pgtype.UUID, scope, key, mode string) (PublishPolicyView, error) {
	if !validPublishMode(mode) {
		return PublishPolicyView{}, &KnowledgeInvalidError{"mode must be manual, auto_after_review or auto"}
	}
	switch scope {
	case PublishScopeBase:
		if _, err := s.baseByKey(ctx, workspaceID, key); err != nil {
			return PublishPolicyView{}, err
		}
	case PublishScopeKind:
		if _, err := s.loadKnowledgeKind(ctx, workspaceID, key); err != nil {
			return PublishPolicyView{}, err
		}
	default:
		return PublishPolicyView{}, &KnowledgeInvalidError{"scope must be base or kind"}
	}
	row, err := s.Queries.UpsertKnowledgePublishPolicy(ctx, db.UpsertKnowledgePublishPolicyParams{
		WorkspaceID: workspaceID, Scope: scope, ScopeKey: key, Mode: mode, UpdatedBy: actorID,
	})
	if err != nil {
		return PublishPolicyView{}, fmt.Errorf("save publish policy: %w", err)
	}
	return PublishPolicyView{Scope: row.Scope, Key: row.ScopeKey, Mode: row.Mode, UpdatedAt: row.UpdatedAt.Time}, nil
}

// DeletePublishPolicy removes a policy, so its scope falls back to the default.
func (s *KnowledgeService) DeletePublishPolicy(ctx context.Context, workspaceID pgtype.UUID, scope, key string) error {
	if scope != PublishScopeBase && scope != PublishScopeKind {
		return &KnowledgeInvalidError{"scope must be base or kind"}
	}
	n, err := s.Queries.DeleteKnowledgePublishPolicy(ctx, db.DeleteKnowledgePublishPolicyParams{WorkspaceID: workspaceID, Scope: scope, ScopeKey: key})
	if err != nil {
		return fmt.Errorf("delete publish policy: %w", err)
	}
	if n == 0 {
		return ErrKnowledgePolicyNotFound
	}
	return nil
}

// resolvePublishMode picks the mode for a document of kind in base: the kind's
// policy, then the base's, then the fallback. Kind and base may be empty.
func resolvePublishMode(policies []db.KnowledgePublishPolicy, kind, base, fallback string) string {
	byKind, byBase := "", ""
	for _, p := range policies {
		switch {
		case p.Scope == PublishScopeKind && p.ScopeKey == kind && kind != "":
			byKind = p.Mode
		case p.Scope == PublishScopeBase && p.ScopeKey == base && base != "":
			byBase = p.Mode
		}
	}
	switch {
	case byKind != "":
		return byKind
	case byBase != "":
		return byBase
	}
	return fallback
}

// agentPublishMode is the mode an agent's draft is governed by.
func (s *KnowledgeService) agentPublishMode(ctx context.Context, workspaceID pgtype.UUID, kind, base string) (string, error) {
	policies, err := s.Queries.ListKnowledgePublishPolicies(ctx, workspaceID)
	if err != nil {
		return "", fmt.Errorf("load publish policies: %w", err)
	}
	return resolvePublishMode(policies, kind, base, PublishModeManual), nil
}

// ReviewDocument records a critic's verdict on a draft. Only an agent of a squad
// other than the author's may review. An "ok" verdict publishes the draft when
// the policy for its kind or base is auto_after_review and the author's squad is
// allowed to publish automatically; otherwise the verdict waits for a person.
func (s *KnowledgeService) ReviewDocument(ctx context.Context, workspaceID, linkID pgtype.UUID, reviewer KnowledgeActor, verdict, note string) (KnowledgeDocumentView, error) {
	note = trimNote(note)
	switch verdict {
	case KnowledgeVerdictOK, KnowledgeVerdictChanges:
	default:
		return KnowledgeDocumentView{}, &KnowledgeInvalidError{"verdict must be ok or changes"}
	}
	link, err := s.loadLink(ctx, workspaceID, linkID)
	if err != nil {
		return KnowledgeDocumentView{}, err
	}
	switch link.State {
	case KnowledgeLinkDraft:
	case KnowledgeLinkPublished:
		return KnowledgeDocumentView{}, &KnowledgeConflictError{"the document is already published"}
	default:
		return KnowledgeDocumentView{}, &KnowledgeConflictError{"the document is not stored yet; retry the write first"}
	}
	if reviewer.Type != KnowledgeActorAgent || !reviewer.ID.Valid {
		return KnowledgeDocumentView{}, ErrKnowledgeNotCritic
	}
	criticSquad, err := s.Queries.GetAgentSquadID(ctx, reviewer.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return KnowledgeDocumentView{}, ErrKnowledgeNotCritic
	}
	if err != nil {
		return KnowledgeDocumentView{}, fmt.Errorf("load reviewer squad: %w", err)
	}
	author := link.SquadID
	if author.Valid && util.UUIDToString(author) == util.UUIDToString(criticSquad) {
		return KnowledgeDocumentView{}, ErrKnowledgeNotCritic
	}
	link, err = s.Queries.SetKnowledgeLinkReview(ctx, db.SetKnowledgeLinkReviewParams{
		ReviewVerdict: verdict, ReviewByID: reviewer.ID, ReviewSquadID: criticSquad, ReviewNote: note,
		ID: linkID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return KnowledgeDocumentView{}, fmt.Errorf("record review: %w", err)
	}
	if verdict != KnowledgeVerdictOK {
		return s.linkView(ctx, workspaceID, link)
	}
	base, err := s.baseByID(ctx, workspaceID, link.KnowledgeBaseID)
	if err != nil {
		return KnowledgeDocumentView{}, err
	}
	mode, err := s.agentPublishMode(ctx, workspaceID, link.Kind, base.Key)
	if err != nil {
		return KnowledgeDocumentView{}, err
	}
	if mode != PublishModeAfterReview {
		return s.linkView(ctx, workspaceID, link)
	}
	allowed, err := s.squadMayAutoPublish(ctx, workspaceID, author)
	if err != nil {
		return KnowledgeDocumentView{}, err
	}
	if !allowed {
		return s.linkView(ctx, workspaceID, link)
	}
	paused, err := s.autoPublishPaused(ctx, workspaceID)
	if err != nil {
		return KnowledgeDocumentView{}, err
	}
	if paused {
		return s.linkView(ctx, workspaceID, link)
	}
	view, err := s.PublishDocument(ctx, workspaceID, linkID, reviewer)
	if err != nil {
		return KnowledgeDocumentView{}, err
	}
	s.recordAutoPublish(ctx, AutoPublishEvent{
		WorkspaceID: workspaceID, LinkID: linkID, KnowledgeBase: base.Key, Kind: link.Kind,
		Mode: PublishModeAfterReview, ActorType: GovernanceActorPolicy, AuthorSquadID: author, CriticSquadID: criticSquad,
	})
	return view, nil
}

func trimNote(s string) string {
	const max = 500
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}
