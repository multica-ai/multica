package messagedelivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// txStarter starts the transactions parent-integrity-guarded writes run in.
type txStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service owns the Labrastro message-delivery module: route configuration,
// delivery decisions, the send worker and the compensation scanner. HTTP
// handlers and the server assembly share ONE instance so a decision created
// by any path (API test send, event wakeup, compensator) lands in the same
// queue the worker drains.
type Service struct {
	Queries *db.Queries
	// Tx starts transactions for parent-integrity-guarded writes (the
	// workspace FOR SHARE lock around decision/receipt inserts, review
	// R3). The server assembly passes the pool; tests do the same.
	Tx txStarter
	// Sender performs the external send. nil (or a transport-less
	// deployment) fails sends with ErrorCodeSenderUnavailable.
	Sender Sender
	// Verifier proves external group/topic targets against the live
	// platform before a route referencing them may be saved or sent
	// (review R1). nil fails such saves closed with
	// TargetUnverifiableError — an unverifiable target is never stored.
	Verifier TargetVerifier
	// Syncer re-runs the EXISTING run-terminal sync for sources whose
	// completion event was lost. nil disables that compensation half.
	Syncer RunSyncer
	// AppURL is the browser origin used for task links in create_issue
	// cards. Empty omits the link (self-host without MULTICA_APP_URL).
	AppURL string
	// Log receives operational events. Content text, target addresses and
	// credentials are never logged.
	Log *slog.Logger
	// Now is injectable for tests; production uses time.Now.
	Now func() time.Time

	// ScanEvery is the compensator cadence. Zero defaults to 30s.
	ScanEvery time.Duration
	// MaxSendAttempts caps automatic retries before a delivery fails.
	// Zero defaults to 5.
	MaxSendAttempts int

	notify chan struct{}
	// decideNotify is the source wakeup: autopilot:run_done / inbox:new /
	// activity:created / comment:created land here as a latency hint and
	// the scan loop runs a decide pass over the persisted sources. Lossy
	// by design — the periodic compensator is the guarantee.
	decideNotify chan struct{}
	done         chan struct{}

	scanWarningMu sync.Mutex
	scanWarnings  map[string]time.Time
}

// New builds the module. Call Run to start the worker and compensator.
func New(queries *db.Queries) *Service {
	return &Service{
		Queries:      queries,
		Log:          slog.Default(),
		Now:          time.Now,
		notify:       make(chan struct{}, 1),
		decideNotify: make(chan struct{}, 1),
		done:         make(chan struct{}),
	}
}

