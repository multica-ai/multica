package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	ghEventPullRequestReview        = "pull_request_review"
	ghEventPullRequestReviewComment = "pull_request_review_comment"

	// At most 2 consecutive automated developer-agent fix rounds may run per
	// (issue, PR) without human intervention in Multica. Round 3+ still syncs
	// the GitHub comment onto the issue timeline for human visibility.
	maxConsecutivePRReviewRounds int32 = 2

	maxInlineDiffHunkLines = 12
)

var prReviewDispatchLocks [64]sync.Mutex

func lockPRReviewIssuePR(issueID, pullRequestID pgtype.UUID) func() {
	h := fnv.New32a()
	_, _ = h.Write(issueID.Bytes[:])
	_, _ = h.Write(pullRequestID.Bytes[:])
	mu := &prReviewDispatchLocks[h.Sum32()%uint32(len(prReviewDispatchLocks))]
	mu.Lock()
	return mu.Unlock
}

type ghWebhookActor struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

type ghPullRequestReviewPayload struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository struct {
		Name    string         `json:"name"`
		Private bool           `json:"private"`
		Owner   ghWebhookActor `json:"owner"`
	} `json:"repository"`
	PullRequest struct {
		Number  int            `json:"number"`
		State   string         `json:"state"`
		Draft   bool           `json:"draft"`
		Merged  bool           `json:"merged"`
		HTMLURL string         `json:"html_url"`
		User    ghWebhookActor `json:"user"`
		Head    struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
	Review struct {
		ID                int64          `json:"id"`
		State             string         `json:"state"`
		Body              string         `json:"body"`
		HTMLURL           string         `json:"html_url"`
		CommitID          string         `json:"commit_id"`
		AuthorAssociation string         `json:"author_association"`
		User              ghWebhookActor `json:"user"`
	} `json:"review"`
}

type ghPullRequestReviewCommentPayload struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository struct {
		Name    string         `json:"name"`
		Private bool           `json:"private"`
		Owner   ghWebhookActor `json:"owner"`
	} `json:"repository"`
	PullRequest struct {
		Number  int            `json:"number"`
		State   string         `json:"state"`
		Draft   bool           `json:"draft"`
		Merged  bool           `json:"merged"`
		HTMLURL string         `json:"html_url"`
		User    ghWebhookActor `json:"user"`
		Head    struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
	Comment struct {
		ID                int64          `json:"id"`
		PullRequestReview int64          `json:"pull_request_review_id"`
		InReplyToID       int64          `json:"in_reply_to_id"`
		Path              string         `json:"path"`
		Line              *int           `json:"line"`
		StartLine         *int           `json:"start_line"`
		OriginalLine      *int           `json:"original_line"`
		OriginalStartLine *int           `json:"original_start_line"`
		DiffHunk          string         `json:"diff_hunk"`
		CommitID          string         `json:"commit_id"`
		Body              string         `json:"body"`
		HTMLURL           string         `json:"html_url"`
		AuthorAssociation string         `json:"author_association"`
		User              ghWebhookActor `json:"user"`
	} `json:"comment"`
}

type ghPRReviewSignal struct {
	eventType      string
	githubEventID  int64
	githubReviewID int64
	installationID int64
	repoOwner      string
	repoName       string
	prNumber       int32
	prState        string
	prHTMLURL      string
	prAuthorLogin  string
	prHeadSHA      string
	actorLogin     string
	reviewState    string
	commitSHA      string
	filePath       string
	startLine      int
	endLine        int
	inReplyToID    int64
	diffHunk       string
	body           string
	htmlURL        string
}

type prReviewDeveloperTarget struct {
	Agent db.Agent
	Squad *db.Squad
}

type prReviewRoundSnapshot struct {
	rounds              int32
	activeRoundTaskID   pgtype.UUID
	activeRoundReviewID pgtype.Int8
}

type prReviewPreparedSync struct {
	alreadyProcessed     bool
	skip                 bool
	issue                db.Issue
	comment              db.Comment
	issueRevision        int64
	delivery             db.GithubPrReviewDelivery
	target               *prReviewDeveloperTarget
	circuitBreakerPaused bool
	coalesceOnly         bool
}

