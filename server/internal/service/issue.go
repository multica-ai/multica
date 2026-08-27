package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issueguard"
	"github.com/multica-ai/multica/server/internal/issueposition"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// IssueService is the single service-layer entry point for creating issues.
// Both the HTTP `POST /issues` handler and the future Lark `/issue` command
// call into Create so that duplicate guard, issue numbering, attachment
// linking, broadcast, analytics, and agent/squad enqueue stay aligned. The
// service deliberately does NOT depend on http.Request — callers parse
// their own transport and pass a fully-resolved IssueCreateParams.
type IssueService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Bus       *events.Bus
	Analytics analytics.Client
	// Metrics is the shared business-metrics collector. Wired by
	// cmd/server/router.go after construction; nil in tests / self-hosted
	// without the metrics listener — obsmetrics.RecordEvent treats a nil
	// Metrics as "PostHog only", so leaving it unset is safe.
	Metrics     *obsmetrics.BusinessMetrics
	TaskService *TaskService
	// Entitlements supplies Cloud's effective issue-count instruction. Nil is
	// the self-hosted unlimited path.
	Entitlements entitlement.Provider
}

func NewIssueService(q *db.Queries, tx TxStarter, bus *events.Bus, ac analytics.Client, ts *TaskService) *IssueService {
	return &IssueService{
		Queries:     q,
		TxStarter:   tx,
		Bus:         bus,
		Analytics:   ac,
		TaskService: ts,
	}
}

// IssueCreateParams carries the already-validated, already-resolved inputs
// to IssueService.Create. The handler owns the parsing step that turns its
// request payload into this struct; the service stays transport-agnostic.
type IssueCreateParams struct {
	WorkspaceID   pgtype.UUID
	Title         string
	Description   pgtype.Text
	Status        string
	Priority      string
	AssigneeType  pgtype.Text
	AssigneeID    pgtype.UUID
	CreatorType   string // "agent" or "member"
	CreatorID     pgtype.UUID
	ParentIssueID pgtype.UUID
	ProjectID     pgtype.UUID
	StartDate     pgtype.Date
	DueDate       pgtype.Date
	OriginType    pgtype.Text
	OriginID      pgtype.UUID
	AttachmentIDs []pgtype.UUID
	// LabelIDs are the issue-scoped labels to attach to the new issue. They
	// are validated and written inside the create transaction (see Create),
	// so the issue is never committed with a partial or wrong label set. An
	// unknown or non-issue label id fails the whole create with
	// ErrIssueLabelNotFound rather than being silently dropped.
	LabelIDs       []pgtype.UUID
	AllowDuplicate bool
	// Stage groups this issue into an ordered barrier group under its parent
	// (NULL = unstaged). See issue_child_done.go for the staged-barrier wake.
	Stage pgtype.Int4
	// SourceContext is set only by the comment-scoped manual create endpoint.
	// Its immutable snapshot and cloned attachment rows commit in the same
	// transaction as the new issue.
	SourceContext *SourceContextCapture
}

// IssueCreateOpts groups optional knobs for IssueService.Create. Most
// callers leave it zero-valued.
type IssueCreateOpts struct {
	// BroadcastPayload, if non-nil, is invoked after the issue row is
	// created and attachments are linked. Its return value is sent as
	// the EventIssueCreated payload via the event bus. The HTTP handler
	// uses this hook to inject its IssueResponse without forcing this
	// package to depend on handler-layer types. If nil, the service
	// emits a minimal `{"issue_id": <uuid>}` payload — enough for cache
	// invalidation, but front-ends that expect the full response shape
	// must provide BroadcastPayload. The labels argument is the authoritative
	// snapshot attached in the create transaction, so the emitted payload can
	// carry the new issue's labels and every online client renders it already
	// labeled instead of blank until a refetch.
	BroadcastPayload func(issue db.Issue, attachments []db.Attachment, labels []db.IssueLabel) map[string]any

	// ActorID overrides the actor ID used for broadcast + analytics
	// when it differs from the creator on the row. Agent-created issues
	// use the agent UUID here (the creator_id column is the daemon
	// owner). Empty falls back to CreatorID.
	ActorID string

	// AnalyticsAgentID is the agent associated with the issue for
	// analytics purposes (assignee agent or, for agent-created issues,
	// the creator agent). Resolved by the caller because it depends on
	// transport context.
	AnalyticsAgentID string

	// Platform tags the IssueCreated analytics + business-metrics event
	// with the client surface the request came in on (web / desktop /
	// daemon / lark / autopilot). Derived from middleware's client
	// metadata at the handler layer.
	Platform string

	// AssignedAgentRunFireAt creates the automatic assigned-agent task in a
	// durable deferred state. Channel /issue uses this while detached media is
	// still resolving, then promotes the returned task after attachment binding.
	// Zero preserves the ordinary immediate enqueue path.
	AssignedAgentRunFireAt time.Time
}

