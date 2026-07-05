package dingtalk

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// RegistrationSessionStatus is the discriminated state a `begin`
// session lives in. The HTTP status endpoint serializes the underlying
// string verbatim so the frontend can pattern-match without parsing
// prose. Values mirror the lark install flow: pending | success | error.
type RegistrationSessionStatus string

const (
	// RegistrationStatusPending means the QR has been minted and the
	// background goroutine is still polling DingTalk.
	RegistrationStatusPending RegistrationSessionStatus = "pending"

	// RegistrationStatusSuccess means the device flow returned
	// credentials AND the installation row committed. `installation_id`
	// is populated.
	RegistrationStatusSuccess RegistrationSessionStatus = "success"

	// RegistrationStatusError means the session reached a terminal
	// failure. `error_reason` is set to a stable code so the frontend
	// can render the right copy without parsing `error_message`.
	RegistrationStatusError RegistrationSessionStatus = "error"
)

// Reason codes the service stores on a failed session. Stable strings
// so the frontend can switch on them without parsing prose.
const (
	RegistrationReasonExpired                = "expired"
	RegistrationReasonInstallFailed          = "install_failed"
	RegistrationReasonProtocol               = "dingtalk_protocol_error"
	RegistrationReasonCredentialsCheckFailed = "credentials_check_failed"
	RegistrationReasonInstallationConflict   = "installation_conflict"
	RegistrationReasonInternalError          = "internal_error"
)

// AppCredentialVerifier validates a freshly minted (client_id,
// client_secret) pair before the service commits it. The production
// implementation exchanges the pair for an app access token — the
// cheapest call that proves the created app is alive. Nil disables the
// check (tests, offline deployments).
type AppCredentialVerifier interface {
	VerifyAppCredentials(ctx context.Context, clientID, clientSecret string) error
}

// RegistrationServiceConfig configures the service.
type RegistrationServiceConfig struct {
	// SessionTTL caps how long a successful or errored session stays in
	// the in-process cache before GC. Default 30 minutes — long enough
	// for the frontend to fetch the final status after the dialog
	// closes, short enough that abandoned sessions do not pin memory
	// forever.
	SessionTTL time.Duration

	// Now is overridable for deterministic expiry-bound tests.
	Now func() time.Time

	// Logger is used for protocol-level warnings. Nil uses slog.Default().
	Logger *slog.Logger
}