func (h *Handler) handlePullRequestReviewEvent(ctx context.Context, body []byte) {
	var p ghPullRequestReviewPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Warn("github webhook: decode pull_request_review payload failed", "err", err)
		return
	}
	if !strings.EqualFold(p.Action, "submitted") {
		return
	}
	state := strings.ToLower(strings.TrimSpace(p.Review.State))
	trimmedBody := strings.TrimSpace(p.Review.Body)
	switch state {
	case "changes_requested":
		// Actionable even without a top-level summary body.
	case "commented":
		// GitHub emits an empty-body "commented" review alongside inline review
		// comments; those inline comments arrive via pull_request_review_comment.
		if trimmedBody == "" {
			return
		}
	default:
		return
	}
	sig, ok := buildPRReviewSignalBase(
		ghEventPullRequestReview,
		p.Review.ID,
		p.Installation.ID,
		p.Repository.Owner.Login,
		p.Repository.Name,
		p.Repository.Private,
		p.PullRequest.Number,
		derivePRState(p.PullRequest.State, p.PullRequest.Draft, p.PullRequest.Merged),
		p.PullRequest.HTMLURL,
		p.PullRequest.User.Login,
		p.PullRequest.Head.SHA,
		p.Review.User,
		p.Review.AuthorAssociation,
	)
	if !ok {
		return
	}
	sig.githubReviewID = p.Review.ID
	sig.reviewState = state
	sig.commitSHA = strings.TrimSpace(p.Review.CommitID)
	sig.body = trimmedBody
	sig.htmlURL = strings.TrimSpace(p.Review.HTMLURL)
	h.processGitHubPRReviewSignal(ctx, sig)
}

func (h *Handler) handlePullRequestReviewCommentEvent(ctx context.Context, body []byte) {
	var p ghPullRequestReviewCommentPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Warn("github webhook: decode pull_request_review_comment payload failed", "err", err)
		return
	}
	if !strings.EqualFold(p.Action, "created") {
		return
	}
	trimmedBody := strings.TrimSpace(p.Comment.Body)
	if trimmedBody == "" {
		return
	}
	sig, ok := buildPRReviewSignalBase(
		ghEventPullRequestReviewComment,
		p.Comment.ID,
		p.Installation.ID,
		p.Repository.Owner.Login,
		p.Repository.Name,
		p.Repository.Private,
		p.PullRequest.Number,
		derivePRState(p.PullRequest.State, p.PullRequest.Draft, p.PullRequest.Merged),
		p.PullRequest.HTMLURL,
		p.PullRequest.User.Login,
		p.PullRequest.Head.SHA,
		p.Comment.User,
		p.Comment.AuthorAssociation,
	)
	if !ok {
		return
	}
	startLine, endLine := resolveReviewCommentLines(p.Comment.StartLine, p.Comment.Line, p.Comment.OriginalStartLine, p.Comment.OriginalLine)
	sig.githubReviewID = p.Comment.PullRequestReview
	sig.commitSHA = strings.TrimSpace(p.Comment.CommitID)
	sig.filePath = strings.TrimSpace(p.Comment.Path)
	sig.startLine = startLine
	sig.endLine = endLine
	sig.inReplyToID = p.Comment.InReplyToID
	sig.diffHunk = p.Comment.DiffHunk
	sig.body = trimmedBody
	sig.htmlURL = strings.TrimSpace(p.Comment.HTMLURL)
	h.processGitHubPRReviewSignal(ctx, sig)
}

func buildPRReviewSignalBase(
	eventType string,
	githubEventID int64,
	installationID int64,
	repoOwner string,
	repoName string,
	repoPrivate bool,
	prNumber int,
	prState string,
	prHTMLURL string,
	prAuthorLogin string,
	prHeadSHA string,
	actor ghWebhookActor,
	authorAssociation string,
) (ghPRReviewSignal, bool) {
	repoOwner = strings.TrimSpace(repoOwner)
	repoName = strings.TrimSpace(repoName)
	actorLogin := strings.TrimSpace(actor.Login)
	prAuthorLogin = strings.TrimSpace(prAuthorLogin)
	if githubEventID <= 0 || installationID <= 0 || repoOwner == "" || repoName == "" || prNumber <= 0 || prNumber > math.MaxInt32 {
		return ghPRReviewSignal{}, false
	}
	if prState == "merged" || prState == "closed" {
		return ghPRReviewSignal{}, false
	}
	if isBotGitHubActor(actorLogin, actor.Type) {
		return ghPRReviewSignal{}, false
	}
	if prAuthorLogin != "" && strings.EqualFold(actorLogin, prAuthorLogin) {
		return ghPRReviewSignal{}, false
	}
	if !isTrustedGitHubReviewAuthor(repoPrivate, authorAssociation) {
		return ghPRReviewSignal{}, false
	}
	return ghPRReviewSignal{
		eventType:      eventType,
		githubEventID:  githubEventID,
		installationID: installationID,
		repoOwner:      repoOwner,
		repoName:       repoName,
		prNumber:       int32(prNumber),
		prState:        prState,
		prHTMLURL:      strings.TrimSpace(prHTMLURL),
		prAuthorLogin:  prAuthorLogin,
		prHeadSHA:      strings.TrimSpace(prHeadSHA),
		actorLogin:     actorLogin,
	}, true
}

