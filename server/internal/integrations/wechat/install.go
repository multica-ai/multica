package wechat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the WeChat install backend. Unlike Slack (BYO paste) and like
// Feishu (device-flow scan), WeChat uses a QR-login: the installer calls begin,
// gets a QR code URL the user scans with their personal WeChat, and polls status
// until the iLink backend confirms and returns the bot_token + base_url + bot id.
// The RegistrationService owns: the in-process session state (QR URL + the
// polled credentials), the at-rest encryption of bot_token (no plaintext
// storage, even in dev), and the shared persistInstall transaction + list / get
// / revoke management surface.

var (
	// ErrInstallationNotFound surfaces "no row matches in this workspace".
	ErrInstallationNotFound = errors.New("wechat installation not found")
	// ErrBotOwnedByAnotherWorkspace is returned when the scanned WeChat bot is
	// already connected to a live owner in a DIFFERENT Multica workspace — it
	// would collide with the (channel_type, app_id) routing index.
	ErrBotOwnedByAnotherWorkspace = errors.New("wechat: this WeChat bot is already connected to a different Multica workspace")
	// ErrBotOwnedBySameWorkspace is returned when the bot is already connected to
	// a DIFFERENT (live, non-archived) agent in the SAME workspace.
	ErrBotOwnedBySameWorkspace = errors.New("wechat: this WeChat bot is already connected to another agent in this workspace")
	// ErrBotOwnedByArchivedAgent is returned when the bot's owning agent is
	// archived (archiving is reversible, so it still holds the bot).
	ErrBotOwnedByArchivedAgent = errors.New("wechat: this WeChat bot is connected to an archived agent in this workspace")
)

// RegistrationSessionStatus is the discriminated state a begin session lives in.
type RegistrationSessionStatus string

const (
	RegistrationStatusPending RegistrationSessionStatus = "pending"
	RegistrationStatusSuccess RegistrationSessionStatus = "success"
	RegistrationStatusError   RegistrationSessionStatus = "error"
)

// Reason codes the service stores on a failed session. Stable strings so the
// frontend can switch on them without parsing prose.
const (
	RegistrationReasonExpired              = "expired"
	RegistrationReasonAccessDenied         = "access_denied"
	RegistrationReasonProtocol             = "ilink_protocol_error"
	RegistrationReasonInstallationConflict = "installation_conflict"
	RegistrationReasonInstallerBindFailed  = "installer_bind_failed"
	RegistrationReasonSessionLost          = "session_lost"
	RegistrationReasonInternalError        = "internal_error"
)

// installQueries is the slice of generated queries the service needs. WithTx
// returns the same interface bound to a transaction so persistInstall runs its
// upsert atomically (and so tests can inject a fake without a real DB).
type installQueries interface {
	WithTx(tx pgx.Tx) installQueries
	UpsertChannelInstallation(ctx context.Context, arg db.UpsertChannelInstallationParams) (db.ChannelInstallation, error)
	ReclaimDeadChannelInstallationByAppID(ctx context.Context, arg db.ReclaimDeadChannelInstallationByAppIDParams) (pgtype.UUID, error)
	GetChannelInstallationOwnerByAppID(ctx context.Context, arg db.GetChannelInstallationOwnerByAppIDParams) (db.GetChannelInstallationOwnerByAppIDRow, error)
	ListChannelInstallationsByWorkspace(ctx context.Context, arg db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error)
	GetChannelInstallationInWorkspace(ctx context.Context, arg db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error)
	SetChannelInstallationStatus(ctx context.Context, arg db.SetChannelInstallationStatusParams) error
	// CreateChannelUserBinding auto-binds the scanner's WeChat id to the
	// installer's Multica account at install time ("scanner = owner"), so the
	// owner's own messages skip the binding-prompt flow. Idempotent via ON
	// CONFLICT (re-install / re-scan just no-ops).
	CreateChannelUserBinding(ctx context.Context, arg db.CreateChannelUserBindingParams) (db.ChannelUserBinding, error)
}

// dbInstallQueries adapts *db.Queries to installQueries (the generated WithTx
// returns *db.Queries, so we wrap it to return the interface — the same adapter
// pattern engine.ChatSession and slack.InstallService use).
type dbInstallQueries struct{ *db.Queries }

