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
	// ignored for agents: they only ever write drafts.
	Publish bool
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

// Bases reports the workspace's knowledge bases and state.
func (s *KnowledgeService) Bases(ctx context.Context, workspaceID pgtype.UUID) (KnowledgeBasesView, error) {
	status, err := s.Status(ctx, workspaceID)
	if err != nil {
		return KnowledgeBasesView{}, err
	}
	return KnowledgeBasesView{Offered: status.Offered, State: status.State, KnowledgeBases: status.KnowledgeBases}, nil
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
	switch {
	case in.KB == "":
		return &KnowledgeInvalidError{"kb is required"}
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
	return nil
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
	if actor.Type == KnowledgeActorAgent {
		in.Publish = false
		if in.KB == DecisionsKnowledgeKey {
			return KnowledgeDocumentView{}, false, ErrKnowledgeForbidden
		}
	}
	ws, err := s.workspaceClient(ctx, workspaceID)
	if err != nil {
		return KnowledgeDocumentView{}, false, err
	}
	base, err := s.Queries.GetKnowledgeBaseByKey(ctx, db.GetKnowledgeBaseByKeyParams{WorkspaceID: workspaceID, Key: in.KB})
	if errors.Is(err, pgx.ErrNoRows) {
		return KnowledgeDocumentView{}, false, ErrKnowledgeBaseNotFound
	}
	if err != nil {
		return KnowledgeDocumentView{}, false, fmt.Errorf("load knowledge base: %w", err)
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

	settled, err := s.send(ctx, ws, base, link, in.Publish)
	if err != nil {
		// A person retries their own write; only the system's is kept.
		if actor.Type != KnowledgeActorSystem {
			if _, delErr := s.Queries.DeleteKnowledgeLink(ctx, db.DeleteKnowledgeLinkParams{ID: link.ID, WorkspaceID: workspaceID}); delErr != nil {
				slog.Warn("knowledge: could not drop the record of a failed write", "link_id", util.UUIDToString(link.ID), "error", delErr)
			}
		}
		return KnowledgeDocumentView{}, false, err
	}
	view, err := s.linkView(ctx, workspaceID, settled)
	return view, true, err
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
	switch {
	case link.SourceUrl != "" && !publish:
		return s.settle(ctx, link, "", KnowledgeLinkDraft, "")
	case link.SourceUrl != "":
		doc, err = ws.CreateURL(ctx, base.WeknoraKbID, knowledge.URLDocument{URL: link.SourceUrl, Title: link.Title})
	default:
		doc, err = ws.CreateManual(ctx, base.WeknoraKbID, knowledge.ManualDocument{Title: link.Title, Content: link.PendingContent, Publish: publish})
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
// write is retried with publishing on.
func (s *KnowledgeService) PublishDocument(ctx context.Context, workspaceID, linkID pgtype.UUID) (KnowledgeDocumentView, error) {
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
	return s.linkView(ctx, workspaceID, link)
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
	linked := map[string]bool{}
	for _, l := range links {
		if l.WeknoraKnowledgeID != "" {
			linked[util.UUIDToString(l.KnowledgeBaseID)+"/"+l.WeknoraKnowledgeID] = true
		}
		if state == "" || l.State == state {
			if _, ok := bases[util.UUIDToString(l.KnowledgeBaseID)]; ok {
				out.Documents = append(out.Documents, linkViewWith(bases, l))
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
func (s *KnowledgeService) Search(ctx context.Context, workspaceID pgtype.UUID, query, kb string, limit int) (KnowledgeSearchResult, error) {
	out := KnowledgeSearchResult{State: "ready", Hits: []KnowledgeHitView{}}
	query = strings.TrimSpace(query)
	if query == "" {
		return out, &KnowledgeInvalidError{"q is required"}
	}
	ws, err := s.workspaceClient(ctx, workspaceID)
	if err != nil {
		return out, err
	}
	bases, err := s.baseKeys(ctx, workspaceID)
	if err != nil {
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
	hits, err := ws.Search(ctx, knowledge.SearchRequest{Query: query, KnowledgeBaseIDs: ids, Limit: limit})
	if errors.Is(err, knowledge.ErrUnavailable) {
		out.State = KnowledgeStateOffline
		return out, nil
	}
	if err != nil {
		return out, err
	}
	for _, h := range hits {
		out.Hits = append(out.Hits, KnowledgeHitView{
			DocumentID: h.DocumentID, KnowledgeBase: byWeKnora[h.KnowledgeBaseID], Title: h.Title,
			Content: h.Content, Source: h.Source, Score: h.Score,
		})
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
		if _, err := s.send(ctx, ws, base, l, false); err != nil {
			slog.Warn("knowledge: retry of a failed write did not succeed", "link_id", util.UUIDToString(l.ID), "error", err)
			if errors.Is(err, knowledge.ErrUnavailable) {
				return
			}
		}
	}
}
