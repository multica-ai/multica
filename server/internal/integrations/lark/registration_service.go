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

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

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
	// SessionTTL caps how long a successful or errored session stays in
	// the in-process cache before GC. Default 30 minutes — long enough
	// for the frontend to fetch the final status after the dialog
	// closes, short enough that abandoned sessions do not pin memory
	// forever. Independent of the device-flow expiry (Lark's
	// expire_in, ~10 min).
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
// In-process session storage is intentional: device-flow sessions are
// short-lived (<10 min), the QR has no value outside the same browser
// session that initiated it, and persisting half-completed installs
// into Postgres would add a migration + GC sweep without delivering any
// product capability the user can re-use across server restarts.
type RegistrationService struct {
	cfg         RegistrationServiceConfig
	client      *RegistrationClient
	api         APIClient
	queries     *ChannelStore
	tx          TxStarter
	installs    *InstallationService
	binder      InstallerBinder
	authQueries authQueriesAdapter

	// bus is optional. When wired (SetEventBus), a successful install
	// publishes lark_installation:created the moment the row commits, so
	// every workspace client refreshes its connection badge without
	// waiting for a browser to poll the status endpoint to success. Nil
	// is valid — install still works, it just won't push the WS frame.
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
		sessions:    make(map[string]*registrationSession),
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

// registrationSession is the in-memory state for one in-flight install.
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
	// session (e.g. expiry fired between status reads), don't clobber
	// the first reason — the user already saw it.
	if s.status != RegistrationStatusPending {
		return
	}
	s.status = RegistrationStatusError
	s.errorReason = reason
	s.errorMessage = msg
	s.gcAfter = gcAfter
}

// RegistrationSessionState is the read-only snapshot the handler
// serializes to the frontend. Internal mutex is hidden by construction.
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
	// Agent ownership pre-check — without this, a workspace admin
	// could open an install session against another workspace's agent
	// by guessing the UUID, and the device_code minted against Lark
	// would still produce credentials. The handler does the same
	// check; doing it here too keeps the service self-defending.
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
	sess := &registrationSession{
		id:          sessionID,
		workspaceID: p.WorkspaceID,
		agentID:     p.AgentID,
		initiatorID: p.InitiatorID,
		deviceCode:  begin.DeviceCode,
		domain:      begin.Domain,
		qrCodeURL:   begin.QRCodeURL,
		interval:    begin.Interval,
		expiresAt:   now.Add(begin.ExpiresIn),
		region:      region,
		status:      RegistrationStatusPending,
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
func (s *RegistrationService) GetSession(workspaceID pgtype.UUID, sessionID string) (RegistrationSessionState, error) {
	if strings.TrimSpace(sessionID) == "" {
		return RegistrationSessionState{}, ErrRegistrationSessionNotFound
	}
	s.gcExpiredLocked()
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
			sess.markError(RegistrationReasonExpired, "QR expired before authorization", s.gcDeadline())
			return
		case <-time.After(interval):
		}

		res, err := s.client.Poll(ctx, domain, deviceCode)
		if err != nil {
			var re *RegistrationError
			if errors.As(err, &re) {
				s.cfg.Logger.Warn("lark registration: protocol error",
					"session_id", sess.id, "code", re.Code, "desc", re.Description)
				sess.markError(RegistrationReasonProtocol, re.Error(), s.gcDeadline())
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
			sess.markError(reason, res.Err.Error(), s.gcDeadline())
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
	info, err := s.api.GetBotInfo(ctx, creds)
	if err != nil {
		s.cfg.Logger.Warn("lark registration: bot info failed",
			"session_id", sess.id, "err", err)
		sess.markError(RegistrationReasonBotInfoFailed, err.Error(), s.gcDeadline())
		return
	}
	if info.OpenID == "" {
		sess.markError(RegistrationReasonBotInfoFailed, "bot info missing open_id", s.gcDeadline())
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
		sess.markError(RegistrationReasonInternalError, err.Error(), s.gcDeadline())
		return
	}

	tx, err := s.tx.Begin(ctx)
	if err != nil {
		s.cfg.Logger.Error("lark registration: begin tx",
			"session_id", sess.id, "err", err)
		sess.markError(RegistrationReasonInternalError, err.Error(), s.gcDeadline())
		return
	}
	defer tx.Rollback(ctx)
	qtx := s.queries.WithTx(tx)

	// If the same Feishu app (app_id) was previously bound to a DIFFERENT
	// agent in this workspace and later revoked, that revoked row still
	// holds the (channel_type, config->>'app_id') unique index slot and
	// blocks the UpsertChannelInstallation INSERT below. Remove the
	// revoked placeholder first — the transaction wraps both the delete
	// and the upsert so a failure between them rolls back cleanly. The
	// current agent (sess.agentID) is excluded: re-connecting the SAME
	// agent reactivates its own revoked row in place via the upsert's
	// ON CONFLICT, keeping its installation_id and every binding intact.
	if err := qtx.RemoveRevokedInstallationByAppID(ctx, sess.workspaceID, sess.agentID, res.ClientID); err != nil {
		s.cfg.Logger.Warn("lark registration: cleanup revoked installation",
			"session_id", sess.id, "err", err)
		sess.markError(RegistrationReasonInternalError, err.Error(), s.gcDeadline())
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
		sess.markError(RegistrationReasonInstallationConflict, err.Error(), s.gcDeadline())
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
		sess.markError(RegistrationReasonInstallerBindFailed, err.Error(), s.gcDeadline())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		s.cfg.Logger.Error("lark registration: commit",
			"session_id", sess.id, "err", err)
		sess.markError(RegistrationReasonInternalError, err.Error(), s.gcDeadline())
		return
	}
	sess.markSuccess(inst.ID, s.gcDeadline())
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

func (s *RegistrationService) gcDeadline() time.Time {
	return s.cfg.Now().Add(s.cfg.SessionTTL)
}

// gcExpiredLocked drops any session whose `gcAfter` is in the past.
// Pending sessions are NOT GC'd here — runPolling will set their
// gcAfter when it terminates, and an expired-by-deadline session
// closes itself.
func (s *RegistrationService) gcExpiredLocked() {
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
var ErrRegistrationSessionNotFound = errors.New("lark registration: session not found")

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
