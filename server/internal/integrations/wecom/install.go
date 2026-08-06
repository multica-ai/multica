package wecom

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Session status wire values (spec §4). Kept as string constants because the
// DB CHECK constraint uses the same literals and the frontend switches on
// them directly.
const (
	InstallStatusCreating = "creating"
	InstallStatusPending  = "pending"
	InstallStatusSuccess  = "success"
	InstallStatusError    = "error"
)

// InstallErrorReason enumerates the stable error codes surfaced to the
// frontend (spec §7.3.1). error_message is user-safe copy only; diagnostic
// details go to logs.
const (
	InstallErrorExpired                 = "expired"
	InstallErrorGenerateFailed          = "generate_failed"
	InstallErrorIntegrationUnconfigured = "integration_unconfigured"
	InstallErrorInstallationConflict    = "installation_conflict"
	InstallErrorWecomProtocolError      = "wecom_protocol_error"
	InstallErrorInternalError           = "internal_error"
)

// Install flow tunables (spec §3.3, §4). Every value is a field on
// InstallServiceConfig so tests can shrink windows without redefining the
// service.
const (
	defaultQRTTL                = 5 * time.Minute
	defaultGenerateDeadline     = 30 * time.Second
	defaultPendingPollInterval  = 2 * time.Second
	defaultCreatingPollInterval = 1 * time.Second
	defaultLeaseTTL             = 30 * time.Second
	defaultUpstreamTimeout      = 10 * time.Second
	defaultTerminalRetention    = 30 * time.Minute
	defaultRateWindow           = 10 * time.Minute
	defaultRatePerUser          = 5
	defaultRatePerWorkspace     = 30
)

// DefaultSourceID is the WeCom-issued caller identifier shared by Cloud and
// self-hosted deployments. Operators may override it via MULTICA_WECOM_SOURCE_ID
// for debugging without re-provisioning the source.
const DefaultSourceID = "multica"

var (
	// ErrInstallInProgress: another begin already has this agent's pending
	// slot and the caller lacks recovery permission. HTTP 409.
	ErrInstallInProgress = errors.New("wecom: install already in progress")
	// ErrAgentMismatch: Idempotency-Key hit an existing session but its
	// agent_id differs — a replayed HTTP request cannot silently retarget.
	// HTTP 409.
	ErrAgentMismatch = errors.New("wecom: idempotency key belongs to a different agent")
	// ErrActiveInstallationExists: a live WeCom installation is already
	// bound to this agent; disconnect it first. HTTP 409.
	ErrActiveInstallationExists = errors.New("wecom: agent already has an active wecom installation")
	// ErrRateLimited: 10-minute begin count exceeded (user or workspace).
	// HTTP 429.
	ErrRateLimited = errors.New("wecom: install begin rate limit exceeded")
	// ErrIdempotencyKeyRequired: begin was called with an empty header.
	// HTTP 400.
	ErrIdempotencyKeyRequired = errors.New("wecom: Idempotency-Key header is required")
	// ErrIdempotencyKeyTooLong: header exceeded 128 bytes (spec §7.3.1).
	// HTTP 400.
	ErrIdempotencyKeyTooLong = errors.New("wecom: Idempotency-Key must not exceed 128 bytes")
	// ErrSessionNotFound: unknown / GC'd session, cross-workspace, or the
	// caller cannot view this session. Handler maps to 404.
	ErrSessionNotFound = errors.New("wecom: install session not found")

	// Manual-install sentinels (bot id + secret typed in by hand). The scan
	// flow cannot hit these: WeCom hands it credentials that are valid by
	// construction and unique by construction.

	// ErrBotCredentialsRequired: bot_id or secret was empty, over the length
	// cap, or carried control characters. HTTP 400.
	ErrBotCredentialsRequired = errors.New("wecom: bot_id and secret are required")
	// ErrInvalidBotCredentials: WeCom rejected the pair at the subscribe
	// handshake. HTTP 400.
	ErrInvalidBotCredentials = errors.New("wecom: bot credentials rejected by wecom")
	// ErrVerifyUnavailable: the credential probe could not reach WeCom, so
	// whether the credentials are valid is unknown. HTTP 502 — deliberately
	// NOT reported as bad credentials.
	ErrVerifyUnavailable = errors.New("wecom: could not verify bot credentials")
	// ErrBotIDOwnedByAnotherWorkspace: the (wecom, bot_id) routing slot is
	// live in a different Multica workspace. HTTP 409.
	ErrBotIDOwnedByAnotherWorkspace = errors.New("wecom: bot id is connected in another workspace")
	// ErrBotIDOwnedInThisWorkspace: another agent in this workspace already
	// holds the bot id. HTTP 409.
	ErrBotIDOwnedInThisWorkspace = errors.New("wecom: bot id is already connected to another agent in this workspace")
	// ErrBotIDOwnedByArchivedAgent: the holder is an archived agent, so the
	// binding is recoverable by unarchiving rather than by rebinding. HTTP 409.
	ErrBotIDOwnedByArchivedAgent = errors.New("wecom: bot id is held by an archived agent")
	// ErrManualInstallUnsupported: no secretbox key, so a submitted secret
	// cannot be sealed. HTTP 503.
	ErrManualInstallUnsupported = errors.New("wecom: manual install not configured")
)

