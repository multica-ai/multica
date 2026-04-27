package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// randomID returns a random 16-byte hex string used as a request ID for
// in-memory stores (model list, local skills, CLI update, etc.).
func randomID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type txStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type dbExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Config struct {
	AllowSignup         bool
	AllowedEmails       []string
	AllowedEmailDomains []string
}

type Handler struct {
	Queries               *db.Queries
	DB                    dbExecutor
	TxStarter             txStarter
	Hub                   *realtime.Hub
	Bus                   *events.Bus
	TaskService           *service.TaskService
	AutopilotService      *service.AutopilotService
	EmailService          *service.EmailService
	RedmineClient         *service.RedmineClient
	UpdateStore           *UpdateStore
	ModelListStore        *ModelListStore
	LocalSkillListStore   LocalSkillListStore
	LocalSkillImportStore LocalSkillImportStore
	Storage               storage.Storage
	CFSigner              *auth.CloudFrontSigner
	Analytics             analytics.Client
	cfg                   Config
}

func New(queries *db.Queries, txStarter txStarter, hub *realtime.Hub, bus *events.Bus, emailService *service.EmailService, store storage.Storage, cfSigner *auth.CloudFrontSigner, analyticsClient analytics.Client, cfg Config) *Handler {
	var executor dbExecutor
	if candidate, ok := txStarter.(dbExecutor); ok {
		executor = candidate
	}

	if analyticsClient == nil {
		analyticsClient = analytics.NoopClient{}
	}

	taskSvc := service.NewTaskService(queries, txStarter, hub, bus)
	return &Handler{
		Queries:               queries,
		DB:                    executor,
		TxStarter:             txStarter,
		Hub:                   hub,
		Bus:                   bus,
		TaskService:           taskSvc,
		AutopilotService:      service.NewAutopilotService(queries, txStarter, bus, taskSvc),
		EmailService:          emailService,
		RedmineClient:         service.NewRedmineClient(),
		UpdateStore:           NewUpdateStore(),
		ModelListStore:        NewModelListStore(),
		LocalSkillListStore:   NewInMemoryLocalSkillListStore(),
		LocalSkillImportStore: NewInMemoryLocalSkillImportStore(),
		Storage:               store,
		CFSigner:              cfSigner,
		Analytics:             analyticsClient,
		cfg:                   cfg,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Thin wrappers around util functions (preserve existing handler code unchanged).
func parseUUID(s string) pgtype.UUID                { return util.ParseUUID(s) }
func uuidToString(u pgtype.UUID) string             { return util.UUIDToString(u) }
func textToPtr(t pgtype.Text) *string               { return util.TextToPtr(t) }
func ptrToText(s *string) pgtype.Text               { return util.PtrToText(s) }
func strToText(s string) pgtype.Text                { return util.StrToText(s) }
func timestampToString(t pgtype.Timestamptz) string { return util.TimestampToString(t) }
func timestampToPtr(t pgtype.Timestamptz) *string   { return util.TimestampToPtr(t) }
func uuidToPtr(u pgtype.UUID) *string               { return util.UUIDToPtr(u) }

// publish sends a domain event through the event bus.
func (h *Handler) publish(eventType, workspaceID, actorType, actorID string, payload any) {
	h.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   actorType,
		ActorID:     actorID,
		Payload:     payload,
	})
}

// publishTask is publish() plus a TaskID hint so the realtime layer can route
// the event to the per-task scope rather than the whole workspace.
func (h *Handler) publishTask(eventType, workspaceID, actorType, actorID, taskID string, payload any) {
	h.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   actorType,
		ActorID:     actorID,
		TaskID:      taskID,
		Payload:     payload,
	})
}

// publishChat is publish() plus a ChatSessionID hint so the realtime layer
// can route the event to the per-chat-session scope.
func (h *Handler) publishChat(eventType, workspaceID, actorType, actorID, chatSessionID string, payload any) {
	h.Bus.Publish(events.Event{
		Type:          eventType,
		WorkspaceID:   workspaceID,
		ActorType:     actorType,
		ActorID:       actorID,
		ChatSessionID: chatSessionID,
		Payload:       payload,
	})
}

func isNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func requestUserID(r *http.Request) string {
	return r.Header.Get("X-User-ID")
}

// resolveActor determines whether the request is from an agent or a human member.
// If X-Agent-ID and X-Task-ID headers are both set, validates that the task
// belongs to the claimed agent (defense-in-depth against manual header spoofing).
// If only X-Agent-ID is set, validates that the agent belongs to the workspace.
// Returns ("agent", agentID) on success, ("member", userID) otherwise.
func (h *Handler) resolveActor(r *http.Request, userID, workspaceID string) (actorType, actorID string) {
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		return "member", userID
	}

	// Validate the agent exists in the target workspace.
	agent, err := h.Queries.GetAgent(r.Context(), parseUUID(agentID))
	if err != nil || uuidToString(agent.WorkspaceID) != workspaceID {
		slog.Debug("resolveActor: X-Agent-ID rejected, agent not found or workspace mismatch", "agent_id", agentID, "workspace_id", workspaceID)
		return "member", userID
	}

	// When X-Task-ID is provided, cross-check that the task belongs to this agent.
	if taskID := r.Header.Get("X-Task-ID"); taskID != "" {
		task, err := h.Queries.GetAgentTask(r.Context(), parseUUID(taskID))
		if err != nil || uuidToString(task.AgentID) != agentID {
			slog.Debug("resolveActor: X-Task-ID rejected, task not found or agent mismatch", "agent_id", agentID, "task_id", taskID)
			return "member", userID
		}
	}

	return "agent", agentID
}

func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := requestUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

// resolveWorkspaceID returns the workspace UUID for this request. Delegates
// to middleware.ResolveWorkspaceIDFromRequest so middleware-protected routes
// and middleware-less routes (e.g. /api/upload-file) share identical
// resolution behavior — including slug → UUID translation via the DB.
//
// Returns "" when no workspace identifier was provided or a slug was provided
// but doesn't match any workspace.
func (h *Handler) resolveWorkspaceID(r *http.Request) string {
	return middleware.ResolveWorkspaceIDFromRequest(r, h.Queries)
}

// ctxMember returns the workspace member from context (set by workspace middleware).
func ctxMember(ctx context.Context) (db.Member, bool) {
	return middleware.MemberFromContext(ctx)
}

// ctxWorkspaceID returns the workspace ID from context (set by workspace middleware).
func ctxWorkspaceID(ctx context.Context) string {
	return middleware.WorkspaceIDFromContext(ctx)
}

// workspaceIDFromURL returns the workspace ID from context (preferred) or chi URL param (fallback).
func workspaceIDFromURL(r *http.Request, param string) string {
	if id := middleware.WorkspaceIDFromContext(r.Context()); id != "" {
		return id
	}
	return chi.URLParam(r, param)
}

// workspaceMember returns the member from middleware context, or falls back to a DB
// lookup when the handler is called directly (e.g. in tests).
func (h *Handler) workspaceMember(w http.ResponseWriter, r *http.Request, workspaceID string) (db.Member, bool) {
	if m, ok := ctxMember(r.Context()); ok {
		return m, true
	}
	return h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
}

func roleAllowed(role string, roles ...string) bool {
	for _, candidate := range roles {
		if role == candidate {
			return true
		}
	}
	return false
}

func countOwners(members []db.Member) int {
	owners := 0
	for _, member := range members {
		if member.Role == "owner" {
			owners++
		}
	}
	return owners
}

func (h *Handler) getWorkspaceMember(ctx context.Context, userID, workspaceID string) (db.Member, error) {
	return h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
}

func (h *Handler) requireWorkspaceMember(w http.ResponseWriter, r *http.Request, workspaceID, notFoundMsg string) (db.Member, bool) {
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Member{}, false
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return db.Member{}, false
	}

	member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, notFoundMsg)
		return db.Member{}, false
	}

	return member, true
}

