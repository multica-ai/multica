package lark

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

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// pgUniqueViolation is the Postgres SQLSTATE for a unique-constraint violation.
// A rebind upsert that trips the (channel_type, config->>'app_id') index after
// the dead-owner reclaim ran means a LIVE owner still holds the slot.
const pgUniqueViolation = "23505"

// RegistrationSessionStatus is the discriminated state a `begin`
// session lives in. The HTTP status endpoint serializes the underlying
// string verbatim so the frontend can pattern-match without parsing
// prose.
type RegistrationSessionStatus string

const (
	// RegistrationStatusPending means the QR has been minted and the
	// background goroutine is still polling Lark. The frontend keeps
	// polling our status endpoint at the cadence we return in
	// poll_interval_seconds.
	RegistrationStatusPending RegistrationSessionStatus = "pending"

	// RegistrationStatusSuccess means the device-flow returned
	// credentials AND the lark_installation + installer-binding pair
	// committed. `installation_id` is populated. The frontend closes
	// the dialog and invalidates the installations cache.
	RegistrationStatusSuccess RegistrationSessionStatus = "success"

	// RegistrationStatusError means the session reached a terminal
	// failure (expired, user-denied, Lark protocol error, follow-up
	// bot-info / DB error). `error_reason` is set to a stable code so
	// the frontend can render the right copy without parsing
	// `error_message`.
	RegistrationStatusError RegistrationSessionStatus = "error"
)

// Reason codes the service stores on a failed session. Stable strings
// so the frontend can switch on them without parsing prose.
const (
	RegistrationReasonExpired              = "expired"
	RegistrationReasonAccessDenied         = "access_denied"
	RegistrationReasonProtocol             = "lark_protocol_error"
	RegistrationReasonBotInfoFailed        = "bot_info_failed"
	RegistrationReasonInstallationConflict = "installation_conflict"
	RegistrationReasonInstallerBindFailed  = "installer_bind_failed"
	RegistrationReasonInternalError        = "internal_error"
)

