package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/cloudruntime"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflagdispatch"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/integrations/lark"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
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
	// DisableWorkspaceCreation, when true, makes POST /api/workspaces return
	// 403 for every caller. There is no role/owner exception because the repo
	// has no platform-admin concept; operators bootstrap the workspace with
	// the flag off, then flip it on and restart so subsequent users join via
	// invitation only. The public /api/config endpoint mirrors this flag so
	// the UI can hide every "Create workspace" affordance — see #3433.
	DisableWorkspaceCreation bool
	// PublicURL is the absolute base URL the API is reachable at from the
	// public internet, with no trailing slash (e.g. "https://app.multica.ai").
	// Used only to build webhook_url responses for autopilot webhook triggers
	// — never for auth, routing, or workspace resolution. Empty when unset,
	// in which case clients fall back to webhook_path + their own origin.
	// Reading the public host from request headers (Host / X-Forwarded-Host)
	// is intentionally avoided so a misconfigured reverse proxy cannot trick
	// the server into minting webhook URLs pointing at an attacker-controlled
	// host.
	PublicURL string
	// TrustedProxies are CIDRs whose source IP we trust to set
	// X-Forwarded-For / X-Real-IP. Empty means "trust nothing": the rate
	// limiter uses r.RemoteAddr exclusively. Populated via the
	// MULTICA_TRUSTED_PROXIES env var (comma-separated CIDRs, e.g.
	// "10.0.0.0/8,127.0.0.1/32"). This is specifically to keep the per-IP
	// webhook limiter from being bypassed by a spoofed XFF on deployments
	// without a header-stripping reverse proxy in front.
	TrustedProxies []netip.Prefix
	// CloudRuntimeFleetURL enables the SaaS-only remote Fleet adapter when set.
	// Empty keeps self-hosted deployments explicit: cloud runtime endpoints
	// return 503 instead of attempting to dial a hard-coded private service.
	CloudRuntimeFleetURL     string
	CloudRuntimeFleetTimeout time.Duration
	AttachmentDownloadMode   string
	AttachmentDownloadURLTTL time.Duration
	// AttachmentFrameAncestors are trusted browser origins allowed to embed
	// attachment preview responses. In production this should mirror the
	// frontend/CORS origin allowlist so split app/api self-hosted deployments
	// can frame API-hosted PDFs without allowing arbitrary third-party frames.
	AttachmentFrameAncestors []string
}

type cloudRuntimeProxy interface {
	Enabled() bool
	Do(ctx context.Context, req cloudruntime.Request) (*cloudruntime.Response, error)
}

type RuntimeProfileRefreshNotifier interface {
	NotifyRuntimeProfilesChanged(workspaceID, profileID string)
}