// maxBotCredentialLen caps the hand-typed fields. WeCom bot ids and secrets are
// well under this; the cap exists so a paste accident cannot push an unbounded
// string into the config JSONB.
const maxBotCredentialLen = 256

// InstallStore is the narrow slice of the generated queries InstallService
// needs. WithTx returns the same interface bound to a transaction; the real
// adapter is dbInstallStore. Tests inject a fake so the service can be
// exercised without a live Postgres.
type InstallStore interface {
	WithTx(tx pgx.Tx) InstallStore
	LockWecomInstallBeginWorkspace(ctx context.Context, workspaceID pgtype.UUID) error
	GetWecomInstallSessionByRequestHash(ctx context.Context, arg db.GetWecomInstallSessionByRequestHashParams) (db.WecomInstallSession, error)
	GetPendingWecomInstallSessionByAgent(ctx context.Context, arg db.GetPendingWecomInstallSessionByAgentParams) (db.WecomInstallSession, error)
	CountWecomInstallSessionsInWindow(ctx context.Context, arg db.CountWecomInstallSessionsInWindowParams) (db.CountWecomInstallSessionsInWindowRow, error)
	GetActiveWecomInstallationForAgent(ctx context.Context, arg db.GetActiveWecomInstallationForAgentParams) (db.ChannelInstallation, error)
	CreateWecomInstallSession(ctx context.Context, arg db.CreateWecomInstallSessionParams) (db.WecomInstallSession, error)
	GetWecomInstallSession(ctx context.Context, id pgtype.UUID) (db.WecomInstallSession, error)
	ClaimDueWecomInstallSession(ctx context.Context, arg db.ClaimDueWecomInstallSessionParams) (db.WecomInstallSession, error)
	DeferClaimedWecomInstallSession(ctx context.Context, arg db.DeferClaimedWecomInstallSessionParams) (int64, error)
	CompleteWecomInstallSession(ctx context.Context, arg db.CompleteWecomInstallSessionParams) (int64, error)
	FailWecomInstallSession(ctx context.Context, arg db.FailWecomInstallSessionParams) (int64, error)
	PurgeTerminalWecomInstallSessions(ctx context.Context, arg db.PurgeTerminalWecomInstallSessionsParams) (int64, error)
	GetAgentInWorkspace(ctx context.Context, arg db.GetAgentInWorkspaceParams) (db.Agent, error)
	GetUser(ctx context.Context, id pgtype.UUID) (db.User, error)
	GetChannelInstallationByAppID(ctx context.Context, arg db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error)
	GetChannelInstallationOwnerByAppID(ctx context.Context, arg db.GetChannelInstallationOwnerByAppIDParams) (db.GetChannelInstallationOwnerByAppIDRow, error)
	ReclaimDeadChannelInstallationByAppID(ctx context.Context, arg db.ReclaimDeadChannelInstallationByAppIDParams) (pgtype.UUID, error)
	DeleteRevokedChannelInstallationForReplacement(ctx context.Context, arg db.DeleteRevokedChannelInstallationForReplacementParams) (pgtype.UUID, error)
	UpsertChannelInstallation(ctx context.Context, arg db.UpsertChannelInstallationParams) (db.ChannelInstallation, error)
	GetChannelInstallationInWorkspace(ctx context.Context, arg db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error)
	SetChannelInstallationStatus(ctx context.Context, arg db.SetChannelInstallationStatusParams) error
	ListChannelInstallationsByWorkspace(ctx context.Context, arg db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error)
}