// RegistrationServiceConfig configures the service.
type RegistrationServiceConfig struct {
	// SessionTTL caps how long a finished session stays readable, and is
	// also the tail added to the QR window when the session is first
	// registered. Default 30 minutes — long enough for the frontend to
	// fetch the final status after the dialog closes, short enough that
	// finished sessions do not linger. Independent of the device-flow
	// expiry (Lark's expires_in, currently 1h), which bounds the PENDING
	// phase.
	SessionTTL time.Duration

	// Now is overridable for deterministic expiry-bound tests.
	Now func() time.Time

	// Logger is used for protocol-level warnings (Lark error codes,
	// post-success bot info failures). Nil uses slog.Default().
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
//  1. opens a new device-flow session against Lark (Begin),
//  2. tracks the session's polling state in-process,
//  3. runs the background polling goroutine,
//  4. on success, calls APIClient.GetBotInfo with the freshly minted
//     credentials, then writes lark_installation + the installer's
//     lark_user_binding in a single transaction.
//
// The status the browser polls lives in the shared InstallSessionStore
// (Redis in multi-replica deploys), NOT in this process. It used to be
// in-process only, which meant GET /lark/install/{id}/status could be
// answered solely by the process that served begin — any other replica
// 404'd and the dialog gave up ~5s in with "session lost" (MUL-7340).
// The in-process map below now holds only what the polling goroutine
// needs to talk to Lark; the device_code never leaves it.
type RegistrationService struct {
	cfg          RegistrationServiceConfig
	client       *RegistrationClient
	api          APIClient
	queries      *ChannelStore
	tx           TxStarter
	installs     *InstallationService
	binder       InstallerBinder
	authQueries  authQueriesAdapter
	sessionStore InstallSessionStore

	// bus is optional. When wired (SetEventBus), a successful install
	// publishes lark_installation:created the moment the row commits, so
	// every workspace client refreshes its connection badge without
	// waiting for a browser to poll the status endpoint to success. Nil
	// is valid — install still works, it just won't push the WS frame.
	bus *events.Bus

	// sessions holds the polling goroutines' working state, keyed by
	// session id. A miss here is normal and harmless: it just means the
	// goroutine belongs to another process (or has finished). Status
	// reads never consult this map.
	mu       sync.Mutex
	sessions map[string]*registrationSession
}

const (
	// sessionWriteTimeout bounds a single status write. These cannot ride
	// the polling goroutine's own context: that context is deadlined to
	// the device_code expiry, so the expiry branch itself would always
	// write with an already-cancelled context.
	sessionWriteTimeout = 10 * time.Second

	// Retry budget for recording a terminal outcome, sized to ride out a
	// Redis failover rather than just a dropped packet. 10 attempts means
	// 9 waits — 200ms doubling into the 15s cap — which is 55.4s of
	// backoff, plus up to sessionWriteTimeout per attempt on top. The
	// goroutine has no other work left at this point, and the alternative
	// (dropping a completion already committed in Postgres) is what leaves
	// a bound user staring at a pending dialog.
	//
	// This is a bounded recovery window, not a guarantee: an outage that
	// outlasts it still releases the session, and the read path then
	// reports expiry once the QR window closes.
	terminalWriteAttempts       = 10
	terminalWriteInitialBackoff = 200 * time.Millisecond
	terminalWriteMaxBackoff     = 15 * time.Second
)

// authQueriesAdapter is the minimal lookup surface the service needs
// before kicking off a session: agent ↔ workspace ownership validation.
// Kept as an interface so tests can drop in a stub instead of a real
// *db.Queries + Postgres fixture.
type authQueriesAdapter interface {
	GetAgentInWorkspace(ctx context.Context, params db.GetAgentInWorkspaceParams) (db.Agent, error)
}

// NewRegistrationService wires the device-flow client, the APIClient
// (for the post-success GetBotInfo lookup), and the DB write path. Any
// required dependency missing surfaces as a constructor error so a
// silent half-init at startup cannot leave the install button
// returning 500s at runtime.
func NewRegistrationService(
	cfg RegistrationServiceConfig,
	client *RegistrationClient,
	api APIClient,
	queries *db.Queries,
	tx TxStarter,
	installs *InstallationService,
	binder InstallerBinder,
) (*RegistrationService, error) {
	if client == nil {
		return nil, errors.New("lark registration: RegistrationClient is required")
	}
	if api == nil {
		return nil, errors.New("lark registration: APIClient is required")
	}
	if queries == nil {
		return nil, errors.New("lark registration: queries is required")
	}
	if tx == nil {
		return nil, errors.New("lark registration: TxStarter is required")
	}
	if installs == nil {
		return nil, errors.New("lark registration: InstallationService is required")
	}
	if binder == nil {
		return nil, errors.New("lark registration: InstallerBinder is required")
	}
	return &RegistrationService{
		cfg:         cfg.withDefaults(),
		client:      client,
		api:         api,
		queries:     NewChannelStore(queries),
		tx:          tx,
		installs:    installs,
		binder:      binder,
		authQueries: queries,
		// Defaults to the single-process store; SetInstallSessionStore
		// swaps in the Redis one when the deployment has Redis.
		sessionStore: NewMemoryInstallSessionStore(),
		sessions:     make(map[string]*registrationSession),
	}, nil
}

// SetEventBus wires the optional event bus AFTER construction so the
// six positional constructor-validation cases stay untouched and the
// bus remains nil-safe. With it set, finishSuccess publishes
// lark_installation:created at the row-commit point — the authoritative
// moment of truth — instead of relying on the HTTP status-poll handler
// to emit it only when a browser happens to poll to success.
func (s *RegistrationService) SetEventBus(bus *events.Bus) {
	s.bus = bus
}

// publishInstalled emits lark_installation:created on the optional bus.
// Mirrors the revoke path (RevokeLarkInstallation publishes
// lark_installation:revoked from its handler): both events broadcast to
// the whole workspace via the SubscribeAll fanout, and the frontend
// invalidates larkKeys.installations on the lark_installation prefix, so
// every mounted surface (agent Integrations tab, inspector, Settings)
// refreshes its connection badge with no page reload. Covers fresh
// installs and revoked→active re-installs alike — both ride the same
// UpsertLarkInstallation write. Nil-safe.
func (s *RegistrationService) publishInstalled(workspaceID, installationID pgtype.UUID) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(events.Event{
		Type:        protocol.EventLarkInstallationCreated,
		WorkspaceID: uuidString(workspaceID),
		ActorType:   "system",
		Payload:     map[string]any{"installation_id": uuidString(installationID)},
	})
}

