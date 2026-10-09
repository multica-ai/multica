package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/qoderruntime"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type qoderSettings struct {
	Name          string            `json:"name"`
	BaseURL       string            `json:"base_url"`
	EnvironmentID string            `json:"environment_id"`
	QoderToken    string            `json:"qoder_token,omitempty"`
	GitHubTokens  map[string]string `json:"github_tokens,omitempty"`
	MulticaToken  string            `json:"multica_token,omitempty"`
}

type qoderSettingsView struct {
	Configured    bool     `json:"configured"`
	Available     bool     `json:"available"`
	Name          string   `json:"name"`
	BaseURL       string   `json:"base_url"`
	EnvironmentID string   `json:"environment_id"`
	HasToken      bool     `json:"has_token"`
	Repositories  []string `json:"repositories"`
	Enabled       bool     `json:"enabled"`
	Status        string   `json:"status"`
	LastError     string   `json:"last_error"`
}

func (h *Handler) readQoderSettings(ctx context.Context, ws string) (qoderSettings, bool, string, string, error) {
	var encrypted, status, lastError string
	var enabled bool
	err := h.DB.QueryRow(ctx, `SELECT config_encrypted, enabled, status, last_error FROM qoder_connection WHERE workspace_id=$1`, ws).Scan(&encrypted, &enabled, &status, &lastError)
	if err != nil {
		return qoderSettings{}, false, "", "", err
	}
	raw, err := h.openVCSSecret(encrypted)
	if err != nil {
		return qoderSettings{}, false, "", "", fmt.Errorf("cannot decrypt QCA configuration")
	}
	var cfg qoderSettings
	err = json.Unmarshal([]byte(raw), &cfg)
	return cfg, enabled, status, lastError, err
}

func (h *Handler) GetQoderConnection(w http.ResponseWriter, r *http.Request) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	view := qoderSettingsView{Available: h.VCSSecretBox != nil && os.Getenv("MULTICA_QODER_STATE_DIR") != "", Repositories: []string{}}
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
		writeError(w, 500, "cannot read QCA configuration")
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
	for repo := range cfg.GitHubTokens {
		view.Repositories = append(view.Repositories, repo)
	}
	sort.Strings(view.Repositories)
	writeJSON(w, 200, view)
}

