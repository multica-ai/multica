package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func decodeMediaJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		writeError(w, 400, "invalid media request")
		return false
	}
	return true
}

func (h *Handler) mediaCaller(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	user, ok := requireUserID(w, r)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	ws, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	if _, err := h.getWorkspaceMember(r.Context(), user, uuidToString(ws)); err != nil {
		writeError(w, 403, "workspace access denied")
		return ws, pgtype.UUID{}, false
	}
	return ws, parseUUID(user), true
}

func (h *Handler) CreateMediaJob(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.mediaCaller(w, r)
	if !ok {
		return
	}
	var req protocol.MediaRequest
	if !decodeMediaJSON(w, r, &req) {
		return
	}
	if req.Provider == "" {
		req.Provider = "auto"
	}
	if err := req.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	uploaderType, uploaderID := h.resolveActor(r, uuidToString(user), uuidToString(ws))
	params := db.CreateMediaJobParams{WorkspaceID: ws, CallerID: user, UploaderType: uploaderType, UploaderID: parseUUID(uploaderID)}
	if r.Header.Get("X-Actor-Source") == "task_token" {
		bound := r.Header.Get("X-Task-ID")
		if req.TaskID != "" && req.TaskID != bound {
			writeError(w, 403, "task_id must match task token")
			return
		}
		req.TaskID = bound
	} else if req.TaskID != "" {
		writeError(w, 403, "task_id requires task token")
		return
	}
	if req.TaskID != "" {
		taskID, ok := parseUUIDOrBadRequest(w, req.TaskID, "task_id")
		if !ok {
			return
		}
		task, err := h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{ID: taskID, WorkspaceID: ws})
		if err != nil || uploaderType != "agent" || task.AgentID != params.UploaderID {
			writeError(w, 403, "task access denied")
			return
		}
		params.TaskID, params.IssueID, params.ChatSessionID = task.ID, task.IssueID, task.ChatSessionID
		if req.IssueID != "" && req.IssueID != uuidToString(task.IssueID) {
			writeError(w, 403, "issue_id must match task")
			return
		}
		req.IssueID = uuidToString(task.IssueID)
	} else if req.IssueID != "" {
		issue, ok := h.loadIssueForUser(w, r, req.IssueID)
		if !ok {
			return
		}
		params.IssueID = issue.ID
		req.IssueID = uuidToString(issue.ID)
	}
	for _, input := range req.Inputs {
		id, ok := parseUUIDOrBadRequest(w, input.AttachmentID, "input attachment_id")
		if !ok {
			return
		}
		att, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{ID: id, WorkspaceID: ws})
		if err != nil {
			writeError(w, 404, "input attachment not found")
			return
		}
		if att.ChatSessionID.Valid && att.ChatSessionID != params.ChatSessionID {
			if _, ok := h.gatePublicChatSessionForUser(w, r, uuidToString(user), uuidToString(ws), uuidToString(att.ChatSessionID)); !ok {
				return
			}
		}
	}
	params.Request, _ = json.Marshal(req)
	job, err := h.Queries.CreateMediaJob(r.Context(), params)
	if err != nil {
		writeError(w, 500, "create media job failed")
		return
	}
	writeJSON(w, 202, h.mediaJobResponse(r, job))
}

func (h *Handler) mediaJobResponse(r *http.Request, job db.MediaJob) protocol.MediaJob {
	result := protocol.MediaJob{ID: uuidToString(job.ID), State: job.State, RuntimeID: uuidToString(job.RuntimeID), Error: job.Error, ExpiresAt: job.ExpiresAt.Time}
	_ = json.Unmarshal(job.Request, &result.Request)
	if job.AttachmentID.Valid && job.State == "completed" {
		if att, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{ID: job.AttachmentID, WorkspaceID: job.WorkspaceID}); err == nil {
			out := h.attachmentToResponse(att, attachmentURLModeFromRequest(r))
			result.AttachmentID, result.MarkdownURL = out.ID, out.MarkdownURL
			label := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)").Replace(att.Filename)
			prefix := "!file"
			if strings.HasPrefix(att.ContentType, "image/") {
				prefix = "!"
			}
			result.Markdown = fmt.Sprintf("%s[%s](%s)", prefix, label, out.MarkdownURL)
		}
	}
	return result
}