func (c RegistrationServiceConfig) withDefaults() RegistrationServiceConfig {
	if c.SessionTTL == 0 {
		c.SessionTTL = 30 * time.Minute
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// RegistrationService owns the device-flow install lifecycle. It is the
// one place that:
//
//  1. opens a new device-flow session against DingTalk (Begin),
//  2. tracks the session's polling state in-process,
//  3. runs the background polling goroutine,
//  4. on success, optionally verifies the credentials, then writes the
//     dingtalk channel_installation row.
//
// Unlike the lark flow there is no installer identity binding: the
// DingTalk poll success payload does not say who scanned, so binding a
// DingTalk account to a Multica user happens later through the
// channel binding-token flow once the inbound transport lands.
//
// In-process session storage is intentional, for the same reason the
// lark service documents: sessions are short-lived, the QR has no value
// outside the browser session that initiated it, and persisting
// half-completed installs would add a migration + GC sweep without
// delivering any product capability.
type RegistrationService struct {
	cfg         RegistrationServiceConfig
	client      *RegistrationClient
	installs    *InstallationService
	verifier    AppCredentialVerifier
	authQueries authQueriesAdapter

	// bus is optional. When wired (SetEventBus), a successful install
	// publishes dingtalk_installation:created the moment the row
	// commits, so every workspace client refreshes its connection badge
	// without waiting for a browser to poll the status endpoint. Nil is
	// valid — install still works, it just won't push the WS frame.
	bus *events.Bus

	mu       sync.Mutex
	sessions map[string]*registrationSession
}

// authQueriesAdapter is the minimal lookup surface the service needs
// before kicking off a session: agent ↔ workspace ownership validation.
// Kept as an interface so tests can drop in a stub instead of a real
// *db.Queries + Postgres fixture.
type authQueriesAdapter interface {
	GetAgentInWorkspace(ctx context.Context, params db.GetAgentInWorkspaceParams) (db.Agent, error)
}

// NewRegistrationService wires the device-flow client and the DB write
// path. verifier may be nil (credential check skipped); every other
// dependency missing surfaces as a constructor error so a silent
// half-init at startup cannot leave the install button returning 500s
// at runtime.
func NewRegistrationService(
	cfg RegistrationServiceConfig,
	client *RegistrationClient,
	installs *InstallationService,
	queries *db.Queries,
	verifier AppCredentialVerifier,
) (*RegistrationService, error) {
	if client == nil {
		return nil, errors.New("dingtalk registration: RegistrationClient is required")
	}
	if installs == nil {
		return nil, errors.New("dingtalk registration: InstallationService is required")
	}
	if queries == nil {
		return nil, errors.New("dingtalk registration: queries is required")
	}
	return &RegistrationService{
		cfg:         cfg.withDefaults(),
		client:      client,
		installs:    installs,
		verifier:    verifier,
		authQueries: queries,
		sessions:    make(map[string]*registrationSession),
	}, nil
}

// SetEventBus wires the optional event bus AFTER construction so the
// constructor-validation cases stay untouched and the bus remains
// nil-safe.
func (s *RegistrationService) SetEventBus(bus *events.Bus) {
	s.bus = bus
}

// publishInstalled emits dingtalk_installation:created on the optional
// bus. Both install and revoke broadcast to the whole workspace via the
// SubscribeAll fanout; the frontend invalidates the dingtalk
// installations query on the dingtalk_installation prefix. Nil-safe.
func (s *RegistrationService) publishInstalled(workspaceID, installationID pgtype.UUID) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(events.Event{
		Type:        protocol.EventDingTalkInstallationCreated,
		WorkspaceID: uuidString(workspaceID),
		ActorType:   "system",
		Payload:     map[string]any{"installation_id": uuidString(installationID)},
	})
}

// registrationSession is the in-memory state for one in-flight install.
type registrationSession struct {
	id          string
	workspaceID pgtype.UUID
	agentID     pgtype.UUID
	initiatorID pgtype.UUID

	deviceCode string
	qrCodeURL  string
	interval   time.Duration
	expiresAt  time.Time

	mu             sync.Mutex
	status         RegistrationSessionStatus
	installationID pgtype.UUID
	errorReason    string
	errorMessage   string
	gcAfter        time.Time
}

func (s *registrationSession) snapshot() RegistrationSessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return RegistrationSessionState{
		ID:             s.id,
		Status:         s.status,
		InstallationID: s.installationID,
		ErrorReason:    s.errorReason,
		ErrorMessage:   s.errorMessage,
	}
}

func (s *registrationSession) markSuccess(installationID pgtype.UUID, gcAfter time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = RegistrationStatusSuccess
	s.installationID = installationID
	s.gcAfter = gcAfter
}

func (s *registrationSession) markError(reason, msg string, gcAfter time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Idempotent: if a parallel goroutine already terminated the
	// session, don't clobber the first reason — the user already saw it.
	if s.status != RegistrationStatusPending {
		return
	}
	s.status = RegistrationStatusError
	s.errorReason = reason
	s.errorMessage = msg
	s.gcAfter = gcAfter
}

// RegistrationSessionState is the read-only snapshot the handler
// serializes to the frontend.
type RegistrationSessionState struct {
	ID             string
	Status         RegistrationSessionStatus
	InstallationID pgtype.UUID
	ErrorReason    string
	ErrorMessage   string
}

// BeginInstallParams is the trusted input from the handler — the
// workspace, agent, and initiating user have already been authenticated
// and authorized at the router (admin role on the workspace; agent
// belongs to the workspace).
type BeginInstallParams struct {
	WorkspaceID pgtype.UUID
	AgentID     pgtype.UUID
	InitiatorID pgtype.UUID
}

// BeginInstallResult is the public payload the handler echoes to the
// frontend. The session_id is the opaque handle the frontend uses to
// poll status; we deliberately do NOT echo the device_code (DingTalk
// would honor a poll from anywhere if it leaked).
type BeginInstallResult struct {
	SessionID           string
	QRCodeURL           string
	ExpiresInSeconds    int
	PollIntervalSeconds int
}