// registrationSession is the polling goroutine's working state for one
// in-flight install. It holds ONLY what that goroutine needs in order to
// keep talking to Lark; the status the browser reads lives in the shared
// InstallSessionStore so any process can serve it.
//
// deviceCode deliberately stays here and is never persisted: it is a
// bearer credential — anyone holding it can complete the authorization —
// and only the process running the goroutine has any use for it.
type registrationSession struct {
	id          string
	workspaceID pgtype.UUID
	agentID     pgtype.UUID
	initiatorID pgtype.UUID

	deviceCode string
	domain     string
	qrCodeURL  string
	interval   time.Duration
	expiresAt  time.Time
	// region is the cloud the install was started against. The polling
	// loop reads it as the initial value of its `region` local; if the
	// poll stream surfaces a tenant_brand mid-flow, the local flips to
	// RegionLark, but the session field stays at what the user picked
	// (it is informational — the authoritative cloud flows back through
	// finishSuccess via the loop's local).
	region Region
}

// markSuccess records the terminal success and drops the goroutine's
// working state.
func (s *RegistrationService) markSuccess(sess *registrationSession, installationID pgtype.UUID) {
	s.markTerminal(sess, InstallSessionOutcome{
		Status:         RegistrationStatusSuccess,
		InstallationID: installationID,
	})
}

// markError records a terminal failure under the same first-writer-wins
// contract as markSuccess.
func (s *RegistrationService) markError(sess *registrationSession, reason, msg string) {
	s.markTerminal(sess, InstallSessionOutcome{
		Status:       RegistrationStatusError,
		ErrorReason:  reason,
		ErrorMessage: msg,
	})
}

// markTerminal writes the session's final outcome and then drops the
// goroutine's working state. The store applies first-writer-wins, so when
// the expiry deadline and a poll result race, whichever lands first is
// what the user keeps seeing.
//
// The write is RETRIED with backoff, and the session is held until it
// settles. By the time a success reaches here the installation and the
// installer binding are already committed in Postgres — so dropping this
// write on the first error would leave a user who bound successfully
// watching the dialog sit on "pending" until the QR expires, then fail.
// Retrying is safe because MarkTerminal is idempotent and
// first-writer-wins: a retry can only re-assert the outcome already
// recorded, never replace it with a different one.
//
// Retention runs from now rather than from the session's start: the
// dialog has to be able to read this outcome after the QR window closed.
func (s *RegistrationService) markTerminal(sess *registrationSession, outcome InstallSessionOutcome) {
	defer s.forgetSession(sess.id)

	delay := terminalWriteInitialBackoff
	var err error
	for attempt := 1; attempt <= terminalWriteAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), sessionWriteTimeout)
		err = s.sessionStore.MarkTerminal(ctx, sess.id, outcome, s.cfg.SessionTTL)
		cancel()
		if err == nil {
			return
		}
		// The session record is gone (expired out, or the store was
		// wiped). Nothing to record and nothing a retry could fix.
		if errors.Is(err, ErrInstallSessionNotFound) {
			s.cfg.Logger.Warn("lark registration: session gone before its outcome was recorded",
				"session_id", sess.id, "status", string(outcome.Status))
			return
		}
		if attempt == terminalWriteAttempts {
			break
		}
		s.cfg.Logger.Warn("lark registration: record session outcome failed, retrying",
			"session_id", sess.id, "attempt", attempt, "retry_in", delay, "err", err)
		time.Sleep(delay)
		if delay *= 2; delay > terminalWriteMaxBackoff {
			delay = terminalWriteMaxBackoff
		}
	}

	// Out of retries. For a success the install itself is live and
	// lark_installation:created already went out, so the workspace shows
	// the Bot connected; it is this one dialog that will time out. Log the
	// installation id so the outcome is recoverable by hand.
	s.cfg.Logger.Error("lark registration: gave up recording session outcome",
		"session_id", sess.id,
		"status", string(outcome.Status),
		"reason", outcome.ErrorReason,
		"installation_id", uuidString(outcome.InstallationID),
		"attempts", terminalWriteAttempts,
		"err", err)
}