// ErrActiveDuplicate signals that the duplicate guard found an active
// issue with the same (workspace, project, parent, title) tuple and
// AllowDuplicate was false. The IssueCreateResult.DuplicateIssue field is
// populated when this error is returned so callers can render the
// conflict (HTTP 409, Lark card, etc.).
var ErrActiveDuplicate = errors.New("active duplicate issue exists")

// ErrParentIssueNotFound signals that the supplied ParentIssueID does
// not exist in the issue's workspace. The service refuses to create
// orphaned or cross-workspace child issues; callers translate this into
// their transport's 400 / Lark card error.
var ErrParentIssueNotFound = errors.New("parent issue not found in this workspace")

// ErrProjectNotFound signals that the supplied ProjectID does not exist
// in the issue's workspace. Cross-workspace project IDs are rejected
// here so every create entry (HTTP `POST /issues`, Lark `/issue`, future
// MCP / API key callers) enforces the same workspace boundary without
// having to remember it. Callers translate this into 400.
var ErrProjectNotFound = errors.New("project not found in this workspace")

// ErrIssueLabelNotFound signals that one of the supplied LabelIDs does not
// exist in the issue's workspace or is not an issue-scoped label. The whole
// create is rejected so a new issue is never born with a partial or wrong
// label set. Callers translate this into their transport's 400.
var ErrIssueLabelNotFound = errors.New("issue label not found in this workspace")

// ErrIssueStatusUnavailable signals that the requested custom status was
// archived between the caller's pre-flight validation and the create
// transaction. Callers translate this into a 409 — the request was valid when
// it arrived, so retrying against the refreshed catalog is the remedy.
var ErrIssueStatusUnavailable = errors.New("issue status is no longer available")

var ErrSourceContextAlreadyAttached = errors.New("source context is already attached")

// IssueCreateResult is the typed return from IssueService.Create.
//
//   - On the happy path: Issue is the new row, Attachments lists the
//     linked attachments (may be empty), DuplicateIssue is nil.
//   - On ErrActiveDuplicate: DuplicateIssue is the row that blocked the
//     create; Issue and Attachments are zero.
type IssueCreateResult struct {
	Issue       db.Issue
	Attachments []db.Attachment
	// AssignedTaskID is populated when Create enqueues the automatic task for
	// an agent assignee, including a task deferred by AssignedAgentRunFireAt.
	AssignedTaskID pgtype.UUID
	// Labels is the authoritative set of labels attached to the new issue in
	// the create transaction (empty when none were requested). Callers echo it
	// on the create response + issue:created event so every client renders the
	// new issue already labeled and a new client can detect that the backend
	// understood label_ids (see the create handler's compatibility contract).
	Labels         []db.IssueLabel
	DuplicateIssue *db.Issue
}