// isTrustedGitHubReviewAuthor rejects drive-by reviews on public repositories
// from outsiders so arbitrary GitHub accounts cannot inject instructions or
// trigger agent runs. On private repositories every user who can submit a
// review already holds explicit repository access.
func isTrustedGitHubReviewAuthor(repoPrivate bool, authorAssociation string) bool {
	if repoPrivate {
		return true
	}
	switch strings.ToUpper(strings.TrimSpace(authorAssociation)) {
	case "OWNER", "MEMBER", "COLLABORATOR":
		return true
	default:
		return false
	}
}

func isBotGitHubActor(login, actorType string) bool {
	login = strings.TrimSpace(login)
	actorType = strings.TrimSpace(actorType)
	if login == "" {
		return true
	}
	if actorType != "" && !strings.EqualFold(actorType, "User") {
		return true
	}
	lower := strings.ToLower(login)
	return strings.HasSuffix(lower, "[bot]")
}

func resolveReviewCommentLines(startLine, line, origStartLine, origLine *int) (start, end int) {
	if line != nil && *line > 0 {
		end = *line
		if startLine != nil && *startLine > 0 {
			start = *startLine
		} else {
			start = end
		}
		return start, end
	}
	if origLine != nil && *origLine > 0 {
		end = *origLine
		if origStartLine != nil && *origStartLine > 0 {
			start = *origStartLine
		} else {
			start = end
		}
		return start, end
	}
	return 0, 0
}

func (h *Handler) processGitHubPRReviewSignal(ctx context.Context, sig ghPRReviewSignal) {
	linked, err := h.Queries.ListLinkedIssuesForGitHubPR(ctx, db.ListLinkedIssuesForGitHubPRParams{
		RepoOwner:      strings.ToLower(sig.repoOwner),
		RepoName:       strings.ToLower(sig.repoName),
		PrNumber:       sig.prNumber,
		InstallationID: sig.installationID,
	})
	if err != nil {
		slog.Warn("github pr review: lookup linked issues failed",
			"installation_id", sig.installationID,
			"repo", sig.repoOwner+"/"+sig.repoName,
			"pr", sig.prNumber,
			"err", err,
		)
		return
	}
	if len(linked) == 0 {
		return
	}
	for _, row := range linked {
		if !githubFeaturesEnabled(row.Workspace) {
			continue
		}
		if row.PrState == "merged" || row.PrState == "closed" {
			continue
		}
		if row.PrAuthorLogin != "" && strings.EqualFold(sig.actorLogin, row.PrAuthorLogin) {
			continue
		}
		h.syncGitHubPRReviewToIssue(ctx, sig, row)
	}
}

