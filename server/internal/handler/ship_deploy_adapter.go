package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/deploy"
	// Side-effect import: registers all built-in adapters via init().
	_ "github.com/multica-ai/multica/server/pkg/deploy/adapters"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/secrets"
)

// ListDeployAdapters returns the names of every registered deploy
// adapter. Drives the env-config dialog dropdown so adding a new
// adapter is purely server-side — the frontend reads the list at
// runtime.
func (h *Handler) ListDeployAdapters(w http.ResponseWriter, r *http.Request) {
	_, _, ok := h.requireShipHubEnabled(w, r)
	if !ok {
		return
	}
	names := deploy.Names()
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		a, err := deploy.Get(name)
		if err != nil {
			continue
		}
		out = append(out, map[string]any{
			"kind":              name,
			"supports_poll":     a.SupportsPoll(),
			"supports_rollback": a.SupportsRollback(),
			// Webhook URL is the public address users paste into the
			// provider's UI. Mirrors the GitHub webhook URL convention.
			"webhook_url": deployAdapterWebhookURL(name),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"adapters": out})
}

// ConfigureDeployAdapterRequest is the body for
// PUT /api/deploy_environments/{id}/adapter. Either the workspace owner
// is configuring this env for a non-default adapter for the first time,
// or they're rotating a stored credential / secret.
type ConfigureDeployAdapterRequest struct {
	AdapterKind string `json:"adapter_kind"`
	// Config is whatever the adapter expects (JSON object). Encoded as
	// a JSON object on the wire, transcoded to a json.RawMessage for
	// encryption.
	Config json.RawMessage `json:"config"`
	// WebhookSecret is optional — caller supplies it when (re-)setting
	// the inbound webhook signing secret. Empty / omitted = leave the
	// previously stored secret alone.
	WebhookSecret string `json:"webhook_secret,omitempty"`
}

