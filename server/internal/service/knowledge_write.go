package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The write path of the company knowledge base: drafts first, a person
// publishes. Every write leaves a knowledge_link row keyed by an idempotency
// key, so a retry never duplicates a document.
//
// The row is claimed before WeKnora is asked, in state "failed" with the
// pending content. A crash or an outage therefore leaves something a retry can
// finish, and a repeated key finds the row instead of writing again.

const (
	// KnowledgeLinkDraft, KnowledgeLinkPublished and KnowledgeLinkFailed
	// mirror knowledge_link.state.
	KnowledgeLinkDraft     = "draft"
	KnowledgeLinkPublished = "published"
	KnowledgeLinkFailed    = "failed"

	// KnowledgeActorAgent, KnowledgeActorMember and KnowledgeActorSystem mirror
	// knowledge_link.created_by_type.
	KnowledgeActorAgent  = "agent"
	KnowledgeActorMember = "member"
	KnowledgeActorSystem = "system"

	// DecisionsKnowledgeKey is the base only Multica writes to.
	DecisionsKnowledgeKey = "decisions"

	knowledgeMaxTitle   = 200
	knowledgeMaxContent = 1 << 20
	knowledgeMaxURL     = 2048
	// knowledgeSearchDefaultLimit is how many hits a search returns.
	knowledgeSearchDefaultLimit = 10
	// knowledgeListPageSize bounds the published documents listed per base.
	knowledgeListPageSize = 100
)

// knowledgeSourceTypes are the values knowledge_link.source_type accepts.
var knowledgeSourceTypes = map[string]bool{
	"issue": true, "comment": true, "proposal": true, "brief": true,
	"project": true, "upload": true, "url": true, "sync": true,
}

var (
	// ErrKnowledgeBaseNotFound: no live knowledge base has this key.
	ErrKnowledgeBaseNotFound = errors.New("knowledge base not found")
	// ErrKnowledgeDocumentNotFound: no document with this id in the workspace.
	ErrKnowledgeDocumentNotFound = errors.New("knowledge document not found")
	// ErrKnowledgeForbidden: the actor may not do this to this base.
	ErrKnowledgeForbidden = errors.New("this actor may not write to this knowledge base")
)

// KnowledgeNotReadyError means the workspace has no usable knowledge: it never
// enabled it, or provisioning has not finished. State is one of the
// Knowledge* state constants.
type KnowledgeNotReadyError struct{ State string }

func (e *KnowledgeNotReadyError) Error() string {
	return "knowledge is not ready for this workspace (" + e.State + ")"
}

// KnowledgeInvalidError is a request the caller can fix. Message is written
// for the caller.
type KnowledgeInvalidError struct{ Message string }

func (e *KnowledgeInvalidError) Error() string { return e.Message }

// KnowledgeConflictError is a request that does not fit the document's state.
type KnowledgeConflictError struct{ Message string }

func (e *KnowledgeConflictError) Error() string { return e.Message }

// KnowledgeActor is who writes. Type is one of the KnowledgeActor constants;
// ID is unset for the system.
type KnowledgeActor struct {
	Type string
	ID   pgtype.UUID
	// Curator is true for a human owner or admin, who may use restricted bases
	// that no grant names. The system actor is always a curator.
	Curator bool
}

// KnowledgeDocumentInput is a new document. Exactly one of Content and URL is
// set.
type KnowledgeDocumentInput struct {
	// KB is the knowledge base key, such as "market".
	KB      string
	Title   string
	Content string
	URL     string
	// SourceType defaults to "url" for a URL and "upload" otherwise.
	SourceType string
	SourceID   pgtype.UUID
	// IdempotencyKey makes a retry return the first write. Empty means every
	// call writes.
	IdempotencyKey string
	// Publish indexes the document at once instead of holding a draft. It is
	// ignored for agents: they only ever write drafts, unless the publish policy
	// of the kind or base says auto.
	Publish bool

	// Kind is a knowledge kind key. Empty writes an untyped document. A kind
	// with a default base lets KB be left empty.
	Kind string
	// InitiativeID and InitiativeFolder file the document in an initiative's
	// folder of the ventures base. The folder is named INI-<seq>-<slug>.
	InitiativeID     pgtype.UUID
	InitiativeFolder string
	StageKey         string
	// Confidence is low, medium or high, or empty.
	Confidence string
	// ValidUntil is the date the document stops being current.
	ValidUntil pgtype.Date
	// SupersedesLinkID is a document of this workspace that this one replaces.
	SupersedesLinkID pgtype.UUID
	// Tags are extra labels pushed to WeKnora with the kind and the folder.
	Tags []string
}

// KnowledgeDocumentView is one document as the API reports it. ID is the
// knowledge_link id and is empty for a document that exists only in WeKnora
// (uploaded or synced there).
type KnowledgeDocumentView struct {
	ID            string     `json:"id"`
	DocumentID    string     `json:"document_id"`
	KnowledgeBase string     `json:"knowledge_base"`
	Title         string     `json:"title"`
	State         string     `json:"state"`
	Kind          string     `json:"kind"`
	URL           string     `json:"url"`
	SourceType    string     `json:"source_type"`
	SourceID      *string    `json:"source_id"`
	CreatedByType string     `json:"created_by_type"`
	CreatedByID   *string    `json:"created_by_id"`
	CreatedAt     time.Time  `json:"created_at"`
	PublishedAt   *time.Time `json:"published_at"`
	Error         string     `json:"error"`

	// KnowledgeKind is the document's kind key, empty when it has none.
	KnowledgeKind string  `json:"knowledge_kind"`
	InitiativeID  *string `json:"initiative_id"`
	StageKey      string  `json:"stage_key"`
	Confidence    *string `json:"confidence"`
	// ValidUntil is a YYYY-MM-DD date.
	ValidUntil   *string `json:"valid_until"`
	SupersedesID *string `json:"supersedes_id"`
	// Superseded is true once a published document names this one as replaced.
	Superseded    bool     `json:"superseded"`
	Tags          []string `json:"tags"`
	ReviewVerdict string   `json:"review_verdict"`
}