func (h *Handler) syncGitHubPRReviewToIssue(ctx context.Context, sig ghPRReviewSignal, row db.ListLinkedIssuesForGitHubPRRow) {
	// Serialize comment sync + task coalesce/enqueue for one (issue, PR) so a
	// simultaneous burst of inline review comments folds into one queued task
	// rather than racing separate top-level thread inserts.
	unlock := lockPRReviewIssuePR(row.Issue.ID, row.PullRequestID)
	defer unlock()

	prepared, err := h.prepareGitHubPRReviewSync(ctx, sig, row)
	if err != nil {
		slog.Warn("github pr review: prepare issue sync failed",
			"issue_id", uuidToString(row.Issue.ID),
			"pr_id", uuidToString(row.PullRequestID),
			"event_type", sig.eventType,
			"github_event_id", sig.githubEventID,
			"err", err,
		)
		return
	}
	if prepared.alreadyProcessed || prepared.skip {
		return
	}

	h.publish(protocol.EventCommentCreated, uuidToString(prepared.issue.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(prepared.comment, nil, nil),
		"issue_title":         prepared.issue.Title,
		"issue_assignee_type": textToPtr(prepared.issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(prepared.issue.AssigneeID),
		"issue_status":        prepared.issue.Status,
		"issue_revision":      prepared.issueRevision,
	})

	if prepared.target == nil || prepared.circuitBreakerPaused {
		return
	}
	h.dispatchGitHubPRReviewTask(
		ctx,
		prepared.issue,
		row.PullRequestID,
		prepared.delivery.ID,
		prepared.comment.ID,
		prepared.target,
		sig.githubReviewID,
		prepared.coalesceOnly,
	)
}

func (h *Handler) prepareGitHubPRReviewSync(ctx context.Context, sig ghPRReviewSignal, row db.ListLinkedIssuesForGitHubPRRow) (prReviewPreparedSync, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return prReviewPreparedSync{}, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockGitHubPRReviewSync(ctx, db.LockGitHubPRReviewSyncParams{
		IssueID:       row.Issue.ID,
		PullRequestID: row.PullRequestID,
	}); err != nil {
		return prReviewPreparedSync{}, err
	}

	exists, err := qtx.ExistsGitHubPRReviewDelivery(ctx, db.ExistsGitHubPRReviewDeliveryParams{
		IssueID:       row.Issue.ID,
		EventType:     sig.eventType,
		GithubEventID: sig.githubEventID,
	})
	if err != nil {
		return prReviewPreparedSync{}, err
	}
	if exists {
		return prReviewPreparedSync{alreadyProcessed: true}, nil
	}

	issue, err := qtx.GetIssue(ctx, row.Issue.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return prReviewPreparedSync{skip: true}, nil
		}
		return prReviewPreparedSync{}, err
	}
	if issue.TriageState.Valid {
		return prReviewPreparedSync{skip: true}, nil
	}
	cat := issuestatus.Category(ctx, qtx, issue.WorkspaceID, issue.Status)
	if cat == issuestatus.CategoryDone || cat == issuestatus.CategoryClosed {
		return prReviewPreparedSync{skip: true}, nil
	}

	target := h.resolvePRReviewDeveloperTarget(ctx, qtx, issue, row.LinkedByID)
	var circuitBreakerPaused bool
	var coalesceOnly bool
	if target != nil {
		snap, err := h.effectivePRReviewRounds(ctx, qtx, issue, row.PullRequestID)
		if err != nil {
			return prReviewPreparedSync{}, err
		}
		sameReviewSubmission := sig.githubReviewID > 0 &&
			snap.activeRoundReviewID.Valid &&
			snap.activeRoundReviewID.Int64 == sig.githubReviewID
		if snap.rounds >= maxConsecutivePRReviewRounds && !sameReviewSubmission {
			var headSha pgtype.Text
			if h.TaskService != nil {
				headSha = h.TaskService.ResolveIssueReviewSHAParam(ctx, issue.ID)
			}
			var squadID pgtype.UUID
			if target.Squad != nil {
				squadID = target.Squad.ID
			}
			hasQueued, err := qtx.HasQueuedTaskForIssueAndAgent(ctx, db.HasQueuedTaskForIssueAndAgentParams{
				IssueID:          issue.ID,
				AgentID:          target.Agent.ID,
				NewLeaderSquadID: squadID,
				HeadSha:          headSha,
			})
			if err != nil {
				return prReviewPreparedSync{}, err
			}
			if hasQueued {
				coalesceOnly = true
			} else {
				circuitBreakerPaused = true
			}
		}
	}

	prURL := sig.prHTMLURL
	if prURL == "" {
		prURL = row.PrHtmlUrl
	}
	content := formatGitHubPRReviewComment(sig, prURL, circuitBreakerPaused)

	commentID := dbid.NewV7()
	created, err := qtx.CreateComment(ctx, db.CreateCommentParams{
		ID:          commentID,
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     content,
		Type:        "comment",
	})
	if err != nil {
		return prReviewPreparedSync{}, err
	}

	delivery, err := qtx.CreateGitHubPRReviewDelivery(ctx, db.CreateGitHubPRReviewDeliveryParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   issue.WorkspaceID,
		IssueID:       issue.ID,
		PullRequestID: row.PullRequestID,
		EventType:     sig.eventType,
		GithubEventID: sig.githubEventID,
		CommentID:     commentID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return prReviewPreparedSync{alreadyProcessed: true}, nil
		}
		return prReviewPreparedSync{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return prReviewPreparedSync{}, err
	}
	return prReviewPreparedSync{
		issue:                issue,
		comment:              created.Comment(),
		issueRevision:        created.IssueRevision,
		delivery:             delivery,
		target:               target,
		circuitBreakerPaused: circuitBreakerPaused,
		coalesceOnly:         coalesceOnly,
	}, nil
}

