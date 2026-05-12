package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/secrets"
)

// secretsEncryptString is a thin handler-package alias so the package
// import lives only here. Tests override secrets directly.
func secretsEncryptString(plaintext string) ([]byte, error) {
	return secrets.EncryptString(plaintext)
}

var nonAlpha = regexp.MustCompile(`[^a-zA-Z]`)
var workspaceSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// generateIssuePrefix produces a 2-5 char uppercase prefix from a workspace name.
// Examples: "Jiayuan's Workspace" → "JIA", "My Team" → "MYT", "AB" → "AB".
func generateIssuePrefix(name string) string {
	letters := nonAlpha.ReplaceAllString(name, "")
	if len(letters) == 0 {
		return "WS"
	}
	letters = strings.ToUpper(letters)
	if len(letters) > 3 {
		letters = letters[:3]
	}
	return letters
}

type WorkspaceResponse struct {
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	Slug                 string  `json:"slug"`
	Description          *string `json:"description"`
	Context              *string `json:"context"`
	Settings             any     `json:"settings"`
	Repos                any     `json:"repos"`
	IssuePrefix          string  `json:"issue_prefix"`
	OrchestratorAgentID  *string `json:"orchestrator_agent_id"` // optional pointer to the workspace's orchestrator agent — woken up on agent-authored issue comments to drive cross-agent workflows
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
	// ChannelsEnabled gates the entire Channels feature surface — when false
	// the sidebar entry hides and every /api/channels endpoint 404s.
	ChannelsEnabled      bool   `json:"channels_enabled"`
	// ChannelRetentionDays is the workspace-level default retention window
	// for channel messages, in days. null/nil = retain forever.
	ChannelRetentionDays *int32 `json:"channel_retention_days"`
	// ShipHubEnabled gates the entire Ship Hub feature surface — same
	// "invisible feature" guarantee as ChannelsEnabled.
	ShipHubEnabled bool `json:"ship_hub_enabled"`
	// GitHubTokenSet is true when a workspace-level GitHub PAT is stored.
	// The raw token NEVER appears in API responses — only this presence
	// boolean is leaked to clients. Phase 2 moves the token into the
	// encrypted workspace_secret table; the legacy ship_hub.github_token
	// settings field is migrated on startup.
	GitHubTokenSet bool `json:"github_token_set"`
	// ShipHubWebhookURL is the public URL the workspace owner copies into
	// GitHub's webhook config. Computed from MULTICA_API_BASE_URL so the
	// frontend doesn't have to thread the env var through.
	ShipHubWebhookURL string `json:"ship_hub_webhook_url"`
	// ShipHubWebhookSecretSet mirrors GitHubTokenSet — true when a
	// webhook secret has been configured. The plaintext value is only
	// ever returned by POST .../regenerate_webhook_secret.
	ShipHubWebhookSecretSet bool `json:"ship_hub_webhook_secret_set"`
	// ShipHubSmokeWorkflowSet — true when a smoke-test GitHub Actions
	// workflow filename has been configured for the workspace. Drives
	// the Ship Hub release page's "Run smoke tests" button: when false,
	// the affordance hides (it would 400 anyway), and the smoke status
	// pill renders "Not configured" instead of an empty dash. Phase 7c
	// polish — adding this stops users clicking a button that's
	// guaranteed to error.
	ShipHubSmokeWorkflowSet bool `json:"ship_hub_smoke_workflow_set"`
	// Phase 7d follow-up — per-risk-tier approval rule. One of
	// "member" / "admin" / "approver" / "two". Defaults preserve the
	// legacy hardcoded behavior (low/medium → "member", high →
	// "approver", critical → "two") so existing workspaces don't see
	// a silent change post-migration.
	ShipHubApprovalLow      string `json:"ship_hub_approval_low"`
	ShipHubApprovalMedium   string `json:"ship_hub_approval_medium"`
	ShipHubApprovalHigh     string `json:"ship_hub_approval_high"`
	ShipHubApprovalCritical string `json:"ship_hub_approval_critical"`
	// ShipHubApproverCanBeAuthor — when false, separation-of-duties
	// is enforced: a verifier in the release's PR-author set is
	// rejected. Defaults to true (small teams typically self-verify).
	ShipHubApproverCanBeAuthor bool `json:"ship_hub_approver_can_be_author"`
	// ShipHubDeployWorkflowStagingSet — true when a staging deploy
	// workflow filename is configured. When set, the deploy poller
	// goroutine watches GitHub Actions for completed runs of that
	// workflow on main and auto-links matching releases by sha.
	// The release page UI reads this flag to swap the "awaiting deploy"
	// copy from "click Mark deploy as landed" to "polling — link should
	// land within 4min of the workflow completing".
	//
	// We surface the boolean only (not the filename) for parity with
	// ShipHubSmokeWorkflowSet — the filename isn't a secret but
	// consistency keeps the response shape predictable.
	ShipHubDeployWorkflowStagingSet    bool `json:"ship_hub_deploy_workflow_staging_set"`
	ShipHubDeployWorkflowProductionSet bool `json:"ship_hub_deploy_workflow_production_set"`
}

