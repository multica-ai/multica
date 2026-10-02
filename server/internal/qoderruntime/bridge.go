package qoderruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

const Provider = "qoder_cloud"

type Config struct {
	BeforeStep       func(context.Context) (func(), error) `json:"-"`
	OnReady          func(context.Context, string) error   `json:"-"`
	GitHubTokens     map[string]string                     `json:"-"`
	GitHubTokenFiles map[string]string                     `json:"github_token_files,omitempty"`
	MulticaURL       string                                `json:"multica_url"`
	MulticaToken     string                                `json:"multica_token"`
	WorkspaceID      string                                `json:"workspace_id"`
	QoderURL         string                                `json:"qoder_url"`
	QoderToken       string                                `json:"qoder_token"`
	EnvironmentID    string                                `json:"environment_id"`
	StateDir         string                                `json:"state_dir"`
	Name             string                                `json:"name,omitempty"`
	PollInterval     time.Duration                         `json:"-"`
}

type Bridge struct {
	cfg            Config
	multica, qoder *api
	state          *journal
	path           string
	log            *slog.Logger
}

func New(cfg Config, log *slog.Logger) (*Bridge, error) {
	workspace, err := uuid.Parse(cfg.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspace ID must be a UUID")
	}
	cfg.WorkspaceID = workspace.String()
	if cfg.StateDir == "" || cfg.EnvironmentID == "" {
		return nil, fmt.Errorf("state directory and environment ID are required")
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 3 * time.Second
	}
	if cfg.PollInterval < time.Millisecond {
		return nil, fmt.Errorf("poll interval must be positive")
	}
	if cfg.Name == "" {
		cfg.Name = "Qoder Cloud Agent"
	}
	m, err := newAPI(cfg.MulticaURL, cfg.MulticaToken, cfg.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("Multica: %w", err)
	}
	q, err := newAPI(cfg.QoderURL, cfg.QoderToken, "")
	if err != nil {
		return nil, fmt.Errorf("Qoder: %w", err)
	}
	if log == nil {
		log = slog.Default()
	}
	return &Bridge{cfg: cfg, multica: m, qoder: q, path: filepath.Join(cfg.StateDir, "state.json"), log: log}, nil
}