// ConfigureDeployAdapter persists the adapter kind + encrypted config.
// Owner-only by router middleware; the handler additionally checks the
// adapter kind is registered so an unknown value can't land in the DB.
func (h *Handler) ConfigureDeployAdapter(w http.ResponseWriter, r *http.Request) {
	env, _, ok := h.loadDeployEnvironment(w, r)
	if !ok {
		return
	}
	var req ConfigureDeployAdapterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	kind := strings.TrimSpace(req.AdapterKind)
	if kind == "" {
		writeError(w, http.StatusBadRequest, "adapter_kind is required")
		return
	}
	if _, err := deploy.Get(kind); err != nil {
		writeError(w, http.StatusBadRequest, "unknown adapter_kind: "+kind)
		return
	}
	if len(req.Config) == 0 {
		// Empty config is acceptable for github_actions (no per-env
		// config) but for everything else it's a misconfiguration.
		req.Config = json.RawMessage(`{}`)
	}

	encryptedConfig, err := secrets.EncryptString(string(req.Config))
	if err != nil {
		slog.Warn("ship deploy adapter: encrypt config failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to encrypt config")
		return
	}

	var encryptedSecret []byte
	if req.WebhookSecret != "" {
		enc, err := secrets.EncryptString(req.WebhookSecret)
		if err != nil {
			slog.Warn("ship deploy adapter: encrypt webhook secret failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to encrypt webhook secret")
			return
		}
		encryptedSecret = enc
	}

	if _, err := h.Queries.UpsertDeployAdapterConfig(r.Context(), db.UpsertDeployAdapterConfigParams{
		EnvironmentID:          env.ID,
		AdapterKind:            kind,
		ConfigEncrypted:        encryptedConfig,
		WebhookSecretEncrypted: encryptedSecret,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist adapter config")
		return
	}
	if err := h.Queries.SetDeployEnvironmentAdapterKind(r.Context(), db.SetDeployEnvironmentAdapterKindParams{
		ID:          env.ID,
		AdapterKind: kind,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update environment adapter kind")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"environment_id":   uuidToString(env.ID),
		"adapter_kind":     kind,
		"webhook_url":      deployAdapterWebhookURL(kind),
		"webhook_secret_set": len(encryptedSecret) > 0,
	})
}

// PollDeployEnvironment forces a poll against the env's configured
// adapter. Returns 400 with ErrPollNotSupported when the adapter
// doesn't expose a poll path. Members can poll; rollback (below) is
// owner-only.
func (h *Handler) PollDeployEnvironment(w http.ResponseWriter, r *http.Request) {
	env, wsID, ok := h.loadDeployEnvironment(w, r)
	if !ok {
		return
	}
	adapter, adapterEnv, ok := h.resolveAdapterForEnvironment(w, r.Context(), env)
	if !ok {
		return
	}
	if !adapter.SupportsPoll() {
		writeError(w, http.StatusBadRequest, "polling not supported by this adapter")
		return
	}
	state, err := adapter.PollCurrent(r.Context(), adapterEnv)
	if err != nil {
		if errors.Is(err, deploy.ErrPollNotSupported) {
			writeError(w, http.StatusBadRequest, "polling not supported by this adapter")
			return
		}
		writeError(w, http.StatusBadGateway, "poll failed: "+err.Error())
		return
	}
	if state == nil {
		writeJSON(w, http.StatusOK, map[string]any{"current": nil})
		return
	}
	// If the current SHA differs from what we have stored, record a new
	// deploy row and bump the env's current_sha. Idempotent: a poll that
	// returns the same SHA is a no-op.
	if env.CurrentSha.Valid && env.CurrentSha.String == state.CurrentSHA {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_sha":         state.CurrentSHA,
			"current_deployed_at": state.DeployedAt.Format(time.RFC3339),
			"changed":             false,
		})
		return
	}
	if state.CurrentSHA != "" {
		_, _ = h.Queries.InsertDeploy(r.Context(), db.InsertDeployParams{
			WorkspaceID:   wsID,
			EnvironmentID: env.ID,
			Ref:           env.TargetBranch,
			Sha:           state.CurrentSHA,
			Status:        db.DeployStatusSucceeded,
			TriggeredBy:   pgtype.UUID{},
			StartedAt:     pgtype.Timestamptz{Time: state.DeployedAt, Valid: !state.DeployedAt.IsZero()},
			CompletedAt:   pgtype.Timestamptz{Time: state.DeployedAt, Valid: !state.DeployedAt.IsZero()},
			LogUrl:        pgtype.Text{String: state.LogURL, Valid: state.LogURL != ""},
			// Adapter learned the SHA via a provider API call. Same
			// shape as the workflow_run path; adapter calls happen
			// over the wire, not over a manual click.
			Provenance:    db.DeployProvenanceWorkflowRun,
			ProvenanceRef: pgtype.Text{String: state.LogURL, Valid: state.LogURL != ""},
		})
		_, _ = h.Queries.RecomputeEnvCurrentFromDeploys(r.Context(), env.ID)
	}
	h.publish(protocol.EventDeployCompleted, uuidToString(wsID), "system", "", map[string]any{
		"environment_id": uuidToString(env.ID),
		"sha":            state.CurrentSHA,
		"status":         "succeeded",
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"current_sha":         state.CurrentSHA,
		"current_deployed_at": state.DeployedAt.Format(time.RFC3339),
		"changed":             true,
	})
}

// RollbackDeployRequest body.
type RollbackDeployRequest struct {
	TargetSHA string `json:"target_sha"`
}