// dbInstallStore adapts *db.Queries to InstallStore. The generated WithTx
// returns *db.Queries (concrete), so this wrapper re-wraps it to satisfy the
// interface (same pattern used by engine.dbSessionQueries).
type dbInstallStore struct{ *db.Queries }

// NewInstallStore returns the production adapter.
func NewInstallStore(q *db.Queries) InstallStore { return dbInstallStore{q} }

// WithTx binds a transaction and returns the interface-typed store.
func (s dbInstallStore) WithTx(tx pgx.Tx) InstallStore {
	return dbInstallStore{s.Queries.WithTx(tx)}
}

// InstallServiceConfig captures the tunables spec §3.3 / §4.3 requires. Zero
// values are filled in with the defaults constants above via withDefaults,
// so callers usually leave every field zero.
type InstallServiceConfig struct {
	// SourceID is the WeCom-issued caller identifier the generate/query_result
	// endpoints need. Empty means the deployment is running in maintenance
	// mode (no upstream calls).
	SourceID string
	// Box seals the short-lived scode / qr_code_url so a DB dump cannot
	// re-arm an aborted install. MUST be non-nil for a fully wired service.
	Box *secretbox.Box
	// Provider talks to the WeCom generate / query_result endpoints. Nil
	// means maintenance mode.
	Provider Provider
	// DialURL is the long-connection endpoint the manual-install credential
	// probe connects to. Empty means DefaultDialURL.
	DialURL string
	// Verify probes a submitted bot id / secret pair. Nil installs a wrapper
	// around VerifyCredentials; tests inject a fake so the default test path
	// never dials the network. A *AuthFailedError return means "WeCom rejected
	// these credentials"; any other error means "could not determine".
	Verify func(ctx context.Context, botID, secret string) error

	QRTTL                time.Duration
	GenerateDeadline     time.Duration
	PendingPollInterval  time.Duration
	CreatingPollInterval time.Duration
	LeaseTTL             time.Duration
	UpstreamTimeout      time.Duration
	TerminalRetention    time.Duration
	RateWindow           time.Duration
	RatePerUser          int
	RatePerWorkspace     int

	Now    func() time.Time
	Logger *slog.Logger
}

