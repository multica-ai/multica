package handler

// CEREBRO-PATCH(handler-cerebro-routes): cerebro modification of upstream file

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
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/duplicatecheck"
	cerebroinfisical "github.com/multica-ai/multica/server/internal/cerebro/infisical"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate" // CEREBRO-PATCH(handler-approval-gate): FIR-2586 shared approval seam.
	"github.com/multica-ai/multica/server/internal/cloudruntime"
	"github.com/multica-ai/multica/server/internal/daemonws"
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
	// CEREBRO-PATCH(scanner-discovery-token): bearer token for daemon scanner discovery endpoint.
	ScannerDiscoveryToken string
}

type cloudRuntimeProxy interface {
	Enabled() bool
	Do(ctx context.Context, req cloudruntime.Request) (*cloudruntime.Response, error)
	// CEREBRO-PATCH(handler): persona integration additions.
}

type Handler struct {
	Queries *db.Queries
	// CEREBRO-PATCH(agent-infisical-secrets): cerebro queries for per-agent secret refs.
	CerebroQueries        *cerebrodb.Queries
	DB                    dbExecutor
	TxStarter             txStarter
	Hub                   *realtime.Hub
	DaemonHub             *daemonws.Hub
	Bus                   *events.Bus
	TaskService           *service.TaskService
	AutopilotService      *service.AutopilotService
	EmailService          *service.EmailService
	UpdateStore           UpdateStore
	ModelListStore        ModelListStore
	LocalSkillListStore   LocalSkillListStore
	LocalSkillImportStore LocalSkillImportStore
	LivenessStore         LivenessStore
	HeartbeatScheduler    HeartbeatScheduler
	Storage               storage.Storage
	CFSigner              *auth.CloudFrontSigner
	Analytics             analytics.Client
	PATCache              *auth.PATCache
	DaemonTokenCache      *auth.DaemonTokenCache
	MembershipCache       *auth.MembershipCache
	WebhookRateLimiter    WebhookRateLimiter
	WebhookIPRateLimiter  WebhookRateLimiter
	CloudRuntime          cloudRuntimeProxy
	cfg                   Config
	// CEREBRO-PATCH(handler-cerebro-fields): cerebro budget guard, web push,
	// and daemon ping state.
	BudgetService *service.BudgetService
	PushService   *service.PushService
	PingStore     *PingStore
	// CEREBRO-PATCH(handler-channel-listen): channel-listener (per-(channel,agent)
	// listen-mode) service. Set by the router after construction so the upstream
	// handler.New signature stays unchanged.
	ChannelListen ChannelListenInvoker
	// CEREBRO-PATCH(handler-runtime-pause): cerebro runtime pause/unpause service.
	RuntimePause RuntimePauseInvoker
	// CEREBRO-PATCH(handler-group-permissions): cerebro group-permission gate.
	GroupPermissions GroupPermissionsInvoker
	// CEREBRO-PATCH(handler-mention-trigger-gate): cerebro @mention trigger gate.
	MentionTriggerGate MentionTriggerGateInvoker
	// CEREBRO-PATCH(handler-comment-target-guard): FIR-2674 reject agent comments with no target.
	CommentTargetGuard CommentTargetGuardInvoker
	// CEREBRO-PATCH(handler-private-agent-run-request): FIR-2385 — turns a member's
	// tag of an unowned private agent into a run-request in the owner's inbox.
	PrivateAgentRunRequester PrivateAgentRunRequesterInvoker
	// CEREBRO-PATCH(handler-runtime-account): cerebro daemon-driven account
	// registration service. Wired by the router after construction.
	RuntimeAccount RuntimeAccountInvoker
	// CEREBRO-PATCH(handler-persona-mask): JEH-1079 mask checker. Wired by
	// the router after construction; nil = persona not configured (no redaction).
	PersonaMask PersonaMaskInvoker
	// CEREBRO-PATCH(handler-persona-mask-audit): JEH-1173 redaction ledger.
	// Wired by the router after construction; nil = no audit row written.
	PersonaMaskAudit PersonaMaskAuditWriter
	// CEREBRO-PATCH(handler-github-pr-heal): JEH-1919 PR-card self-heal hook.
	PullRequestLinkHealer PullRequestLinkHealer
	// CEREBRO-PATCH(handler-infisical-provisioner): scoped-per-user Infisical
	// machine identity provisioner. Wired by the router from
	// MULTICA_CREDENTIALS_KEY + INFISICAL_ADMIN_* env. nil = provisioning
	// disabled (allowlist saves still succeed; secret fetch on claim returns
	// empty).
	InfisicalProvisioner *cerebroinfisical.Provisioner
	// CEREBRO-PATCH(handler-tool-meta): JEH-1353 — ordered list of registered
	// tools and name→description lookup for the tool grant admin API.
	cerebroToolItems  []CerebroToolItem
	cerebroToolDesc   map[string]string
	cerebroToolStatus map[string]string // CEREBRO-PATCH(handler-tool-status): reject stale grants for excluded tools.
	// CEREBRO-PATCH(handler-runtime-tools-admin): JEH-1710 unified runtime
	runtimeToolsAdmin RuntimeToolsAdminService
	// CEREBRO-PATCH(handler-runtime-tools-scan): JEH-1710 daemon-side ingest
	runtimeToolsScan RuntimeToolsScanService
	// CEREBRO-PATCH(handler-capability-register): FIR-2129 normalized capability register.
	capabilityRegister CapabilityRegisterService
	// CEREBRO-PATCH(handler-cloud-runtime-tool-scan): FIR-2284 server-side scan for cloud runtimes.
	cloudRuntimeToolScanner CloudRuntimeToolScanner
	// CEREBRO-PATCH(handler-duplicate-check): FIR-2504 inject a custom judge
	// (test fake or workspace-aware gateway) into CheckSimilarIssues; nil
	// means the default env-resolved gateway is used.
	DuplicateCheckJudger *duplicatecheck.Judger
	// CEREBRO-PATCH(handler-custom-status-resolver): FIR-1550 v2b — resolver invoked from UpdateIssue.
	CustomStatusResolver CustomStatusResolver
	// CEREBRO-PATCH(handler-identity-provisioner): FIR-2523 Google Workspace
	// auto-membership hook. Wired by the router; nil = no auto-provisioning.
	IdentityProvisioner IdentityProvisionerInvoker
	// CEREBRO-PATCH(handler-approval-gate): FIR-2586 shared approval seam for
	// daemon repo checkout. nil when CEREBRO_APPROVAL_GATE_ENABLED is off, so an
	// "Ask" verdict keeps its prior block; non-nil routes it to the one /approvals
	// inbox (CheckDaemonRepoCapability creates the ask, the daemon long-polls it).
	ApprovalGate *permgate.Gate
	// CEREBRO-PATCH(handler-semantic-search): FIR-2604 hybrid (FTS+vector) seam.
	SemanticSearch SemanticSearchInvoker
}