// forgetSession drops the goroutine's working state once it has
// terminated. The durable row outlives it by SessionTTL so the dialog can
// still read the final status.
func (s *RegistrationService) forgetSession(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

// RegistrationSessionState is the read-only snapshot the handler
// serializes to the frontend. Internal mutex is hidden by construction.
// InitiatorID is not serialized to the client — the handler uses it only
// to authorize the status read (session initiator or workspace admin).
type RegistrationSessionState struct {
	ID             string
	Status         RegistrationSessionStatus
	InstallationID pgtype.UUID
	ErrorReason    string
	ErrorMessage   string
	InitiatorID    pgtype.UUID
}

// BeginInstallParams is the trusted input from the handler — the
// workspace, agent, and initiating user have already been authenticated
// and authorized by the handler (canManageAgent: the agent's owner OR a
// workspace owner/admin; agent belongs to the workspace). The service
// re-checks agent↔workspace membership below as defense-in-depth.
type BeginInstallParams struct {
	WorkspaceID pgtype.UUID
	AgentID     pgtype.UUID
	InitiatorID pgtype.UUID
	// Region picks which cloud's accounts host the device-flow begins
	// against — Feishu (mainland, accounts.feishu.cn) or Lark
	// (international, accounts.larksuite.com). The user picks this
	// explicitly in the UI ("Bind to Feishu" vs "Bind to Lark") so the
	// QR rendered up front already targets the right cloud and Lark
	// users do not have to hit a Feishu URL first and rely on the
	// tenant-brand auto-switch. Empty / unknown values fall back to
	// Feishu, matching RegionOrDefault, so existing callers without
	// the new field keep working.
	Region Region
}

// BeginInstallResult is the public payload the handler echoes to the
// frontend. The session_id is the opaque handle the frontend uses to
// poll status; we deliberately do NOT echo the device_code or the
// polling interval (which is internal scheduling state).
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
//
// The session_id is the only opaque token returned to the browser —
// the device_code is server-side only (Lark would honor a poll from
// anywhere if the device_code leaked, so we never echo it).
func (s *RegistrationService) BeginInstall(ctx context.Context, p BeginInstallParams) (BeginInstallResult, error) {
	if !p.WorkspaceID.Valid || !p.AgentID.Valid || !p.InitiatorID.Valid {
		return BeginInstallResult{}, errors.New("lark registration: workspace, agent, and initiator are required")
	}
	// Agent↔workspace pre-check — without this, a caller could open an
	// install session against another workspace's agent by guessing the
	// UUID, and the device_code minted against Lark would still produce
	// credentials. The handler already loads this agent to run
	// canManageAgent; re-checking here keeps the service self-defending.
	//
	// We keep the agent: its name pre-fills the bot name on Lark's
	// PersonalAgent creation form (see botNamePreset) so the installed
	// bot reads "<agent> - Multica" instead of "{用户姓名}的智能助手".
	agent, err := s.authQueries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          p.AgentID,
		WorkspaceID: p.WorkspaceID,
	})
	if err != nil {
		return BeginInstallResult{}, fmt.Errorf("lark registration: agent not in workspace: %w", err)
	}

	// Normalize the requested region: empty / unknown → Feishu, the same
	// back-compat invariant the storage layer uses (RegionOrDefault).
	// This both protects the device-flow client from a bogus value
	// from the handler AND means a pre-region caller (omitting the
	// field) keeps getting the historical mainland-first behaviour.
	region := RegionOrDefault(string(p.Region))

	begin, err := s.client.Begin(ctx, botNamePreset(agent.Name), region)
	if err != nil {
		return BeginInstallResult{}, fmt.Errorf("lark registration: begin: %w", err)
	}

	now := s.cfg.Now()
	sessionID, err := randomSessionID()
	if err != nil {
		return BeginInstallResult{}, fmt.Errorf("lark registration: mint session id: %w", err)
	}
	expiresAt := now.Add(begin.ExpiresIn)

	// Register the session BEFORE starting the goroutine. If this fails the
	// whole begin fails: a session the browser could never read is worse
	// than no QR at all — it would render a scannable code and then report
	// "session lost" on the first poll, which is exactly the bug this
	// store exists to prevent.
	//
	// Retention covers the QR window PLUS the terminal-read window, so a
	// session that ends at the last second can still be read afterwards.
	// Sizing it off Lark's expires_in (currently 1h) rather than a fixed
	// constant is what keeps the two from drifting apart.
	if err := s.sessionStore.Create(ctx, InstallSessionState{
		ID:          sessionID,
		WorkspaceID: p.WorkspaceID,
		InitiatorID: p.InitiatorID,
		Status:      RegistrationStatusPending,
		ExpiresAt:   expiresAt,
	}, begin.ExpiresIn+s.cfg.SessionTTL); err != nil {
		return BeginInstallResult{}, fmt.Errorf("lark registration: register session: %w", err)
	}

	sess := &registrationSession{
		id:          sessionID,
		workspaceID: p.WorkspaceID,
		agentID:     p.AgentID,
		initiatorID: p.InitiatorID,
		deviceCode:  begin.DeviceCode,
		domain:      begin.Domain,
		qrCodeURL:   begin.QRCodeURL,
		interval:    begin.Interval,
		expiresAt:   expiresAt,
		region:      region,
	}
	s.mu.Lock()
	s.sessions[sessionID] = sess
	s.mu.Unlock()

	// The polling goroutine outlives the request context, so we cannot
	// reuse ctx here. We size its own context to the device_code
	// expiry — the worst case is a session that quietly times out
	// after Lark's window closes, which we surface to the user as
	// RegistrationReasonExpired on the next status read.
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
// initiated by one workspace cannot be polled by another (the session
// id is unguessable but defense-in-depth costs nothing here).
//
// ErrRegistrationSessionNotFound is returned for unknown / expired /
// GC'd sessions; the frontend treats it the same as an error reason
// of "session_lost" — prompt the user to restart the install.
func (s *RegistrationService) GetSession(ctx context.Context, workspaceID pgtype.UUID, sessionID string) (RegistrationSessionState, error) {
	if strings.TrimSpace(sessionID) == "" {
		return RegistrationSessionState{}, ErrRegistrationSessionNotFound
	}
	// Read the shared store, never the in-process map: the goroutine that
	// owns this session may be running in a different backend process.
	// The read is workspace-scoped, so a session id from another workspace
	// is indistinguishable from one that never existed — leaking "exists
	// but wrong workspace" would let a caller enumerate session ids across
	// workspaces.
	stored, err := s.sessionStore.Get(ctx, workspaceID, sessionID)
	if err != nil {
		if errors.Is(err, ErrInstallSessionNotFound) {
			return RegistrationSessionState{}, ErrRegistrationSessionNotFound
		}
		return RegistrationSessionState{}, fmt.Errorf("lark registration: load session: %w", err)
	}

	state := RegistrationSessionState{
		ID:             stored.ID,
		Status:         stored.Status,
		InstallationID: stored.InstallationID,
		ErrorReason:    stored.ErrorReason,
		ErrorMessage:   stored.ErrorMessage,
		InitiatorID:    stored.InitiatorID,
	}

	// Still pending past Lark's window means the process owning the polling
	// goroutine died before it could record the expiry — no other replica
	// can finish that scan. Report the expiry from the timestamp rather
	// than leaving the dialog polling a session that will never move.
	if state.Status == RegistrationStatusPending &&
		!stored.ExpiresAt.IsZero() && !stored.ExpiresAt.After(s.cfg.Now()) {
		state.Status = RegistrationStatusError
		state.ErrorReason = RegistrationReasonExpired
		state.ErrorMessage = "QR expired before authorization"
	}
	return state, nil
}

