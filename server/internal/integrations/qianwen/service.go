package qianwen

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	taskservice "github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	maxQueryBytes         = 16 * 1024
	maxStatusOutputRunes  = 8_000
	claimReleaseTimeout   = 100 * time.Millisecond
	pairingCodeKeyDomain  = "multica:qianwen:pairing-code:key:v1"
	pairingCodeMACDomain  = "multica:qianwen:pairing-code:mac:v1\x00"
	pairingCodeSpace      = 100_000_000
	pairingCodeAttempts   = 8
	pairingCodeUniqueIdx  = "idx_qianwen_pairing_code_installation_code"
	pairingIdentityDomain = "multica:qianwen:pairing-identity:v1\x00"
	pairingNonceDomain    = "multica:qianwen:invocation-nonce:v1\x00"
	pairingRequestDomain  = "multica:qianwen:pairing-request:v1\x00"
	pairingIdentityLimit  = 5
	pairingInstallLimit   = 20
)

var (
	ErrUnauthorized              = errors.New("qianwen: invalid connection credentials")
	ErrInstallationNotFound      = errors.New("qianwen: installation not found")
	ErrInstallationAlreadyActive = errors.New("qianwen: installation is already active")
	ErrPairingUnavailable        = errors.New("qianwen: pairing is not configured")
	ErrPairingCodeInvalid        = errors.New("qianwen: pairing code is invalid or expired")
	ErrPairingRateLimited        = errors.New("qianwen: pairing attempt limit exceeded")
	ErrPairingAccessDenied       = errors.New("qianwen: pairing target no longer has agent access")
	ErrBindingAlreadyAssigned    = errors.New("qianwen: identity is already bound to another user")
	ErrInvocationReplay          = errors.New("qianwen: invocation nonce was already used")
	ErrRequestNotFound           = errors.New("qianwen: request not found")
	ErrRequestConflict           = errors.New("qianwen: request_id was already used for a different query")
	ErrInvalidRequest            = errors.New("qianwen: invalid request")
	ErrUnsupportedCommand        = errors.New("qianwen: unsupported channel command")
	ErrTaskNotQueued             = errors.New("qianwen: request stored but task not queued")
)

// SubmitRequest is the stable request body exposed to a Qianwen Skill tool.
// request_id must be a caller-generated UUID so Qianwen retries are idempotent.
type SubmitRequest struct {
	RequestID string `json:"request_id"`
	Query     string `json:"query"`
}

// SubmitInvocation combines the JSON tool body with Qianwen system-context
// identity and replay metadata supplied in fixed headers.
type SubmitInvocation struct {
	Request  SubmitRequest
	Identity InvocationMetadata
}

// StatusInvocation carries the path request id plus the same signed identity
// envelope used by submit.
type StatusInvocation struct {
	RequestID string
	Identity  InvocationMetadata
}

// SubmitResult is deliberately small so the Skill endpoint can acknowledge
// inside Qianwen's three-second tool timeout. The actual result is polled.
type SubmitResult struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
}