func (s *Service) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Service) maxAttempts() int {
	if s.MaxSendAttempts > 0 {
		return s.MaxSendAttempts
	}
	return 5
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// ---- route configuration ----

// RouteInput is the create/update payload. Zero values are invalid where a
// field is required; Enabled defaults to true on create and to "unchanged"
// on update when nil.
type RouteInput struct {
	InstallationID  string
	TargetType      string
	TargetUserID    string
	TargetChatID    string
	TargetMessageID string
	TargetThreadID  string
	Conditions      string
	ContentMode     string
	Enabled         *bool
}

// resolvedTarget is the validated outcome of a RouteInput: the installation
// row plus the canonical target pieces a decision (and the route row) need.
type resolvedTarget struct {
	installation db.ChannelInstallation
	targetType   string
	userID       pgtype.UUID
	chatID       pgtype.Text
	messageID    pgtype.Text
	threadID     pgtype.Text
	targetKey    string
	openID       string      // member targets: the live bound platform id
	projectID    pgtype.UUID // team routes: the workspace project filter
}

// Type exposes the resolved target type to the HTTP surface (approval
// refuses member targets; they carry their own consent via binding).
func (t resolvedTarget) Type() string { return t.targetType }

// ResolveTarget validates a route payload against current state: the bot
// installation must exist, be active and belong to this workspace; a member
// target must be a current member AND hold a binding on THIS installation
// (never "some recent binding of the channel"); group/topic targets must
// carry addressable anchors.
func (s *Service) ResolveTarget(ctx context.Context, workspaceID, autopilotID pgtype.UUID, in RouteInput) (resolvedTarget, error) {
	return s.resolveTarget(ctx, workspaceID, autopilotID, in, true)
}

// ResolveTargetForApproval resolves a target for the APPROVAL flow: it
// verifies the platform-side facts (installation, reachability, anchor) but
// deliberately skips the approval lookup — an approval cannot require
// itself. Only the owner/admin gate above decides who may approve.
func (s *Service) ResolveTargetForApproval(ctx context.Context, workspaceID, autopilotID pgtype.UUID, in RouteInput) (resolvedTarget, error) {
	return s.resolveTarget(ctx, workspaceID, autopilotID, in, false)
}

func (s *Service) resolveTarget(ctx context.Context, workspaceID, autopilotID pgtype.UUID, in RouteInput, requireApproval bool) (resolvedTarget, error) {
	if !ValidTargetTypes[in.TargetType] {
		return resolvedTarget{}, &InvalidRouteError{Field: "target_type"}
	}
	if !ValidRouteConditions[in.Conditions] {
		return resolvedTarget{}, &InvalidRouteError{Field: "conditions"}
	}
	if !ValidContentModes[in.ContentMode] {
		return resolvedTarget{}, &InvalidRouteError{Field: "content_mode"}
	}
	target, err := s.resolveDeliveryTarget(ctx, workspaceID, in)
	if err != nil {
		return resolvedTarget{}, err
	}
	if requireApproval && target.targetType != TargetMember {
		if err := s.targetApproved(ctx, workspaceID, autopilotID, target.installation.ID, target.targetKey); err != nil {
			return resolvedTarget{}, err
		}
	}
	return target, nil
}

// resolveDeliveryTarget shares installation, binding and live destination checks
// across all sources. Each caller separately enforces its authorization scope.
func (s *Service) resolveDeliveryTarget(ctx context.Context, workspaceID pgtype.UUID, in RouteInput) (resolvedTarget, error) {
	instID, err := util.ParseUUID(in.InstallationID)
	if err != nil {
		return resolvedTarget{}, &InvalidRouteError{Field: "installation_id"}
	}
	inst, err := s.Queries.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          instID,
		WorkspaceID: workspaceID,
		ChannelType: "feishu",
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return resolvedTarget{}, &InstallationInvalidError{Reason: "installation not found in workspace"}
	}
	if err != nil {
		return resolvedTarget{}, fmt.Errorf("load installation: %w", err)
	}
	if inst.Status != "active" {
		return resolvedTarget{}, &InstallationInvalidError{Reason: "installation is revoked"}
	}

	out := resolvedTarget{
		installation: inst,
		targetType:   in.TargetType,
		chatID:       pgtype.Text{String: in.TargetChatID, Valid: in.TargetChatID != ""},
		messageID:    pgtype.Text{String: in.TargetMessageID, Valid: in.TargetMessageID != ""},
		threadID:     pgtype.Text{String: in.TargetThreadID, Valid: in.TargetThreadID != ""},
	}

	switch in.TargetType {
	case TargetMember:
		userID, parseErr := util.ParseUUID(in.TargetUserID)
		if parseErr != nil {
			return resolvedTarget{}, &InvalidRouteError{Field: "target_user_id"}
		}
		_, memberErr := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      userID,
			WorkspaceID: workspaceID,
		})
		if errors.Is(memberErr, pgx.ErrNoRows) {
			return resolvedTarget{}, &TargetNotMemberError{}
		} else if memberErr != nil {
			return resolvedTarget{}, fmt.Errorf("load target member: %w", memberErr)
		}
		// Installation-precise binding: (workspace, installation, user).
		// Re-checked before every send; a route is only saved when the
		// member is addressable NOW.
		_, bindErr := s.Queries.GetChannelUserBindingForDelivery(ctx, db.GetChannelUserBindingForDeliveryParams{
			WorkspaceID:    workspaceID,
			InstallationID: instID,
			MulticaUserID:  userID,
		})
		if errors.Is(bindErr, pgx.ErrNoRows) {
			return resolvedTarget{}, &MemberNotBoundError{}
		} else if bindErr != nil {
			return resolvedTarget{}, fmt.Errorf("load member binding: %w", bindErr)
		}
		out.userID = userID
		// Canonical UUID form in the target key (review R5): two textual
		// spellings of one member id must be ONE target, or the dedup
		// guarantee silently splits.
		in.TargetUserID = util.UUIDToString(userID)
	case TargetGroup:
		if in.TargetChatID == "" {
			return resolvedTarget{}, &InvalidRouteError{Field: "target_chat_id"}
		}
		// Fail-closed reachability check through the live platform
		// (review R1): a group route is only saved when the pinned bot
		// can actually see the chat.
		if s.Verifier == nil {
			return resolvedTarget{}, &TargetUnverifiableError{}
		}
		if err := s.Verifier.VerifyGroupTarget(ctx, VerifyTargetRequest{
			WorkspaceID:    util.UUIDToString(workspaceID),
			InstallationID: util.UUIDToString(instID),
			ChannelType:    inst.ChannelType,
			ChatID:         in.TargetChatID,
		}); err != nil {
			return resolvedTarget{}, classifyTargetVerifyError(err)
		}
	case TargetTopic:
		if in.TargetChatID == "" {
			return resolvedTarget{}, &InvalidRouteError{Field: "target_chat_id"}
		}
		if in.TargetMessageID == "" {
			return resolvedTarget{}, &InvalidRouteError{Field: "target_message_id"}
		}
		// Verify the anchor→chat relationship through the live platform
		// (review R1): the reply endpoint addresses only the anchor, so a
		// declared-but-foreign chat could silently redirect delivery.
		// The VERIFIED chat id is what gets stored.
		if s.Verifier == nil {
			return resolvedTarget{}, &TargetUnverifiableError{}
		}
		verified, err := s.Verifier.VerifyTopicTarget(ctx, VerifyTargetRequest{
			WorkspaceID:    util.UUIDToString(workspaceID),
			InstallationID: util.UUIDToString(instID),
			ChannelType:    inst.ChannelType,
			ChatID:         in.TargetChatID,
			MessageID:      in.TargetMessageID,
		})
		if err != nil {
			return resolvedTarget{}, classifyTargetVerifyError(err)
		}
		out.chatID = pgtype.Text{String: verified, Valid: true}
		in.TargetChatID = verified
	}

	out.targetKey = TargetKey(in.TargetType, in.TargetUserID, in.TargetChatID, in.TargetMessageID)
	return out, nil
}

