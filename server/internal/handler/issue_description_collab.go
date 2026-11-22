package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/collab"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Live co-editing of issue descriptions (MUL-9778) runs on the same
// collaboration service as docs (doc_collab.go), under the document name
// "issue:<id>". The description stays an ordinary issue field: every live
// save goes through UpdateIssue, so activity, mention notifications and
// subscriptions follow it as they follow any other description change.

const (
	// Live saves come every 5 to 30 seconds while someone types. A writer
	// whose base is more than this many description revisions old gets a
	// revision conflict instead of a merge.
	issueDescriptionRevisionsKept   = 50
	issueDescriptionMaxBytes        = 2 << 20
	issueDescriptionSeenMentionsMax = 1000
)

// issueCollab is the collaboration service for issue descriptions: nil
// unless it is configured and the issue_description_live flag is on. The
// internal endpoints it calls back are not gated, so sessions open when the
// flag goes off still save.
func (h *Handler) issueCollab(ctx context.Context) *collab.Client {
	if h.Collab == nil || !featureflags.IssueDescriptionLiveEnabled(ctx, h.FeatureFlags) {
		return nil
	}
	return h.Collab
}

// recordIssueDescriptionRevision keeps the description of recent revisions,
// the merge base for a write that started from one of them. The revision
// being replaced is recorded too: an issue's first description, and one
// written by a path that keeps no history, has no row of its own.
func recordIssueDescriptionRevision(ctx context.Context, qtx *db.Queries, prev, issue db.Issue) error {
	for _, rev := range []db.Issue{prev, issue} {
		if err := qtx.InsertIssueDescriptionRevision(ctx, db.InsertIssueDescriptionRevisionParams{
			IssueID: rev.ID, WorkspaceID: rev.WorkspaceID, Revision: rev.DescriptionRevision, Content: rev.Description.String,
		}); err != nil {
			return err
		}
	}
	return qtx.PruneIssueDescriptionRevisions(ctx, db.PruneIssueDescriptionRevisionsParams{
		IssueID: issue.ID, KeepFrom: issue.DescriptionRevision - issueDescriptionRevisionsKept,
	})
}

// descriptionOnlyParams writes `description` and leaves every other field as
// `current` has it.
func descriptionOnlyParams(current db.Issue, description string, sourceTask pgtype.UUID) db.UpdateIssueParams {
	return db.UpdateIssueParams{
		ID:            current.ID,
		SourceTaskID:  sourceTask,
		Description:   pgtype.Text{String: description, Valid: true},
		AssigneeType:  current.AssigneeType,
		AssigneeID:    current.AssigneeID,
		StartDate:     current.StartDate,
		DueDate:       current.DueDate,
		ParentIssueID: current.ParentIssueID,
		ProjectID:     current.ProjectID,
		Stage:         current.Stage,
	}
}

// publishIssueDescriptionUpdated announces a write that changed only the
// description, with the payload UpdateIssue gives the same change. `extra`
// carries what a live save adds for the listeners.
func (h *Handler) publishIssueDescriptionUpdated(ctx context.Context, prev, issue db.Issue, actorType, actorID string, extra map[string]any) {
	resp := issueToResponse(issue, h.getIssuePrefix(ctx, issue.WorkspaceID))
	h.fillStatusCategory(ctx, issue.WorkspaceID, projectaccess.Hidden{}, &resp)
	payload := map[string]any{
		"issue":                      resp,
		"assignee_changed":           false,
		"status_changed":             false,
		"priority_changed":           false,
		"project_changed":            false,
		"start_date_changed":         false,
		"due_date_changed":           false,
		"description_changed":        true,
		"title_changed":              false,
		"parent_changed":             false,
		"prev_parent_issue_id":       uuidToPtr(prev.ParentIssueID),
		"prev_title":                 prev.Title,
		"prev_assignee_type":         textToPtr(prev.AssigneeType),
		"prev_assignee_id":           uuidToPtr(prev.AssigneeID),
		"prev_status":                prev.Status,
		"prev_priority":              prev.Priority,
		"prev_start_date":            dateToPtr(prev.StartDate),
		"prev_due_date":              dateToPtr(prev.DueDate),
		"prev_description":           textToPtr(prev.Description),
		"creator_type":               prev.CreatorType,
		"creator_id":                 uuidToString(prev.CreatorID),
		"duplicate_of_issue_id":      liveDuplicateMark(issue.Status, issue.DuplicateOfIssueID),
		"prev_duplicate_of_issue_id": liveDuplicateMark(prev.Status, prev.DuplicateOfIssueID),
	}
	for k, v := range extra {
		payload[k] = v
	}
	h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), actorType, actorID, payload)
}