type Handler struct {
	Queries               *db.Queries
	DB                    dbExecutor
	TxStarter             txStarter
	Hub                   *realtime.Hub
	DaemonHub             *daemonws.Hub
	DaemonProfileRefresh  RuntimeProfileRefreshNotifier
	Bus                   *events.Bus
	TaskService           *service.TaskService
	IssueService          *service.IssueService
	AutopilotService      *service.AutopilotService
	EmailService          *service.EmailService
	UpdateStore           UpdateStore
	ModelListStore        ModelListStore
	LocalSkillListStore   LocalSkillListStore
	LocalSkillImportStore LocalSkillImportStore
	DaemonFeatureFlags    *featureflagdispatch.Evaluator
	LivenessStore         LivenessStore
	HeartbeatScheduler    HeartbeatScheduler
	Storage               storage.Storage
	CFSigner              *auth.CloudFrontSigner
	Analytics             analytics.Client
	// Metrics is the shared business-metrics collector built by main.go.
	// May be nil in tests / self-hosted with the metrics listener disabled;
	// every Record* method is nil-safe and obsmetrics.RecordEvent treats a
	// nil Metrics as "PostHog only".
	Metrics              *obsmetrics.BusinessMetrics
	PATCache             *auth.PATCache
	DaemonTokenCache     *auth.DaemonTokenCache
	MembershipCache      *auth.MembershipCache
	WebhookRateLimiter   WebhookRateLimiter
	WebhookIPRateLimiter WebhookRateLimiter
	CloudRuntime         cloudRuntimeProxy
	// Lark integration. All three are nil when the Lark master key
	// (MULTICA_LARK_SECRET_KEY) is unset; the corresponding HTTP
	// handlers return 503 in that case so a misconfigured self-host
	// deployment surfaces a clear error instead of silently using a
	// zero key. Wired in cmd/server/router.go after handler.New.
	LarkInstallations *lark.InstallationService
	LarkBindingTokens *lark.BindingTokenService
	// LarkRegistration owns the device-flow install lifecycle: begin
	// a registration session against accounts.feishu.cn, poll, and
	// on success write lark_installation + the installer's
	// lark_user_binding in one DB transaction. Nil when the at-rest
	// key is unset or the RegistrationService failed to construct at
	// boot.
	LarkRegistration *lark.RegistrationService
	// LarkAPIClient is the live transport that backs SendInteractiveCard,
	// PatchInteractiveCard, SendBindingPromptCard, GetBotInfo. The
	// router wires the real Lark HTTP client whenever
	// MULTICA_LARK_SECRET_KEY is set; tests that need a no-op
	// behaviour can swap in `lark.NewStubAPIClient(...)` directly. The
	// UI consults IsConfigured() to decide whether to surface install
	// entry points.
	LarkAPIClient lark.APIClient
	// ChannelSupervisor owns the per-installation supervisor goroutines
	// that hold the §4.4 WS lease and drive each channel.Channel
	// (MUL-3620 generalized the Feishu-only Hub into this channel-agnostic
	// engine). The router constructs it UNCONDITIONALLY — it drives any
	// channel type, not just Feishu, so it does not depend on the Lark
	// master key; each platform registers its Factory only when configured
	// (Feishu when MULTICA_LARK_SECRET_KEY is set). The router does NOT
	// call Run; the process owner (main.go) starts it under a long-running
	// context and joins via WaitWithTimeout (bounded, fenced by
	// ShutdownTimeout) during graceful shutdown so the lease renewer yields
	// cleanly when the DB is healthy without blocking process exit if the
	// pool is frozen — at worst the next replica waits the full TTL.
	ChannelSupervisor *engine.Supervisor
	// ChannelRouter is the channel-agnostic inbound pipeline (the shared
	// handler the Supervisor injects into every Channel). main.go calls
	// Drain on it during shutdown, after the Supervisor has stopped
	// delivering events, to flush debounced run triggers and join in-flight
	// reply goroutines. Built unconditionally (even without Lark).
	ChannelRouter *engine.Router
	cfg           Config
}