// runPolling is the background loop. The pattern matches the upstream
// SDK: wait → poll → branch on result. On any terminal outcome we
// either record the installation+binding (success) or mark the session
// errored.
func (s *RegistrationService) runPolling(sess *registrationSession) {
	// Bound the entire polling life by Lark's expiry — once that
	// window closes, no further poll can succeed.
	ctx, cancel := context.WithDeadline(context.Background(), sess.expiresAt)
	defer cancel()

	interval := sess.interval
	if interval <= 0 {
		interval = time.Duration(registrationDefaultPollSeconds) * time.Second
	}
	domain := sess.domain
	deviceCode := sess.deviceCode
	// region tracks which cloud this install belongs to. It starts at
	// whatever the user picked at begin-time (Feishu by default; the
	// frontend now exposes an explicit Lark CTA that begins on
	// accounts.larksuite.com directly). The SwitchedDomain branch
	// below is still honored as a safety net — if a user clicks the
	// Feishu CTA but actually authorizes with a Lark-international
	// account, the poll stream surfaces tenant_brand="lark" and we
	// flip the local accordingly. So at finishSuccess time `region`
	// is the authoritative per-install cloud, derived first from the
	// user's UI choice and then from the protocol's role-based switch
	// — never by string-matching accounts hostnames (so staging/mock
	// domains classify correctly too).
	region := sess.region
	if region == "" {
		region = RegionFeishu
	}

	for {
		select {
		case <-ctx.Done():
			s.cfg.Logger.Info("lark registration: session expired",
				"session_id", sess.id,
				"workspace_id", uuidString(sess.workspaceID))
			s.markError(sess, RegistrationReasonExpired, "QR expired before authorization")
			return
		case <-time.After(interval):
		}

		res, err := s.client.Poll(ctx, domain, deviceCode)
		if err != nil {
			var re *RegistrationError
			if errors.As(err, &re) {
				s.cfg.Logger.Warn("lark registration: protocol error",
					"session_id", sess.id, "code", re.Code, "desc", re.Description)
				s.markError(sess, RegistrationReasonProtocol, re.Error())
				return
			}
			// Transient transport error (DNS, network) — log and try
			// again on the next tick rather than killing the session,
			// which lets a 30-second cross-region blip self-heal.
			s.cfg.Logger.Warn("lark registration: transport error, will retry",
				"session_id", sess.id, "err", err)
			continue
		}

		switch {
		case res.SwitchedDomain != "":
			// Tenant-brand switch — re-aim immediately without
			// honoring the interval, matching the upstream SDK's
			// behavior. Lark emits the brand hint exactly once on the
			// transition poll and the credential-bearing response
			// lands on the next call to the new domain.
			//
			// Both directions are honored (feishu→lark and lark→feishu)
			// so the split-CTA UI's "wrong entry" path recovers
			// regardless of which CTA the user picked. The new region
			// rides on the same PollResult so we never have to
			// re-derive it from the host string here — staging / mock
			// accounts hosts then classify correctly without
			// hostname-prefix matching.
			domain = res.SwitchedDomain
			region = res.SwitchedRegion
			s.cfg.Logger.Info("lark registration: switched cloud after tenant-brand mismatch",
				"session_id", sess.id, "domain", domain, "region", string(region))
			continue
		case res.ClientID != "" && res.ClientSecret != "":
			s.finishSuccess(ctx, sess, res, region)
			return
		case res.Err != nil:
			reason := RegistrationReasonProtocol
			if res.Err.Code == "access_denied" {
				reason = RegistrationReasonAccessDenied
			} else if res.Err.Code == "expired_token" {
				reason = RegistrationReasonExpired
			}
			s.cfg.Logger.Info("lark registration: terminal error",
				"session_id", sess.id, "code", res.Err.Code, "desc", res.Err.Description)
			s.markError(sess, reason, res.Err.Error())
			return
		case res.Status == "slow_down":
			// Honor Lark's back-off — bump by 5s, per RFC 8628 §3.5.
			interval += 5 * time.Second
		default:
			// authorization_pending — keep the interval, loop.
		}
	}
}

