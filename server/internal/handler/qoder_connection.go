package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/qoderruntime"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type qoderSettings struct {
	Name              string            `json:"name"`
	BaseURL           string            `json:"base_url"`
	EnvironmentID     string            `json:"environment_id"`
	QoderToken        string            `json:"qoder_token,omitempty"`
	GitHubTokens      map[string]string `json:"github_tokens,omitempty"`
	MulticaToken      string            `json:"multica_token,omitempty"`
	MaxConcurrency    int               `json:"max_concurrency,omitempty"`
	RunTimeoutMinutes int               `json:"run_timeout_minutes,omitempty"`
}

const (
	defaultQoderConcurrency = 3
	defaultQoderRunTimeout  = 60
	maxQoderRunTimeout      = 240
)

// withDefaults fills limits for connections saved before they were configurable.
func (s qoderSettings) withDefaults() qoderSettings {
	if s.MaxConcurrency == 0 {
		s.MaxConcurrency = defaultQoderConcurrency
	}
	if s.RunTimeoutMinutes == 0 {
		s.RunTimeoutMinutes = defaultQoderRunTimeout
	}
	return s
}

type qoderSettingsView struct {
	Configured        bool     `json:"configured"`
	Available         bool     `json:"available"`
	Name              string   `json:"name"`
	BaseURL           string   `json:"base_url"`
	EnvironmentID     string   `json:"environment_id"`
	HasToken          bool     `json:"has_token"`
	Repositories      []string `json:"repositories"`
	Enabled           bool     `json:"enabled"`
	Status            string   `json:"status"`
	LastError         string   `json:"last_error"`
	MaxConcurrency    int      `json:"max_concurrency"`
	RunTimeoutMinutes int      `json:"run_timeout_minutes"`
	ActiveRuns        int      `json:"active_runs"`
	QueuedRuns        int      `json:"queued_runs"`
}

func (h *Handler) readQoderSettings(ctx context.Context, ws string) (qoderSettings, bool, string, string, error) {
	var encrypted, status, lastError string
	var enabled bool
	err := h.DB.QueryRow(ctx, `SELECT config_encrypted, enabled, status, last_error FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, ws).Scan(&encrypted, &enabled, &status, &lastError)
	if err != nil {
		return qoderSettings{}, false, "", "", err
	}
	raw, err := h.openVCSSecret(encrypted)
	if err != nil {
		return qoderSettings{}, false, "", "", fmt.Errorf("cannot decrypt Qoder Cloud Agents configuration")
	}
	var cfg qoderSettings
	err = json.Unmarshal([]byte(raw), &cfg)
	return cfg.withDefaults(), enabled, status, lastError, err
}

func (h *Handler) GetQoderConnection(w http.ResponseWriter, r *http.Request) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	view := qoderSettingsView{Available: h.VCSSecretBox != nil, Repositories: []string{}}
	if !view.Available {
		writeJSON(w, 200, view)
		return
	}
	cfg, enabled, status, lastError, err := h.readQoderSettings(r.Context(), uuidToString(ws))
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 200, view)
		return
	}
	if err != nil {
		writeError(w, 500, "cannot read Qoder Cloud Agents configuration")
		return
	}
	var online bool
	_ = h.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM agent_runtime WHERE workspace_id=$1 AND provider='qoder_cloud' AND last_seen_at>now()-interval '45 seconds' AND status='online')`, ws).Scan(&online)
	if status == "online" && enabled && !online {
		status = "offline"
	}
	view.Configured = true
	view.Name = cfg.Name
	view.BaseURL = cfg.BaseURL
	view.EnvironmentID = cfg.EnvironmentID
	view.HasToken = cfg.QoderToken != ""
	view.Enabled = enabled
	view.Status = status
	view.LastError = lastError
	view.MaxConcurrency = cfg.MaxConcurrency
	view.RunTimeoutMinutes = cfg.RunTimeoutMinutes
	_ = h.DB.QueryRow(r.Context(), `SELECT count(*) FILTER (WHERE t.status IN ('dispatched','running')), count(*) FILTER (WHERE t.status='queued') FROM agent_task_queue t JOIN agent_runtime rt ON rt.id=t.runtime_id WHERE rt.workspace_id=$1 AND rt.provider='qoder_cloud' AND t.status IN ('queued','dispatched','running')`, ws).Scan(&view.ActiveRuns, &view.QueuedRuns)
	for repo := range cfg.GitHubTokens {
		view.Repositories = append(view.Repositories, repo)
	}
	sort.Strings(view.Repositories)
	writeJSON(w, 200, view)
}