// shipHubSettingsKey is the JSON object inside workspace.settings that holds
// Ship-Hub-specific configuration. Kept under a namespaced key so other
// settings (analytics opt-out, etc.) can coexist without collisions.
const shipHubSettingsKey = "ship_hub"

// ReadShipHubGitHubTokenForReconciler is the cmd/server-visible accessor
// for the workspace-stored GitHub PAT. Lives here (not in cmd/server)
// because the storage shape is a workspace-package concern; exporting just
// the read path keeps the write path private.
//
// Phase 2 prefers the encrypted workspace_secret row; the settings path
// is the legacy fallback for workspaces that haven't been migrated yet.
// Pass the workspace's encrypted-secret ciphertext (or nil) so the
// reconciler doesn't have to call back into queries from this package.
func ReadShipHubGitHubTokenForReconciler(settings []byte) string {
	return readShipHubGitHubToken(settings)
}

// ReadShipHubGitHubTokenFromEncrypted decrypts the value_encrypted blob
// returned by Queries.GetWorkspaceSecret. Returns "" on any error so a
// corrupted row degrades to "no token" rather than blocking the
// reconciler.
func ReadShipHubGitHubTokenFromEncrypted(ciphertext []byte) string {
	if len(ciphertext) == 0 {
		return ""
	}
	plaintext, err := secrets.DecryptString(ciphertext)
	if err != nil {
		slog.Warn("ship hub: decrypt github token failed", "error", err)
		return ""
	}
	return plaintext
}

// readShipHubGitHubToken extracts the workspace's GitHub PAT from settings.
// Returns "" when not set. The token is stored unencrypted in v1 — see
// the encryption TODO in UpdateWorkspace.
func readShipHubGitHubToken(settings []byte) string {
	if len(settings) == 0 {
		return ""
	}
	var s map[string]any
	if err := json.Unmarshal(settings, &s); err != nil {
		return ""
	}
	sub, ok := s[shipHubSettingsKey].(map[string]any)
	if !ok {
		return ""
	}
	tok, _ := sub["github_token"].(string)
	return tok
}

// redactShipHubSettings strips the github_token from a settings map before
// the workspace response goes back to the client. This is the only field
// that must not echo back; everything else under ship_hub is fine to leak
// (auto-promote, target_url, etc. are non-secret).
func redactShipHubSettings(settings map[string]any) {
	sub, ok := settings[shipHubSettingsKey].(map[string]any)
	if !ok {
		return
	}
	delete(sub, "github_token")
	if len(sub) == 0 {
		// Drop the empty container so the client doesn't see a stub.
		delete(settings, shipHubSettingsKey)
	} else {
		settings[shipHubSettingsKey] = sub
	}
}

// workspaceToResponse projects a workspace row into the public JSON
// shape. Static derivations only — for fields that need a DB lookup
// (e.g. "is the encrypted github_token row present?") use
// workspaceToResponseWithSecrets which the workspace handler invokes
// when it has access to the queries object.
func workspaceToResponse(w db.Workspace) WorkspaceResponse {
	return workspaceToResponseWithSecretFlags(w, secretFlags{})
}

// secretFlags carries the presence-only signals that need DB lookups
// alongside the workspace row. Decoupling these from the row itself
// keeps the legacy callsites (tests, listeners) functional without
// requiring a Queries handle everywhere.
//
// WebhookURL is request-derived (forwarded-proto/host or direct host)
// when set by workspaceToResponseFull, falling back to env-derived
// webhookPublicURL() for paths that don't have a request in scope
// (background goroutines).
type secretFlags struct {
	HasEncryptedGitHubToken    bool
	HasEncryptedWebhookSecret  bool
	WebhookURL                 string
}