// writeIssueForCaller answers with the issue as the caller may see it.
func (h *Handler) writeIssueForCaller(w http.ResponseWriter, r *http.Request, issue db.Issue) {
	resp := issueToResponse(issue, h.getIssuePrefix(r.Context(), issue.WorkspaceID))
	hidden, err := h.hiddenProjects(r, uuidToString(issue.WorkspaceID))
	if err != nil {
		hidden = projectaccess.Hidden{}
	}
	h.fillStatusCategory(r.Context(), issue.WorkspaceID, hidden, &resp)
	writeJSON(w, http.StatusOK, resp)
}

// flushIssueDescriptionCollab persists what people have typed into an open
// description, so an agent reading the issue sees it. Best effort, and only
// for agents: people read the description from the live session itself, and
// a slow service must not hold up every issue page.
func (h *Handler) flushIssueDescriptionCollab(r *http.Request, issue db.Issue) db.Issue {
	client := h.issueCollab(r.Context())
	if client == nil {
		return issue
	}
	if actorType, _ := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID)); actorType != "agent" {
		return issue
	}
	ctx, cancel := context.WithTimeout(r.Context(), docCollabFlushTimeout)
	defer cancel()
	if err := client.Flush(ctx, collab.IssueDocumentName(uuidToString(issue.ID))); err != nil {
		slog.Warn("issue description collab flush failed", "issue_id", uuidToString(issue.ID), "error", err)
		return issue
	}
	fresh, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return issue
	}
	return fresh
}

// CreateIssueCollabTicket lets an editor join the live session of an issue's
// description. Guests are refused by the guest route list before this runs;
// the issue loader applies project visibility.
func (h *Handler) CreateIssueCollabTicket(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	client := h.issueCollab(r.Context())
	if client == nil {
		writeJSON(w, http.StatusOK, DocCollabTicketResponse{Enabled: false})
		return
	}
	var req DocCollabTicketRequest
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req)
	}
	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	expires := time.Now().Add(docCollabTicketTTL)
	document := collab.IssueDocumentName(uuidToString(issue.ID))
	token := collab.SignTicket(client.Secret(), collab.Ticket{
		Document:    document,
		WorkspaceID: workspaceID,
		Actor:       h.collabActor(r.Context(), actorType, actorID),
		ExpiresAt:   expires,
	})
	resp := DocCollabTicketResponse{
		Enabled: true, Token: token, Document: document, ExpiresAt: expires.UTC().Format(time.RFC3339),
	}
	if req.State {
		resp.State = h.issueDescriptionCollabState(r.Context(), issue)
	}
	writeJSON(w, http.StatusOK, resp)
}

// issueDescriptionCollabState is docCollabState for a description.
func (h *Handler) issueDescriptionCollabState(ctx context.Context, issue db.Issue) string {
	row, err := h.Queries.GetIssueDescriptionForCollab(ctx, issue.ID)
	if err != nil || row.WorkspaceID != issue.WorkspaceID || row.Ystate == nil || !row.YstateRevision.Valid {
		return ""
	}
	size := len(row.Ystate)
	if row.YstateRevision.Int64 != row.DescriptionRevision || size > docCollabStateMaxBytes {
		return ""
	}
	if size > 1<<20 && size > 4*len(row.Description.String) {
		return ""
	}
	return base64.StdEncoding.EncodeToString(row.Ystate)
}