// finishSuccess runs the post-poll finalization: bot info lookup +
// installation insert + installer binding, all in a single DB
// transaction.
func (s *RegistrationService) finishSuccess(ctx context.Context, sess *registrationSession, res *PollResult, region Region) {
	// Carry the detected region onto the credentials so the GetBotInfo
	// call below hits the right open-platform host: a Lark-international
	// install must reach open.larksuite.com, not the Feishu default.
	creds := InstallationCredentials{AppID: res.ClientID, AppSecret: res.ClientSecret, Region: region}
	// Re-registering an existing Bot issues a fresh client_secret under
	// the SAME client_id, and Lark revokes every tenant_access_token
	// minted from the previous one. The API client caches tokens by
	// app_id — which did not change — so tell it to drop the entry
	// before we mint with the new credentials; otherwise the first call
	// below, and every outbound after it, replays a token Lark now
	// rejects (#7611).
	if invalidator, ok := s.api.(TokenCacheInvalidator); ok {
		invalidator.InvalidateTokenCache(res.ClientID)
	}
	info, err := s.api.GetBotInfo(ctx, creds)
	if err != nil {
		s.cfg.Logger.Warn("lark registration: bot info failed",
			"session_id", sess.id, "err", err)
		s.markError(sess, RegistrationReasonBotInfoFailed, err.Error())
		return
	}
	if info.OpenID == "" {
		s.markError(sess, RegistrationReasonBotInfoFailed, "bot info missing open_id")
		return
	}

	// Encrypt the app_secret before the transaction so the seal cost
	// doesn't sit inside the DB lock. The InstallationService's Upsert
	// would do this for us, but we need the encrypted blob inside the
	// transaction-scoped queries handle so the installer-bind commits
	// alongside the installation insert — replicate the Seal here.
	sealed, err := s.installs.box.Seal([]byte(res.ClientSecret))
	if err != nil {
		s.cfg.Logger.Error("lark registration: seal app_secret",
			"session_id", sess.id, "err", err)
		s.markError(sess, RegistrationReasonInternalError, err.Error())
		return
	}

	tx, err := s.tx.Begin(ctx)
	if err != nil {
		s.cfg.Logger.Error("lark registration: begin tx",
			"session_id", sess.id, "err", err)
		s.markError(sess, RegistrationReasonInternalError, err.Error())
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.queries.WithTx(tx)

	// If the same Feishu app (app_id) is held by a DEAD prior owner — a revoked
	// placeholder left by a DIFFERENT agent in this workspace, or an orphan whose
	// workspace/agent was deleted (#4810) — that row still occupies the
	// (channel_type, config->>'app_id') unique slot and blocks the
	// UpsertChannelInstallation INSERT below. Reclaim it first — the transaction
	// wraps both the delete and the upsert so a failure between them rolls back
	// cleanly. A live owner is left in place (the SAME agent's own revoked row is
	// reactivated in place by the upsert; an active/archived agent stays owned),
	// so the upsert surfaces the conflict below instead of stealing the bot.
	if err := qtx.ReclaimDeadInstallationByAppID(ctx, sess.workspaceID, sess.agentID, res.ClientID); err != nil {
		s.cfg.Logger.Warn("lark registration: reclaim dead installation",
			"session_id", sess.id, "err", err)
		s.markError(sess, RegistrationReasonInternalError, err.Error())
		return
	}

	inst, err := qtx.UpsertLarkInstallation(ctx, UpsertInstallationParams{
		WorkspaceID:        sess.workspaceID,
		AgentID:            sess.agentID,
		AppID:              res.ClientID,
		AppSecretEncrypted: sealed,
		BotOpenID:          string(info.OpenID),
		BotUnionID:         textOrNull(info.UnionID),
		InstallerUserID:    sess.initiatorID,
		Region:             string(region),
	})
	if err != nil {
		s.cfg.Logger.Warn("lark registration: upsert installation",
			"session_id", sess.id, "err", err)
		// A unique violation here means the app_id slot is held by a LIVE owner
		// (the reclaim above already cleared every dead one). Surface who holds
		// it — another agent in this workspace, an archived agent, or a different
		// workspace — instead of leaking the raw Postgres error.
		msg := err.Error()
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			msg = s.liveOwnerConflictMessage(ctx, sess.workspaceID, res.ClientID)
		}
		s.markError(sess, RegistrationReasonInstallationConflict, msg)
		return
	}

	if err := s.binder.BindInstallerTx(ctx, qtx, InstallerBindParams{
		WorkspaceID:    sess.workspaceID,
		InstallationID: inst.ID,
		MulticaUserID:  sess.initiatorID,
		LarkOpenID:     res.OpenID,
	}); err != nil {
		s.cfg.Logger.Warn("lark registration: bind installer",
			"session_id", sess.id, "err", err)
		s.markError(sess, RegistrationReasonInstallerBindFailed, err.Error())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		s.cfg.Logger.Error("lark registration: commit",
			"session_id", sess.id, "err", err)
		s.markError(sess, RegistrationReasonInternalError, err.Error())
		return
	}
	s.markSuccess(sess, inst.ID)
	// Publish at the commit point so the connection badge updates on every
	// workspace client without a page refresh — not only on the tab that
	// happens to poll the status endpoint to success.
	s.publishInstalled(sess.workspaceID, inst.ID)
	s.cfg.Logger.Info("lark registration: install complete",
		"session_id", sess.id,
		"workspace_id", uuidString(sess.workspaceID),
		"agent_id", uuidString(sess.agentID),
		"installation_id", uuidString(inst.ID))
}