func workspaceToResponseWithSecretFlags(w db.Workspace, flags secretFlags) WorkspaceResponse {
	var settings map[string]any
	if w.Settings != nil {
		json.Unmarshal(w.Settings, &settings)
	}
	if settings == nil {
		settings = map[string]any{}
	}
	// "Is a token configured?" is true when EITHER the legacy settings
	// JSON has it OR the encrypted store has a row. Same for the webhook
	// secret across the plaintext column + encrypted store.
	tokenSet := readShipHubGitHubToken(w.Settings) != "" || flags.HasEncryptedGitHubToken
	webhookSet := (w.ShipHubWebhookSecret.Valid && w.ShipHubWebhookSecret.String != "") || flags.HasEncryptedWebhookSecret
	redactShipHubSettings(settings)
	var repos any
	if w.Repos != nil {
		json.Unmarshal(w.Repos, &repos)
	}
	if repos == nil {
		repos = []any{}
	}
	var retention *int32
	if w.ChannelRetentionDays.Valid {
		v := w.ChannelRetentionDays.Int32
		retention = &v
	}
	return WorkspaceResponse{
		ID:                         uuidToString(w.ID),
		Name:                       w.Name,
		Slug:                       w.Slug,
		Description:                textToPtr(w.Description),
		Context:                    textToPtr(w.Context),
		Settings:                   settings,
		Repos:                      repos,
		IssuePrefix:                w.IssuePrefix,
		OrchestratorAgentID:        uuidToPtr(w.OrchestratorAgentID),
		CreatedAt:                  timestampToString(w.CreatedAt),
		UpdatedAt:                  timestampToString(w.UpdatedAt),
		ChannelsEnabled:            w.ChannelsEnabled,
		ChannelRetentionDays:       retention,
		ShipHubEnabled:             w.ShipHubEnabled,
		GitHubTokenSet:             tokenSet,
		ShipHubWebhookURL:          flags.WebhookURL, // pass-through; built upstream when a request is in scope
		ShipHubWebhookSecretSet:    webhookSet,
		ShipHubSmokeWorkflowSet:    w.ShipHubSmokeWorkflow.Valid && w.ShipHubSmokeWorkflow.String != "",
		ShipHubApprovalLow:         w.ShipHubApprovalLow,
		ShipHubApprovalMedium:      w.ShipHubApprovalMedium,
		ShipHubApprovalHigh:        w.ShipHubApprovalHigh,
		ShipHubApprovalCritical:    w.ShipHubApprovalCritical,
		ShipHubApproverCanBeAuthor: w.ShipHubApproverCanBeAuthor,
		ShipHubDeployWorkflowStagingSet: w.ShipHubDeployWorkflowStaging.Valid &&
			w.ShipHubDeployWorkflowStaging.String != "",
		ShipHubDeployWorkflowProductionSet: w.ShipHubDeployWorkflowProduction.Valid &&
			w.ShipHubDeployWorkflowProduction.String != "",
	}
}

// workspaceToResponseFull is the preferred call shape from any handler
// that already has a context + queries handle. It populates the secret
// presence flags from the encrypted store.
//
// Pass r when available so the webhook URL is derived from the live
// request's forwarded-proto/host (the only honest source for deployments
// behind a reverse proxy). Pass nil for code paths without a request in
// scope; the URL falls back to MULTICA_API_BASE_URL.
func (h *Handler) workspaceToResponseFull(ctx context.Context, r *http.Request, w db.Workspace) WorkspaceResponse {
	flags := secretFlags{
		WebhookURL: webhookPublicURLFromRequest(r),
	}
	if _, err := h.Queries.GetWorkspaceSecret(ctx, db.GetWorkspaceSecretParams{
		WorkspaceID: w.ID,
		Name:        "github_token",
	}); err == nil {
		flags.HasEncryptedGitHubToken = true
	}
	if _, err := h.Queries.GetWorkspaceSecret(ctx, db.GetWorkspaceSecretParams{
		WorkspaceID: w.ID,
		Name:        "github_webhook_secret",
	}); err == nil {
		flags.HasEncryptedWebhookSecret = true
	}
	return workspaceToResponseWithSecretFlags(w, flags)
}

type MemberResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	UserID      string `json:"user_id"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at"`
}

func memberToResponse(m db.Member) MemberResponse {
	return MemberResponse{
		ID:          uuidToString(m.ID),
		WorkspaceID: uuidToString(m.WorkspaceID),
		UserID:      uuidToString(m.UserID),
		Role:        m.Role,
		CreatedAt:   timestampToString(m.CreatedAt),
	}
}

func (h *Handler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaces, err := h.Queries.ListWorkspaces(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspaces")
		return
	}

	resp := make([]WorkspaceResponse, len(workspaces))
	for i, ws := range workspaces {
		resp[i] = h.workspaceToResponseFull(r.Context(), r, ws)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	id := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "workspace id")
	if !ok {
		return
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), idUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, h.workspaceToResponseFull(r.Context(), r, ws))
}

type CreateWorkspaceRequest struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description"`
	Context     *string `json:"context"`
	IssuePrefix *string `json:"issue_prefix"`
}