// Create runs the full issue-creation pipeline atomically end-to-end:
//
//  1. Begin transaction.
//  2. Resolve & validate parent / project belong to the same workspace.
//  3. Lock & check the duplicate guard.
//  4. Increment the workspace issue counter.
//  5. Insert the issue row (with optional origin stamping).
//  6. Commit.
//  7. Link any pre-uploaded attachments (post-commit, idempotent).
//  8. For a media-gated channel issue, persist its deferred assigned-agent
//     task in the issue transaction so both rows become visible atomically.
//     Ordinary creates keep their existing event-before-enqueue ordering.
//  9. Publish EventIssueCreated to the bus (payload via opts.BroadcastPayload).
//  10. Capture the IssueCreated analytics event.
//  11. Enqueue the ordinary agent task or trigger the squad leader when the
//     issue is assigned and not in `backlog`.
//
// Validation that lives in the service (parent existence, project
// workspace membership, parent → project back-fill) is enforced here so
// every create entry — HTTP `POST /issues`, Lark `/issue`, future
// MCP/API-key callers — shares the same workspace boundary semantics.
// Caller-owned validation is limited to transport-shaped checks: title
// required, RFC3339 date format, assignee pair sanity.
func (s *IssueService) Create(ctx context.Context, p IssueCreateParams, opts IssueCreateOpts) (IssueCreateResult, error) {
	issueCountPolicy := ResolveIssueCountPolicy(ctx, s.Entitlements, p.WorkspaceID)
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return IssueCreateResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	if p.SourceContext != nil {
		if _, err := qtx.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{
			ID: p.SourceContext.SourceIssueID, WorkspaceID: p.WorkspaceID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return IssueCreateResult{}, ErrSourceIssueDeleted
			}
			return IssueCreateResult{}, fmt.Errorf("lock source issue: %w", err)
		}
		locked, err := qtx.LockCommentAncestorPath(ctx, db.LockCommentAncestorPathParams{
			CommentID: p.SourceContext.AnchorCommentID, WorkspaceID: p.WorkspaceID,
			IssueID: p.SourceContext.SourceIssueID,
		})
		if err != nil {
			return IssueCreateResult{}, fmt.Errorf("lock anchor comment thread: %w", err)
		}
		if len(locked) == 0 {
			return IssueCreateResult{}, ErrAnchorCommentDeleted
		}
		current, err := BuildSourceContext(ctx, qtx, p.WorkspaceID, p.SourceContext.AnchorCommentID)
		if err != nil {
			return IssueCreateResult{}, err
		}
		if current.Digest != p.SourceContext.Digest {
			return IssueCreateResult{}, ErrSourceContextChanged
		}
	}

	// A create landing on a CUSTOM status takes the shared catalog lock AND
	// re-resolves the status inside this transaction. The caller validated the
	// status before the transaction opened, which is early enough to return a
	// clean 400 but too early to be safe: an archive can commit in between.
	// Re-checking under the lock is what makes the status provably active at
	// the moment the row is written. Built-in statuses skip both — they can
	// never be archived, so the common path is unchanged. (MUL-6243)
	if !issuestatus.IsBuiltIn(p.Status) {
		if err := qtx.LockIssueStatusCatalogShared(ctx, p.WorkspaceID); err != nil {
			return IssueCreateResult{}, err
		}
		if _, err := issuestatus.Resolve(ctx, qtx, p.WorkspaceID, p.Status); err != nil {
			if errors.Is(err, issuestatus.ErrUnknownStatus) {
				return IssueCreateResult{}, ErrIssueStatusUnavailable
			}
			return IssueCreateResult{}, err
		}
	}

	// Resolve and validate parent / project before reading from the
	// duplicate guard so a forged parent or project ID is rejected
	// before we touch the issue counter. Both checks scope by
	// WorkspaceID — there is no path from this service to a row in a
	// foreign workspace.
	projectID := p.ProjectID
	if p.ParentIssueID.Valid {
		parent, err := qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
			ID:          p.ParentIssueID,
			WorkspaceID: p.WorkspaceID,
		})
		if err != nil || !parent.ID.Valid {
			return IssueCreateResult{}, ErrParentIssueNotFound
		}
		// Back-fill project from parent when the caller did not pin
		// one explicitly. Matches the long-standing HTTP behavior: a
		// sub-issue inherits its parent's project unless overridden.
		if !projectID.Valid {
			projectID = parent.ProjectID
		}
	}
	if projectID.Valid {
		if _, err := qtx.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
			ID:          projectID,
			WorkspaceID: p.WorkspaceID,
		}); err != nil {
			return IssueCreateResult{}, ErrProjectNotFound
		}
	}

	// Validate labels before we increment the issue counter so a stale or
	// wrong-scope selection fails the create cheaply. The de-duplicated rows
	// are attached to the issue below, inside this same transaction, and
	// echoed back as the authoritative snapshot in the result.
	labels, err := validateIssueLabels(ctx, qtx, p.WorkspaceID, p.LabelIDs)
	if err != nil {
		return IssueCreateResult{}, err
	}

	duplicate, found, err := issueguard.LockAndFindActiveDuplicate(ctx, qtx, p.WorkspaceID, projectID, p.ParentIssueID, p.Title, p.AllowDuplicate)
	if err != nil {
		return IssueCreateResult{}, fmt.Errorf("duplicate guard: %w", err)
	}
	if found {
		dup := duplicate
		return IssueCreateResult{DuplicateIssue: &dup}, ErrActiveDuplicate
	}

	issueNumber, err := AllocateIssueNumber(ctx, qtx, p.WorkspaceID, issueCountPolicy)
	if err != nil {
		return IssueCreateResult{}, fmt.Errorf("allocate issue number: %w", err)
	}

	// New issues sort to the top of their (workspace, status) column for
	// manual ordering. Computed inside the tx, after IncrementIssueCounter
	// has already taken the workspace row lock, so two concurrent creates
	// in the same workspace see each other's positions and don't both
	// land on the same min-1 slot. Concurrent manual reorder via
	// UpdateIssue(position) does NOT take this lock, so a create racing
	// a reorder is still allowed to collide on position — manual ordering
	// is best-effort and the UI tolerates equal positions by falling back
	// to the secondary ORDER BY key.
	newPosition, err := issueposition.NextTopPosition(ctx, tx, p.WorkspaceID, p.Status)
	if err != nil {
		return IssueCreateResult{}, fmt.Errorf("next top position: %w", err)
	}

	var issue db.Issue
	var assignedTask db.AgentTaskQueue
	if p.OriginType.Valid {
		issue, err = qtx.CreateIssueWithOrigin(ctx, db.CreateIssueWithOriginParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   p.WorkspaceID,
			Title:         p.Title,
			Description:   p.Description,
			Status:        p.Status,
			Priority:      p.Priority,
			AssigneeType:  p.AssigneeType,
			AssigneeID:    p.AssigneeID,
			CreatorType:   p.CreatorType,
			CreatorID:     p.CreatorID,
			ParentIssueID: p.ParentIssueID,
			Position:      newPosition,
			StartDate:     p.StartDate,
			DueDate:       p.DueDate,
			Number:        issueNumber,
			ProjectID:     projectID,
			OriginType:    p.OriginType,
			OriginID:      p.OriginID,
			Stage:         p.Stage,
		})
	} else {
		issue, err = qtx.CreateIssue(ctx, db.CreateIssueParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   p.WorkspaceID,
			Title:         p.Title,
			Description:   p.Description,
			Status:        p.Status,
			Priority:      p.Priority,
			AssigneeType:  p.AssigneeType,
			AssigneeID:    p.AssigneeID,
			CreatorType:   p.CreatorType,
			CreatorID:     p.CreatorID,
			ParentIssueID: p.ParentIssueID,
			Position:      newPosition,
			StartDate:     p.StartDate,
			DueDate:       p.DueDate,
			Number:        issueNumber,
			ProjectID:     projectID,
			Stage:         p.Stage,
		})
	}
	if err != nil {
		return IssueCreateResult{}, fmt.Errorf("create issue: %w", err)
	}

	if p.SourceContext != nil {
		if _, err := PersistSourceContext(ctx, qtx, *p.SourceContext, issue.ID, pgtype.UUID{}); err != nil {
			return IssueCreateResult{}, fmt.Errorf("persist source context: %w", err)
		}
	} else if p.OriginType.Valid && p.OriginType.String == "quick_create" && p.OriginID.Valid {
		task, taskErr := qtx.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{
			ID: p.OriginID, WorkspaceID: p.WorkspaceID,
		})
		if taskErr != nil {
			return IssueCreateResult{}, fmt.Errorf("load quick-create origin task: %w", taskErr)
		}
		if p.CreatorType != "agent" || !p.CreatorID.Valid || p.CreatorID != task.AgentID {
			return IssueCreateResult{}, errors.New("quick-create origin task does not belong to the creating agent")
		}
		var quickCreate QuickCreateContext
		if err := json.Unmarshal(task.Context, &quickCreate); err != nil {
			return IssueCreateResult{}, fmt.Errorf("decode quick-create origin context: %w", err)
		}
		if quickCreate.Type != QuickCreateContextType {
			return IssueCreateResult{}, errors.New("quick-create origin task has invalid context type")
		}
		contextWorkspaceID, parseErr := util.ParseUUID(quickCreate.WorkspaceID)
		if parseErr != nil || contextWorkspaceID != p.WorkspaceID {
			return IssueCreateResult{}, errors.New("quick-create origin context has invalid workspace")
		}
		if quickCreate.SourceContextID != "" {
			contextID, parseErr := util.ParseUUID(quickCreate.SourceContextID)
			if parseErr != nil {
				return IssueCreateResult{}, fmt.Errorf("invalid quick-create source context id: %w", parseErr)
			}
			requesterID, parseErr := util.ParseUUID(quickCreate.RequesterID)
			if parseErr != nil || !task.OriginatorUserID.Valid || requesterID != task.OriginatorUserID {
				return IssueCreateResult{}, errors.New("quick-create source context has invalid requester")
			}
			pending, pendingErr := qtx.GetPendingIssueSourceContextByOriginTask(ctx, db.GetPendingIssueSourceContextByOriginTaskParams{
				WorkspaceID: p.WorkspaceID, OriginTaskID: task.ID,
			})
			if pendingErr != nil {
				if errors.Is(pendingErr, pgx.ErrNoRows) {
					return IssueCreateResult{}, ErrSourceContextAlreadyAttached
				}
				return IssueCreateResult{}, fmt.Errorf("load pending quick-create source context: %w", pendingErr)
			}
			if pending.ID != contextID || pending.CapturedByUserID != requesterID {
				return IssueCreateResult{}, errors.New("quick-create source context ownership mismatch")
			}
			if _, attachErr := qtx.AttachIssueSourceContext(ctx, db.AttachIssueSourceContextParams{
				IssueID: issue.ID, WorkspaceID: p.WorkspaceID, ID: contextID, OriginTaskID: task.ID,
			}); attachErr != nil {
				if errors.Is(attachErr, pgx.ErrNoRows) {
					return IssueCreateResult{}, ErrSourceContextAlreadyAttached
				}
				return IssueCreateResult{}, fmt.Errorf("attach quick-create source context: %w", attachErr)
			}
		}
	}

	// Attach labels inside the create transaction so the issue and its
	// labels commit together — the old flow created the issue first and
	// attached labels in a second, non-atomic round-trip whose partial
	// failure left the issue mis-categorized. AttachLabelToIssue is
	// workspace/resource_type-guarded and ON CONFLICT DO NOTHING, and the
	// ids were already validated above.
	for _, label := range labels {
		if err := qtx.AttachLabelToIssueOnCreate(ctx, db.AttachLabelToIssueOnCreateParams{
			IssueID:     issue.ID,
			LabelID:     label.ID,
			WorkspaceID: p.WorkspaceID,
		}); err != nil {
			return IssueCreateResult{}, fmt.Errorf("attach issue label: %w", err)
		}
	}

	if !opts.AssignedAgentRunFireAt.IsZero() && s.shouldEnqueueAgentTaskWithQueries(ctx, qtx, issue) {
		// The issue must never become visible without its media-gated assigned
		// task. Inserting both rows through qtx makes the unique-index winner
		// deterministic: any observer that can discover the committed issue also
		// sees the inert deferred task and must merge into it.
		assignedTask, err = s.TaskService.createDeferredChannelIssueTaskWithQueries(ctx, qtx, issue, opts.AssignedAgentRunFireAt)
		if err != nil {
			return IssueCreateResult{}, fmt.Errorf("create deferred channel issue task: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return IssueCreateResult{}, fmt.Errorf("commit: %w", err)
	}

	attachments := s.linkAttachments(ctx, issue, p.AttachmentIDs)

	actorID := opts.ActorID
	if actorID == "" {
		actorID = util.UUIDToString(issue.CreatorID)
	}

	var assignedTaskID pgtype.UUID
	if !opts.AssignedAgentRunFireAt.IsZero() {
		assignedTaskID = assignedTask.ID
		if assignedTaskID.Valid {
			if err := s.TaskService.hydrateDeferredChannelIssueTaskOverlay(ctx, assignedTask); err != nil {
				// Runtime overlays are best-effort on every enqueue path. The task is
				// already durable and safely deferred, so an optional integration
				// failure must not turn a committed issue into a retry duplicate.
				slog.Warn("hydrate deferred channel issue task overlay failed",
					"issue_id", util.UUIDToString(issue.ID),
					"task_id", util.UUIDToString(assignedTask.ID),
					"error", err)
			}
		} else if s.shouldEnqueueSquadLeaderOnAssign(ctx, issue) {
			// AssignedAgentRunFireAt currently belongs to channel /issue, which
			// always resolves an agent assignee. Preserve the ordinary squad path
			// for any future caller that supplies the option with a squad.
			s.enqueueSquadLeaderTask(ctx, issue, pgtype.UUID{}, p.CreatorType, actorID)
		}
	}

	s.publishIssueCreated(issue, attachments, labels, p.CreatorType, actorID, opts)
	s.captureCreatedAnalytics(issue, p.CreatorType, actorID, opts)
	if opts.AssignedAgentRunFireAt.IsZero() {
		assignedTaskID = s.maybeEnqueueOnAssign(ctx, issue, p.CreatorType, actorID, opts.AssignedAgentRunFireAt)
	}

	return IssueCreateResult{Issue: issue, Attachments: attachments, Labels: labels, AssignedTaskID: assignedTaskID}, nil
}

// validateIssueLabels checks that every requested label exists in the
// workspace and is issue-scoped, returning the de-duplicated label rows to
// attach. Returning the full rows (not just ids) lets Create echo an
// authoritative label snapshot on the create response + issue:created event
// without a second query. It mirrors the workspace + resource_type='issue'
// guard already enforced by AttachLabelToIssue so an unknown or wrong-scope id
// surfaces as ErrIssueLabelNotFound instead of a silent no-op insert. Invalid
// (zero) UUIDs are skipped. The label count per issue is small, so a GetLabel
// per distinct id is fine and avoids introducing a new batch query.
func validateIssueLabels(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID, labelIDs []pgtype.UUID) ([]db.IssueLabel, error) {
	if len(labelIDs) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(labelIDs))
	deduped := make([]db.IssueLabel, 0, len(labelIDs))
	for _, labelID := range labelIDs {
		if !labelID.Valid {
			continue
		}
		key := util.UUIDToString(labelID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		label, err := qtx.GetLabel(ctx, db.GetLabelParams{
			ID:          labelID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrIssueLabelNotFound
			}
			return nil, fmt.Errorf("get issue label: %w", err)
		}
		if label.ResourceType != "issue" {
			return nil, ErrIssueLabelNotFound
		}
		deduped = append(deduped, label)
	}
	return deduped, nil
}

// linkAttachments links the given attachment IDs to the newly created
// issue and returns the re-fetched attachment rows so callers can build
// their response without a second query. Errors are logged and swallowed
// — attachment linking is a best-effort post-commit step, and a stale
// attachment row doesn't justify failing the whole create.
func (s *IssueService) linkAttachments(ctx context.Context, issue db.Issue, ids []pgtype.UUID) []db.Attachment {
	if len(ids) == 0 {
		return nil
	}
	if _, err := s.Queries.LinkAttachmentsToIssue(ctx, db.LinkAttachmentsToIssueParams{
		IssueID:       issue.ID,
		WorkspaceID:   issue.WorkspaceID,
		AttachmentIds: ids,
		BumpRevision:  false,
	}); err != nil {
		slog.Error("failed to link attachments to issue",
			"issue_id", util.UUIDToString(issue.ID),
			"error", err)
		return nil
	}
	list, err := s.Queries.ListAttachmentsByIssue(ctx, db.ListAttachmentsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		slog.Warn("failed to list attachments for new issue",
			"issue_id", util.UUIDToString(issue.ID),
			"error", err)
		return nil
	}
	return list
}

func (s *IssueService) publishIssueCreated(issue db.Issue, attachments []db.Attachment, labels []db.IssueLabel, creatorType, actorID string, opts IssueCreateOpts) {
	if s.Bus == nil {
		return
	}
	var payload map[string]any
	if opts.BroadcastPayload != nil {
		payload = opts.BroadcastPayload(issue, attachments, labels)
	} else {
		// Minimal fallback so cache invalidations still fire even if the
		// caller forgot to supply a builder. Front-ends that expect the
		// full IssueResponse must pass BroadcastPayload.
		payload = map[string]any{"issue_id": util.UUIDToString(issue.ID)}
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ActorType:   creatorType,
		ActorID:     actorID,
		Payload:     payload,
	})
}