// CustomStatusResolver is the upstream-side seam for the cerebro status-model
// sidecar. UpdateIssue invokes it after the upstream base-status write so a
// project's custom_status pin stays in sync with the new base — and so the
// status-picker can pass an explicit custom_status_key end-to-end through the
// normal upstream update path (boards/live updates/activity log/triggers all
// fire on the same event). requestedKey == "" means "auto-resolve to the
// first matching custom status under newBase".
//
// CEREBRO-PATCH(handler-custom-status-resolver-iface): FIR-1550 v2b seam.
type CustomStatusResolver interface {
	ResolveCustomStatusAfterBaseChange(
		ctx context.Context,
		issueID, projectID, workspaceID pgtype.UUID,
		newBase, actorID, actorType, requestedKey string,
	) error
	// ValidateCustomStatusKey is the read-only pre-commit guard: UpdateIssue
	// calls it before committing the base-status change so an invalid explicit
	// key returns 400 without half-applying the base status. It returns non-nil
	// ONLY for a genuine key mismatch (the 400 case); empty key, no model, and
	// infra hiccups all return nil so the normal update proceeds.
	ValidateCustomStatusKey(
		ctx context.Context,
		projectID pgtype.UUID,
		newBase, requestedKey string,
	) error
}

// ErrCustomStatusKeyMismatch is the sentinel returned by a
// CustomStatusResolver implementation when the caller's explicit
// custom_status_key cannot be applied (key missing, or its base does not
// match the resulting base). UpdateIssue maps this to a 400 so the client
// knows the picker payload itself is the problem.
//
// CEREBRO-PATCH(handler-custom-status-resolver-err): FIR-1550 v2b error sentinel.
var ErrCustomStatusKeyMismatch = errors.New("custom_status_key is not valid for the resulting base status in this project's model")