func (h *Handler) qoderInput(w http.ResponseWriter, r *http.Request, ws string) (qoderSettings, bool) {
	var cfg qoderSettings
	if h.VCSSecretBox == nil || os.Getenv("MULTICA_QODER_STATE_DIR") == "" {
		writeError(w, 503, "QCA requires MULTICA_VCS_SECRET_KEY and MULTICA_QODER_STATE_DIR on the server")
		return cfg, false
	}
	var req struct {
		Name          string            `json:"name"`
		BaseURL       string            `json:"base_url"`
		EnvironmentID string            `json:"environment_id"`
		QoderToken    string            `json:"qoder_token"`
		GitHubTokens  map[string]string `json:"github_tokens"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, 400, "invalid QCA configuration")
		return cfg, false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeError(w, 400, "configuration must contain one JSON object")
		return cfg, false
	}
	cfg, _, _, _, err := h.readQoderSettings(r.Context(), ws)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 500, "cannot read QCA configuration")
		return cfg, false
	}
	cfg.Name = strings.TrimSpace(req.Name)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	cfg.EnvironmentID = strings.TrimSpace(req.EnvironmentID)
	// The managed entry point is restricted to Qoder's public API, preventing SSRF.
	if cfg.BaseURL != "https://api.qoder.com/api/v1/cloud" || cfg.EnvironmentID == "" || cfg.Name == "" {
		writeError(w, 400, "name, environment and official API URL are required")
		return cfg, false
	}
	if req.QoderToken != "" {
		cfg.QoderToken = strings.TrimSpace(req.QoderToken)
	}
	if cfg.QoderToken == "" || strings.ContainsAny(cfg.QoderToken, "\r\n\t ") {
		writeError(w, 400, "QCA token is required")
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
			writeError(w, 400, "invalid QCA resource")
			return
		}
		req.Header.Set("Authorization", "Bearer "+cfg.QoderToken)
		res, err := client.Do(req)
		if err != nil {
			writeError(w, 502, "QCA connection failed")
			return
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			writeError(w, 400, fmt.Sprintf("QCA access check returned HTTP %d", res.StatusCode))
			return
		}
		var resource struct {
			ID string `json:"id"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&resource)
		res.Body.Close()
		if err != nil || resource.ID != expectedID {
			writeError(w, 502, "invalid QCA resource response")
			return
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Handler) SaveQoderConnection(w http.ResponseWriter, r *http.Request) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	user, ok := requireUserID(w, r)
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "cannot save QCA configuration")
		return
	}
	defer tx.Rollback(r.Context())
	// Serialize writes even before a connection exists.
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1, 19))`, uuidToString(ws)); err != nil {
		writeError(w, 500, "cannot lock QCA configuration")
		return
	}
	cfg, ok := h.qoderInput(w, r, uuidToString(ws))
	if !ok {
		return
	}
	var active bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM agent_task_queue t JOIN agent_runtime rt ON rt.id=t.runtime_id WHERE rt.workspace_id=$1 AND rt.provider='qoder_cloud' AND t.status IN ('running','dispatched'))`, ws).Scan(&active)
	if err != nil {
		writeError(w, 500, "cannot check active QCA tasks")
		return
	}
	if active {
		writeError(w, 409, "wait for active QCA tasks to finish before changing configuration")
		return
	}
	rawToken, err := auth.GeneratePATToken()
	if err != nil {
		writeError(w, 500, "cannot generate service credential")
		return
	}
	q := h.Queries.WithTx(tx)
	pat, err := q.CreatePersonalAccessToken(r.Context(), db.CreatePersonalAccessTokenParams{UserID: parseUUID(user), Name: "Managed QCA runtime", TokenHash: auth.HashToken(rawToken), TokenPrefix: rawToken[:12]})
	if err != nil {
		writeError(w, 500, "cannot create service credential")
		return
	}
	cfg.MulticaToken = rawToken
	raw, _ := json.Marshal(cfg)
	encrypted, err := h.sealVCSSecret(string(raw))
	if err != nil {
		writeError(w, 500, "cannot encrypt QCA configuration")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE personal_access_token SET revoked=true WHERE id IN (SELECT token_id FROM qoder_connection WHERE workspace_id=$1)`, ws)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO qoder_connection(workspace_id,owner_id,token_id,config_encrypted) VALUES($1,$2,$3,$4) ON CONFLICT(workspace_id) DO UPDATE SET owner_id=excluded.owner_id,token_id=excluded.token_id,config_encrypted=excluded.config_encrypted,enabled=true,revision=qoder_connection.revision+1,status='starting',last_error='',updated_at=now()`, ws, parseUUID(user), pat.ID, encrypted)
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, 500, "cannot save QCA configuration")
		return
	}
	h.GetQoderConnection(w, r)
}