// PublishAttachmentsChanged refreshes attachments and the issue projection
// after a detached channel media transaction. Issue creation is broadcast
// before the remote download finishes, so the attachment event closes the
// cache gap for current clients. The issue:updated event carries the newly
// materialized description through a pre-existing protocol that installed
// desktop clients already understand.
func (s *IssueService) PublishAttachmentsChanged(ctx context.Context, issue db.Issue, actorID pgtype.UUID) {
	if s.Bus == nil {
		return
	}
	if s.Queries == nil {
		s.publishIssueAttachmentsChanged(issue, actorID, 0)
		return
	}

	current, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		slog.Warn("failed to load issue after channel media bind",
			"issue_id", util.UUIDToString(issue.ID), "error", err)
		s.publishIssueAttachmentsChanged(issue, actorID, 0)
		return
	}
	workspace, err := s.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		slog.Warn("failed to load workspace after channel media bind",
			"workspace_id", util.UUIDToString(issue.WorkspaceID), "error", err)
		// Without the workspace we cannot publish the matching owner snapshot.
		// Keep this auxiliary event unversioned so clients invalidate instead of
		// advancing the owner revision past a snapshot they never received.
		s.publishIssueAttachmentsChanged(issue, actorID, 0)
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ActorType:   "member",
		ActorID:     util.UUIDToString(actorID),
		Payload: map[string]any{
			"issue":            IssueToMapWithCategory(ctx, s.Queries, current, workspace.IssuePrefix),
			"assignee_changed": false,
			"status_changed":   false,
			"project_changed":  false,
		},
	})
	// Publish the auxiliary projection only after the full owner snapshot at
	// this revision. Reversing these two events makes revision-aware clients
	// reject the issue:updated payload as an equal-revision duplicate.
	s.publishIssueAttachmentsChanged(current, actorID, current.Revision)
}

