package qoderruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const Provider = "qoder_cloud"

type Config struct {
	DaemonID         string                                `json:"daemon_id,omitempty"`
	LoadState        func(context.Context) ([]byte, error) `json:"-"`
	SaveState        func(context.Context, []byte) error   `json:"-"`
	BeforeStep       func(context.Context) (func(), error) `json:"-"`
	OnReady          func(context.Context, string) error   `json:"-"`
	GitHubTokens     map[string]string                     `json:"-"`
	GitHubTokenFiles map[string]string                     `json:"github_token_files,omitempty"`
	// GitHubAppToken issues a short-lived installation token restricted to one
	// repository through the workspace's GitHub App integration. It returns
	// ErrGitHubAppNotCovered when no bound installation authorizes the
	// repository. The token stays in the transient request copy only.
	GitHubAppToken    func(ctx context.Context, owner, repo string) (string, int64, error) `json:"-"`
	MulticaURL        string                                                               `json:"multica_url"`
	MulticaToken      string                                                               `json:"multica_token"`
	WorkspaceID       string                                                               `json:"workspace_id"`
	QoderURL          string                                                               `json:"qoder_url"`
	QoderToken        string                                                               `json:"qoder_token"`
	EnvironmentID     string                                                               `json:"environment_id"`
	StateDir          string                                                               `json:"state_dir"`
	Name              string                                                               `json:"name,omitempty"`
	RunTimeout        time.Duration                                                        `json:"-"`
	SubmissionTimeout time.Duration                                                        `json:"-"`
	PollInterval      time.Duration                                                        `json:"-"`
	// MaxConcurrency bounds simultaneous remote sessions; extra tasks stay queued in Multica.
	MaxConcurrency int `json:"max_concurrency,omitempty"`
}

// MaxConcurrencyLimit caps how many sessions one connection may run at once.
const MaxConcurrencyLimit = 10

type Bridge struct {
	cfg            Config
	multica, qoder *api
	state          *journal
	path           string
	log            *slog.Logger
	turn           int
	replyTurn      int
	replyStep      bool
}

