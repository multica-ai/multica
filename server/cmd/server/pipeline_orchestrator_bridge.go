package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	pipelineEventIssueStatusChanged = "issue.status_changed"
	pipelineEventIssueComment       = "issue.comment_created"
	pipelineEventPRMerged           = "pr.merged"
	pipelineEventPRMergeConflict    = "pr.merge_conflict"
	pipelineEventPRChecksPassed     = "pr.checks_passed"
	pipelineEventPRChecksFailed     = "pr.checks_failed"
	pipelineEventRunFailed          = "run.failed"
)

type pipelineBridgeConfig struct {
	WebhookURL          string
	AllowedProjectIDs   map[string]bool
	OrchestratorActorID string
	Timeout             time.Duration
}

func pipelineBridgeConfigFromEnv() pipelineBridgeConfig {
	return pipelineBridgeConfig{
		WebhookURL:          strings.TrimSpace(os.Getenv("ONESTEP_PIPELINE_ORCHESTRATOR_WEBHOOK_URL")),
		AllowedProjectIDs:   csvSet(os.Getenv("ONESTEP_PIPELINE_PROJECT_IDS")),
		OrchestratorActorID: strings.TrimSpace(os.Getenv("ONESTEP_PIPELINE_ORCHESTRATOR_ACTOR_ID")),
		Timeout:             envDuration("ONESTEP_PIPELINE_ORCHESTRATOR_WEBHOOK_TIMEOUT", 5*time.Second),
	}
}