func (s *IssueService) publishIssueAttachmentsChanged(issue db.Issue, actorID pgtype.UUID, revision int64) {
	payload := map[string]any{"issue_id": util.UUIDToString(issue.ID)}
	if revision > 0 {
		payload["issue_revision"] = revision
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventIssueAttachmentsChanged,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ActorType:   "member",
		ActorID:     util.UUIDToString(actorID),
		Payload:     payload,
	})
}

func (s *IssueService) captureCreatedAnalytics(issue db.Issue, creatorType, actorID string, opts IssueCreateOpts) {
	if s.Analytics == nil {
		return
	}
	source, taskID, autopilotRunID := classifyOrigin(issue, opts)
	analyticsActorID := actorID
	if creatorType == "agent" {
		analyticsActorID = "agent:" + actorID
	}
	obsmetrics.RecordEvent(s.Analytics, s.Metrics, analytics.IssueCreated(
		analyticsActorID,
		util.UUIDToString(issue.WorkspaceID),
		util.UUIDToString(issue.ID),
		opts.AnalyticsAgentID,
		taskID,
		autopilotRunID,
		source,
		opts.Platform,
	))
}

// classifyOrigin maps the issue's origin_type / origin_id columns into the
// analytics source labels. Unknown origin_type falls back to SourceManual
// with the warning logged — analytics drift is preferable to dropping the
// event entirely.
func classifyOrigin(issue db.Issue, opts IssueCreateOpts) (source, taskID, autopilotRunID string) {
	source = analytics.SourceManual
	if !issue.OriginType.Valid {
		return source, "", ""
	}
	originID := util.UUIDToString(issue.OriginID)
	switch issue.OriginType.String {
	case "quick_create", "agent_create":
		// Both link the issue back to the agent_task_queue row that created it
		// (agent_create is the ordinary agent `issue create` path, MUL-4305);
		// surface that task id and keep the manual source label.
		return analytics.SourceManual, originID, ""
	case "autopilot":
		return analytics.SourceAutopilot, "", originID
	default:
		slog.Warn("analytics: unknown issue origin type",
			"origin_type", issue.OriginType.String,
			"issue_id", util.UUIDToString(issue.ID),
		)
		return analytics.SourceManual, "", ""
	}
}