func (h *Handler) qoderInput(w http.ResponseWriter, r *http.Request, ws string) (qoderSettings, bool) {
	var cfg qoderSettings
	if h.VCSSecretBox == nil {
		writeError(w, 503, "Qoder Cloud Agents requires MULTICA_VCS_SECRET_KEY on the server")
		return cfg, false
	}
	var req struct {
		Name              string            `json:"name"`
		BaseURL           string            `json:"base_url"`
		EnvironmentID     string            `json:"environment_id"`
		QoderToken        string            `json:"qoder_token"`
		GitHubTokens      map[string]string `json:"github_tokens"`
		MaxConcurrency    int               `json:"max_concurrency"`
		RunTimeoutMinutes int               `json:"run_timeout_minutes"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, 400, "invalid Qoder Cloud Agents configuration")
		return cfg, false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeError(w, 400, "configuration must contain one JSON object")
		return cfg, false
	}
	cfg, _, _, _, err := h.readQoderSettings(r.Context(), ws)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 500, "cannot read Qoder Cloud Agents configuration")
		return cfg, false
	}
	cfg.Name = strings.TrimSpace(req.Name)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	cfg.EnvironmentID = strings.TrimSpace(req.EnvironmentID)
	if !qoderAPIURLs[cfg.BaseURL] || cfg.EnvironmentID == "" || cfg.Name == "" {
		writeError(w, 400, "name, environment and official API URL are required")
		return cfg, false
	}
	if req.MaxConcurrency != 0 {
		cfg.MaxConcurrency = req.MaxConcurrency
	}
	if req.RunTimeoutMinutes != 0 {
		cfg.RunTimeoutMinutes = req.RunTimeoutMinutes
	}
	cfg = cfg.withDefaults()
	if cfg.MaxConcurrency < 1 || cfg.MaxConcurrency > qoderruntime.MaxConcurrencyLimit {
		writeError(w, 400, fmt.Sprintf("concurrency must be between 1 and %d", qoderruntime.MaxConcurrencyLimit))
		return cfg, false
	}
	if cfg.RunTimeoutMinutes < 5 || cfg.RunTimeoutMinutes > maxQoderRunTimeout {
		writeError(w, 400, fmt.Sprintf("run timeout must be between 5 and %d minutes", maxQoderRunTimeout))
		return cfg, false
	}
	if req.QoderToken != "" {
		cfg.QoderToken = strings.TrimSpace(req.QoderToken)
	}
	if cfg.QoderToken == "" || strings.ContainsAny(cfg.QoderToken, "\r\n\t ") {
		writeError(w, 400, "Qoder PAT is required")
		return cfg, false
	}
	if req.GitHubTokens != nil {
		next := map[string]string{}
		for raw, token := range req.GitHubTokens {
			key, err := qoderruntime.RepositoryKey(raw)
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

func (h *Handler) CheckQoderConnection(w http.ResponseWriter, r *http.Request) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	cfg, ok := h.qoderInput(w, r, uuidToString(ws))
	if !ok {
		return
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for path, expectedID := range map[string]string{"/environments/" + url.PathEscape(cfg.EnvironmentID): cfg.EnvironmentID} {
		req, err := http.NewRequestWithContext(r.Context(), "GET", cfg.BaseURL+path, nil)
		if err != nil {
			writeError(w, 400, "invalid Qoder Cloud Agents resource")
			return
		}
		req.Header.Set("Authorization", "Bearer "+cfg.QoderToken)
		res, err := client.Do(req)
		if err != nil {
			writeError(w, 502, "Qoder Cloud Agents connection failed")
			return
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			writeError(w, 400, fmt.Sprintf("Qoder Cloud Agents access check returned HTTP %d", res.StatusCode))
			return
		}
		var resource struct {
			ID string `json:"id"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&resource)
		res.Body.Close()
		if err != nil || resource.ID != expectedID {
			writeError(w, 502, "invalid Qoder Cloud Agents resource response")
			return
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// SaveQoderConnection keeps a paused connection paused; only
// ResumeQoderConnection (or the first connect) enables execution.
func (h *Handler) SaveQoderConnection(w http.ResponseWriter, r *http.Request) {
	h.saveQoderConnection(w, r, false)
}

func (h *Handler) ResumeQoderConnection(w http.ResponseWriter, r *http.Request) {
	h.saveQoderConnection(w, r, true)
}

func (h *Handler) saveQoderConnection(w http.ResponseWriter, r *http.Request, resume bool) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	user, ok := requireUserID(w, r)
	if !ok {
		return
	}
	cfg, ok := h.qoderInput(w, r, uuidToString(ws))
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "cannot save Qoder Cloud Agents configuration")
		return
	}
	defer tx.Rollback(r.Context())
	// Serialize writes even before a connection exists.
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1, 19))`, uuidToString(ws)); err != nil {
		writeError(w, 500, "cannot lock Qoder Cloud Agents configuration")
		return
	}
	var busy bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1 AND busy_until>now())`, ws).Scan(&busy); err != nil {
		writeError(w, 500, "cannot check Qoder Cloud Agents connection")
		return
	}
	if busy {
		writeError(w, 409, "Qoder Cloud Agents is synchronizing; retry shortly")
		return
	}

	var journal string
	if err := tx.QueryRow(r.Context(), `SELECT journal_encrypted FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, ws).Scan(&journal); err != nil && !errors.Is(err, pgx.ErrNoRows) {
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
			Run     json.RawMessage   `json:"run"`
			Runs    []json.RawMessage `json:"runs"`
			Cleanup []string          `json:"cleanup"`
		}
		if json.Unmarshal([]byte(raw), &state) != nil {
			writeError(w, 500, "cannot read execution state")
			return
		}
		if len(state.Cleanup) > 0 || len(state.Runs) > 0 || (len(state.Run) > 0 && string(state.Run) != "null") {
			writeError(w, 409, "wait for Qoder Cloud Agents execution cleanup before changing configuration")
			return
		}
	}
	var revokedHash string
	_ = tx.QueryRow(r.Context(), `SELECT d.token_hash FROM daemon_token d JOIN cloud_agent_connection c ON c.token_id=d.id AND c.provider='qoder_cloud' WHERE c.workspace_id=$1`, ws).Scan(&revokedHash)
	var active bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM agent_task_queue t JOIN agent_runtime rt ON rt.id=t.runtime_id WHERE rt.workspace_id=$1 AND rt.provider='qoder_cloud' AND t.status IN ('running','dispatched'))`, ws).Scan(&active)
	if err != nil {
		writeError(w, 500, "cannot check active Qoder Cloud Agents tasks")
		return
	}
	if active {
		writeError(w, 409, "wait for active Qoder Cloud Agents tasks to finish before changing configuration")
		return
	}
	rawToken, err := auth.GenerateDaemonToken()
	if err != nil {
		writeError(w, 500, "cannot generate service credential")
		return
	}
	q := h.Queries.WithTx(tx)
	pat, err := q.CreateDaemonToken(r.Context(), db.CreateDaemonTokenParams{WorkspaceID: ws, DaemonID: "qoder-managed-" + uuidToString(ws), TokenHash: auth.HashToken(rawToken), ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}})
	if err != nil {
		writeError(w, 500, "cannot create service credential")
		return
	}
	cfg.MulticaToken = rawToken
	raw, _ := json.Marshal(cfg)
	encrypted, err := h.sealVCSSecret(string(raw))
	if err != nil {
		writeError(w, 500, "cannot encrypt Qoder Cloud Agents configuration")
		return
	}
	_, err = tx.Exec(r.Context(), `DELETE FROM daemon_token WHERE id IN (SELECT token_id FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1)`, ws)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO cloud_agent_connection(workspace_id,provider,owner_id,token_id,config_encrypted) VALUES($1,'qoder_cloud',$2,$3,$4) ON CONFLICT(workspace_id,provider) DO UPDATE SET owner_id=excluded.owner_id,token_id=excluded.token_id,config_encrypted=excluded.config_encrypted,enabled=($5 OR cloud_agent_connection.enabled),journal_encrypted='',revision=cloud_agent_connection.revision+1,status=CASE WHEN $5 OR cloud_agent_connection.enabled THEN 'starting' ELSE cloud_agent_connection.status END,last_error='',updated_at=now()`, ws, parseUUID(user), pat.ID, encrypted, resume)
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, 500, "cannot save Qoder Cloud Agents configuration")
		return
	}
	h.DaemonTokenCache.Invalidate(r.Context(), revokedHash)
	h.GetQoderConnection(w, r)
}

// RunQoderConnections owns managed bridges for this server. A database advisory
// lock elects one supervisor, and each bridge journal lives encrypted in
// cloud_agent_connection, so a restart or another replica resumes it.
func (h *Handler) RunQoderConnections(ctx context.Context, pool *pgxpool.Pool, serverURL string) {
	if h.VCSSecretBox == nil {
		return
	}
	for ctx.Err() == nil {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.WarnContext(ctx, "Qoder supervisor database connection unavailable", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}
		var locked bool
		err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(71243, 8191)`).Scan(&locked)
		if err == nil && locked {
			h.runQoderSupervisor(ctx, conn, serverURL)
		}
		if locked {
			_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(71243,8191)`)
		}
		conn.Release()
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

type managedQoderRun struct {
	revision int64
	cancel   context.CancelFunc
	done     chan error
}

func (h *Handler) runQoderSupervisor(ctx context.Context, conn *pgxpool.Conn, serverURL string) {
	runs := map[string]managedQoderRun{}
	retryAt := map[string]time.Time{}
	failures := map[string]int{}
	defer func() {
		for _, run := range runs {
			run.cancel()
			<-run.done
		}
	}()
	for ctx.Err() == nil {
		_, _ = conn.Exec(ctx, `UPDATE daemon_token d SET expires_at=now()+interval '1 hour' FROM cloud_agent_connection c JOIN member m ON m.workspace_id=c.workspace_id AND m.user_id=c.owner_id WHERE c.provider='qoder_cloud' AND d.id=c.token_id AND c.enabled=true AND m.role IN ('owner','admin') AND d.expires_at<now()+interval '30 minutes'`)
		revoked, revokeErr := conn.Query(ctx, `DELETE FROM daemon_token d USING cloud_agent_connection c WHERE c.provider='qoder_cloud' AND d.id=c.token_id AND (NOT c.enabled OR NOT EXISTS(SELECT 1 FROM member m WHERE m.workspace_id=c.workspace_id AND m.user_id=c.owner_id AND m.role IN ('owner','admin'))) RETURNING d.token_hash`)
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
		rows, err := conn.Query(ctx, `SELECT c.workspace_id::text,c.revision FROM cloud_agent_connection c JOIN workspace w ON w.id=c.workspace_id JOIN member m ON m.workspace_id=c.workspace_id AND m.user_id=c.owner_id WHERE c.provider='qoder_cloud' AND c.enabled=true AND m.role IN ('owner','admin')`)
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
					slog.WarnContext(ctx, "Qoder bridge stopped", "workspace_id", ws, "error", err)
					message = "remote execution unavailable; retrying"
				}
				_, _ = conn.Exec(ctx, `UPDATE cloud_agent_connection SET status='error',last_error=$2 WHERE provider='qoder_cloud' AND workspace_id=$1`, ws, message)
				delete(runs, ws)
				retryLater[ws] = true
				failures[ws]++
				delay := 10 * time.Second << min(failures[ws]-1, 5)
				retryAt[ws] = time.Now().Add(delay)
			default:
			}
		}
		h.cleanupInactiveQoder(ctx)
		for ws, rev := range desired {
			if retryLater[ws] || time.Now().Before(retryAt[ws]) {
				continue
			}
			if _, ok := runs[ws]; ok {
				continue
			}
			cfg, _, _, _, err := h.readQoderSettings(ctx, ws)
			if err != nil {
				continue
			}
			bridge, err := qoderruntime.New(qoderruntime.Config{DaemonID: "qoder-managed-" + ws, MulticaURL: serverURL, MulticaToken: cfg.MulticaToken, WorkspaceID: ws, QoderURL: cfg.BaseURL, QoderToken: cfg.QoderToken, EnvironmentID: cfg.EnvironmentID, Name: cfg.Name, MaxConcurrency: cfg.MaxConcurrency, RunTimeout: time.Duration(cfg.RunTimeoutMinutes) * time.Minute, GitHubTokens: cfg.GitHubTokens,
				LoadState: func(loadCtx context.Context) ([]byte, error) {
					var encrypted string
					if err := h.DB.QueryRow(loadCtx, `SELECT journal_encrypted FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, ws).Scan(&encrypted); err != nil {
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
					_, err = h.DB.Exec(saveCtx, `UPDATE cloud_agent_connection SET journal_encrypted=$2 WHERE provider='qoder_cloud' AND workspace_id=$1`, ws, encrypted)
					return err
				},
				OnReady: func(readyCtx context.Context, runtimeID string) error {
					return h.qoderReady(readyCtx, ws, rev, runtimeID)
				},
				BeforeStep: func(stepCtx context.Context) (func(), error) {
					return h.acquireQoderStep(stepCtx, ws, rev)
				}}, nil)
			if err != nil {
				_, _ = conn.Exec(ctx, `UPDATE cloud_agent_connection SET status='error',last_error='invalid execution configuration' WHERE provider='qoder_cloud' AND workspace_id=$1`, ws)
				continue
			}
			runCtx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			runs[ws] = managedQoderRun{rev, cancel, done}
			_, _ = conn.Exec(ctx, `UPDATE cloud_agent_connection SET status='starting',last_error='' WHERE provider='qoder_cloud' AND workspace_id=$1`, ws)
			go func() { done <- bridge.Run(runCtx) }()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// StopQoderConnection pauses an idle connection without discarding its secrets.
