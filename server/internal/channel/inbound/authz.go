package inbound

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

// AuthzErrCode classifies why the authz step rejected an event. The
// dispatcher (or the pipeline caller) may use the code to select a
// platform-specific reply template; the Reply field on AuthzError
// already carries the default human-readable message.
type AuthzErrCode string

const (
	// AuthzWsNotBound means the chat has no row in channel_chat_binding,
	// so we cannot determine which workspace the event belongs to.
	AuthzWsNotBound AuthzErrCode = "WS_NOT_BOUND"

	// AuthzNotMember means the sender is not a member of the workspace
	// that the chat is bound to. The reply MUST NOT include a binding
	// link — strangers must not be able to fish for invite tokens
	// (TC-authz-2, QA AC7.1 "保护新人入群骚扰路径").
	AuthzNotMember AuthzErrCode = "NOT_MEMBER"

	// AuthzIdentityUnresolved means the sender's Multica user identity
	// could not be resolved from the external channel identity. This
	// occurs when identity-bind (T8) has not yet wired the
	// channel_user_binding lookup. Fail-closed: treat as not-a-member.
	AuthzIdentityUnresolved AuthzErrCode = "IDENTITY_UNRESOLVED"

	// AuthzNoPermission means the sender is a workspace member but does
	// not hold the specific permission required for the requested action
	// (e.g. changing someone else's issue status, TC-authz-3 AC7.2).
	AuthzNoPermission AuthzErrCode = "NO_PERMISSION"

	// AuthzUnsupportedDelete means the intent is a delete operation.
	// Delete is blocked at the intent-mapping layer (T9 maps it to
	// Unsupported) and again here as a defence-in-depth measure
	// (TC-authz-4, AC7.3). The reply directs the user to the Web UI.
	AuthzUnsupportedDelete AuthzErrCode = "UNSUPPORTED_DELETE"

	AuthzPrivateUnsupported AuthzErrCode = "PRIVATE_UNSUPPORTED"
)

// replyTemplates centralises the user-facing rejection messages. Tests
// assert against these exact strings so QA can verify no binding link
// leaks to strangers (TC-authz-2).
var replyTemplates = map[AuthzErrCode]string{
	AuthzWsNotBound:         "[WS_NOT_BOUND] 当前群尚未绑定工作区，请先完成绑定。",
	AuthzNotMember:          "[NOT_MEMBER] 你不是当前工作区的成员，无法执行操作。",
	AuthzIdentityUnresolved: "[IDENTITY_UNRESOLVED] 无法识别发送者身份，请稍后重试。",
	AuthzNoPermission:       "[NO_PERMISSION] 你没有权限修改此 Issue 的状态。",
	AuthzUnsupportedDelete:  "[UNSUPPORTED_DELETE] 删除操作不支持在群内执行，请回 Web 端操作。",
	AuthzPrivateUnsupported: "[PRIVATE_UNSUPPORTED] 私聊仅用于账号绑定和系统提示，请在已绑定群里处理业务。",
}

// AuthzError is returned by the authz step when the event is rejected.
// It carries a machine-readable Code and a human-readable Reply that
// the pipeline caller should deliver to the originating chat. The
// error implements the ReplySender interface so callers can extract the
// reply without type-asserting to *AuthzError directly.
type AuthzError struct {
	Code  AuthzErrCode
	Reply string
}

func (e *AuthzError) Error() string {
	return fmt.Sprintf("authz: %s", e.Code)
}

// GetReply implements ReplySender so the pipeline can extract the reply
// text without knowing the concrete error type.
func (e *AuthzError) GetReply() string { return e.Reply }

// ReplySender is the interface the pipeline uses to extract a reply
// message from an error. Steps that reject events with a user-visible
// message should implement this interface on their error type.
type ReplySender interface {
	GetReply() string
}

// AuthzStore is the persistence contract the authz Step depends on.
// Production wires an adapter backed by sqlc-generated queries; tests
// pass a fake satisfying AuthzStore directly. The interface is narrow
// so the authz Step stays free of pgx / sqlc types.
type AuthzStore interface {
	// LookupWorkspaceID returns the workspace_id bound to the given
	// (connectionID, chat_id) pair for this chat's channel_chat_binding row.
	// connectionID is the configured channel connection id used by binding
	// tables and the registry. If no binding exists it returns pgx.ErrNoRows.
	LookupWorkspaceID(ctx context.Context, connectionID, chatID string) (pgtype.UUID, error)

	// LookupPrimaryWorkspaceID returns the workspace_id of the PRIMARY
	// binding for the given (connectionID, chat_id) pair. Optional; callers
	// that need primary-only semantics may use it. Inbound authz uses
	// LookupWorkspaceID so non-primary chat bindings still process commands.
	LookupPrimaryWorkspaceID(ctx context.Context, connectionID, chatID string) (pgtype.UUID, error)

	// IsWorkspaceMember returns true if (userID, workspaceID) exists in
	// the member table. The caller must ensure userID.Valid == true;
	// passing an invalid UUID is a programming error.
	IsWorkspaceMember(ctx context.Context, userID, workspaceID pgtype.UUID) (bool, error)

	ResolveUserID(ctx context.Context, connectionID, externalUserID string) (pgtype.UUID, error)

	// CheckIssuePermission returns nil if the user is allowed to perform
	// the requested action on the issue identified by (workspaceID,
	// issueKey). It returns an error if the issue is not found or the
	// user lacks permission.
	CheckIssuePermission(ctx context.Context, workspaceID, userID pgtype.UUID, issueKey string) error
}