func (s *IssueService) maybeEnqueueOnAssign(ctx context.Context, issue db.Issue, creatorType, actorID string, agentRunFireAt time.Time) pgtype.UUID {
	if !issue.AssigneeType.Valid || !issue.AssigneeID.Valid {
		return pgtype.UUID{}
	}
	// Backlog is the parking lot: nothing runs from it, so nothing here needs
	// explaining either. A custom status in the backlog category parks the
	// same way. (MUL-6243)
	if issuestatus.Effective(ctx, s.Queries, issue.WorkspaceID, issue.Status) == "backlog" {
		return pgtype.UUID{}
	}
	verdict, admitted := agentAssigneeVerdict(ctx, s.Queries, issue)
	if !admitted && verdict.Reason == dispatch.ReasonRuntimeUnusable {
		// Assignment has no response the assigner reads for this outcome, so the
		// refusal explains itself on the issue instead of vanishing (MUL-6164).
		// Only here, not in the create-with-assignee path above: that one runs
		// inside the issue's transaction, and a notice about a machine has no
		// business deciding whether the issue itself commits.
		s.noteRuntimeUnusable(ctx, issue, verdict)
	}
	if admitted {
		var task db.AgentTaskQueue
		var err error
		if agentRunFireAt.IsZero() {
			task, err = s.TaskService.EnqueueTaskForIssue(ctx, issue)
		} else {
			task, err = s.TaskService.EnqueueDeferredChannelIssueTask(ctx, issue, agentRunFireAt)
		}
		if err != nil {
			slog.Warn("enqueue agent task on create failed",
				"issue_id", util.UUIDToString(issue.ID),
				"error", err)
		} else {
			return task.ID
		}
	}
	if s.shouldEnqueueSquadLeaderOnAssign(ctx, issue) {
		s.enqueueSquadLeaderTask(ctx, issue, pgtype.UUID{}, creatorType, actorID)
	}
	return pgtype.UUID{}
}

