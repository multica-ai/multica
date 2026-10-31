package claudemanagedruntime

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
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

const Provider = "claude_managed"

var validVaultID = regexp.MustCompile(`^vlt_[a-zA-Z0-9_-]{1,128}$`)

type Config struct {
	DaemonID         string                                `json:"daemon_id,omitempty"`
	LoadState        func(context.Context) ([]byte, error) `json:"-"`
	SaveState        func(context.Context, []byte) error   `json:"-"`
	BeforeStep       func(context.Context) (func(), error) `json:"-"`
	OnReady          func(context.Context, string) error   `json:"-"`
	GitHubTokens     map[string]string                     `json:"-"`
	GitHubTokenFiles map[string]string                     `json:"github_token_files,omitempty"`
	MulticaURL       string                                `json:"multica_url"`
	MulticaToken     string                                `json:"multica_token"`
	WorkspaceID      string                                `json:"workspace_id"`
	AnthropicURL     string                                `json:"anthropic_url"`
	AnthropicToken   string                                `json:"anthropic_token"`
	EnvironmentID    string                                `json:"environment_id"`
	// VaultIDs are attached to every new session, e.g. to authenticate a
	// GitHub MCP server that opens pull requests.
	VaultIDs          []string      `json:"vault_ids,omitempty"`
	StateDir          string        `json:"state_dir"`
	Name              string        `json:"name,omitempty"`
	RunTimeout        time.Duration `json:"-"`
	SubmissionTimeout time.Duration `json:"-"`
	PollInterval      time.Duration `json:"-"`
	// MaxConcurrency bounds simultaneous remote sessions; extra tasks stay queued in Multica.
	MaxConcurrency int `json:"max_concurrency,omitempty"`
}

// MaxConcurrencyLimit caps how many sessions one connection may run at once.
const MaxConcurrencyLimit = 10

type Bridge struct {
	cfg                Config
	multica, anthropic *api
	state              *journal
	path               string
	log                *slog.Logger
	turn               int
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
	for _, id := range cfg.VaultIDs {
		if !validVaultID.MatchString(id) {
			return nil, fmt.Errorf("vault IDs must look like vlt_...")
		}
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
		cfg.Name = "Claude Managed Agents"
	}
	m, err := newAPI(cfg.MulticaURL, cfg.MulticaToken, cfg.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("Multica: %w", err)
	}
	if cfg.AnthropicURL == "" {
		cfg.AnthropicURL = DefaultAnthropicURL
	}
	q, err := newAnthropicAPI(cfg.AnthropicURL, cfg.AnthropicToken)
	if err != nil {
		return nil, fmt.Errorf("Anthropic: %w", err)
	}
	if log == nil {
		log = slog.Default()
	}
	return &Bridge{cfg: cfg, multica: m, anthropic: q, path: filepath.Join(cfg.StateDir, "state.json"), log: log}, nil
}