// Run is single-instance per state directory and processes one run at a time.
// Restart resumes the journal instead of declaring remote work orphaned.
func (b *Bridge) Run(ctx context.Context) error {
	if err := os.MkdirAll(b.cfg.StateDir, 0700); err != nil {
		return err
	}
	lock, err := lockState(filepath.Join(b.cfg.StateDir, "bridge.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	sum := sha256.Sum256([]byte(b.multica.base + "\n" + b.cfg.WorkspaceID + "\n" + b.qoder.base + "\n" + b.cfg.EnvironmentID))
	binding := hex.EncodeToString(sum[:])
	b.state, err = readJournal(b.path, binding)
	if err != nil {
		return err
	}
	// Validate remote access before accepting work. Existing runs still reconcile
	// even if the live agent/environment was removed after the session snapshot.
	if b.state.Run == nil {
		if err := b.Check(ctx); err != nil {
			return err
		}
	}

	// A stable daemon identity reconnects to the same runtime registration.
	if err = b.register(ctx, binding); err != nil {
		return err
	}
	if err = b.persist(); err != nil {
		return err
	}
	b.log.InfoContext(ctx, "Qoder runtime registered", "runtime_id", b.state.RuntimeID)
	if b.cfg.OnReady != nil {
		if err := b.cfg.OnReady(ctx, b.state.RuntimeID); err != nil {
			return err
		}
	}
	ticker := time.NewTicker(b.cfg.PollInterval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		// Heartbeats run even while event delivery or submission reconciliation is failing.
		if err := b.multica.call(ctx, http.MethodPost, "/api/daemon/heartbeat", map[string]any{"runtime_id": b.state.RuntimeID}, nil); err != nil {
			b.log.WarnContext(ctx, "runtime heartbeat failed", "error", err)
			var denied *apiError
			if errors.As(err, &denied) && (denied.Status == 401 || denied.Status == 403 || denied.Status == 404) {
				if b.state.Run != nil && b.state.Run.SessionID != "" && b.state.Run.Phase != "final" {
					if cancelErr := b.cancel(ctx); cancelErr != nil {
						b.log.WarnContext(ctx, "waiting to stop remote work after runtime access was lost", "error", cancelErr)
						select {
						case <-ctx.Done():
							return nil
						case <-ticker.C:
							continue
						}
					}
				}
				return fmt.Errorf("runtime access lost: %w", err)
			}
		} else if err := b.guardedStep(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			var diskErr *persistenceError
			if errors.As(err, &diskErr) {
				return err
			}
			b.log.WarnContext(ctx, "Qoder bridge will retry reconciliation", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
func (b *Bridge) register(ctx context.Context, binding string) error {
	var response struct {
		Runtimes []struct {
			ID       string `json:"id"`
			Provider string `json:"provider"`
		} `json:"runtimes"`
	}
	err := b.multica.call(ctx, http.MethodPost, "/api/daemon/register", map[string]any{
		"workspace_id": b.cfg.WorkspaceID, "daemon_id": "qoder-cloud-" + binding[:24], "device_name": b.cfg.Name,
		"runtimes": []map[string]string{{"name": b.cfg.Name, "type": Provider, "status": "online", "runtime_mode": "cloud"}},
	}, &response)
	if err != nil {
		return err
	}
	if len(response.Runtimes) != 1 || response.Runtimes[0].ID == "" || response.Runtimes[0].Provider != Provider {
		return fmt.Errorf("invalid runtime registration response")
	}
	if b.state.Run != nil && b.state.RuntimeID != "" && b.state.RuntimeID != response.Runtimes[0].ID {
		return fmt.Errorf("active journal belongs to a deleted runtime; reconcile its remote session before restarting")
	}
	b.state.RuntimeID = response.Runtimes[0].ID
	return nil
}
func (b *Bridge) taskCall(ctx context.Context, suffix string, body, out any) error {
	return b.multica.call(ctx, http.MethodPost, "/api/daemon/tasks/"+url.PathEscape(b.state.Run.TaskID)+suffix, body, out)
}
func (b *Bridge) step(ctx context.Context) error {
	if b.state.Run == nil {
		var response struct {
			Task *claimedTask `json:"task"`
		}
		if err := b.multica.call(ctx, http.MethodPost, "/api/daemon/runtimes/"+url.PathEscape(b.state.RuntimeID)+"/tasks/claim", map[string]any{}, &response); err != nil {
			return err
		}
		if response.Task == nil {
			return nil
		}
		r, err := b.prepare(ctx, response.Task)
		if err != nil {
			r = &run{TaskID: response.Task.ID, Phase: "final", Outcome: "failed", Failure: err.Error()}
		}
		b.state.Run = r
		if err := b.persist(); err != nil {
			return err
		}
	}
	r := b.state.Run
	var status struct {
		Status string `json:"status"`
	}
	err := b.multica.call(ctx, http.MethodGet, "/api/daemon/tasks/"+url.PathEscape(r.TaskID)+"/status", nil, &status)
	var remoteErr *apiError
	if err != nil && !(errors.As(err, &remoteErr) && remoteErr.Status == 404) {
		return err
	}
	if err != nil || status.Status == "cancelled" || status.Status == "failed" || status.Status == "completed" {
		// Do not leave remote work running after deletion, cancellation or server-side timeout.
		if r.SessionID != "" && r.Phase != "final" {
			if err := b.cancel(ctx); err != nil {
				return err
			}
		}
		if status.Status == "cancelled" {
			if err := b.taskCall(ctx, "/cancel-ack", map[string]any{}, nil); err != nil {
				return err
			}
		}
		b.state.Run = nil
		return b.persist()
	}
	if r.Phase == "final" {
		return b.deliver(ctx)
	}
	if status.Status == "dispatched" {
		if err := b.taskCall(ctx, "/start", map[string]any{}, nil); err != nil {
			return err
		}
	} else if status.Status != "running" {
		return fmt.Errorf("unexpected Multica run status %q", status.Status)
	}
	if r.Phase == "prepared" {
		resources, err := b.sessionResources(r.Resources)
		if err != nil {
			return b.reject(ctx, err.Error())
		}
		var session struct {
			ID string `json:"id"`
		}
		if err := b.qoder.call(ctx, http.MethodPost, "/sessions", map[string]any{"agent": r.AgentID, "environment_id": b.cfg.EnvironmentID, "title": "Multica run " + r.TaskID, "metadata": map[string]string{"multica_task_id": r.TaskID, "multica_workspace_id": b.cfg.WorkspaceID}, "resources": resources}, &session); err != nil {
			if definitelyRejected(err) {
				return b.reject(ctx, "Qoder rejected session creation: "+err.Error())
			}
			return err
		}
		if session.ID == "" {
			return fmt.Errorf("Qoder create returned no session ID")
		}
		r.SessionID = session.ID
		r.Phase = "created"
		if err := b.persist(); err != nil {
			return err
		}
	}
	if err := b.taskCall(ctx, "/session", map[string]string{"session_id": r.SessionID}, nil); err != nil {
		return err
	}
	if r.Phase == "created" {
		// Save intent before POST. After an ambiguous response/restart, never resend automatically.
		r.Phase = "sending"
		if err := b.persist(); err != nil {
			return err
		}
		if err := b.qoder.call(ctx, http.MethodPost, "/sessions/"+url.PathEscape(r.SessionID)+"/events", map[string]any{"events": []map[string]any{{"type": "user.message", "content": []map[string]string{{"type": "text", "text": r.Prompt}}}}}, nil); err != nil {
			if definitelyRejected(err) {
				return b.reject(ctx, "Qoder rejected the task message: "+err.Error())
			}
			return err
		}
		r.Phase = "running"
		if err := b.persist(); err != nil {
			return err
		}
	}
	return b.events(ctx)
}
func (b *Bridge) cancel(ctx context.Context) error {
	r := b.state.Run
	if err := b.qoder.call(ctx, http.MethodPost, "/sessions/"+url.PathEscape(r.SessionID)+"/cancel", map[string]any{}, nil); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == 404 {
			return nil
		}
		return err
	}
	var s struct {
		Status string `json:"status"`
	}
	if err := b.qoder.call(ctx, http.MethodGet, "/sessions/"+url.PathEscape(r.SessionID), nil, &s); err != nil {
		return err
	}
	if s.Status != "idle" && s.Status != "terminated" {
		return fmt.Errorf("waiting for Qoder cancellation")
	}
	return nil
}
func (b *Bridge) deliver(ctx context.Context) error {
	r := b.state.Run
	body := map[string]any{"session_id": r.SessionID}
	suffix := "/complete"
	if r.Outcome == "completed" {
		body["output"] = r.Output
	} else {
		suffix = "/fail"
		body["error"] = r.Failure
		body["failure_reason"] = "qoder_cloud_execution"
	}
	if err := b.taskCall(ctx, suffix, body, nil); err != nil {
		return err
	}
	b.state.Run = nil
	return b.persist()
}

func definitelyRejected(err error) bool {
	var rejected *apiError
	return errors.As(err, &rejected) && rejected.Status >= 400 && rejected.Status < 500 && rejected.Status != 408
}
func (b *Bridge) reject(ctx context.Context, reason string) error {
	r := b.state.Run
	r.Phase = "final"
	r.Outcome = "failed"
	r.Failure = reason
	if err := b.persist(); err != nil {
		return err
	}
	return b.deliver(ctx)
}

// Check validates access without registering a runtime or executing a task.
func (b *Bridge) Check(ctx context.Context) error {
	resources := []struct {
		api            *api
		path, id, name string
	}{
		{b.multica, "/api/workspaces/" + url.PathEscape(b.cfg.WorkspaceID), b.cfg.WorkspaceID, "Multica workspace"},
		{b.qoder, "/environments/" + url.PathEscape(b.cfg.EnvironmentID), b.cfg.EnvironmentID, "Qoder environment"},
	}
	for _, resource := range resources {
		var response struct {
			ID string `json:"id"`
		}
		if err := resource.api.call(ctx, http.MethodGet, resource.path, nil, &response); err != nil {
			return fmt.Errorf("validate %s: %w", resource.name, err)
		}
		if response.ID != resource.id {
			return fmt.Errorf("validate %s: unexpected resource identity", resource.name)
		}
	}
	return nil
}

func (b *Bridge) guardedStep(ctx context.Context) error {
	if b.cfg.BeforeStep != nil {
		release, err := b.cfg.BeforeStep(ctx)
		if err != nil {
			return err
		}
		defer release()
	}
	return b.step(ctx)
}