func New(queries *db.Queries, txStarter txStarter, hub *realtime.Hub, bus *events.Bus, emailService *service.EmailService, store storage.Storage, cfSigner *auth.CloudFrontSigner, analyticsClient analytics.Client, cfg Config, daemonHubs ...*daemonws.Hub) *Handler {
	var executor dbExecutor
	if candidate, ok := txStarter.(dbExecutor); ok {
		executor = candidate
	}

	if analyticsClient == nil {
		analyticsClient = analytics.NoopClient{}
	}
	if mode, ok := normalizeAttachmentDownloadMode(cfg.AttachmentDownloadMode); ok {
		cfg.AttachmentDownloadMode = string(mode)
	} else {
		slog.Warn("invalid ATTACHMENT_DOWNLOAD_MODE, using auto", "value", cfg.AttachmentDownloadMode)
		cfg.AttachmentDownloadMode = string(attachmentDownloadModeAuto)
	}
	if cfg.AttachmentDownloadURLTTL <= 0 {
		cfg.AttachmentDownloadURLTTL = defaultAttachmentDownloadURLTTL
	}

	var daemonHub *daemonws.Hub
	if len(daemonHubs) > 0 {
		daemonHub = daemonHubs[0]
	}
	var daemonProfileRefresh RuntimeProfileRefreshNotifier
	if daemonHub != nil {
		daemonProfileRefresh = daemonHub
	}

	taskSvc := service.NewTaskService(queries, txStarter, hub, bus, daemonHub)
	taskSvc.Analytics = analyticsClient
	return &Handler{
		Queries:               queries,
		DB:                    executor,
		TxStarter:             txStarter,
		Hub:                   hub,
		DaemonHub:             daemonHub,
		DaemonProfileRefresh:  daemonProfileRefresh,
		Bus:                   bus,
		TaskService:           taskSvc,
		IssueService:          service.NewIssueService(queries, txStarter, bus, analyticsClient, taskSvc),
		AutopilotService:      service.NewAutopilotService(queries, txStarter, bus, taskSvc),
		EmailService:          emailService,
		UpdateStore:           NewInMemoryUpdateStore(),
		ModelListStore:        NewInMemoryModelListStore(),
		LocalSkillListStore:   NewInMemoryLocalSkillListStore(),
		LocalSkillImportStore: NewInMemoryLocalSkillImportStore(),
		LivenessStore:         NewNoopLivenessStore(),
		HeartbeatScheduler:    NewPassthroughHeartbeatScheduler(queries),
		Storage:               store,
		CFSigner:              cfSigner,
		Analytics:             analyticsClient,
		WebhookRateLimiter:    NewMemoryWebhookRateLimiter(DefaultWebhookRateLimit()),
		WebhookIPRateLimiter:  NewMemoryWebhookIPRateLimiter(DefaultWebhookIPRateLimit()),
		CloudRuntime: cloudruntime.NewClient(cloudruntime.Config{
			BaseURL: cfg.CloudRuntimeFleetURL,
			Timeout: cfg.CloudRuntimeFleetTimeout,
		}),
		cfg: cfg,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeMeasuredJSON behaves like writeJSON but returns the encoded body size so
// callers can record payload bytes in slow-endpoint diagnostics. It measures the
// uncompressed JSON length and is unrelated to transport compression.
func writeMeasuredJSON(w http.ResponseWriter, status int, v any) (int, error) {
	body, err := json.Marshal(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode response")
		return 0, err
	}
	body = append(body, '\n')
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		return len(body), err
	}
	return len(body), nil
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Thin wrappers around util functions.
//
// parseUUID is intentionally the panicking variant: any handler call site
// reachable here is expected to feed a UUID that is either (a) a sqlc round-trip
// of a DB-sourced value, or (b) a raw request input that has already been
// validated upstream. A panic here means an unguarded user-input string slipped
// in — that is a real bug we want surfaced loudly (chi's middleware.Recoverer
// converts it to a 500) instead of silently corrupting data via a zero UUID.
//
// For unvalidated user input at request boundaries, use parseUUIDOrBadRequest
// (writes 400) — never feed raw chi.URLParam / request-body strings into
// parseUUID directly when the call writes to the database.
func parseUUID(s string) pgtype.UUID                { return util.MustParseUUID(s) }
func uuidToString(u pgtype.UUID) string             { return util.UUIDToString(u) }
func textToPtr(t pgtype.Text) *string               { return util.TextToPtr(t) }
func ptrToText(s *string) pgtype.Text               { return util.PtrToText(s) }
func strToText(s string) pgtype.Text                { return util.StrToText(s) }
func timestampToString(t pgtype.Timestamptz) string { return util.TimestampToString(t) }
func timestampToPtr(t pgtype.Timestamptz) *string   { return util.TimestampToPtr(t) }
func dateToPtr(d pgtype.Date) *string               { return util.DateToPtr(d) }
func uuidToPtr(u pgtype.UUID) *string               { return util.UUIDToPtr(u) }
func int8ToPtr(v pgtype.Int8) *int64                { return util.Int8ToPtr(v) }
func int4ToPtr(v pgtype.Int4) *int32                { return util.Int4ToPtr(v) }
func ptrToInt4(v *int32) pgtype.Int4                { return util.PtrToInt4(v) }

// parseUUIDOrBadRequest validates a UUID string sourced from user input
// (URL params, request body, headers). On invalid input it writes a 400
// response and returns ok=false; callers must return immediately.
//
// Use this anywhere a malformed UUID would otherwise reach a write query
// (DELETE / UPDATE) — the silent zero-UUID behavior of the old ParseUUID
// caused real silent-data-loss bugs (#1661).
func parseUUIDOrBadRequest(w http.ResponseWriter, s, fieldName string) (pgtype.UUID, bool) {
	u, err := util.ParseUUID(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+fieldName)
		return pgtype.UUID{}, false
	}
	return u, true
}

func parseUUIDSliceOrBadRequest(w http.ResponseWriter, ids []string, fieldName string) ([]pgtype.UUID, bool) {
	uuids := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		u, err := util.ParseUUID(id)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid "+fieldName)
			return nil, false
		}
		uuids[i] = u
	}
	return uuids, true
}

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

// isCheckViolation reports whether err is a PostgreSQL CHECK constraint
// violation (SQLSTATE 23514). Used to translate column-level CHECK failures
// into a 4xx instead of a generic 500.
func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}

func requestUserID(r *http.Request) string {
	return r.Header.Get("X-User-ID")
}