func (q dbInstallQueries) WithTx(tx pgx.Tx) installQueries {
	return dbInstallQueries{q.Queries.WithTx(tx)}
}

// pgUniqueViolation is the Postgres SQLSTATE for a unique-constraint violation.
const pgUniqueViolation = "23505"

// RegistrationService owns the QR-login session state + the at-rest encryption
// of the bot_token (so no caller can write a channel_installation with a
// plaintext token) + the shared install transaction. The box MUST be non-nil.
type RegistrationService struct {
	box    *secretbox.Box
	q      installQueries
	tx     engine.TxStarter
	client *iLinkClient
	logger *slog.Logger

	// SessionTTL caps how long a (terminal) session stays in the cache before GC.
	sessionTTL time.Duration
	now        func() time.Time

	mu       sync.Mutex
	sessions map[string]*registrationSession
}

type registrationSession struct {
	// qrcode is the iLink QR token: it is both the value rendered as a QR image
	// and the correlation key pollQRStatus re-queries by (iLink has no separate
	// session token). It is also the session map key.
	qrcode      string
	workspaceID pgtype.UUID
	agentID     pgtype.UUID
	installerID pgtype.UUID
	createdAt   time.Time
	// Polled result (set when status reaches success/error).
	status        RegistrationSessionStatus
	installation  db.ChannelInstallation
	login         QRLoginResponse
	errorReason   string
	errorMessage  string
	completedAt   time.Time
}

// BeginInstallResponse is the result of starting a QR-login session.
type BeginInstallResponse struct {
	SessionID           string
	QRCodeURL           string
	ExpiresInSeconds    int
	PollIntervalSeconds int
}

// NewRegistrationService binds the service to queries, a tx starter
// (*pgxpool.Pool), and an encryption box. baseURL seeds the iLink client for the
// QR-login flow (overridable via MULTICA_WECHAT_BASE_URL for proxy/mock/staging).
func NewRegistrationService(q *db.Queries, tx engine.TxStarter, box *secretbox.Box, baseURL string, logger *slog.Logger) (*RegistrationService, error) {
	if box == nil {
		return nil, errors.New("wechat: RegistrationService requires a non-nil secretbox.Box")
	}
	if q == nil {
		return nil, errors.New("wechat: RegistrationService requires queries")
	}
	if tx == nil {
		return nil, errors.New("wechat: RegistrationService requires a tx starter")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RegistrationService{
		box:        box,
		q:          dbInstallQueries{q},
		tx:         tx,
		client:     newILinkClient(baseURL, logger),
		logger:     logger,
		sessionTTL: 30 * time.Minute,
		now:        time.Now,
		sessions:   make(map[string]*registrationSession),
	}, nil
}

// BeginInstallParams carries the identity of the install the caller wants to
// create / replace.
type BeginInstallParams struct {
	WorkspaceID pgtype.UUID
	AgentID     pgtype.UUID
	InitiatorID pgtype.UUID
}

// BeginInstall starts a QR-login session: fetches the QR code from iLink and
// stashes the qrcode token. The frontend renders the QR and polls
// GetSessionStatus at the returned cadence. The iLink backend resolves the scan
// status lazily (on each GetSessionStatus call drives one pollQRStatus), so
// this method returns immediately after minting the QR.
func (s *RegistrationService) BeginInstall(ctx context.Context, p BeginInstallParams) (BeginInstallResponse, error) {
	qrToken, qrImage, err := s.client.getQRCode(ctx)
	if err != nil {
		return BeginInstallResponse{}, fmt.Errorf("wechat: get qrcode: %w", err)
	}
	sess := &registrationSession{
		qrcode:      qrToken,
		workspaceID: p.WorkspaceID,
		agentID:     p.AgentID,
		installerID: p.InitiatorID,
		createdAt:   s.now(),
		status:      RegistrationStatusPending,
	}
	s.mu.Lock()
	s.sessions[qrToken] = sess
	s.gcLocked()
	s.mu.Unlock()
	// Drive the iLink status long-poll in the background: get_qrcode_status is a
	// BLOCKING call (holds until the user scans+confirms), so the HTTP status
	// endpoint must NOT call pollOnce synchronously (it would stack 45s calls on
	// every 2s frontend poll). This goroutine blocks on the long-poll, advances
	// the session on confirm, and re-polls on timeout until terminal. It exits
	// once the session reaches a terminal state or the service is done.
	go s.runPollLoop(sess)
	return BeginInstallResponse{
		// The qrcode token doubles as the session id the frontend polls with.
		SessionID:           qrToken,
		QRCodeURL:           qrImage, // the scannable content URL (liteapp.weixin.qq.com/...), NOT the token
		ExpiresInSeconds:    300,     // iLink QR codes are valid ~5 min
		PollIntervalSeconds: 2,
	}, nil
}

// runPollLoop drives the iLink status long-poll for one session in the
// background. get_qrcode_status blocks until scan/confirm (or its own timeout),
// so this loop issues one blocking call at a time: on confirm it persists the
// installation; on timeout it re-polls; on a terminal session it exits. Uses a
// fresh background context (not tied to the begin HTTP request, which has long
// returned).
func (s *RegistrationService) runPollLoop(sess *registrationSession) {
	for {
		// Stop if the session already reached a terminal state (another loop,
		// GC, or a race advanced it).
		s.mu.Lock()
		st := sess.status
		s.mu.Unlock()
		if st != RegistrationStatusPending {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), longPollTimeout+10*time.Second)
		status, login, err := s.client.pollQRStatus(ctx, sess.qrcode)
		cancel()
		if err != nil {
			// Real error (not a timeout): log and keep the session pending so the
			// next loop iteration retries. Avoid a tight spin on persistent errors
			// by sleeping briefly.
			s.logger.Warn("wechat install: poll status error", "error", err)
			time.Sleep(3 * time.Second)
			continue
		}
		switch status {
		case "confirmed":
			s.completeInstall(context.Background(), sess, login)
			return
		case "expired":
			s.failSession(sess, RegistrationReasonExpired, "QR code expired")
			return
		case "denied":
			s.failSession(sess, RegistrationReasonAccessDenied, "user denied or cancelled")
			return
		case "pending":
			// long-poll timed out without a scan; loop and block again.
			continue
		default:
			s.failSession(sess, RegistrationReasonProtocol, fmt.Sprintf("unexpected status %q", status))
			return
		}
	}
}