func writeIssueDescriptionMergeConflict(w http.ResponseWriter, result collab.Result) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error":           "the description overlaps edits made since its base revision",
		"code":            "issue_description_merge_conflict",
		"conflicts":       result.Conflicts,
		"actual_revision": result.Revision,
		"content":         result.Content,
	})
}

func writeIssueDescriptionEditFailed(w http.ResponseWriter, failure *collab.EditFailure, revision int64, content string) {
	msg := "old_text was not found in the description"
	switch failure.Reason {
	case "ambiguous":
		msg = "old_text matches more than once; quote more context or set replace_all"
	case "empty":
		msg = "old_text must not be empty"
	}
	writeJSON(w, http.StatusConflict, map[string]any{
		"error":           msg,
		"code":            "issue_description_edit_failed",
		"failure":         failure,
		"actual_revision": revision,
		"content":         content,
	})
}

// writeIssueCollabFailure renders every non-ok result of a write through the
// live session. It reports false for StatusOK.
func writeIssueCollabFailure(w http.ResponseWriter, result collab.Result) bool {
	switch result.Status {
	case collab.StatusOK:
		return false
	case collab.StatusConflict:
		writeIssueDescriptionMergeConflict(w, result)
	case collab.StatusEditFailed:
		failure := result.Failure
		if failure == nil {
			failure = &collab.EditFailure{Reason: "not_found"}
		}
		writeIssueDescriptionEditFailed(w, failure, result.Revision, result.Content)
	case collab.StatusGone:
		writeError(w, http.StatusNotFound, "issue not found")
	case collab.StatusTooLarge:
		writeError(w, http.StatusRequestEntityTooLarge, "description must be at most 2 MB")
	default:
		writeError(w, http.StatusInternalServerError, "failed to update issue")
	}
	return true
}

// issueDescriptionBase resolves the description a writer started from:
// description_base_revision when given, else the description_base text the
// web editor sends. Neither means a whole replacement. ok is false once a
// response has been written.
func (h *Handler) issueDescriptionBase(w http.ResponseWriter, r *http.Request, issue db.Issue, req UpdateIssueRequest) (base *string, ok bool) {
	if req.DescriptionBaseRevision == nil {
		return req.DescriptionBase, true
	}
	rev := *req.DescriptionBaseRevision
	switch {
	case rev == issue.DescriptionRevision:
		current := issue.Description.String
		return &current, true
	case rev >= 0 && rev < issue.DescriptionRevision:
		content, err := h.Queries.GetIssueDescriptionRevisionContent(r.Context(), db.GetIssueDescriptionRevisionContentParams{
			IssueID: issue.ID, Revision: rev,
		})
		if err == nil {
			return &content, true
		}
		if !isNotFound(err) {
			writeError(w, http.StatusInternalServerError, "failed to update issue")
			return nil, false
		}
	}
	writeRevisionConflict(w, "issue_description", issue.ID, rev, issue.DescriptionRevision)
	return nil, false
}