// resolveActor determines whether the request is from an agent or a human member.
//
// First-class signal: X-Actor-Source set to "task_token" means the request
// authenticated via an `mat_` task-scoped token. The auth middleware sets
// that header (and stripped any client-supplied value first), so it is
// authoritative — the bound (agent_id, task_id) cannot be forged or
// stripped by the agent process. This is the path MUL-2600 relies on to
// reject agent-process traffic on owner-only endpoints.
//
// Fallback signal (legacy CLI / member-token paths): the request MUST
// carry both X-Agent-ID and a valid X-Task-ID, and the task must belong
// to the claimed agent. Otherwise we fall back to "member".
//
// X-Agent-ID alone is not trusted: any workspace member can guess or observe
// an agent's UUID, and a member-supplied X-Agent-ID would otherwise let that
// member impersonate the agent and bypass the private-agent gate (#2359
// review). The daemon always pairs the two headers, so requiring both has
// no effect on legitimate agent callers but closes the impersonation path.
//
// Returns ("agent", agentID) on success, ("member", userID) otherwise.
func (h *Handler) resolveActor(r *http.Request, userID, workspaceID string) (actorType, actorID string) {
	if r.Header.Get("X-Actor-Source") == "task_token" {
		// Server-set header — auth middleware also forced X-Agent-ID
		// from the token row. Trust it directly without re-querying.
		return "agent", r.Header.Get("X-Agent-ID")
	}
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		return "member", userID
	}
	taskID := r.Header.Get("X-Task-ID")
	if taskID == "" {
		slog.Debug("resolveActor: X-Agent-ID present but X-Task-ID missing, refusing to trust agent identity", "agent_id", agentID)
		return "member", userID
	}

	agentUUID, err := util.ParseUUID(agentID)
	if err != nil {
		slog.Debug("resolveActor: X-Agent-ID is not a valid UUID, falling back to member", "agent_id", agentID)
		return "member", userID
	}
	// Validate the agent exists in the target workspace.
	agent, err := h.Queries.GetAgent(r.Context(), agentUUID)
	if err != nil || uuidToString(agent.WorkspaceID) != workspaceID {
		slog.Debug("resolveActor: X-Agent-ID rejected, agent not found or workspace mismatch", "agent_id", agentID, "workspace_id", workspaceID)
		return "member", userID
	}

	taskUUID, err := util.ParseUUID(taskID)
	if err != nil {
		slog.Debug("resolveActor: X-Task-ID is not a valid UUID, falling back to member", "task_id", taskID)
		return "member", userID
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil || uuidToString(task.AgentID) != agentID {
		slog.Debug("resolveActor: X-Task-ID rejected, task not found or agent mismatch", "agent_id", agentID, "task_id", taskID)
		return "member", userID
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
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		return db.Member{}, err
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return db.Member{}, err
	}
	return h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userUUID,
		WorkspaceID: wsUUID,
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
		userUUID, err := util.ParseUUID(userID)
		if err != nil {
			return false
		}
		wsUUID, err := util.ParseUUID(workspaceID)
		if err != nil {
			return false
		}
		_, err = h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          userUUID,
			WorkspaceID: wsUUID,
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

	// Try identifier format first (e.g., "JIA-42"). resolveIssueByIdentifier
	// silently returns false for non-identifier strings, falling through to
	// the UUID path below.
	if issue, ok := h.resolveIssueByIdentifier(r.Context(), issueID, workspaceID); ok {
		return issue, true
	}

	issueUUID, err := util.ParseUUID(issueID)
	if err != nil {
		// Not a valid UUID and didn't match identifier format → 404 (consistent
		// with previous silent-zero behavior, which would also have produced 404).
		writeError(w, http.StatusNotFound, "issue not found")
		return db.Issue{}, false
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace_id")
		return db.Issue{}, false
	}
	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: wsUUID,
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
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return db.Issue{}, false
	}
	issue, err := h.Queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
		WorkspaceID: wsUUID,
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

	agentUUID, ok := parseUUIDOrBadRequest(w, agentID, "agent id")
	if !ok {
		return db.Agent{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.Agent{}, false
	}

	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
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

	itemUUID, ok := parseUUIDOrBadRequest(w, itemID, "inbox item id")
	if !ok {
		return db.InboxItem{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.InboxItem{}, false
	}

	item, err := h.Queries.GetInboxItemInWorkspace(r.Context(), db.GetInboxItemInWorkspaceParams{
		ID:          itemUUID,
		WorkspaceID: wsUUID,
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