// Active work must be cancelled through the task API before stopping service.
func (h *Handler) StopQoderConnection(w http.ResponseWriter, r *http.Request) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "cannot stop Qoder Cloud Agents runtime")
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,19))`, uuidToString(ws))
	if err != nil {
		writeError(w, 500, "cannot lock Qoder Cloud Agents configuration")
		return
	}
	var busy bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1 AND busy_until>now())`, ws).Scan(&busy); err != nil {
		writeError(w, 500, "cannot check Qoder Cloud Agents connection")
		return
	}
	if busy {
		writeError(w, 409, "Qoder Cloud Agents is synchronizing; retry shortly")
		return
	}
	var revokedHash string
	_ = tx.QueryRow(r.Context(), `SELECT d.token_hash FROM daemon_token d JOIN cloud_agent_connection c ON c.token_id=d.id AND c.provider='qoder_cloud' WHERE c.workspace_id=$1`, ws).Scan(&revokedHash)
	var active bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM agent_task_queue t JOIN agent_runtime rt ON rt.id=t.runtime_id WHERE rt.workspace_id=$1 AND rt.provider='qoder_cloud' AND t.status IN ('running','dispatched'))`, ws).Scan(&active)
	if err != nil {
		writeError(w, 500, "cannot check active Qoder Cloud Agents tasks")
		return
	}
	if active {
		writeError(w, 409, "wait for or cancel active Qoder Cloud Agents tasks before stopping")
		return
	}
	_, err = tx.Exec(r.Context(), `DELETE FROM daemon_token WHERE id IN (SELECT token_id FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1)`, ws)
	if err != nil {
		writeError(w, 500, "cannot revoke runtime credential")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE cloud_agent_connection SET enabled=false,revision=revision+1,status='stopped',last_error='' WHERE provider='qoder_cloud' AND workspace_id=$1`, ws)
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, 500, "cannot stop Qoder Cloud Agents runtime")
		return
	}
	h.DaemonTokenCache.Invalidate(r.Context(), revokedHash)
	h.GetQoderConnection(w, r)
}