func (h *Handler) loadMediaJob(w http.ResponseWriter, r *http.Request) (db.MediaJob, bool) {
	ws, user, ok := h.mediaCaller(w, r)
	if !ok {
		return db.MediaJob{}, false
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "job id")
	if !ok {
		return db.MediaJob{}, false
	}
	if err := h.Queries.ExpireMediaJobs(r.Context(), ws); err != nil {
		writeError(w, 500, "expire media jobs failed")
		return db.MediaJob{}, false
	}
	job, err := h.Queries.GetMediaJob(r.Context(), db.GetMediaJobParams{ID: id, WorkspaceID: ws})
	if err != nil || job.CallerID != user || (r.Header.Get("X-Actor-Source") == "task_token" && uuidToString(job.TaskID) != r.Header.Get("X-Task-ID")) {
		writeError(w, 404, "media job not found")
		return db.MediaJob{}, false
	}
	return job, true
}

func (h *Handler) GetMediaJob(w http.ResponseWriter, r *http.Request) {
	job, ok := h.loadMediaJob(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, h.mediaJobResponse(r, job))
}

func (h *Handler) CancelMediaJob(w http.ResponseWriter, r *http.Request) {
	job, ok := h.loadMediaJob(w, r)
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "cancel failed")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	job, err = q.LockMediaJob(r.Context(), db.LockMediaJobParams{ID: job.ID, WorkspaceID: job.WorkspaceID})
	if err == nil && (job.State == "queued" || job.State == "running") {
		job, err = q.FinishMediaJob(r.Context(), db.FinishMediaJobParams{ID: job.ID, WorkspaceID: job.WorkspaceID, State: "cancelled"})
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, 500, "cancel failed")
		return
	}
	writeJSON(w, 200, h.mediaJobResponse(r, job))
}

func (h *Handler) ClaimMediaJob(w http.ResponseWriter, r *http.Request) {
	var req protocol.MediaClaimRequest
	if !decodeMediaJSON(w, r, &req) {
		return
	}
	rt, ok := h.requireDaemonRuntimeAccess(w, r, req.RuntimeID)
	if !ok {
		return
	}
	if err := h.Queries.ExpireMediaJobs(r.Context(), rt.WorkspaceID); err != nil {
		writeError(w, 500, "expire media jobs failed")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "claim failed")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	rt, err = q.LockAgentRuntime(r.Context(), rt.ID)
	if err != nil {
		writeError(w, 500, "claim failed")
		return
	}
	if !h.requireDaemonRuntimeAuthorized(w, r, uuidToString(rt.WorkspaceID), rt.DaemonID, rt.OwnerID, "runtime not found") {
		return
	}
	// The daemon has one serial worker per runtime. Another claim means the
	// previous assignment ended, even if its dispatch or receipt was lost.
	if err := q.SettleMediaRuntimeJobs(r.Context(), db.SettleMediaRuntimeJobsParams{WorkspaceID: rt.WorkspaceID, RuntimeID: rt.ID}); err != nil {
		writeError(w, 500, "settle previous assignment failed")
		return
	}
	runtimes, err := q.AvailableMediaRuntimes(r.Context(), rt.WorkspaceID)
	if err != nil {
		writeError(w, 500, "runtime selection failed")
		return
	}
	// Filter unroutable operations and callers before the bounded queue scan,
	// so an unavailable provider's backlog cannot starve another provider.
	routes := make([]map[string]string, 0, 4)
	for _, capability := range []string{"image-generate", "image-edit"} {
		request := protocol.MediaRequest{Provider: "auto", Capability: capability}
		if selected := service.SelectMediaRuntime([]db.AgentRuntime{rt}, rt.OwnerID, request, time.Now()); selected != nil {
			routes = append(routes, map[string]string{"provider": protocol.MediaTagProvider(selected.Tag), "capability": capability})
		}
	}
	rawRoutes, _ := json.Marshal(routes)
	jobs, err := q.QueuedMediaJobs(r.Context(), db.QueuedMediaJobsParams{WorkspaceID: rt.WorkspaceID, RuntimeID: rt.ID, Routes: rawRoutes})
	if err != nil {
		writeError(w, 500, "claim failed")
		return
	}
	for _, job := range jobs {
		if _, err := h.getWorkspaceMember(r.Context(), uuidToString(job.CallerID), uuidToString(job.WorkspaceID)); err != nil {
			continue
		}
		var request protocol.MediaRequest
		if json.Unmarshal(job.Request, &request) != nil {
			continue
		}
		selected := service.SelectMediaRuntime(runtimes, job.CallerID, request, time.Now())
		if selected == nil || selected.ID != rt.ID {
			continue
		}
		attempt := uuid.NewString()
		job, err = q.StartMediaJob(r.Context(), db.StartMediaJobParams{ID: job.ID, WorkspaceID: job.WorkspaceID, RuntimeID: rt.ID, Attempt: parseUUID(attempt), CapabilityTag: selected.Tag})
		if err != nil {
			writeError(w, 500, "claim failed")
			return
		}
		token, err := mediaJobToken(job, uuidToString(rt.OwnerID))
		if err != nil || tx.Commit(r.Context()) != nil {
			writeError(w, 500, "claim failed")
			return
		}
		writeJSON(w, 200, protocol.MediaClaimResponse{Assignment: &protocol.MediaAssignment{Job: h.mediaJobResponse(r, job), Attempt: attempt, Token: token}})
		return
	}
	if tx.Commit(r.Context()) != nil {
		writeError(w, 500, "claim failed")
		return
	}
	writeJSON(w, 200, protocol.MediaClaimResponse{})
}

