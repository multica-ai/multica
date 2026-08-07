package wecom

// install.go — the scan-code install session lifecycle.
//
// Creating a WeCom smart bot by QR is a multi-minute conversation: ask WeCom for
// a QR, show it to an admin, then poll until they scan it and WeCom hands back
// the bot's credentials. None of that fits in the request that started it, so a
// session row carries the state and install_worker.go drives it.
//
// This file owns the HTTP-facing half — admitting a begin, and reading a session
// back for the status poll. It deliberately does NOT call WeCom: the worker owns
// every upstream call, so a slow or wedged WeCom cannot hold an HTTP request
// open.
//
// The manual bot_id + secret path is not here; that is installation.go's
// Upsert, which the worker also reuses to bind a scanned bot.

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

// Session status wire values. String constants because the DB CHECK constraint
// uses the same literals and the frontend switches on them directly.
const (
	InstallStatusCreating = "creating"
	InstallStatusPending  = "pending"
	InstallStatusSuccess  = "success"
	InstallStatusError    = "error"
)

// Install error reasons are the stable machine codes the frontend switches on.
// The error_message stored alongside is operator-facing detail.
const (
	InstallErrorExpired                 = "expired"
	InstallErrorGenerateFailed          = "generate_failed"
	InstallErrorIntegrationUnconfigured = "integration_unconfigured"
	InstallErrorInstallationConflict    = "installation_conflict"
	InstallErrorWecomProtocolError      = "wecom_protocol_error"
	InstallErrorInternalError           = "internal_error"
)

// Install flow tunables. Each is a field on InstallServiceConfig so tests can
// shrink a window without redefining the service.
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

// DefaultSourceID is the WeCom-issued caller identifier. Operators may override
// it with MULTICA_WECOM_SOURCE_ID without re-provisioning the source.
const DefaultSourceID = "multica"

var (
	// ErrInstallInProgress: another begin holds this agent's live session slot
	// and the caller cannot resume it. HTTP 409.
	ErrInstallInProgress = errors.New("wecom: install already in progress")
	// ErrAgentMismatch: the idempotency key hit an existing session whose agent
	// differs. A replayed HTTP request must not silently retarget which agent
	// gets the bot. HTTP 409.
	ErrAgentMismatch = errors.New("wecom: idempotency key belongs to a different agent")
	// ErrActiveInstallationExists: a live WeCom installation is already bound to
	// this agent; disconnect it first. HTTP 409.
	ErrActiveInstallationExists = errors.New("wecom: agent already has an active wecom installation")
	// ErrRateLimited: the begin window count was exceeded, per user or per
	// workspace. HTTP 429.
	ErrRateLimited = errors.New("wecom: install begin rate limit exceeded")
	// ErrIdempotencyKeyRequired: begin was called without the header. HTTP 400.
	ErrIdempotencyKeyRequired = errors.New("wecom: Idempotency-Key header is required")
	// ErrIdempotencyKeyTooLong: the header exceeded maxIdempotencyKeyLen. HTTP 400.
	ErrIdempotencyKeyTooLong = errors.New("wecom: Idempotency-Key is too long")
	// ErrSessionNotFound: unknown, purged, or cross-workspace session. HTTP 404.
	ErrSessionNotFound = errors.New("wecom: install session not found")
	// ErrInstallNotConfigured: no secret key or no provider, so the scan flow
	// cannot run at all. HTTP 503.
	ErrInstallNotConfigured = errors.New("wecom: scan install not configured")
)

// maxIdempotencyKeyLen bounds the client-chosen header so a paste accident
// cannot push an unbounded string at the hash.
const maxIdempotencyKeyLen = 128

// BotBinder is the bind half of the finalize step: exactly the slice of
// InstallationService the worker needs. An interface so the worker's success
// path is testable without a live Postgres, and so boot can declare an
// interface-typed variable rather than storing a possibly-nil
// *InstallationService (a typed nil in an interface is not nil, which would
// defeat Configured() and panic at finalize). *InstallationService is the
// production value.
type BotBinder interface {
	Upsert(ctx context.Context, p InstallationParams) (Installation, error)
}

// InstallStore is the narrow slice of generated queries the install flow needs.
// WithTx returns the same interface bound to a transaction; tests inject a fake
// so the service runs without a live Postgres.
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
}

type dbInstallStore struct{ *db.Queries }

// NewInstallStore returns the production adapter.
func NewInstallStore(q *db.Queries) InstallStore { return dbInstallStore{q} }

// WithTx binds a transaction and returns the interface-typed store.
func (s dbInstallStore) WithTx(tx pgx.Tx) InstallStore {
	return dbInstallStore{s.Queries.WithTx(tx)}
}

