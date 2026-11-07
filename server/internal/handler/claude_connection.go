package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/claudemanagedruntime"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The Claude Managed Agents connection mirrors the Qoder one (qoder_connection.go):
// one encrypted cloud_agent_connection row per workspace, and a server-run
// bridge supervised under a database advisory lock. The Anthropic API base is
// fixed, so a connection cannot be pointed at another host.

const (
	claudeManagedDaemonPrefix = "claude-managed-"
	// claudeConnectionLockSeed keys the per-workspace pg_advisory_xact_lock
	// that serializes connection writes and bridge steps.
	claudeConnectionLockSeed = 23
	defaultClaudeConcurrency = 3
	defaultClaudeRunTimeout  = 60
	maxClaudeRunTimeout      = 240
)

type claudeSettings struct {
	Name              string            `json:"name"`
	EnvironmentID     string            `json:"environment_id"`
	AnthropicToken    string            `json:"anthropic_token,omitempty"`
	GitHubTokens      map[string]string `json:"github_tokens,omitempty"`
	VaultIDs          []string          `json:"vault_ids,omitempty"`
	MulticaToken      string            `json:"multica_token,omitempty"`
	MaxConcurrency    int               `json:"max_concurrency,omitempty"`
	RunTimeoutMinutes int               `json:"run_timeout_minutes,omitempty"`
}

func (s claudeSettings) withDefaults() claudeSettings {
	if s.MaxConcurrency == 0 {
		s.MaxConcurrency = defaultClaudeConcurrency
	}
	if s.RunTimeoutMinutes == 0 {
		s.RunTimeoutMinutes = defaultClaudeRunTimeout
	}
	return s
}

type claudeSettingsView struct {
	Configured        bool     `json:"configured"`
	Available         bool     `json:"available"`
	Name              string   `json:"name"`
	EnvironmentID     string   `json:"environment_id"`
	HasToken          bool     `json:"has_token"`
	Repositories      []string `json:"repositories"`
	VaultIDs          []string `json:"vault_ids"`
	Enabled           bool     `json:"enabled"`
	Status            string   `json:"status"`
	LastError         string   `json:"last_error"`
	MaxConcurrency    int      `json:"max_concurrency"`
	RunTimeoutMinutes int      `json:"run_timeout_minutes"`
	ActiveRuns        int      `json:"active_runs"`
	QueuedRuns        int      `json:"queued_runs"`
}

func (h *Handler) readClaudeSettings(ctx context.Context, ws string) (claudeSettings, bool, string, string, error) {
	var encrypted, status, lastError string
	var enabled bool
	err := h.DB.QueryRow(ctx, `SELECT config_encrypted, enabled, status, last_error FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1`, ws).Scan(&encrypted, &enabled, &status, &lastError)
	if err != nil {
		return claudeSettings{}, false, "", "", err
	}
	raw, err := h.openVCSSecret(encrypted)
	if err != nil {
		return claudeSettings{}, false, "", "", fmt.Errorf("cannot decrypt Claude Managed Agents configuration")
	}
	var cfg claudeSettings
	err = json.Unmarshal([]byte(raw), &cfg)
	return cfg.withDefaults(), enabled, status, lastError, err
}