// shouldEnqueueAgentTaskWithQueries returns true when an issue create should
// trigger the assigned agent. Backlog issues are skipped — backlog acts as a
// parking lot for pre-assigning without immediate execution. The assignment
// path does the same test through agentAssigneeVerdict, which also tells it
// WHY a refusal happened; this one runs inside the create transaction, where
// there is nothing to tell anyone yet.
//
// Mirrors handler.shouldEnqueueAgentTask; kept here to make the service
// self-contained, since both code paths must move together.
func (s *IssueService) shouldEnqueueAgentTaskWithQueries(ctx context.Context, q *db.Queries, issue db.Issue) bool {
	// Resolved through q, not s.Queries: this runs inside the create
	// transaction and must see the same snapshot as the rest of it. (MUL-6243)
	if issuestatus.Effective(ctx, q, issue.WorkspaceID, issue.Status) == "backlog" {
		return false
	}
	return isAgentAssigneeReadyWithQueries(ctx, q, issue)
}

func isAgentAssigneeReadyWithQueries(ctx context.Context, q *db.Queries, issue db.Issue) bool {
	_, ok := agentAssigneeVerdict(ctx, q, issue)
	return ok
}

// agentAssigneeVerdict resolves the issue's agent assignee through the shared
// readiness check and reports whether work may be enqueued for it, plus the
// verdict when it may not.
//
// Only a BLOCKED verdict stops the enqueue. A merely offline machine still
// queues: that work runs when the laptop comes back, and people rely on it.
func agentAssigneeVerdict(ctx context.Context, q *db.Queries, issue db.Issue) (AgentVerdict, bool) {
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "agent" || !issue.AssigneeID.Valid {
		return AgentVerdict{}, false
	}
	agent, err := q.GetAgent(ctx, issue.AssigneeID)
	if err != nil {
		return AgentVerdict{}, false
	}
	verdict, err := AgentReadiness(ctx, q, agent)
	if err != nil {
		return AgentVerdict{}, false
	}
	return verdict, !verdict.Blocked()
}