// SessionState is the snapshot the HTTP status endpoint serializes.
type SessionState struct {
	Status         RegistrationSessionStatus
	InstallationID pgtype.UUID
	ErrorReason    string
	ErrorMessage   string
}

// GetSession returns the current state of a QR-login session. Returns
// (zero, false) when the session is unknown / expired / GC'd — the caller maps
// that to a terminal "session_lost" error so the frontend stops polling.
func (s *RegistrationService) GetSession(workspaceID pgtype.UUID, sessionID string) (SessionState, bool) {
	s.mu.Lock()
	sess, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return SessionState{}, false
	}
	// Workspace isolation: a session from another workspace is treated as
	// unknown (no existence leak).
	if sess.workspaceID != workspaceID {
		return SessionState{}, false
	}
	// NOTE: no synchronous poll here. The iLink get_qrcode_status endpoint is a
	// BLOCKING long-poll; calling it per HTTP status request would stack 45s
	// calls on every 2s frontend poll. BeginInstall launched a background
	// runPollLoop that drives the long-poll and advances this session; this
	// method just reads the in-memory state that loop updates.
	return SessionState{
		Status:         sess.status,
		InstallationID: sess.installation.ID,
		ErrorReason:    sess.errorReason,
		ErrorMessage:   sess.errorMessage,
	}, true
}