// RollbackDeployEnvironment dispatches a rollback through the env's
// adapter. Owner/admin-gated by middleware (registered in router.go).
func (h *Handler) RollbackDeployEnvironment(w http.ResponseWriter, r *http.Request) {
	env, wsID, ok := h.loadDeployEnvironment(w, r)
	if !ok {
		return
	}
	var req RollbackDeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.TargetSHA) == "" {
		writeError(w, http.StatusBadRequest, "target_sha is required")
		return
	}
	adapter, adapterEnv, ok := h.resolveAdapterForEnvironment(w, r.Context(), env)
	if !ok {
		return
	}
	if !adapter.SupportsRollback() {
		writeError(w, http.StatusBadRequest, "rollback not supported by this adapter")
		return
	}
	if err := adapter.Rollback(r.Context(), adapterEnv, req.TargetSHA); err != nil {
		if errors.Is(err, deploy.ErrRollbackNotSupported) {
			writeError(w, http.StatusBadRequest, "rollback not supported by this adapter")
			return
		}
		writeError(w, http.StatusBadGateway, "rollback failed: "+err.Error())
		return
	}
	// Record the user-initiated rollback as a deploy row in
	// rolled_back state. The provider's webhook (or the next poll) will
	// flip it to succeeded once the redeploy completes.
	userID := requestUserID(r)
	creator, _ := h.parseUserUUIDOrZero(userID)
	deployRow, _ := h.Queries.InsertDeploy(r.Context(), db.InsertDeployParams{
		WorkspaceID:   wsID,
		EnvironmentID: env.ID,
		Ref:           env.TargetBranch,
		Sha:           strings.TrimSpace(req.TargetSHA),
		Status:        db.DeployStatusRolledBack,
		TriggeredBy:   creator,
		StartedAt:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
		CompletedAt:   pgtype.Timestamptz{},
		// User-initiated rollback dispatch — manual_assertion with a
		// canned ref noting the target SHA.
		Provenance:    db.DeployProvenanceManualAssertion,
		ProvenanceRef: pgtype.Text{String: "rollback to " + strings.TrimSpace(req.TargetSHA), Valid: true},
	})
	h.publish(protocol.EventDeployStarted, uuidToString(wsID), "member", userID, map[string]any{
		"environment_id": uuidToString(env.ID),
		"deploy":         deployToResponse(deployRow),
		"rollback":       true,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"environment_id": uuidToString(env.ID),
		"target_sha":     req.TargetSHA,
		"deploy_id":      uuidToString(deployRow.ID),
	})
}

// HandleDeployAdapterWebhook is the multi-adapter webhook receiver.
// URL pattern: /api/integrations/deploy/{adapter}/webhook.
//
// Flow mirrors HandleGitHubWebhook (Phase 2):
//   1. Read body (capped) + look up the adapter by URL slug.
//   2. Iterate every deploy_adapter_config row with matching kind.
//      For each, decrypt the config + secret and let the adapter try
//      VerifySignature; the first env whose signature matches OWNS the
//      delivery.
//   3. Dispatch OnWebhook on the matched env.
//   4. Translate the resulting DeployEvent into deploy row updates.
//
// We deliberately scan all candidate envs (rather than letting the
// client tell us which env it belongs to) — an unauthenticated webhook
// header can't be trusted. At Ship Hub's scale (small teams, few envs
// per adapter), the linear scan is fine.
func (h *Handler) HandleDeployAdapterWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	if len(body) > maxWebhookBody {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	kind := chi.URLParam(r, "adapter")
	adapter, err := deploy.Get(kind)
	if err != nil {
		writeError(w, http.StatusNotFound, "unknown adapter")
		return
	}
	// github_actions has its own dedicated receiver; sending a payload
	// here is a configuration mistake we surface explicitly.
	if kind == "github_actions" {
		writeError(w, http.StatusBadRequest, "github_actions webhooks are received at /api/integrations/github/webhook")
		return
	}
	matchedEnv, adapterEnv, ok := h.findEnvByAdapterSignature(r.Context(), adapter, kind, r.Header, body)
	if !ok {
		writeError(w, http.StatusUnauthorized, "signature verification failed")
		return
	}
	go h.dispatchDeployAdapterWebhook(adapter, matchedEnv, adapterEnv, body)
	writeJSON(w, http.StatusOK, map[string]any{"received": true})
}