func (h *Handler) effectivePRReviewRounds(ctx context.Context, q *db.Queries, issue db.Issue, pullRequestID pgtype.UUID) (prReviewRoundSnapshot, error) {
	state, err := q.GetGitHubPRReviewWakeupState(ctx, db.GetGitHubPRReviewWakeupStateParams{
		IssueID:       issue.ID,
		PullRequestID: pullRequestID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return prReviewRoundSnapshot{}, nil
		}
		return prReviewRoundSnapshot{}, err
	}
	if state.ConsecutiveRounds <= 0 {
		return prReviewRoundSnapshot{
			activeRoundTaskID:   state.ActiveRoundTaskID,
			activeRoundReviewID: state.ActiveRoundReviewID,
		}, nil
	}
	intervened, err := q.HasHumanIssueInterventionSince(ctx, db.HasHumanIssueInterventionSinceParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Since:       state.LastTriggeredAt,
	})
	if err != nil {
		return prReviewRoundSnapshot{}, err
	}
	if intervened {
		return prReviewRoundSnapshot{}, nil
	}
	return prReviewRoundSnapshot{
		rounds:              state.ConsecutiveRounds,
		activeRoundTaskID:   state.ActiveRoundTaskID,
		activeRoundReviewID: state.ActiveRoundReviewID,
	}, nil
}

func (h *Handler) dispatchGitHubPRReviewTask(
	ctx context.Context,
	issue db.Issue,
	pullRequestID pgtype.UUID,
	deliveryID pgtype.UUID,
	commentID pgtype.UUID,
	target *prReviewDeveloperTarget,
	githubReviewID int64,
	coalesceOnly bool,
) {
	if h.TaskService == nil || target == nil {
		return
	}
	headSha := h.TaskService.ResolveIssueReviewSHAParam(ctx, issue.ID)
	var squadID pgtype.UUID
	if target.Squad != nil {
		squadID = target.Squad.ID
	}

	tryCoalesce := func() (db.AgentTaskQueue, bool) {
		merged, err := h.Queries.CoalesceGitHubPRReviewCommentIntoQueuedTask(ctx, db.CoalesceGitHubPRReviewCommentIntoQueuedTaskParams{
			NewTriggerCommentID: commentID,
			NewLeaderSquadID:    squadID,
			NewTriggerSummary:   h.TaskService.BuildCommentTriggerSummary(ctx, issue.WorkspaceID, commentID),
			IssueID:             issue.ID,
			AgentID:             target.Agent.ID,
			HeadSha:             headSha,
		})
		if err != nil {
			if !isNotFound(err) {
				slog.Warn("github pr review: coalesce queued task failed",
					"issue_id", uuidToString(issue.ID),
					"agent_id", uuidToString(target.Agent.ID),
					"err", err,
				)
			}
			return db.AgentTaskQueue{}, false
		}
		h.TaskService.BroadcastTaskCoalesced(ctx, merged)
		return merged, true
	}

	if coalesceOnly {
		if merged, ok := tryCoalesce(); ok {
			h.recordGitHubPRReviewTaskRound(ctx, issue, pullRequestID, deliveryID, merged.ID, githubReviewID)
		}
		return
	}

	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if merged, ok := tryCoalesce(); ok {
			h.recordGitHubPRReviewTaskRound(ctx, issue, pullRequestID, deliveryID, merged.ID, githubReviewID)
			return
		}

		var enqueued db.AgentTaskQueue
		var err error
		if target.Squad != nil {
			enqueued, err = h.TaskService.EnqueueTaskForSquadLeader(ctx, issue, target.Agent.ID, target.Squad.ID, commentID, service.OriginDerived)
		} else {
			enqueued, err = h.TaskService.EnqueueTaskForMention(ctx, issue, target.Agent.ID, commentID, service.OriginDerived)
		}
		if err == nil {
			h.recordGitHubPRReviewTaskRound(ctx, issue, pullRequestID, deliveryID, enqueued.ID, githubReviewID)
			return
		}
		if !errors.Is(err, service.ErrDuplicatePendingTask) {
			slog.Warn("github pr review: enqueue developer agent failed",
				"issue_id", uuidToString(issue.ID),
				"agent_id", uuidToString(target.Agent.ID),
				"err", err,
			)
			return
		}

		if merged, ok := tryCoalesce(); ok {
			h.recordGitHubPRReviewTaskRound(ctx, issue, pullRequestID, deliveryID, merged.ID, githubReviewID)
			return
		}
	}
}