// InstallServiceConfig captures the tunables. The zero value is filled in by
// withDefaults, so a caller only sets what it wants to change.
type InstallServiceConfig struct {
	// Provider talks to WeCom's QR endpoints. Nil disables the scan flow; the
	// worker still runs so in-flight sessions reach a terminal state instead of
	// hanging as a forever-spinning dialog.
	Provider Provider

	// Box seals scode and qr_code_url at rest. Nil disables the scan flow for
	// the same reason a nil box disables InstallationService: those values are a
	// bearer credential for "finish creating this bot".
	Box *secretbox.Box

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

	Logger *slog.Logger
	Now    func() time.Time
}

func (c InstallServiceConfig) withDefaults() InstallServiceConfig {
	if c.QRTTL <= 0 {
		c.QRTTL = defaultQRTTL
	}
	if c.GenerateDeadline <= 0 {
		c.GenerateDeadline = defaultGenerateDeadline
	}
	if c.PendingPollInterval <= 0 {
		c.PendingPollInterval = defaultPendingPollInterval
	}
	if c.CreatingPollInterval <= 0 {
		c.CreatingPollInterval = defaultCreatingPollInterval
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = defaultLeaseTTL
	}
	if c.UpstreamTimeout <= 0 {
		c.UpstreamTimeout = defaultUpstreamTimeout
	}
	if c.TerminalRetention <= 0 {
		c.TerminalRetention = defaultTerminalRetention
	}
	if c.RateWindow <= 0 {
		c.RateWindow = defaultRateWindow
	}
	if c.RatePerUser <= 0 {
		c.RatePerUser = defaultRatePerUser
	}
	if c.RatePerWorkspace <= 0 {
		c.RatePerWorkspace = defaultRatePerWorkspace
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// InstallService owns the HTTP-facing install lifecycle: admitting a begin
// (idempotency, guards, rate limit) and reading a session back.
type InstallService struct {
	store         InstallStore
	tx            engine.TxStarter
	installations BotBinder
	cfg           InstallServiceConfig
	notify        func()
}

// NewInstallService binds the production store to the pool. installations is
// what the worker uses to bind a scanned bot; notify wakes the worker so
// generate runs immediately rather than on the next poll tick.
func NewInstallService(q *db.Queries, tx engine.TxStarter, installations BotBinder, cfg InstallServiceConfig, notify func()) *InstallService {
	return newInstallService(NewInstallStore(q), tx, installations, cfg, notify)
}

func newInstallService(store InstallStore, tx engine.TxStarter, installations BotBinder, cfg InstallServiceConfig, notify func()) *InstallService {
	if notify == nil {
		notify = func() {}
	}
	return &InstallService{
		store:         store,
		tx:            tx,
		installations: installations,
		cfg:           cfg.withDefaults(),
		notify:        notify,
	}
}

// Configured reports whether the service can drive a scan install end to end.
// A false result still lets the worker run: in-flight sessions need to reach a
// terminal state rather than spin forever.
func (s *InstallService) Configured() bool {
	return s != nil && s.cfg.Provider != nil && s.cfg.Box != nil && s.installations != nil
}

// SetNotify replaces the wake callback. The service is constructed before the
// worker that consumes it, so boot wires this after both exist rather than
// making the two constructors circular.
func (s *InstallService) SetNotify(notify func()) {
	if notify == nil {
		notify = func() {}
	}
	s.notify = notify
}

// BeginInstallParams is the trusted input the handler has already authorized.
type BeginInstallParams struct {
	WorkspaceID pgtype.UUID
	AgentID     pgtype.UUID
	InitiatorID pgtype.UUID
	// CallerIsWorkspaceAdmin lets an admin resume somebody else's stuck
	// session, so a colleague who closed their laptop mid-scan does not leave
	// the agent's install slot locked until it expires.
	CallerIsWorkspaceAdmin bool
	IdempotencyKey         string
}

// BeginInstallResult is what POST /wecom/install/begin returns. The QR itself
// arrives on the status poll, once the worker has fetched it.
type BeginInstallResult struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

// BeginInstall admits or reuses a session.
//
// Everything runs under a per-workspace advisory lock. Without it, concurrent
// begins race past each of the guards below — two `creating` rows for one agent,
// a missed pending recovery, a rate limit both callers read as unexceeded — and
// every duplicate spends a WeCom generate quota slot and shows the admin a
// second QR for a bot nobody will own.
func (s *InstallService) BeginInstall(ctx context.Context, p BeginInstallParams) (BeginInstallResult, error) {
	if !s.Configured() {
		return BeginInstallResult{}, ErrInstallNotConfigured
	}
	key := strings.TrimSpace(p.IdempotencyKey)
	if key == "" {
		return BeginInstallResult{}, ErrIdempotencyKeyRequired
	}
	if len(key) > maxIdempotencyKeyLen {
		return BeginInstallResult{}, ErrIdempotencyKeyTooLong
	}
	hash := hashIdempotencyKey(key)

	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return BeginInstallResult{}, fmt.Errorf("wecom begin: start tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.store.WithTx(tx)

	if err := qtx.LockWecomInstallBeginWorkspace(ctx, p.WorkspaceID); err != nil {
		return BeginInstallResult{}, fmt.Errorf("wecom begin: advisory lock: %w", err)
	}

	// Replay: the same key from the same initiator always returns the same
	// session. A cross-agent replay is a hard conflict, never a retarget.
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

	// A live bot already exists for this agent. Creating a second one on WeCom's
	// side would leave the first orphaned there.
	if _, err := qtx.GetActiveWecomInstallationForAgent(ctx, db.GetActiveWecomInstallationForAgentParams{
		WorkspaceID: p.WorkspaceID,
		AgentID:     p.AgentID,
	}); err == nil {
		return BeginInstallResult{}, ErrActiveInstallationExists
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return BeginInstallResult{}, fmt.Errorf("wecom begin: active install lookup: %w", err)
	}

	// At most one live session per agent (enforced by a partial unique index).
	// The initiator can always resume theirs; an admin can resume anyone's.
	if pending, err := qtx.GetPendingWecomInstallSessionByAgent(ctx, db.GetPendingWecomInstallSessionByAgentParams{
		WorkspaceID: p.WorkspaceID,
		AgentID:     p.AgentID,
	}); err == nil {
		if !uuidsEqual(pending.InitiatorUserID, p.InitiatorID) && !p.CallerIsWorkspaceAdmin {
			return BeginInstallResult{}, ErrInstallInProgress
		}
		if err := tx.Commit(ctx); err != nil {
			return BeginInstallResult{}, fmt.Errorf("wecom begin: commit resume: %w", err)
		}
		return BeginInstallResult{SessionID: util.UUIDToString(pending.ID), Status: pending.Status}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return BeginInstallResult{}, fmt.Errorf("wecom begin: pending lookup: %w", err)
	}

	// Rate window. Counts every session in the window regardless of outcome:
	// each begin can spend a WeCom generate slot, and that quota belongs to the
	// deployment, shared across workspaces.
	counts, err := qtx.CountWecomInstallSessionsInWindow(ctx, db.CountWecomInstallSessionsInWindowParams{
		WorkspaceID:     p.WorkspaceID,
		InitiatorUserID: p.InitiatorID,
		Since:           pgtype.Timestamptz{Time: s.cfg.Now().Add(-s.cfg.RateWindow), Valid: true},
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
	// Wake the worker so generate runs now instead of on the next poll tick —
	// the admin is watching a spinner.
	s.notify()
	return BeginInstallResult{SessionID: util.UUIDToString(session.ID), Status: session.Status}, nil
}

// SessionSnapshot is what the status handler returns. It never carries the
// ciphertext columns; QRCodeURL is populated only for an authorized viewer.
type SessionSnapshot struct {
	ID              pgtype.UUID
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	InitiatorUserID pgtype.UUID
	Status          string
	QRCodeURL       string
	ExpiresAt       time.Time
	InstallationID  pgtype.UUID
	ErrorReason     string
	ErrorMessage    string
}

// GetSession loads a session, scoped to the workspace so a session id cannot be
// used to enumerate across tenants. The handler already scoped the route; this
// re-checks because the cost is one comparison.
//
// decryptQR must be true only for the initiator or a workspace admin: the QR URL
// embeds the scode, so anyone holding it can finish creating the bot.
func (s *InstallService) GetSession(ctx context.Context, workspaceID, sessionID pgtype.UUID, decryptQR bool) (SessionSnapshot, error) {
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
			// A QR we cannot decrypt is no reason to fail the poll: the status is
			// still useful, and the worker will terminate the session.
			s.cfg.Logger.WarnContext(ctx, "wecom: decrypt qr_code_url failed",
				"session_id", util.UUIDToString(row.ID), "error", err)
		}
	}
	return snap, nil
}

// hashIdempotencyKey collapses the client's string onto a fixed-width column.
// Storing it raw would put a client-chosen value in every DB dump for no product
// benefit.
func hashIdempotencyKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// mintLeaseToken returns the token that fences one worker's claim.
func mintLeaseToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("wecom: mint lease token: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

// sealAndEncode seals plaintext and base64-encodes it for a TEXT column.
func sealAndEncode(box *secretbox.Box, plaintext []byte) (string, error) {
	if box == nil {
		return "", errors.New("wecom: secretbox not configured")
	}
	sealed, err := box.Seal(plaintext)
	if err != nil {
		return "", fmt.Errorf("wecom: seal: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// decodeAndOpen is sealAndEncode's inverse.
func decodeAndOpen(box *secretbox.Box, encoded string) ([]byte, error) {
	if box == nil {
		return nil, errors.New("wecom: secretbox not configured")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("wecom: decode: %w", err)
	}
	return box.Open(raw)
}

func uuidsEqual(a, b pgtype.UUID) bool {
	return a.Valid && b.Valid && a.Bytes == b.Bytes
}