// Run is single-instance per journal and processes up to MaxConcurrency runs.
// Restart resumes the journal instead of declaring remote work orphaned. An
// on-disk journal is guarded by a file lock; an external journal's owner
// (the server's Claude Managed Agents supervisor) must guarantee a single bridge itself.
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
	binding := Binding(b.multica.base, b.cfg.WorkspaceID, b.anthropic.base, b.cfg.EnvironmentID)
	if b.cfg.SaveState != nil {
		binding = Binding("", b.cfg.WorkspaceID, b.anthropic.base, b.cfg.EnvironmentID)
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
	b.log.InfoContext(ctx, "Claude Managed Agents runtime registered", "runtime_id", b.state.RuntimeID)
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
			b.log.WarnContext(ctx, "Claude Managed Agents bridge will retry reconciliation", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// StandaloneDaemonPrefix starts the default daemon_id of a bridge run outside
// the server. It must stay clear of the "claude-managed-" namespace, which the
// server reserves for the bridge it runs itself.
const StandaloneDaemonPrefix = "claude-bridge-"

func (b *Bridge) register(ctx context.Context, binding string) error {
	daemonID := b.cfg.DaemonID
	if daemonID == "" {
		daemonID = StandaloneDaemonPrefix + binding[:24]
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

// step advances every active run, then claims new work while capacity remains.
// One failing run must not stall the others, so errors are collected and the
// first is returned after all runs had their turn. Rotating the starting run
// keeps a slow provider call from starving the same run on every tick.
func (b *Bridge) step(ctx context.Context) error {
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
			return b.rejectWithCleanup(ctx, r, "Claude execution deadline exceeded")
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
		if r.Phase != "final" {
			if err := b.cleanupRemote(ctx, r); err != nil {
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
	if r.Phase == "creating" {
		// The provider may have created a session before the response was lost.
		// Reattach to it; create again only once nothing carries this task's ID.
		sessionID, err := b.findCreatedSession(ctx, r)
		if err != nil {
			if definitelyRejected(err) {
				b.logPossibleSession(ctx, r, err)
				return b.reject(ctx, r, "Claude rejected the session lookup after an unconfirmed create: "+err.Error())
			}
			r.RetryAt = time.Now().Add(retryDelay(err))
			if perr := b.persist(); perr != nil {
				return perr
			}
			return err
		}
		switch {
		case sessionID != "":
			r.SessionID = sessionID
			r.Phase, r.Started = "running", true
		case r.CreateRetried:
			return b.reject(ctx, r, "Claude session creation was not confirmed; inspect remote sessions before retrying")
		default:
			r.CreateRetried = true
			r.Phase = "prepared"
		}
		if err := b.persist(); err != nil {
			return err
		}
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
		resources, err := b.sessionResources(r.Resources)
		if err != nil {
			return b.reject(ctx, r, err.Error())
		}
		fingerprint := b.sessionFingerprint(resources)
		if r.PriorSessionID != "" {
			continued, err := b.continueSession(ctx, r, fingerprint)
			if err != nil {
				return err
			}
			if continued {
				r.SessionID = r.PriorSessionID
				r.Continued = true
				r.Phase = "created"
				if err := b.persist(); err != nil {
					return err
				}
			}
		}
		if r.Phase == "prepared" {
			if err := b.createSession(ctx, r, resources, fingerprint); err != nil || r.Phase != "running" {
				return err
			}
		}
	}
	if err := b.taskCall(ctx, r, "/session", map[string]string{"session_id": r.SessionID}, nil); err != nil {
		return err
	}
	if r.Phase == "created" {
		// Save intent before POST. After an ambiguous response/restart, never resend automatically.
		r.Phase = "sending"
		r.SubmissionDeadline = time.Now().Add(b.cfg.SubmissionTimeout)
		if err := b.persist(); err != nil {
			return err
		}
		if err := b.anthropic.call(ctx, http.MethodPost, "/sessions/"+url.PathEscape(r.SessionID)+"/events", map[string]any{"events": userMessage(r.Prompt)}, nil); err != nil {
			if rateLimited(err) {
				r.Phase = "created"
				r.SubmissionDeadline = time.Time{}
				r.RetryAt = time.Now().Add(retryDelay(err))
				return b.persist()
			}
			if definitelyRejected(err) && r.Continued {
				// The earlier session can no longer take messages; a definite
				// rejection means nothing was delivered, so start a fresh session.
				r.SessionID, r.PriorSessionID, r.Continued = "", "", false
				r.Phase = "prepared"
				r.SubmissionDeadline = time.Time{}
				return b.persist()
			}
			if definitelyRejected(err) {
				return b.reject(ctx, r, "Claude rejected the task message: "+err.Error())
			}
			return err
		}
		r.Phase = "running"
		if err := b.persist(); err != nil {
			return err
		}
	}
	return b.events(ctx, r)
}

// userMessage is the event that hands the run's prompt to Claude.
func userMessage(prompt string) []map[string]any {
	return []map[string]any{{"type": "user.message", "content": []map[string]string{{"type": "text", "text": prompt}}}}
}

// createSession starts the session with the prompt as its initial event, so
// creation and submission are one request, and moves the run to "running"
// (started) on success. A rate-limited request leaves it "prepared" for a
// later retry; a definite refusal finalizes it.
func (b *Bridge) createSession(ctx context.Context, r *run, resources []map[string]any, fingerprint string) error {
	var session struct {
		ID string `json:"id"`
	}
	r.Phase = "creating"
	if r.CreateAttemptAt.IsZero() {
		r.CreateAttemptAt = time.Now()
	}
	if err := b.persist(); err != nil {
		return err
	}
	metadata := map[string]string{"multica_task_id": r.TaskID, "multica_workspace_id": b.cfg.WorkspaceID, resourcesMetadataKey: fingerprint}
	request := map[string]any{"agent": r.AgentID, "environment_id": b.cfg.EnvironmentID, "title": "Multica run " + r.TaskID, "metadata": metadata, "resources": resources, "initial_events": userMessage(r.Prompt)}
	if len(b.cfg.VaultIDs) > 0 {
		request["vault_ids"] = b.cfg.VaultIDs
	}
	if err := b.anthropic.call(ctx, http.MethodPost, "/sessions", request, &session); err != nil {
		if rateLimited(err) {
			r.Phase = "prepared"
			r.RetryAt = time.Now().Add(retryDelay(err))
			return b.persist()
		}
		if definitelyRejected(err) {
			return b.reject(ctx, r, "Claude rejected session creation: "+err.Error())
		}
		return err
	}
	if session.ID == "" {
		return fmt.Errorf("Claude create returned no session ID")
	}
	// The prompt went out as the session's initial event, so the run has
	// started; its whole history belongs to this run.
	r.SessionID = session.ID
	r.Phase, r.Started = "running", true
	return b.persist()
}

// createClockSkew widens the created_at lower bound of the ambiguous-create
// lookup, because the attempt time comes from the bridge's clock while
// created_at comes from Anthropic's. The task ID match is what identifies the
// session, so a generous window costs only a longer listing.
const createClockSkew = 5 * time.Minute

// maxCreateLookupPages bounds the ambiguous-create lookup; sessions of one
// agent created within minutes of the attempt fit in far fewer pages.
const maxCreateLookupPages = 10

// findCreatedSession returns the session an unconfirmed create made for r, or
// "" when the agent has no session carrying r's task ID since the attempt
// within maxCreateLookupPages.
func (b *Bridge) findCreatedSession(ctx context.Context, r *run) (string, error) {
	q := url.Values{
		"agent_id":        {r.AgentID},
		"created_at[gte]": {r.CreateAttemptAt.Add(-createClockSkew).UTC().Format(time.RFC3339)},
		"order":           {"desc"},
		"limit":           {"100"},
	}
	for range maxCreateLookupPages {
		var page struct {
			Data []struct {
				ID       string            `json:"id"`
				Metadata map[string]string `json:"metadata"`
			} `json:"data"`
			NextPage *string `json:"next_page"`
		}
		if err := b.anthropic.call(ctx, http.MethodGet, "/sessions?"+q.Encode(), nil, &page); err != nil {
			return "", err
		}
		for _, s := range page.Data {
			if s.ID != "" && s.Metadata["multica_task_id"] == r.TaskID && s.Metadata["multica_workspace_id"] == b.cfg.WorkspaceID {
				return s.ID, nil
			}
		}
		if page.NextPage == nil || *page.NextPage == "" || *page.NextPage == q.Get("page") {
			return "", nil
		}
		q.Set("page", *page.NextPage)
	}
	// Treat a truncated listing as not found, so the single retry or the
	// unconfirmed-creation failure applies instead of retrying the lookup forever.
	b.log.WarnContext(ctx, "Claude session lookup reached its page limit; treating the unconfirmed create as not found", "task_id", r.TaskID, "agent_id", r.AgentID, "pages", maxCreateLookupPages)
	return "", nil
}

// resourcesMetadataKey stores resourceFingerprint on each session Multica creates.
const resourcesMetadataKey = "multica_resources"

// resourceFingerprint identifies the repositories, branches and credentials a
// session was created with. Only this digest leaves the bridge.
func resourceFingerprint(resources []map[string]any) string {
	h := sha256.New()
	h.Write([]byte("multica-claude-resources-v1\n"))
	for _, res := range resources {
		raw, _ := res["url"].(string)
		key, _ := repositoryKey(raw)
		token, _ := res["authorization_token"].(string)
		fmt.Fprintf(h, "%s\x00%s\x00%s\n", key, checkoutBranch(res["checkout"]), token)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// sessionFingerprint identifies what a session was created with. Vaults are
// included because vault_ids cannot be changed on an existing session; without
// any, it equals resourceFingerprint so earlier sessions stay reusable.
func (b *Bridge) sessionFingerprint(resources []map[string]any) string {
	fingerprint := resourceFingerprint(resources)
	if len(b.cfg.VaultIDs) == 0 {
		return fingerprint
	}
	h := sha256.New()
	h.Write([]byte(fingerprint))
	for _, id := range slices.Sorted(slices.Values(b.cfg.VaultIDs)) {
		fmt.Fprintf(h, "\x00vault\x00%s", id)
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
		Status        string            `json:"status"`
		Agent         json.RawMessage   `json:"agent"`
		EnvironmentID string            `json:"environment_id"`
		Metadata      map[string]string `json:"metadata"`
	}
	if err := b.anthropic.call(ctx, http.MethodGet, "/sessions/"+url.PathEscape(r.PriorSessionID), nil, &s); err != nil {
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
	return s.Metadata[resourcesMetadataKey] == fingerprint, nil
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

// Claude has no cancel endpoint: an interrupt stops the current turn and the
// session becomes idle. Sessions are never archived or deleted so their
// history stays available.
var (
	cancelConfirmWait = 3 * time.Second
	cancelConfirmPoll = 250 * time.Millisecond
)

// cancelSession interrupts a session and confirms it stopped. An error leaves
// the session queued for retry; a missing session counts as already stopped.
// A stopped session may refuse an interrupt, so it is not sent one, and a
// definite refusal counts as success once the session reads as stopped.
func (b *Bridge) cancelSession(ctx context.Context, sessionID string) error {
	path := "/sessions/" + url.PathEscape(sessionID)
	if stopped, err := b.sessionStopped(ctx, path); err != nil || stopped {
		return err
	}
	interrupt := map[string]any{"events": []map[string]string{{"type": "user.interrupt"}}}
	if err := b.anthropic.call(ctx, http.MethodPost, path+"/events", interrupt, nil); err != nil {
		if err = ignoreMissing(err); err == nil || !definitelyRejected(err) {
			return err
		}
		if stopped, checkErr := b.sessionStopped(ctx, path); checkErr == nil && stopped {
			return nil
		}
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, cancelConfirmWait)
	defer cancel()
	for {
		stopped, err := b.sessionStopped(ctx, path)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("waiting for Claude cancellation")
			}
			return err
		}
		if stopped {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for Claude cancellation")
		case <-time.After(cancelConfirmPoll):
		}
	}
}

// sessionStopped reports whether the session at path is idle or terminated;
// a missing session counts as stopped.
func (b *Bridge) sessionStopped(ctx context.Context, path string) (bool, error) {
	var s struct {
		Status string `json:"status"`
	}
	if err := b.anthropic.call(ctx, http.MethodGet, path, nil, &s); err != nil {
		if err = ignoreMissing(err); err != nil {
			return false, err
		}
		return true, nil
	}
	return s.Status == "idle" || s.Status == "terminated", nil
}

func ignoreMissing(err error) error {
	var ae *apiError
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		return nil
	}
	return err
}
func (b *Bridge) deliver(ctx context.Context, r *run) error {
	body := map[string]any{"session_id": r.SessionID}
	suffix := "/complete"
	if r.Outcome == "completed" {
		body["output"] = r.Output
	} else {
		suffix = "/fail"
		body["error"] = r.Failure
		body["failure_reason"] = "claude_managed_execution"
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

// rejectWithCleanup fails r after queueing its remote session for cancellation.
func (b *Bridge) rejectWithCleanup(ctx context.Context, r *run, reason string) error {
	if err := b.cleanupRemote(ctx, r); err != nil {
		return err
	}
	return b.reject(ctx, r, reason)
}

// cleanupRemote queues r's remote session for cancellation. Without a session
// ID, an unconfirmed create may still have made one, so the lookup runs
// first; a failed lookup does not hold the run back.
// Persist compensating cancellation separately; a provider outage must not
// block the next task or discard the remote session identity.
func (b *Bridge) cleanupRemote(ctx context.Context, r *run) error {
	sessionID := r.SessionID
	if sessionID == "" && (r.Phase == "creating" || !r.CreateAttemptAt.IsZero()) {
		found, err := b.findCreatedSession(ctx, r)
		if err != nil {
			b.logPossibleSession(ctx, r, err)
		}
		sessionID = found
	}
	if sessionID != "" {
		b.queueCleanup(sessionID)
		if err := b.persist(); err != nil {
			return err
		}
		b.cleanup(ctx)
	}
	return nil
}

func (b *Bridge) logPossibleSession(ctx context.Context, r *run, err error) {
	b.log.WarnContext(ctx, "Claude session lookup failed; a remote session may exist for this task and must be inspected manually", "task_id", r.TaskID, "agent_id", r.AgentID, "error", err)
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
	type checked struct {
		api            *api
		path, id, name string
	}
	resources := []checked{
		{b.multica, "/api/workspaces/" + url.PathEscape(b.cfg.WorkspaceID), b.cfg.WorkspaceID, "Multica workspace"},
		{b.anthropic, "/environments/" + url.PathEscape(b.cfg.EnvironmentID), b.cfg.EnvironmentID, "Claude environment"},
	}
	for _, id := range b.cfg.VaultIDs {
		resources = append(resources, checked{b.anthropic, "/vaults/" + url.PathEscape(id), id, "Claude vault " + id})
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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if b.cfg.BeforeStep != nil {
		release, err := b.cfg.BeforeStep(ctx)
		if err != nil {
			return err
		}
		defer release()
	}
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
	// Entries are tried in order within one timeout; a failing entry stays
	// queued without holding back the ones after it.
	cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var pending []string
	for _, id := range b.state.Cleanup {
		if cleanupCtx.Err() != nil || b.cancelSession(cleanupCtx, id) != nil {
			pending = append(pending, id)
		}
	}
	if len(pending) == len(b.state.Cleanup) {
		return
	}
	b.state.Cleanup = pending
	if err := b.persist(); err != nil {
		b.log.ErrorContext(ctx, "cannot persist remote cleanup", "error", err)
	}
}

// Binding identifies one workspace/environment independently of process lifetime.
func Binding(multicaURL, workspaceID, anthropicURL, environmentID string) string {
	sum := sha256.Sum256([]byte(strings.TrimRight(multicaURL, "/") + "\n" + workspaceID + "\n" + strings.TrimRight(anthropicURL, "/") + "\n" + environmentID))
	return hex.EncodeToString(sum[:])
}

// CancelRemoteSession is used by the managed supervisor after user access is
// revoked. It needs only Anthropic credentials, never a former owner's Multica PAT.
func CancelRemoteSession(ctx context.Context, baseURL, token, sessionID string) error {
	api, err := newAnthropicAPI(baseURL, token)
	if err != nil {
		return err
	}
	b := &Bridge{anthropic: api}
	return b.cancelSession(ctx, sessionID)
}