func (h *Handler) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	if req.Name == "" || req.Slug == "" {
		writeError(w, http.StatusBadRequest, "name and slug are required")
		return
	}
	if !workspaceSlugPattern.MatchString(req.Slug) {
		writeError(w, http.StatusBadRequest, "slug must contain only lowercase letters, numbers, and hyphens")
		return
	}
	if isReservedSlug(req.Slug) {
		writeError(w, http.StatusBadRequest, "slug is reserved")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workspace")
		return
	}
	defer tx.Rollback(r.Context())

	issuePrefix := generateIssuePrefix(req.Name)
	if req.IssuePrefix != nil && strings.TrimSpace(*req.IssuePrefix) != "" {
		issuePrefix = strings.ToUpper(strings.TrimSpace(*req.IssuePrefix))
	}

	qtx := h.Queries.WithTx(tx)
	ws, err := qtx.CreateWorkspace(r.Context(), db.CreateWorkspaceParams{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: ptrToText(req.Description),
		Context:     ptrToText(req.Context),
		IssuePrefix: issuePrefix,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "workspace slug already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create workspace: "+err.Error())
		return
	}

	_, err = qtx.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: ws.ID,
		UserID:      parseUUID(userID),
		Role:        "owner",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add owner: "+err.Error())
		return
	}

	// Becoming a workspace member is the physical event that "completes" onboarding —
	// keep this atomic with CreateMember so `member` and `onboarded_at`
	// can never disagree. COALESCE in MarkUserOnboarded keeps it idempotent.
	if _, err := qtx.MarkUserOnboarded(r.Context(), parseUUID(userID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark user onboarded")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workspace")
		return
	}

	// "Is this the user's first workspace?" is derived in PostHog by looking
	// at whether they have a prior workspace_created event, not stamped at
	// emit time. Stamping here would race under concurrent creates without
	// a schema change, and the event stream answers the question exactly.
	h.Analytics.Capture(analytics.WorkspaceCreated(userID, uuidToString(ws.ID)))

	slog.Info("workspace created", append(logger.RequestAttrs(r), "workspace_id", uuidToString(ws.ID), "name", ws.Name, "slug", ws.Slug)...)
	writeJSON(w, http.StatusCreated, workspaceToResponse(ws))
}

type UpdateWorkspaceRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Context     *string `json:"context"`
	Settings    any     `json:"settings"`
	Repos       any     `json:"repos"`
	IssuePrefix *string `json:"issue_prefix"`
	// ChannelsEnabled is gated by ChannelsEnabledSet so a PATCH that doesn't
	// mention the flag (e.g. a name change) leaves it untouched. Same for
	// ChannelRetentionDays — Set=true with the value=nil clears the override.
	ChannelsEnabled         *bool  `json:"channels_enabled"`
	ChannelsEnabledSet      bool   `json:"channels_enabled_set"`
	ChannelRetentionDays    *int32 `json:"channel_retention_days"`
	ChannelRetentionDaysSet bool   `json:"channel_retention_days_set"`
	// ShipHubEnabled mirrors ChannelsEnabled — paired-bool gate so a PATCH
	// without the field doesn't silently flip the feature off.
	ShipHubEnabled    *bool `json:"ship_hub_enabled"`
	ShipHubEnabledSet bool  `json:"ship_hub_enabled_set"`
	// GitHubToken is write-only. The plaintext token is merged into
	// workspace.settings under ship_hub.github_token; the read path strips
	// it before responding. Empty string clears the stored token.
	// TODO(security): encrypt at rest before Phase 2 (use the same KMS
	// path the daemon-token cache uses). Tracked in the Phase 1 PR.
	GitHubToken    *string `json:"github_token,omitempty"`
	GitHubTokenSet bool    `json:"github_token_set,omitempty"`
	// OrchestratorAgentID + OrchestratorAgentIDSet use the paired-bool pattern
	// so a PATCH can distinguish "don't touch" from "explicitly clear to NULL".
	// Set the bool true to apply; pass null in OrchestratorAgentID to clear.
	OrchestratorAgentID    *string `json:"orchestrator_agent_id,omitempty"`
	OrchestratorAgentIDSet bool    `json:"orchestrator_agent_id_set,omitempty"`
	// Phase 7d follow-up — per-risk-tier approval rule. Each tier
	// uses the paired-bool gate (FooSet=true to apply; missing means
	// "leave alone"). Values are validated against the same enum the
	// SQL CHECK constraint enforces, so a typo never reaches the DB.
	ShipHubApprovalLow         *string `json:"ship_hub_approval_low,omitempty"`
	ShipHubApprovalLowSet      bool    `json:"ship_hub_approval_low_set,omitempty"`
	ShipHubApprovalMedium      *string `json:"ship_hub_approval_medium,omitempty"`
	ShipHubApprovalMediumSet   bool    `json:"ship_hub_approval_medium_set,omitempty"`
	ShipHubApprovalHigh        *string `json:"ship_hub_approval_high,omitempty"`
	ShipHubApprovalHighSet     bool    `json:"ship_hub_approval_high_set,omitempty"`
	ShipHubApprovalCritical    *string `json:"ship_hub_approval_critical,omitempty"`
	ShipHubApprovalCriticalSet bool    `json:"ship_hub_approval_critical_set,omitempty"`
	// ShipHubApproverCanBeAuthor — paired-bool gate for the
	// "verifier may have authored a release PR" toggle.
	ShipHubApproverCanBeAuthor    *bool `json:"ship_hub_approver_can_be_author,omitempty"`
	ShipHubApproverCanBeAuthorSet bool  `json:"ship_hub_approver_can_be_author_set,omitempty"`
	// Phase 7d follow-up — auto-detect deploys by polling GitHub
	// Actions. The poller watches completed runs of these workflow
	// files on main and auto-links matching releases by sha. nil
	// pointer = "do nothing"; empty/whitespace string clears (auto-
	// detection turns off; the manual Mark-deployed button is the
	// fallback). Path is relative to the repo's `.github/workflows/`,
	// e.g. "production.yml".
	ShipHubDeployWorkflowStaging       *string `json:"ship_hub_deploy_workflow_staging,omitempty"`
	ShipHubDeployWorkflowStagingSet    bool    `json:"ship_hub_deploy_workflow_staging_set,omitempty"`
	ShipHubDeployWorkflowProduction    *string `json:"ship_hub_deploy_workflow_production,omitempty"`
	ShipHubDeployWorkflowProductionSet bool    `json:"ship_hub_deploy_workflow_production_set,omitempty"`
}