func (c InstallServiceConfig) withDefaults() InstallServiceConfig {
	if c.QRTTL == 0 {
		c.QRTTL = defaultQRTTL
	}
	if c.GenerateDeadline == 0 {
		c.GenerateDeadline = defaultGenerateDeadline
	}
	if c.PendingPollInterval == 0 {
		c.PendingPollInterval = defaultPendingPollInterval
	}
	if c.CreatingPollInterval == 0 {
		c.CreatingPollInterval = defaultCreatingPollInterval
	}
	if c.LeaseTTL == 0 {
		c.LeaseTTL = defaultLeaseTTL
	}
	if c.UpstreamTimeout == 0 {
		c.UpstreamTimeout = defaultUpstreamTimeout
	}
	if c.TerminalRetention == 0 {
		c.TerminalRetention = defaultTerminalRetention
	}
	if c.RateWindow == 0 {
		c.RateWindow = defaultRateWindow
	}
	if c.RatePerUser == 0 {
		c.RatePerUser = defaultRatePerUser
	}
	if c.RatePerWorkspace == 0 {
		c.RatePerWorkspace = defaultRatePerWorkspace
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Verify == nil {
		dialURL := c.DialURL
		logger := c.Logger
		c.Verify = func(ctx context.Context, botID, secret string) error {
			return VerifyCredentials(ctx, VerifyCredentialsConfig{
				DialURL: dialURL,
				BotID:   botID,
				Secret:  secret,
				Logger:  logger,
			})
		}
	}
	return c
}

// InstallService owns the HTTP-facing install lifecycle: begin (idempotency,
// pending resume, conflict, rate limit), status, and the finalize path the
// worker calls once WeCom's query_result returns success.
//
// It intentionally does NOT talk to the WeCom generate endpoint itself —
// that runs on the install worker so a slow upstream cannot stall an HTTP
// request. Begin only inserts the `creating` row and wakes the worker.
type InstallService struct {
	cfg    InstallServiceConfig
	store  InstallStore
	tx     engine.TxStarter
	notify func()
}

// NewInstallService binds the production store adapter to the pool +
// notification hook. notify is called after a successful begin so the
// InstallWorker starts a fresh generate on the next tick.
func NewInstallService(q *db.Queries, tx engine.TxStarter, cfg InstallServiceConfig, notify func()) *InstallService {
	return newInstallService(NewInstallStore(q), tx, cfg, notify)
}

func newInstallService(store InstallStore, tx engine.TxStarter, cfg InstallServiceConfig, notify func()) *InstallService {
	if notify == nil {
		notify = func() {}
	}
	return &InstallService{
		cfg:    cfg.withDefaults(),
		store:  store,
		tx:     tx,
		notify: notify,
	}
}

// Configured reports whether the service can drive an install end-to-end.
// Maintenance mode (no secret key / no provider) still lets the worker
// sweep stale sessions but refuses new begins.
func (s *InstallService) Configured() bool {
	return s.cfg.Box != nil && s.cfg.Provider != nil && strings.TrimSpace(s.cfg.SourceID) != ""
}

// SetNotify replaces the wake callback. Wiring order: the InstallService is
// constructed BEFORE the InstallWorker (the worker holds the service, not
// the other way around), so SetNotify lets router.go plug the worker's
// Notify in after both objects exist without a circular dependency.
func (s *InstallService) SetNotify(notify func()) {
	if notify == nil {
		s.notify = func() {}
		return
	}
	s.notify = notify
}

// BeginInstallParams is the trusted input from the handler: workspace,
// agent, and initiator are already authenticated + authorized (canManageAgent),
// and the Idempotency-Key header has been extracted verbatim.
type BeginInstallParams struct {
	WorkspaceID    pgtype.UUID
	AgentID        pgtype.UUID
	InitiatorID    pgtype.UUID
	IdempotencyKey string
	// CallerIsWorkspaceAdmin lets an owner/admin recover a pending session
	// initiated by a different user. Plain agent owners cannot.
	CallerIsWorkspaceAdmin bool
}

// BeginInstallResult is the wire payload spec §7.3.1 returns from
// POST /wecom/install/begin. Status may be creating / pending / success /
// error — the handler always returns 202 and the frontend polls status for
// the QR / final outcome.
type BeginInstallResult struct {
	SessionID string
	Status    string
}

// BeginInstall admits or reuses a session for p. The advisory lock keeps
// concurrent replays / different-key races serialized per workspace so
// duplicate `creating` inserts, missed pending recoveries, and rate-limit
// bypasses are all closed at the transaction level rather than in Go
// bookkeeping.
func (s *InstallService) BeginInstall(ctx context.Context, p BeginInstallParams) (BeginInstallResult, error) {
	if !s.Configured() {
		return BeginInstallResult{}, fmt.Errorf("wecom: install not configured")
	}
	key := strings.TrimSpace(p.IdempotencyKey)
	if key == "" {
		return BeginInstallResult{}, ErrIdempotencyKeyRequired
	}
	if len(key) > 128 {
		return BeginInstallResult{}, ErrIdempotencyKeyTooLong
	}
	hash := hashIdempotencyKey(key)

	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return BeginInstallResult{}, fmt.Errorf("wecom begin: start tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.store.WithTx(tx)

	if err := qtx.LockWecomInstallBeginWorkspace(ctx, p.WorkspaceID); err != nil {
		return BeginInstallResult{}, fmt.Errorf("wecom begin: advisory lock: %w", err)
	}

	// Request-hash replay: same key from the same initiator always returns
	// the same session. Cross-agent replay is a hard 409 because a replayed
	// HTTP retry cannot silently retarget.
	if existing, err := qtx.GetWecomInstallSessionByRequestHash(ctx, db.GetWecomInstallSessionByRequestHashParams{
		WorkspaceID:     p.WorkspaceID,
		InitiatorUserID: p.InitiatorID,
		RequestKeyHash:  hash,
	}); err == nil {
		if !uuidsEqual(existing.AgentID, p.AgentID) {
			return BeginInstallResult{}, ErrAgentMismatch
		}
		if err := tx.Commit(ctx); err != nil {
			return BeginInstallResult{}, fmt.Errorf("wecom begin: commit replay: %w", err)
		}
		return BeginInstallResult{SessionID: util.UUIDToString(existing.ID), Status: existing.Status}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return BeginInstallResult{}, fmt.Errorf("wecom begin: replay lookup: %w", err)
	}

	// Active-installation guard: a live bot exists for this agent already;
	// disconnect it before scanning again.
	if _, err := qtx.GetActiveWecomInstallationForAgent(ctx, db.GetActiveWecomInstallationForAgentParams{
		WorkspaceID: p.WorkspaceID,
		AgentID:     p.AgentID,
	}); err == nil {
		return BeginInstallResult{}, ErrActiveInstallationExists
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return BeginInstallResult{}, fmt.Errorf("wecom begin: active install lookup: %w", err)
	}

	// Pending session recovery: at most one creating/pending row exists per
	// (workspace, agent). The initiator can always resume; other agent
	// owners get 409 install_in_progress; workspace admins can resume too.
	if pending, err := qtx.GetPendingWecomInstallSessionByAgent(ctx, db.GetPendingWecomInstallSessionByAgentParams{
		WorkspaceID: p.WorkspaceID,
		AgentID:     p.AgentID,
	}); err == nil {
		canResume := uuidsEqual(pending.InitiatorUserID, p.InitiatorID) || p.CallerIsWorkspaceAdmin
		if !canResume {
			return BeginInstallResult{}, ErrInstallInProgress
		}
		if err := tx.Commit(ctx); err != nil {
			return BeginInstallResult{}, fmt.Errorf("wecom begin: commit resume: %w", err)
		}
		return BeginInstallResult{SessionID: util.UUIDToString(pending.ID), Status: pending.Status}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return BeginInstallResult{}, fmt.Errorf("wecom begin: pending lookup: %w", err)
	}

	// Rate window. Counts every session — success, error, pending — in the
	// window because each new begin can consume a WeCom generate quota slot.
	windowStart := s.cfg.Now().Add(-s.cfg.RateWindow)
	counts, err := qtx.CountWecomInstallSessionsInWindow(ctx, db.CountWecomInstallSessionsInWindowParams{
		WorkspaceID:     p.WorkspaceID,
		InitiatorUserID: p.InitiatorID,
		Since:           pgtype.Timestamptz{Time: windowStart, Valid: true},
	})
	if err != nil {
		return BeginInstallResult{}, fmt.Errorf("wecom begin: rate count: %w", err)
	}
	if counts.ByUser >= int64(s.cfg.RatePerUser) || counts.Total >= int64(s.cfg.RatePerWorkspace) {
		return BeginInstallResult{}, ErrRateLimited
	}

	session, err := qtx.CreateWecomInstallSession(ctx, db.CreateWecomInstallSessionParams{
		WorkspaceID:     p.WorkspaceID,
		AgentID:         p.AgentID,
		InitiatorUserID: p.InitiatorID,
		RequestKeyHash:  hash,
	})
	if err != nil {
		return BeginInstallResult{}, fmt.Errorf("wecom begin: create session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return BeginInstallResult{}, fmt.Errorf("wecom begin: commit: %w", err)
	}
	// Wake the install worker so generate runs on the next tick rather than
	// waiting a full poll cycle.
	s.notify()
	return BeginInstallResult{SessionID: util.UUIDToString(session.ID), Status: session.Status}, nil
}

// SessionSnapshot is what GetSession returns to the status handler. It never
// exposes the ciphertext columns — the handler decrypts qr_code_url in place
// via DecryptQRCodeURL only for initiator / admin viewers.
type SessionSnapshot struct {
	ID              pgtype.UUID
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	InitiatorUserID pgtype.UUID
	Status          string
	QRCodeURL       string // decrypted; only populated for authorized viewers
	ExpiresAt       time.Time
	InstallationID  pgtype.UUID
	ErrorReason     string
	ErrorMessage    string
}

// GetSession loads a session by id, scoped to the workspace to prevent
// enumeration across tenants. The handler already scoped the URL, but the
// service re-checks defense-in-depth. Unknown / cross-workspace ids surface
// as ErrSessionNotFound so the caller can uniformly map to 404.
//
// DecryptQR controls whether qr_code_url is decoded and returned; the
// handler passes true only for the session initiator or a workspace admin.
func (s *InstallService) GetSession(ctx context.Context, workspaceID pgtype.UUID, sessionID pgtype.UUID, decryptQR bool) (SessionSnapshot, error) {
	row, err := s.store.GetWecomInstallSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SessionSnapshot{}, ErrSessionNotFound
		}
		return SessionSnapshot{}, fmt.Errorf("wecom get session: %w", err)
	}
	if !uuidsEqual(row.WorkspaceID, workspaceID) {
		return SessionSnapshot{}, ErrSessionNotFound
	}
	snap := SessionSnapshot{
		ID:              row.ID,
		WorkspaceID:     row.WorkspaceID,
		AgentID:         row.AgentID,
		InitiatorUserID: row.InitiatorUserID,
		Status:          row.Status,
	}
	if row.ExpiresAt.Valid {
		snap.ExpiresAt = row.ExpiresAt.Time
	}
	if row.InstallationID.Valid {
		snap.InstallationID = row.InstallationID
	}
	if row.ErrorReason.Valid {
		snap.ErrorReason = row.ErrorReason.String
	}
	if row.ErrorMessage.Valid {
		snap.ErrorMessage = row.ErrorMessage.String
	}
	if decryptQR && row.QrCodeUrlEncrypted.Valid && s.cfg.Box != nil {
		if plain, err := decodeAndOpen(s.cfg.Box, row.QrCodeUrlEncrypted.String); err == nil {
			snap.QRCodeURL = string(plain)
		} else {
			s.cfg.Logger.Warn("wecom: decrypt qr_code_url failed",
				"session_id", util.UUIDToString(row.ID), "err", err)
		}
	}
	return snap, nil
}

// SessionState is the "which is my next status" summary the frontend cares
// about. Kept out of GetSession so callers can request it without decrypting.
func SessionStateFromStatus(status string) string {
	switch status {
	case InstallStatusCreating, InstallStatusPending, InstallStatusSuccess, InstallStatusError:
		return status
	default:
		return InstallStatusError
	}
}

// hashIdempotencyKey collapses the client's random string onto a fixed-width
// hash column. Storing the raw key would leak the client's replay salt into a
// DB dump without adding any product value.
func hashIdempotencyKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// mintLeaseToken returns a random opaque token stored on wecom_install_session
// while a worker holds the row. 24 bytes URL-safe base64 keeps it short
// enough for TEXT storage and unforgeable across replicas.
func mintLeaseToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// sealAndEncode seals plaintext with box and returns a base64 blob suitable
// for the TEXT columns. Empty plaintext returns empty string so the DEFAULT
// NULL semantics are preserved by the caller (pgtype.Text{Valid:false}).
func sealAndEncode(box *secretbox.Box, plaintext []byte) (string, error) {
	if len(plaintext) == 0 {
		return "", nil
	}
	sealed, err := box.Seal(plaintext)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// decodeAndOpen reverses sealAndEncode. Malformed base64 or auth failures
// bubble up so the caller can degrade (e.g. render creating instead of
// pending) rather than fabricate a URL.
func decodeAndOpen(box *secretbox.Box, encoded string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	return box.Open(raw)
}

func uuidsEqual(a, b pgtype.UUID) bool {
	if !a.Valid || !b.Valid {
		return false
	}
	return a.Bytes == b.Bytes
}

// bindBotParams is the trusted input to bindBot: the secret is already sealed
// and base64-encoded, and the bot id is already trimmed.
type bindBotParams struct {
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	InstallerUserID pgtype.UUID
	BotID           string
	SecretEncrypted string
}

// bindUpsertError marks a failure of the UpsertChannelInstallation statement
// specifically, so callers can tell "the (wecom, bot_id) routing slot is taken"
// apart from an infrastructure failure on one of the surrounding statements.
// The scan path renders it as installation_conflict; the manual path resolves
// the actual live owner and reports which one it is.
type bindUpsertError struct{ err error }

func (e *bindUpsertError) Error() string { return e.err.Error() }
func (e *bindUpsertError) Unwrap() error { return e.err }

// bindBot writes the installation row for a bot whose credentials are already
// established: locale snapshot, dead-owner reclaim, revoked-row replacement,
// then the upsert. Shared by the scan flow (InstallWorker.finalizeSuccess,
// which follows it with CompleteWecomInstallSession) and the manual flow
// (InstallManual, which commits directly) so the two paths cannot drift on the
// routing-slot cleanup rules.
//
// Runs entirely inside the caller's transaction; the caller owns commit and
// rollback.
func (s *InstallService) bindBot(ctx context.Context, qtx InstallStore, p bindBotParams) (db.ChannelInstallation, error) {
	// Locale snapshot from the initiator's user.language. Failure to load
	// degrades to default locale — an install must not be denied because
	// the initiator picked an unsupported language.
	locale := DefaultLocale
	if user, err := qtx.GetUser(ctx, p.InstallerUserID); err == nil {
		locale = NormalizeLocale(user.Language.String)
	}

	// Reclaim any dead installation holding this botid so the upsert below
	// cannot trip the (channel_type, app_id) unique index on a revoked
	// ghost owner.
	if _, err := qtx.ReclaimDeadChannelInstallationByAppID(ctx, db.ReclaimDeadChannelInstallationByAppIDParams{
		ChannelType: string(TypeWecom),
		AppID:       p.BotID,
		WorkspaceID: p.WorkspaceID,
		AgentID:     p.AgentID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelInstallation{}, fmt.Errorf("bind bot: reclaim dead: %w", err)
	}
	if _, err := qtx.DeleteRevokedChannelInstallationForReplacement(ctx, db.DeleteRevokedChannelInstallationForReplacementParams{
		WorkspaceID: p.WorkspaceID,
		AgentID:     p.AgentID,
		ChannelType: string(TypeWecom),
		NewAppID:    p.BotID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelInstallation{}, fmt.Errorf("bind bot: delete revoked replacement: %w", err)
	}

	cfgBytes, err := (InstallationConfig{
		AppID:           p.BotID,
		SecretEncrypted: p.SecretEncrypted,
		Locale:          locale,
	}).Marshal()
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("bind bot: marshal config: %w", err)
	}

	inst, err := qtx.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
		WorkspaceID:     p.WorkspaceID,
		AgentID:         p.AgentID,
		ChannelType:     string(TypeWecom),
		Config:          cfgBytes,
		InstallerUserID: p.InstallerUserID,
	})
	if err != nil {
		return db.ChannelInstallation{}, &bindUpsertError{err: err}
	}
	return inst, nil
}

// ManualConfigured reports whether hand-entered bot credentials can be
// installed. Unlike Configured() it needs only the secretbox key: SourceID and
// the HTTP Provider exist solely for the scan-code generate / query_result
// calls, so a deployment that never provisioned a WeCom scan source can still
// connect a bot by typing its credentials.
func (s *InstallService) ManualConfigured() bool {
	return s.cfg.Box != nil
}

// InstallManualParams is the trusted input from the handler: workspace, agent
// and initiator are authenticated and authorized (canManageAgent).
type InstallManualParams struct {
	WorkspaceID pgtype.UUID
	AgentID     pgtype.UUID
	InitiatorID pgtype.UUID
	BotID       string
	Secret      string
}

// InstallManual connects an EXISTING WeCom smart bot from credentials the user
// typed in, as an alternative to the scan flow (which always provisions a brand
// new bot). Unlike BeginInstall this is synchronous: there is no generate/poll
// round trip to keep off the request path, so the caller gets the real outcome
// — including "that secret is wrong" — in the response.
//
// Order matters. The credential probe occupies the bot's connection slot, so
// every cheap rejection (validation, this agent already connected, bot id owned
// elsewhere) runs BEFORE it, and the probe never kicks a live connection.
func (s *InstallService) InstallManual(ctx context.Context, p InstallManualParams) (db.ChannelInstallation, error) {
	if !s.ManualConfigured() {
		return db.ChannelInstallation{}, ErrManualInstallUnsupported
	}
	botID := strings.TrimSpace(p.BotID)
	secret := strings.TrimSpace(p.Secret)
	if !validBotCredential(botID) || !validBotCredential(secret) {
		return db.ChannelInstallation{}, ErrBotCredentialsRequired
	}

	// Same product rule as the scan flow: one active WeCom bot per agent,
	// disconnect before connecting another.
	if _, err := s.store.GetActiveWecomInstallationForAgent(ctx, db.GetActiveWecomInstallationForAgentParams{
		WorkspaceID: p.WorkspaceID,
		AgentID:     p.AgentID,
	}); err == nil {
		return db.ChannelInstallation{}, ErrActiveInstallationExists
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelInstallation{}, fmt.Errorf("wecom manual: active install lookup: %w", err)
	}

	// Cheap accurate rejection before the probe. This is NOT the real guard —
	// two callers submitting the same bot id concurrently both read no live
	// owner here — the unique index below is. It exists so the common case
	// gets a precise message without paying for a WeCom round trip.
	if err := s.checkBotIDAvailable(ctx, p.WorkspaceID, p.AgentID, botID); err != nil {
		return db.ChannelInstallation{}, err
	}

	if err := s.cfg.Verify(ctx, botID, secret); err != nil {
		var authErr *AuthFailedError
		if errors.As(err, &authErr) {
			s.cfg.Logger.InfoContext(ctx, "wecom manual install: credentials rejected",
				"bot_id", botID,
				"errcode", authErr.Code,
				"workspace_id", util.UUIDToString(p.WorkspaceID),
			)
			return db.ChannelInstallation{}, ErrInvalidBotCredentials
		}
		s.cfg.Logger.WarnContext(ctx, "wecom manual install: verification unavailable",
			"bot_id", botID,
			"error", err,
			"workspace_id", util.UUIDToString(p.WorkspaceID),
		)
		return db.ChannelInstallation{}, ErrVerifyUnavailable
	}

	sealed, err := sealAndEncode(s.cfg.Box, []byte(secret))
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("wecom manual: seal secret: %w", err)
	}

	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("wecom manual: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	inst, err := s.bindBot(ctx, s.store.WithTx(tx), bindBotParams{
		WorkspaceID:     p.WorkspaceID,
		AgentID:         p.AgentID,
		InstallerUserID: p.InitiatorID,
		BotID:           botID,
		SecretEncrypted: sealed,
	})
	if err != nil {
		var upsertErr *bindUpsertError
		if errors.As(err, &upsertErr) {
			// The routing slot was taken between the pre-check and the insert,
			// or is held by a row the reclaim gate deliberately spares. Resolve
			// the live owner on the base store — this tx is aborted.
			s.cfg.Logger.WarnContext(ctx, "wecom manual install: upsert conflict",
				"bot_id", botID, "error", err)
			return db.ChannelInstallation{}, s.botIDConflictErr(ctx, p.WorkspaceID, p.AgentID, botID)
		}
		return db.ChannelInstallation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("wecom manual: commit: %w", err)
	}
	return inst, nil
}