func (h *Handler) recordGitHubPRReviewTaskRound(
	ctx context.Context,
	issue db.Issue,
	pullRequestID pgtype.UUID,
	deliveryID pgtype.UUID,
	taskID pgtype.UUID,
	githubReviewID int64,
) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		slog.Warn("github pr review: begin round state tx failed", "err", err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockGitHubPRReviewSync(ctx, db.LockGitHubPRReviewSyncParams{
		IssueID:       issue.ID,
		PullRequestID: pullRequestID,
	}); err != nil {
		slog.Warn("github pr review: lock round state failed", "err", err)
		return
	}

	if deliveryID.Valid {
		if err := qtx.SetGitHubPRReviewDeliveryTask(ctx, db.SetGitHubPRReviewDeliveryTaskParams{
			ID:              deliveryID,
			TriggeredTaskID: taskID,
		}); err != nil {
			slog.Warn("github pr review: stamp delivery task failed", "err", err)
		}
	}

	snap, err := h.effectivePRReviewRounds(ctx, qtx, issue, pullRequestID)
	if err != nil {
		slog.Warn("github pr review: read wakeup state failed", "err", err)
		return
	}
	sameTask := snap.activeRoundTaskID.Valid && snap.activeRoundTaskID == taskID
	sameReview := githubReviewID > 0 &&
		snap.activeRoundReviewID.Valid &&
		snap.activeRoundReviewID.Int64 == githubReviewID

	nextRounds := snap.rounds
	if sameTask || sameReview {
		if nextRounds < 1 {
			nextRounds = 1
		}
	} else {
		nextRounds = snap.rounds + 1
	}

	reviewParam := snap.activeRoundReviewID
	if githubReviewID > 0 {
		reviewParam = pgtype.Int8{Int64: githubReviewID, Valid: true}
	} else if !sameTask {
		reviewParam = pgtype.Int8{}
	}

	if _, err := qtx.UpsertGitHubPRReviewWakeupState(ctx, db.UpsertGitHubPRReviewWakeupStateParams{
		IssueID:             issue.ID,
		PullRequestID:       pullRequestID,
		WorkspaceID:         issue.WorkspaceID,
		ConsecutiveRounds:   nextRounds,
		ActiveRoundTaskID:   taskID,
		ActiveRoundReviewID: reviewParam,
	}); err != nil {
		slog.Warn("github pr review: upsert wakeup state failed", "err", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Warn("github pr review: commit wakeup state failed", "err", err)
	}
}

func isCodingWorkflowStatus(status string) bool {
	switch status {
	case issuestatus.Backlog, issuestatus.Todo, issuestatus.InProgress:
		return true
	default:
		return false
	}
}

func (h *Handler) resolvePRReviewDeveloperTarget(
	ctx context.Context,
	q *db.Queries,
	issue db.Issue,
	prLinkedByID pgtype.UUID,
) *prReviewDeveloperTarget {
	var isReviewHandoffStep bool
	var handoffAssigneeType string
	var handoffAssigneeID pgtype.UUID
	def, project, err := issueworkflow.ForProject(ctx, q, issue.WorkspaceID, issue.ProjectID)
	if err == nil && def == nil {
		def, err = issueworkflow.OutsideProject(ctx, q, issue.WorkspaceID)
	}
	if err == nil && def != nil {
		if step, ok := def.Step(issue.Status); ok && step.HandsOff() && !isCodingWorkflowStatus(issue.Status) {
			isReviewHandoffStep = true
			if resolvedType, resolvedID, ok := issueworkflow.ResolveHandler(step, project, issue.CreatorType, issue.CreatorID); ok {
				handoffAssigneeType = resolvedType
				handoffAssigneeID = resolvedID
			}
		}
	}

	isReviewHandler := func(agentID, squadID pgtype.UUID) bool {
		if !isReviewHandoffStep {
			return false
		}
		if handoffAssigneeType == "agent" && agentID.Valid && handoffAssigneeID == agentID {
			return true
		}
		if handoffAssigneeType == "squad" && squadID.Valid && handoffAssigneeID == squadID {
			return true
		}
		return false
	}

	recentTasks, _ := q.ListRunnableIssueDeveloperTasks(ctx, db.ListRunnableIssueDeveloperTasksParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})

	workspaceIDStr := uuidToString(issue.WorkspaceID)
	var issueOriginatorUserID string
	if issue.CreatorType == "member" && issue.CreatorID.Valid {
		issueOriginatorUserID = uuidToString(issue.CreatorID)
	} else {
		for _, t := range recentTasks {
			if t.OriginatorUserID.Valid {
				issueOriginatorUserID = uuidToString(t.OriginatorUserID)
				break
			}
		}
	}

	loadReadyAgent := func(agentID pgtype.UUID) (db.Agent, bool) {
		if !agentID.Valid {
			return db.Agent{}, false
		}
		agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          agentID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil || agent.ArchivedAt.Valid || !agent.RuntimeID.Valid || agent.Kind != "user" {
			return db.Agent{}, false
		}
		if !h.canInvokeAgent(ctx, agent, "system", "", issueOriginatorUserID, workspaceIDStr) {
			return db.Agent{}, false
		}
		if verdict, err := service.AgentReadiness(ctx, h.runtimeLookup(obsmetrics.RuntimeLookupSourceComment), agent); err != nil || verdict.Blocked() {
			return db.Agent{}, false
		}
		return agent, true
	}

	loadReadySquadTarget := func(squadID pgtype.UUID) *prReviewDeveloperTarget {
		if !squadID.Valid || isReviewHandler(pgtype.UUID{}, squadID) {
			return nil
		}
		squad, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          squadID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil || squad.ArchivedAt.Valid || !squad.LeaderID.Valid {
			return nil
		}
		if isReviewHandler(squad.LeaderID, squad.ID) {
			return nil
		}
		leader, ok := loadReadyAgent(squad.LeaderID)
		if !ok {
			return nil
		}
		return &prReviewDeveloperTarget{Agent: leader, Squad: &squad}
	}

	taskSquadID := func(t db.ListRunnableIssueDeveloperTasksRow) pgtype.UUID {
		if t.SquadID.Valid {
			return t.SquadID
		}
		return t.UsageSquadID
	}

	squadTargetForAgentFromHistory := func(agentID pgtype.UUID) *prReviewDeveloperTarget {
		for _, t := range recentTasks {
			if t.AgentID != agentID {
				continue
			}
			if sid := taskSquadID(t); sid.Valid {
				if target := loadReadySquadTarget(sid); target != nil {
					return target
				}
			}
		}
		return nil
	}

	// 1. If currently assigned to a non-review agent or squad, wake the current assignee.
	if issue.AssigneeType.Valid && issue.AssigneeID.Valid {
		switch issue.AssigneeType.String {
		case "agent":
			if !isReviewHandler(issue.AssigneeID, pgtype.UUID{}) {
				if squadTarget := squadTargetForAgentFromHistory(issue.AssigneeID); squadTarget != nil && squadTarget.Agent.ID == issue.AssigneeID {
					return squadTarget
				}
				if agent, ok := loadReadyAgent(issue.AssigneeID); ok {
					return &prReviewDeveloperTarget{Agent: agent}
				}
			}
		case "squad":
			if target := loadReadySquadTarget(issue.AssigneeID); target != nil {
				return target
			}
		}
	}

	// 2. When moved to review/shipping, prefer the agent/squad that linked the PR
	// (routing squad workers through their squad leader).
	if prLinkedByID.Valid {
		if squadTarget := squadTargetForAgentFromHistory(prLinkedByID); squadTarget != nil {
			return squadTarget
		}
		if !isReviewHandler(prLinkedByID, pgtype.UUID{}) {
			if agent, ok := loadReadyAgent(prLinkedByID); ok {
				return &prReviewDeveloperTarget{Agent: agent}
			}
		}
	}

	// 3. Otherwise inspect prior non-cancelled task history before the review step.
	for _, t := range recentTasks {
		sid := taskSquadID(t)
		if isReviewHandoffStep && (t.WorkflowStep == issue.Status || isReviewHandler(t.AgentID, sid)) {
			continue
		}
		if sid.Valid {
			if target := loadReadySquadTarget(sid); target != nil {
				return target
			}
		}
		if !isReviewHandler(t.AgentID, pgtype.UUID{}) {
			if agent, ok := loadReadyAgent(t.AgentID); ok {
				return &prReviewDeveloperTarget{Agent: agent}
			}
		}
	}

	return nil
}