// RequestStatus is the public polling response. It only exposes the redacted
// assistant transcript, never raw task result/error payloads.
type RequestStatus struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	TaskID    string `json:"task_id,omitempty"`
	Output    string `json:"output,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Message   string `json:"message,omitempty"`
}

// InstallationResult returns the one-time plaintext token after install. The
// token is never recoverable from the database later.
type InstallationResult struct {
	Installation db.ChannelInstallation
	ConnectionID string
	AccessToken  string
}

// PairingCodeResult contains the one-time plaintext returned by the management
// API. Only its keyed digest is persisted.
type PairingCodeResult struct {
	Code      string
	ExpiresAt time.Time
}

type PairingRedeemRequest struct {
	Code     string
	Identity InvocationMetadata
}

type PairingBindingResult struct {
	InstallationID pgtype.UUID
	MulticaUserID  pgtype.UUID
}

type serviceQueries interface {
	InstallQianwenPersonal(context.Context, db.InstallQianwenPersonalParams) (db.ChannelInstallation, error)
	ListChannelInstallationsByWorkspace(context.Context, db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error)
	GetChannelInstallationInWorkspace(context.Context, db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error)
	GetChannelInstallationByAppID(context.Context, db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error)
	RevokeQianwenInstallation(context.Context, db.RevokeQianwenInstallationParams) (int64, error)
	GetMemberByUserAndWorkspace(context.Context, db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
	GetActiveQianwenInvocationUser(context.Context, db.GetActiveQianwenInvocationUserParams) (pgtype.UUID, error)
	ClaimQianwenRequest(context.Context, db.ClaimQianwenRequestParams) (db.QianwenSkillRequest, error)
	GetQianwenRequest(context.Context, db.GetQianwenRequestParams) (db.QianwenSkillRequest, error)
	SetQianwenRequestSession(context.Context, db.SetQianwenRequestSessionParams) (int64, error)
	ReleaseQianwenRequestClaim(context.Context, db.ReleaseQianwenRequestClaimParams) (int64, error)
	GetChatSession(context.Context, pgtype.UUID) (db.ChatSession, error)
	GetAgent(context.Context, pgtype.UUID) (db.Agent, error)
	GetQianwenRequestStatus(context.Context, db.GetQianwenRequestStatusParams) (db.GetQianwenRequestStatusRow, error)
	UpsertQianwenPairingCode(context.Context, db.UpsertQianwenPairingCodeParams) (db.QianwenPairingCode, error)
}

// RequestSessionEnsurer isolates every external request in its own durable
// Multica chat while reusing the channel engine's session/binding lifecycle.
type RequestSessionEnsurer interface {
	EnsureSession(context.Context, engine.EnsureSessionInput) (pgtype.UUID, error)
}

// ChannelTaskSender is the atomic task/message seam implemented by TaskService.
// The finalizer runs in the same transaction as the task and input message, so
// the request ledger can never point at a half-created turn (or vice versa).
type ChannelTaskSender interface {
	SendChannelDirectChatMessage(context.Context, db.ChatSession, db.Agent, pgtype.UUID, string, taskservice.ChannelDirectChatFinalize) (*taskservice.DirectChatSendResult, error)
}

// Service owns the personal Skill credential and polling ingress. A durable
// request ledger provides conflict detection, crash-safe idempotency, and a
// status handle independent from an archivable chat binding.
type Service struct {
	q                serviceQueries
	dbq              *db.Queries
	tx               engine.TxStarter
	sessions         RequestSessionEnsurer
	tasks            ChannelTaskSender
	pairingDigestKey []byte
	pairingRandom    io.Reader
	now              func() time.Time
}

func NewService(q *db.Queries, sessions RequestSessionEnsurer, tasks ChannelTaskSender, tx engine.TxStarter, deploymentSecret []byte) (*Service, error) {
	service, err := newService(q, sessions, tasks, deploymentSecret)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, errors.New("qianwen: service requires a transaction starter")
	}
	service.dbq = q
	service.tx = tx
	return service, nil
}

func newService(q serviceQueries, sessions RequestSessionEnsurer, tasks ChannelTaskSender, deploymentSecret []byte) (*Service, error) {
	if q == nil {
		return nil, errors.New("qianwen: service requires queries")
	}
	if sessions == nil {
		return nil, errors.New("qianwen: service requires a chat session service")
	}
	if tasks == nil {
		return nil, errors.New("qianwen: service requires an atomic channel task sender")
	}
	service := &Service{
		q:             q,
		sessions:      sessions,
		tasks:         tasks,
		pairingRandom: rand.Reader,
		now:           time.Now,
	}
	if len(deploymentSecret) > 0 {
		keyMAC := hmac.New(sha256.New, deploymentSecret)
		_, _ = keyMAC.Write([]byte(pairingCodeKeyDomain))
		service.pairingDigestKey = keyMAC.Sum(nil)
	}
	return service, nil
}

// PairingSupported reports whether this service instance has every capability
// required to mint and redeem identity-pairing codes. Existing bound identities
// can continue to submit when this is false; only new pairing is unavailable.
func (s *Service) PairingSupported() bool {
	return s != nil && len(s.pairingDigestKey) > 0 && s.dbq != nil && s.tx != nil
}

// InstallPersonal creates a private Skill connection for an agent or
// reactivates one that was explicitly revoked. An active installation is never
// mutated here because silently replacing its credential would break an
// already configured Tool. Provider-side credential replacement remains an
// account-level acceptance gate.
func (s *Service) InstallPersonal(ctx context.Context, workspaceID, agentID, installerID pgtype.UUID) (InstallationResult, error) {
	if !s.PairingSupported() {
		return InstallationResult{}, ErrPairingUnavailable
	}
	connectionID, token, err := generateCredentials()
	if err != nil {
		return InstallationResult{}, err
	}
	config, err := encodeInstallConfig(connectionID, token)
	if err != nil {
		return InstallationResult{}, fmt.Errorf("encode installation config: %w", err)
	}
	row, err := s.q.InstallQianwenPersonal(ctx, db.InstallQianwenPersonalParams{
		WorkspaceID:     workspaceID,
		AgentID:         agentID,
		Config:          config,
		InstallerUserID: installerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, lookupErr := s.q.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
			WorkspaceID: workspaceID,
			ChannelType: string(TypeQianwen),
		})
		if lookupErr != nil {
			return InstallationResult{}, fmt.Errorf("classify qianwen installation conflict: %w", lookupErr)
		}
		for _, installation := range existing {
			if installation.AgentID == agentID && installation.Status == "active" {
				return InstallationResult{}, ErrInstallationAlreadyActive
			}
		}
		return InstallationResult{}, ErrInstallationNotFound
	}
	if err != nil {
		return InstallationResult{}, fmt.Errorf("persist qianwen installation: %w", err)
	}
	return InstallationResult{
		Installation: row,
		ConnectionID: connectionID,
		AccessToken:  token,
	}, nil
}

func (s *Service) ListByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]db.ChannelInstallation, error) {
	return s.q.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: workspaceID,
		ChannelType: string(TypeQianwen),
	})
}

func (s *Service) GetInWorkspace(ctx context.Context, id, workspaceID pgtype.UUID) (db.ChannelInstallation, error) {
	row, err := s.q.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          id,
		WorkspaceID: workspaceID,
		ChannelType: string(TypeQianwen),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelInstallation{}, ErrInstallationNotFound
	}
	return row, err
}

func (s *Service) Revoke(ctx context.Context, workspaceID, id pgtype.UUID) error {
	rows, err := s.q.RevokeQianwenInstallation(ctx, db.RevokeQianwenInstallationParams{
		InstallationID: id,
		WorkspaceID:    workspaceID,
	})
	if err != nil {
		return fmt.Errorf("revoke qianwen installation: %w", err)
	}
	if rows != 1 {
		return ErrInstallationNotFound
	}
	return nil
}

// UnbindCurrentUser removes only the authenticated Multica member's Qianwen
// identity, pending spoken code, and successful short-lived pairing replay
// state. It does not revoke the installation credential or affect another
// bound member. The SQL statement owns the lifecycle locks and is idempotent
// while the installation still exists.
func (s *Service) UnbindCurrentUser(ctx context.Context, workspaceID, installationID, userID pgtype.UUID) error {
	if s.dbq == nil || s.tx == nil {
		return errors.New("qianwen: unbind requires transaction support")
	}
	installation, err := s.q.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          installationID,
		WorkspaceID: workspaceID,
		ChannelType: string(TypeQianwen),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInstallationNotFound
	}
	if err != nil {
		return fmt.Errorf("load qianwen installation for unbind: %w", err)
	}

	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin qianwen unbind transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.dbq.WithTx(tx)
	if err := qtx.LockSubscriberWrites(ctx, db.LockSubscriberWritesParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	}); err != nil {
		return fmt.Errorf("lock qianwen unbind member lifecycle: %w", err)
	}
	if _, err := qtx.LockWorkspaceForChatSessionCreate(ctx, workspaceID); errors.Is(err, pgx.ErrNoRows) {
		return ErrInstallationNotFound
	} else if err != nil {
		return fmt.Errorf("lock qianwen unbind workspace: %w", err)
	}
	if _, err := qtx.LockAgentForAutopilotAssignment(ctx, db.LockAgentForAutopilotAssignmentParams{
		ID:          installation.AgentID,
		WorkspaceID: workspaceID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return ErrInstallationNotFound
	} else if err != nil {
		return fmt.Errorf("lock qianwen unbind agent: %w", err)
	}
	if _, err := qtx.LockQianwenInstallationForUnbind(ctx, db.LockQianwenInstallationForUnbindParams{
		InstallationID: installationID,
		WorkspaceID:    workspaceID,
		AgentID:        installation.AgentID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return ErrInstallationNotFound
	} else if err != nil {
		return fmt.Errorf("lock qianwen installation for unbind: %w", err)
	}
	if _, err := qtx.LockActiveMember(ctx, db.LockActiveMemberParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return ErrInstallationNotFound
	} else if err != nil {
		return fmt.Errorf("lock active qianwen member for unbind: %w", err)
	}
	if err := qtx.DeleteQianwenCurrentUserState(ctx, db.DeleteQianwenCurrentUserStateParams{
		InstallationID: installationID,
		MulticaUserID:  userID,
		WorkspaceID:    workspaceID,
	}); err != nil {
		return fmt.Errorf("delete current qianwen user state: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit qianwen unbind: %w", err)
	}
	return nil
}

// MintPairingCode atomically replaces the current code for one active
// installation and authenticated Multica user. PostgreSQL supplies the TTL;
// the plaintext never crosses the query boundary.
func (s *Service) MintPairingCode(ctx context.Context, workspaceID, installationID, userID pgtype.UUID) (PairingCodeResult, error) {
	if len(s.pairingDigestKey) == 0 {
		return PairingCodeResult{}, ErrPairingUnavailable
	}
	for attempt := 0; attempt < pairingCodeAttempts; attempt++ {
		n, err := rand.Int(s.pairingRandom, big.NewInt(pairingCodeSpace))
		if err != nil {
			return PairingCodeResult{}, fmt.Errorf("generate qianwen pairing code: %w", err)
		}
		code := fmt.Sprintf("%08d", n.Int64())
		mac := hmac.New(sha256.New, s.pairingDigestKey)
		_, _ = mac.Write([]byte(pairingCodeMACDomain))
		_, _ = mac.Write(installationID.Bytes[:])
		_, _ = mac.Write([]byte(code))
		row, err := s.q.UpsertQianwenPairingCode(ctx, db.UpsertQianwenPairingCodeParams{
			CodeDigest:     mac.Sum(nil),
			MulticaUserID:  userID,
			InstallationID: installationID,
			WorkspaceID:    workspaceID,
		})
		if isPairingCodeCollision(err) {
			continue
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return PairingCodeResult{}, ErrInstallationNotFound
		}
		if err != nil {
			return PairingCodeResult{}, fmt.Errorf("persist qianwen pairing code: %w", err)
		}
		return PairingCodeResult{Code: code, ExpiresAt: row.ExpiresAt.Time}, nil
	}
	return PairingCodeResult{}, errors.New("qianwen: could not allocate a unique pairing code")
}

func isPairingCodeCollision(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == pairingCodeUniqueIdx
}

// RedeemPairingCode verifies the semantic HMAC, serializes the installation's
// rolling brute-force budget, consumes the one-time spoken code, and binds the
// opaque Qianwen identity to the Multica user selected when the code was
// minted. A short-lived terminal-outcome ledger makes both exact HTTP replays
// and provider retries with fresh timestamp/nonce values idempotent.
func (s *Service) RedeemPairingCode(ctx context.Context, connectionID, token string, request PairingRedeemRequest) (PairingBindingResult, error) {
	if len(s.pairingDigestKey) == 0 || s.dbq == nil || s.tx == nil {
		return PairingBindingResult{}, ErrPairingUnavailable
	}
	if len(request.Code) != 8 || strings.Trim(request.Code, "0123456789") != "" {
		return PairingBindingResult{}, ErrPairingCodeInvalid
	}
	invokedAt, err := verifyPairingRedeemMAC(token, request.Code, request.Identity)
	if err != nil {
		return PairingBindingResult{}, err
	}
	installation, err := s.authenticate(ctx, connectionID, token)
	if err != nil {
		return PairingBindingResult{}, err
	}

	codeDigest := s.pairingCodeDigest(installation.ID, request.Code)
	identityDigest := s.pairingIdentityDigest(installation.ID, request.Identity.OpenUserID, request.Identity.OpenUUID)
	nonceDigest := s.pairingNonceDigest(installation.ID, request.Identity.Timestamp, request.Identity.Nonce)
	requestDigest := s.pairingRequestDigest(installation.ID, request.Code, request.Identity)
	candidate, candidateErr := s.dbq.GetLiveQianwenPairingCode(ctx, db.GetLiveQianwenPairingCodeParams{
		InstallationID: installation.ID,
		CodeDigest:     codeDigest,
	})
	candidateFound := candidateErr == nil
	if candidateErr != nil && !errors.Is(candidateErr, pgx.ErrNoRows) {
		return PairingBindingResult{}, fmt.Errorf("pre-read qianwen pairing code: %w", candidateErr)
	}
	priorOutcome, priorErr := s.dbq.FindCompletedQianwenInvocationByRequestDigest(ctx, db.FindCompletedQianwenInvocationByRequestDigestParams{
		InstallationID: installation.ID,
		RequestDigest:  requestDigest,
	})
	priorTargetFound := priorErr == nil && priorOutcome.Outcome.Valid && priorOutcome.Outcome.String == "paired" && priorOutcome.MulticaUserID.Valid
	if priorErr != nil && !errors.Is(priorErr, pgx.ErrNoRows) {
		return PairingBindingResult{}, fmt.Errorf("pre-read qianwen invocation outcome: %w", priorErr)
	}

	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return PairingBindingResult{}, fmt.Errorf("begin qianwen pairing transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.dbq.WithTx(tx)

	memberIDs := []pgtype.UUID{installation.InstallerUserID}
	if candidateFound && candidate.MulticaUserID != installation.InstallerUserID {
		memberIDs = append(memberIDs, candidate.MulticaUserID)
	}
	if priorTargetFound && !containsUUID(memberIDs, priorOutcome.MulticaUserID) {
		memberIDs = append(memberIDs, priorOutcome.MulticaUserID)
	}
	sort.Slice(memberIDs, func(i, j int) bool {
		return bytes.Compare(memberIDs[i].Bytes[:], memberIDs[j].Bytes[:]) < 0
	})
	for _, userID := range memberIDs {
		if err := qtx.LockSubscriberWrites(ctx, db.LockSubscriberWritesParams{
			WorkspaceID: installation.WorkspaceID,
			UserID:      userID,
		}); err != nil {
			return PairingBindingResult{}, fmt.Errorf("lock qianwen pairing member lifecycle: %w", err)
		}
	}
	if _, err := qtx.LockWorkspaceForChatSessionCreate(ctx, installation.WorkspaceID); errors.Is(err, pgx.ErrNoRows) {
		return PairingBindingResult{}, ErrUnauthorized
	} else if err != nil {
		return PairingBindingResult{}, fmt.Errorf("lock qianwen pairing workspace: %w", err)
	}
	agent, err := qtx.LockAgentForAutopilotAssignment(ctx, db.LockAgentForAutopilotAssignmentParams{
		ID:          installation.AgentID,
		WorkspaceID: installation.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PairingBindingResult{}, ErrPairingAccessDenied
	}
	if err != nil {
		return PairingBindingResult{}, fmt.Errorf("lock qianwen pairing agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return PairingBindingResult{}, ErrPairingAccessDenied
	}

	locked, err := qtx.LockQianwenInstallationForPairing(ctx, db.LockQianwenInstallationForPairingParams{
		InstallationID:  installation.ID,
		WorkspaceID:     installation.WorkspaceID,
		AgentID:         installation.AgentID,
		InstallerUserID: installation.InstallerUserID,
		ConnectionID:    connectionID,
		AccessTokenHash: hashAccessToken(token),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PairingBindingResult{}, ErrUnauthorized
	}
	if err != nil {
		return PairingBindingResult{}, fmt.Errorf("lock qianwen pairing installation: %w", err)
	}
	for _, userID := range memberIDs {
		if _, err := qtx.LockActiveMember(ctx, db.LockActiveMemberParams{
			UserID:      userID,
			WorkspaceID: locked.WorkspaceID,
		}); errors.Is(err, pgx.ErrNoRows) {
			if userID == locked.InstallerUserID {
				return PairingBindingResult{}, ErrUnauthorized
			}
			return PairingBindingResult{}, ErrPairingAccessDenied
		} else if err != nil {
			return PairingBindingResult{}, fmt.Errorf("lock active qianwen pairing member: %w", err)
		}
	}

	existingNonce, err := qtx.GetLiveQianwenInvocationByNonceForUpdate(ctx, db.GetLiveQianwenInvocationByNonceForUpdateParams{
		InstallationID: locked.ID,
		NonceDigest:    nonceDigest,
	})
	if err == nil {
		if !hmac.Equal(existingNonce.RequestDigest, requestDigest) || !existingNonce.Outcome.Valid {
			return PairingBindingResult{}, ErrInvocationReplay
		}
		if existingNonce.Outcome.String == "paired" && !containsUUID(memberIDs, existingNonce.MulticaUserID) {
			return PairingBindingResult{}, errors.New("qianwen: paired outcome appeared after lifecycle fences were selected")
		}
		return pairingOutcome(existingNonce)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PairingBindingResult{}, fmt.Errorf("load qianwen invocation nonce: %w", err)
	}
	existingRequest, err := qtx.FindCompletedQianwenInvocationByRequestDigest(ctx, db.FindCompletedQianwenInvocationByRequestDigestParams{
		InstallationID: locked.ID,
		RequestDigest:  requestDigest,
	})
	if err == nil {
		if existingRequest.Outcome.String == "paired" && !containsUUID(memberIDs, existingRequest.MulticaUserID) {
			return PairingBindingResult{}, errors.New("qianwen: paired outcome appeared after lifecycle fences were selected")
		}
		return pairingOutcome(existingRequest)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PairingBindingResult{}, fmt.Errorf("load qianwen invocation outcome: %w", err)
	}
	if !invocationTimestampFresh(invokedAt, s.now()) {
		return PairingBindingResult{}, ErrStaleInvocation
	}

	counts, err := qtx.GetQianwenPairingAttemptCounts(ctx, db.GetQianwenPairingAttemptCountsParams{
		InstallationID: locked.ID,
		IdentityDigest: identityDigest,
	})
	if err != nil {
		return PairingBindingResult{}, fmt.Errorf("count qianwen pairing failures: %w", err)
	}
	if counts.IdentityFailures >= pairingIdentityLimit || counts.InstallationFailures >= pairingInstallLimit {
		return PairingBindingResult{}, ErrPairingRateLimited
	}

	pairing, err := qtx.GetLiveQianwenPairingCodeForUpdate(ctx, db.GetLiveQianwenPairingCodeForUpdateParams{
		InstallationID: locked.ID,
		CodeDigest:     codeDigest,
	})
	if errors.Is(err, pgx.ErrNoRows) || (candidateFound && err == nil && pairing.MulticaUserID != candidate.MulticaUserID) {
		if err := s.recordInvalidPairing(ctx, qtx, locked.ID, nonceDigest, requestDigest, identityDigest); err != nil {
			return PairingBindingResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return PairingBindingResult{}, fmt.Errorf("commit qianwen pairing failure: %w", err)
		}
		return PairingBindingResult{}, ErrPairingCodeInvalid
	}
	if err != nil {
		return PairingBindingResult{}, fmt.Errorf("load qianwen pairing code: %w", err)
	}
	if !candidateFound || pairing.MulticaUserID != candidate.MulticaUserID {
		return PairingBindingResult{}, ErrPairingCodeInvalid
	}

	allowed, err := qtx.CanQianwenPairingUserInvokeAgent(ctx, db.CanQianwenPairingUserInvokeAgentParams{
		MulticaUserID:  pairing.MulticaUserID,
		InstallationID: locked.ID,
	})
	if err != nil {
		return PairingBindingResult{}, fmt.Errorf("check qianwen pairing agent access: %w", err)
	}
	if !allowed {
		return PairingBindingResult{}, ErrPairingAccessDenied
	}
	bindingConfig, err := json.Marshal(map[string]string{
		"open_uuid":      request.Identity.OpenUUID,
		"identity_scope": "skill",
	})
	if err != nil {
		return PairingBindingResult{}, fmt.Errorf("encode qianwen binding config: %w", err)
	}
	if _, err := qtx.CreateChannelUserBinding(ctx, db.CreateChannelUserBindingParams{
		WorkspaceID:    locked.WorkspaceID,
		MulticaUserID:  pairing.MulticaUserID,
		InstallationID: locked.ID,
		ChannelType:    string(TypeQianwen),
		ChannelUserID:  request.Identity.OpenUserID,
		Config:         bindingConfig,
	}); errors.Is(err, pgx.ErrNoRows) {
		return PairingBindingResult{}, ErrBindingAlreadyAssigned
	} else if err != nil {
		return PairingBindingResult{}, fmt.Errorf("create qianwen user binding: %w", err)
	}
	rows, err := qtx.DeleteQianwenPairingCode(ctx, db.DeleteQianwenPairingCodeParams{
		InstallationID: locked.ID,
		MulticaUserID:  pairing.MulticaUserID,
		CodeDigest:     codeDigest,
	})
	if err != nil {
		return PairingBindingResult{}, fmt.Errorf("consume qianwen pairing code: %w", err)
	}
	if rows != 1 {
		return PairingBindingResult{}, errors.New("qianwen: pairing code consume lost its row lock")
	}
	if err := qtx.DeleteExpiredQianwenInvocationNonces(ctx, locked.ID); err != nil {
		return PairingBindingResult{}, fmt.Errorf("prune qianwen invocation nonces: %w", err)
	}
	if err := qtx.DeleteExpiredQianwenPairingAttempts(ctx, locked.ID); err != nil {
		return PairingBindingResult{}, fmt.Errorf("prune qianwen pairing attempts: %w", err)
	}
	if _, err := qtx.InsertQianwenInvocationNonce(ctx, db.InsertQianwenInvocationNonceParams{
		InstallationID: locked.ID,
		NonceDigest:    nonceDigest,
		RequestDigest:  requestDigest,
	}); err != nil {
		return PairingBindingResult{}, fmt.Errorf("persist qianwen paired invocation: %w", err)
	}
	if _, err := qtx.CompleteQianwenInvocationNonce(ctx, db.CompleteQianwenInvocationNonceParams{
		Outcome:        pgtype.Text{String: "paired", Valid: true},
		MulticaUserID:  pairing.MulticaUserID,
		InstallationID: locked.ID,
		NonceDigest:    nonceDigest,
		RequestDigest:  requestDigest,
	}); err != nil {
		return PairingBindingResult{}, fmt.Errorf("complete qianwen paired invocation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PairingBindingResult{}, fmt.Errorf("commit qianwen pairing: %w", err)
	}
	return PairingBindingResult{InstallationID: locked.ID, MulticaUserID: pairing.MulticaUserID}, nil
}

func containsUUID(values []pgtype.UUID, target pgtype.UUID) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *Service) recordInvalidPairing(ctx context.Context, qtx *db.Queries, installationID pgtype.UUID, nonceDigest, requestDigest, identityDigest []byte) error {
	if err := qtx.DeleteExpiredQianwenInvocationNonces(ctx, installationID); err != nil {
		return fmt.Errorf("prune qianwen invocation nonces: %w", err)
	}
	if err := qtx.DeleteExpiredQianwenPairingAttempts(ctx, installationID); err != nil {
		return fmt.Errorf("prune qianwen pairing attempts: %w", err)
	}
	if _, err := qtx.InsertQianwenInvocationNonce(ctx, db.InsertQianwenInvocationNonceParams{
		InstallationID: installationID,
		NonceDigest:    nonceDigest,
		RequestDigest:  requestDigest,
	}); err != nil {
		return fmt.Errorf("persist invalid qianwen invocation: %w", err)
	}
	if _, err := qtx.InsertQianwenPairingFailure(ctx, db.InsertQianwenPairingFailureParams{
		InstallationID: installationID,
		IdentityDigest: identityDigest,
	}); err != nil {
		return fmt.Errorf("record qianwen pairing failure: %w", err)
	}
	if _, err := qtx.CompleteQianwenInvocationNonce(ctx, db.CompleteQianwenInvocationNonceParams{
		Outcome:        pgtype.Text{String: "code_invalid", Valid: true},
		MulticaUserID:  pgtype.UUID{},
		InstallationID: installationID,
		NonceDigest:    nonceDigest,
		RequestDigest:  requestDigest,
	}); err != nil {
		return fmt.Errorf("complete invalid qianwen invocation: %w", err)
	}
	return nil
}

func pairingOutcome(row db.QianwenInvocationNonce) (PairingBindingResult, error) {
	if !row.Outcome.Valid {
		return PairingBindingResult{}, ErrInvocationReplay
	}
	switch row.Outcome.String {
	case "paired":
		if !row.MulticaUserID.Valid {
			return PairingBindingResult{}, errors.New("qianwen: paired invocation is missing its user")
		}
		return PairingBindingResult{InstallationID: row.InstallationID, MulticaUserID: row.MulticaUserID}, nil
	case "code_invalid":
		return PairingBindingResult{}, ErrPairingCodeInvalid
	default:
		return PairingBindingResult{}, errors.New("qianwen: invocation has an unknown terminal outcome")
	}
}

func (s *Service) pairingCodeDigest(installationID pgtype.UUID, code string) []byte {
	mac := hmac.New(sha256.New, s.pairingDigestKey)
	_, _ = mac.Write([]byte(pairingCodeMACDomain))
	_, _ = mac.Write(installationID.Bytes[:])
	_, _ = mac.Write([]byte(code))
	return mac.Sum(nil)
}

func (s *Service) pairingIdentityDigest(installationID pgtype.UUID, openUserID, openUUID string) []byte {
	mac := hmac.New(sha256.New, s.pairingDigestKey)
	_, _ = mac.Write([]byte(pairingIdentityDomain))
	_, _ = mac.Write(installationID.Bytes[:])
	_, _ = mac.Write([]byte(openUserID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(openUUID))
	return mac.Sum(nil)
}

func (s *Service) pairingNonceDigest(installationID pgtype.UUID, timestamp, nonce string) []byte {
	mac := hmac.New(sha256.New, s.pairingDigestKey)
	_, _ = mac.Write([]byte(pairingNonceDomain))
	_, _ = mac.Write(installationID.Bytes[:])
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(nonce))
	return mac.Sum(nil)
}

func (s *Service) pairingRequestDigest(installationID pgtype.UUID, code string, identity InvocationMetadata) []byte {
	mac := hmac.New(sha256.New, s.pairingDigestKey)
	_, _ = mac.Write([]byte(pairingRequestDomain))
	_, _ = mac.Write(installationID.Bytes[:])
	_, _ = mac.Write([]byte(identity.OpenUserID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(identity.OpenUUID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(code))
	return mac.Sum(nil)
}

// Submit authenticates one private Skill request, claims its durable
// idempotency row, and atomically creates the task, channel-ingested user
// message, and final ledger pointer. The HTTP context bounds every synchronous
// operation so Qianwen's three-second tool deadline remains enforceable.
func (s *Service) Submit(ctx context.Context, connectionID, token string, invocation SubmitInvocation) (SubmitResult, error) {
	requestID, query, err := normalizeSubmit(invocation.Request)
	if err != nil {
		return SubmitResult{}, err
	}
	if err := VerifySubmitInvocationSignature(token, invocation, s.now()); err != nil {
		return SubmitResult{}, err
	}
	installation, err := s.authenticate(ctx, connectionID, token)
	if err != nil {
		return SubmitResult{}, err
	}
	boundUserID, err := s.resolveInvocationUser(ctx, installation, connectionID, token, invocation.Identity)
	if err != nil {
		return SubmitResult{}, err
	}
	requestUUID := util.MustParseUUID(requestID)
	queryHash := sha256.Sum256([]byte(query))
	accessTokenHash := hashAccessToken(token)
	claimed, ownsClaim, err := s.claimRequest(ctx, installation, boundUserID, connectionID, token, accessTokenHash, requestUUID, queryHash[:], invocation.Identity)
	if err != nil {
		return SubmitResult{}, err
	}
	if !ownsClaim {
		return SubmitResult{RequestID: requestID, Status: "accepted"}, nil
	}

	claimPublished := false
	defer func() {
		if claimPublished {
			return
		}
		// The request context may already be cancelled when task creation fails.
		// Release with a short detached context; the fencing token makes this a
		// no-op if a newer owner has already reclaimed the row.
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claimReleaseTimeout)
		defer cancel()
		if _, releaseErr := s.q.ReleaseQianwenRequestClaim(releaseCtx, db.ReleaseQianwenRequestClaimParams{
			InstallationID: installation.ID,
			RequestID:      requestUUID,
			MulticaUserID:  boundUserID,
			ClaimToken:     claimed.ClaimToken,
		}); releaseErr != nil {
			slog.Warn("release qianwen request claim failed", "request_id", requestID, "error", releaseErr)
		}
	}()

	sessionID, err := s.sessions.EnsureSession(ctx, engine.EnsureSessionInput{
		WorkspaceID:    installation.WorkspaceID,
		AgentID:        installation.AgentID,
		InstallationID: installation.ID,
		Sender:         boundUserID,
		BindingKey:     requestID,
		ChatType:       channel.ChatTypeP2P,
		Title:          deriveQianwenSessionTitle(query),
	})
	if err != nil {
		return SubmitResult{}, fmt.Errorf("ensure qianwen request session: %w", err)
	}
	updated, err := s.q.SetQianwenRequestSession(ctx, db.SetQianwenRequestSessionParams{
		ChatSessionID:  sessionID,
		InstallationID: installation.ID,
		RequestID:      requestUUID,
		MulticaUserID:  boundUserID,
		ClaimToken:     claimed.ClaimToken,
	})
	if err != nil {
		return SubmitResult{}, fmt.Errorf("persist qianwen request session: %w", err)
	}
	if updated != 1 {
		return SubmitResult{}, ErrTaskNotQueued
	}

	session, err := s.q.GetChatSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SubmitResult{}, ErrTaskNotQueued
		}
		return SubmitResult{}, fmt.Errorf("load qianwen request session: %w", err)
	}
	if session.WorkspaceID != installation.WorkspaceID || session.AgentID != installation.AgentID || session.CreatorID != boundUserID {
		return SubmitResult{}, errors.New("qianwen: request session does not belong to the bound identity")
	}
	agent, err := s.q.GetAgent(ctx, installation.AgentID)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("load qianwen request agent: %w", err)
	}

	if agent.WorkspaceID != installation.WorkspaceID || agent.ID != installation.AgentID || agent.ArchivedAt.Valid {
		return SubmitResult{}, ErrPairingAccessDenied
	}

	_, err = s.tasks.SendChannelDirectChatMessage(ctx, session, agent, boundUserID, query,
		func(finalizeCtx context.Context, qtx *db.Queries, task db.AgentTaskQueue) error {
			if _, authorityErr := qtx.LockQianwenSubmitAuthority(finalizeCtx, db.LockQianwenSubmitAuthorityParams{
				InstallationID:  installation.ID,
				WorkspaceID:     installation.WorkspaceID,
				AgentID:         installation.AgentID,
				MulticaUserID:   boundUserID,
				ConnectionID:    connectionID,
				AccessTokenHash: accessTokenHash,
				OpenUserID:      invocation.Identity.OpenUserID,
				OpenUuid:        invocation.Identity.OpenUUID,
			}); authorityErr != nil {
				if errors.Is(authorityErr, pgx.ErrNoRows) {
					return ErrUnauthorized
				}
				return fmt.Errorf("revalidate qianwen submit authority: %w", authorityErr)
			}
			rows, completeErr := qtx.CompleteQianwenRequest(finalizeCtx, db.CompleteQianwenRequestParams{
				TaskID:         task.ID,
				InstallationID: installation.ID,
				RequestID:      requestUUID,
				MulticaUserID:  boundUserID,
				ClaimToken:     claimed.ClaimToken,
			})
			if completeErr != nil {
				return fmt.Errorf("complete request ledger: %w", completeErr)
			}
			if rows != 1 {
				return errors.New("request claim was lost before task commit")
			}
			return nil
		})
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return SubmitResult{}, ErrUnauthorized
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return SubmitResult{}, err
		}
		// Keep internal task/runtime details on the server. The public handler
		// tells the Skill only that retrying this request id is safe.
		return SubmitResult{}, fmt.Errorf("%w: %v", ErrTaskNotQueued, err)
	}
	claimPublished = true
	return SubmitResult{RequestID: requestID, Status: "accepted"}, nil
}

func (s *Service) claimRequest(ctx context.Context, installation db.ChannelInstallation, boundUserID pgtype.UUID, connectionID, token, accessTokenHash string, requestID pgtype.UUID, queryHash []byte, identity InvocationMetadata) (db.QianwenSkillRequest, bool, error) {
	row, err := s.q.ClaimQianwenRequest(ctx, db.ClaimQianwenRequestParams{
		RequestID:       requestID,
		QuerySha256:     queryHash,
		AgentID:         installation.AgentID,
		InstallationID:  installation.ID,
		WorkspaceID:     installation.WorkspaceID,
		MulticaUserID:   boundUserID,
		ConnectionID:    connectionID,
		AccessTokenHash: accessTokenHash,
		OpenUserID:      identity.OpenUserID,
		OpenUuid:        identity.OpenUUID,
	})
	if err == nil {
		return row, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.QianwenSkillRequest{}, false, fmt.Errorf("claim qianwen request: %w", err)
	}
	current, authErr := s.authenticate(ctx, connectionID, token)
	if authErr != nil || current.ID != installation.ID {
		if authErr != nil && !errors.Is(authErr, ErrUnauthorized) {
			return db.QianwenSkillRequest{}, false, authErr
		}
		return db.QianwenSkillRequest{}, false, ErrUnauthorized
	}
	currentUserID, bindingErr := s.resolveInvocationUser(ctx, current, connectionID, token, identity)
	if bindingErr != nil {
		return db.QianwenSkillRequest{}, false, bindingErr
	}
	if currentUserID != boundUserID {
		return db.QianwenSkillRequest{}, false, ErrRequestConflict
	}

	existing, err := s.q.GetQianwenRequest(ctx, db.GetQianwenRequestParams{
		InstallationID: installation.ID,
		RequestID:      requestID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.QianwenSkillRequest{}, false, ErrUnauthorized
	}
	if err != nil {
		return db.QianwenSkillRequest{}, false, fmt.Errorf("load claimed qianwen request: %w", err)
	}
	if existing.MulticaUserID != boundUserID || !bytes.Equal(existing.QuerySha256, queryHash) {
		return db.QianwenSkillRequest{}, false, ErrRequestConflict
	}
	// Either a task is already durable or another request owner holds the
	// active DB-clock lease. Both are accepted idempotent replays; status polling
	// observes the eventual task without starting a competing run.
	return existing, false, nil
}

func (s *Service) Status(ctx context.Context, connectionID, token string, invocation StatusInvocation) (RequestStatus, error) {
	requestID, err := normalizeRequestID(invocation.RequestID)
	if err != nil {
		return RequestStatus{}, err
	}
	if err := VerifyStatusInvocationSignature(token, invocation, s.now()); err != nil {
		return RequestStatus{}, err
	}
	installation, err := s.authenticate(ctx, connectionID, token)
	if err != nil {
		return RequestStatus{}, err
	}
	boundUserID, err := s.resolveInvocationUser(ctx, installation, connectionID, token, invocation.Identity)
	if err != nil {
		if errors.Is(err, ErrPairingAccessDenied) || errors.Is(err, ErrIdentityUnavailable) {
			return RequestStatus{}, ErrRequestNotFound
		}
		return RequestStatus{}, err
	}
	row, err := s.q.GetQianwenRequestStatus(ctx, db.GetQianwenRequestStatusParams{
		ConnectionID:    connectionID,
		AccessTokenHash: hashAccessToken(token),
		InstallationID:  installation.ID,
		RequestID:       util.MustParseUUID(requestID),
		MulticaUserID:   boundUserID,
		OpenUserID:      invocation.Identity.OpenUserID,
		OpenUuid:        invocation.Identity.OpenUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RequestStatus{}, ErrRequestNotFound
	}
	if err != nil {
		return RequestStatus{}, fmt.Errorf("load qianwen request status: %w", err)
	}
	return s.mapStatus(requestID, row), nil
}

func (s *Service) resolveInvocationUser(ctx context.Context, installation db.ChannelInstallation, connectionID, token string, identity InvocationMetadata) (pgtype.UUID, error) {
	userID, err := s.q.GetActiveQianwenInvocationUser(ctx, db.GetActiveQianwenInvocationUserParams{
		InstallationID:  installation.ID,
		ConnectionID:    connectionID,
		AccessTokenHash: hashAccessToken(token),
		OpenUserID:      identity.OpenUserID,
		OpenUuid:        identity.OpenUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, ErrPairingAccessDenied
	}
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("resolve qianwen invocation identity: %w", err)
	}
	return userID, nil
}

func (s *Service) authenticate(ctx context.Context, connectionID, token string) (db.ChannelInstallation, error) {
	if !ValidCredentialShape(connectionID, token) {
		return db.ChannelInstallation{}, ErrUnauthorized
	}
	row, err := s.q.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: string(TypeQianwen),
		AppID:       connectionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelInstallation{}, ErrUnauthorized
	}
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("load qianwen installation: %w", err)
	}
	if row.Status != "active" || !verifyAccessToken(row.Config, token) {
		return db.ChannelInstallation{}, ErrUnauthorized
	}
	if _, err := s.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      row.InstallerUserID,
		WorkspaceID: row.WorkspaceID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelInstallation{}, ErrUnauthorized
	} else if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("verify qianwen installer membership: %w", err)
	}
	return row, nil
}

func normalizeSubmit(req SubmitRequest) (requestID, query string, err error) {
	requestID, err = normalizeRequestID(req.RequestID)
	if err != nil {
		return "", "", err
	}
	query = strings.TrimSpace(req.Query)
	if query == "" {
		return "", "", fmt.Errorf("%w: query is required", ErrInvalidRequest)
	}
	if len(query) > maxQueryBytes || !utf8.ValidString(query) {
		return "", "", fmt.Errorf("%w: query must be valid UTF-8 and at most %d bytes", ErrInvalidRequest, maxQueryBytes)
	}
	if _, ok := engine.ParseIssueCommand(query); ok {
		return "", "", fmt.Errorf("%w: /issue is not supported by the polling bridge", ErrUnsupportedCommand)
	}
	if _, ok := engine.ParseFreshSessionCommand(query); ok {
		return "", "", fmt.Errorf("%w: /new is unnecessary because every polling request starts a fresh session", ErrUnsupportedCommand)
	}
	return requestID, query, nil
}

func normalizeRequestID(raw string) (string, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id == uuid.Nil {
		return "", fmt.Errorf("%w: request_id must be a UUID", ErrInvalidRequest)
	}
	return id.String(), nil
}

func (s *Service) mapStatus(requestID string, row db.GetQianwenRequestStatusRow) RequestStatus {
	out := RequestStatus{RequestID: requestID}
	if !row.TaskID.Valid {
		out.Status = "accepted"
		if !row.ClaimActive {
			out.Status = "failed"
			out.Message = "The request has no durable task. Retry it with the same request_id or inspect the agent in Multica."
		}
		return out
	}
	out.TaskID = util.UUIDToString(row.TaskID)
	switch row.TaskStatus {
	case "queued", "deferred":
		out.Status = "queued"
	case "dispatched", "running", "waiting_local_directory":
		out.Status = "running"
	case "completed":
		out.Status = "completed"
		out.Output, out.Truncated = truncateRunes(row.Output, maxStatusOutputRunes)
	case "failed":
		out.Status = "failed"
		out.Message = "The task failed. Open Multica to inspect the execution details."
	case "cancelled":
		out.Status = "cancelled"
	default:
		out.Status = "unknown"
		out.Message = "The task is in an unrecognized state. Open Multica to inspect it."
	}
	return out
}

func truncateRunes(value string, limit int) (string, bool) {
	if limit <= 0 {
		return "", value != ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value, false
	}
	return string(runes[:limit]) + "…", true
}