// validApprovalRule is the single source of truth for accepted rule
// values. Mirrored on:
//   - the SQL CHECK constraint in migration 090
//   - ship.ApprovalRule* constants in the service package
//   - the frontend types/workspace.ts ApprovalRule union
//
// Returns true when `s` is one of the four allowed values, otherwise
// false. The handler rejects unknown values with a 400 so a typo
// from a future client release doesn't silently drop into the DB.
func validApprovalRule(s string) bool {
	switch s {
	case "member", "admin", "approver", "two":
		return true
	}
	return false
}

func (h *Handler) UpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	id := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "workspace id")
	if !ok {
		return
	}

	var req UpdateWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateWorkspaceParams{
		ID: idUUID,
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Context != nil {
		params.Context = pgtype.Text{String: *req.Context, Valid: true}
	}
	if req.Settings != nil {
		s, _ := json.Marshal(req.Settings)
		params.Settings = s
	}
	// Ship Hub GitHub token write path.
	//
	// Phase 2 stores the token encrypted in workspace_secret. The legacy
	// settings path is left in place so a token written by a Phase 1
	// build still round-trips correctly until the startup migrator
	// moves it. New writes ALWAYS land in the encrypted store and the
	// settings JSON is cleared in the same call.
	//
	// nil pointer = "do nothing"; empty string = "clear".
	if req.GitHubTokenSet {
		if req.GitHubToken == nil || *req.GitHubToken == "" {
			_ = h.Queries.DeleteWorkspaceSecret(r.Context(), db.DeleteWorkspaceSecretParams{
				WorkspaceID: idUUID,
				Name:        "github_token",
			})
			_ = h.Queries.ClearShipHubTokenInSettings(r.Context(), idUUID)
		} else {
			ciphertext, err := secretsEncryptString(*req.GitHubToken)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to encrypt token")
				return
			}
			if _, err := h.Queries.UpsertWorkspaceSecret(r.Context(), db.UpsertWorkspaceSecretParams{
				WorkspaceID:    idUUID,
				Name:           "github_token",
				ValueEncrypted: ciphertext,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to store token")
				return
			}
			// Clear the legacy settings copy if present so the encrypted
			// store is the single source of truth going forward.
			_ = h.Queries.ClearShipHubTokenInSettings(r.Context(), idUUID)
		}
	}
	if req.Repos != nil {
		reposJSON, _ := json.Marshal(req.Repos)
		params.Repos = reposJSON
	}
	if req.IssuePrefix != nil {
		prefix := strings.ToUpper(strings.TrimSpace(*req.IssuePrefix))
		if prefix != "" {
			params.IssuePrefix = pgtype.Text{String: prefix, Valid: true}
		}
	}
	params.ChannelsEnabledSet = req.ChannelsEnabledSet
	if req.ChannelsEnabled != nil {
		params.ChannelsEnabled = pgtype.Bool{Bool: *req.ChannelsEnabled, Valid: true}
	}
	params.ChannelRetentionDaysSet = req.ChannelRetentionDaysSet
	if req.ChannelRetentionDays != nil {
		params.ChannelRetentionDays = pgtype.Int4{Int32: *req.ChannelRetentionDays, Valid: true}
	}
	params.ShipHubEnabledSet = req.ShipHubEnabledSet
	if req.ShipHubEnabled != nil {
		params.ShipHubEnabled = pgtype.Bool{Bool: *req.ShipHubEnabled, Valid: true}
	}
	// Approval rule patches. Each tier validates the value against
	// the enum BEFORE persisting so a malformed PATCH lands as a 400
	// rather than the SQL CHECK constraint surfacing a 500.
	if req.ShipHubApprovalLowSet {
		params.ShipHubApprovalLowSet = true
		if req.ShipHubApprovalLow != nil {
			if !validApprovalRule(*req.ShipHubApprovalLow) {
				writeError(w, http.StatusBadRequest, "invalid ship_hub_approval_low value")
				return
			}
			params.ShipHubApprovalLow = pgtype.Text{String: *req.ShipHubApprovalLow, Valid: true}
		}
	}
	if req.ShipHubApprovalMediumSet {
		params.ShipHubApprovalMediumSet = true
		if req.ShipHubApprovalMedium != nil {
			if !validApprovalRule(*req.ShipHubApprovalMedium) {
				writeError(w, http.StatusBadRequest, "invalid ship_hub_approval_medium value")
				return
			}
			params.ShipHubApprovalMedium = pgtype.Text{String: *req.ShipHubApprovalMedium, Valid: true}
		}
	}
	if req.ShipHubApprovalHighSet {
		params.ShipHubApprovalHighSet = true
		if req.ShipHubApprovalHigh != nil {
			if !validApprovalRule(*req.ShipHubApprovalHigh) {
				writeError(w, http.StatusBadRequest, "invalid ship_hub_approval_high value")
				return
			}
			params.ShipHubApprovalHigh = pgtype.Text{String: *req.ShipHubApprovalHigh, Valid: true}
		}
	}
	if req.ShipHubApprovalCriticalSet {
		params.ShipHubApprovalCriticalSet = true
		if req.ShipHubApprovalCritical != nil {
			if !validApprovalRule(*req.ShipHubApprovalCritical) {
				writeError(w, http.StatusBadRequest, "invalid ship_hub_approval_critical value")
				return
			}
			params.ShipHubApprovalCritical = pgtype.Text{String: *req.ShipHubApprovalCritical, Valid: true}
		}
	}
	if req.ShipHubApproverCanBeAuthorSet {
		params.ShipHubApproverCanBeAuthorSet = true
		if req.ShipHubApproverCanBeAuthor != nil {
			params.ShipHubApproverCanBeAuthor = pgtype.Bool{Bool: *req.ShipHubApproverCanBeAuthor, Valid: true}
		}
	}
	// Deploy-workflow patches — paired-bool gate per environment.
	// nil pointer with set=true clears the column (auto-detection off);
	// non-empty string sets it. We TrimSpace to keep accidental newlines
	// (common when the user pastes from a YAML file) from breaking the
	// poller's URL path.
	if req.ShipHubDeployWorkflowStagingSet {
		params.ShipHubDeployWorkflowStagingSet = true
		if req.ShipHubDeployWorkflowStaging != nil {
			trimmed := strings.TrimSpace(*req.ShipHubDeployWorkflowStaging)
			if trimmed == "" {
				params.ShipHubDeployWorkflowStaging = pgtype.Text{}
			} else {
				params.ShipHubDeployWorkflowStaging = pgtype.Text{String: trimmed, Valid: true}
			}
		}
	}
	if req.ShipHubDeployWorkflowProductionSet {
		params.ShipHubDeployWorkflowProductionSet = true
		if req.ShipHubDeployWorkflowProduction != nil {
			trimmed := strings.TrimSpace(*req.ShipHubDeployWorkflowProduction)
			if trimmed == "" {
				params.ShipHubDeployWorkflowProduction = pgtype.Text{}
			} else {
				params.ShipHubDeployWorkflowProduction = pgtype.Text{String: trimmed, Valid: true}
			}
		}
	}
	if req.OrchestratorAgentIDSet {
		params.OrchestratorAgentIDSet = true
		if req.OrchestratorAgentID != nil && *req.OrchestratorAgentID != "" {
			agentUUID, ok := parseUUIDOrBadRequest(w, *req.OrchestratorAgentID, "orchestrator_agent_id")
			if !ok {
				return
			}
			// Verify the orchestrator agent belongs to this workspace —
			// otherwise a malicious or misconfigured client could point at
			// an agent in a different workspace and trigger cross-workspace
			// dispatch.
			agent, err := h.Queries.GetAgent(r.Context(), agentUUID)
			if err != nil || agent.WorkspaceID != idUUID {
				writeError(w, http.StatusBadRequest, "orchestrator_agent_id must reference an agent in this workspace")
				return
			}
			if agent.ArchivedAt.Valid {
				writeError(w, http.StatusBadRequest, "orchestrator_agent_id must reference a non-archived agent")
				return
			}
			params.OrchestratorAgentID = agentUUID
		}
		// nil OrchestratorAgentID with set=true clears the pointer.
	}

	ws, err := h.Queries.UpdateWorkspace(r.Context(), params)
	if err != nil {
		slog.Warn("update workspace failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to update workspace: "+err.Error())
		return
	}

	slog.Info("workspace updated", append(logger.RequestAttrs(r), "workspace_id", id)...)
	userID := requestUserID(r)
	full := h.workspaceToResponseFull(r.Context(), r, ws)
	h.publish(protocol.EventWorkspaceUpdated, uuidToString(ws.ID), "member", userID, map[string]any{"workspace": full})

	writeJSON(w, http.StatusOK, full)
}

