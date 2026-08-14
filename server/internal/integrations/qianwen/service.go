package qianwen

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	taskservice "github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	maxQueryBytes        = 16 * 1024
	maxStatusOutputRunes = 8_000
	claimReleaseTimeout  = 100 * time.Millisecond
)

var (
	ErrUnauthorized         = errors.New("qianwen: invalid connection credentials")
	ErrInstallationNotFound = errors.New("qianwen: installation not found")
	ErrRequestNotFound      = errors.New("qianwen: request not found")
	ErrRequestConflict      = errors.New("qianwen: request_id was already used for a different query")
	ErrInvalidRequest       = errors.New("qianwen: invalid request")
	ErrUnsupportedCommand   = errors.New("qianwen: unsupported channel command")
	ErrTaskNotQueued        = errors.New("qianwen: request stored but task not queued")
)

// SubmitRequest is the stable request body exposed to a Qianwen Skill tool.
// request_id must be a caller-generated UUID so Qianwen retries are idempotent.
type SubmitRequest struct {
	RequestID string `json:"request_id"`
	Query     string `json:"query"`
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

// InstallationResult returns the one-time plaintext token after install or
// rotation. The token is never recoverable from the database later.
type InstallationResult struct {
	Installation db.ChannelInstallation
	ConnectionID string
	AccessToken  string
}

type serviceQueries interface {
	UpsertChannelInstallation(context.Context, db.UpsertChannelInstallationParams) (db.ChannelInstallation, error)
	ListChannelInstallationsByWorkspace(context.Context, db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error)
	GetChannelInstallationInWorkspace(context.Context, db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error)
	GetChannelInstallationByAppID(context.Context, db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error)
	SetChannelInstallationStatus(context.Context, db.SetChannelInstallationStatusParams) error
	GetMemberByUserAndWorkspace(context.Context, db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
	ClaimQianwenRequest(context.Context, db.ClaimQianwenRequestParams) (db.QianwenSkillRequest, error)
	GetQianwenRequest(context.Context, db.GetQianwenRequestParams) (db.QianwenSkillRequest, error)
	SetQianwenRequestSession(context.Context, db.SetQianwenRequestSessionParams) (int64, error)
	ReleaseQianwenRequestClaim(context.Context, db.ReleaseQianwenRequestClaimParams) (int64, error)
	GetChatSession(context.Context, pgtype.UUID) (db.ChatSession, error)
	GetAgent(context.Context, pgtype.UUID) (db.Agent, error)
	GetQianwenRequestStatus(context.Context, db.GetQianwenRequestStatusParams) (db.GetQianwenRequestStatusRow, error)
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
	q        serviceQueries
	sessions RequestSessionEnsurer
	tasks    ChannelTaskSender
}

func NewService(q *db.Queries, sessions RequestSessionEnsurer, tasks ChannelTaskSender) (*Service, error) {
	return newService(q, sessions, tasks)
}

func newService(q serviceQueries, sessions RequestSessionEnsurer, tasks ChannelTaskSender) (*Service, error) {
	if q == nil {
		return nil, errors.New("qianwen: service requires queries")
	}
	if sessions == nil {
		return nil, errors.New("qianwen: service requires a chat session service")
	}
	if tasks == nil {
		return nil, errors.New("qianwen: service requires an atomic channel task sender")
	}
	return &Service{q: q, sessions: sessions, tasks: tasks}, nil
}

// InstallPersonal creates or rotates the private Skill connection for an
// agent. Upsert preserves the installation id (and therefore old request
// sessions) while replacing the public connection id and token digest.
func (s *Service) InstallPersonal(ctx context.Context, workspaceID, agentID, installerID pgtype.UUID) (InstallationResult, error) {
	connectionID, token, err := generateCredentials()
	if err != nil {
		return InstallationResult{}, err
	}
	config, err := encodeInstallConfig(connectionID, token)
	if err != nil {
		return InstallationResult{}, fmt.Errorf("encode installation config: %w", err)
	}
	row, err := s.q.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{
		WorkspaceID:     workspaceID,
		AgentID:         agentID,
		ChannelType:     string(TypeQianwen),
		Config:          config,
		InstallerUserID: installerID,
	})
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

func (s *Service) Revoke(ctx context.Context, id pgtype.UUID) error {
	return s.q.SetChannelInstallationStatus(ctx, db.SetChannelInstallationStatusParams{ID: id, Status: "revoked"})
}

// Submit authenticates one private Skill request, claims its durable
// idempotency row, and atomically creates the task, channel-ingested user
// message, and final ledger pointer. The HTTP context bounds every synchronous
// operation so Qianwen's three-second tool deadline remains enforceable.
func (s *Service) Submit(ctx context.Context, connectionID, token string, req SubmitRequest) (SubmitResult, error) {
	installation, err := s.authenticate(ctx, connectionID, token)
	if err != nil {
		return SubmitResult{}, err
	}
	requestID, query, err := normalizeSubmit(req)
	if err != nil {
		return SubmitResult{}, err
	}
	requestUUID := util.MustParseUUID(requestID)
	queryHash := sha256.Sum256([]byte(query))
	accessTokenHash := hashAccessToken(token)
	claimed, ownsClaim, err := s.claimRequest(ctx, installation, connectionID, token, accessTokenHash, requestUUID, queryHash[:])
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
			ClaimToken:     claimed.ClaimToken,
		}); releaseErr != nil {
			slog.Warn("release qianwen request claim failed", "request_id", requestID, "error", releaseErr)
		}
	}()

	sessionID, err := s.sessions.EnsureSession(ctx, engine.EnsureSessionInput{
		WorkspaceID:    installation.WorkspaceID,
		AgentID:        installation.AgentID,
		InstallationID: installation.ID,
		Sender:         installation.InstallerUserID,
		BindingKey:     requestID,
		ChatType:       channel.ChatTypeP2P,
	})
	if err != nil {
		return SubmitResult{}, fmt.Errorf("ensure qianwen request session: %w", err)
	}
	updated, err := s.q.SetQianwenRequestSession(ctx, db.SetQianwenRequestSessionParams{
		ChatSessionID:  sessionID,
		InstallationID: installation.ID,
		RequestID:      requestUUID,
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
	agent, err := s.q.GetAgent(ctx, installation.AgentID)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("load qianwen request agent: %w", err)
	}

	_, err = s.tasks.SendChannelDirectChatMessage(ctx, session, agent, installation.InstallerUserID, query,
		func(finalizeCtx context.Context, qtx *db.Queries, task db.AgentTaskQueue) error {
			if _, authorityErr := qtx.LockQianwenSubmitAuthority(finalizeCtx, db.LockQianwenSubmitAuthorityParams{
				InstallationID:  installation.ID,
				WorkspaceID:     installation.WorkspaceID,
				AgentID:         installation.AgentID,
				InstallerUserID: installation.InstallerUserID,
				ConnectionID:    connectionID,
				AccessTokenHash: accessTokenHash,
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

func (s *Service) claimRequest(ctx context.Context, installation db.ChannelInstallation, connectionID, token, accessTokenHash string, requestID pgtype.UUID, queryHash []byte) (db.QianwenSkillRequest, bool, error) {
	row, err := s.q.ClaimQianwenRequest(ctx, db.ClaimQianwenRequestParams{
		RequestID:       requestID,
		QuerySha256:     queryHash,
		AgentID:         installation.AgentID,
		InstallationID:  installation.ID,
		WorkspaceID:     installation.WorkspaceID,
		InstallerUserID: installation.InstallerUserID,
		ConnectionID:    connectionID,
		AccessTokenHash: accessTokenHash,
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
	if !bytes.Equal(existing.QuerySha256, queryHash) {
		return db.QianwenSkillRequest{}, false, ErrRequestConflict
	}
	// Either a task is already durable or another request owner holds the
	// active DB-clock lease. Both are accepted idempotent replays; status polling
	// observes the eventual task without starting a competing run.
	return existing, false, nil
}

func (s *Service) Status(ctx context.Context, connectionID, token, rawRequestID string) (RequestStatus, error) {
	installation, err := s.authenticate(ctx, connectionID, token)
	if err != nil {
		return RequestStatus{}, err
	}
	requestID, err := normalizeRequestID(rawRequestID)
	if err != nil {
		return RequestStatus{}, err
	}
	row, err := s.q.GetQianwenRequestStatus(ctx, db.GetQianwenRequestStatusParams{
		ConnectionID:    connectionID,
		AccessTokenHash: hashAccessToken(token),
		InstallationID:  installation.ID,
		RequestID:       util.MustParseUUID(requestID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RequestStatus{}, ErrRequestNotFound
	}
	if err != nil {
		return RequestStatus{}, fmt.Errorf("load qianwen request status: %w", err)
	}
	return s.mapStatus(requestID, row), nil
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