func (s *IssueService) shouldEnqueueSquadLeaderOnAssign(ctx context.Context, issue db.Issue) bool {
	if issuestatus.Effective(ctx, s.Queries, issue.WorkspaceID, issue.Status) == "backlog" {
		return false
	}
	return s.isSquadLeaderReady(ctx, issue)
}

func (s *IssueService) isSquadLeaderReady(ctx context.Context, issue db.Issue) bool {
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "squad" || !issue.AssigneeID.Valid {
		return false
	}
	squad, err := s.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          issue.AssigneeID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return false
	}
	agent, err := s.Queries.GetAgent(ctx, squad.LeaderID)
	if err != nil {
		return false
	}
	verdict, err := AgentReadiness(ctx, s.Queries, agent)
	if err != nil {
		return false
	}
	return verdict.Ready()
}

func (s *IssueService) enqueueSquadLeaderTask(ctx context.Context, issue db.Issue, triggerCommentID pgtype.UUID, authorType, authorID string) {
	squad, err := s.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          issue.AssigneeID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return
	}
	hasPending, err := s.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issue.ID,
		AgentID: squad.LeaderID,
		// Key dedup on the reviewed head (TEN-356).
		HeadSha: headShaText(s.TaskService.ResolveIssueReviewSHA(ctx, issue.ID)),
	})
	if err != nil || hasPending {
		return
	}
	if _, err := s.TaskService.EnqueueTaskForSquadLeader(ctx, issue, squad.LeaderID, squad.ID, triggerCommentID); err != nil {
		slog.Warn("enqueue squad leader task on create failed",
			"issue_id", util.UUIDToString(issue.ID),
			"squad_id", util.UUIDToString(squad.ID),
			"leader_id", util.UUIDToString(squad.LeaderID),
			"error", err)
	}
}