func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}

	members, err := h.Queries.ListMembers(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}

	resp := make([]MemberResponse, len(members))
	for i, m := range members {
		resp[i] = memberToResponse(m)
	}

	writeJSON(w, http.StatusOK, resp)
}

type MemberWithUserResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	UserID      string  `json:"user_id"`
	Role        string  `json:"role"`
	CreatedAt   string  `json:"created_at"`
	Name        string  `json:"name"`
	Email       string  `json:"email"`
	AvatarURL   *string `json:"avatar_url"`
}

func (h *Handler) ListMembersWithUser(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	members, err := h.Queries.ListMembersWithUser(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}

	resp := make([]MemberWithUserResponse, len(members))
	for i, m := range members {
		resp[i] = MemberWithUserResponse{
			ID:          uuidToString(m.ID),
			WorkspaceID: uuidToString(m.WorkspaceID),
			UserID:      uuidToString(m.UserID),
			Role:        m.Role,
			CreatedAt:   timestampToString(m.CreatedAt),
			Name:        m.UserName,
			Email:       m.UserEmail,
			AvatarURL:   textToPtr(m.UserAvatarUrl),
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

type CreateMemberRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func memberWithUserResponse(member db.Member, user db.User) MemberWithUserResponse {
	return MemberWithUserResponse{
		ID:          uuidToString(member.ID),
		WorkspaceID: uuidToString(member.WorkspaceID),
		UserID:      uuidToString(member.UserID),
		Role:        member.Role,
		CreatedAt:   timestampToString(member.CreatedAt),
		Name:        user.Name,
		Email:       user.Email,
		AvatarURL:   textToPtr(user.AvatarUrl),
	}
}

func normalizeMemberRole(role string) (string, bool) {
	if role == "" {
		return "member", true
	}

	role = strings.TrimSpace(role)
	switch role {
	case "owner", "admin", "member":
		return role, true
	default:
		return "", false
	}
}

func (h *Handler) CreateMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	var req CreateMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	role, valid := normalizeMemberRole(req.Role)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid member role")
		return
	}
	if role == "owner" && requester.Role != "owner" {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	user, err := h.Queries.GetUserByEmail(r.Context(), email)
	if err != nil {
		if isNotFound(err) {
			// Auto-create user with email so they can be invited before signing up
			user, err = h.Queries.CreateUser(r.Context(), db.CreateUserParams{
				Name:  email,
				Email: email,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to create user")
				return
			}
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load user")
			return
		}
	}

	member, err := h.Queries.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: requester.WorkspaceID,
		UserID:      user.ID,
		Role:        role,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "user is already a member")
			return
		}
		slog.Warn("create member failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID, "email", email)...)
		writeError(w, http.StatusInternalServerError, "failed to create member")
		return
	}

	slog.Info("member added", append(logger.RequestAttrs(r), "member_id", uuidToString(member.ID), "workspace_id", workspaceID, "email", email, "role", role)...)
	userID := requestUserID(r)
	eventPayload := map[string]any{"member": memberWithUserResponse(member, user)}
	if ws, err := h.Queries.GetWorkspace(r.Context(), requester.WorkspaceID); err == nil {
		eventPayload["workspace_name"] = ws.Name
	}
	h.publish(protocol.EventMemberAdded, uuidToString(requester.WorkspaceID), "member", userID, eventPayload)

	writeJSON(w, http.StatusCreated, memberWithUserResponse(member, user))
}

type UpdateMemberRequest struct {
	Role string `json:"role"`
}

func (h *Handler) UpdateMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	memberID := chi.URLParam(r, "memberId")
	memberUUID, ok := parseUUIDOrBadRequest(w, memberID, "member id")
	if !ok {
		return
	}
	target, err := h.Queries.GetMember(r.Context(), memberUUID)
	if err != nil || uuidToString(target.WorkspaceID) != uuidToString(requester.WorkspaceID) {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}

	var req UpdateMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Role) == "" {
		writeError(w, http.StatusBadRequest, "role is required")
		return
	}

	role, valid := normalizeMemberRole(req.Role)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid member role")
		return
	}

	if (target.Role == "owner" || role == "owner") && requester.Role != "owner" {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if target.Role == "owner" && role != "owner" {
		members, err := h.Queries.ListMembers(r.Context(), target.WorkspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update member")
			return
		}
		if countOwners(members) <= 1 {
			writeError(w, http.StatusBadRequest, "workspace must have at least one owner")
			return
		}
	}

	updatedMember, err := h.Queries.UpdateMemberRole(r.Context(), db.UpdateMemberRoleParams{
		ID:   target.ID,
		Role: role,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update member")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), updatedMember.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load member")
		return
	}

	userID := requestUserID(r)
	h.publish(protocol.EventMemberUpdated, uuidToString(requester.WorkspaceID), "member", userID, map[string]any{
		"member": memberWithUserResponse(updatedMember, user),
	})

	writeJSON(w, http.StatusOK, memberWithUserResponse(updatedMember, user))
}