func (h *Handler) requireWorkspaceRole(w http.ResponseWriter, r *http.Request, workspaceID, notFoundMsg string, roles ...string) (db.Member, bool) {
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, notFoundMsg)
	if !ok {
		return db.Member{}, false
	}
	if !roleAllowed(member.Role, roles...) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.Member{}, false
	}
	return member, true
}

// isWorkspaceEntity checks whether a user_id belongs to the given workspace,
// as either a member or an agent depending on userType.
func (h *Handler) isWorkspaceEntity(ctx context.Context, userType, userID, workspaceID string) bool {
	switch userType {
	case "member":
		_, err := h.getWorkspaceMember(ctx, userID, workspaceID)
		return err == nil
	case "agent":
		_, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          parseUUID(userID),
			WorkspaceID: parseUUID(workspaceID),
		})
		return err == nil
	default:
		return false
	}
}

func (h *Handler) loadIssueForUser(w http.ResponseWriter, r *http.Request, issueID string) (db.Issue, bool) {
	if _, ok := requireUserID(w, r); !ok {
		return db.Issue{}, false
	}

	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Issue{}, false
	}

	// Try identifier format first (e.g., "JIA-42").
	if issue, ok := h.resolveIssueByIdentifier(r.Context(), issueID, workspaceID); ok {
		return issue, true
	}

	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          parseUUID(issueID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return db.Issue{}, false
	}
	return issue, true
}

// resolveIssueByIdentifier tries to look up an issue by "PREFIX-NUMBER" format.
func (h *Handler) resolveIssueByIdentifier(ctx context.Context, id, workspaceID string) (db.Issue, bool) {
	parts := splitIdentifier(id)
	if parts == nil {
		return db.Issue{}, false
	}
	if workspaceID == "" {
		return db.Issue{}, false
	}
	issue, err := h.Queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
		WorkspaceID: parseUUID(workspaceID),
		Number:      parts.number,
	})
	if err != nil {
		return db.Issue{}, false
	}
	return issue, true
}

type identifierParts struct {
	prefix string
	number int32
}

func splitIdentifier(id string) *identifierParts {
	idx := -1
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == '-' {
			idx = i
			break
		}
	}
	if idx <= 0 || idx >= len(id)-1 {
		return nil
	}
	numStr := id[idx+1:]
	num := 0
	for _, c := range numStr {
		if c < '0' || c > '9' {
			return nil
		}
		num = num*10 + int(c-'0')
	}
	if num <= 0 {
		return nil
	}
	return &identifierParts{prefix: id[:idx], number: int32(num)}
}

// getIssuePrefix fetches the issue_prefix for a workspace.
// Falls back to generating a prefix from the workspace name if the stored
// prefix is empty (e.g. workspaces created before the prefix was introduced).
func (h *Handler) getIssuePrefix(ctx context.Context, workspaceID pgtype.UUID) string {
	ws, err := h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return ""
	}
	if ws.IssuePrefix != "" {
		return ws.IssuePrefix
	}
	return generateIssuePrefix(ws.Name)
}

func (h *Handler) loadAgentForUser(w http.ResponseWriter, r *http.Request, agentID string) (db.Agent, bool) {
	if _, ok := requireUserID(w, r); !ok {
		return db.Agent{}, false
	}

	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Agent{}, false
	}

	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          parseUUID(agentID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return db.Agent{}, false
	}
	return agent, true
}

func (h *Handler) loadInboxItemForUser(w http.ResponseWriter, r *http.Request, itemID string) (db.InboxItem, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.InboxItem{}, false
	}

	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.InboxItem{}, false
	}

	item, err := h.Queries.GetInboxItemInWorkspace(r.Context(), db.GetInboxItemInWorkspaceParams{
		ID:          parseUUID(itemID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "inbox item not found")
		return db.InboxItem{}, false
	}

	if item.RecipientType != "member" || uuidToString(item.RecipientID) != userID {
		writeError(w, http.StatusNotFound, "inbox item not found")
		return db.InboxItem{}, false
	}
	return item, true
}
