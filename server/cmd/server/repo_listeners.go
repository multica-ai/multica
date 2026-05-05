package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// RepoApprovalWebhookConfig configures the optional outbound webhook fired
// when new workspace repos are saved. URL is required. Secret and HeaderName
// must be supplied together — providing one without the other is rejected
// at registration time, so receivers never get a half-configured request
// (e.g. a header with no value, or a secret sent under an unintended name).
type RepoApprovalWebhookConfig struct {
	URL        string
	Secret     string
	HeaderName string
}

// registerRepoApprovalWebhook subscribes to workspace_repos:created and POSTs
// the new repo URLs to an external approval service. The external service is
// expected to evaluate each URL and call back via `multica repo approve <url>`
// for the ones it allows.
//
// The subscription is only registered when cfg.URL is set to a non-empty
// value — without it the feature is fully disabled and the listener does
// not exist on the bus.
func registerRepoApprovalWebhook(bus *events.Bus, cfg RepoApprovalWebhookConfig, httpClient *http.Client) {
	cfg.URL = strings.TrimSpace(cfg.URL)
	if cfg.URL == "" {
		return
	}
	cfg.Secret = strings.TrimSpace(cfg.Secret)
	cfg.HeaderName = strings.TrimSpace(cfg.HeaderName)
	if (cfg.Secret == "") != (cfg.HeaderName == "") {
		slog.Error("repo approval webhook: REPO_APPROVAL_WEBHOOK_SECRET and REPO_APPROVAL_WEBHOOK_HEADER must be set together; refusing to register webhook")
		return
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	bus.Subscribe(protocol.EventWorkspaceReposCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		repos, ok := payload["repos"]
		if !ok {
			return
		}

		body, err := json.Marshal(map[string]any{
			"workspace_id": e.WorkspaceID,
			"repos":        repos,
		})
		if err != nil {
			slog.Error("repo approval webhook: failed to marshal payload", "error", err, "workspace_id", e.WorkspaceID)
			return
		}

		// Detached from the request lifecycle: the publisher's ctx is gone by
		// the time we run, and we don't want a slow webhook to block the
		// HTTP response that triggered the publish.
		go postRepoApprovalWebhook(httpClient, cfg, body, e.WorkspaceID)
	})
}

func postRepoApprovalWebhook(client *http.Client, cfg RepoApprovalWebhookConfig, body []byte, workspaceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		slog.Error("repo approval webhook: failed to build request", "error", err, "workspace_id", workspaceID)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Secret != "" {
		req.Header.Set(cfg.HeaderName, cfg.Secret)
	}

	resp, err := client.Do(req)
	if err != nil {
		slog.Error("repo approval webhook: request failed", "error", err, "workspace_id", workspaceID, "url", cfg.URL)
		return
	}
	defer func() {
		// Drain so the underlying connection can be reused by the keep-alive pool.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		slog.Warn("repo approval webhook: non-2xx response", "status", resp.StatusCode, "workspace_id", workspaceID, "url", cfg.URL)
		return
	}
	slog.Info("repo approval webhook: delivered", "status", resp.StatusCode, "workspace_id", workspaceID)
}