// targetApproved reports whether the (source, bot, target) triple currently
// holds an active workspace-admin approval (repair contract §2, review R1).
func (s *Service) targetApproved(ctx context.Context, workspaceID, autopilotID, installationID pgtype.UUID, targetKey string) error {
	return targetApprovedWith(ctx, s.Queries, workspaceID, autopilotID, installationID, targetKey)
}

func targetApprovedWith(ctx context.Context, q *db.Queries, workspaceID, autopilotID, installationID pgtype.UUID, targetKey string) error {
	_, err := q.GetActiveLabrastroMessageApprovedTarget(ctx, db.GetActiveLabrastroMessageApprovedTargetParams{
		WorkspaceID:    workspaceID,
		AutopilotID:    autopilotID,
		InstallationID: installationID,
		TargetKey:      targetKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return &TargetNotApprovedError{}
	}
	if err != nil {
		return &TargetUnverifiableError{Detail: "check approval: " + err.Error()}
	}
	return nil
}

// ---- approved-target administration (owner/admin only; enforced upstream) ----

// ApproveTarget grants an active approval for one (automation, bot, target)
// triple. The caller resolves the target first so the approval covers the
// VERIFIED chat id, mirroring what a route against it would store.
func (s *Service) ApproveTarget(ctx context.Context, ap db.Autopilot, approver db.Member, target resolvedTarget) (db.LabrastroMessageApprovedTarget, error) {
	var row db.LabrastroMessageApprovedTarget
	err := s.withTargetWrite(ctx, ap.WorkspaceID, ap.ID, approver, target, true, func(q *db.Queries) error {
		var err error
		row, err = q.ApproveLabrastroMessageTarget(ctx, db.ApproveLabrastroMessageTargetParams{
			WorkspaceID: ap.WorkspaceID, AutopilotID: ap.ID, InstallationID: target.installation.ID,
			TargetKey: target.targetKey, TargetType: target.targetType, ApprovedBy: approver.UserID,
		})
		return err
	})
	if isUniqueViolation(err) {
		return row, ErrRouteAlreadyExists
	}
	return row, err
}

// Revocation and queued cancellation commit together. The exact approval ID
// prevents a stale request from revoking a newer approval of the same target.
func (s *Service) RevokeTarget(ctx context.Context, ap db.Autopilot, target db.LabrastroMessageApprovedTarget) (int, error) {
	cancelled := 0
	err := s.withParentLock(ctx, ap.WorkspaceID, func(q *db.Queries) error {
		if _, err := q.LockAutopilotForUpdate(ctx, db.LockAutopilotForUpdateParams{ID: ap.ID, WorkspaceID: ap.WorkspaceID}); err != nil {
			return err
		}
		revoked, err := q.RevokeLabrastroMessageTarget(ctx, db.RevokeLabrastroMessageTargetParams{
			ID: target.ID, WorkspaceID: ap.WorkspaceID, AutopilotID: ap.ID,
			InstallationID: target.InstallationID, TargetKey: target.TargetKey,
		})
		if err != nil {
			return err
		}
		if len(revoked) == 0 {
			return ErrApprovedTargetNotFound
		}
		rows, err := q.CancelLabrastroMessageDeliveriesByTarget(ctx, db.CancelLabrastroMessageDeliveriesByTargetParams{
			WorkspaceID: ap.WorkspaceID, AutopilotID: ap.ID, InstallationID: target.InstallationID,
			TargetKey: target.TargetKey, ErrorCode: pgtype.Text{String: ErrorCodeTargetNotApproved, Valid: true},
			LastError: pgtype.Text{String: "target approval revoked", Valid: true},
		})
		cancelled = len(rows)
		return err
	})
	return cancelled, err
}

// ListApprovedTargets returns the automation's active approvals.
func (s *Service) ListApprovedTargets(ctx context.Context, ap db.Autopilot) ([]db.LabrastroMessageApprovedTarget, error) {
	return s.Queries.ListLabrastroMessageApprovedTargets(ctx, db.ListLabrastroMessageApprovedTargetsParams{
		WorkspaceID: ap.WorkspaceID,
		AutopilotID: ap.ID,
	})
}

// classifyTargetVerifyError maps adapter verification failures onto the
// module's typed errors. Anything unrecognized is treated as UNVERIFIABLE,
// so a save never proceeds on an unknown verdict.
func classifyTargetVerifyError(err error) error {
	var mismatch *TargetAnchorMismatchError
	var unreachable *TargetUnreachableError
	switch {
	case errors.Is(err, ErrTargetUnreachable):
		return &TargetUnreachableError{Detail: err.Error()}
	case errors.Is(err, ErrTargetAnchorMismatch):
		return &TargetAnchorMismatchError{Detail: err.Error()}
	case errors.As(err, &mismatch):
		return mismatch
	case errors.As(err, &unreachable):
		return unreachable
	}
	return &TargetUnverifiableError{Detail: err.Error()}
}

func (s *Service) enabledOrDefault(in RouteInput, create bool, current bool) bool {
	if in.Enabled != nil {
		return *in.Enabled
	}
	if create {
		return true
	}
	return current
}

// CreateRoute saves a new delivery rule. The acting member (resolved by the
// HTTP layer through requireAutopilotWrite) is stamped as creator AND first
// updater — the config author is recorded, distinct from the recipient and
// from whoever authorized the automation.
func (s *Service) CreateRoute(ctx context.Context, ap db.Autopilot, member db.Member, in RouteInput) (db.LabrastroMessageRoute, error) {
	target, err := s.ResolveTarget(ctx, ap.WorkspaceID, ap.ID, in)
	if err != nil {
		return db.LabrastroMessageRoute{}, err
	}
	return s.createRoute(ctx, ap.WorkspaceID, member, routeConfig{
		scope: RouteSourceRun, autopilotID: ap.ID, target: target,
		conditions: in.Conditions, contentMode: in.ContentMode,
		enabled: s.enabledOrDefault(in, true, false),
	})
}

// UpdateRoute edits a route under optimistic concurrency. Zero rows from a
// matching-id-but-stale-revision update surface as ErrRouteRevisionConflict.
// Disabling through an edit cancels the route's not-yet-started sends.
func (s *Service) UpdateRoute(ctx context.Context, route db.LabrastroMessageRoute, member db.Member, expectedRevision int32, in RouteInput) (db.LabrastroMessageRoute, error) {
	target, err := s.ResolveTarget(ctx, route.WorkspaceID, route.AutopilotID, in)
	if err != nil {
		return db.LabrastroMessageRoute{}, err
	}
	return s.updateRoute(ctx, route, member, expectedRevision, routeConfig{
		scope: RouteSourceRun, autopilotID: route.AutopilotID, target: target,
		conditions: in.Conditions, contentMode: in.ContentMode,
		enabled: s.enabledOrDefault(in, false, route.Enabled),
	})
}

// SetRouteEnabled flips a route's enabled flag under optimistic
// concurrency. Disable stops new enqueues (rules only decide while enabled)
// and cancels queued sends; transitioning from disabled to enabled resets
// the eligibility boundary so the disabled window is never backfilled.
func (s *Service) SetRouteEnabled(ctx context.Context, route db.LabrastroMessageRoute, member db.Member, enabled bool, expectedRevision int32) (db.LabrastroMessageRoute, error) {
	return s.setRouteEnabled(ctx, route, member, enabled, expectedRevision)
}

// DeleteRoute removes the rule and cancels its queued sends. Decisions
// already made (sent/failed/...) keep their snapshots and receipts for
// audit — deleting a rule never rewrites history.
func (s *Service) DeleteRoute(ctx context.Context, route db.LabrastroMessageRoute) error {
	return s.withParentLock(ctx, route.WorkspaceID, func(q *db.Queries) error {
		// Delete takes the row lock before cancelling: all decisions hold
		// SHARE on this row until insertion, so no late candidate can escape.
		if err := q.DeleteLabrastroMessageRoute(ctx, db.DeleteLabrastroMessageRouteParams{ID: route.ID, WorkspaceID: route.WorkspaceID}); err != nil {
			return err
		}
		return cancelRouteDeliveriesWith(ctx, q, route.ID, ErrorCodeRouteDeleted, "route deleted")
	})
}

func cancelRouteDeliveriesWith(ctx context.Context, q *db.Queries, routeID pgtype.UUID, code, reason string) error {
	_, err := q.CancelLabrastroMessageDeliveriesByRoute(ctx, db.CancelLabrastroMessageDeliveriesByRouteParams{
		RouteID: routeID, ErrorCode: pgtype.Text{String: code, Valid: true}, LastError: pgtype.Text{String: reason, Valid: true},
	})
	return err
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

// ---- delivery decisions ----

// EnqueueRunDeliveries uses the production candidate query and decision writer,
// restricted to one run. The compensator uses the same path without a run filter.
func (s *Service) EnqueueRunDeliveries(ctx context.Context, runID pgtype.UUID) (int, error) {
	return s.enqueueRunCandidates(ctx, runID, math.MaxInt32)
}

// sourceFactsFromRun assembles the persisted source evidence from a run row.
func sourceFactsFromRun(run db.AutopilotRun) sourceFacts {
	return sourceFacts{
		RunID:               util.UUIDToString(run.ID),
		RunStatus:           run.Status,
		RunCompletedAt:      run.CompletedAt.Time,
		RunCompletedAtValid: run.CompletedAt.Valid,
		TaskID:              util.UUIDToString(run.TaskID),
		TaskIDValid:         run.TaskID.Valid,
		IssueID:             util.UUIDToString(run.IssueID),
		IssueIDValid:        run.IssueID.Valid,
		Result:              run.Result,
		FailureReason:       run.FailureReason.String,
		ReasonCode:          run.ReasonCode.String,
	}
}

// decisionInputFromSource is the ONE assembly point for decision inputs
// (repair contract §3): both the event path and the compensator build
// through here, so neither can silently omit a persisted field.
func (s *Service) decisionInputFromSource(ap db.Autopilot, route db.LabrastroMessageRoute, src sourceFacts) decisionInput {
	return decisionInput{
		WorkspaceID:         util.UUIDToString(ap.WorkspaceID),
		AutopilotID:         util.UUIDToString(ap.ID),
		RunID:               src.RunID,
		RunStatus:           src.RunStatus,
		RunCompletedAt:      src.RunCompletedAt,
		RunCompletedAtValid: src.RunCompletedAtValid,
		ExecutionMode:       ap.ExecutionMode,
		RunTaskID:           src.TaskID,
		RunTaskIDValid:      src.TaskIDValid,
		Route:               route,
		Run: runFields{
			Result:        src.Result,
			FailureReason: src.FailureReason,
			ReasonCode:    src.ReasonCode,
			IssueID:       src.IssueID,
			IssueIDValid:  src.IssueIDValid,
		},
	}
}

// decideDelivery freezes content + target for one (run, route) pair and
// records the decision — a send when the condition matches, a suppressed
// marker otherwise. Both count as decisions: a source this module has judged
// is never re-judged. Returns 1 when THIS call created the row, 0 when a
// decision already existed.
func (s *Service) decideDelivery(ctx context.Context, ap db.Autopilot, in decisionInput) (int, error) {
	// Eligibility window: only runs completing at/after the rule's
	// boundary. Outside the window is NOT a decision — the candidate scan
	// filters those pairs out too, so nothing accumulates.
	if in.Route.EffectiveFrom.Valid && in.RunCompletedAtValid && in.RunCompletedAt.Before(in.Route.EffectiveFrom.Time) {
		return 0, nil
	}
	target, content, ref, shardTotal, err := s.buildDecisionPayload(ctx, ap, in)
	if err != nil {
		return 0, err
	}

	status := DeliveryStatusQueued
	var errorCode pgtype.Text
	if ref.ExecutionMode == SourceKindUnknown {
		status = DeliveryStatusSuppressed
		errorCode = pgtype.Text{String: "source_unresolved", Valid: true}
	} else if !routeMatchesRun(in.Route.Conditions, in.RunStatus) {
		// Condition mismatch (including skipped runs): record the
		// decision so the compensator stops revisiting this source.
		status = DeliveryStatusSuppressed
		errorCode = pgtype.Text{String: ErrorCodeConditionMismatch, Valid: true}
	}

	return s.insertDecision(ctx, sourceDecision{
		scope: RouteSourceRun, workspaceID: util.UUIDToString(ap.WorkspaceID),
		autopilotID: ap.ID, runID: mustUUID(in.RunID), sourceRefID: in.RunID,
		sourceCreatedAt: in.RunCompletedAt,
		routeID:         in.Route.ID, routeRevision: in.Route.Revision,
		installationID: in.Route.InstallationID, targetKey: in.Route.TargetKey,
		kind: ref.ExecutionMode, status: status, errorCode: errorCode.String,
		target: target, content: content, ref: ref, shardTotal: shardTotal,
	})
}

// buildDecisionPayload renders the frozen content/target/ref triple. This
// is the redaction boundary: only curated fields cross it.
func (s *Service) buildDecisionPayload(ctx context.Context, ap db.Autopilot, in decisionInput) (targetSnapshot, contentSnapshot, sourceRef, int, error) {
	target := targetSnapshot{
		TargetType:   in.Route.TargetType,
		ChannelType:  in.Route.ChannelType,
		Installation: util.UUIDToString(in.Route.InstallationID),
	}
	// Member targets freeze the CURRENT bound platform id, and the send
	// path re-resolves the binding before dialing (revocation between
	// decision and send fails the delivery with member_unbound, it does
	// not send to a stale address).
	if in.Route.TargetType == TargetMember && in.Route.TargetUserID.Valid {
		target.UserID = util.UUIDToString(in.Route.TargetUserID)
		binding, err := s.Queries.GetChannelUserBindingForDelivery(ctx, db.GetChannelUserBindingForDeliveryParams{
			WorkspaceID:    ap.WorkspaceID,
			InstallationID: in.Route.InstallationID,
			MulticaUserID:  in.Route.TargetUserID,
		})
		switch {
		case err == nil:
			target.OpenID = binding.ChannelUserID
		case errors.Is(err, pgx.ErrNoRows):
			// Unbound between rule save and decision: the send would
			// fail anyway; freeze without an address so the send-time
			// failure is member_unbound.
		default:
			return target, contentSnapshot{}, sourceRef{}, 0,
				fmt.Errorf("resolve member binding for decision: %w", err)
		}
	}
	target.ChatID = in.Route.TargetChatID.String
	target.MessageID = in.Route.TargetMessageID.String
	target.ThreadID = in.Route.TargetThreadID.String

	// The source kind comes from the run's persisted links (review R7);
	// the frozen ref records that original mode, never the mutable
	// autopilot configuration.
	sourceKind := sourceKindFromRun(in.Run.IssueIDValid, in.RunTaskIDValid, in.Run.Result, in.ExecutionMode)
	ref := sourceRef{
		RunID:         in.RunID,
		ExecutionMode: sourceKind,
		IssueID:       in.Run.IssueID,
	}

	var content contentSnapshot
	switch sourceKind {
	case SourceKindRunOnly:
		output, hasOutput := extractRunOnlyOutput(in.Run.Result)
		content = buildRunOnlyContent(
			ap.Title,
			in.RunStatus,
			in.Run.ReasonCode,
			in.Run.FailureReason,
			output,
			hasOutput,
			in.Route.ContentMode == ContentWithOutput,
		)
	case SourceKindCreateIssue:
		ident, issueStatus, slug := s.issueRef(ctx, ap.WorkspaceID, in)
		content = buildCreateIssueContent(ap.Title, in.RunStatus, ident, issueStatus, s.AppURL, slug)
		ref.IssueIdentifier = ident
		ref.IssueStatus = issueStatus
	}

	if sourceKind == SourceKindUnknown {
		return target, content, ref, 0, nil
	}
	shards := splitShards(NewMessage(content.Text, content.Link))
	return target, content, ref, len(shards), nil
}

// issueRef resolves the linked issue identifier, its status at the run's first
// settlement, and the workspace slug for a create_issue card. The atomic
// terminal write freezes first_terminal_status alongside the run and quota;
// later issue changes cannot rewrite it. Older runs without that structured
// signal display an unnamed status. A deleted issue removes the link without
// preventing delivery.
func (s *Service) issueRef(ctx context.Context, workspaceID pgtype.UUID, in decisionInput) (string, string, string) {
	if !in.Run.IssueIDValid {
		return "", "", ""
	}
	issueID, err := util.ParseUUID(in.Run.IssueID)
	if err != nil {
		return "", "", ""
	}
	issue, err := s.Queries.GetIssue(ctx, issueID)
	if err != nil {
		return "", "", ""
	}
	prefix := ""
	slug := ""
	if ws, err := s.Queries.GetWorkspace(ctx, workspaceID); err == nil {
		prefix = ws.IssuePrefix
		slug = ws.Slug
	}
	return issueIdentifier(prefix, issue.Number), s.firstTerminalIssueStatus(issue, in), slug
}

// firstTerminalIssueStatus reports the status to name on the card: ONLY the
// structured first_terminal_status the terminal sync froze into run.result
// (both completed and failed branches — repair contract §3, review R11).
// There is deliberately NO string parsing of failure_reason: that column
// also carries raw task errors (paths, credentials) from other producers,
// and everything without a structured signal degrades to an unnamed-status
// card rather than guessing.
func (s *Service) firstTerminalIssueStatus(issue db.Issue, in decisionInput) string {
	if len(in.Run.Result) == 0 {
		return ""
	}
	var payload struct {
		FirstTerminalStatus string `json:"first_terminal_status"`
	}
	if err := json.Unmarshal(in.Run.Result, &payload); err != nil {
		return ""
	}
	return payload.FirstTerminalStatus
}

// issueIdentifier mirrors service.IssueIdentifier ("MUL-42", or "#42"
// without a prefix). Kept local so this module stays a leaf package; the
// format is API-visible contract and must not drift — a test pins the
// format.
func issueIdentifier(prefix string, number int32) string {
	if prefix == "" {
		return "#" + strconv.Itoa(int(number))
	}
	return prefix + "-" + strconv.Itoa(int(number))
}

func mustUUID(s string) pgtype.UUID {
	u, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return u
}