func (h *Handler) DeleteMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	memberID := chi.URLParam(r, "memberId")
	memberUUID, ok := parseUUIDOrBadRequest(w, memberID, "member id")
	if !ok {
		return
	}
	target, err := h.Queries.GetMember(r.Context(), memberUUID)
	if err != nil || uuidToString(target.WorkspaceID) != uuidToString(requester.WorkspaceID) {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}

	if target.Role == "owner" && requester.Role != "owner" {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if target.Role == "owner" {
		members, err := h.Queries.ListMembers(r.Context(), target.WorkspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete member")
			return
		}
		if countOwners(members) <= 1 {
			writeError(w, http.StatusBadRequest, "workspace must have at least one owner")
			return
		}
	}

	requesterUserID := requestUserID(r)
	result, err := h.revokeAndRemoveMember(r.Context(), target.WorkspaceID, target.UserID, target.ID, parseUUID(requesterUserID))
	if err != nil {
		slog.Warn("delete member failed", append(logger.RequestAttrs(r), "error", err, "member_id", memberID, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete member")
		return
	}

	wsIDStr := uuidToString(requester.WorkspaceID)
	logRevocation(result, wsIDStr, uuidToString(target.UserID))
	h.publishRevocation(r.Context(), result, wsIDStr, "member", requesterUserID)

	slog.Info("member removed", append(logger.RequestAttrs(r), "member_id", uuidToString(target.ID), "workspace_id", workspaceID, "user_id", uuidToString(target.UserID))...)
	h.publish(protocol.EventMemberRemoved, wsIDStr, "member", requesterUserID, map[string]any{
		"member_id":    uuidToString(target.ID),
		"workspace_id": wsIDStr,
		"user_id":      uuidToString(target.UserID),
	})

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) LeaveWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	if member.Role == "owner" {
		members, err := h.Queries.ListMembers(r.Context(), member.WorkspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to leave workspace")
			return
		}
		if countOwners(members) <= 1 {
			writeError(w, http.StatusBadRequest, "workspace must have at least one owner")
			return
		}
	}

	result, err := h.revokeAndRemoveMember(r.Context(), member.WorkspaceID, member.UserID, member.ID, member.UserID)
	if err != nil {
		slog.Warn("leave workspace failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to leave workspace")
		return
	}

	userID := requestUserID(r)
	logRevocation(result, workspaceID, uuidToString(member.UserID))
	h.publishRevocation(r.Context(), result, workspaceID, "member", userID)

	slog.Info("member removed", append(logger.RequestAttrs(r), "member_id", uuidToString(member.ID), "workspace_id", workspaceID, "user_id", uuidToString(member.UserID))...)
	h.publish(protocol.EventMemberRemoved, workspaceID, "member", userID, map[string]any{
		"member_id":    uuidToString(member.ID),
		"workspace_id": workspaceID,
		"user_id":      uuidToString(member.UserID),
	})

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")

	// Defense in depth: the route is already gated by the
	// RequireWorkspaceRoleFromURL("owner") middleware, but we re-check here
	// so that the handler is safe regardless of how it gets wired up
	// (direct calls in tests, future router refactors, etc.).
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if requester.Role != "owner" {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	// At this point workspaceMember has resolved → workspaceID is a valid UUID
	// (the lookup would have errored otherwise), so reuse the resolved value.
	if err := h.Queries.DeleteWorkspace(r.Context(), requester.WorkspaceID); err != nil {
		slog.Warn("delete workspace failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete workspace")
		return
	}

	slog.Info("workspace deleted", append(logger.RequestAttrs(r), "workspace_id", workspaceID)...)
	h.publish(protocol.EventWorkspaceDeleted, workspaceID, "member", requestUserID(r), map[string]any{
		"workspace_id": workspaceID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// mergeShipHubToken merges a new github_token value into a workspace settings
// JSON blob.
//
//   - currentSettings: the existing row's settings (used so we don't clobber
//     other ship_hub.* fields the client wasn't editing).
//   - patchSettings:   the settings the request body included (may be nil).
//                      Caller-provided values win over current except for the
//                      token, which is always taken from `token`.
//   - token:           pointer-of-string. nil means "do nothing"; an empty
//                      string clears the stored token.
//
// Returns the marshalled JSON to write back. Never returns nil so the column
// always holds a valid JSON object.
func mergeShipHubToken(currentSettings, patchSettings []byte, token *string) []byte {
	merged := map[string]any{}
	// Start from current — the request body is a full replacement of the
	// settings object today, but we re-merge defensively so future fields
	// the client didn't include don't get wiped by a token-only PATCH.
	if len(currentSettings) > 0 {
		_ = json.Unmarshal(currentSettings, &merged)
	}
	if len(patchSettings) > 0 {
		var patch map[string]any
		if err := json.Unmarshal(patchSettings, &patch); err == nil {
			for k, v := range patch {
				merged[k] = v
			}
		}
	}
	sub, _ := merged[shipHubSettingsKey].(map[string]any)
	if sub == nil {
		sub = map[string]any{}
	}
	if token != nil {
		if *token == "" {
			delete(sub, "github_token")
		} else {
			sub["github_token"] = *token
		}
	}
	if len(sub) == 0 {
		delete(merged, shipHubSettingsKey)
	} else {
		merged[shipHubSettingsKey] = sub
	}
	out, _ := json.Marshal(merged)
	return out
}