// BeginInstall opens a fresh device-flow session and kicks off the
// background polling goroutine. The returned payload feeds the QR-code
// dialog on the frontend; the polling goroutine runs until success,
// terminal failure, or device_code expiry.
func (s *RegistrationService) BeginInstall(ctx context.Context, p BeginInstallParams) (BeginInstallResult, error) {
	if !p.WorkspaceID.Valid || !p.AgentID.Valid || !p.InitiatorID.Valid {
		return BeginInstallResult{}, errors.New("dingtalk registration: workspace, agent, and initiator are required")
	}
	// Agent ownership pre-check — without this, a workspace admin could
	// open an install session against another workspace's agent by
	// guessing the UUID. The handler does the same check; doing it here
	// too keeps the service self-defending. (Unlike Lark's flow there
	// is no name pre-fill: the DingTalk protocol carries no app-name
	// field, so the agent row is only consulted for ownership.)
	if _, err := s.authQueries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          p.AgentID,
		WorkspaceID: p.WorkspaceID,
	}); err != nil {
		return BeginInstallResult{}, fmt.Errorf("dingtalk registration: agent not in workspace: %w", err)
	}

	begin, err := s.client.Begin(ctx)
	if err != nil {
		return BeginInstallResult{}, fmt.Errorf("dingtalk registration: begin: %w", err)
	}

	now := s.cfg.Now()
	sessionID, err := randomSessionID()
	if err != nil {
		return BeginInstallResult{}, fmt.Errorf("dingtalk registration: mint session id: %w", err)
	}
	sess := &registrationSession{
		id:          sessionID,
		workspaceID: p.WorkspaceID,
		agentID:     p.AgentID,
		initiatorID: p.InitiatorID,
		deviceCode:  begin.DeviceCode,
		qrCodeURL:   begin.QRCodeURL,
		interval:    begin.Interval,
		expiresAt:   now.Add(begin.ExpiresIn),
		status:      RegistrationStatusPending,
	}
	s.mu.Lock()
	s.sessions[sessionID] = sess
	s.mu.Unlock()

	// The polling goroutine outlives the request context, so we cannot
	// reuse ctx here. Its own context is sized to the (capped) device
	// code expiry — see registrationMaxPollWindow.
	go s.runPolling(sess)

	return BeginInstallResult{
		SessionID:           sessionID,
		QRCodeURL:           begin.QRCodeURL,
		ExpiresInSeconds:    int(begin.ExpiresIn / time.Second),
		PollIntervalSeconds: int(begin.Interval / time.Second),
	}, nil
}

// GetSession returns the current state of an in-flight or recently-
// finished session. The workspace UUID is required so a session
// initiated by one workspace cannot be polled by another.
//
// ErrRegistrationSessionNotFound is returned for unknown / expired /
// GC'd sessions; the frontend treats it the same as an error reason of
// "session_lost" — prompt the user to restart the install.
func (s *RegistrationService) GetSession(workspaceID pgtype.UUID, sessionID string) (RegistrationSessionState, error) {
	if strings.TrimSpace(sessionID) == "" {
		return RegistrationSessionState{}, ErrRegistrationSessionNotFound
	}
	s.gcExpired()
	s.mu.Lock()
	sess, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return RegistrationSessionState{}, ErrRegistrationSessionNotFound
	}
	if !uuidEqual(sess.workspaceID, workspaceID) {
		// Treat as not found — leaking "exists but wrong workspace"
		// would let an attacker enumerate session ids across workspaces.
		return RegistrationSessionState{}, ErrRegistrationSessionNotFound
	}
	return sess.snapshot(), nil
}