// KnowledgeDocumentList is a document listing. State is "ready", or "offline"
// when WeKnora did not answer and only Multica's own records are listed.
type KnowledgeDocumentList struct {
	State     string                  `json:"state"`
	Documents []KnowledgeDocumentView `json:"documents"`
}

// KnowledgeHitView is one retrieved passage.
type KnowledgeHitView struct {
	DocumentID    string  `json:"document_id"`
	KnowledgeBase string  `json:"knowledge_base"`
	Title         string  `json:"title"`
	Content       string  `json:"content"`
	Source        string  `json:"source"`
	Score         float64 `json:"score"`
	// KnowledgeKind is the kind of the document, empty when it has none or
	// WeKnora holds it without a link.
	KnowledgeKind string `json:"knowledge_kind"`
}

// KnowledgeSearchResult is a search. State is "ready" or "offline"; hits are
// empty when offline. Retrieval always returns something, so the caller
// judges relevance by Score and content.
type KnowledgeSearchResult struct {
	State string             `json:"state"`
	Hits  []KnowledgeHitView `json:"hits"`
}

// KnowledgeBasesView is the knowledge bases a member may use and the state
// they are in. It carries no admin detail, so members may read it.
type KnowledgeBasesView struct {
	Offered        bool                `json:"offered"`
	State          string              `json:"state"`
	KnowledgeBases []KnowledgeBaseView `json:"knowledge_bases"`
}

// Bases reports the workspace's knowledge bases and state, as the system sees
// them: every base. Callers acting for a person or an agent use BasesFor.
func (s *KnowledgeService) Bases(ctx context.Context, workspaceID pgtype.UUID) (KnowledgeBasesView, error) {
	return s.BasesFor(ctx, workspaceID, KnowledgeActor{Type: KnowledgeActorSystem})
}

// BasesFor reports the knowledge bases the actor may read, and the state.
func (s *KnowledgeService) BasesFor(ctx context.Context, workspaceID pgtype.UUID, actor KnowledgeActor) (KnowledgeBasesView, error) {
	status, err := s.Status(ctx, workspaceID)
	if err != nil {
		return KnowledgeBasesView{}, err
	}
	grants, err := s.actorKnowledgeGrants(ctx, workspaceID, actor)
	if err != nil {
		return KnowledgeBasesView{}, err
	}
	visible := make([]KnowledgeBaseView, 0, len(status.KnowledgeBases))
	for _, b := range status.KnowledgeBases {
		if knowledgeCanRead(actor, b.Visibility, b.ID, grants) {
			visible = append(visible, b)
		}
	}
	return KnowledgeBasesView{Offered: status.Offered, State: status.State, KnowledgeBases: visible}, nil
}

// workspaceClient returns the tenant-scoped client of a workspace whose
// knowledge is ready.
func (s *KnowledgeService) workspaceClient(ctx context.Context, workspaceID pgtype.UUID) (knowledge.Workspace, error) {
	if ok, _ := s.offeredReason(); !ok {
		return nil, &KnowledgeNotReadyError{State: KnowledgeStateDisabled}
	}
	row, err := s.Queries.GetWorkspaceKnowledge(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &KnowledgeNotReadyError{State: KnowledgeStateDisabled}
	}
	if err != nil {
		return nil, fmt.Errorf("load workspace knowledge: %w", err)
	}
	if row.State != KnowledgeStateReady {
		return nil, &KnowledgeNotReadyError{State: row.State}
	}
	key, err := s.Box.Open(row.ApiKeyCiphertext)
	if err != nil {
		return nil, fmt.Errorf("open workspace knowledge key (was MULTICA_KNOWLEDGE_SECRET_KEY changed?): %w", err)
	}
	return s.Client.Workspace(string(key)), nil
}

// baseKeys maps knowledge base ids to their keys.
func (s *KnowledgeService) baseKeys(ctx context.Context, workspaceID pgtype.UUID) (map[string]db.KnowledgeBase, error) {
	bases, err := s.Queries.ListKnowledgeBases(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list knowledge bases: %w", err)
	}
	out := make(map[string]db.KnowledgeBase, len(bases))
	for _, b := range bases {
		out[util.UUIDToString(b.ID)] = b
	}
	return out, nil
}