// RuntimePauseInvoker is the upstream-side seam that the cerebro runtime
// pause service plugs into. Methods on *Handler in runtime_pause_cerebro.go
// type-assert this to call the concrete service without importing the
// cerebro package directly (which would create an import cycle).
//
// CEREBRO-PATCH(handler-runtime-pause-iface): seam for cerebro runtime pause.
type RuntimePauseInvoker interface {
	PauseRuntime(ctx context.Context, runtimeID pgtype.UUID, opts RuntimePauseOptions) (RuntimePauseState, error)
	UnpauseRuntime(ctx context.Context, runtimeID pgtype.UUID) (RuntimePauseState, error)
}

// RuntimePauseOptions mirrors cerebroruntime.PauseOptions on the upstream
// side of the seam so callers don't need to import the cerebro package.
type RuntimePauseOptions struct {
	UnpauseAt time.Time
	Reason    string
}

// RuntimePauseState is the subset of the post-pause/unpause runtime row
// that the HTTP handler needs to render the API response.
type RuntimePauseState struct {
	WorkspaceID pgtype.UUID
	PausedAt    pgtype.Timestamptz
	UnpauseAt   pgtype.Timestamptz
	PauseReason pgtype.Text
}

// MentionTriggerGateInvoker is the upstream-side seam for Cerebro's
// @mention trigger gate.
type MentionTriggerGateInvoker interface {
	CanTriggerMention(ctx context.Context, r *http.Request, workspaceID string, agentID, ownerID pgtype.UUID) (bool, error)
}

// CommentTargetGuardInvoker is the upstream-side seam for Cerebro's "no agent
// comment without a target" guard (FIR-2674). RejectComment returns
// (message, ok=false) when an agent-authored comment references no target and
// must be rejected; ok=true means it passes. A nil invoker disables the guard.
//
// CEREBRO-PATCH(handler-comment-target-guard-iface): seam for FIR-2674.
type CommentTargetGuardInvoker interface {
	RejectComment(authorType, content string) (string, bool)
}

// PrivateAgentRunRequesterInvoker is the upstream-side seam for FIR-2385. It is
// invoked from the comment mention path when a *member* tags a private agent
// they do not own (the trigger gate has already refused). The cerebro
// implementation records a run-request in the owner's inbox and dedups on
// (agent, comment); the boolean trigger gate stays a pure authz answer.
//
// CEREBRO-PATCH(handler-private-agent-run-request-iface): seam for FIR-2385.
type PrivateAgentRunRequesterInvoker interface {
	RequestPrivateAgentRun(ctx context.Context, workspaceID string, agentID, ownerID, issueID, commentID, requesterID pgtype.UUID, agentName string) error
}