// pollOnce does one iLink qrcode/status poll and advances the session. On
// "confirmed" it persists the installation (encrypting the bot_token); on
// terminal failure it records the reason. Idempotent: a session already
// advanced is a no-op. Errors are swallowed into the session state (the frontend
// reads them off the next GetSession), never propagated — the poll runs detached
// from the HTTP request path.
func (s *RegistrationService) pollOnce(ctx context.Context, sess *registrationSession) {
	status, login, err := s.client.pollQRStatus(ctx, sess.qrcode)
	if err != nil {
		// A transient network error leaves the session pending (frontend retries);
		// do not flip to error on a single transient failure.
		s.logger.WarnContext(ctx, "wechat install: poll status transient error", "error", err)
		return
	}
	switch status {
	case "pending":
		return // keep polling (covers iLink "init" and "scan" states)
	case "confirmed":
		s.completeInstall(ctx, sess, login)
	case "expired":
		s.failSession(sess, RegistrationReasonExpired, "QR code expired")
	case "denied":
		s.failSession(sess, RegistrationReasonAccessDenied, "user denied or cancelled")
	default:
		// Unknown status — treat as a protocol error rather than silently hanging.
		s.failSession(sess, RegistrationReasonProtocol, fmt.Sprintf("unexpected status %q", status))
	}
}

// completeInstall encrypts the credentials and persists the installation, then
// flips the session to success. On any failure the session is marked error with
// a stable reason.
func (s *RegistrationService) completeInstall(ctx context.Context, sess *registrationSession, login QRLoginResponse) {
	if login.BotToken == "" || login.BaseURL == "" || login.IlinkBotID == "" {
		s.failSession(sess, RegistrationReasonProtocol, "confirmed response missing credentials")
		return
	}
	// Encrypt the bot_token at rest (base64-encoded ciphertext in config).
	encToken, err := s.box.Seal([]byte(login.BotToken))
	if err != nil {
		s.failSession(sess, RegistrationReasonInternalError, fmt.Sprintf("encrypt bot token: %v", err))
		return
	}
	cfg := installConfig{
		AppID:             login.IlinkBotID,
		BotTokenEncrypted: encodeBase64(encToken),
		BaseURL:           login.BaseURL,
		IlinkUserID:       login.IlinkUserID,
	}
	configJSON, err := jsonMarshalConfig(cfg)
	if err != nil {
		s.failSession(sess, RegistrationReasonInternalError, fmt.Sprintf("marshal config: %v", err))
		return
	}
	inst, err := s.persistInstall(ctx, installPersist{
		wsID:        sess.workspaceID,
		agentID:     sess.agentID,
		installerID: sess.installerID,
		appIDKey:    login.IlinkBotID,
		configJSON:  configJSON,
	})
	if err != nil {
		reason := RegistrationReasonInternalError
		switch {
		case errors.Is(err, ErrBotOwnedByAnotherWorkspace), errors.Is(err, ErrBotOwnedBySameWorkspace), errors.Is(err, ErrBotOwnedByArchivedAgent):
			reason = RegistrationReasonInstallationConflict
		}
		s.failSession(sess, reason, err.Error())
		return
	}
	// "Scanner = owner": auto-bind the scanning WeChat account to the installer's
	// Multica account so the owner's own messages skip the binding-prompt flow
	// (no need to click a bind link on first message). The iLink confirm response
	// carries ilink_user_id = the WeChat id that scanned. Idempotent via ON
	// CONFLICT; a failure here is logged but does not undo the install (the user
	// can still bind via the normal prompt as a fallback).
	if login.IlinkUserID != "" && sess.installerID.Valid {
		if _, berr := s.q.CreateChannelUserBinding(ctx, db.CreateChannelUserBindingParams{
			WorkspaceID:    sess.workspaceID,
			MulticaUserID:  sess.installerID,
			InstallationID: inst.ID,
			ChannelType:    string(TypeWechat),
			ChannelUserID:  login.IlinkUserID,
			Config:         []byte(`{}`),
		}); berr != nil {
			s.logger.WarnContext(ctx, "wechat install: auto-bind scanner failed; user will see binding prompt",
				"installation_id", inst.ID, "wechat_user_id", login.IlinkUserID, "error", berr)
		}
	}
	s.mu.Lock()
	sess.status = RegistrationStatusSuccess
	sess.installation = inst
	sess.login = login
	sess.completedAt = s.now()
	s.mu.Unlock()
}

func (s *RegistrationService) failSession(sess *registrationSession, reason, msg string) {
	s.mu.Lock()
	sess.status = RegistrationStatusError
	sess.errorReason = reason
	sess.errorMessage = msg
	sess.completedAt = s.now()
	s.mu.Unlock()
}