func New(cfg Config, log *slog.Logger) (*Bridge, error) {
	workspace, err := uuid.Parse(cfg.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspace ID must be a UUID")
	}
	cfg.WorkspaceID = workspace.String()
	if cfg.EnvironmentID == "" {
		return nil, fmt.Errorf("environment ID is required")
	}
	if (cfg.LoadState == nil) != (cfg.SaveState == nil) {
		return nil, fmt.Errorf("journal load and save must be configured together")
	}
	if cfg.LoadState == nil && cfg.StateDir == "" {
		return nil, fmt.Errorf("state directory is required when the journal is stored on disk")
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 3 * time.Second
	}
	if cfg.PollInterval < time.Millisecond {
		return nil, fmt.Errorf("poll interval must be positive")
	}
	if cfg.RunTimeout == 0 {
		cfg.RunTimeout = time.Hour
	}
	if cfg.SubmissionTimeout == 0 {
		cfg.SubmissionTimeout = 2 * time.Minute
	}
	if cfg.RunTimeout < time.Second || cfg.SubmissionTimeout < time.Second {
		return nil, fmt.Errorf("timeouts must be positive")
	}
	if cfg.MaxConcurrency == 0 {
		cfg.MaxConcurrency = 1
	}
	if cfg.MaxConcurrency < 1 || cfg.MaxConcurrency > MaxConcurrencyLimit {
		return nil, fmt.Errorf("max concurrency must be between 1 and %d", MaxConcurrencyLimit)
	}
	if cfg.Name == "" {
		cfg.Name = "Qoder Cloud Agents"
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

// Run is single-instance per journal and processes up to MaxConcurrency runs.
// Restart resumes the journal instead of declaring remote work orphaned. An
// on-disk journal is guarded by a file lock; an external journal's owner
// (the server's Qoder supervisor) must guarantee a single bridge itself.
func (b *Bridge) Run(ctx context.Context) error {
	var err error
	if b.cfg.LoadState == nil {
		if err = os.MkdirAll(b.cfg.StateDir, 0700); err != nil {
			return err
		}
		lock, err := lockState(filepath.Join(b.cfg.StateDir, "bridge.lock"))
		if err != nil {
			return err
		}
		defer lock.Close()
	}
	binding := Binding(b.multica.base, b.cfg.WorkspaceID, b.qoder.base, b.cfg.EnvironmentID)
	if b.cfg.SaveState != nil {
		binding = Binding("", b.cfg.WorkspaceID, b.qoder.base, b.cfg.EnvironmentID)
	}
	if b.cfg.LoadState != nil {
		var raw []byte
		raw, err = b.cfg.LoadState(ctx)
		if err == nil {
			b.state, err = decodeJournal(raw, binding)
		}
	} else {
		b.state, err = readJournal(b.path, binding)
	}
	if err != nil {
		return err
	}
	// Validate remote access before accepting work. Existing runs still reconcile
	// even if the live agent/environment was removed after the session snapshot.
	if len(b.state.Runs) == 0 {
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
		b.cleanup(ctx)
		// Heartbeats run even while event delivery or submission reconciliation is failing.
		if err := b.multica.call(ctx, http.MethodPost, "/api/daemon/heartbeat", map[string]any{"runtime_id": b.state.RuntimeID}, nil); err != nil {
			b.log.WarnContext(ctx, "runtime heartbeat failed", "error", err)
			var denied *apiError
			if errors.As(err, &denied) && (denied.Status == 401 || denied.Status == 403 || denied.Status == 404) {
				if cancelErr := b.cancelActive(ctx); cancelErr != nil {
					b.log.WarnContext(ctx, "waiting to stop remote work after runtime access was lost", "error", cancelErr)
					select {
					case <-ctx.Done():
						return nil
					case <-ticker.C:
						continue
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
	daemonID := b.cfg.DaemonID
	if daemonID == "" {
		daemonID = "qoder-cloud-" + binding[:24]
	}
	var response struct {
		Runtimes []struct {
			ID       string `json:"id"`
			Provider string `json:"provider"`
		} `json:"runtimes"`
	}
	err := b.multica.call(ctx, http.MethodPost, "/api/daemon/register", map[string]any{
		"workspace_id": b.cfg.WorkspaceID, "daemon_id": daemonID, "device_name": b.cfg.Name,
		"runtimes": []map[string]string{{"name": b.cfg.Name, "type": Provider, "status": "online", "runtime_mode": "cloud"}},
	}, &response)
	if err != nil {
		return err
	}
	if len(response.Runtimes) != 1 || response.Runtimes[0].ID == "" || response.Runtimes[0].Provider != Provider {
		return fmt.Errorf("invalid runtime registration response")
	}
	if len(b.state.Runs) > 0 && b.state.RuntimeID != "" && b.state.RuntimeID != response.Runtimes[0].ID {
		return fmt.Errorf("active journal belongs to a deleted runtime; reconcile its remote session before restarting")
	}
	b.state.RuntimeID = response.Runtimes[0].ID
	return nil
}
func (b *Bridge) taskCall(ctx context.Context, r *run, suffix string, body, out any) error {
	return b.multica.call(ctx, http.MethodPost, "/api/daemon/tasks/"+url.PathEscape(r.TaskID)+suffix, body, out)
}

// step alternates normal polling with one pending file transfer. Each runs
// under a separate guard and deadline; transfers cannot spend another run's
// budget, and normal polling and heartbeats still run between transfers.
func (b *Bridge) step(ctx context.Context) error {
	if b.replyStep {
		b.replyStep = false
		if r := b.nextReplyRun(); r != nil {
			b.replyTurn++
			statusCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := b.stepRun(statusCtx, r)
			cancel()
			if err != nil {
				return err
			}
			// The status check may have removed a cancelled or completed run.
			for _, active := range b.state.Runs {
				if active == r {
					if pending, err := b.attachReplyFile(ctx, r); err != nil || pending {
						return err
					}
					return b.deliver(ctx, r)
				}
			}
			return nil
		}
	}
	err := b.stepRuns(ctx)
	b.replyStep = b.nextReplyRun() != nil
	return err
}

func (b *Bridge) nextReplyRun() *run {
	for i := range b.state.Runs {
		r := b.state.Runs[(b.replyTurn+i)%len(b.state.Runs)]
		if r.Phase == "final" && r.Outcome == "completed" && r.Reply != nil && r.Reply.Handled < len(r.Reply.Delivered) {
			return r
		}
	}
	return nil
}

// stepRuns advances every active run, then claims new work while capacity remains.
// One failing run must not stall the others, so errors are collected and the
// first is returned after all runs had their turn. Rotating the starting run
// keeps a slow provider call from starving the same run on every tick.
func (b *Bridge) stepRuns(ctx context.Context) error {
	var firstErr error
	keep := func(r *run, err error) error {
		var diskErr *persistenceError
		if errors.As(err, &diskErr) {
			return err
		}
		if firstErr == nil {
			firstErr = fmt.Errorf("run %s: %w", r.TaskID, err)
		}
		return nil
	}
	runs := append([]*run(nil), b.state.Runs...)
	for i := range runs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r := runs[(b.turn+i)%len(runs)]
		if err := b.stepRun(ctx, r); err != nil {
			if err := keep(r, err); err != nil {
				return err
			}
		}
	}
	b.turn++
	for len(b.state.Runs) < b.cfg.MaxConcurrency && ctx.Err() == nil {
		r, err := b.claim(ctx)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			break
		}
		if r == nil {
			break
		}
		if err := b.stepRun(ctx, r); err != nil {
			if err := keep(r, err); err != nil {
				return err
			}
		}
	}
	return firstErr
}

func (b *Bridge) claim(ctx context.Context) (*run, error) {
	var response struct {
		Task *claimedTask `json:"task"`
	}
	if err := b.multica.call(ctx, http.MethodPost, "/api/daemon/runtimes/"+url.PathEscape(b.state.RuntimeID)+"/tasks/claim", map[string]any{}, &response); err != nil {
		return nil, err
	}
	if response.Task == nil {
		return nil, nil
	}
	r, err := b.prepare(ctx, response.Task)
	if err != nil {
		r = &run{TaskID: response.Task.ID, Phase: "final", Outcome: "failed", Failure: err.Error()}
	}
	r.Deadline = time.Now().Add(b.cfg.RunTimeout)
	b.state.Runs = append(b.state.Runs, r)
	if err := b.persist(); err != nil {
		return nil, err
	}
	return r, nil
}

func (b *Bridge) stepRun(ctx context.Context, r *run) error {
	if r.Phase != "final" {
		if r.Deadline.IsZero() {
			r.Deadline = time.Now().Add(b.cfg.RunTimeout)
			if err := b.persist(); err != nil {
				return err
			}
		}
		if time.Now().After(r.Deadline) || (!r.SubmissionDeadline.IsZero() && !r.Started && time.Now().After(r.SubmissionDeadline)) {
			// Persist compensating cancellation separately; a provider outage must
			// not block the next task or discard the remote session identity.
			if r.SessionID != "" {
				b.queueCleanup(r.SessionID)
				if err := b.persist(); err != nil {
					return err
				}
				b.cleanup(ctx)
			}
			return b.reject(ctx, r, "Qoder execution deadline exceeded")
		}
	}
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
			b.queueCleanup(r.SessionID)
			if err := b.persist(); err != nil {
				return err
			}
			b.cleanup(ctx)
		}
		if err == nil {
			if err := b.reportCredits(ctx, r); err != nil {
				return err
			}
		}
		if status.Status == "cancelled" {
			if err := b.taskCall(ctx, r, "/cancel-ack", map[string]any{}, nil); err != nil {
				return err
			}
		}
		b.state.remove(r)
		return b.persist()
	}
	if r.Phase == "final" {
		return b.deliver(ctx, r)
	}
	if time.Now().Before(r.RetryAt) {
		return nil
	}
	if r.Phase == "running" {
		if err := b.refreshRunningCredentials(ctx, r); err != nil {
			return err
		}
	}
	if r.Phase == "creating" {
		// The provider may have created a session before the response was lost.
		// Never create a second session automatically for the same run.
		return b.reject(ctx, r, "Qoder session creation was not confirmed; inspect remote sessions before retrying")
	}
	if status.Status == "dispatched" {
		if err := b.taskCall(ctx, r, "/start", map[string]any{}, nil); err != nil {
			return err
		}
	} else if status.Status != "running" {
		return fmt.Errorf("unexpected Multica run status %q", status.Status)
	}
	if r.Phase == "prepared" {
		// Resolve credentials before reusing a session too, so a revoked or
		// changed repository credential is never carried into a follow-up.
		resources, fingerprint, rotating, err := b.sessionResources(ctx, r.Resources)
		if err != nil {
			return b.reject(ctx, r, err.Error())
		}
		// A rotating credential is timestamped for the live refresh; only the
		// timestamp is journaled, never the token.
		if rotating {
			r.CredentialIssuedAt = time.Now()
		} else {
			r.CredentialIssuedAt = time.Time{}
		}
		repositoryCopies := resources
		resources = append(resources, r.Files...)
		if r.PriorSessionID != "" && (len(r.Files) == 0 || r.MountFiles) {
			continued, err := b.continueSession(ctx, r, fingerprint)
			if err != nil {
				return err
			}
			if continued {
				r.SessionID = r.PriorSessionID
				r.Continued = true
				// A GitHub App credential rotates on every resolution; push the
				// fresh token into the reused session before any message goes
				// out. The run stays "prepared" until the refresh completed, so
				// a rate-limited or failed refresh retries the whole continue
				// path next tick instead of sending with a stale credential.
				if rotating {
					switch err := b.refreshRepositoryCredentials(ctx, r.SessionID, repositoryCopies); {
					case err == nil:
					case errors.Is(err, errRefreshDeferred):
						r.RetryAt = time.Now().Add(30 * time.Second)
						return b.persist()
					case errors.Is(err, errRefreshRefused):
						// The session cannot take the update (for example the
						// repository is no longer mounted there); start a fresh
						// session below.
						r.SessionID, r.PriorSessionID, r.Continued = "", "", false
						r.Credits = runCredits{}
						r.MountedFiles = 0
					default:
						return err
					}
				}
				if r.Continued {
					// One journal write covers both the completed refresh and
					// the phase flip.
					r.Phase = "created"
					if err := b.persist(); err != nil {
						return err
					}
				}
			}
		}
		if r.Phase == "prepared" {
			if r.Continued {
				// A deferred refresh left the run bound to a prior session
				// that is no longer continuable; start clean so the fresh
				// session is not treated as continued (no history cursor, no
				// remounted files, no stale credit base).
				r.SessionID, r.PriorSessionID, r.Continued = "", "", false
				r.Credits = runCredits{}
			}
			if err := b.createSession(ctx, r, resources, fingerprint); err != nil || r.Phase != "created" {
				return err
			}
		}
	}
	if err := b.taskCall(ctx, r, "/session", map[string]string{"session_id": r.SessionID}, nil); err != nil {
		return err
	}
	if r.Phase == "created" && r.Continued && r.Cursor == "" {
		latest, err := b.latestEventID(ctx, r.SessionID)
		if definitelyRejected(err) {
			// The earlier session is gone; nothing was sent yet, so start fresh.
			r.SessionID, r.PriorSessionID, r.Continued = "", "", false
			r.Credits = runCredits{}
			r.Phase = "prepared"
			return b.persist()
		}
		if err != nil {
			return err
		}
		r.Cursor = latest
	}
	if r.Phase == "created" && r.Continued {
		// A continued session lacks this message's files; mount them before the
		// message that names them. The cursor is already set, and events before
		// the message are skipped, so the mounts do not read as this turn.
		for r.MountedFiles < len(r.Files) {
			err := b.qoder.call(ctx, http.MethodPost, "/sessions/"+url.PathEscape(r.SessionID)+"/resources", r.Files[r.MountedFiles], nil)
			if rateLimited(err) {
				r.RetryAt = time.Now().Add(30 * time.Second)
				return b.persist()
			}
			if definitelyRejected(err) {
				// Nothing was sent yet; start a fresh session with the files instead.
				r.SessionID, r.PriorSessionID, r.Continued, r.Cursor = "", "", false, ""
				r.Credits = runCredits{}
				r.MountedFiles = 0
				r.Phase = "prepared"
				return b.persist()
			}
			if err != nil {
				return err
			}
			r.MountedFiles++
			if err := b.persist(); err != nil {
				return err
			}
		}
	}
	if r.Phase == "created" {
		// Save intent before POST. After an ambiguous response/restart, never resend automatically.
		r.Phase = "sending"
		r.SubmissionDeadline = time.Now().Add(b.cfg.SubmissionTimeout)
		if err := b.persist(); err != nil {
			return err
		}
		content := []map[string]any{{"type": "text", "text": r.Prompt}}
		for _, source := range r.Images {
			content = append(content, map[string]any{"type": "image", "source": source})
		}
		if err := b.qoder.call(ctx, http.MethodPost, "/sessions/"+url.PathEscape(r.SessionID)+"/events", map[string]any{"events": []map[string]any{{"type": "user.message", "content": content}}}, nil); err != nil {
			if rateLimited(err) {
				r.Phase = "created"
				r.SubmissionDeadline = time.Time{}
				r.RetryAt = time.Now().Add(30 * time.Second)
				return b.persist()
			}
			if definitelyRejected(err) && r.Continued {
				// The earlier session can no longer take messages; a definite
				// rejection means nothing was delivered, so start a fresh session.
				r.SessionID, r.PriorSessionID, r.Continued, r.Cursor = "", "", false, ""
				r.Credits = runCredits{}
				r.Phase = "prepared"
				r.SubmissionDeadline = time.Time{}
				return b.persist()
			}
			if definitelyRejected(err) {
				return b.reject(ctx, r, "Qoder rejected the task message: "+err.Error())
			}
			return err
		}
		r.Phase = "running"
		r.Images = nil
		if err := b.persist(); err != nil {
			return err
		}
	}
	return b.events(ctx, r)
}

// createSession moves the run to "created" on success. A rate-limited request
// leaves it "prepared" for a later retry; a definite refusal finalizes it.
func (b *Bridge) createSession(ctx context.Context, r *run, resources []map[string]any, fingerprint string) error {
	var session struct {
		ID string `json:"id"`
	}
	r.Phase = "creating"
	if err := b.persist(); err != nil {
		return err
	}
	metadata := map[string]string{"source": "multica", "multica_task_id": r.TaskID, "multica_workspace_id": b.cfg.WorkspaceID, resourcesMetadataKey: fingerprint}
	if err := b.qoder.call(ctx, http.MethodPost, "/sessions", map[string]any{"agent": r.AgentID, "environment_id": b.cfg.EnvironmentID, "title": "Multica run " + r.TaskID, "metadata": metadata, "resources": resources}, &session); err != nil {
		if rateLimited(err) {
			r.Phase = "prepared"
			r.RetryAt = time.Now().Add(30 * time.Second)
			return b.persist()
		}
		if definitelyRejected(err) {
			return b.reject(ctx, r, "Qoder rejected session creation: "+err.Error())
		}
		return err
	}
	if session.ID == "" {
		return fmt.Errorf("Qoder create returned no session ID")
	}
	r.SessionID = session.ID
	r.Phase = "created"
	return b.persist()
}

// resourcesMetadataKey stores resourceFingerprint on each session Multica creates.
const resourcesMetadataKey = "multica_resources"

// fingerprintEntry is one repository's stable credential identity: a manual
// token hashes as itself (rotation must invalidate session reuse), a GitHub
// App token hashes as its installation binding so rotating short-lived tokens
// keep the fingerprint stable.
type fingerprintEntry struct {
	key, branch, credential string
}

// resourceFingerprint identifies the repositories, branches and credentials a
// session was created with. Only this digest leaves the bridge.
func resourceFingerprint(entries []fingerprintEntry) string {
	h := sha256.New()
	h.Write([]byte("multica-qoder-resources-v1\n"))
	for _, entry := range entries {
		fmt.Fprintf(h, "%s\x00%s\x00%s\n", entry.key, entry.branch, entry.credential)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// checkoutBranch reads the branch from a freshly built resource
// (map[string]string) or one restored from the journal (map[string]any).
func checkoutBranch(checkout any) string {
	raw, err := json.Marshal(checkout)
	if err != nil {
		return ""
	}
	var c struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &c)
	return c.Name
}

// continueSession reports whether the agent's previous session on this issue
// can take the follow-up. Only an idle session that no other active run is
// using, created with the same repositories, branches and credentials,
// qualifies; anything else (including sessions without a fingerprint) falls
// back to a new session.
func (b *Bridge) continueSession(ctx context.Context, r *run, fingerprint string) (bool, error) {
	for _, other := range b.state.Runs {
		if other != r && other.SessionID == r.PriorSessionID {
			return false, nil
		}
	}
	var s struct {
		sessionUsage
		Status        string            `json:"status"`
		Agent         json.RawMessage   `json:"agent"`
		EnvironmentID string            `json:"environment_id"`
		Metadata      map[string]string `json:"metadata"`
	}
	if err := b.qoder.call(ctx, http.MethodGet, "/sessions/"+url.PathEscape(r.PriorSessionID), nil, &s); err != nil {
		if definitelyRejected(err) {
			return false, nil
		}
		return false, err
	}
	if s.Status != "idle" {
		return false, nil
	}
	if id := sessionAgentID(s.Agent); (id != "" && id != r.AgentID) || (s.EnvironmentID != "" && s.EnvironmentID != b.cfg.EnvironmentID) {
		return false, nil
	}

	if s.Metadata[resourcesMetadataKey] != fingerprint {
		return false, nil
	}
	r.Credits = runCredits{}
	if total := s.total(); total != nil {
		base := creditsMicros(*total)
		r.Credits.Base = &base
	}
	return true, nil
}

// errRefreshDeferred and errRefreshRefused classify a repository credential
// refresh: the session's provider asked us to retry later, or refused the
// update (including a repository that is no longer mounted), respectively.
var (
	errRefreshDeferred = errors.New("session resource update rate limited")
	errRefreshRefused  = errors.New("session refused the repository credential update")
)

// sessionRepositoryResourceID finds the mounted-resource ID of one GitHub
// repository in a session, following the resource list's pagination.
func (b *Bridge) sessionRepositoryResourceID(ctx context.Context, sessionID, key string) (string, error) {
	page := ""
	for range 100 {
		path := "/sessions/" + url.PathEscape(sessionID) + "/resources"
		if page != "" {
			path += "?page=" + url.QueryEscape(page)
		}
		var response struct {
			Data []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"data"`
			NextPage string `json:"next_page"`
		}
		if err := b.qoder.call(ctx, http.MethodGet, path, nil, &response); err != nil {
			return "", err
		}
		for _, resource := range response.Data {
			if resource.Type != "github_repository" {
				continue
			}
			if candidate, err := repositoryKey(resource.URL); err == nil && candidate == key {
				return resource.ID, nil
			}
		}
		if response.NextPage == "" {
			return "", nil
		}
		page = response.NextPage
	}
	return "", fmt.Errorf("session resource list did not settle")
}

// refreshRepositoryCredentials pushes fresh tokens into a session's mounted
// repositories. The add-resource endpoint only accepts files, so an existing
// repository is updated through its per-resource credential endpoint, matched
// by canonical repository URL so a token never lands on another resource.
func (b *Bridge) refreshRepositoryCredentials(ctx context.Context, sessionID string, resources []map[string]any) error {
	for i := range resources {
		raw, _ := resources[i]["url"].(string)
		key, err := repositoryKey(raw)
		if err != nil {
			return fmt.Errorf("refresh session repository credential: %w", err)
		}
		token, _ := resources[i]["authorization_token"].(string)
		resourceID, err := b.sessionRepositoryResourceID(ctx, sessionID, key)
		if err != nil {
			if rateLimited(err) {
				return errRefreshDeferred
			}
			if definitelyRejected(err) {
				return errRefreshRefused
			}
			return err
		}
		if resourceID == "" {
			return errRefreshRefused
		}
		err = b.qoder.call(ctx, http.MethodPost, "/sessions/"+url.PathEscape(sessionID)+"/resources/"+url.PathEscape(resourceID), map[string]string{"authorization_token": token}, nil)
		if err != nil {
			if rateLimited(err) {
				return errRefreshDeferred
			}
			if definitelyRejected(err) {
				return errRefreshRefused
			}
			return err
		}
	}
	return nil
}

// credentialRefreshInterval is how long a run waits before re-issuing a
// rotating GitHub App credential into its live session; GitHub installation
// tokens expire after one hour, so the refresh lands with margin to spare.
const credentialRefreshInterval = 45 * time.Minute

// refreshRunningCredentials keeps a live session's short-lived GitHub token
// usable for runs longer than the token's lifetime: about 45 minutes after
// the last issuance it re-resolves the credentials and pushes them into the
// session. Only the issuance timestamp is journaled, never the token; its
// zero value disables the refresh (static credentials, or a session that
// refused the update for good). Transient failures back off and retry.
func (b *Bridge) refreshRunningCredentials(ctx context.Context, r *run) error {
	if r.CredentialIssuedAt.IsZero() || time.Now().Before(r.CredentialIssuedAt.Add(credentialRefreshInterval)) {
		return nil
	}
	resources, _, rotating, err := b.sessionResources(ctx, r.Resources)
	if err != nil {
		// The run is already executing; a failed refresh (for example a
		// just-revoked installation) must not kill it. Retry next tick.
		b.log.WarnContext(ctx, "Qoder credential refresh failed", "task_id", r.TaskID, "error", err)
		r.CredentialIssuedAt = time.Now().Add(-credentialRefreshInterval + time.Minute)
		return b.persist()
	}
	if !rotating {
		// Credentials became manual mid-flight (for example an operator bound
		// a PAT); nothing expires anymore.
		r.CredentialIssuedAt = time.Time{}
		return b.persist()
	}
	if err := b.refreshRepositoryCredentials(ctx, r.SessionID, resources); err != nil {
		b.log.WarnContext(ctx, "Qoder credential refresh push failed", "task_id", r.TaskID, "error", err)
		if errors.Is(err, errRefreshRefused) {
			r.CredentialIssuedAt = time.Time{}
		} else {
			r.CredentialIssuedAt = time.Now().Add(-credentialRefreshInterval + time.Minute)
		}
		return b.persist()
	}
	r.CredentialIssuedAt = time.Now()
	return b.persist()
}

// sessionAgentID accepts the agent as a bare ID or as an object with an id.
func sessionAgentID(raw json.RawMessage) string {
	var id string
	if json.Unmarshal(raw, &id) == nil {
		return id
	}
	var obj struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &obj)
	return obj.ID
}

// cancelActive stops every remote session that is still executing.
func (b *Bridge) cancelActive(ctx context.Context) error {
	var firstErr error
	for _, r := range b.state.Runs {
		if r.SessionID == "" || r.Phase == "final" {
			continue
		}
		if err := b.cancelSession(ctx, r.SessionID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
func (b *Bridge) cancelSession(ctx context.Context, sessionID string) error {
	if err := b.qoder.call(ctx, http.MethodPost, "/sessions/"+url.PathEscape(sessionID)+"/cancel", map[string]any{}, nil); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == 404 {
			return nil
		}
		return err
	}
	var s struct {
		Status string `json:"status"`
	}
	if err := b.qoder.call(ctx, http.MethodGet, "/sessions/"+url.PathEscape(sessionID), nil, &s); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == 404 {
			return nil
		}
		return err
	}
	if s.Status != "idle" && s.Status != "terminated" {
		return fmt.Errorf("waiting for Qoder cancellation")
	}
	return nil
}
func (b *Bridge) deliver(ctx context.Context, r *run) error {
	if r.QuickCreate != nil && !r.QuickCreate.Created && r.Outcome == "completed" {
		if err := b.createQuickIssue(ctx, r); err != nil {
			return err
		}
	}
	if r.Reply != nil && r.Outcome == "completed" {
		if r.Reply.Handled < len(r.Reply.Delivered) {
			return nil
		}
	}
	if err := b.reportCredits(ctx, r); err != nil {
		return err
	}
	if r.Leader != nil && r.Outcome == "completed" {
		if err := b.deliverLeader(ctx, r); err != nil {
			return err
		}
	}
	body := map[string]any{"session_id": r.SessionID}
	suffix := "/complete"
	if r.Outcome == "completed" {
		body["output"] = r.Output
		if r.Reply != nil {
			body["output"] = r.Reply.output(r.Output)
		}
	} else {
		suffix = "/fail"
		body["error"] = r.Failure
		body["failure_reason"] = "qoder_cloud_execution"
	}
	if err := b.taskCall(ctx, r, suffix, body, nil); err != nil {
		return err
	}
	b.state.remove(r)
	return b.persist()
}

func definitelyRejected(err error) bool {
	var rejected *apiError
	return errors.As(err, &rejected) && rejected.Status >= 400 && rejected.Status < 500 && rejected.Status != 408 && rejected.Status != 429
}
func (b *Bridge) reject(ctx context.Context, r *run, reason string) error {
	r.Phase = "final"
	r.Outcome = "failed"
	r.Failure = reason
	if err := b.persist(); err != nil {
		return err
	}
	return b.deliver(ctx, r)
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
		if resource.api == b.multica && strings.HasPrefix(b.cfg.MulticaToken, "mdt_") {
			continue
		}
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
		acquireCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		release, err := b.cfg.BeforeStep(acquireCtx)
		cancel()
		if err != nil {
			return err
		}
		defer release()
	}
	// The managed guard's 45-second lease starts when acquisition commits.
	// Give the work its own 30 seconds after acquisition, within that lease.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return b.step(ctx)
}

func rateLimited(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Status == http.StatusTooManyRequests
}

func (b *Bridge) queueCleanup(id string) {
	for _, existing := range b.state.Cleanup {
		if existing == id {
			return
		}
	}
	b.state.Cleanup = append(b.state.Cleanup, id)
}
func (b *Bridge) cleanup(ctx context.Context) {
	if len(b.state.Cleanup) == 0 {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := b.cancelSession(cleanupCtx, b.state.Cleanup[0]); err != nil {
		return
	}
	b.state.Cleanup = b.state.Cleanup[1:]
	if err := b.persist(); err != nil {
		b.log.ErrorContext(ctx, "cannot persist remote cleanup", "error", err)
	}
}

// Binding identifies one workspace/environment independently of process lifetime.
func Binding(multicaURL, workspaceID, qoderURL, environmentID string) string {
	sum := sha256.Sum256([]byte(strings.TrimRight(multicaURL, "/") + "\n" + workspaceID + "\n" + strings.TrimRight(qoderURL, "/") + "\n" + environmentID))
	return hex.EncodeToString(sum[:])
}

// CancelRemoteSession is used by the managed supervisor after user access is
// revoked. It needs only Qoder credentials, never a former owner's Multica PAT.
func CancelRemoteSession(ctx context.Context, baseURL, token, sessionID string) error {
	api, err := newAPI(baseURL, token, "")
	if err != nil {
		return err
	}
	b := &Bridge{qoder: api}
	return b.cancelSession(ctx, sessionID)
}