// checkBotIDAvailable rejects a bot id whose (wecom, bot_id) routing slot is
// held by a LIVE installation. A revoked holder is not a conflict: bindBot's
// reclaim / replacement statements free it, which is what makes "disconnect,
// then reconnect the same bot" work.
func (s *InstallService) checkBotIDAvailable(ctx context.Context, wsID, agentID pgtype.UUID, botID string) error {
	existing, err := s.store.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: string(TypeWecom),
		AppID:       botID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("wecom manual: bot id lookup: %w", err)
	}
	if existing.Status != "active" {
		return nil
	}
	return s.botIDConflictErr(ctx, wsID, agentID, botID)
}

// botIDConflictErr classifies who holds the routing slot so the caller can
// render an accurate message instead of a catch-all that always blames another
// workspace. Mirrors slack.InstallService.liveOwnerConflictErr. A slot that
// looks free by the time we look (concurrent disconnect) degrades to the
// cross-workspace sentinel; a retry then succeeds.
func (s *InstallService) botIDConflictErr(ctx context.Context, wsID, agentID pgtype.UUID, botID string) error {
	owner, err := s.store.GetChannelInstallationOwnerByAppID(ctx, db.GetChannelInstallationOwnerByAppIDParams{
		ChannelType: string(TypeWecom),
		AppID:       botID,
	})
	if err != nil {
		return ErrBotIDOwnedByAnotherWorkspace
	}
	switch {
	case !uuidsEqual(owner.WorkspaceID, wsID):
		return ErrBotIDOwnedByAnotherWorkspace
	case owner.AgentArchivedAt.Valid:
		return ErrBotIDOwnedByArchivedAgent
	case uuidsEqual(owner.AgentID, agentID):
		// Same agent, same bot: the caller's own active installation. The
		// active-install guard should already have caught this; reaching here
		// means it was created concurrently.
		return ErrActiveInstallationExists
	default:
		return ErrBotIDOwnedInThisWorkspace
	}
}

// validBotCredential enforces the hand-entry limits: non-empty, bounded, and
// free of control characters (a stray newline from a copy-paste would otherwise
// end up inside the config JSON and in log lines).
func validBotCredential(v string) bool {
	if v == "" || len(v) > maxBotCredentialLen {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