// ChannelListenInvoker is the upstream-side seam that the cerebro
// channel-listen service plugs into. The interface lets the upstream comment
// handler call the cerebro service without importing it (which would create
// an import cycle: handler → cerebro/channels → uses TaskService → ...).
//
// CEREBRO-PATCH(handler-channel-listen-iface): seam for cerebro listen-mode,
// channel-list archived filter, and explicit unarchive-on-DM-reopen (JEH-1046).
type ChannelListenInvoker interface {
	EnqueueChannelListenerTasks(ctx context.Context, issue db.Issue, comment db.Comment, parentComment *db.Comment, authorType, authorID string)
	FilterArchivedChannels(ctx context.Context, userID pgtype.UUID, rows []db.ListChannelsForUserRow, includeArchived bool) []db.ListChannelsForUserRow
	MaybeUnarchiveForUser(ctx context.Context, channelID, userID pgtype.UUID, workspaceID, actorType, actorID string)
	// CEREBRO-PATCH(handler-dm-promote-iface): JEH-1131 — seam for the
	// DM-to-channel promotion that runs after mention dispatch.
	PromoteDMOnMention(ctx context.Context, issue db.Issue, comment db.Comment, parentComment *db.Comment, workspaceID, actorType, actorID string)
}

// New constructs a Handler. The pushService argument is cerebro-specific
// (Web Push delivery). Pass nil from upstream-only call sites.
//
// CEREBRO-PATCH(handler-new-pushsvc): pushService threaded through.
func New(queries *db.Queries, txStarter txStarter, hub *realtime.Hub, bus *events.Bus, emailService *service.EmailService, pushService *service.PushService, store storage.Storage, cfSigner *auth.CloudFrontSigner, analyticsClient analytics.Client, cfg Config, daemonHubs ...*daemonws.Hub) *Handler {
	var executor dbExecutor
	if candidate, ok := txStarter.(dbExecutor); ok {
		executor = candidate
	}

	if analyticsClient == nil {
		analyticsClient = analytics.NoopClient{}
	}

	var daemonHub *daemonws.Hub
	if len(daemonHubs) > 0 {
		daemonHub = daemonHubs[0]
	}

	taskSvc := service.NewTaskService(queries, txStarter, hub, bus, daemonHub)
	taskSvc.Analytics = analyticsClient
	return &Handler{
		Queries:               queries,
		DB:                    executor,
		TxStarter:             txStarter,
		Hub:                   hub,
		DaemonHub:             daemonHub,
		Bus:                   bus,
		TaskService:           taskSvc,
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
		// CEREBRO-PATCH(handler-cerebro-init): cerebro budget guard, web push,
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
func uuidToPtr(u pgtype.UUID) *string               { return util.UUIDToPtr(u) }
func int8ToPtr(v pgtype.Int8) *int64                { return util.Int8ToPtr(v) }

// CEREBRO-PATCH(handler-bool-helpers): cerebro per-runtime sandbox toggle uses
// nullable bool semantics (nil = inherit env-var default).
func boolToPtr(b pgtype.Bool) *bool { return util.BoolToPtr(b) }
func ptrToBool(p *bool) pgtype.Bool { return util.PtrToBool(p) }

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

// CEREBRO-PATCH(publish-to-audience): publish() with audience restriction so
// WS fan-out can be limited to specific users (project access control).
func (h *Handler) publishToAudience(eventType, workspaceID, actorType, actorID string, payload any, audience []string) {
	h.Bus.Publish(events.Event{
		Type:            eventType,
		WorkspaceID:     workspaceID,
		ActorType:       actorType,
		ActorID:         actorID,
		Payload:         payload,
		AudienceUserIDs: audience,
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

	var issue db.Issue
	// Try identifier format first (e.g., "JIA-42"). resolveIssueByIdentifier
	// silently returns false for non-identifier strings, falling through to
	// the UUID path below.
	if found, ok := h.resolveIssueByIdentifier(r.Context(), issueID, workspaceID); ok {
		issue = found
	} else {
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
		got, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          issueUUID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "issue not found")
			return db.Issue{}, false
		}
		issue = got
	}

	// CEREBRO-PATCH(loadIssue-access-check): enforce project access / channel
	// membership. For tasks this is project access / standalone privacy; for
	// channels/DMs it's participant membership. canAccessIssue dispatches on
	// issue.Kind. Returning 404 (not 403) hides existence from non-members.
	if member, ok := h.resolveMemberFromRequest(r); ok {
		if !h.canAccessIssue(r.Context(), member, issue) {
			writeError(w, http.StatusNotFound, "issue not found")
			return db.Issue{}, false
		}
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