// findEnvByAdapterSignature does the per-env signature scan described in
// HandleDeployAdapterWebhook's flow comment. Returns the matched
// db.DeployEnvironment plus the populated *deploy.Environment ready for
// dispatch.
func (h *Handler) findEnvByAdapterSignature(
	ctx context.Context,
	adapter deploy.Adapter,
	kind string,
	headers http.Header,
	body []byte,
) (db.DeployEnvironment, *deploy.Environment, bool) {
	configs, err := h.Queries.ListDeployAdapterConfigsByKind(ctx, kind)
	if err != nil {
		slog.Warn("ship deploy adapter: list configs failed", "kind", kind, "error", err)
		return db.DeployEnvironment{}, nil, false
	}
	for _, cfg := range configs {
		env, err := h.Queries.GetDeployEnvironment(ctx, cfg.EnvironmentID)
		if err != nil {
			continue
		}
		configPlain, err := secrets.DecryptString(cfg.ConfigEncrypted)
		if err != nil {
			slog.Warn("ship deploy adapter: decrypt config failed",
				"environment_id", cfg.EnvironmentID, "error", err)
			continue
		}
		var secretPlain string
		if len(cfg.WebhookSecretEncrypted) > 0 {
			s, err := secrets.DecryptString(cfg.WebhookSecretEncrypted)
			if err == nil {
				secretPlain = s
			}
		}
		adapterEnv := &deploy.Environment{
			ID:            env.ID,
			WorkspaceID:   env.WorkspaceID,
			AdapterKind:   env.AdapterKind,
			Config:        json.RawMessage(configPlain),
			WebhookSecret: secretPlain,
			TargetBranch:  env.TargetBranch,
			Name:          env.Name,
		}
		if err := adapter.VerifySignature(adapterEnv, headers, body); err != nil {
			continue
		}
		return env, adapterEnv, true
	}
	return db.DeployEnvironment{}, nil, false
}

// dispatchDeployAdapterWebhook is the async worker. The HTTP path
// already returned 200; we run OnWebhook on a fresh background context
// so a slow provider call doesn't pin the goroutine forever.
func (h *Handler) dispatchDeployAdapterWebhook(
	adapter deploy.Adapter,
	matchedEnv db.DeployEnvironment,
	adapterEnv *deploy.Environment,
	body []byte,
) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("ship deploy adapter: panic in dispatch",
				"adapter", adapter.Name(), "panic", r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	event, err := adapter.OnWebhook(ctx, adapterEnv, body)
	if err != nil {
		if errors.Is(err, deploy.ErrIrrelevantPayload) {
			return
		}
		slog.Warn("ship deploy adapter: OnWebhook failed",
			"adapter", adapter.Name(), "environment_id", matchedEnv.ID, "error", err)
		return
	}
	if event == nil {
		return
	}
	h.applyDeployEvent(ctx, matchedEnv, event)
}