// RunQoderConnections owns managed bridges for this server. A database advisory
// lock elects one supervisor; its journal directory must survive server restarts.
func (h *Handler) RunQoderConnections(ctx context.Context, pool *pgxpool.Pool, serverURL, stateDir string) {
	if h.VCSSecretBox == nil || stateDir == "" {
		return
	}
	for ctx.Err() == nil {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return
		}
		var locked bool
		err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(71243, 8191)`).Scan(&locked)
		if err == nil && locked {
			h.runQoderSupervisor(ctx, conn, serverURL, stateDir)
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

func (h *Handler) runQoderSupervisor(ctx context.Context, conn *pgxpool.Conn, serverURL, stateDir string) {
	runs := map[string]managedQoderRun{}
	defer func() {
		for _, run := range runs {
			run.cancel()
			<-run.done
		}
	}()
	for ctx.Err() == nil {
		rows, err := conn.Query(ctx, `SELECT c.workspace_id::text,c.revision FROM qoder_connection c JOIN workspace w ON w.id=c.workspace_id JOIN member m ON m.workspace_id=c.workspace_id AND m.user_id=c.owner_id WHERE c.enabled=true`)
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
					message = err.Error()
				}
				_, _ = conn.Exec(ctx, `UPDATE qoder_connection SET status='error',last_error=$2 WHERE workspace_id=$1`, ws, message)
				delete(runs, ws)
				retryLater[ws] = true
			default:
			}
		}
		for ws, rev := range desired {
			if retryLater[ws] {
				continue
			}
			if _, ok := runs[ws]; ok {
				continue
			}
			cfg, _, _, _, err := h.readQoderSettings(ctx, ws)
			if err != nil {
				continue
			}
			bridge, err := qoderruntime.New(qoderruntime.Config{MulticaURL: serverURL, MulticaToken: cfg.MulticaToken, WorkspaceID: ws, QoderURL: cfg.BaseURL, QoderToken: cfg.QoderToken, EnvironmentID: cfg.EnvironmentID, Name: cfg.Name, StateDir: filepath.Join(stateDir, ws, fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.BaseURL+cfg.EnvironmentID)))), GitHubTokens: cfg.GitHubTokens,
				OnReady: func(readyCtx context.Context, runtimeID string) error {
					return h.qoderReady(readyCtx, ws, rev, runtimeID)
				},
				BeforeStep: func(stepCtx context.Context) (func(), error) {
					tx, err := h.TxStarter.Begin(stepCtx)
					if err != nil {
						return nil, err
					}
					release := func() { _ = tx.Rollback(context.Background()) }
					if _, err = tx.Exec(stepCtx, `SELECT pg_advisory_xact_lock(hashtextextended($1,19))`, ws); err != nil {
						release()
						return nil, err
					}
					var valid bool
					err = tx.QueryRow(stepCtx, `SELECT EXISTS(SELECT 1 FROM qoder_connection c JOIN member m ON m.workspace_id=c.workspace_id AND m.user_id=c.owner_id WHERE c.workspace_id=$1 AND c.revision=$2 AND c.enabled=true)`, ws, rev).Scan(&valid)
					if err != nil {
						release()
						return nil, err
					}
					if !valid {
						release()
						return nil, context.Canceled
					}
					return release, nil
				}}, nil)
			if err != nil {
				_, _ = conn.Exec(ctx, `UPDATE qoder_connection SET status='error',last_error='invalid execution configuration' WHERE workspace_id=$1`, ws)
				continue
			}
			runCtx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			runs[ws] = managedQoderRun{rev, cancel, done}
			_, _ = conn.Exec(ctx, `UPDATE qoder_connection SET status='starting',last_error='' WHERE workspace_id=$1`, ws)
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
		writeError(w, 500, "cannot stop QCA runtime")
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,19))`, uuidToString(ws))
	if err != nil {
		writeError(w, 500, "cannot lock QCA configuration")
		return
	}
	var active bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM agent_task_queue t JOIN agent_runtime rt ON rt.id=t.runtime_id WHERE rt.workspace_id=$1 AND rt.provider='qoder_cloud' AND t.status IN ('running','dispatched'))`, ws).Scan(&active)
	if err != nil {
		writeError(w, 500, "cannot check active QCA tasks")
		return
	}
	if active {
		writeError(w, 409, "wait for or cancel active QCA tasks before stopping")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE qoder_connection SET enabled=false,revision=revision+1,status='stopped',last_error='' WHERE workspace_id=$1`, ws)
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, 500, "cannot stop QCA runtime")
		return
	}
	h.GetQoderConnection(w, r)
}

func (h *Handler) qoderReady(ctx context.Context, ws string, revision int64, runtimeID string) error {
	if _, err := h.DB.Exec(ctx, `UPDATE agent_runtime SET visibility='public' WHERE id=$1 AND workspace_id=$2`, runtimeID, ws); err != nil {
		return fmt.Errorf("cannot publish managed runtime")
	}
	_, err := h.DB.Exec(ctx, `UPDATE qoder_connection SET status='online',last_error='' WHERE workspace_id=$1 AND revision=$2`, ws, revision)
	return err
}