func (h *Handler) qoderReady(ctx context.Context, ws string, revision int64, runtimeID string) error {
	if _, err := h.DB.Exec(ctx, `UPDATE agent_runtime SET visibility='public',owner_id=(SELECT owner_id FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$2) WHERE id=$1 AND workspace_id=$2`, runtimeID, ws); err != nil {
		return fmt.Errorf("cannot publish managed runtime")
	}
	_, err := h.DB.Exec(ctx, `UPDATE cloud_agent_connection SET status='online',last_error='' WHERE provider='qoder_cloud' AND workspace_id=$1 AND revision=$2`, ws, revision)
	return err
}

func (h *Handler) qoderCredentialAllowed(r *http.Request, ws, daemonID string) (bool, error) {
	// Prefer the hash DaemonAuth recorded for the authenticating token: a WS
	// RPC replays the request in-process without an Authorization header
	// (MUL-8154). Fall back to the header for callers outside DaemonAuth.
	tokenHash := middleware.DaemonTokenHashFromContext(r.Context())
	if tokenHash == "" {
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if raw == "" {
			return false, nil
		}
		tokenHash = auth.HashToken(raw)
	}
	var allowed bool
	err := h.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM cloud_agent_connection c JOIN daemon_token d ON d.id=c.token_id JOIN member m ON m.workspace_id=c.workspace_id AND m.user_id=c.owner_id WHERE c.provider='qoder_cloud' AND c.workspace_id=$1 AND c.enabled AND d.daemon_id=$2 AND d.token_hash=$3 AND d.expires_at>now() AND m.role IN ('owner','admin'))`, ws, daemonID, tokenHash).Scan(&allowed)
	return allowed, err
}

func (h *Handler) acquireQoderStep(stepCtx context.Context, ws string, rev int64) (func(), error) {
	tx, err := h.TxStarter.Begin(stepCtx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(stepCtx)
	release := func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = h.DB.Exec(releaseCtx, `UPDATE cloud_agent_connection SET busy_until=NULL WHERE provider='qoder_cloud' AND workspace_id=$1 AND revision=$2`, ws, rev)
	}
	if _, err = tx.Exec(stepCtx, `SELECT pg_advisory_xact_lock(hashtextextended($1,19))`, ws); err != nil {
		return nil, err
	}
	var valid bool
	err = tx.QueryRow(stepCtx, `SELECT EXISTS(SELECT 1 FROM cloud_agent_connection c JOIN member m ON m.workspace_id=c.workspace_id AND m.user_id=c.owner_id WHERE c.provider='qoder_cloud' AND c.workspace_id=$1 AND c.revision=$2 AND c.enabled=true AND m.role IN ('owner','admin') AND (c.busy_until IS NULL OR c.busy_until<now()))`, ws, rev).Scan(&valid)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, context.Canceled
	}
	if _, err := tx.Exec(stepCtx, `UPDATE cloud_agent_connection SET busy_until=now()+interval '45 seconds' WHERE provider='qoder_cloud' AND workspace_id=$1 AND revision=$2`, ws, rev); err != nil {
		return nil, err
	}
	if err := tx.Commit(stepCtx); err != nil {
		return nil, err
	}
	return release, nil
}