// applyDeployEvent translates an adapter-emitted DeployEvent into DB
// writes + a WS event. Idempotent on identical (env, sha) pairs because
// the InsertDeploy call inserts a new attempt row and the env's
// current_sha bump is conditional on terminal success.
func (h *Handler) applyDeployEvent(ctx context.Context, env db.DeployEnvironment, event *deploy.DeployEvent) {
	status, ok := normalizeDeployStatus(event.Status)
	if !ok {
		// Adapter returned a non-canonical status string. Best to
		// record the SHA at "pending" so the timeline still shows
		// activity, but log so we can spot adapter bugs in production.
		slog.Warn("ship deploy adapter: unknown status",
			"adapter_kind", env.AdapterKind, "status", event.Status)
		status = db.DeployStatusPending
	}
	occurred := event.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now()
	}
	startedAt := pgtype.Timestamptz{}
	completedAt := pgtype.Timestamptz{}
	switch status {
	case db.DeployStatusInProgress:
		startedAt = pgtype.Timestamptz{Time: occurred, Valid: true}
	case db.DeployStatusSucceeded, db.DeployStatusFailed, db.DeployStatusRolledBack:
		startedAt = pgtype.Timestamptz{Time: occurred, Valid: true}
		completedAt = pgtype.Timestamptz{Time: occurred, Valid: true}
	}
	deployRow, err := h.Queries.InsertDeploy(ctx, db.InsertDeployParams{
		WorkspaceID:   env.WorkspaceID,
		EnvironmentID: env.ID,
		Ref:           strings.TrimSpace(firstNonEmptyString(event.Ref, env.TargetBranch)),
		Sha:           strings.TrimSpace(event.SHA),
		Status:        status,
		TriggeredBy:   pgtype.UUID{},
		StartedAt:     startedAt,
		CompletedAt:   completedAt,
		LogUrl:        pgtype.Text{String: event.LogURL, Valid: event.LogURL != ""},
		ErrorMessage:  pgtype.Text{String: event.ErrorMsg, Valid: event.ErrorMsg != ""},
		// Provenance: webhook from a deploy adapter (GitHub deployments,
		// Vercel, Netlify, etc.). The webhook payload is the evidence.
		Provenance:    db.DeployProvenanceWebhook,
		ProvenanceRef: pgtype.Text{String: event.LogURL, Valid: event.LogURL != ""},
	})
	if err != nil {
		slog.Warn("ship deploy adapter: insert deploy failed",
			"environment_id", env.ID, "error", err)
		return
	}
	if status == db.DeployStatusSucceeded && event.SHA != "" {
		if _, err := h.Queries.RecomputeEnvCurrentFromDeploys(ctx, env.ID); err != nil {
			slog.Warn("ship deploy adapter: recompute current sha failed",
				"environment_id", env.ID, "error", err)
		}
	}
	eventName := protocol.EventDeployStarted
	if status == db.DeployStatusSucceeded || status == db.DeployStatusFailed || status == db.DeployStatusRolledBack {
		eventName = protocol.EventDeployCompleted
	}
	h.publish(eventName, uuidToString(env.WorkspaceID), "system", "", map[string]any{
		"environment_id": uuidToString(env.ID),
		"deploy":         deployToResponse(deployRow),
		"sha":            event.SHA,
		"status":         string(status),
	})
}

// resolveAdapterForEnvironment loads the encrypted adapter config for
// an env and returns the registered adapter + its runtime Environment.
// Used by the poll + rollback handlers; the webhook receiver uses
// findEnvByAdapterSignature instead because it has to scan all envs.
func (h *Handler) resolveAdapterForEnvironment(w http.ResponseWriter, ctx context.Context, env db.DeployEnvironment) (deploy.Adapter, *deploy.Environment, bool) {
	kind := env.AdapterKind
	if kind == "" {
		kind = "github_actions"
	}
	adapter, err := deploy.Get(kind)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unknown adapter_kind: "+kind)
		return nil, nil, false
	}
	cfg, err := h.Queries.GetDeployAdapterConfig(ctx, env.ID)
	configPlain := json.RawMessage(`{}`)
	secretPlain := ""
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to load adapter config")
			return nil, nil, false
		}
		// No row yet — only valid for github_actions (default).
		if kind != "github_actions" {
			writeError(w, http.StatusBadRequest, "adapter not configured for this environment")
			return nil, nil, false
		}
	} else {
		plain, err := secrets.DecryptString(cfg.ConfigEncrypted)
		if err == nil {
			configPlain = json.RawMessage(plain)
		}
		if len(cfg.WebhookSecretEncrypted) > 0 {
			if s, err := secrets.DecryptString(cfg.WebhookSecretEncrypted); err == nil {
				secretPlain = s
			}
		}
	}
	return adapter, &deploy.Environment{
		ID:            env.ID,
		WorkspaceID:   env.WorkspaceID,
		AdapterKind:   kind,
		Config:        configPlain,
		WebhookSecret: secretPlain,
		TargetBranch:  env.TargetBranch,
		Name:          env.Name,
	}, true
}

// firstNonEmptyString picks the first non-empty trimmed string. Local
// helper to avoid pulling in the pkg/deploy/adapters internal helper.
func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// deployAdapterWebhookURL is the public URL pattern for the multi-
// adapter receiver. Mirrors webhookPublicURL() in ship_webhook.go but
// includes the adapter kind in the path.
func deployAdapterWebhookURL(kind string) string {
	base := strings.TrimRight(os.Getenv("MULTICA_API_BASE_URL"), "/")
	if base == "" {
		base = "http://localhost:8080"
	}
	return fmt.Sprintf("%s/api/integrations/deploy/%s/webhook", base, kind)
}