// liveOwnerConflictMessage builds the user-facing copy for a rebind refused
// because the Feishu app's routing slot is held by a LIVE owner. It names which
// kind of owner so the user knows how to recover, instead of the old catch-all
// "connected to a different Multica workspace" that lied when the real owner sat
// in the SAME workspace (#4810). Looked up on the base pool, not the aborted
// upsert tx. If the slot turns out free (a concurrent disconnect between the
// upsert and this read), a generic message is enough — the user can just retry.
func (s *RegistrationService) liveOwnerConflictMessage(ctx context.Context, requestingWorkspaceID pgtype.UUID, appID string) string {
	owner, err := s.queries.InstallationOwnerByAppID(ctx, appID)
	if err != nil {
		return "This Feishu app is already connected to another agent. Disconnect it there first, then connect it here."
	}
	switch {
	case owner.WorkspaceID != requestingWorkspaceID:
		return "This Feishu app is already connected to a different Multica workspace. Disconnect it there before connecting it here."
	case owner.AgentArchivedAt.Valid:
		return "This Feishu app is connected to an archived agent in this workspace. Restore that agent, or disconnect its bot, before connecting it here."
	default:
		return "This Feishu app is already connected to another agent in this workspace. Disconnect it there first, then connect it here."
	}
}

func (s *RegistrationService) gcDeadline() time.Time {
	return s.cfg.Now().Add(s.cfg.SessionTTL)
}