func (h *Handler) GetClaudeConnection(w http.ResponseWriter, r *http.Request) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	view := claudeSettingsView{Available: h.VCSSecretBox != nil, Repositories: []string{}, VaultIDs: []string{}}
	if !view.Available {
		writeJSON(w, 200, view)
		return
	}
	cfg, enabled, status, lastError, err := h.readClaudeSettings(r.Context(), uuidToString(ws))
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 200, view)
		return
	}
	if err != nil {
		writeError(w, 500, "cannot read Claude Managed Agents configuration")
		return
	}
	var online bool
	_ = h.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM agent_runtime WHERE workspace_id=$1 AND provider='claude_managed' AND daemon_id=$2 AND last_seen_at>now()-interval '45 seconds' AND status='online')`, ws, claudeManagedDaemonPrefix+uuidToString(ws)).Scan(&online)
	if status == "online" && enabled && !online {
		status = "offline"
	}
	view.Configured = true
	view.Name = cfg.Name
	view.EnvironmentID = cfg.EnvironmentID
	view.HasToken = cfg.AnthropicToken != ""
	view.Enabled = enabled
	view.Status = status
	view.LastError = lastError
	view.MaxConcurrency = cfg.MaxConcurrency
	view.RunTimeoutMinutes = cfg.RunTimeoutMinutes
	if cfg.VaultIDs != nil {
		view.VaultIDs = cfg.VaultIDs
	}
	_ = h.DB.QueryRow(r.Context(), `SELECT count(*) FILTER (WHERE t.status IN ('dispatched','running')), count(*) FILTER (WHERE t.status='queued') FROM agent_task_queue t JOIN agent_runtime rt ON rt.id=t.runtime_id WHERE rt.workspace_id=$1 AND rt.provider='claude_managed' AND rt.daemon_id=$2 AND t.status IN ('queued','dispatched','running')`, ws, claudeManagedDaemonPrefix+uuidToString(ws)).Scan(&view.ActiveRuns, &view.QueuedRuns)
	for repo := range cfg.GitHubTokens {
		view.Repositories = append(view.Repositories, repo)
	}
	sort.Strings(view.Repositories)
	writeJSON(w, 200, view)
}

func (h *Handler) claudeInput(w http.ResponseWriter, r *http.Request, ws string) (claudeSettings, bool) {
	var cfg claudeSettings
	if h.VCSSecretBox == nil {
		writeError(w, 503, "Claude Managed Agents requires MULTICA_VCS_SECRET_KEY on the server")
		return cfg, false
	}
	var req struct {
		Name              string            `json:"name"`
		EnvironmentID     string            `json:"environment_id"`
		AnthropicToken    string            `json:"anthropic_token"`
		GitHubTokens      map[string]string `json:"github_tokens"`
		VaultIDs          []string          `json:"vault_ids"`
		MaxConcurrency    int               `json:"max_concurrency"`
		RunTimeoutMinutes int               `json:"run_timeout_minutes"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, 400, "invalid Claude Managed Agents configuration")
		return cfg, false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeError(w, 400, "configuration must contain one JSON object")
		return cfg, false
	}
	cfg, _, _, _, err := h.readClaudeSettings(r.Context(), ws)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 500, "cannot read Claude Managed Agents configuration")
		return cfg, false
	}
	cfg.Name = strings.TrimSpace(req.Name)
	cfg.EnvironmentID = strings.TrimSpace(req.EnvironmentID)
	if cfg.EnvironmentID == "" || cfg.Name == "" || !strings.HasPrefix(cfg.EnvironmentID, "env_") {
		writeError(w, 400, "name and environment are required")
		return cfg, false
	}
	if req.MaxConcurrency != 0 {
		cfg.MaxConcurrency = req.MaxConcurrency
	}
	if req.RunTimeoutMinutes != 0 {
		cfg.RunTimeoutMinutes = req.RunTimeoutMinutes
	}
	cfg = cfg.withDefaults()
	if cfg.MaxConcurrency < 1 || cfg.MaxConcurrency > claudemanagedruntime.MaxConcurrencyLimit {
		writeError(w, 400, fmt.Sprintf("concurrency must be between 1 and %d", claudemanagedruntime.MaxConcurrencyLimit))
		return cfg, false
	}
	if cfg.RunTimeoutMinutes < 5 || cfg.RunTimeoutMinutes > maxClaudeRunTimeout {
		writeError(w, 400, fmt.Sprintf("run timeout must be between 5 and %d minutes", maxClaudeRunTimeout))
		return cfg, false
	}
	if req.AnthropicToken != "" {
		cfg.AnthropicToken = strings.TrimSpace(req.AnthropicToken)
	}
	if cfg.AnthropicToken == "" || strings.ContainsAny(cfg.AnthropicToken, "\r\n\t ") {
		writeError(w, 400, "Anthropic API key is required")
		return cfg, false
	}
	if req.VaultIDs != nil {
		vaults := []string{}
		for _, id := range req.VaultIDs {
			if id = strings.TrimSpace(id); id != "" {
				vaults = append(vaults, id)
			}
		}
		cfg.VaultIDs = vaults
	}
	if req.GitHubTokens != nil {
		next := map[string]string{}
		for raw, token := range req.GitHubTokens {
			key, err := claudemanagedruntime.RepositoryKey(raw)
			if err != nil {
				writeError(w, 400, "invalid GitHub repository URL")
				return cfg, false
			}
			if _, exists := next[key]; exists {
				writeError(w, 400, "duplicate repository binding")
				return cfg, false
			}
			if token == "" {
				token = cfg.GitHubTokens[key]
			}
			if strings.TrimSpace(token) == "" || strings.ContainsAny(strings.TrimSpace(token), "\r\n\t ") {
				writeError(w, 400, "GitHub token is required for each repository")
				return cfg, false
			}
			next[key] = strings.TrimSpace(token)
		}
		cfg.GitHubTokens = next
	}
	return cfg, true
}