// updateIssueDescriptionViaCollab sends UpdateIssue's description through the
// live session, so it merges into what people are typing instead of
// overwriting it. handled is false when the service is unreachable and the
// caller should write the description directly; ok is false once a response
// has been written.
func (h *Handler) updateIssueDescriptionViaCollab(w http.ResponseWriter, r *http.Request, client *collab.Client, issue db.Issue, req UpdateIssueRequest, actorType, actorID string) (handled, ok bool) {
	base, ok := h.issueDescriptionBase(w, r, issue, req)
	if !ok {
		return true, false
	}
	write := collab.WriteRequest{
		Actor: h.collabActor(r.Context(), actorType, actorID), Content: req.Description, BaseContent: base,
	}
	if task := h.wakeupSourceTaskID(r); task.Valid {
		write.TaskID = uuidToString(task)
	}
	result, err := client.Write(r.Context(), collab.IssueDocumentName(uuidToString(issue.ID)), write)
	if err != nil {
		if errors.Is(err, collab.ErrUnavailable) {
			slog.Warn("issue description collab write failed; writing directly", "issue_id", uuidToString(issue.ID), "error", err)
			return false, true
		}
		slog.Error("issue description collab write failed", append(logger.RequestAttrs(r), "issue_id", uuidToString(issue.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update issue")
		return true, false
	}
	if writeIssueCollabFailure(w, result) {
		return true, false
	}
	return true, true
}

// issueUpdateOnlyDescription reports whether an UpdateIssue body changes
// nothing but the description.
func issueUpdateOnlyDescription(raw map[string]json.RawMessage) bool {
	for key := range raw {
		switch key {
		case "description", "description_base", "description_base_revision":
		default:
			return false
		}
	}
	return true
}

type EditIssueDescriptionRequest struct {
	Edits []collab.TextEdit `json:"edits"`
}

// EditIssueDescription applies find-and-replace edits to the description.
// Each edit quotes the text it changes, so it lands wherever that text now is,
// even while others are editing, and fails only when someone changed that
// very text.
func (h *Handler) EditIssueDescription(w http.ResponseWriter, r *http.Request) {
	r = h.withWakeupActor(r)
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req EditIssueDescriptionRequest
	r.Body = http.MaxBytesReader(w, r.Body, docBodyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Edits) == 0 || len(req.Edits) > docEditsMax {
		writeError(w, http.StatusBadRequest, "edits must contain between 1 and 100 edits")
		return
	}
	for i := range req.Edits {
		req.Edits[i].OldText = sanitizeNullBytes(req.Edits[i].OldText)
		req.Edits[i].NewText = sanitizeNullBytes(req.Edits[i].NewText)
		if !utf8.ValidString(req.Edits[i].OldText) || !utf8.ValidString(req.Edits[i].NewText) {
			writeError(w, http.StatusBadRequest, "edits must be valid UTF-8")
			return
		}
	}
	ctx := r.Context()
	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	sourceTask := h.wakeupSourceTaskID(r)

	if client := h.issueCollab(ctx); client != nil {
		edit := collab.EditRequest{Actor: h.collabActor(ctx, actorType, actorID), Edits: req.Edits}
		if sourceTask.Valid {
			edit.TaskID = uuidToString(sourceTask)
		}
		result, err := client.Edit(ctx, collab.IssueDocumentName(uuidToString(issue.ID)), edit)
		if err == nil {
			if writeIssueCollabFailure(w, result) {
				return
			}
			updated, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to load issue")
				return
			}
			h.writeIssueForCaller(w, r, updated)
			return
		}
		if !errors.Is(err, collab.ErrUnavailable) {
			slog.Error("issue description collab edit failed", append(logger.RequestAttrs(r), "issue_id", uuidToString(issue.ID), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to edit issue description")
			return
		}
		slog.Warn("issue description collab edit failed; editing directly", "issue_id", uuidToString(issue.ID), "error", err)
	}

	tx, err := h.beginWakeupWrite(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to edit issue description")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	current, err := qtx.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "issue not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to edit issue description")
		}
		return
	}
	content, failure := collab.ApplyTextEdits(current.Description.String, req.Edits)
	if failure != nil {
		writeIssueDescriptionEditFailed(w, failure, current.DescriptionRevision, current.Description.String)
		return
	}
	if len(content) > issueDescriptionMaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "description must be at most 2 MB")
		return
	}
	if content == current.Description.String {
		h.writeIssueForCaller(w, r, current)
		return
	}
	updated, err := qtx.UpdateIssue(ctx, descriptionOnlyParams(current, content, sourceTask))
	if err == nil {
		err = recordIssueDescriptionRevision(ctx, qtx, current, updated)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		slog.Error("edit issue description failed", append(logger.RequestAttrs(r), "issue_id", uuidToString(issue.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to edit issue description")
		return
	}
	h.publishIssueDescriptionUpdated(ctx, current, updated, actorType, actorID, nil)
	h.writeIssueForCaller(w, r, updated)
}

// --- Internal endpoints called by the collaboration service ---

func (h *Handler) GetCollabIssue(w http.ResponseWriter, r *http.Request) {
	issueUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "issue id")
	if !ok {
		return
	}
	row, err := h.Queries.GetIssueDescriptionForCollab(r.Context(), issueUUID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "issue not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load issue")
		}
		return
	}
	resp := collabDocResponse{
		WorkspaceID: uuidToString(row.WorkspaceID), Content: row.Description.String, Revision: row.DescriptionRevision,
	}
	if row.Ystate != nil && row.YstateRevision.Valid {
		state := base64.StdEncoding.EncodeToString(row.Ystate)
		resp.YState, resp.YStateRevision = &state, &row.YstateRevision.Int64
	}
	writeJSON(w, http.StatusOK, resp)
}

type collabIssueStoreRequest struct {
	BaseRevision int64            `json:"base_revision"`
	Content      string           `json:"content"`
	YState       string           `json:"ystate"`
	Contributors []DocContributor `json:"contributors"`
	TaskID       string           `json:"task_id"`
	StateOnly    bool             `json:"state_only"`
	// SeenMentions are the mentions ("type:id") the live session has already
	// stored, so a mention removed and typed again in the same session does
	// not notify twice.
	SeenMentions []string `json:"seen_mentions"`
	// Live marks a save of what people typed, as opposed to one agent or
	// API write; its activity is folded per editor (see activity_listeners).
	Live bool `json:"live"`
}

var attachmentDownloadRe = regexp.MustCompile(`/api/attachments/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})/download`)

// referencedAttachmentIDs lists the attachments a description links to.
func referencedAttachmentIDs(content string) []pgtype.UUID {
	seen := map[string]bool{}
	var ids []pgtype.UUID
	for _, match := range attachmentDownloadRe.FindAllStringSubmatch(content, -1) {
		id := strings.ToLower(match[1])
		if seen[id] {
			continue
		}
		seen[id] = true
		if parsed, err := util.ParseUUID(id); err == nil {
			ids = append(ids, parsed)
		}
	}
	return ids
}

// StoreCollabIssue saves a live description through UpdateIssue, with the
// Yjs state behind it. The base revision is the description's own, so other
// fields changing in between never conflict with it.
func (h *Handler) StoreCollabIssue(w http.ResponseWriter, r *http.Request) {
	issueUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "issue id")
	if !ok {
		return
	}
	var req collabIssueStoreRequest
	r.Body = http.MaxBytesReader(w, r.Body, docCollabBodyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Content = sanitizeNullBytes(req.Content)
	if len(req.Content) > issueDescriptionMaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "description must be at most 2 MB")
		return
	}
	ystate, err := base64.StdEncoding.DecodeString(req.YState)
	if err != nil || len(ystate) == 0 {
		writeError(w, http.StatusBadRequest, "ystate must be base64")
		return
	}
	contributors := make([]DocContributor, 0, len(req.Contributors))
	for _, c := range req.Contributors {
		if _, err := util.ParseUUID(c.ID); err != nil || (c.Type != "member" && c.Type != "agent") {
			writeError(w, http.StatusBadRequest, "invalid contributor")
			return
		}
		contributors = append(contributors, c)
	}
	if len(req.SeenMentions) > issueDescriptionSeenMentionsMax {
		req.SeenMentions = req.SeenMentions[:issueDescriptionSeenMentionsMax]
	}

	actorType, actorID := "system", ""
	if len(contributors) > 0 {
		actorType, actorID = contributors[0].Type, contributors[0].ID
	}
	var sourceTask pgtype.UUID
	task := ""
	if actorType == "agent" {
		if parsed, err := util.ParseUUID(req.TaskID); err == nil {
			sourceTask, task = parsed, uuidToString(parsed)
		}
	}
	ctx := context.WithValue(r.Context(), wakeupActorKey{}, wakeupActor{actorType, actorID, task})
	tx, err := h.beginWakeupWrite(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store issue description")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	current, err := qtx.LockIssueByIDForUpdate(ctx, issueUUID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "issue not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to store issue description")
		}
		return
	}
	if current.DescriptionRevision != req.BaseRevision {
		writeJSON(w, http.StatusConflict, map[string]any{"revision": current.DescriptionRevision, "content": current.Description.String})
		return
	}

	updated := current
	changed := !req.StateOnly && req.Content != current.Description.String
	var linked int64
	content := req.Content
	if changed {
		attachments, err := qtx.ListAttachmentsByIssue(ctx, db.ListAttachmentsByIssueParams{IssueID: current.ID, WorkspaceID: current.WorkspaceID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to store issue description")
			return
		}
		// Channel media keeps its provenance markers, which the editor drops.
		// The session saw everything stored, so a link it removed stays removed.
		stored := current.Description.String
		content = mergeIssueChannelMediaDescription(stored, req.Content, &stored, attachments)
		changed = content != stored
	}
	if changed {
		updated, err = qtx.UpdateIssue(ctx, descriptionOnlyParams(current, content, sourceTask))
		if err == nil {
			err = recordIssueDescriptionRevision(ctx, qtx, current, updated)
		}
		if err == nil {
			linked, err = linkLiveDescriptionAttachments(ctx, qtx, updated, contributors)
		}
		if err != nil {
			slog.Error("store issue description failed", "issue_id", uuidToString(issueUUID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to store issue description")
			return
		}
	}
	if err := qtx.UpsertIssueDescriptionCollabState(ctx, db.UpsertIssueDescriptionCollabStateParams{
		IssueID: updated.ID, WorkspaceID: updated.WorkspaceID, Ystate: ystate, Revision: updated.DescriptionRevision,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store issue description")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store issue description")
		return
	}

	mentions := []string{}
	if changed {
		extra := map[string]any{"description_seen_mentions": req.SeenMentions}
		if req.Live {
			extra["description_live_editors"] = contributors
		}
		h.publishIssueDescriptionUpdated(r.Context(), current, updated, actorType, actorID, extra)
		// After issue:updated: clients admit the snapshot before an auxiliary
		// event at the same revision.
		if linked > 0 {
			h.publish(protocol.EventIssueAttachmentsChanged, uuidToString(updated.WorkspaceID), actorType, actorID, map[string]any{
				"issue_id":       uuidToString(updated.ID),
				"issue_revision": updated.Revision,
			})
		}
		for _, m := range util.ParseMentions(updated.Description.String) {
			mentions = append(mentions, m.Type+":"+m.ID)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": updated.DescriptionRevision, "mentions": mentions})
}

// linkLiveDescriptionAttachments binds files that editors of a live session
// uploaded and linked in the description, which the solo editor binds by
// sending attachment_ids with its save.
func linkLiveDescriptionAttachments(ctx context.Context, qtx *db.Queries, issue db.Issue, contributors []DocContributor) (int64, error) {
	ids := referencedAttachmentIDs(issue.Description.String)
	if len(ids) == 0 || len(contributors) == 0 {
		return 0, nil
	}
	uploaders := make([]pgtype.UUID, 0, len(contributors))
	for _, c := range contributors {
		uploaders = append(uploaders, parseUUID(c.ID))
	}
	return qtx.LinkUnboundAttachmentsToIssue(ctx, db.LinkUnboundAttachmentsToIssueParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, AttachmentIds: ids, UploaderIds: uploaders,
	})
}