// ErrRegistrationSessionNotFound is what the service returns for
// unknown / GC'd sessions. The handler maps it to 404.
var ErrRegistrationSessionNotFound = errors.New("lark registration: session not found")

func randomSessionID() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// botNamePreset builds the display name we pre-fill on Lark's
// PersonalAgent creation form so the installed bot reads
// "<agent> - Multica" instead of Lark's auto-generated
// "{用户姓名}的智能助手". Lark treats this as a default the installer can
// still edit; we never get to lock the final name. A blank agent name
// (defensive — Agent.Name is NOT NULL in schema) degrades to plain
// "Multica" rather than a dangling " - Multica".
func botNamePreset(agentName string) string {
	name := strings.TrimSpace(agentName)
	if name == "" {
		return "Multica"
	}
	return name + " - Multica"
}

// uuidString is the package-local UUID-to-string helper defined in
// hub.go; redeclared `func uuidString(u pgtype.UUID) string` removed
// to avoid the symbol collision.
//
// InstallationService.box is unexported but reachable from this file
// because both live in package `lark`; we read it directly in
// finishSuccess so the Seal happens outside the DB transaction (which
// would otherwise hold a row lock across the crypto call).

// SetInstallSessionStore swaps the in-flight session store. The router
// calls this with the Redis implementation when the deployment has Redis;
// without it the service keeps its in-process default, which is correct
// for local development and a single replica but reproduces MUL-7340 on a
// multi-replica deployment.
func (s *RegistrationService) SetInstallSessionStore(store InstallSessionStore) {
	if store == nil {
		return
	}
	s.sessionStore = store
}