func validateKnowledgeInput(in *KnowledgeDocumentInput) error {
	in.Title = strings.TrimSpace(in.Title)
	in.URL = strings.TrimSpace(in.URL)
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	in.Confidence = strings.ToLower(strings.TrimSpace(in.Confidence))
	in.StageKey = strings.TrimSpace(in.StageKey)
	in.InitiativeFolder = strings.TrimSpace(in.InitiativeFolder)
	switch {
	case in.KB == "" && in.Kind == "":
		return &KnowledgeInvalidError{"kb is required unless a kind with a default base is given"}
	case in.Kind != "" && !kindKeyPattern.MatchString(in.Kind):
		return &KnowledgeInvalidError{"kind must be a lowercase key such as market_report"}
	case in.Title == "":
		return &KnowledgeInvalidError{"title is required"}
	case utf8.RuneCountInString(in.Title) > knowledgeMaxTitle:
		return &KnowledgeInvalidError{fmt.Sprintf("title is longer than %d characters", knowledgeMaxTitle)}
	case (strings.TrimSpace(in.Content) == "") == (in.URL == ""):
		return &KnowledgeInvalidError{"send exactly one of content and url"}
	case len(in.Content) > knowledgeMaxContent:
		return &KnowledgeInvalidError{fmt.Sprintf("content is larger than %d bytes", knowledgeMaxContent)}
	}
	if in.URL != "" {
		u, err := url.Parse(in.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(in.URL) > knowledgeMaxURL {
			return &KnowledgeInvalidError{"url must be an http or https address"}
		}
	}
	if in.SourceType == "" {
		in.SourceType = "upload"
		if in.URL != "" {
			in.SourceType = "url"
		}
	}
	if !knowledgeSourceTypes[in.SourceType] {
		return &KnowledgeInvalidError{"unknown source_type " + in.SourceType}
	}
	switch in.Confidence {
	case "", "low", "medium", "high":
	default:
		return &KnowledgeInvalidError{"confidence must be low, medium or high"}
	}
	if utf8.RuneCountInString(in.StageKey) > 64 {
		return &KnowledgeInvalidError{"stage_key is longer than 64 characters"}
	}
	if in.InitiativeFolder != "" {
		if !initiativeFolderPattern.MatchString(in.InitiativeFolder) {
			return &KnowledgeInvalidError{"initiative_folder must look like INI-3-invoicing"}
		}
		if !in.InitiativeID.Valid {
			return &KnowledgeInvalidError{"initiative_folder needs initiative_id"}
		}
	}
	tags, err := normalizeKnowledgeTags(in.Tags)
	if err != nil {
		return err
	}
	in.Tags = tags
	return nil
}

// normalizeKnowledgeTags lowercases and deduplicates tags, which become WeKnora
// labels. At most ten, each at most 64 characters.
func normalizeKnowledgeTags(tags []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		if utf8.RuneCountInString(t) > 64 {
			return nil, &KnowledgeInvalidError{"a tag is longer than 64 characters"}
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > 10 {
		return nil, &KnowledgeInvalidError{"at most 10 tags"}
	}
	return out, nil
}

// CreateDocument writes a document. Agents always write a draft; a member or
// the system may publish at once. The bool reports whether this call wrote
// anything: false means the idempotency key had already been used and the
// existing document is returned.
//
// WeKnora being down is knowledge.ErrUnavailable. The write is then not
// recorded, unless the system made it: a system write stays as a failed row
// that RetryFailed finishes.
func (s *KnowledgeService) CreateDocument(ctx context.Context, workspaceID pgtype.UUID, actor KnowledgeActor, in KnowledgeDocumentInput) (KnowledgeDocumentView, bool, error) {
	if err := validateKnowledgeInput(&in); err != nil {
		return KnowledgeDocumentView{}, false, err
	}
	ws, err := s.workspaceClient(ctx, workspaceID)
	if err != nil {
		return KnowledgeDocumentView{}, false, err
	}
	if _, err := s.resolveWriteKind(ctx, workspaceID, &in); err != nil {
		return KnowledgeDocumentView{}, false, err
	}
	if in.InitiativeID.Valid && in.InitiativeFolder == "" {
		folder, err := s.initiativeFolder(ctx, workspaceID, in.InitiativeID)
		if err != nil {
			return KnowledgeDocumentView{}, false, err
		}
		in.InitiativeFolder = folder
	}
	if actor.Type == KnowledgeActorAgent {
		in.Publish = false
		if in.KB == DecisionsKnowledgeKey {
			return KnowledgeDocumentView{}, false, ErrKnowledgeForbidden
		}
	}
	if in.KB == "" {
		return KnowledgeDocumentView{}, false, &KnowledgeInvalidError{"kb is required; kind " + in.Kind + " has no default base"}
	}
	base, err := s.Queries.GetKnowledgeBaseByKey(ctx, db.GetKnowledgeBaseByKeyParams{WorkspaceID: workspaceID, Key: in.KB})
	if errors.Is(err, pgx.ErrNoRows) {
		return KnowledgeDocumentView{}, false, ErrKnowledgeBaseNotFound
	}
	if err != nil {
		return KnowledgeDocumentView{}, false, fmt.Errorf("load knowledge base: %w", err)
	}
	if in.SupersedesLinkID.Valid {
		if _, err := s.Queries.GetKnowledgeLink(ctx, db.GetKnowledgeLinkParams{ID: in.SupersedesLinkID, WorkspaceID: workspaceID}); errors.Is(err, pgx.ErrNoRows) {
			return KnowledgeDocumentView{}, false, &KnowledgeInvalidError{"supersedes_id is not a document of this workspace"}
		} else if err != nil {
			return KnowledgeDocumentView{}, false, fmt.Errorf("load superseded document: %w", err)
		}
	}

	// An agent's draft is published at once only when the policy says auto;
	// its author squad is recorded for the critic rule.
	publishNow := in.Publish
	var (
		mode     string
		authorSq pgtype.UUID
	)
	grants, err := s.actorKnowledgeGrants(ctx, workspaceID, actor)
	if err != nil {
		return KnowledgeDocumentView{}, false, err
	}
	if !knowledgeCanWrite(actor, base.Visibility, util.UUIDToString(base.ID), grants) {
		return KnowledgeDocumentView{}, false, ErrKnowledgeForbidden
	}
	if actor.Type == KnowledgeActorAgent {
		if mode, err = s.agentPublishMode(ctx, workspaceID, in.Kind, in.KB); err != nil {
			return KnowledgeDocumentView{}, false, err
		}
		publishNow = mode == PublishModeAuto
		if authorSq, err = s.agentSquad(ctx, actor); err != nil {
			return KnowledgeDocumentView{}, false, err
		}
	}
	if actor.Type == KnowledgeActorSystem {
		mode = PublishModeAuto
	}

	key := in.IdempotencyKey
	if key == "" {
		key = "auto:" + uuid.NewString()
	}
	claimed, err := s.Queries.ClaimKnowledgeLink(ctx, db.ClaimKnowledgeLinkParams{
		WorkspaceID: workspaceID, KnowledgeBaseID: base.ID,
		SourceType: in.SourceType, SourceID: in.SourceID, IdempotencyKey: key,
		CreatedByType: actor.Type, CreatedByID: actor.ID,
		Title: in.Title, SourceUrl: in.URL, PendingContent: in.Content, PublishRequested: in.Publish,
		Kind: in.Kind, InitiativeID: in.InitiativeID, StageKey: in.StageKey, SquadID: authorSq,
		Confidence: pgtype.Text{String: in.Confidence, Valid: in.Confidence != ""},
		ValidUntil: in.ValidUntil, SupersedesLinkID: in.SupersedesLinkID, Tags: documentTagNames(in),
	})
	if err != nil {
		return KnowledgeDocumentView{}, false, fmt.Errorf("record knowledge write: %w", err)
	}
	link, err := s.Queries.GetKnowledgeLinkByKey(ctx, db.GetKnowledgeLinkByKeyParams{WorkspaceID: workspaceID, IdempotencyKey: key})
	if err != nil {
		return KnowledgeDocumentView{}, false, fmt.Errorf("load knowledge write: %w", err)
	}
	if claimed == 0 && link.State != KnowledgeLinkFailed {
		// A retry of a write that already landed.
		view, err := s.linkView(ctx, workspaceID, link)
		return view, false, err
	}

	settled, err := s.send(ctx, ws, base, link, publishNow)
	if err != nil {
		// A person retries their own write; only the system's is kept.
		if actor.Type != KnowledgeActorSystem {
			if _, delErr := s.Queries.DeleteKnowledgeLink(ctx, db.DeleteKnowledgeLinkParams{ID: link.ID, WorkspaceID: workspaceID}); delErr != nil {
				slog.Warn("knowledge: could not drop the record of a failed write", "link_id", util.UUIDToString(link.ID), "error", delErr)
			}
		}
		return KnowledgeDocumentView{}, false, err
	}
	if settled.State == KnowledgeLinkPublished && mode == PublishModeAuto && actor.Type != KnowledgeActorMember {
		s.recordAutoPublish(ctx, AutoPublishEvent{
			WorkspaceID: workspaceID, LinkID: settled.ID, KnowledgeBase: base.Key, Kind: in.Kind,
			Mode: PublishModeAuto, AuthorSquadID: authorSq,
		})
	}
	s.linkInitiativeArtifact(ctx, settled, actor)
	view, err := s.linkView(ctx, workspaceID, settled)
	return view, true, err
}

// resolveWriteKind checks a write's kind: it must exist, the content must carry
// its required headings, and an empty base takes the kind's default. It returns
// the kind key ("" for an untyped write).
func (s *KnowledgeService) resolveWriteKind(ctx context.Context, workspaceID pgtype.UUID, in *KnowledgeDocumentInput) (string, error) {
	if in.Kind == "" {
		return "", nil
	}
	k, err := s.loadKnowledgeKind(ctx, workspaceID, in.Kind)
	if errors.Is(err, ErrKnowledgeKindNotFound) {
		return "", &KnowledgeInvalidError{"unknown kind " + in.Kind + "; see multica knowledge kinds"}
	}
	if err != nil {
		return "", err
	}
	if in.URL == "" {
		if missing := MissingSections(k.RequiredSections, in.Content); len(missing) > 0 {
			return "", &KnowledgeMissingSectionsError{Kind: in.Kind, Missing: missing}
		}
	}
	if in.KB == "" {
		in.KB = k.DefaultBaseKey
	}
	return in.Kind, nil
}

// agentSquad returns the squad of an agent actor, or the zero id when the agent
// is in none.
func (s *KnowledgeService) agentSquad(ctx context.Context, actor KnowledgeActor) (pgtype.UUID, error) {
	if !actor.ID.Valid {
		return pgtype.UUID{}, nil
	}
	sq, err := s.Queries.GetAgentSquadID(ctx, actor.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, nil
	}
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("load agent squad: %w", err)
	}
	return sq, nil
}

// documentTagNames is what a document carries into WeKnora: its kind, its
// initiative folder, and the caller's tags. Each is a WeKnora tag.
func documentTagNames(in KnowledgeDocumentInput) []string {
	names := make([]string, 0, len(in.Tags)+2)
	if in.Kind != "" {
		names = append(names, in.Kind)
	}
	if in.InitiativeFolder != "" {
		names = append(names, in.InitiativeFolder)
	}
	return append(names, in.Tags...)
}

// send performs the WeKnora side of a claimed link and records the outcome.
// publish is true to index the document now, in addition to the link's own
// publish_requested. A URL held as a draft makes no WeKnora call.
func (s *KnowledgeService) send(ctx context.Context, ws knowledge.Workspace, base db.KnowledgeBase, link db.KnowledgeLink, publish bool) (db.KnowledgeLink, error) {
	publish = publish || link.PublishRequested
	var (
		doc knowledge.Document
		err error
	)
	// Tags are created in WeKnora when the document first reaches it: a draft
	// has none, so an initiative folder appears with its first written document.
	switch {
	case link.SourceUrl != "" && !publish:
		return s.settle(ctx, link, "", KnowledgeLinkDraft, "")
	case link.SourceUrl != "":
		var tagIDs []string
		if tagIDs, err = s.weknoraTagIDs(ctx, ws, base.WeknoraKbID, link.Tags); err == nil {
			doc, err = ws.CreateURL(ctx, base.WeknoraKbID, knowledge.URLDocument{URL: link.SourceUrl, Title: link.Title, TagIDs: tagIDs})
		}
		if err == nil {
			err = s.checkImportedHeadings(ctx, ws, link, doc.ID)
		}
	default:
		var tagIDs []string
		if tagIDs, err = s.weknoraTagIDs(ctx, ws, base.WeknoraKbID, link.Tags); err == nil {
			doc, err = ws.CreateManual(ctx, base.WeknoraKbID, knowledge.ManualDocument{Title: link.Title, Content: link.PendingContent, Publish: publish, TagIDs: tagIDs})
		}
	}
	var dup *knowledge.DuplicateError
	if errors.As(err, &dup) {
		// WeKnora already holds this document; adopt it.
		doc, err = dup.Existing, nil
	}
	if err != nil {
		if _, settleErr := s.settle(ctx, link, link.WeknoraKnowledgeID, link.State, err.Error()); settleErr != nil {
			slog.Warn("knowledge: could not record a failed write", "link_id", util.UUIDToString(link.ID), "error", settleErr)
		}
		return link, err
	}
	state := KnowledgeLinkDraft
	if publish {
		state = KnowledgeLinkPublished
	}
	return s.settle(ctx, link, doc.ID, state, "")
}

// checkImportedHeadings applies the kind's required headings to a page WeKnora
// has just imported. A page without them is deleted from WeKnora again and the
// caller gets the missing list, the same refusal a written draft gets. A held
// URL draft makes no WeKnora call, so its headings are checked when it is
// published.
func (s *KnowledgeService) checkImportedHeadings(ctx context.Context, ws knowledge.Workspace, link db.KnowledgeLink, docID string) error {
	if link.Kind == "" {
		return nil
	}
	k, err := s.loadKnowledgeKind(ctx, link.WorkspaceID, link.Kind)
	if errors.Is(err, ErrKnowledgeKindNotFound) || (err == nil && len(k.RequiredSections) == 0) {
		return nil
	}
	if err != nil {
		return err
	}
	doc, err := ws.Read(ctx, docID)
	if err != nil {
		return fmt.Errorf("read imported page: %w", err)
	}
	missing := MissingSections(k.RequiredSections, doc.Content)
	if len(missing) == 0 {
		return nil
	}
	if err := ws.Delete(ctx, docID); err != nil && !errors.Is(err, knowledge.ErrNotFound) {
		slog.Warn("knowledge: could not remove an imported page that lacks its kind's headings", "document_id", docID, "error", err)
	}
	return &KnowledgeMissingSectionsError{Kind: link.Kind, Missing: missing}
}

// weknoraTagIDs finds or creates each named tag in the knowledge base.
func (s *KnowledgeService) weknoraTagIDs(ctx context.Context, ws knowledge.Workspace, kbID string, names []string) ([]string, error) {
	ids := make([]string, 0, len(names))
	for _, name := range names {
		id, err := ws.EnsureTag(ctx, kbID, name)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *KnowledgeService) settle(ctx context.Context, link db.KnowledgeLink, weknoraID, state, errText string) (db.KnowledgeLink, error) {
	return s.Queries.SettleKnowledgeLink(ctx, db.SettleKnowledgeLinkParams{
		ID: link.ID, WorkspaceID: link.WorkspaceID, WeknoraKnowledgeID: weknoraID, State: state, Error: errText,
	})
}

func (s *KnowledgeService) loadLink(ctx context.Context, workspaceID, id pgtype.UUID) (db.KnowledgeLink, error) {
	if ok, _ := s.offeredReason(); !ok {
		return db.KnowledgeLink{}, &KnowledgeNotReadyError{State: KnowledgeStateDisabled}
	}
	link, err := s.Queries.GetKnowledgeLink(ctx, db.GetKnowledgeLinkParams{ID: id, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.KnowledgeLink{}, ErrKnowledgeDocumentNotFound
	}
	if err != nil {
		return db.KnowledgeLink{}, fmt.Errorf("load knowledge document: %w", err)
	}
	return link, nil
}

// PublishDocument indexes a draft, making it searchable. A URL draft is
// fetched now. Publishing a published document returns it unchanged. A failed
// write is retried with publishing on. actor is who publishes, as the
// initiative timeline records it when the document takes an artifact.
func (s *KnowledgeService) PublishDocument(ctx context.Context, workspaceID, linkID pgtype.UUID, actor KnowledgeActor) (KnowledgeDocumentView, error) {
	link, err := s.loadLink(ctx, workspaceID, linkID)
	if err != nil {
		return KnowledgeDocumentView{}, err
	}
	if link.State == KnowledgeLinkPublished {
		return s.linkView(ctx, workspaceID, link)
	}
	ws, err := s.workspaceClient(ctx, workspaceID)
	if err != nil {
		return KnowledgeDocumentView{}, err
	}
	if link.WeknoraKnowledgeID != "" {
		doc, err := ws.Publish(ctx, link.WeknoraKnowledgeID)
		if err != nil {
			return KnowledgeDocumentView{}, err
		}
		link, err = s.settle(ctx, link, doc.ID, KnowledgeLinkPublished, "")
		if err != nil {
			return KnowledgeDocumentView{}, fmt.Errorf("record publish: %w", err)
		}
		s.linkInitiativeArtifact(ctx, link, actor)
		return s.linkView(ctx, workspaceID, link)
	}
	base, err := s.baseByID(ctx, workspaceID, link.KnowledgeBaseID)
	if err != nil {
		return KnowledgeDocumentView{}, err
	}
	link, err = s.send(ctx, ws, base, link, true)
	if err != nil {
		return KnowledgeDocumentView{}, err
	}
	s.linkInitiativeArtifact(ctx, link, actor)
	return s.linkView(ctx, workspaceID, link)
}

// linkInitiativeArtifact takes a published document into its initiative's
// checklist when it names one. A failure is logged and does not fail the
// publish: the document is already searchable.
func (s *KnowledgeService) linkInitiativeArtifact(ctx context.Context, link db.KnowledgeLink, actor KnowledgeActor) {
	if s.Initiatives == nil || link.State != KnowledgeLinkPublished || !link.InitiativeID.Valid || link.Kind == "" {
		return
	}
	if _, err := s.Initiatives.LinkArtifact(ctx, link.WorkspaceID, link.InitiativeID, link.ID, link.Kind, initiativeActorOf(actor)); err != nil {
		slog.Warn("knowledge: could not take the published document into its initiative",
			"link_id", util.UUIDToString(link.ID), "error", err)
	}
}

// initiativeActorOf is the actor a knowledge write is recorded as on an
// initiative's timeline. A retry the system makes is the system's.
func initiativeActorOf(a KnowledgeActor) InitiativeActor {
	switch a.Type {
	case KnowledgeActorMember:
		return InitiativeActor{Type: "member", ID: a.ID}
	case KnowledgeActorAgent:
		return InitiativeActor{Type: "agent", ID: a.ID}
	}
	return InitiativeActor{Type: "system"}
}

func (s *KnowledgeService) baseByID(ctx context.Context, workspaceID, id pgtype.UUID) (db.KnowledgeBase, error) {
	bases, err := s.baseKeys(ctx, workspaceID)
	if err != nil {
		return db.KnowledgeBase{}, err
	}
	b, ok := bases[util.UUIDToString(id)]
	if !ok {
		return db.KnowledgeBase{}, ErrKnowledgeBaseNotFound
	}
	return b, nil
}

// DiscardDocument deletes a draft or a failed write. A published document is
// removed with DeleteDocument, which only admins reach.
func (s *KnowledgeService) DiscardDocument(ctx context.Context, workspaceID, linkID pgtype.UUID) error {
	link, err := s.loadLink(ctx, workspaceID, linkID)
	if err != nil {
		return err
	}
	if link.State == KnowledgeLinkPublished {
		return &KnowledgeConflictError{"a published document cannot be discarded; delete it instead"}
	}
	return s.remove(ctx, workspaceID, link)
}

// DeleteDocument removes a document in any state from WeKnora and from
// Multica's records.
func (s *KnowledgeService) DeleteDocument(ctx context.Context, workspaceID, linkID pgtype.UUID) error {
	link, err := s.loadLink(ctx, workspaceID, linkID)
	if err != nil {
		return err
	}
	return s.remove(ctx, workspaceID, link)
}

func (s *KnowledgeService) remove(ctx context.Context, workspaceID pgtype.UUID, link db.KnowledgeLink) error {
	// A draft URL and a write WeKnora never accepted exist only here.
	if link.WeknoraKnowledgeID != "" {
		ws, err := s.workspaceClient(ctx, workspaceID)
		if err != nil {
			return err
		}
		if err := ws.Delete(ctx, link.WeknoraKnowledgeID); err != nil && !errors.Is(err, knowledge.ErrNotFound) {
			return err
		}
	}
	if _, err := s.Queries.DeleteKnowledgeLink(ctx, db.DeleteKnowledgeLinkParams{ID: link.ID, WorkspaceID: workspaceID}); err != nil {
		return fmt.Errorf("delete knowledge record: %w", err)
	}
	return nil
}

// ListDocuments lists a workspace's documents. kb is a key or empty for all;
// state is "draft", "published", "failed" or empty for all. Drafts and failed
// writes come from Multica's records; published documents come from WeKnora
// (so ones added there appear too), enriched with the records. When WeKnora
// does not answer, the list falls back to the records and says "offline".
func (s *KnowledgeService) ListDocuments(ctx context.Context, workspaceID pgtype.UUID, kb, state string) (KnowledgeDocumentList, error) {
	return s.ListDocumentsFor(ctx, workspaceID, KnowledgeActor{Type: KnowledgeActorSystem}, kb, state)
}

// ListDocumentsFor is ListDocuments over the bases the actor may read. A
// restricted base the actor may not read is reported as not found when named.
func (s *KnowledgeService) ListDocumentsFor(ctx context.Context, workspaceID pgtype.UUID, actor KnowledgeActor, kb, state string) (KnowledgeDocumentList, error) {
	out := KnowledgeDocumentList{State: "ready", Documents: []KnowledgeDocumentView{}}
	switch state {
	case "", KnowledgeLinkDraft, KnowledgeLinkPublished, KnowledgeLinkFailed:
	default:
		return out, &KnowledgeInvalidError{"state must be draft, published or failed"}
	}
	ws, err := s.workspaceClient(ctx, workspaceID)
	if err != nil {
		return out, err
	}
	bases, err := s.baseKeys(ctx, workspaceID)
	if err != nil {
		return out, err
	}
	if err := s.dropUnreadable(ctx, workspaceID, actor, bases); err != nil {
		return out, err
	}
	var only *db.KnowledgeBase
	if kb != "" {
		for _, b := range bases {
			if b.Key == kb {
				b := b
				only = &b
			}
		}
		if only == nil {
			return out, ErrKnowledgeBaseNotFound
		}
	}
	params := db.ListKnowledgeLinksParams{WorkspaceID: workspaceID}
	if only != nil {
		params.KnowledgeBaseID = only.ID
	}
	links, err := s.Queries.ListKnowledgeLinks(ctx, params)
	if err != nil {
		return out, fmt.Errorf("list knowledge records: %w", err)
	}
	superseded, err := s.Queries.ListSupersededLinkIDs(ctx, workspaceID)
	if err != nil {
		return out, fmt.Errorf("list superseded documents: %w", err)
	}
	supersededSet := map[string]bool{}
	for _, id := range superseded {
		supersededSet[util.UUIDToString(id)] = true
	}
	linked := map[string]bool{}
	for _, l := range links {
		if l.WeknoraKnowledgeID != "" {
			linked[util.UUIDToString(l.KnowledgeBaseID)+"/"+l.WeknoraKnowledgeID] = true
		}
		if state == "" || l.State == state {
			if _, ok := bases[util.UUIDToString(l.KnowledgeBaseID)]; ok {
				v := linkViewWith(bases, l)
				v.Superseded = supersededSet[v.ID]
				out.Documents = append(out.Documents, v)
			}
		}
	}
	if state != "" && state != KnowledgeLinkPublished {
		return out, nil
	}
	// Published documents WeKnora holds that Multica did not write.
	for _, b := range bases {
		if only != nil && b.ID != only.ID {
			continue
		}
		page, err := ws.ListDocuments(ctx, b.WeknoraKbID, knowledge.ListOptions{PageSize: knowledgeListPageSize})
		if errors.Is(err, knowledge.ErrUnavailable) {
			out.State = KnowledgeStateOffline
			return out, nil
		}
		if err != nil {
			return out, err
		}
		for _, d := range page.Items {
			if d.IsDraft() || linked[util.UUIDToString(b.ID)+"/"+d.ID] {
				continue
			}
			out.Documents = append(out.Documents, KnowledgeDocumentView{
				DocumentID: d.ID, KnowledgeBase: b.Key, Title: d.Title, State: KnowledgeLinkPublished,
				Kind: d.Type, URL: d.Source, CreatedAt: d.CreatedAt,
			})
		}
	}
	return out, nil
}

// Search is retrieval across the workspace's knowledge bases, or one of them
// (kb is its key). It calls no LLM. WeKnora not answering is the "offline"
// state with no hits, never an error.
// Search is retrieval across the workspace's knowledge bases, or one of them
// (kb is its key). It calls no LLM. WeKnora not answering is the "offline"
// state with no hits, never an error.
func (s *KnowledgeService) Search(ctx context.Context, workspaceID pgtype.UUID, query, kb string, limit int) (KnowledgeSearchResult, error) {
	return s.SearchKind(ctx, workspaceID, query, kb, "", limit)
}

// SearchKind is Search narrowed to documents of one kind (empty for all). A
// document superseded by a published one is never returned: the newer document
// replaces it, so the old one would only mislead.
func (s *KnowledgeService) SearchKind(ctx context.Context, workspaceID pgtype.UUID, query, kb, kind string, limit int) (KnowledgeSearchResult, error) {
	return s.SearchKindFor(ctx, workspaceID, KnowledgeActor{Type: KnowledgeActorSystem}, query, kb, kind, limit)
}

// SearchKindFor is SearchKind over the bases the actor may read. An agent with
// no grants searches the workspace-visible bases only, which is what automatic
// issue context uses.
func (s *KnowledgeService) SearchKindFor(ctx context.Context, workspaceID pgtype.UUID, actor KnowledgeActor, query, kb, kind string, limit int) (KnowledgeSearchResult, error) {
	out := KnowledgeSearchResult{State: "ready", Hits: []KnowledgeHitView{}}
	query = strings.TrimSpace(query)
	if query == "" {
		return out, &KnowledgeInvalidError{"q is required"}
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	ws, err := s.workspaceClient(ctx, workspaceID)
	if err != nil {
		return out, err
	}
	bases, err := s.baseKeys(ctx, workspaceID)
	if err != nil {
		return out, err
	}
	if err := s.dropUnreadable(ctx, workspaceID, actor, bases); err != nil {
		return out, err
	}
	byWeKnora := map[string]string{}
	var ids []string
	for _, b := range bases {
		if kb != "" && b.Key != kb {
			continue
		}
		byWeKnora[b.WeknoraKbID] = b.Key
		ids = append(ids, b.WeknoraKbID)
	}
	if len(ids) == 0 {
		if kb != "" {
			return out, ErrKnowledgeBaseNotFound
		}
		return out, nil
	}
	if limit <= 0 {
		limit = knowledgeSearchDefaultLimit
	}
	// Over-fetch: superseded documents and other kinds are dropped after the
	// search, and the caller still gets up to limit results.
	fetch := limit * 2
	if fetch > knowledgeSearchMaxFetch {
		fetch = knowledgeSearchMaxFetch
	}
	hits, err := ws.Search(ctx, knowledge.SearchRequest{Query: query, KnowledgeBaseIDs: ids, Limit: fetch})
	if errors.Is(err, knowledge.ErrUnavailable) {
		out.State = KnowledgeStateOffline
		return out, nil
	}
	if err != nil {
		return out, err
	}
	taxonomy, err := s.hitTaxonomy(ctx, workspaceID, hits)
	if err != nil {
		return out, err
	}
	for _, h := range hits {
		if len(out.Hits) >= limit {
			break
		}
		link, linked := taxonomy.byDocument[h.DocumentID]
		if linked && taxonomy.superseded[util.UUIDToString(link.ID)] {
			continue
		}
		if kind != "" && (!linked || link.Kind != kind) {
			continue
		}
		view := KnowledgeHitView{
			DocumentID: h.DocumentID, KnowledgeBase: byWeKnora[h.KnowledgeBaseID], Title: h.Title,
			Content: h.Content, Source: h.Source, Score: h.Score,
		}
		if linked {
			view.KnowledgeKind = link.Kind
		}
		out.Hits = append(out.Hits, view)
	}
	return out, nil
}

// knowledgeSearchMaxFetch caps the over-fetch of a filtered search.
const knowledgeSearchMaxFetch = 100

// hitTaxonomy holds what the registry knows about the documents of a search:
// their links by WeKnora id, and the links that a published document supersedes.
type hitTaxonomy struct {
	byDocument map[string]db.KnowledgeLink
	superseded map[string]bool
}

func (s *KnowledgeService) hitTaxonomy(ctx context.Context, workspaceID pgtype.UUID, hits []knowledge.SearchHit) (hitTaxonomy, error) {
	out := hitTaxonomy{byDocument: map[string]db.KnowledgeLink{}, superseded: map[string]bool{}}
	if len(hits) == 0 {
		return out, nil
	}
	docIDs := make([]string, 0, len(hits))
	for _, h := range hits {
		docIDs = append(docIDs, h.DocumentID)
	}
	links, err := s.Queries.GetKnowledgeLinksByDocumentIDs(ctx, db.GetKnowledgeLinksByDocumentIDsParams{WorkspaceID: workspaceID, DocumentIds: docIDs})
	if err != nil {
		return out, fmt.Errorf("load document taxonomy: %w", err)
	}
	for _, l := range links {
		out.byDocument[l.WeknoraKnowledgeID] = l
	}
	superseded, err := s.Queries.ListSupersededLinkIDs(ctx, workspaceID)
	if err != nil {
		return out, fmt.Errorf("load superseded documents: %w", err)
	}
	for _, id := range superseded {
		out.superseded[util.UUIDToString(id)] = true
	}
	return out, nil
}

func (s *KnowledgeService) linkView(ctx context.Context, workspaceID pgtype.UUID, l db.KnowledgeLink) (KnowledgeDocumentView, error) {
	bases, err := s.baseKeys(ctx, workspaceID)
	if err != nil {
		return KnowledgeDocumentView{}, err
	}
	return linkViewWith(bases, l), nil
}

func linkViewWith(bases map[string]db.KnowledgeBase, l db.KnowledgeLink) KnowledgeDocumentView {
	v := KnowledgeDocumentView{
		ID: util.UUIDToString(l.ID), DocumentID: l.WeknoraKnowledgeID,
		KnowledgeBase: bases[util.UUIDToString(l.KnowledgeBaseID)].Key,
		Title:         l.Title, State: l.State, Kind: "manual", URL: l.SourceUrl, SourceType: l.SourceType,
		CreatedByType: l.CreatedByType, CreatedAt: l.CreatedAt.Time, Error: l.Error,
	}
	if l.SourceUrl != "" {
		v.Kind = "url"
	}
	if l.SourceID.Valid {
		id := util.UUIDToString(l.SourceID)
		v.SourceID = &id
	}
	if l.CreatedByID.Valid {
		id := util.UUIDToString(l.CreatedByID)
		v.CreatedByID = &id
	}
	if l.PublishedAt.Valid {
		t := l.PublishedAt.Time
		v.PublishedAt = &t
	}
	v.KnowledgeKind = l.Kind
	v.StageKey = l.StageKey
	if l.InitiativeID.Valid {
		id := util.UUIDToString(l.InitiativeID)
		v.InitiativeID = &id
	}
	if l.Confidence.Valid {
		c := l.Confidence.String
		v.Confidence = &c
	}
	if l.ValidUntil.Valid {
		d := l.ValidUntil.Time.Format("2006-01-02")
		v.ValidUntil = &d
	}
	if l.SupersedesLinkID.Valid {
		id := util.UUIDToString(l.SupersedesLinkID)
		v.SupersedesID = &id
	}
	v.Tags = append([]string{}, l.Tags...)
	v.ReviewVerdict = l.ReviewVerdict
	return v
}

// RetryFailed finishes the system's writes WeKnora has not accepted yet, such
// as a decision record written while it was down. Errors are logged and left
// for the next call.
func (s *KnowledgeService) RetryFailed(ctx context.Context, workspaceID pgtype.UUID) {
	if s == nil {
		return
	}
	links, err := s.Queries.ListRetryableKnowledgeLinks(ctx, workspaceID)
	if err != nil || len(links) == 0 {
		if err != nil {
			slog.Warn("knowledge: list failed writes", "error", err)
		}
		return
	}
	ws, err := s.workspaceClient(ctx, workspaceID)
	if err != nil {
		return
	}
	for _, l := range links {
		base, err := s.baseByID(ctx, workspaceID, l.KnowledgeBaseID)
		if err != nil {
			continue
		}
		settled, err := s.send(ctx, ws, base, l, false)
		if err != nil {
			slog.Warn("knowledge: retry of a failed write did not succeed", "link_id", util.UUIDToString(l.ID), "error", err)
			if errors.Is(err, knowledge.ErrUnavailable) {
				return
			}
			continue
		}
		s.linkInitiativeArtifact(ctx, settled, KnowledgeActor{Type: KnowledgeActorSystem})
	}
}