// runPolling is the background loop: wait → poll → branch on result.
func (s *RegistrationService) runPolling(sess *registrationSession) {
	ctx, cancel := context.WithDeadline(context.Background(), sess.expiresAt)
	defer cancel()

	interval := sess.interval
	if interval <= 0 {
		interval = time.Duration(registrationDefaultPollSeconds) * time.Second
	}

	for {
		select {
		case <-ctx.Done():
			s.cfg.Logger.Info("dingtalk registration: session expired",
				"session_id", sess.id,
				"workspace_id", uuidString(sess.workspaceID))
			sess.markError(RegistrationReasonExpired, "QR expired before authorization", s.gcDeadline())
			return
		case <-time.After(interval):
		}

		res, err := s.client.Poll(ctx, sess.deviceCode)
		if err != nil {
			var re *RegistrationError
			if errors.As(err, &re) {
				s.cfg.Logger.Warn("dingtalk registration: protocol error",
					"session_id", sess.id, "code", re.Code, "desc", re.Description)
				sess.markError(RegistrationReasonProtocol, re.Error(), s.gcDeadline())
				return
			}
			// Transient transport error (DNS, network) — log and try
			// again on the next tick rather than killing the session.
			s.cfg.Logger.Warn("dingtalk registration: transport error, will retry",
				"session_id", sess.id, "err", err)
			continue
		}

		switch {
		case res.ClientID != "" && res.ClientSecret != "":
			s.finishSuccess(ctx, sess, res)
			return
		case res.Err != nil:
			reason := RegistrationReasonProtocol
			switch res.Err.Code {
			case "expired":
				reason = RegistrationReasonExpired
			case "fail":
				// FAIL covers user-denied and DingTalk-side create
				// failures alike; fail_reason (prose) rides along in
				// error_message for the diagnostic tooltip.
				reason = RegistrationReasonInstallFailed
			}
			s.cfg.Logger.Info("dingtalk registration: terminal error",
				"session_id", sess.id, "code", res.Err.Code, "desc", res.Err.Description)
			sess.markError(reason, res.Err.Error(), s.gcDeadline())
			return
		default:
			// WAITING — keep the interval, loop.
		}
	}
}

// finishSuccess runs the post-poll finalization: optional credential
// verification, then the installation upsert. A single row write —
// no transaction needed (contrast lark, which pairs the install with
// an installer binding; DingTalk's poll payload has no installer
// identity to bind).
func (s *RegistrationService) finishSuccess(ctx context.Context, sess *registrationSession, res *RegistrationPollResult) {
	if s.verifier != nil {
		if err := s.verifier.VerifyAppCredentials(ctx, res.ClientID, res.ClientSecret); err != nil {
			s.cfg.Logger.Warn("dingtalk registration: credentials check failed",
				"session_id", sess.id, "err", err)
			sess.markError(RegistrationReasonCredentialsCheckFailed, err.Error(), s.gcDeadline())
			return
		}
	}

	inst, err := s.installs.Upsert(ctx, InstallationParams{
		WorkspaceID:     sess.workspaceID,
		AgentID:         sess.agentID,
		ClientID:        res.ClientID,
		ClientSecret:    res.ClientSecret,
		InstallerUserID: sess.initiatorID,
	})
	if err != nil {
		s.cfg.Logger.Warn("dingtalk registration: upsert installation",
			"session_id", sess.id, "err", err)
		sess.markError(RegistrationReasonInstallationConflict, err.Error(), s.gcDeadline())
		return
	}

	sess.markSuccess(inst.ID, s.gcDeadline())
	// Publish at the commit point so the connection badge updates on
	// every workspace client without a page refresh — not only on the
	// tab that happens to poll the status endpoint to success.
	s.publishInstalled(sess.workspaceID, inst.ID)
	s.cfg.Logger.Info("dingtalk registration: install complete",
		"session_id", sess.id,
		"workspace_id", uuidString(sess.workspaceID),
		"agent_id", uuidString(sess.agentID),
		"installation_id", uuidString(inst.ID))
}

func (s *RegistrationService) gcDeadline() time.Time {
	return s.cfg.Now().Add(s.cfg.SessionTTL)
}

// gcExpired drops any session whose `gcAfter` is in the past. Pending
// sessions are NOT GC'd here — runPolling sets their gcAfter when it
// terminates, and an expired-by-deadline session closes itself.
func (s *RegistrationService) gcExpired() {
	now := s.cfg.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sess := range s.sessions {
		sess.mu.Lock()
		drop := !sess.gcAfter.IsZero() && sess.gcAfter.Before(now)
		sess.mu.Unlock()
		if drop {
			delete(s.sessions, id)
		}
	}
}

// ErrRegistrationSessionNotFound is what the service returns for
// unknown / GC'd sessions. The handler maps it to 404.
var ErrRegistrationSessionNotFound = errors.New("dingtalk registration: session not found")

func randomSessionID() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func uuidEqual(a, b pgtype.UUID) bool {
	if !a.Valid || !b.Valid {
		return false
	}
	return a.Bytes == b.Bytes
}

func uuidString(u pgtype.UUID) string { return util.UUIDToString(u) }

// truncate caps s at n bytes for log/error tails.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