// AuthzConfig holds the dependencies for the authz Step. All fields
// are required unless noted otherwise.
type AuthzConfig struct {
	Store     AuthzStore
	ReplySink ChannelReplySink
	// SendReplies controls whether the authz step sends the rejection
	// reply directly via the channel gateway. When false (e.g. in
	// tests), the step only returns the error and the caller is
	// responsible for delivering the reply.
	SendReplies bool
	// RejectAsSkip makes authz rejections terminate the pipeline cleanly
	// after sending the reply. Tests keep the default false value so they
	// can assert the concrete AuthzError.
	RejectAsSkip bool
}

// authzStep is the Step that enforces authorization policies on every
// inbound event. It sits in the pipeline after intent-recog (so it can
// inspect IntentKind) and before dispatch (so it can block unauthorised
// actions before they reach the facade layer).
//
// Checks performed (in order):
//  1. Chat binding — the chat must be bound to a workspace.
//  2. Identity resolution — the sender's Multica user ID must be
//     resolvable. Fail-closed when identity-bind (T8) is not wired.
//  3. Workspace membership — the sender must be a workspace member.
//  4. Delete rejection — delete intents are always blocked.
//  5. Issue permission — status-change intents verify the sender has
//     permission on the target issue. Missing issue_key is rejected.
type authzStep struct {
	cfg AuthzConfig
}

// NewAuthzStep returns the authz Step wired with the given config.
func NewAuthzStep(cfg AuthzConfig) Step {
	return &authzStep{cfg: cfg}
}

func (*authzStep) Name() string { return "authz" }

func (s *authzStep) Run(ctx context.Context, evt port.InboundEvent) (port.InboundEvent, Decision, error) {
	if evt.Type == port.EventTypeMessageRecalled {
		return evt, DecisionContinue, nil
	}
	if evt.ChatType == port.ChatTypeDirect {
		// Production direct chats are stopped by direct-chat-policy before
		// authz. Keep this bypass as defence against duplicate rejection if a
		// test or custom pipeline still invokes authz directly.
		return evt, DecisionContinue, nil
	}

	// --- 1. Chat binding (TC-authz-1) ---
	// Use LookupWorkspaceID so any binding for this chat grants workspace context.
	wsID, err := s.cfg.Store.LookupWorkspaceID(ctx, evt.ConnectionID(), evt.ChatID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.reject(ctx, evt, AuthzWsNotBound)
		}
		return evt, DecisionContinue, fmt.Errorf("authz: lookup workspace: %w", err)
	}

	// --- 2. Identity resolution (fail-closed) ---
	userID, err := s.cfg.Store.ResolveUserID(ctx, evt.ConnectionID(), evt.SenderID)
	if err != nil || !userID.Valid {
		return s.reject(ctx, evt, AuthzIdentityUnresolved)
	}

	// --- 3. Workspace membership (TC-authz-2) ---
	isMember, err := s.cfg.Store.IsWorkspaceMember(ctx, userID, wsID)
	if err != nil {
		return evt, DecisionContinue, fmt.Errorf("authz: check membership: %w", err)
	}
	if !isMember {
		return s.reject(ctx, evt, AuthzNotMember)
	}

	// --- 4. Delete rejection (TC-authz-4) ---
	// Defence-in-depth: T9 already maps delete commands to
	// IntentUnsupported, but we reject IntentDelete here as well in
	// case a future intent source (e.g. chat semantic fallback) emits
	// IntentDelete directly.
	if evt.Intent.Kind == port.IntentDelete {
		return s.reject(ctx, evt, AuthzUnsupportedDelete)
	}

	// --- 5. Issue permission (TC-authz-3) ---
	if evt.Intent.Kind == port.IntentSetStatus {
		issueKey := evt.Intent.Params["issue_key"]
		if issueKey == "" {
			// Fail-closed: SetStatus without issue_key is either an
			// upstream bug or a malicious payload. Reject explicitly.
			return s.reject(ctx, evt, AuthzNoPermission)
		}
		if err := s.cfg.Store.CheckIssuePermission(ctx, wsID, userID, issueKey); err != nil {
			return s.reject(ctx, evt, AuthzNoPermission)
		}
	}

	return evt, DecisionContinue, nil
}

func (s *authzStep) reject(ctx context.Context, evt port.InboundEvent, code AuthzErrCode) (port.InboundEvent, Decision, error) {
	authzErr := &AuthzError{
		Code:  code,
		Reply: replyTemplates[code],
	}
	s.maybeSendReply(ctx, evt, authzErr.Reply)
	if s.cfg.RejectAsSkip {
		return evt, DecisionSkip, nil
	}
	return evt, DecisionContinue, authzErr
}

// maybeSendReply delivers the reply text to the originating chat when
// SendReplies is enabled and the reply sink is available. Errors are
// logged at Warn level but not propagated because the authz rejection takes
// precedence.
func (s *authzStep) maybeSendReply(ctx context.Context, evt port.InboundEvent, text string) {
	if !s.cfg.SendReplies || s.cfg.ReplySink == nil {
		return
	}
	target := port.TargetChat(evt.ChatID)
	if evt.ChatType == port.ChatTypeDirect {
		target = port.TargetUser(evt.SenderID)
	}
	if err := s.cfg.ReplySink.SendText(ctx, evt, port.OutboundMessage{
		Target: target,
		Text:   text,
	}); err != nil {
		slog.Warn("authz: failed to send reply",
			"event_id", evt.EventID,
			"channel_name", evt.ChannelName,
			"chat_id", evt.ChatID,
			"error", err,
		)
	}
}

// Compile-time interface conformance.
var _ Step = (*authzStep)(nil)