func (h *Handler) CheckClaudeConnection(w http.ResponseWriter, r *http.Request) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	cfg, ok := h.claudeInput(w, r, uuidToString(ws))
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := claudemanagedruntime.CheckAccess(ctx, claudemanagedruntime.DefaultAnthropicURL, cfg.AnthropicToken, cfg.EnvironmentID, cfg.VaultIDs); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// SaveClaudeConnection keeps a paused connection paused; only
// ResumeClaudeConnection (or the first connect) enables execution.
func (h *Handler) SaveClaudeConnection(w http.ResponseWriter, r *http.Request) {
	h.saveClaudeConnection(w, r, false)
}

func (h *Handler) ResumeClaudeConnection(w http.ResponseWriter, r *http.Request) {
	h.saveClaudeConnection(w, r, true)
}

func (h *Handler) saveClaudeConnection(w http.ResponseWriter, r *http.Request, resume bool) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	user, ok := requireUserID(w, r)
	if !ok {
		return
	}
	cfg, ok := h.claudeInput(w, r, uuidToString(ws))
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "cannot save Claude Managed Agents configuration")
		return
	}
	defer tx.Rollback(r.Context())
	// Serialize writes even before a connection exists.
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1, $2))`, uuidToString(ws), claudeConnectionLockSeed); err != nil {
		writeError(w, 500, "cannot lock Claude Managed Agents configuration")
		return
	}
	var busy bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1 AND busy_until>now())`, ws).Scan(&busy); err != nil {
		writeError(w, 500, "cannot check Claude Managed Agents connection")
		return
	}
	if busy {
		writeError(w, 409, "Claude Managed Agents is synchronizing; retry shortly")
		return
	}
	var journal string
	if err := tx.QueryRow(r.Context(), `SELECT journal_encrypted FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1`, ws).Scan(&journal); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 500, "cannot read execution state")
		return
	}
	if journal != "" {
		raw, err := h.openVCSSecret(journal)
		if err != nil {
			writeError(w, 500, "cannot read execution state")
			return
		}
		var state struct {
			Runs    []json.RawMessage `json:"runs"`
			Cleanup []string          `json:"cleanup"`
		}
		if json.Unmarshal([]byte(raw), &state) != nil {
			writeError(w, 500, "cannot read execution state")
			return
		}
		if len(state.Cleanup) > 0 || len(state.Runs) > 0 {
			writeError(w, 409, "wait for Claude Managed Agents execution cleanup before changing configuration")
			return
		}
	}
	var revokedHash string
	_ = tx.QueryRow(r.Context(), `SELECT d.token_hash FROM daemon_token d JOIN cloud_agent_connection c ON c.token_id=d.id AND c.provider='claude_managed' WHERE c.workspace_id=$1`, ws).Scan(&revokedHash)
	var active bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM agent_task_queue t JOIN agent_runtime rt ON rt.id=t.runtime_id WHERE rt.workspace_id=$1 AND rt.provider='claude_managed' AND rt.daemon_id=$2 AND t.status IN ('running','dispatched'))`, ws, claudeManagedDaemonPrefix+uuidToString(ws)).Scan(&active)
	if err != nil {
		writeError(w, 500, "cannot check active Claude Managed Agents tasks")
		return
	}
	if active {
		writeError(w, 409, "wait for active Claude Managed Agents tasks to finish before changing configuration")
		return
	}
	rawToken, err := auth.GenerateDaemonToken()
	if err != nil {
		writeError(w, 500, "cannot generate service credential")
		return
	}
	q := h.Queries.WithTx(tx)
	pat, err := q.CreateDaemonToken(r.Context(), db.CreateDaemonTokenParams{WorkspaceID: ws, DaemonID: claudeManagedDaemonPrefix + uuidToString(ws), TokenHash: auth.HashToken(rawToken), ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}})
	if err != nil {
		writeError(w, 500, "cannot create service credential")
		return
	}
	cfg.MulticaToken = rawToken
	raw, _ := json.Marshal(cfg)
	encrypted, err := h.sealVCSSecret(string(raw))
	if err != nil {
		writeError(w, 500, "cannot encrypt Claude Managed Agents configuration")
		return
	}
	_, err = tx.Exec(r.Context(), `DELETE FROM daemon_token WHERE id IN (SELECT token_id FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1)`, ws)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO cloud_agent_connection(workspace_id,provider,owner_id,token_id,config_encrypted) VALUES($1,'claude_managed',$2,$3,$4) ON CONFLICT(workspace_id,provider) DO UPDATE SET owner_id=excluded.owner_id,token_id=excluded.token_id,config_encrypted=excluded.config_encrypted,enabled=($5 OR cloud_agent_connection.enabled),journal_encrypted='',revision=cloud_agent_connection.revision+1,status=CASE WHEN $5 OR cloud_agent_connection.enabled THEN 'starting' ELSE cloud_agent_connection.status END,last_error='',updated_at=now()`, ws, parseUUID(user), pat.ID, encrypted, resume)
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, 500, "cannot save Claude Managed Agents configuration")
		return
	}
	h.DaemonTokenCache.Invalidate(r.Context(), revokedHash)
	h.GetClaudeConnection(w, r)
}