func registerPipelineOrchestratorBridge(bus *events.Bus, queries *db.Queries, cfg pipelineBridgeConfig) {
	if strings.TrimSpace(cfg.WebhookURL) == "" {
		return
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	bridge := newPipelineOrchestratorBridge(queries, cfg)

	bus.Subscribe(protocol.EventIssueUpdated, bridge.onIssueUpdated)
	bus.Subscribe(protocol.EventCommentCreated, bridge.onCommentCreated)
	bus.Subscribe(protocol.EventPullRequestUpdated, bridge.onPullRequestUpdated)
	bus.Subscribe(protocol.EventAutopilotRunDone, bridge.onAutopilotRunDone)
}

type pipelineOrchestratorBridge struct {
	queries *db.Queries
	cfg     pipelineBridgeConfig
	client  *http.Client

	now   func() time.Time
	async bool

	loadIssueContext        func(context.Context, string) (pipelineIssueContext, bool)
	getAutopilotRun         func(context.Context, pgtype.UUID) (db.AutopilotRun, error)
	listPullRequestsByIssue func(context.Context, pgtype.UUID) ([]db.ListPullRequestsByIssueRow, error)

	mu   sync.Mutex
	seen map[string]struct{}
}

type pipelineWebhookPayload struct {
	EventID       string `json:"event_id"`
	EventType     string `json:"event_type"`
	WorkspaceID   string `json:"workspace_id"`
	ProjectID     string `json:"project_id,omitempty"`
	IssueID       string `json:"issue_id,omitempty"`
	ParentIssueID string `json:"parent_issue_id,omitempty"`
	CommentID     string `json:"comment_id,omitempty"`
	PRNumber      int32  `json:"pr_number,omitempty"`
	ActorType     string `json:"actor_type"`
	ActorID       string `json:"actor_id,omitempty"`
	OccurredAt    string `json:"occurred_at"`
}

type pipelineIssueContext struct {
	Issue          db.Issue
	Parent         *db.Issue
	Metadata       map[string]any
	ParentMetadata map[string]any
}

type pipelineRunTriggerContext struct {
	ProjectID     string
	IssueID       string
	ParentIssueID string
}

func newPipelineOrchestratorBridge(queries *db.Queries, cfg pipelineBridgeConfig) *pipelineOrchestratorBridge {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	b := &pipelineOrchestratorBridge{
		queries: queries,
		cfg:     cfg,
		client:  &http.Client{Timeout: timeout},
		now:     time.Now,
		async:   true,
		seen:    make(map[string]struct{}),
	}
	b.loadIssueContext = b.queryPipelineIssueContext
	if queries != nil {
		b.getAutopilotRun = queries.GetAutopilotRun
		b.listPullRequestsByIssue = queries.ListPullRequestsByIssue
	}
	return b
}

func (b *pipelineOrchestratorBridge) onIssueUpdated(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	if changed, _ := payload["status_changed"].(bool); !changed {
		return
	}
	issue, ok := payload["issue"].(handler.IssueResponse)
	if !ok || issue.ID == "" {
		return
	}
	prevStatus, _ := payload["prev_status"].(string)
	if prevStatus == issue.Status {
		return
	}
	ctx, ok := b.loadIssueContext(context.Background(), issue.ID)
	if !ok || !ctx.isDeliveryIssue(b.cfg.AllowedProjectIDs) {
		return
	}
	if ctx.waitingOnHuman() {
		return
	}

	eventID := stableEventID("issue-status", issue.ID, prevStatus, issue.Status, issue.UpdatedAt)
	b.dispatch(pipelineWebhookPayload{
		EventID:       eventID,
		EventType:     pipelineEventIssueStatusChanged,
		WorkspaceID:   issue.WorkspaceID,
		ProjectID:     uuidPtrString(ctx.effectiveProjectID()),
		IssueID:       issue.ID,
		ParentIssueID: uuidPtrString(ctx.effectiveParentID()),
		ActorType:     coalesceActorType(e.ActorType),
		ActorID:       e.ActorID,
		OccurredAt:    b.now().UTC().Format(time.RFC3339),
	})
}

func (b *pipelineOrchestratorBridge) onCommentCreated(e events.Event) {
	if e.ActorType == "system" {
		return
	}
	if b.cfg.OrchestratorActorID != "" && e.ActorID == b.cfg.OrchestratorActorID {
		return
	}
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	comment, ok := payload["comment"].(handler.CommentResponse)
	if !ok || comment.ID == "" || comment.IssueID == "" {
		return
	}
	keyword, ok := pipelineCommentKeyword(comment.Content)
	if !ok {
		return
	}
	ctx, ok := b.loadIssueContext(context.Background(), comment.IssueID)
	if !ok || !ctx.isDeliveryIssue(b.cfg.AllowedProjectIDs) {
		return
	}
	if ctx.waitingOnHuman() && !(e.ActorType == "member" && isOwnerAcceptanceKeyword(keyword)) {
		return
	}

	b.dispatch(pipelineWebhookPayload{
		EventID:       stableEventID("comment", comment.ID),
		EventType:     pipelineEventIssueComment,
		WorkspaceID:   e.WorkspaceID,
		ProjectID:     uuidPtrString(ctx.effectiveProjectID()),
		IssueID:       comment.IssueID,
		ParentIssueID: uuidPtrString(ctx.effectiveParentID()),
		CommentID:     comment.ID,
		ActorType:     coalesceActorType(e.ActorType),
		ActorID:       e.ActorID,
		OccurredAt:    b.now().UTC().Format(time.RFC3339),
	})
}

func (b *pipelineOrchestratorBridge) onPullRequestUpdated(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	linked := stringSlicePayload(payload["linked_issue_ids"])
	prResp, hasPR := payload["pull_request"].(handler.GitHubPullRequestResponse)
	if hasPR {
		for _, issueID := range linked {
			b.dispatchPullRequestForIssue(e, issueID, prResp)
		}
		return
	}
	for _, issueID := range linked {
		b.dispatchCheckSuitesForIssue(e, issueID)
	}
}

func (b *pipelineOrchestratorBridge) dispatchPullRequestForIssue(e events.Event, issueID string, pr handler.GitHubPullRequestResponse) {
	eventType := ""
	switch {
	case pr.State == "merged":
		eventType = pipelineEventPRMerged
	case pr.MergeableState != nil && *pr.MergeableState == "dirty":
		eventType = pipelineEventPRMergeConflict
	case pr.ChecksConclusion != nil && *pr.ChecksConclusion == "passed":
		eventType = pipelineEventPRChecksPassed
	case pr.ChecksConclusion != nil && *pr.ChecksConclusion == "failed":
		eventType = pipelineEventPRChecksFailed
	default:
		return
	}
	ctx, ok := b.loadIssueContext(context.Background(), issueID)
	if !ok || !ctx.isDeliveryIssue(b.cfg.AllowedProjectIDs) || ctx.waitingOnHuman() {
		return
	}
	b.dispatch(pipelineWebhookPayload{
		EventID:       stableEventID("pr", pr.ID, issueID, eventType, pr.State, ptrString(pr.MergeableState), ptrString(pr.ChecksConclusion), ptrString(pr.MergedAt)),
		EventType:     eventType,
		WorkspaceID:   e.WorkspaceID,
		ProjectID:     uuidPtrString(ctx.effectiveProjectID()),
		IssueID:       issueID,
		ParentIssueID: uuidPtrString(ctx.effectiveParentID()),
		PRNumber:      pr.Number,
		ActorType:     coalesceActorType(e.ActorType),
		ActorID:       e.ActorID,
		OccurredAt:    b.now().UTC().Format(time.RFC3339),
	})
}

func (b *pipelineOrchestratorBridge) dispatchCheckSuitesForIssue(e events.Event, issueID string) {
	if b.listPullRequestsByIssue == nil {
		return
	}
	ctx, ok := b.loadIssueContext(context.Background(), issueID)
	if !ok || !ctx.isDeliveryIssue(b.cfg.AllowedProjectIDs) || ctx.waitingOnHuman() {
		return
	}
	rows, err := b.listPullRequestsByIssue(context.Background(), ctx.Issue.ID)
	if err != nil {
		return
	}
	for _, pr := range rows {
		conclusion := aggregatePipelineChecksConclusion(pr.ChecksFailed, pr.ChecksPassed, pr.ChecksPending, pr.ChecksTotal)
		eventType := ""
		switch conclusion {
		case "passed":
			eventType = pipelineEventPRChecksPassed
		case "failed":
			eventType = pipelineEventPRChecksFailed
		default:
			continue
		}
		b.dispatch(pipelineWebhookPayload{
			EventID:       stableEventID("pr-checks", util.UUIDToString(pr.ID), issueID, conclusion, strconv.FormatInt(pr.ChecksPassed, 10), strconv.FormatInt(pr.ChecksFailed, 10), strconv.FormatInt(pr.ChecksPending, 10)),
			EventType:     eventType,
			WorkspaceID:   e.WorkspaceID,
			ProjectID:     uuidPtrString(ctx.effectiveProjectID()),
			IssueID:       issueID,
			ParentIssueID: uuidPtrString(ctx.effectiveParentID()),
			PRNumber:      pr.PrNumber,
			ActorType:     coalesceActorType(e.ActorType),
			ActorID:       e.ActorID,
			OccurredAt:    b.now().UTC().Format(time.RFC3339),
		})
	}
}

func (b *pipelineOrchestratorBridge) onAutopilotRunDone(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	if status, _ := payload["status"].(string); status != "failed" {
		return
	}
	runID, _ := payload["run_id"].(string)
	if runID == "" {
		return
	}
	if b.getAutopilotRun == nil {
		return
	}
	runUUID, err := util.ParseUUID(runID)
	if err != nil {
		return
	}
	run, err := b.getAutopilotRun(context.Background(), runUUID)
	if err != nil {
		return
	}

	runCtx := pipelineContextFromRunTrigger(run)
	if run.IssueID.Valid {
		runCtx.IssueID = util.UUIDToString(run.IssueID)
	}
	if runCtx.IssueID == "" {
		return
	}
	ctx, ok := b.loadIssueContext(context.Background(), runCtx.IssueID)
	if !ok || !ctx.isDeliveryIssue(b.cfg.AllowedProjectIDs) || ctx.waitingOnHuman() {
		return
	}
	projectID := runCtx.ProjectID
	if projectID == "" {
		projectID = uuidPtrString(ctx.effectiveProjectID())
	}
	parentIssueID := runCtx.ParentIssueID
	if parentIssueID == "" {
		parentIssueID = uuidPtrString(ctx.effectiveParentID())
	}
	b.dispatch(pipelineWebhookPayload{
		EventID:       stableEventID("run-failed", runID),
		EventType:     pipelineEventRunFailed,
		WorkspaceID:   e.WorkspaceID,
		ProjectID:     projectID,
		IssueID:       runCtx.IssueID,
		ParentIssueID: parentIssueID,
		ActorType:     coalesceActorType(e.ActorType),
		ActorID:       e.ActorID,
		OccurredAt:    b.now().UTC().Format(time.RFC3339),
	})
}

func (b *pipelineOrchestratorBridge) dispatch(payload pipelineWebhookPayload) {
	if payload.EventID == "" || payload.EventType == "" || payload.WorkspaceID == "" {
		return
	}
	if !b.markSeen(payload.EventID) {
		return
	}
	deliver := func() {
		if err := b.post(payload); err != nil {
			slog.Warn("pipeline orchestrator bridge: webhook delivery failed",
				"event_id", payload.EventID,
				"event_type", payload.EventType,
				"error", err,
			)
		}
	}
	if b.async {
		go deliver()
	} else {
		deliver()
	}
}

func (b *pipelineOrchestratorBridge) post(payload pipelineWebhookPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, b.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", payload.EventID)
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func (b *pipelineOrchestratorBridge) markSeen(eventID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.seen[eventID]; exists {
		return false
	}
	b.seen[eventID] = struct{}{}
	return true
}

func (b *pipelineOrchestratorBridge) queryPipelineIssueContext(ctx context.Context, issueID string) (pipelineIssueContext, bool) {
	if b.queries == nil || issueID == "" {
		return pipelineIssueContext{}, false
	}
	parsed, err := util.ParseUUID(issueID)
	if err != nil {
		return pipelineIssueContext{}, false
	}
	issue, err := b.queries.GetIssue(ctx, parsed)
	if err != nil {
		return pipelineIssueContext{}, false
	}
	out := pipelineIssueContext{
		Issue:    issue,
		Metadata: issueMetadataMap(issue.Metadata),
	}
	if issue.ParentIssueID.Valid {
		if parent, err := b.queries.GetIssue(ctx, issue.ParentIssueID); err == nil {
			out.Parent = &parent
			out.ParentMetadata = issueMetadataMap(parent.Metadata)
		}
	}
	return out, true
}

func (c pipelineIssueContext) isDeliveryIssue(allowedProjects map[string]bool) bool {
	if !projectAllowed(c.Issue.ProjectID, c.Parent, allowedProjects) {
		return false
	}
	return metadataLooksDelivery(c.Metadata) || metadataLooksDelivery(c.ParentMetadata)
}

func (c pipelineIssueContext) waitingOnHuman() bool {
	return stringMetadata(c.Metadata, "waiting_on") == "human" || stringMetadata(c.ParentMetadata, "waiting_on") == "human"
}

func (c pipelineIssueContext) effectiveParentID() pgtype.UUID {
	if c.Issue.ParentIssueID.Valid {
		return c.Issue.ParentIssueID
	}
	return pgtype.UUID{}
}

func (c pipelineIssueContext) effectiveProjectID() pgtype.UUID {
	if c.Issue.ProjectID.Valid {
		return c.Issue.ProjectID
	}
	if c.Parent != nil && c.Parent.ProjectID.Valid {
		return c.Parent.ProjectID
	}
	return pgtype.UUID{}
}

func metadataLooksDelivery(meta map[string]any) bool {
	if len(meta) == 0 {
		return false
	}
	if boolMetadata(meta, "no_dev") {
		return false
	}
	workflow := stringMetadata(meta, "workflow")
	pipelineStatus := stringMetadata(meta, "pipeline_status")
	if workflow == "discussion" || pipelineStatus == "discussion" {
		return false
	}
	return workflow == "delivery" || strings.HasPrefix(pipelineStatus, "waiting_") || pipelineStatus == "blocked" || pipelineStatus == "complete"
}

func projectAllowed(issueProject pgtype.UUID, parent *db.Issue, allowed map[string]bool) bool {
	if len(allowed) == 0 {
		return true
	}
	if issueProject.Valid && allowed[util.UUIDToString(issueProject)] {
		return true
	}
	return parent != nil && parent.ProjectID.Valid && allowed[util.UUIDToString(parent.ProjectID)]
}

func pipelineCommentKeyword(content string) (string, bool) {
	first := ""
	for _, line := range strings.Split(content, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			first = trimmed
			break
		}
	}
	if first == "" {
		return "", false
	}
	switch strings.ToLower(first) {
	case "pass", "request changes", "blocked":
		return strings.ToLower(first), true
	}
	switch first {
	case "验收通过", "驗收通過":
		return "验收通过", true
	case "验收不通过", "驗收不通過":
		return "验收不通过", true
	}
	return "", false
}

func isOwnerAcceptanceKeyword(keyword string) bool {
	return keyword == "验收通过" || keyword == "验收不通过"
}

func pipelineContextFromRunTrigger(run db.AutopilotRun) pipelineRunTriggerContext {
	if len(run.TriggerPayload) == 0 {
		return pipelineRunTriggerContext{}
	}
	if ctx, ok := pipelineContextFromPayload(run.TriggerPayload); ok {
		return ctx
	}

	var envelope struct {
		EventPayload json.RawMessage `json:"eventPayload"`
	}
	if err := json.Unmarshal(run.TriggerPayload, &envelope); err != nil || len(envelope.EventPayload) == 0 {
		return pipelineRunTriggerContext{}
	}
	ctx, _ := pipelineContextFromPayload(envelope.EventPayload)
	return ctx
}

func pipelineContextFromPayload(raw []byte) (pipelineRunTriggerContext, bool) {
	var payload struct {
		ProjectID     string `json:"project_id"`
		IssueID       string `json:"issue_id"`
		ParentIssueID string `json:"parent_issue_id"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return pipelineRunTriggerContext{}, false
	}
	if payload.ProjectID == "" && payload.IssueID == "" && payload.ParentIssueID == "" {
		return pipelineRunTriggerContext{}, false
	}
	return pipelineRunTriggerContext{
		ProjectID:     payload.ProjectID,
		IssueID:       payload.IssueID,
		ParentIssueID: payload.ParentIssueID,
	}, true
}

func issueMetadataMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil || meta == nil {
		return map[string]any{}
	}
	return meta
}

func stringMetadata(meta map[string]any, key string) string {
	v, _ := meta[key].(string)
	return v
}

func boolMetadata(meta map[string]any, key string) bool {
	v, _ := meta[key].(bool)
	return v
}

func stableEventID(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		clean = append(clean, strings.TrimSpace(part))
	}
	return strings.Join(clean, ":")
}

func coalesceActorType(actorType string) string {
	switch actorType {
	case "member", "agent", "system":
		return actorType
	default:
		return "system"
	}
}

func stringSlicePayload(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func aggregatePipelineChecksConclusion(failed, passed, pending, total int64) string {
	switch {
	case total == 0:
		return ""
	case failed > 0:
		return "failed"
	case pending > 0:
		return "pending"
	case passed > 0:
		return "passed"
	default:
		return ""
	}
}

func uuidPtrString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return util.UUIDToString(id)
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func csvSet(raw string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		item := strings.TrimSpace(part)
		if item != "" {
			out[item] = true
		}
	}
	return out
}