func formatGitHubPRReviewComment(sig ghPRReviewSignal, prURL string, circuitBreakerPaused bool) string {
	var b strings.Builder
	prRef := fmt.Sprintf("%s/%s#%d", sig.repoOwner, sig.repoName, sig.prNumber)
	if prURL != "" {
		prRef = fmt.Sprintf("[%s](%s)", prRef, prURL)
	}
	actor := escapeMarkdownInline(sig.actorLogin)

	switch sig.eventType {
	case ghEventPullRequestReview:
		label := "Commented"
		if sig.reviewState == "changes_requested" {
			label = "Changes requested"
		}
		fmt.Fprintf(&b, "**GitHub PR Review — %s** on %s by **@%s**", label, prRef, actor)
		if sig.htmlURL != "" {
			fmt.Fprintf(&b, " ([view review](%s))", sig.htmlURL)
		}
		b.WriteString("\n")
		if short := shortCommitSHA(sig.commitSHA); short != "" {
			fmt.Fprintf(&b, "- Commit: `%s`\n", short)
		}
		b.WriteString("\n")
		if sig.body != "" {
			b.WriteString(sig.body)
		} else {
			b.WriteString("_Reviewer requested changes without a top-level summary comment._")
		}

	case ghEventPullRequestReviewComment:
		fmt.Fprintf(&b, "**GitHub PR Review Comment** on %s by **@%s**", prRef, actor)
		if sig.htmlURL != "" {
			fmt.Fprintf(&b, " ([view comment](%s))", sig.htmlURL)
		}
		b.WriteString("\n")
		if sig.filePath != "" {
			lineLabel := ""
			if sig.endLine > 0 {
				if sig.startLine > 0 && sig.startLine != sig.endLine {
					lineLabel = fmt.Sprintf(" (lines %d-%d)", sig.startLine, sig.endLine)
				} else {
					lineLabel = fmt.Sprintf(" (line %d)", sig.endLine)
				}
			}
			fmt.Fprintf(&b, "- File: `%s`%s\n", strings.ReplaceAll(sig.filePath, "`", "'"), lineLabel)
		}
		if short := shortCommitSHA(sig.commitSHA); short != "" {
			fmt.Fprintf(&b, "- Commit: `%s`\n", short)
		}
		if sig.inReplyToID > 0 {
			fmt.Fprintf(&b, "- Reply in thread: `#discussion_r%d`\n", sig.inReplyToID)
		}
		if hunk := trimDiffHunk(sig.diffHunk, maxInlineDiffHunkLines); hunk != "" {
			b.WriteString("\n```diff\n")
			b.WriteString(hunk)
			b.WriteString("\n```\n")
		}
		b.WriteString("\n")
		b.WriteString(sig.body)
	}

	if circuitBreakerPaused {
		b.WriteString("\n\n---\n> **Auto-wakeup paused**: Multica already triggered 2 consecutive automated PR review rounds on this PR without human intervention in Multica. Review the feedback above and leave a comment or click **Rerun** on this issue to start another agent pass.")
	}

	out := sanitizeNullBytes(b.String())
	if len(out) > maxCommentContentBytes {
		out = truncateUTF8Bytes(out, maxCommentContentBytes-32) + "\n\n_(truncated)_"
	}
	return out
}

func shortCommitSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func trimDiffHunk(hunk string, maxLines int) string {
	hunk = strings.TrimRight(hunk, "\r\n")
	if strings.TrimSpace(hunk) == "" || maxLines <= 0 {
		return ""
	}
	lines := strings.Split(hunk, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}

func truncateUTF8Bytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && (s[maxBytes]&0xC0) == 0x80 {
		maxBytes--
	}
	return s[:maxBytes]
}