// RunClaudeConnections owns managed Claude bridges for this server, like
// RunQoderConnections: an advisory lock elects one supervisor, and each bridge
// journal lives encrypted in cloud_agent_connection.
func (h *Handler) RunClaudeConnections(ctx context.Context, pool *pgxpool.Pool, serverURL string) {
	if h.VCSSecretBox == nil {
		return
	}
	for ctx.Err() == nil {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.WarnContext(ctx, "Claude supervisor database connection unavailable", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}
		var locked bool
		err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(71243, 8192)`).Scan(&locked)
		if err == nil && locked {
			h.runClaudeSupervisor(ctx, conn, serverURL)
		}
		if locked {
			_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(71243,8192)`)
		}
		conn.Release()
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

type managedClaudeRun struct {
	revision int64
	cancel   context.CancelFunc
	done     chan error
}

func (h *Handler) runClaudeSupervisor(ctx context.Context, conn *pgxpool.Conn, serverURL string) {
	runs := map[string]managedClaudeRun{}
	retryAt := map[string]time.Time{}
	failures := map[string]int{}
	defer func() {
		for _, run := range runs {
			run.cancel()
			<-run.done
		}
	}()
	for ctx.Err() == nil {
		_, _ = conn.Exec(ctx, `UPDATE daemon_token d SET expires_at=now()+interval '1 hour' FROM cloud_agent_connection c JOIN member m ON m.workspace_id=c.workspace_id AND m.user_id=c.owner_id WHERE c.provider='claude_managed' AND d.id=c.token_id AND c.enabled=true AND m.role IN ('owner','admin') AND d.expires_at<now()+interval '30 minutes'`)
		revoked, revokeErr := conn.Query(ctx, `DELETE FROM daemon_token d USING cloud_agent_connection c WHERE c.provider='claude_managed' AND d.id=c.token_id AND (NOT c.enabled OR NOT EXISTS(SELECT 1 FROM member m WHERE m.workspace_id=c.workspace_id AND m.user_id=c.owner_id AND m.role IN ('owner','admin'))) RETURNING d.token_hash`)
		if revokeErr != nil {
			return
		}
		for revoked.Next() {
			var hash string
			if revoked.Scan(&hash) == nil {
				h.DaemonTokenCache.Invalidate(ctx, hash)
			}
		}
		revoked.Close()
		rows, err := conn.Query(ctx, `SELECT c.workspace_id::text,c.revision FROM cloud_agent_connection c JOIN workspace w ON w.id=c.workspace_id JOIN member m ON m.workspace_id=c.workspace_id AND m.user_id=c.owner_id WHERE c.provider='claude_managed' AND c.enabled=true AND m.role IN ('owner','admin')`)
		if err != nil {
			return
		}
		desired := map[string]int64{}
		for rows.Next() {
			var ws string
			var rev int64
			if rows.Scan(&ws, &rev) != nil {
				rows.Close()
				return
			}
			desired[ws] = rev
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return
		}
		retryLater := map[string]bool{}
		for ws, run := range runs {
			if desired[ws] != run.revision {
				run.cancel()
				<-run.done
				delete(runs, ws)
				continue
			}
			select {
			case err := <-run.done:
				message := "execution service stopped; retrying"
				if err != nil {
					slog.WarnContext(ctx, "Claude bridge stopped", "workspace_id", ws, "error", err)
					message = "remote execution unavailable; retrying"
				}
				_, _ = conn.Exec(ctx, `UPDATE cloud_agent_connection SET status='error',last_error=$2 WHERE provider='claude_managed' AND workspace_id=$1`, ws, message)
				delete(runs, ws)
				retryLater[ws] = true
				failures[ws]++
				delay := 10 * time.Second << min(failures[ws]-1, 5)
				retryAt[ws] = time.Now().Add(delay)
			default:
			}
		}
		h.cleanupInactiveClaude(ctx)
		for ws, rev := range desired {
			if retryLater[ws] || time.Now().Before(retryAt[ws]) {
				continue
			}
			if _, ok := runs[ws]; ok {
				continue
			}
			cfg, _, _, _, err := h.readClaudeSettings(ctx, ws)
			if err != nil {
				continue
			}
			bridge, err := claudemanagedruntime.New(claudemanagedruntime.Config{DaemonID: claudeManagedDaemonPrefix + ws, MulticaURL: serverURL, MulticaToken: cfg.MulticaToken, WorkspaceID: ws, AnthropicURL: claudemanagedruntime.DefaultAnthropicURL, AnthropicToken: cfg.AnthropicToken, EnvironmentID: cfg.EnvironmentID, VaultIDs: cfg.VaultIDs, Name: cfg.Name, MaxConcurrency: cfg.MaxConcurrency, RunTimeout: time.Duration(cfg.RunTimeoutMinutes) * time.Minute, GitHubTokens: cfg.GitHubTokens,
				LoadState: func(loadCtx context.Context) ([]byte, error) {
					var encrypted string
					if err := h.DB.QueryRow(loadCtx, `SELECT journal_encrypted FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1`, ws).Scan(&encrypted); err != nil {
						return nil, err
					}
					if encrypted == "" {
						return nil, nil
					}
					raw, err := h.openVCSSecret(encrypted)
					return []byte(raw), err
				},
				SaveState: func(saveCtx context.Context, raw []byte) error {
					encrypted, err := h.sealVCSSecret(string(raw))
					if err != nil {
						return err
					}
					_, err = h.DB.Exec(saveCtx, `UPDATE cloud_agent_connection SET journal_encrypted=$2 WHERE provider='claude_managed' AND workspace_id=$1`, ws, encrypted)
					return err
				},
				OnReady: func(readyCtx context.Context, runtimeID string) error {
					return h.claudeReady(readyCtx, ws, rev, runtimeID)
				},
				BeforeStep: func(stepCtx context.Context) (func(), error) {
					return h.acquireClaudeStep(stepCtx, ws, rev)
				}}, nil)
			if err != nil {
				_, _ = conn.Exec(ctx, `UPDATE cloud_agent_connection SET status='error',last_error='invalid execution configuration' WHERE provider='claude_managed' AND workspace_id=$1`, ws)
				continue
			}
			runCtx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			runs[ws] = managedClaudeRun{rev, cancel, done}
			_, _ = conn.Exec(ctx, `UPDATE cloud_agent_connection SET status='starting',last_error='' WHERE provider='claude_managed' AND workspace_id=$1`, ws)
			go func() { done <- bridge.Run(runCtx) }()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// StopClaudeConnection pauses an idle connection without discarding its secrets.
func (h *Handler) StopClaudeConnection(w http.ResponseWriter, r *http.Request) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "cannot stop Claude Managed Agents runtime")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,$2))`, uuidToString(ws), claudeConnectionLockSeed); err != nil {
		writeError(w, 500, "cannot lock Claude Managed Agents configuration")
		return
	}
	var busy bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1 AND busy_until>now())`, ws).Scan(&busy); err != nil {
		writeError(w, 500, "cannot check Claude Managed Agents connection")
		return
	}
	if busy {
		writeError(w, 409, "Claude Managed Agents is synchronizing; retry shortly")
		return
	}
	var revokedHash string
	_ = tx.QueryRow(r.Context(), `SELECT d.token_hash FROM daemon_token d JOIN cloud_agent_connection c ON c.token_id=d.id AND c.provider='claude_managed' WHERE c.workspace_id=$1`, ws).Scan(&revokedHash)
	var active bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM agent_task_queue t JOIN agent_runtime rt ON rt.id=t.runtime_id WHERE rt.workspace_id=$1 AND rt.provider='claude_managed' AND rt.daemon_id=$2 AND t.status IN ('running','dispatched'))`, ws, claudeManagedDaemonPrefix+uuidToString(ws)).Scan(&active)
	if err != nil {
		writeError(w, 500, "cannot check active Claude Managed Agents tasks")
		return
	}
	if active {
		writeError(w, 409, "wait for or cancel active Claude Managed Agents tasks before stopping")
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM daemon_token WHERE id IN (SELECT token_id FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1)`, ws); err != nil {
		writeError(w, 500, "cannot revoke runtime credential")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE cloud_agent_connection SET enabled=false,revision=revision+1,status='stopped',last_error='' WHERE provider='claude_managed' AND workspace_id=$1`, ws)
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, 500, "cannot stop Claude Managed Agents runtime")
		return
	}
	h.DaemonTokenCache.Invalidate(r.Context(), revokedHash)
	h.GetClaudeConnection(w, r)
}

func (h *Handler) claudeReady(ctx context.Context, ws string, revision int64, runtimeID string) error {
	if _, err := h.DB.Exec(ctx, `UPDATE agent_runtime SET visibility='public',owner_id=(SELECT owner_id FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$2) WHERE id=$1 AND workspace_id=$2`, runtimeID, ws); err != nil {
		return fmt.Errorf("cannot publish managed runtime")
	}
	_, err := h.DB.Exec(ctx, `UPDATE cloud_agent_connection SET status='online',last_error='' WHERE provider='claude_managed' AND workspace_id=$1 AND revision=$2`, ws, revision)
	return err
}

func (h *Handler) acquireClaudeStep(stepCtx context.Context, ws string, rev int64) (func(), error) {
	tx, err := h.TxStarter.Begin(stepCtx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(stepCtx)
	release := func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = h.DB.Exec(releaseCtx, `UPDATE cloud_agent_connection SET busy_until=NULL WHERE provider='claude_managed' AND workspace_id=$1 AND revision=$2`, ws, rev)
	}
	if _, err = tx.Exec(stepCtx, `SELECT pg_advisory_xact_lock(hashtextextended($1,$2))`, ws, claudeConnectionLockSeed); err != nil {
		return nil, err
	}
	var valid bool
	err = tx.QueryRow(stepCtx, `SELECT EXISTS(SELECT 1 FROM cloud_agent_connection c JOIN member m ON m.workspace_id=c.workspace_id AND m.user_id=c.owner_id WHERE c.provider='claude_managed' AND c.workspace_id=$1 AND c.revision=$2 AND c.enabled=true AND m.role IN ('owner','admin') AND (c.busy_until IS NULL OR c.busy_until<now()))`, ws, rev).Scan(&valid)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, context.Canceled
	}
	if _, err := tx.Exec(stepCtx, `UPDATE cloud_agent_connection SET busy_until=now()+interval '45 seconds' WHERE provider='claude_managed' AND workspace_id=$1 AND revision=$2`, ws, rev); err != nil {
		return nil, err
	}
	if err := tx.Commit(stepCtx); err != nil {
		return nil, err
	}
	return release, nil
}