func (h *Handler) ReportMediaJob(w http.ResponseWriter, r *http.Request) {
	var req protocol.MediaResult
	if !decodeMediaJSON(w, r, &req) {
		return
	}
	rt, ok := h.requireDaemonRuntimeAccess(w, r, req.RuntimeID)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, req.JobID, "job_id")
	if !ok {
		return
	}
	attempt, ok := parseUUIDOrBadRequest(w, req.Attempt, "attempt")
	if !ok {
		return
	}
	if len(req.Error) > 1024 {
		writeError(w, 400, "error too long")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "report failed")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	job, err := q.LockMediaJob(r.Context(), db.LockMediaJobParams{ID: id, WorkspaceID: rt.WorkspaceID})
	if err != nil || job.RuntimeID != rt.ID || job.Attempt != attempt {
		writeError(w, 409, "stale media attempt")
		return
	}
	// Receipts are idempotent. A transport retry cannot requeue an old attempt.
	if job.State != "running" {
		writeJSON(w, 200, h.mediaJobResponse(r, job))
		return
	}
	state, message := "failed", req.Error
	if time.Now().After(job.ExpiresAt.Time) {
		message = "job deadline exceeded"
	} else if req.RateLimited && !job.AttachmentID.Valid {
		until := service.MediaCooldown(rt, job.CapabilityTag, req.RetryAfter, time.Now())
		err = q.CoolMediaRuntime(r.Context(), db.CoolMediaRuntimeParams{ID: rt.ID, WorkspaceID: rt.WorkspaceID, Capability: job.CapabilityTag, UntilEpoch: until.Unix()})
		if err != nil {
			writeError(w, 500, "cooldown failed")
			return
		}
		if job.Attempts < 32 {
			state, message = "queued", ""
		} else {
			message = "media quota retry limit exceeded"
		}
	} else if req.Error == "" && job.AttachmentID.Valid {
		state = "completed"
	} else if message == "" {
		message = "executor returned no attachment"
	}
	job, err = q.FinishMediaJob(r.Context(), db.FinishMediaJobParams{ID: id, WorkspaceID: rt.WorkspaceID, State: state, Error: message})
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, 500, "report failed")
		return
	}
	writeJSON(w, 200, h.mediaJobResponse(r, job))
}