// installPersist carries the resolved fields persistInstall writes. appIDKey is
// the iLink bot id stored at config->>'app_id' — the routing key — and MUST
// equal the app_id inside configJSON.
type installPersist struct {
	wsID        pgtype.UUID
	agentID     pgtype.UUID
	installerID pgtype.UUID
	appIDKey    string
	configJSON  []byte
}

// persistInstall upserts the installation keyed by (workspace_id, agent_id,
// channel_type): ONE WeChat bot per agent. Mirrors slack.InstallService.persistInstall
// exactly: reclaim dead owners, upsert, classify the unique violation.
func (s *RegistrationService) persistInstall(ctx context.Context, p installPersist) (db.ChannelInstallation, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("begin install tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	if _, err := qtx.ReclaimDeadChannelInstallationByAppID(ctx, db.ReclaimDeadChannelInstallationByAppIDParams{
		ChannelType: string(TypeWechat),
		AppID:       p.appIDKey,
		WorkspaceID: p.wsID,
		AgentID:     p.agentID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelInstallation{}, fmt.Errorf("reclaim dead wechat installation: %w", err)
	}

	inst, err := qtx.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
		WorkspaceID:     p.wsID,
		AgentID:         p.agentID,
		ChannelType:     string(TypeWechat),
		Config:          p.configJSON,
		InstallerUserID: p.installerID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return db.ChannelInstallation{}, s.liveOwnerConflictErr(ctx, p.wsID, p.appIDKey)
		}
		return db.ChannelInstallation{}, fmt.Errorf("upsert wechat installation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("commit wechat install: %w", err)
	}
	return inst, nil
}

// liveOwnerConflictErr classifies who holds the (wechat, app_id) routing slot
// after the dead-owner reclaim ran. Mirrors slack.InstallService.
func (s *RegistrationService) liveOwnerConflictErr(ctx context.Context, requestingWorkspaceID pgtype.UUID, appID string) error {
	owner, err := s.q.GetChannelInstallationOwnerByAppID(ctx, db.GetChannelInstallationOwnerByAppIDParams{
		ChannelType: string(TypeWechat),
		AppID:       appID,
	})
	if err != nil {
		return ErrBotOwnedByAnotherWorkspace
	}
	switch {
	case owner.WorkspaceID != requestingWorkspaceID:
		return ErrBotOwnedByAnotherWorkspace
	case owner.AgentArchivedAt.Valid:
		return ErrBotOwnedByArchivedAgent
	default:
		return ErrBotOwnedBySameWorkspace
	}
}

// ListByWorkspace returns every WeChat installation in the workspace (active and
// revoked), for the management surface.
func (s *RegistrationService) ListByWorkspace(ctx context.Context, wsID pgtype.UUID) ([]db.ChannelInstallation, error) {
	return s.q.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: wsID,
		ChannelType: string(TypeWechat),
	})
}

// GetInWorkspace is the workspace-scoped lookup so a forged installation id from
// another workspace returns NotFound instead of leaking existence.
func (s *RegistrationService) GetInWorkspace(ctx context.Context, id, wsID pgtype.UUID) (db.ChannelInstallation, error) {
	inst, err := s.q.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          id,
		WorkspaceID: wsID,
		ChannelType: string(TypeWechat),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ChannelInstallation{}, ErrInstallationNotFound
		}
		return db.ChannelInstallation{}, err
	}
	return inst, nil
}

// Revoke flips status to 'revoked'. The row is preserved for audit; a re-install
// flips it back to 'active'. The Supervisor stops supervising the installation
// (ListActiveInstallations filters to active), so its long-poll connection winds
// down, and outbound drops too.
func (s *RegistrationService) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.q.SetChannelInstallationStatus(ctx, db.SetChannelInstallationStatusParams{
		ID:     id,
		Status: "revoked",
	})
}

// gcLocked evicts terminal sessions older than sessionTTL. Caller holds s.mu.
func (s *RegistrationService) gcLocked() {
	cutoff := s.now().Add(-s.sessionTTL)
	for id, sess := range s.sessions {
		if sess.status != RegistrationStatusPending && sess.completedAt.Before(cutoff) {
			delete(s.sessions, id)
		}
	}
}
