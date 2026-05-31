package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	feishuProjectBaseURL = "https://project.feishu.cn"
	feishuProjectMCPURL  = "https://project.feishu.cn/mcp_server/v1"

	feishuProjectSyncPageSize      = 100
	feishuProjectInitialLookback   = 24 * time.Hour
	feishuProjectManualLookback    = 30 * 24 * time.Hour
	feishuProjectIncrementalReplay = 10 * time.Minute
	feishuProjectSyncMaxPages      = 1000

	// How often a successful reconcile should run, and how far back its
	// `updated_at` filter looks. Reconcile is the L3 defence that catches
	// updates the incremental L1 missed (long worker stalls, clock skew,
	// out-of-order events). The +30min safety margin overshoots the
	// 6h interval so we always overlap the previous reconcile window.
	feishuProjectReconcileInterval       = 6 * time.Hour
	feishuProjectReconcileLookback       = 6 * time.Hour
	feishuProjectReconcileLookbackSafety = 30 * time.Minute
	feishuProjectAttachmentMaxSize       = 5 << 20
	// Feishu Project caps every (token, single interface) pair at 15 QPS
	// (project.feishu.cn/b/helpcenter/1p8d7djs/4bsmoql6). The advisory lock keeps
	// only one replica syncing a given integration, so worker fan-out is the only
	// thing that can blow past 15 concurrent calls to the same endpoint. 10 leaves
	// headroom for the bursty start of each page without crossing the cap.
	feishuProjectSyncWorkers           = 10
	feishuProjectSlowItemLogAfter      = 500 * time.Millisecond
	feishuProjectAPIRetryAttempts      = 4
	feishuProjectDownloadRetryAttempts = 3
	// Cap on any per-attempt sleep we'll honor from Feishu's rate-limit reset
	// header, so a misbehaving gateway response can't stall the worker for hours.
	feishuProjectRateLimitMaxSleep = 60 * time.Second
)

// Initial backoff for attachment-download retries. Subsequent attempts use exponential backoff.
// Declared as a var so tests can shrink it.
var feishuProjectDownloadRetryInitialDelay = 300 * time.Millisecond

var ErrFeishuProjectSyncScopeRequired = errors.New("Feishu Project sync requires a bounded sync scope before searching work items")

// FeishuProjectReconcileInterval is the cadence at which a successful
// reconcile run is expected. Exposed for the cmd-level worker scheduler
// to keep the constant in one place.
func FeishuProjectReconcileInterval() time.Duration { return feishuProjectReconcileInterval }

// ErrFeishuProjectQuotaExhausted is returned when the tenant has consumed its
// monthly Feishu Open Platform quota (err_code 99991403, enforced since
// 2024-12-03). Retries within the same calendar month are futile; the worker
// should surface this rather than burn retry budget on calls that cannot succeed.
var ErrFeishuProjectQuotaExhausted = errors.New("Feishu Project API monthly quota exhausted")

type FeishuProjectTxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type FeishuProjectSyncService struct {
	Queries     *db.Queries
	Tx          FeishuProjectTxStarter
	Client      *FeishuProjectClient
	Storage     FeishuProjectStorage
	TaskService *TaskService
}

type FeishuProjectStorage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error)
}

type FeishuProjectSyncSummary struct {
	Created          int `json:"created"`
	Updated          int `json:"updated"`
	Skipped          int `json:"skipped"`
	Errors           int `json:"errors"`
	AttachmentErrors int `json:"attachment_errors"`
}

type FeishuProjectSyncOptions struct {
	WorkItemID string

	// SinceUnixMilli, if > 0, overrides the `updated_at >=` lookback start
	// passed to Feishu's /work_item/filter call. Sync entry computes this
	// from the trigger + cfg watermark and threads it through so the policy
	// lives in one place. Zero means use the legacy per-cfg/fullSync default.
	SinceUnixMilli int64
}

type FeishuProjectWorkItemPage struct {
	Items   []FeishuProjectWorkItem
	PageNum int
	Total   int
}

type FeishuProjectClient struct {
	HTTPClient        *http.Client
	BaseURL           string
	MCPURL            string
	pluginTokenMu     sync.Mutex
	cachedPluginToken string
	pluginTokenKey    string
	pluginTokenTill   time.Time
}

type FeishuProjectWorkItem struct {
	ID                 string
	Type               string
	Title              string
	Description        string
	Status             string
	Priority           string
	OwnerEmail         string
	UpdatedAt          time.Time
	URL                string
	Attachments        []FeishuProjectAttachment
	BusinessLineTokens []FeishuBusinessLineToken
	FieldValues        map[string][]string
}

type FeishuProjectLabelSyncRule struct {
	ID        string `json:"id"`
	Enabled   bool   `json:"enabled"`
	FieldKey  string `json:"field_key"`
	FieldName string `json:"field_name"`
	Match     string `json:"match"`
	LabelName string `json:"label_name"`
}

// FeishuBusinessLineToken represents one biz-line value attached to a work item,
// extracted from the field designated by FeishuProjectIntegration.BusinessLineFieldKey.
// Either ID or Name may be empty depending on what Meego returned.
type FeishuBusinessLineToken struct {
	ID         string
	Name       string
	ParentID   string
	ParentName string
}

// FeishuProjectFieldOption is one selectable option (used for biz-line nodes).
type FeishuProjectFieldOption struct {
	ID         string                     `json:"id"`
	Name       string                     `json:"name"`
	ParentID   string                     `json:"parent_id,omitempty"`
	ParentName string                     `json:"parent_name,omitempty"`
	Children   []FeishuProjectFieldOption `json:"children,omitempty"`
}

// FeishuProjectFieldMeta is one field on a work-item type.
type FeishuProjectFieldMeta struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type FeishuProjectAttachment struct {
	ID          string
	Name        string
	URL         string
	ContentType string
	SizeBytes   int64
}

type FeishuProjectStatusOption struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type feishuProjectSyncTiming struct {
	ownerLookup             time.Duration
	bindingLookup           time.Duration
	issueLookup             time.Duration
	issueUpdate             time.Duration
	issueCreate             time.Duration
	bindingUpsert           time.Duration
	attachments             time.Duration
	attachmentList          time.Duration
	attachmentDownload      time.Duration
	attachmentUpload        time.Duration
	attachmentDB            time.Duration
	attachmentsUploaded     int
	attachmentsSkippedLarge int
	attachmentsSkippedError int
	attachmentsExisting     int
}

func NewFeishuProjectClient() *FeishuProjectClient {
	return &FeishuProjectClient{
		HTTPClient: &http.Client{Timeout: 20 * time.Second},
		BaseURL:    feishuProjectBaseURL,
		MCPURL:     feishuProjectMCPURL,
	}
}

func (s *FeishuProjectSyncService) Sync(ctx context.Context, cfg db.FeishuProjectIntegration, trigger string) (FeishuProjectSyncSummary, error) {
	return s.SyncWithOptions(ctx, cfg, trigger, FeishuProjectSyncOptions{})
}

func (s *FeishuProjectSyncService) SyncWithOptions(ctx context.Context, cfg db.FeishuProjectIntegration, trigger string, opts FeishuProjectSyncOptions) (FeishuProjectSyncSummary, error) {
	if s.Client == nil {
		s.Client = NewFeishuProjectClient()
	}
	run, _ := s.Queries.CreateFeishuProjectSyncRun(ctx, db.CreateFeishuProjectSyncRunParams{
		IntegrationID: cfg.ID,
		WorkspaceID:   cfg.WorkspaceID,
		Status:        "running",
		Trigger:       trigger,
	})
	if !run.ID.Valid {
		slog.Warn("Feishu Project sync run creation failed", "integration_id", UUIDString(cfg.ID), "project_key", cfg.ProjectKey, "trigger", trigger)
	}
	return s.SyncWithRunAndOptions(ctx, cfg, trigger, run, opts)
}

func (s *FeishuProjectSyncService) SyncWithRun(ctx context.Context, cfg db.FeishuProjectIntegration, trigger string, run db.FeishuProjectSyncRun) (FeishuProjectSyncSummary, error) {
	return s.SyncWithRunAndOptions(ctx, cfg, trigger, run, FeishuProjectSyncOptions{})
}

func (s *FeishuProjectSyncService) SyncWithRunAndOptions(ctx context.Context, cfg db.FeishuProjectIntegration, trigger string, run db.FeishuProjectSyncRun, opts FeishuProjectSyncOptions) (FeishuProjectSyncSummary, error) {
	if s.Client == nil {
		s.Client = NewFeishuProjectClient()
	}
	summary := FeishuProjectSyncSummary{}
	var summaryMu sync.Mutex
	var syncErr error
	// Manual triggers do a 30-day bootstrap; reconcile triggers do a 6h30m
	// reconcile pass. Both share the larger-lookback + light-update-existing
	// path so a no-cost attachment refetch isn't triggered for items whose
	// external_id is already bound.
	fullSync := trigger == "manual" || trigger == "reconcile"
	lightUpdateExisting := fullSync && strings.TrimSpace(opts.WorkItemID) == ""
	if opts.SinceUnixMilli == 0 {
		opts.SinceUnixMilli = feishuProjectSinceUnixMilliForTrigger(cfg, trigger, time.Now())
	}
	// Tracks max(item.updated_at) across all goroutines so the watermark is
	// pinned to Feishu's clock, not ours.
	var maxObservedUpdatedAtMs atomic.Int64
	totalCount := 0
	for _, typ := range enabledFeishuProjectTypes(cfg) {
		err := s.Client.QueryWorkItemPagesWithOptions(ctx, cfg, typ, fullSync, opts, func(page FeishuProjectWorkItemPage) error {
			if page.Total > totalCount {
				totalCount = page.Total
			}
			s.updateRunProgress(ctx, run.ID, summary, totalCount, page.PageNum, typ)
			workerCount := feishuProjectSyncWorkers
			if len(page.Items) < workerCount {
				workerCount = len(page.Items)
			}
			if workerCount < 1 {
				return nil
			}
			jobs := make(chan FeishuProjectWorkItem)
			var wg sync.WaitGroup
			for range workerCount {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for item := range jobs {
						if ctx.Err() != nil {
							continue
						}
						result, attachErrs, err := s.syncWorkItem(ctx, cfg, item, lightUpdateExisting)
						if err == nil && !item.UpdatedAt.IsZero() {
							ms := item.UpdatedAt.UnixMilli()
							for {
								cur := maxObservedUpdatedAtMs.Load()
								if ms <= cur || maxObservedUpdatedAtMs.CompareAndSwap(cur, ms) {
									break
								}
							}
						}
						summaryMu.Lock()
						if err != nil {
							summary.Errors++
							syncErr = err
						} else {
							switch result {
							case "created":
								summary.Created++
							case "updated":
								summary.Updated++
							default:
								summary.Skipped++
							}
						}
						summary.AttachmentErrors += attachErrs
						currentSummary := summary
						summaryMu.Unlock()
						s.updateRunProgress(ctx, run.ID, currentSummary, totalCount, page.PageNum, typ)
					}
				}()
			}
			for _, item := range page.Items {
				item.Type = typ
				select {
				case <-ctx.Done():
					close(jobs)
					wg.Wait()
					return ctx.Err()
				case jobs <- item:
				}
			}
			close(jobs)
			wg.Wait()
			return nil
		})
		if err != nil {
			summary.Errors++
			syncErr = err
			continue
		}
	}

	status := "succeeded"
	var errText pgtype.Text
	finishCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	attachmentHint := ""
	if summary.AttachmentErrors > 0 {
		attachmentHint = fmt.Sprintf("%d attachments failed to download (see logs)", summary.AttachmentErrors)
	}
	if syncErr != nil {
		status = "failed"
		combined := syncErr.Error()
		if attachmentHint != "" {
			combined = combined + "; " + attachmentHint
		}
		errText = pgtype.Text{String: combined, Valid: true}
		if err := s.Queries.MarkFeishuProjectIntegrationError(finishCtx, db.MarkFeishuProjectIntegrationErrorParams{
			ID:        cfg.ID,
			LastError: pgtype.Text{String: combined, Valid: true},
		}); err != nil {
			slog.Warn("Feishu Project mark-error failed", "integration_id", UUIDString(cfg.ID), "error", err)
		}
	} else {
		// If this write fails silently, last_synced_at never advances and the
		// next incremental sync replays the full lookback window.
		if err := s.Queries.MarkFeishuProjectIntegrationSynced(finishCtx, db.MarkFeishuProjectIntegrationSyncedParams{
			ID:                  cfg.ID,
			ObservedUpdatedAtMs: maxObservedUpdatedAtMs.Load(),
		}); err != nil {
			slog.Warn("Feishu Project mark-synced failed; last_synced_at not advanced", "integration_id", UUIDString(cfg.ID), "error", err)
		}
		if trigger == "reconcile" {
			if err := s.Queries.MarkFeishuProjectIntegrationReconciled(finishCtx, cfg.ID); err != nil {
				slog.Warn("Feishu Project mark-reconciled failed; will re-attempt next tick", "integration_id", UUIDString(cfg.ID), "error", err)
			}
		}
		if attachmentHint != "" {
			errText = pgtype.Text{String: attachmentHint, Valid: true}
			if err := s.Queries.MarkFeishuProjectIntegrationError(finishCtx, db.MarkFeishuProjectIntegrationErrorParams{
				ID:        cfg.ID,
				LastError: pgtype.Text{String: attachmentHint, Valid: true},
			}); err != nil {
				slog.Warn("Feishu Project mark-error (attachment hint) failed", "integration_id", UUIDString(cfg.ID), "error", err)
			}
		}
	}
	if run.ID.Valid {
		if err := s.Queries.FinishFeishuProjectSyncRun(finishCtx, db.FinishFeishuProjectSyncRunParams{
			ID:           run.ID,
			Status:       status,
			CreatedCount: int32(summary.Created),
			UpdatedCount: int32(summary.Updated),
			SkippedCount: int32(summary.Skipped),
			ErrorCount:   int32(summary.Errors),
			Error:        errText,
		}); err != nil {
			slog.Warn("Feishu Project sync run finish failed", "integration_id", UUIDString(cfg.ID), "project_key", cfg.ProjectKey, "run_id", UUIDString(run.ID), "trigger", trigger, "error", err)
		}
	}
	return summary, syncErr
}

func (s *FeishuProjectSyncService) updateRunProgress(ctx context.Context, runID pgtype.UUID, summary FeishuProjectSyncSummary, totalCount, pageNum int, typ string) {
	if !runID.Valid {
		return
	}
	_ = s.Queries.UpdateFeishuProjectSyncRunProgress(ctx, db.UpdateFeishuProjectSyncRunProgressParams{
		ID:           runID,
		CreatedCount: int32(summary.Created),
		UpdatedCount: int32(summary.Updated),
		SkippedCount: int32(summary.Skipped),
		ErrorCount:   int32(summary.Errors),
		TotalCount:   int32(totalCount),
		CurrentPage:  int32(pageNum),
		CurrentType:  typ,
	})
}

func enabledFeishuProjectTypes(cfg db.FeishuProjectIntegration) []string {
	var out []string
	if cfg.SyncIssue {
		out = append(out, "issue")
	}
	return out
}

func (s *FeishuProjectSyncService) syncWorkItem(ctx context.Context, cfg db.FeishuProjectIntegration, item FeishuProjectWorkItem, lightUpdateExisting bool) (result string, attachErrs int, retErr error) {
	started := time.Now()
	timing := &feishuProjectSyncTiming{}
	defer func() {
		attachErrs = timing.attachmentsSkippedError
		elapsed := time.Since(started)
		if elapsed < feishuProjectSlowItemLogAfter && retErr == nil {
			return
		}
		slog.Info("Feishu Project item sync timing",
			"workspace_id", UUIDString(cfg.WorkspaceID),
			"project_key", cfg.ProjectKey,
			"work_item_type", item.Type,
			"work_item_id", item.ID,
			"result", result,
			"error", retErr,
			"elapsed_ms", elapsed.Milliseconds(),
			"owner_lookup_ms", timing.ownerLookup.Milliseconds(),
			"binding_lookup_ms", timing.bindingLookup.Milliseconds(),
			"issue_lookup_ms", timing.issueLookup.Milliseconds(),
			"issue_update_ms", timing.issueUpdate.Milliseconds(),
			"issue_create_ms", timing.issueCreate.Milliseconds(),
			"binding_upsert_ms", timing.bindingUpsert.Milliseconds(),
			"attachments_ms", timing.attachments.Milliseconds(),
			"attachment_list_ms", timing.attachmentList.Milliseconds(),
			"attachment_download_ms", timing.attachmentDownload.Milliseconds(),
			"attachment_upload_ms", timing.attachmentUpload.Milliseconds(),
			"attachment_db_ms", timing.attachmentDB.Milliseconds(),
			"attachments_total", len(item.Attachments),
			"attachments_existing", timing.attachmentsExisting,
			"attachments_uploaded", timing.attachmentsUploaded,
			"attachments_skipped_large", timing.attachmentsSkippedLarge,
			"attachments_skipped_error", timing.attachmentsSkippedError,
		)
	}()
	if item.ID == "" || item.Title == "" {
		return "skipped", 0, nil
	}
	matchedRoute, routed, routeErr := s.routeWorkItemProject(ctx, cfg, item)
	if routeErr != nil {
		return "skipped", 0, routeErr
	}
	if !routed {
		return "skipped", 0, nil
	}
	var projectID pgtype.UUID
	var fallbackAgentID pgtype.UUID
	if matchedRoute != nil {
		projectID = matchedRoute.ProjectID
		fallbackAgentID = matchedRoute.FallbackAgentID
	}
	mappedStatus := mapFeishuStatus(cfg.StatusMapping, item.Type, item.Status)
	status := mappedStatus
	if status == "" {
		status = "todo"
	}
	mappedPriority := mapFeishuPriority(item.Priority)

	phaseStarted := time.Now()
	binding, err := s.Queries.GetFeishuProjectIssueBindingByExternal(ctx, db.GetFeishuProjectIssueBindingByExternalParams{
		IntegrationID: cfg.ID,
		WorkItemType:  item.Type,
		WorkItemID:    item.ID,
	})
	timing.bindingLookup += time.Since(phaseStarted)
	if err == nil {
		phaseStarted = time.Now()
		issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: binding.IssueID, WorkspaceID: cfg.WorkspaceID})
		timing.issueLookup += time.Since(phaseStarted)
		if err != nil {
			return "skipped", 0, nil
		}
		phaseStarted = time.Now()
		assigneeType, assigneeID := s.resolveAssignee(ctx, cfg, item, mappedStatus, issue.AssigneeType, issue.AssigneeID, fallbackAgentID)
		timing.ownerLookup += time.Since(phaseStarted)
		nextProjectID := issue.ProjectID
		if projectID.Valid {
			nextProjectID = projectID
		}
		nextPriority := issue.Priority
		if mappedPriority != "" {
			nextPriority = mappedPriority
		}

		if lightUpdateExisting {
			// Light-update path for manual full-sync: refresh status / assignee /
			// project only. Skip syncExternalAttachments (~200-500ms per item with
			// any attachment) and preserve the existing title + description so the
			// already-synced attachment markdown block doesn't get clobbered. Per
			// item this collapses to ~25-30ms, so a 1k-item manual sync runs in ~30s
			// instead of multiple minutes. New attachments added in Meego after the
			// first sync are still picked up by the next scheduled (incremental)
			// sync, which keeps its full path.
			if issue.Status == status &&
				sameNullableText(issue.AssigneeType, assigneeType) &&
				sameNullableUUID(issue.AssigneeID, assigneeID) &&
				issue.Priority == nextPriority &&
				issue.ProjectID == nextProjectID {
				labelsChanged, err := s.syncIssueLabels(ctx, cfg, item, issue.ID)
				if err != nil {
					return "skipped", 0, err
				}
				s.reconcileSyncedIssueTasks(ctx, issue, issue)
				if labelsChanged {
					return "updated", 0, nil
				}
				return "skipped", 0, nil
			}
			phaseStarted = time.Now()
			updatedIssue, err := s.Queries.UpdateIssue(ctx, db.UpdateIssueParams{
				ID: issue.ID,
				// Pass invalid pgtype.Text for title/description → COALESCE in
				// queries/issue.sql keeps the current values, so the embedded
				// attachment markdown survives.
				Title:         pgtype.Text{},
				Description:   pgtype.Text{},
				Status:        pgtype.Text{String: status, Valid: true},
				Priority:      pgtype.Text{String: nextPriority, Valid: true},
				AssigneeType:  assigneeType,
				AssigneeID:    assigneeID,
				DueDate:       issue.DueDate,
				ParentIssueID: issue.ParentIssueID,
				ProjectID:     nextProjectID,
			})
			timing.issueUpdate += time.Since(phaseStarted)
			if err != nil {
				return "skipped", 0, err
			}
			phaseStarted = time.Now()
			_, _ = s.Queries.UpsertFeishuProjectIssueBinding(ctx, bindingParams(cfg, issue.ID, item))
			timing.bindingUpsert += time.Since(phaseStarted)
			if _, err := s.syncIssueLabels(ctx, cfg, item, issue.ID); err != nil {
				return "skipped", 0, err
			}
			s.reconcileSyncedIssueTasks(ctx, issue, updatedIssue)
			return "updated", 0, nil
		}

		// Full update path — scheduled (incremental) sync. Includes attachment refetch.
		phaseStarted = time.Now()
		attachmentMarkdown, attachErr := s.syncExternalAttachments(ctx, cfg, issue.ID, item, timing)
		timing.attachments += time.Since(phaseStarted)
		if attachErr != nil {
			return "skipped", 0, attachErr
		}
		nextDesc := externalDescription(item, attachmentMarkdown)
		nextTitle := externalTitle(item)
		if issue.Title == nextTitle && issue.Description.String == nextDesc && issue.Status == status &&
			issue.Priority == nextPriority &&
			sameNullableText(issue.AssigneeType, assigneeType) && sameNullableUUID(issue.AssigneeID, assigneeID) &&
			issue.ProjectID == nextProjectID {
			labelsChanged, err := s.syncIssueLabels(ctx, cfg, item, issue.ID)
			if err != nil {
				return "skipped", 0, err
			}
			s.reconcileSyncedIssueTasks(ctx, issue, issue)
			if labelsChanged {
				return "updated", 0, nil
			}
			return "skipped", 0, nil
		}
		phaseStarted = time.Now()
		updatedIssue, err := s.Queries.UpdateIssue(ctx, db.UpdateIssueParams{
			ID:            issue.ID,
			Title:         pgtype.Text{String: nextTitle, Valid: true},
			Description:   pgtype.Text{String: nextDesc, Valid: true},
			Status:        pgtype.Text{String: status, Valid: true},
			Priority:      pgtype.Text{String: nextPriority, Valid: true},
			AssigneeType:  assigneeType,
			AssigneeID:    assigneeID,
			DueDate:       issue.DueDate,
			ParentIssueID: issue.ParentIssueID,
			ProjectID:     nextProjectID,
		})
		timing.issueUpdate += time.Since(phaseStarted)
		if err != nil {
			return "skipped", 0, err
		}
		phaseStarted = time.Now()
		_, _ = s.Queries.UpsertFeishuProjectIssueBinding(ctx, bindingParams(cfg, issue.ID, item))
		timing.bindingUpsert += time.Since(phaseStarted)
		if _, err := s.syncIssueLabels(ctx, cfg, item, issue.ID); err != nil {
			return "skipped", 0, err
		}
		s.reconcileSyncedIssueTasks(ctx, issue, updatedIssue)
		return "updated", 0, nil
	}

	if !cfg.CreatedByID.Valid {
		return "skipped", 0, fmt.Errorf("feishu project integration has no creator")
	}
	phaseStarted = time.Now()
	assigneeType, assigneeID := s.resolveAssignee(ctx, cfg, item, mappedStatus, pgtype.Text{}, pgtype.UUID{}, fallbackAgentID)
	timing.ownerLookup += time.Since(phaseStarted)
	phaseStarted = time.Now()
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return "skipped", 0, err
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	number, err := qtx.IncrementIssueCounter(ctx, cfg.WorkspaceID)
	if err != nil {
		return "skipped", 0, err
	}
	issue, err := qtx.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID:  cfg.WorkspaceID,
		Title:        externalTitle(item),
		Description:  pgtype.Text{String: externalDescription(item, ""), Valid: true},
		Status:       status,
		Priority:     firstNonEmpty(mappedPriority, "none"),
		AssigneeType: assigneeType,
		AssigneeID:   assigneeID,
		CreatorType:  "member",
		CreatorID:    cfg.CreatedByID,
		Position:     0,
		Number:       number,
		ProjectID:    projectID,
	})
	if err != nil {
		return "skipped", 0, err
	}
	if _, err := qtx.UpsertFeishuProjectIssueBinding(ctx, bindingParams(cfg, issue.ID, item)); err != nil {
		return "skipped", 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "skipped", 0, err
	}
	timing.issueCreate += time.Since(phaseStarted)
	phaseStarted = time.Now()
	attachmentMarkdown, err := s.syncExternalAttachments(ctx, cfg, issue.ID, item, timing)
	timing.attachments += time.Since(phaseStarted)
	if err != nil {
		return "created", 0, err
	}
	if attachmentMarkdown != "" {
		phaseStarted = time.Now()
		updatedIssue, err := s.Queries.UpdateIssue(ctx, db.UpdateIssueParams{
			ID:            issue.ID,
			Title:         pgtype.Text{String: externalTitle(item), Valid: true},
			Description:   pgtype.Text{String: externalDescription(item, attachmentMarkdown), Valid: true},
			Status:        pgtype.Text{String: status, Valid: true},
			Priority:      pgtype.Text{String: firstNonEmpty(mappedPriority, issue.Priority), Valid: true},
			AssigneeType:  assigneeType,
			AssigneeID:    assigneeID,
			DueDate:       issue.DueDate,
			ParentIssueID: issue.ParentIssueID,
			ProjectID:     issue.ProjectID,
		})
		timing.issueUpdate += time.Since(phaseStarted)
		if err != nil {
			return "created", 0, err
		}
		issue = updatedIssue
	}
	if _, err := s.syncIssueLabels(ctx, cfg, item, issue.ID); err != nil {
		return "created", 0, err
	}
	s.reconcileSyncedIssueTasks(ctx, issue, issue)
	return "created", 0, nil
}

func (s *FeishuProjectSyncService) syncIssueLabels(ctx context.Context, cfg db.FeishuProjectIntegration, item FeishuProjectWorkItem, issueID pgtype.UUID) (bool, error) {
	rules := feishuProjectLabelSyncRules(cfg)
	bindings, err := s.Queries.ListFeishuProjectLabelSyncBindingsByIssue(ctx, db.ListFeishuProjectLabelSyncBindingsByIssueParams{
		IntegrationID: cfg.ID,
		IssueID:       issueID,
	})
	if err != nil {
		return false, err
	}
	byRule := make(map[string]db.FeishuProjectLabelSyncBinding, len(bindings))
	for _, binding := range bindings {
		byRule[binding.RuleID] = binding
	}
	desiredRuleLabels := map[string]pgtype.UUID{}
	desiredLabelIDs := map[pgtype.UUID]bool{}
	changed := false
	for _, rule := range rules {
		if !rule.Enabled || strings.TrimSpace(rule.ID) == "" || strings.TrimSpace(rule.FieldKey) == "" || strings.TrimSpace(rule.Match) == "" || strings.TrimSpace(rule.LabelName) == "" {
			continue
		}
		binding, hadBinding := byRule[rule.ID]
		if !feishuProjectLabelRuleMatches(item, rule) {
			continue
		}
		label, err := s.ensureIssueLabel(ctx, cfg.WorkspaceID, rule.LabelName)
		if err != nil {
			return changed, err
		}
		desiredRuleLabels[rule.ID] = label.ID
		desiredLabelIDs[label.ID] = true
		if err := s.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
			IssueID:     issueID,
			LabelID:     label.ID,
			WorkspaceID: cfg.WorkspaceID,
		}); err != nil {
			return changed, err
		}
		if err := s.Queries.UpsertFeishuProjectLabelSyncBinding(ctx, db.UpsertFeishuProjectLabelSyncBindingParams{
			IntegrationID: cfg.ID,
			WorkspaceID:   cfg.WorkspaceID,
			IssueID:       issueID,
			RuleID:        rule.ID,
			LabelID:       label.ID,
		}); err != nil {
			return changed, err
		}
		if !hadBinding || binding.LabelID != label.ID {
			changed = true
		}
	}
	for _, binding := range bindings {
		deleteBinding, detachLabel := feishuProjectLabelSyncCleanupAction(binding, desiredRuleLabels, desiredLabelIDs)
		if !deleteBinding {
			continue
		}
		if detachLabel {
			if err := s.detachManagedLabel(ctx, cfg.WorkspaceID, issueID, binding.LabelID); err != nil {
				return changed, err
			}
		}
		if err := s.Queries.DeleteFeishuProjectLabelSyncBinding(ctx, db.DeleteFeishuProjectLabelSyncBindingParams{
			IntegrationID: cfg.ID,
			IssueID:       issueID,
			RuleID:        binding.RuleID,
		}); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

func feishuProjectLabelSyncCleanupAction(binding db.FeishuProjectLabelSyncBinding, desiredRuleLabels map[string]pgtype.UUID, desiredLabelIDs map[pgtype.UUID]bool) (deleteBinding bool, detachLabel bool) {
	if labelID, ok := desiredRuleLabels[binding.RuleID]; ok && labelID == binding.LabelID {
		return false, false
	}
	return true, !desiredLabelIDs[binding.LabelID]
}

func feishuProjectLabelSyncRules(cfg db.FeishuProjectIntegration) []FeishuProjectLabelSyncRule {
	if len(cfg.LabelSyncRules) == 0 {
		return nil
	}
	var rules []FeishuProjectLabelSyncRule
	if err := json.Unmarshal(cfg.LabelSyncRules, &rules); err != nil {
		slog.Warn("Feishu Project label sync rules decode failed", "integration_id", UUIDString(cfg.ID), "error", err)
		return nil
	}
	return rules
}

func feishuProjectLabelRuleMatches(item FeishuProjectWorkItem, rule FeishuProjectLabelSyncRule) bool {
	want := strings.TrimSpace(rule.Match)
	for _, value := range item.FieldValues[strings.TrimSpace(rule.FieldKey)] {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

func (s *FeishuProjectSyncService) ensureIssueLabel(ctx context.Context, workspaceID pgtype.UUID, name string) (db.IssueLabel, error) {
	name = strings.TrimSpace(name)
	labels, err := s.Queries.ListLabels(ctx, workspaceID)
	if err != nil {
		return db.IssueLabel{}, err
	}
	for _, label := range labels {
		if strings.EqualFold(label.Name, name) {
			return label, nil
		}
	}
	label, err := s.Queries.CreateLabel(ctx, db.CreateLabelParams{
		WorkspaceID: workspaceID,
		Name:        name,
		Color:       "#3b82f6",
	})
	if err == nil {
		return label, nil
	}
	labels, listErr := s.Queries.ListLabels(ctx, workspaceID)
	if listErr != nil {
		return db.IssueLabel{}, err
	}
	for _, label := range labels {
		if strings.EqualFold(label.Name, name) {
			return label, nil
		}
	}
	return db.IssueLabel{}, err
}

func (s *FeishuProjectSyncService) detachManagedLabel(ctx context.Context, workspaceID, issueID, labelID pgtype.UUID) error {
	return s.Queries.DetachLabelFromIssue(ctx, db.DetachLabelFromIssueParams{
		IssueID:     issueID,
		LabelID:     labelID,
		WorkspaceID: workspaceID,
	})
}

func (s *FeishuProjectSyncService) reconcileSyncedIssueTasks(ctx context.Context, prevIssue, issue db.Issue) {
	if s.TaskService == nil {
		return
	}
	if issue.Status == "cancelled" || issue.Status == "done" {
		if err := s.TaskService.CancelTasksForIssue(ctx, issue.ID); err != nil {
			slog.Warn("Feishu Project sync task cancel failed", "issue_id", UUIDString(issue.ID), "status", issue.Status, "error", err)
		}
		return
	}
	if !sameNullableText(prevIssue.AssigneeType, issue.AssigneeType) || !sameNullableUUID(prevIssue.AssigneeID, issue.AssigneeID) {
		if err := s.TaskService.CancelTasksForIssue(ctx, issue.ID); err != nil {
			slog.Warn("Feishu Project sync previous assignee task cancel failed", "issue_id", UUIDString(issue.ID), "error", err)
			return
		}
	}
	s.enqueueSyncedIssueIfNeeded(ctx, issue)
}

func (s *FeishuProjectSyncService) enqueueSyncedIssueIfNeeded(ctx context.Context, issue db.Issue) {
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "agent" || !issue.AssigneeID.Valid {
		return
	}
	hasTask, err := s.Queries.HasTaskForIssueAndAgent(ctx, db.HasTaskForIssueAndAgentParams{
		IssueID: issue.ID,
		AgentID: issue.AssigneeID,
	})
	if err != nil {
		slog.Warn("Feishu Project sync task dedup check failed", "issue_id", UUIDString(issue.ID), "agent_id", UUIDString(issue.AssigneeID), "error", err)
		return
	}
	if hasTask {
		return
	}
	if _, err := s.TaskService.EnqueueTaskForIssue(ctx, issue); err != nil {
		slog.Warn("Feishu Project sync task enqueue failed", "issue_id", UUIDString(issue.ID), "agent_id", UUIDString(issue.AssigneeID), "error", err)
	}
}

// resolveAssignee picks the issue assignee for a synced work item. The chain is:
//
//  1. Owner's agent — only if cfg.AssignOpenItemsToOwnerAgent is on AND the local status
//     is in an "assignable" state (currently "todo"). For non-assignable states (e.g.
//     in_progress, done) we preserve currentType/currentID so we don't fight with a
//     manual reassignment that happened in Multica.
//  2. Owner as workspace member — the normal case when the Meego owner exists in
//     Multica as a workspace member.
//  3. Route's fallback agent — last resort when the owner can't be resolved at all
//     (left Meego / never joined Multica / typo'd email). Per-route so different
//     business lines can have different triage handlers.
//  4. Empty — nothing matched.
//
// The fallback is intentionally below member resolution: 兜底 means "use when nothing
// else fits", so if the human owner is in the workspace they should still own the item
// (otherwise the fallback would silently steal items that have a valid owner).
func (s *FeishuProjectSyncService) resolveAssignee(ctx context.Context, cfg db.FeishuProjectIntegration, item FeishuProjectWorkItem, localStatus string, currentType pgtype.Text, currentID pgtype.UUID, fallbackAgentID pgtype.UUID) (pgtype.Text, pgtype.UUID) {
	if cfg.AssignOpenItemsToOwnerAgent && !isFeishuProjectOwnerAgentAssignableStatus(item.Status, localStatus) {
		return currentType, currentID
	}
	if cfg.AssignOpenItemsToOwnerAgent {
		if t, id := s.resolveOwnerAgent(ctx, cfg.WorkspaceID, item.OwnerEmail); id.Valid {
			return t, id
		}
	}
	if t, id := s.resolveOwnerMember(ctx, cfg.WorkspaceID, item.OwnerEmail); id.Valid {
		return t, id
	}
	if fallbackAgentID.Valid {
		return pgtype.Text{String: "agent", Valid: true}, fallbackAgentID
	}
	return pgtype.Text{}, pgtype.UUID{}
}

// routeWorkItemProject decides which Multica project (inside the integration's workspace) a
// Meego work item belongs to, based on the configured business-line field and the routes
// stored in feishu_project_business_line_route.
//
// Returns:
//   - matched: pointer to the matched route row (nil when routing is disabled). Callers
//     read both ProjectID and FallbackAgentID off it.
//   - routed=true: the item should be synced (either no routing rules apply, or a route matched)
//   - routed=false, err=nil: routing is configured but no rule matched → item is intentionally
//     skipped (operators see this as "no route configured for this biz line")
//   - err != nil: transient lookup failure; caller should bubble up
//
// When the integration has BusinessLineFieldKey == "" (legacy / 1:1 setup), routing is
// disabled and every item is synced into the workspace without a project — matching the
// pre-routing behavior so this change is backward-compatible. In that case matched=nil.
func (s *FeishuProjectSyncService) routeWorkItemProject(ctx context.Context, cfg db.FeishuProjectIntegration, item FeishuProjectWorkItem) (*db.FeishuProjectBusinessLineRoute, bool, error) {
	if strings.TrimSpace(cfg.BusinessLineFieldKey) == "" {
		return nil, true, nil
	}
	routes, err := s.Queries.ListFeishuProjectBusinessLineRoutes(ctx, cfg.ID)
	if err != nil {
		return nil, false, fmt.Errorf("list biz-line routes: %w", err)
	}
	if len(routes) == 0 {
		slog.Warn("Feishu Project sync skipped: routing enabled but no routes configured",
			"integration_id", UUIDString(cfg.ID),
			"project_key", cfg.ProjectKey,
			"work_item_id", item.ID,
		)
		return nil, false, nil
	}
	matched := matchBusinessLineRoute(routes, item.BusinessLineTokens)
	if matched == nil {
		slog.Warn("Feishu Project sync skipped: work item business-line value has no matching route",
			"integration_id", UUIDString(cfg.ID),
			"project_key", cfg.ProjectKey,
			"work_item_id", item.ID,
			"work_item_tokens", formatBusinessLineTokens(item.BusinessLineTokens),
		)
		return nil, false, nil
	}
	return matched, true, nil
}

// matchBusinessLineRoute applies the precedence rules from the design:
//  1. exact leaf-id match (route.business_line_id == any item leaf id)
//  2. exact leaf-name match
//  3. parent-id match (route.business_line_id == any item parent id) — covers parent-level routes
//  4. parent-name match
//
// First-wins by route order. Multiple ties at the same precedence layer log a warning at
// call sites; here we return deterministically the first.
func matchBusinessLineRoute(routes []db.FeishuProjectBusinessLineRoute, tokens []FeishuBusinessLineToken) *db.FeishuProjectBusinessLineRoute {
	if len(routes) == 0 || len(tokens) == 0 {
		return nil
	}
	leafIDs := map[string]bool{}
	leafNames := map[string]bool{}
	parentIDs := map[string]bool{}
	parentNames := map[string]bool{}
	for _, tok := range tokens {
		if id := strings.TrimSpace(tok.ID); id != "" {
			leafIDs[id] = true
		}
		if name := strings.TrimSpace(tok.Name); name != "" {
			leafNames[name] = true
		}
		if pid := strings.TrimSpace(tok.ParentID); pid != "" {
			parentIDs[pid] = true
		}
		if pname := strings.TrimSpace(tok.ParentName); pname != "" {
			parentNames[pname] = true
		}
	}
	matchers := []func(db.FeishuProjectBusinessLineRoute) bool{
		func(r db.FeishuProjectBusinessLineRoute) bool { return leafIDs[strings.TrimSpace(r.BusinessLineID)] },
		func(r db.FeishuProjectBusinessLineRoute) bool {
			return leafNames[strings.TrimSpace(r.BusinessLineName)]
		},
		func(r db.FeishuProjectBusinessLineRoute) bool { return parentIDs[strings.TrimSpace(r.BusinessLineID)] },
		func(r db.FeishuProjectBusinessLineRoute) bool {
			return parentNames[strings.TrimSpace(r.BusinessLineName)]
		},
	}
	for _, m := range matchers {
		for i := range routes {
			if m(routes[i]) {
				return &routes[i]
			}
		}
	}
	return nil
}

func formatBusinessLineTokens(tokens []FeishuBusinessLineToken) string {
	parts := make([]string, 0, len(tokens))
	for _, t := range tokens {
		parts = append(parts, fmt.Sprintf("%s/%s(%s/%s)", t.ID, t.Name, t.ParentID, t.ParentName))
	}
	return strings.Join(parts, ",")
}

func (s *FeishuProjectSyncService) resolveOwnerMember(ctx context.Context, workspaceID pgtype.UUID, email string) (pgtype.Text, pgtype.UUID) {
	if strings.TrimSpace(email) == "" {
		return pgtype.Text{}, pgtype.UUID{}
	}
	user, err := s.Queries.GetUserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return pgtype.Text{}, pgtype.UUID{}
	}
	if _, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: user.ID, WorkspaceID: workspaceID}); err != nil {
		return pgtype.Text{}, pgtype.UUID{}
	}
	return pgtype.Text{String: "member", Valid: true}, user.ID
}

func (s *FeishuProjectSyncService) resolveOwnerAgent(ctx context.Context, workspaceID pgtype.UUID, email string) (pgtype.Text, pgtype.UUID) {
	if strings.TrimSpace(email) == "" {
		return pgtype.Text{}, pgtype.UUID{}
	}
	user, err := s.Queries.GetUserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return pgtype.Text{}, pgtype.UUID{}
	}
	if _, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: user.ID, WorkspaceID: workspaceID}); err != nil {
		return pgtype.Text{}, pgtype.UUID{}
	}
	agent, err := s.Queries.GetFirstAgentByOwnerInWorkspace(ctx, db.GetFirstAgentByOwnerInWorkspaceParams{
		WorkspaceID: workspaceID,
		OwnerID:     user.ID,
	})
	if err != nil {
		return pgtype.Text{}, pgtype.UUID{}
	}
	return pgtype.Text{String: "agent", Valid: true}, agent.ID
}

func isFeishuProjectOwnerAgentAssignableStatus(externalStatus, localStatus string) bool {
	return strings.TrimSpace(localStatus) == "todo"
}

func sameNullableText(a, b pgtype.Text) bool {
	if a.Valid != b.Valid {
		return false
	}
	if !a.Valid {
		return true
	}
	return a.String == b.String
}

func sameNullableUUID(a, b pgtype.UUID) bool {
	if a.Valid != b.Valid {
		return false
	}
	if !a.Valid {
		return true
	}
	return a.Bytes == b.Bytes
}

func bindingParams(cfg db.FeishuProjectIntegration, issueID pgtype.UUID, item FeishuProjectWorkItem) db.UpsertFeishuProjectIssueBindingParams {
	return db.UpsertFeishuProjectIssueBindingParams{
		WorkspaceID:        cfg.WorkspaceID,
		IntegrationID:      cfg.ID,
		IssueID:            issueID,
		ProjectKey:         cfg.ProjectKey,
		WorkItemType:       item.Type,
		WorkItemID:         item.ID,
		ExternalIdentifier: externalIdentifier(item),
		ExternalUrl:        pgtype.Text{String: item.URL, Valid: item.URL != ""},
		ExternalStatusLabel: pgtype.Text{
			String: item.Status,
			Valid:  item.Status != "",
		},
		LastExternalUpdatedAt: pgtype.Timestamptz{Time: item.UpdatedAt, Valid: !item.UpdatedAt.IsZero()},
	}
}

func externalIdentifier(item FeishuProjectWorkItem) string {
	id := strings.TrimSpace(item.ID)
	if id == "" {
		return ""
	}
	prefix := "MEEGO"
	switch strings.ToLower(strings.TrimSpace(item.Type)) {
	case "issue", "bug", "缺陷":
		prefix = "BUG"
	case "story", "需求":
		prefix = "STORY"
	case "task", "任务":
		prefix = "TASK"
	}
	return prefix + "-" + id
}

func externalTitle(item FeishuProjectWorkItem) string {
	identifier := externalIdentifier(item)
	title := strings.TrimSpace(item.Title)
	if identifier == "" {
		return title
	}
	if title == "" {
		return "[" + identifier + "]"
	}
	title = stripExternalTitlePrefix(title, identifier)
	if title == "" {
		return "[" + identifier + "]"
	}
	return "[" + identifier + "] " + title
}

func stripExternalTitlePrefix(title, identifier string) string {
	pattern := `^\s*(?:\[` + regexp.QuoteMeta(identifier) + `\]|` + regexp.QuoteMeta(identifier) + `)\s*[:：\-]?\s*`
	return strings.TrimSpace(regexp.MustCompile(pattern).ReplaceAllString(title, ""))
}

func externalDescription(item FeishuProjectWorkItem, attachmentMarkdown string) string {
	identifier := externalIdentifier(item)
	var b strings.Builder
	if strings.TrimSpace(item.Description) != "" {
		b.WriteString(strings.TrimSpace(item.Description))
		b.WriteString("\n\n")
	}
	if strings.TrimSpace(attachmentMarkdown) != "" {
		b.WriteString(strings.TrimSpace(attachmentMarkdown))
		b.WriteString("\n\n")
	}
	b.WriteString("External-Id: ")
	b.WriteString(identifier)
	if item.URL != "" {
		b.WriteString("\nExternal-Url: ")
		b.WriteString(item.URL)
	}
	return b.String()
}

func normalizeFeishuProjectDescription(raw string) (string, []FeishuProjectAttachment) {
	var attachments []FeishuProjectAttachment
	imageIndex := 0
	re := regexp.MustCompile(`!\[[^\]]*\]\((https?://[^)\s]+)\)\s*(?:<!--\s*([A-Za-z0-9._-]+)\s*-->)?`)
	cleaned := re.ReplaceAllStringFunc(raw, func(match string) string {
		parts := re.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		rawURL := strings.TrimSpace(parts[1])
		if rawURL == "" || !looksLikeFeishuProjectFileURL(rawURL) {
			return match
		}
		imageIndex++
		id := ""
		if len(parts) > 2 {
			id = strings.TrimSpace(parts[2])
		}
		name := id
		if name == "" {
			name = fmt.Sprintf("image-%d", imageIndex)
		}
		attachments = append(attachments, FeishuProjectAttachment{
			ID:          id,
			Name:        name,
			URL:         rawURL,
			ContentType: "image/*",
		})
		return ""
	})
	cleaned = stripFeishuProjectImagePlaceholders(cleaned)
	return strings.TrimSpace(collapseExcessBlankLines(cleaned)), dedupeFeishuProjectAttachments(attachments)
}

func stripFeishuProjectImagePlaceholders(raw string) string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "[图片]" {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func looksLikeFeishuProjectFileURL(rawURL string) bool {
	return strings.Contains(rawURL, "project.feishu.cn/") || strings.Contains(rawURL, "/goapi/v5/platform/file/")
}

func collapseExcessBlankLines(s string) string {
	re := regexp.MustCompile(`\n{3,}`)
	return re.ReplaceAllString(s, "\n\n")
}

func (s *FeishuProjectSyncService) syncExternalAttachments(ctx context.Context, cfg db.FeishuProjectIntegration, issueID pgtype.UUID, item FeishuProjectWorkItem, timing *feishuProjectSyncTiming) (string, error) {
	if s.Storage == nil || len(item.Attachments) == 0 || !cfg.CreatedByID.Valid {
		return "", nil
	}
	phaseStarted := time.Now()
	existing, _ := s.Queries.ListAttachmentsByIssue(ctx, db.ListAttachmentsByIssueParams{
		IssueID:     issueID,
		WorkspaceID: cfg.WorkspaceID,
	})
	bindings, _ := s.Queries.ListFeishuProjectAttachmentBindingsByIssue(ctx, db.ListFeishuProjectAttachmentBindingsByIssueParams{
		IntegrationID: cfg.ID,
		IssueID:       issueID,
	})
	if timing != nil {
		timing.attachmentList += time.Since(phaseStarted)
	}
	attachmentsByID := make(map[pgtype.UUID]db.Attachment, len(existing))
	for _, att := range existing {
		attachmentsByID[att.ID] = att
	}
	// Bindings are the primary dedup key (Feishu attachment ID → local
	// attachment). Filename is kept as a fallback for legacy rows synced
	// before bindings existed, or for the rare case where Feishu returns
	// no attachment ID.
	boundByExternalID := make(map[string]db.Attachment, len(bindings))
	for _, b := range bindings {
		if att, ok := attachmentsByID[b.AttachmentID]; ok {
			boundByExternalID[b.ExternalAttachmentID] = att
		}
	}
	byName := make(map[string]db.Attachment, len(existing))
	for _, att := range existing {
		byName[att.Filename] = att
	}

	lines := make([]string, 0, len(item.Attachments))
	for _, ext := range item.Attachments {
		ext.Name = firstNonEmpty(strings.TrimSpace(ext.Name), strings.TrimSpace(ext.ID), "attachment")
		if ext.Name == "" {
			continue
		}
		externalID := strings.TrimSpace(ext.ID)
		if externalID != "" {
			if att, ok := boundByExternalID[externalID]; ok {
				if timing != nil {
					timing.attachmentsExisting++
				}
				lines = append(lines, attachmentMarkdown(att.Filename, feishuProjectAttachmentContentURL(att), att.ContentType))
				continue
			}
		} else if att, ok := byName[ext.Name]; ok {
			// No external ID — fall back to filename match. Lossy, but only
			// hit when Feishu omits the attachment ID.
			if timing != nil {
				timing.attachmentsExisting++
			}
			lines = append(lines, attachmentMarkdown(att.Filename, feishuProjectAttachmentContentURL(att), att.ContentType))
			continue
		}
		if feishuProjectAttachmentTooLarge(ext) {
			if timing != nil {
				timing.attachmentsSkippedLarge++
			}
			slog.Info("Feishu Project attachment skipped: too large",
				"workspace_id", UUIDString(cfg.WorkspaceID),
				"project_key", cfg.ProjectKey,
				"work_item_type", item.Type,
				"work_item_id", item.ID,
				"filename", ext.Name,
				"size_bytes", ext.SizeBytes,
				"max_bytes", feishuProjectAttachmentMaxSize,
			)
			continue
		}
		phaseStarted = time.Now()
		data, filename, contentType, err := s.Client.DownloadAttachment(ctx, cfg, item, ext)
		if timing != nil {
			timing.attachmentDownload += time.Since(phaseStarted)
		}
		if err != nil {
			if timing != nil {
				timing.attachmentsSkippedError++
			}
			slog.Warn("Feishu Project attachment skipped: download failed",
				"workspace_id", UUIDString(cfg.WorkspaceID),
				"project_key", cfg.ProjectKey,
				"work_item_type", item.Type,
				"work_item_id", item.ID,
				"filename", ext.Name,
				"attachment_id", ext.ID,
				"error", err,
			)
			continue
		}
		if len(data) == 0 {
			continue
		}
		if len(data) > feishuProjectAttachmentMaxSize {
			if timing != nil {
				timing.attachmentsSkippedLarge++
			}
			slog.Info("Feishu Project attachment skipped after download: too large",
				"workspace_id", UUIDString(cfg.WorkspaceID),
				"project_key", cfg.ProjectKey,
				"work_item_type", item.Type,
				"work_item_id", item.ID,
				"filename", ext.Name,
				"size_bytes", len(data),
				"max_bytes", feishuProjectAttachmentMaxSize,
			)
			continue
		}
		filename = firstNonEmpty(filename, ext.Name)
		contentType = firstNonEmpty(contentType, ext.ContentType, http.DetectContentType(data))
		id, err := uuid.NewV7()
		if err != nil {
			return "", err
		}
		key := feishuProjectAttachmentKey(cfg, item, ext, id.String(), filename)
		phaseStarted = time.Now()
		link, err := s.Storage.Upload(ctx, key, data, contentType, filename)
		if timing != nil {
			timing.attachmentUpload += time.Since(phaseStarted)
		}
		if err != nil {
			return "", err
		}
		phaseStarted = time.Now()
		att, err := s.Queries.CreateAttachment(ctx, db.CreateAttachmentParams{
			ID:           pgtype.UUID{Bytes: id, Valid: true},
			WorkspaceID:  cfg.WorkspaceID,
			IssueID:      issueID,
			UploaderType: "member",
			UploaderID:   cfg.CreatedByID,
			Filename:     filename,
			Url:          link,
			ContentType:  contentType,
			SizeBytes:    int64(len(data)),
		})
		if timing != nil {
			timing.attachmentDB += time.Since(phaseStarted)
		}
		if err != nil {
			return "", err
		}
		if timing != nil {
			timing.attachmentsUploaded++
		}
		// Record the external ID → local attachment binding so future syncs
		// dedup on the Feishu attachment ID instead of filename. Failure
		// here is logged and ignored: the attachment is already uploaded
		// and visible to the user; we'll just re-download on the next sync.
		if externalID != "" {
			if _, err := s.Queries.CreateFeishuProjectAttachmentBinding(ctx, db.CreateFeishuProjectAttachmentBindingParams{
				WorkspaceID:          cfg.WorkspaceID,
				IntegrationID:        cfg.ID,
				IssueID:              issueID,
				AttachmentID:         att.ID,
				ExternalAttachmentID: externalID,
				ExternalFilename:     ext.Name,
			}); err != nil {
				slog.Warn("Feishu Project attachment binding create failed",
					"workspace_id", UUIDString(cfg.WorkspaceID),
					"integration_id", UUIDString(cfg.ID),
					"issue_id", UUIDString(issueID),
					"external_attachment_id", externalID,
					"error", err,
				)
			} else {
				boundByExternalID[externalID] = att
			}
		}
		byName[att.Filename] = att
		lines = append(lines, attachmentMarkdown(att.Filename, feishuProjectAttachmentContentURL(att), att.ContentType))
	}
	return strings.Join(lines, "\n"), nil
}

func feishuProjectAttachmentKey(cfg db.FeishuProjectIntegration, item FeishuProjectWorkItem, ext FeishuProjectAttachment, fallbackID, filename string) string {
	id := firstNonEmpty(ext.ID, fallbackID)
	id = feishuProjectSafePathSegment(id)
	extension := path.Ext(filename)
	if extension == "" {
		if exts, _ := mime.ExtensionsByType(ext.ContentType); len(exts) > 0 {
			extension = exts[0]
		}
	}
	return "workspaces/" + UUIDString(cfg.WorkspaceID) + "/feishu-project/" +
		feishuProjectSafePathSegment(UUIDString(cfg.ID)) + "/" +
		feishuProjectSafePathSegment(item.Type) + "/" +
		feishuProjectSafePathSegment(item.ID) + "/" + id + extension
}

func feishuProjectAttachmentTooLarge(att FeishuProjectAttachment) bool {
	return att.SizeBytes > feishuProjectAttachmentMaxSize
}

func feishuProjectSafePathSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	return regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(s, "_")
}

func attachmentMarkdown(filename, url, contentType string) string {
	alt := strings.ReplaceAll(filename, "]", "\\]")
	if strings.HasPrefix(contentType, "image/") || isImageFilename(filename) {
		return "![" + alt + "](" + url + ")"
	}
	return "!file[" + alt + "](" + url + ")"
}

func feishuProjectAttachmentContentURL(att db.Attachment) string {
	return "/api/attachments/" + UUIDString(att.ID) + "/content?workspace_id=" + UUIDString(att.WorkspaceID)
}

func isImageFilename(filename string) bool {
	switch strings.ToLower(path.Ext(filename)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return true
	default:
		return false
	}
}

func mapFeishuStatus(raw []byte, typ, external string) string {
	var mapping map[string]map[string]string
	if err := json.Unmarshal(raw, &mapping); err == nil {
		if byType := mapping[typ]; byType != nil {
			if v := byType[external]; v != "" {
				return v
			}
		}
	}
	var flat map[string]string
	if err := json.Unmarshal(raw, &flat); err == nil {
		return flat[external]
	}
	return ""
}

func mapFeishuPriority(external string) string {
	normalized := strings.ToLower(strings.TrimSpace(external))
	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	switch normalized {
	case "", "<nil>":
		return ""
	case "none", "nopriority", "无", "无优先级", "未设置", "不设置":
		return "none"
	case "urgent", "blocker", "critical", "crit", "highest", "p0", "p00", "s0", "致命", "紧急", "最高", "严重阻塞":
		return "urgent"
	case "high", "major", "p1", "s1", "高", "高优", "高优先级", "严重":
		return "high"
	case "medium", "normal", "middle", "p2", "s2", "中", "中优", "中优先级", "普通", "一般":
		return "medium"
	case "low", "minor", "lowest", "p3", "p4", "s3", "s4", "低", "低优", "低优先级", "轻微":
		return "low"
	default:
		return ""
	}
}

func MapMulticaStatusToFeishu(raw []byte, typ, status string) string {
	var mapping map[string]map[string]string
	if err := json.Unmarshal(raw, &mapping); err == nil {
		if byType := mapping[typ]; byType != nil {
			if v := byType[status]; v != "" {
				return v
			}
		}
	}
	var flat map[string]string
	if err := json.Unmarshal(raw, &flat); err == nil {
		return flat[status]
	}
	return ""
}

func mappedFeishuProjectStatuses(raw []byte, typ string) []string {
	add := func(out []string, seen map[string]bool, status string) []string {
		status = strings.TrimSpace(status)
		if status == "" || seen[status] {
			return out
		}
		seen[status] = true
		return append(out, status)
	}
	seen := map[string]bool{}
	var out []string
	var nested map[string]map[string]string
	if err := json.Unmarshal(raw, &nested); err == nil {
		if byType := nested[typ]; byType != nil {
			for external, local := range byType {
				if strings.TrimSpace(local) != "" {
					out = add(out, seen, external)
				}
			}
			sort.Strings(out)
			return out
		}
	}
	var flat map[string]string
	if err := json.Unmarshal(raw, &flat); err == nil {
		for external, local := range flat {
			if strings.TrimSpace(local) != "" {
				out = add(out, seen, external)
			}
		}
	}
	sort.Strings(out)
	return out
}

func feishuProjectSyncSinceDate(cfg db.FeishuProjectIntegration, now time.Time) string {
	return feishuProjectSyncSince(cfg, now).UTC().Format("2006-01-02")
}

func feishuProjectSyncSinceUnixMilli(cfg db.FeishuProjectIntegration, now time.Time) int64 {
	return feishuProjectSyncSince(cfg, now).UnixMilli()
}

func feishuProjectManualSyncSinceUnixMilli(now time.Time) int64 {
	return now.Add(-feishuProjectManualLookback).UnixMilli()
}

// feishuProjectSyncSince computes the legacy local-clock-based incremental
// lookback. Kept as a fallback for integrations that haven't yet stored a
// Feishu-side watermark (last_seen_updated_at_ms), and for tests that drive
// QueryWorkItemPages directly without a Sync entry.
func feishuProjectSyncSince(cfg db.FeishuProjectIntegration, now time.Time) time.Time {
	since := now.Add(-feishuProjectInitialLookback)
	if cfg.LastSyncedAt.Valid {
		since = cfg.LastSyncedAt.Time.Add(-feishuProjectIncrementalReplay)
	}
	return since
}

// feishuProjectSinceUnixMilliForTrigger picks the `updated_at >=` lookback
// for a sync run based on its trigger. The result is the single point of
// policy for "how far back do we ask Feishu for changes".
//
//   - "manual"    → 30d (full bootstrap on user request)
//   - "reconcile" → 6h30m (6h cadence + 30m safety overshoot)
//   - default     → incremental: last_seen_updated_at_ms - 10m overlap,
//     falling back to legacy local-clock lookback when the watermark
//     hasn't been stored yet (first run after deploy of this change).
//
// Using Feishu's own updated_at value for the incremental watermark removes
// the local-clock dependency in the previous design: a Multica server clock
// that runs ahead of (or behind) Feishu's gateway no longer eats into the
// 10-minute overlap window.
func feishuProjectSinceUnixMilliForTrigger(cfg db.FeishuProjectIntegration, trigger string, now time.Time) int64 {
	switch trigger {
	case "manual":
		return feishuProjectManualSyncSinceUnixMilli(now)
	case "reconcile":
		return now.Add(-feishuProjectReconcileLookback - feishuProjectReconcileLookbackSafety).UnixMilli()
	default:
		if cfg.LastSeenUpdatedAtMs.Valid && cfg.LastSeenUpdatedAtMs.Int64 > 0 {
			return cfg.LastSeenUpdatedAtMs.Int64 - feishuProjectIncrementalReplay.Milliseconds()
		}
		return feishuProjectSyncSinceUnixMilli(cfg, now)
	}
}

func buildFeishuProjectSyncMQL(projectKey, workItemType string, statuses []string, sinceDate, extraFilter string, offset, limit int) string {
	conditions := []string{
		fmt.Sprintf("`work_item_status` IN (%s)", quoteMQLStrings(statuses)),
		fmt.Sprintf("`updated_at` >= %s", quoteMQLString(sinceDate)),
	}
	if filter := normalizeFeishuProjectMQLFilter(extraFilter); filter != "" {
		conditions = append(conditions, "("+filter+")")
	}
	return fmt.Sprintf(
		"SELECT `work_item_id`, `name`, `description`, `work_item_status`, `priority`, `updated_at` FROM `%s`.`%s` WHERE %s ORDER BY `updated_at` DESC LIMIT %d, %d",
		escapeMQLIdent(projectKey),
		escapeMQLIdent(workItemType),
		strings.Join(conditions, " AND "),
		offset,
		limit,
	)
}

func normalizeFeishuProjectMQLFilter(filter string) string {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToUpper(filter), "WHERE ") {
		return strings.TrimSpace(filter[len("WHERE "):])
	}
	return filter
}

func quoteMQLStrings(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			quoted = append(quoted, quoteMQLString(value))
		}
	}
	return strings.Join(quoted, ", ")
}

func quoteMQLString(value string) string {
	return "'" + strings.ReplaceAll(strings.TrimSpace(value), "'", "''") + "'"
}

func (c *FeishuProjectClient) QueryWorkItems(ctx context.Context, cfg db.FeishuProjectIntegration, workItemType string, fullSync bool) ([]FeishuProjectWorkItem, error) {
	var out []FeishuProjectWorkItem
	err := c.QueryWorkItemPagesWithOptions(ctx, cfg, workItemType, fullSync, FeishuProjectSyncOptions{}, func(page FeishuProjectWorkItemPage) error {
		out = append(out, page.Items...)
		return nil
	})
	return out, err
}

func (c *FeishuProjectClient) QueryWorkItemPages(ctx context.Context, cfg db.FeishuProjectIntegration, workItemType string, fullSync bool, handle func(FeishuProjectWorkItemPage) error) error {
	return c.QueryWorkItemPagesWithOptions(ctx, cfg, workItemType, fullSync, FeishuProjectSyncOptions{}, handle)
}

func (c *FeishuProjectClient) QueryWorkItemPagesWithOptions(ctx context.Context, cfg db.FeishuProjectIntegration, workItemType string, fullSync bool, opts FeishuProjectSyncOptions, handle func(FeishuProjectWorkItemPage) error) error {
	statuses := mappedFeishuProjectStatuses(cfg.StatusMapping, workItemType)
	if len(statuses) == 0 {
		return ErrFeishuProjectSyncScopeRequired
	}
	pageNum := 1
	for page := 0; page < feishuProjectSyncMaxPages; page++ {
		req := map[string]any{
			"work_item_type_keys": []string{workItemType},
			"work_item_status":    feishuProjectWorkItemStatusFilter(statuses),
			"page_num":            pageNum,
			"page_size":           feishuProjectSyncPageSize,
			"expand": map[string]any{
				"need_multi_text":  true,
				"need_user_detail": true,
			},
		}
		if strings.TrimSpace(opts.WorkItemID) != "" {
			req["work_item_ids"] = []string{strings.TrimSpace(opts.WorkItemID)}
		}
		now := time.Now()
		// Sync entry computes the precise lookback per trigger and threads it
		// through opts.SinceUnixMilli. Direct callers (tests, future ad-hoc
		// uses) without that context fall back to the legacy fullSync-aware
		// default.
		updatedAtStart := opts.SinceUnixMilli
		if updatedAtStart == 0 {
			updatedAtStart = feishuProjectSyncSinceUnixMilli(cfg, now)
			if fullSync {
				updatedAtStart = feishuProjectManualSyncSinceUnixMilli(now)
			}
		}
		req["updated_at"] = map[string]any{
			"start": updatedAtStart,
		}
		payload, err := c.openAPI(ctx, cfg, http.MethodPost, fmt.Sprintf("/open_api/%s/work_item/filter", cfg.ProjectKey), req)
		if err != nil {
			return err
		}
		items := parseFeishuProjectSearch(payload, workItemType, cfg.ProjectKey, strings.TrimSpace(cfg.BusinessLineFieldKey))
		total, hasTotal := feishuProjectOpenAPITotal(payload)
		if !hasTotal {
			total = 0
		}
		if handle != nil {
			if err := handle(FeishuProjectWorkItemPage{Items: items, PageNum: pageNum, Total: total}); err != nil {
				return err
			}
		}
		if strings.TrimSpace(opts.WorkItemID) != "" {
			return nil
		}
		if hasTotal {
			if pageNum*feishuProjectSyncPageSize >= total {
				return nil
			}
			pageNum++
			continue
		}
		if len(items) < feishuProjectSyncPageSize {
			return nil
		}
		pageNum++
	}
	return fmt.Errorf("Feishu Project sync stopped after %d pages; narrow the sync scope", feishuProjectSyncMaxPages)
}

func feishuProjectWorkItemStatusFilter(statuses []string) []map[string]any {
	out := make([]map[string]any, 0, len(statuses))
	for _, status := range statuses {
		status = strings.TrimSpace(status)
		if status != "" {
			out = append(out, map[string]any{"state_key": status})
		}
	}
	return out
}

func (c *FeishuProjectClient) projectMQLTableName(ctx context.Context, cfg db.FeishuProjectIntegration) (string, error) {
	body := map[string]any{
		"project_keys": []string{cfg.ProjectKey},
	}
	if cfg.ActorUserKey.Valid && strings.TrimSpace(cfg.ActorUserKey.String) != "" {
		body["user_key"] = strings.TrimSpace(cfg.ActorUserKey.String)
	}
	payload, err := c.openAPI(ctx, cfg, http.MethodPost, "/open_api/projects/detail", body)
	if err != nil {
		return "", err
	}
	data, _ := payload["data"].(map[string]any)
	for _, projectAny := range data {
		project, _ := projectAny.(map[string]any)
		if simple := firstNonEmpty(fmt.Sprint(project["simple_name"])); simple != "" {
			return simple, nil
		}
	}
	return cfg.ProjectKey, nil
}

func (c *FeishuProjectClient) mappedStatusLabels(ctx context.Context, cfg db.FeishuProjectIntegration, workItemType string) ([]string, error) {
	mapped := mappedFeishuProjectStatuses(cfg.StatusMapping, workItemType)
	if len(mapped) == 0 {
		return nil, nil
	}
	options, err := c.IssueStatusOptions(ctx, cfg)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]string, len(options))
	byName := make(map[string]string, len(options))
	for _, option := range options {
		byKey[option.Key] = option.Name
		byName[option.Name] = option.Name
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(mapped))
	for _, status := range mapped {
		label := firstNonEmpty(byKey[status], byName[status], status)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, label)
	}
	return out, nil
}

func (c *FeishuProjectClient) IssueStatusOptions(ctx context.Context, cfg db.FeishuProjectIntegration) ([]FeishuProjectStatusOption, error) {
	var statuses []FeishuProjectStatusOption
	templates, err := c.openAPI(ctx, cfg, http.MethodGet, fmt.Sprintf("/open_api/%s/template_list/%s", cfg.ProjectKey, "issue"), nil)
	if err == nil {
		for _, templateID := range parseFeishuProjectTemplateIDs(templates) {
			detail, err := c.openAPI(ctx, cfg, http.MethodGet, fmt.Sprintf("/open_api/%s/template_detail/%s", cfg.ProjectKey, templateID), nil)
			if err != nil {
				return nil, err
			}
			statuses = appendFeishuProjectStatuses(statuses, parseFeishuProjectStateFlowStatuses(detail)...)
		}
	}
	if len(statuses) > 0 {
		return statuses, nil
	}
	payload, err := c.openAPI(ctx, cfg, http.MethodGet, fmt.Sprintf("/open_api/%s/work_item/%s/meta", cfg.ProjectKey, "issue"), nil)
	if err != nil {
		return nil, err
	}
	statuses = parseFeishuProjectStatusOptions(payload)
	if len(statuses) == 0 {
		return nil, fmt.Errorf("Feishu Project issue status metadata is empty")
	}
	return statuses, nil
}

// ListWorkItemFields returns the field definitions of a work-item type so the user
// can pick which one to designate as the business-line field or use in a label-sync
// rule. Backed by POST /open_api/{project_key}/field/all — Meego's
// /work_item/{type}/meta omits custom radio/plugin fields like "BUG提单助手"
// (field_c1f194), whereas /field/all is the complete field registry and /meta's
// output is a strict subset of it. We filter by work_item_scopes so the UI only
// surfaces fields actually applicable to this work-item type.
func (c *FeishuProjectClient) ListWorkItemFields(ctx context.Context, cfg db.FeishuProjectIntegration, workItemType string) ([]FeishuProjectFieldMeta, error) {
	if workItemType == "" {
		workItemType = "issue"
	}
	payload, err := c.openAPI(ctx, cfg, http.MethodPost, fmt.Sprintf("/open_api/%s/field/all", cfg.ProjectKey), map[string]any{})
	if err != nil {
		return nil, err
	}
	return parseFeishuProjectFieldMetas(payload, workItemType), nil
}

// ListFieldOptions returns the option tree of a specific work-item field. Used by the
// routing UI to populate the business-line tree based on the operator's field choice.
//
// Two cases Meego presents:
//  1. Custom select fields (`_select` / `_multi_select` / ...) carry their option list
//     inline in the meta payload — we extract from there.
//  2. Built-in business-line fields (`_biz_line` type) DON'T carry inline options;
//     their valid values come from the space-wide /open_api/{key}/business/all tree.
//     For this case we fall back to that endpoint so the UI doesn't end up empty when
//     the operator picks the "obvious" 业务线 field.
//
// Returns nil only if the field isn't found at all OR has no options under either path.
// Caller (handler/UI) treats nil as "this field has no selectable values".
func (c *FeishuProjectClient) ListFieldOptions(ctx context.Context, cfg db.FeishuProjectIntegration, workItemType, fieldKey string) ([]FeishuProjectFieldOption, error) {
	if workItemType == "" {
		workItemType = "issue"
	}
	if strings.TrimSpace(fieldKey) == "" {
		return nil, fmt.Errorf("field_key is required")
	}
	payload, err := c.openAPI(ctx, cfg, http.MethodGet, fmt.Sprintf("/open_api/%s/work_item/%s/meta", cfg.ProjectKey, workItemType), nil)
	if err != nil {
		return nil, err
	}
	field := findFeishuProjectFieldByKey(payload, fieldKey)
	if field != nil {
		if tree := extractFeishuProjectFieldOptionTree(field); len(tree) > 0 {
			return tree, nil
		}
	}
	// Fallback: maybe this is a _biz_line field whose options live in the space tree.
	// We don't gate on field type because Meego's type keys aren't stable across
	// space versions; just try the second source and accept whatever comes back.
	bizPayload, err := c.openAPI(ctx, cfg, http.MethodGet, fmt.Sprintf("/open_api/%s/business/all", cfg.ProjectKey), nil)
	if err != nil {
		// Don't mask the original "field has no inline options" with a permission
		// error from /business/all — return nil and let the caller surface the
		// empty-state message that points at both possible causes.
		slog.Info("Feishu Project /business/all fallback failed",
			"project_key", cfg.ProjectKey, "field_key", fieldKey, "error", err)
		return nil, nil
	}
	return parseFeishuProjectBusinessLineTree(bizPayload), nil
}

// findFeishuProjectFieldByKey walks a meta payload looking for a field entry whose
// field_key (or field_alias) matches. Meego's meta document nests fields under
// "fields"/"field_list"/tab containers, so we recurse.
func findFeishuProjectFieldByKey(payload map[string]any, fieldKey string) map[string]any {
	var found map[string]any
	var walk func(any)
	walk = func(v any) {
		if found != nil {
			return
		}
		switch x := v.(type) {
		case map[string]any:
			k := strings.TrimSpace(firstNonEmpty(fmt.Sprint(x["field_key"]), fmt.Sprint(x["field_alias"])))
			if k == fieldKey {
				found = x
				return
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(payload)
	return found
}

// extractFeishuProjectFieldOptionTree pulls the option tree out of a single field node.
// Meego stores it under any of `option` / `options` / sometimes nested inside
// `field_value`. We try those and feed the result to the existing biz-line tree parser
// which already knows how to handle nested children with id/option_id/key shapes.
func extractFeishuProjectFieldOptionTree(field map[string]any) []FeishuProjectFieldOption {
	for _, k := range []string{"option", "options"} {
		if v, ok := field[k]; ok {
			return parseFeishuProjectBusinessLineTree(map[string]any{"data": v})
		}
	}
	return nil
}

func (c *FeishuProjectClient) TransitionStatus(ctx context.Context, cfg db.FeishuProjectIntegration, workItemID, workItemType, targetStatus string) error {
	if targetStatus == "" {
		return nil
	}
	payload, err := c.openAPI(ctx, cfg, http.MethodPost, fmt.Sprintf("/open_api/%s/work_item/%s/%s/workflow/query", cfg.ProjectKey, workItemType, workItemID), map[string]any{
		"flow_type": 1,
	})
	if err != nil {
		return err
	}
	transitionID := findTransitionID(payload, targetStatus)
	if transitionID == "" {
		return fmt.Errorf("no Feishu Project transition to %q for work item %s", targetStatus, workItemID)
	}
	transitionIDValue, err := strconv.ParseInt(transitionID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid Feishu Project transition id %q: %w", transitionID, err)
	}
	_, err = c.openAPI(ctx, cfg, http.MethodPost, fmt.Sprintf("/open_api/%s/workflow/%s/%s/node/state_change", cfg.ProjectKey, workItemType, workItemID), map[string]any{
		"transition_id": transitionIDValue,
	})
	return err
}

func (c *FeishuProjectClient) openAPI(ctx context.Context, cfg db.FeishuProjectIntegration, method, path string, body any) (map[string]any, error) {
	token, err := c.pluginToken(ctx, cfg.PluginID, cfg.PluginSecret)
	if err != nil {
		return nil, err
	}
	var rawBody []byte
	if body != nil {
		rawBody, _ = json.Marshal(body)
	}
	var lastErr error
	for attempt := 1; attempt <= feishuProjectAPIRetryAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(rawBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-PLUGIN-TOKEN", token)
		if cfg.ActorUserKey.Valid {
			req.Header.Set("X-USER-KEY", cfg.ActorUserKey.String)
		}
		resp, err := c.httpClient().Do(req)
		if err != nil {
			lastErr = err
			if attempt < feishuProjectAPIRetryAttempts && feishuProjectSleep(ctx, feishuProjectRetryDelay(attempt)) {
				continue
			}
			return nil, err
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("Feishu Project API %s %s http %d: %s", method, path, resp.StatusCode, string(raw))
			if !feishuProjectRetryableHTTPStatus(resp.StatusCode) || attempt >= feishuProjectAPIRetryAttempts {
				return nil, lastErr
			}
			delay := feishuProjectRetryDelay(attempt)
			if resp.StatusCode == http.StatusTooManyRequests {
				if d := feishuProjectRateLimitResetDelay(resp.Header); d > 0 {
					delay = d
				} else {
					delay = feishuProjectRateLimitFallbackDelay(attempt)
				}
			}
			slog.Warn("Feishu Project API retrying",
				"method", method,
				"path", path,
				"status", resp.StatusCode,
				"attempt", attempt,
				"max_attempts", feishuProjectAPIRetryAttempts,
				"delay", delay,
				"body", string(raw),
			)
			if !feishuProjectSleep(ctx, delay) {
				return nil, lastErr
			}
			continue
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		if msg := feishuProjectAPIError(out); msg != "" {
			// Monthly tenant quota — refilled only on the 1st of next month, so
			// no amount of retrying within this run will succeed.
			if feishuProjectIsQuotaExhausted(out) {
				return nil, fmt.Errorf("Feishu Project API %s %s: %s: %w", method, path, msg, ErrFeishuProjectQuotaExhausted)
			}
			lastErr = fmt.Errorf("Feishu Project API %s %s failed: %s", method, path, msg)
			if !feishuProjectRetryableAPIError(out) || attempt >= feishuProjectAPIRetryAttempts {
				return nil, lastErr
			}
			delay := feishuProjectRetryDelay(attempt)
			if feishuProjectIsRateLimited(out) {
				if d := feishuProjectRateLimitResetDelay(resp.Header); d > 0 {
					delay = d
				} else {
					delay = feishuProjectRateLimitFallbackDelay(attempt)
				}
			}
			slog.Warn("Feishu Project API retrying",
				"method", method,
				"path", path,
				"attempt", attempt,
				"max_attempts", feishuProjectAPIRetryAttempts,
				"delay", delay,
				"error", msg,
			)
			if !feishuProjectSleep(ctx, delay) {
				return nil, lastErr
			}
			continue
		}
		return out, nil
	}
	return nil, lastErr
}

// feishuProjectSleep blocks for d, returning false if the context is cancelled
// before the timer fires. Non-positive d returns true immediately.
func feishuProjectSleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// feishuProjectRetryDelay returns the linear-backoff duration we use for
// transient errors that are *not* rate-limit-related (network blips, 5xx,
// upstream gateway timeouts). Rate-limited responses use a separate, larger
// delay computed from the gateway reset header.
func feishuProjectRetryDelay(attempt int) time.Duration {
	return time.Duration(attempt) * time.Second
}

// feishuProjectRateLimitResetDelay reads Feishu's gateway reset header. The
// gateway emits the number of whole seconds until the per-token-per-interface
// bucket recovers; capped at feishuProjectRateLimitMaxSleep so a malformed
// response can't stall a worker indefinitely.
func feishuProjectRateLimitResetDelay(h http.Header) time.Duration {
	raw := strings.TrimSpace(h.Get("x-ogw-ratelimit-reset"))
	if raw == "" {
		return 0
	}
	secs, err := strconv.Atoi(raw)
	if err != nil || secs <= 0 {
		return 0
	}
	d := time.Duration(secs) * time.Second
	if d > feishuProjectRateLimitMaxSleep {
		d = feishuProjectRateLimitMaxSleep
	}
	return d
}

// feishuProjectRateLimitFallbackDelay is the exponential backoff used when the
// gateway returns 429 / err_code 99991400 but no reset header — 2s, 4s, 8s, 16s.
func feishuProjectRateLimitFallbackDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := time.Duration(1<<attempt) * time.Second
	if d > feishuProjectRateLimitMaxSleep {
		d = feishuProjectRateLimitMaxSleep
	}
	return d
}

func feishuProjectRetryableHTTPStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// feishuProjectErrCode pulls err_code from the response envelope, falling back
// to nested err.code (used by a few legacy Feishu Project endpoints).
func feishuProjectErrCode(payload map[string]any) int {
	code, _ := feishuProjectInt(payload["err_code"])
	if code == 0 {
		if errMap, _ := payload["err"].(map[string]any); errMap != nil {
			code, _ = feishuProjectInt(errMap["code"])
		}
	}
	return code
}

// feishuProjectIsRateLimited reports whether the response body indicates the
// per-token-per-interface QPS limit was hit. err_code 99991400 with msg
// "request trigger frequency limit" is the documented signal.
func feishuProjectIsRateLimited(payload map[string]any) bool {
	if feishuProjectErrCode(payload) == 99991400 {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["err_msg"]) + " " + fmt.Sprint(payload["msg"])))
	return strings.Contains(msg, "frequency limit") || strings.Contains(msg, "too many requests")
}

// feishuProjectIsQuotaExhausted reports whether the tenant's monthly Feishu
// Open Platform quota has been consumed (err_code 99991403, enforced since
// 2024-12-03). The bucket only refills on the 1st of the next natural month,
// so retrying within the same run is futile.
func feishuProjectIsQuotaExhausted(payload map[string]any) bool {
	return feishuProjectErrCode(payload) == 99991403
}

func feishuProjectRetryableAPIError(payload map[string]any) bool {
	if feishuProjectIsRateLimited(payload) {
		return true
	}
	if feishuProjectErrCode(payload) == 50007 {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["err_msg"]) + " " + fmt.Sprint(payload["msg"])))
	return strings.Contains(msg, "gateway timeout") || strings.Contains(msg, "try again later")
}

func (c *FeishuProjectClient) callTool(ctx context.Context, cfg db.FeishuProjectIntegration, name string, args map[string]any) (map[string]any, error) {
	token, err := c.pluginToken(ctx, cfg.PluginID, cfg.PluginSecret)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.MCPURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json,text/event-stream")
	req.Header.Set("X-Mcp-Token", token)
	if cfg.ActorUserKey.Valid {
		req.Header.Set("X-USER-KEY", cfg.ActorUserKey.String)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Feishu Project tool %s http %d: %s", name, resp.StatusCode, string(raw))
	}
	var envelope struct {
		Error  any `json:"error"`
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("Feishu Project tool %s failed: %v", name, envelope.Error)
	}
	texts := make([]string, 0, len(envelope.Result.Content))
	for _, item := range envelope.Result.Content {
		if strings.HasPrefix(item.Text, "log_id:") || strings.HasPrefix(item.Text, "logid:") {
			continue
		}
		if strings.TrimSpace(item.Text) != "" {
			texts = append(texts, strings.TrimSpace(item.Text))
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(item.Text), &out); err == nil {
			if toolErr := feishuProjectToolError(out); toolErr != "" {
				return nil, fmt.Errorf("Feishu Project tool %s failed: %s", name, toolErr)
			}
			return out, nil
		}
	}
	if envelope.Result.IsError || len(texts) > 0 {
		return nil, fmt.Errorf("Feishu Project tool %s failed: %s", name, strings.Join(texts, "; "))
	}
	return map[string]any{}, nil
}

func (c *FeishuProjectClient) DownloadAttachment(ctx context.Context, cfg db.FeishuProjectIntegration, item FeishuProjectWorkItem, att FeishuProjectAttachment) ([]byte, string, string, error) {
	if att.ID != "" {
		token, err := c.pluginToken(ctx, cfg.PluginID, cfg.PluginSecret)
		if err != nil {
			return nil, "", "", err
		}
		payload := map[string]any{"uuid": att.ID}
		var lastErr error
		for attempt := 1; attempt <= feishuProjectDownloadRetryAttempts; attempt++ {
			outcome, reqErr := c.downloadAttachmentRequest(ctx, cfg, item, token, payload)
			if reqErr == nil {
				return outcome.raw, firstNonEmpty(outcome.filename, att.Name), firstNonEmpty(outcome.contentType, att.ContentType), nil
			}
			lastErr = reqErr
			// Quota exhaustion will not heal until the next natural month;
			// caller should bail out instead of burning retry budget.
			if errors.Is(reqErr, ErrFeishuProjectQuotaExhausted) {
				return nil, "", "", reqErr
			}
			if !outcome.retryable || attempt == feishuProjectDownloadRetryAttempts {
				break
			}
			delay := feishuProjectDownloadRetryInitialDelay * time.Duration(1<<(attempt-1))
			if outcome.rateLimited {
				if outcome.retryAfter > 0 {
					delay = outcome.retryAfter
				} else {
					delay = feishuProjectRateLimitFallbackDelay(attempt)
				}
			}
			slog.Warn("Feishu Project attachment download retrying",
				"workspace_id", UUIDString(cfg.WorkspaceID),
				"project_key", cfg.ProjectKey,
				"work_item_type", item.Type,
				"work_item_id", item.ID,
				"attachment_id", att.ID,
				"attempt", attempt,
				"max_attempts", feishuProjectDownloadRetryAttempts,
				"delay", delay,
				"rate_limited", outcome.rateLimited,
				"error", reqErr,
			)
			if !feishuProjectSleep(ctx, delay) {
				return nil, "", "", ctx.Err()
			}
		}
		// goapi/project.feishu.cn URLs require a session cookie, so the GET fallback below cannot
		// succeed with the plugin token — only surface the POST error.
		if strings.TrimSpace(att.URL) == "" || looksLikeFeishuProjectFileURL(att.URL) {
			return nil, "", "", lastErr
		}
	}
	if strings.TrimSpace(att.URL) == "" {
		return nil, "", "", fmt.Errorf("Feishu Project attachment %q has no downloadable url or uuid", att.Name)
	}
	if looksLikeFeishuProjectFileURL(att.URL) {
		return nil, "", "", fmt.Errorf("Feishu Project attachment %q has no downloadable uuid (goapi URL requires session cookie)", att.Name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, att.URL, nil)
	if err != nil {
		return nil, "", "", err
	}
	if token, tokenErr := c.pluginToken(ctx, cfg.PluginID, cfg.PluginSecret); tokenErr == nil {
		req.Header.Set("X-PLUGIN-TOKEN", token)
		if cfg.ActorUserKey.Valid {
			req.Header.Set("X-USER-KEY", cfg.ActorUserKey.String)
		}
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, feishuProjectAttachmentMaxSize+1))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", fmt.Errorf("Feishu Project attachment download http %d: %s", resp.StatusCode, string(raw))
	}
	return raw, firstNonEmpty(filenameFromContentDisposition(resp.Header.Get("Content-Disposition")), att.Name), firstNonEmpty(resp.Header.Get("Content-Type"), att.ContentType), nil
}

// feishuProjectDownloadOutcome carries both the response body and the retry
// hints derived from the response (status, body err_code, rate-limit headers).
// Embedded inline rather than returned as a 7-tuple to keep call sites readable.
type feishuProjectDownloadOutcome struct {
	raw         []byte
	filename    string
	contentType string
	retryable   bool
	rateLimited bool
	retryAfter  time.Duration
}

func (c *FeishuProjectClient) downloadAttachmentRequest(ctx context.Context, cfg db.FeishuProjectIntegration, item FeishuProjectWorkItem, token string, body any) (feishuProjectDownloadOutcome, error) {
	rawBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+fmt.Sprintf("/open_api/%s/work_item/%s/%s/file/download", cfg.ProjectKey, item.Type, item.ID), bytes.NewReader(rawBody))
	if err != nil {
		return feishuProjectDownloadOutcome{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PLUGIN-TOKEN", token)
	if cfg.ActorUserKey.Valid {
		req.Header.Set("X-USER-KEY", cfg.ActorUserKey.String)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		// Network-level failures (timeouts, resets) are almost always transient.
		return feishuProjectDownloadOutcome{retryable: true}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, feishuProjectAttachmentMaxSize+1))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		outcome := feishuProjectDownloadOutcome{
			retryable: feishuProjectAttachmentRetryableHTTPStatus(resp.StatusCode),
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			outcome.rateLimited = true
			outcome.retryAfter = feishuProjectRateLimitResetDelay(resp.Header)
		}
		return outcome, fmt.Errorf("Feishu Project attachment download http %d: %s", resp.StatusCode, string(raw))
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err == nil {
			if msg := feishuProjectAPIError(payload); msg != "" {
				if feishuProjectIsQuotaExhausted(payload) {
					return feishuProjectDownloadOutcome{}, fmt.Errorf("Feishu Project attachment download: %s: %w", msg, ErrFeishuProjectQuotaExhausted)
				}
				outcome := feishuProjectDownloadOutcome{
					retryable: feishuProjectAttachmentRetryableAPIError(payload),
				}
				if feishuProjectIsRateLimited(payload) {
					outcome.rateLimited = true
					outcome.retryAfter = feishuProjectRateLimitResetDelay(resp.Header)
				}
				return outcome, fmt.Errorf("Feishu Project attachment download failed: %s", msg)
			}
		}
	}
	return feishuProjectDownloadOutcome{
		raw:         raw,
		filename:    filenameFromContentDisposition(resp.Header.Get("Content-Disposition")),
		contentType: resp.Header.Get("Content-Type"),
	}, nil
}

func feishuProjectAttachmentRetryableHTTPStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// feishuProjectAttachmentRetryableAPIError treats a small set of Feishu Project error codes as
// transient on the attachment download endpoint:
//   - 30019: internal error observed mid-sync, succeeds on retry within hundreds of ms.
//   - 50007: upstream gateway timeout.
//   - 99991400: per-token-per-interface QPS limit hit; retry after gateway reset.
//
// Anything else (including 4xx-mapped business errors) is permanent.
func feishuProjectAttachmentRetryableAPIError(payload map[string]any) bool {
	if feishuProjectIsRateLimited(payload) {
		return true
	}
	code := feishuProjectErrCode(payload)
	if code == 30019 || code == 50007 {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["err_msg"]) + " " + fmt.Sprint(payload["msg"])))
	return strings.Contains(msg, "gateway timeout") || strings.Contains(msg, "try again later")
}

func filenameFromContentDisposition(raw string) string {
	if raw == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(raw)
	if err != nil {
		return ""
	}
	return params["filename"]
}

func (c *FeishuProjectClient) pluginToken(ctx context.Context, pluginID, pluginSecret string) (string, error) {
	key := pluginID + "\x00" + pluginSecret
	now := time.Now()
	c.pluginTokenMu.Lock()
	if c.cachedPluginToken != "" && c.pluginTokenKey == key && now.Before(c.pluginTokenTill) {
		token := c.cachedPluginToken
		c.pluginTokenMu.Unlock()
		return token, nil
	}
	defer c.pluginTokenMu.Unlock()

	body, _ := json.Marshal(map[string]string{"plugin_id": pluginID, "plugin_secret": pluginSecret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/open_api/authen/plugin_token", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed struct {
		ErrCode int    `json:"err_code"`
		ErrMsg  string `json:"err_msg"`
		Data    struct {
			Token       string `json:"token"`
			PluginToken string `json:"plugin_token"`
		} `json:"data"`
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Feishu Project plugin_token http %d: %s", resp.StatusCode, string(raw))
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	token := parsed.Data.Token
	if token == "" {
		token = parsed.Data.PluginToken
	}
	if parsed.ErrCode != 0 || token == "" {
		return "", fmt.Errorf("Feishu Project plugin_token err_code=%d msg=%q", parsed.ErrCode, parsed.ErrMsg)
	}
	c.cachedPluginToken = token
	c.pluginTokenKey = key
	c.pluginTokenTill = now.Add(50 * time.Minute)
	return token, nil
}

func (c *FeishuProjectClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func parseFeishuProjectMQL(payload map[string]any, typ, projectKey string) []FeishuProjectWorkItem {
	var out []FeishuProjectWorkItem
	data, _ := payload["data"].(map[string]any)
	for _, rowsAny := range data {
		rows, _ := rowsAny.([]any)
		for _, rowAny := range rows {
			row, _ := rowAny.(map[string]any)
			fields, _ := row["moql_field_list"].([]any)
			record := map[string]string{}
			fieldValues := map[string][]string{}
			for _, fieldAny := range fields {
				field, _ := fieldAny.(map[string]any)
				key, _ := field["key"].(string)
				if key == "" {
					continue
				}
				if value := feishuProjectFieldValue(field); value != "" {
					record[key] = value
					addFeishuProjectFieldValues(fieldValues, key, "", []string{value})
				}
			}
			id := record["work_item_id"]
			if id == "" {
				continue
			}
			attachments := feishuProjectMQLAttachments(row)
			status := record["work_item_status"]
			if status == "" {
				status = record["status"]
			}
			description, descriptionAttachments := normalizeFeishuProjectDescription(record["description"])
			attachments = append(attachments, descriptionAttachments...)
			updatedAt := feishuProjectTime(record["updated_at"])
			out = append(out, FeishuProjectWorkItem{
				ID:          id,
				Type:        typ,
				Title:       firstNonEmpty(record["name"], record["title"]),
				Description: description,
				Status:      status,
				Priority:    feishuProjectPriorityValue(record),
				OwnerEmail:  extractEmail(firstNonEmpty(record["owner"], record["operator"])),
				UpdatedAt:   updatedAt,
				URL:         fmt.Sprintf("https://project.feishu.cn/%s/%s/detail/%s", projectKey, typ, id),
				Attachments: dedupeFeishuProjectAttachments(attachments),
				FieldValues: fieldValues,
			})
		}
	}
	return out
}

func feishuProjectMQLCount(payload map[string]any) (int, bool) {
	rows, _ := payload["list"].([]any)
	for _, rowAny := range rows {
		row, _ := rowAny.(map[string]any)
		switch v := row["count"].(type) {
		case float64:
			return int(v), true
		case int:
			return v, true
		case int64:
			return int(v), true
		case json.Number:
			n, err := strconv.Atoi(v.String())
			return n, err == nil
		case string:
			n, err := strconv.Atoi(v)
			return n, err == nil
		}
	}
	return 0, false
}

func feishuProjectOpenAPITotal(payload map[string]any) (int, bool) {
	pagination, _ := payload["pagination"].(map[string]any)
	return feishuProjectInt(pagination["total"])
}

func feishuProjectInt(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		n, err := strconv.Atoi(v.String())
		return n, err == nil
	case string:
		n, err := strconv.Atoi(v)
		return n, err == nil
	default:
		return 0, false
	}
}

func parseFeishuProjectSearch(payload map[string]any, typ, projectKey, businessLineFieldKey string) []FeishuProjectWorkItem {
	var out []FeishuProjectWorkItem
	rows, _ := payload["data"].([]any)
	for _, rowAny := range rows {
		row, _ := rowAny.(map[string]any)
		id := firstNonEmpty(feishuProjectIDString(row["id"]), feishuProjectIDString(row["work_item_id"]))
		if id == "" {
			continue
		}
		record := map[string]string{
			"name":               fmt.Sprint(row["name"]),
			"priority":           fmt.Sprint(row["priority"]),
			"work_item_priority": fmt.Sprint(row["work_item_priority"]),
			"sub_stage":          fmt.Sprint(row["sub_stage"]),
			"current_status":     fmt.Sprint(row["current_status"]),
			"work_item_status":   feishuProjectStatusValue(row["work_item_status"]),
			"current_status_key": feishuProjectStatusValue(row["current_status_key"]),
		}
		if ts := feishuProjectTime(row["updated_at"]); !ts.IsZero() {
			record["updated_at"] = ts.Format(time.RFC3339Nano)
		}
		userEmails := feishuProjectUserEmails(row)
		fieldValues := map[string][]string{}
		var businessLineTokens []FeishuBusinessLineToken
		fields, _ := row["fields"].([]any)
		var attachments []FeishuProjectAttachment
		// Index each field by both its field_key and its Chinese display name. Two spaces
		// can give the same logical field different names (经办人 vs 处理人 vs 负责人)
		// — sometimes even custom field_keys like `field_xxx` — so downstream lookups
		// (notably owner-email extraction) need to try by display name too.
		for _, fieldAny := range fields {
			field, _ := fieldAny.(map[string]any)
			key := firstNonEmpty(fmt.Sprint(field["field_key"]), fmt.Sprint(field["field_alias"]))
			if key == "" {
				continue
			}
			attachments = append(attachments, feishuProjectOpenAPIFieldAttachments(field)...)
			value := feishuProjectOpenAPIFieldValue(field)
			displayName := feishuFieldDisplayName(field)
			if feishuProjectIsOwnerField(key, displayName) {
				value = firstNonEmpty(feishuProjectOpenAPIOwnerFieldValue(field["field_value"]), value)
			}
			if value != "" {
				record[key] = value
				if displayName != "" {
					record[displayName] = value
				}
			}
			addFeishuProjectFieldValues(fieldValues, key, displayName, feishuProjectOpenAPIFieldValues(field["field_value"]))
			if businessLineFieldKey != "" && key == businessLineFieldKey {
				businessLineTokens = extractBusinessLineTokens(field["field_value"])
			}
		}
		multiTexts, _ := row["multi_texts"].([]any)
		for _, fieldAny := range multiTexts {
			field, _ := fieldAny.(map[string]any)
			key := fmt.Sprint(field["field_key"])
			if key == "" {
				continue
			}
			attachments = append(attachments, feishuProjectOpenAPIFieldAttachments(field)...)
			value := feishuProjectOpenAPIFieldValue(field)
			displayName := feishuFieldDisplayName(field)
			if feishuProjectIsOwnerField(key, displayName) {
				value = firstNonEmpty(feishuProjectOpenAPIOwnerFieldValue(field["field_value"]), value)
			}
			if value != "" {
				record[key] = value
				if displayName != "" {
					record[displayName] = value
				}
			}
			addFeishuProjectFieldValues(fieldValues, key, displayName, feishuProjectOpenAPIFieldValues(field["field_value"]))
			if businessLineFieldKey != "" && len(businessLineTokens) == 0 && key == businessLineFieldKey {
				businessLineTokens = extractBusinessLineTokens(field["field_value"])
			}
		}
		description, descriptionAttachments := normalizeFeishuProjectDescription(record["description"])
		attachments = append(attachments, descriptionAttachments...)
		updatedAt, _ := time.Parse(time.RFC3339Nano, record["updated_at"])
		ownerEmail := firstNonEmpty(feishuProjectOperatorRoleEmail(row, userEmails), feishuProjectOwnerEmail(record, userEmails))
		out = append(out, FeishuProjectWorkItem{
			ID:                 id,
			Type:               typ,
			Title:              firstNonEmpty(record["name"], record["title"]),
			Description:        description,
			Status:             firstNonEmpty(record["work_item_status"], record["sub_stage"], record["status"]),
			Priority:           feishuProjectPriorityValue(record),
			OwnerEmail:         ownerEmail,
			UpdatedAt:          updatedAt,
			URL:                fmt.Sprintf("https://project.feishu.cn/%s/%s/detail/%s", projectKey, typ, id),
			Attachments:        dedupeFeishuProjectAttachments(attachments),
			BusinessLineTokens: businessLineTokens,
			FieldValues:        fieldValues,
		})
	}
	return out
}

func addFeishuProjectFieldValues(out map[string][]string, key, displayName string, values []string) {
	keys := []string{strings.TrimSpace(key), strings.TrimSpace(displayName)}
	seenKeys := map[string]bool{}
	for _, k := range keys {
		if k == "" || k == "<nil>" || seenKeys[k] {
			continue
		}
		seenKeys[k] = true
		seenVals := map[string]bool{}
		for _, existing := range out[k] {
			seenVals[existing] = true
		}
		for _, v := range values {
			v = strings.TrimSpace(v)
			if v == "" || v == "<nil>" || seenVals[v] {
				continue
			}
			out[k] = append(out[k], v)
			seenVals[v] = true
		}
	}
}

func feishuProjectPriorityValue(record map[string]string) string {
	for _, key := range []string{
		"priority",
		"work_item_priority",
		"issue_priority",
		"severity",
		"严重程度",
		"优先级",
	} {
		if value := firstNonEmpty(record[key]); value != "" {
			return value
		}
	}
	return ""
}

func feishuProjectOperatorRoleEmail(row map[string]any, userEmails map[string]string) string {
	for _, rolesAny := range []any{row["role_members"], nestedMapValue(row, "work_item_attribute", "role_members")} {
		roles, _ := rolesAny.([]any)
		for _, roleAny := range roles {
			role, _ := roleAny.(map[string]any)
			if !feishuProjectIsOperatorRole(role) {
				continue
			}
			members, _ := role["members"].([]any)
			for _, memberAny := range members {
				member, _ := memberAny.(map[string]any)
				if email := extractEmail(fmt.Sprint(member["email"])); email != "" {
					return email
				}
				for _, key := range []string{
					fmt.Sprint(member["user_key"]),
					fmt.Sprint(member["key"]),
					fmt.Sprint(member["id"]),
					fmt.Sprint(member["username"]),
					fmt.Sprint(member["open_id"]),
					fmt.Sprint(member["union_id"]),
				} {
					if email := userEmails[strings.TrimSpace(key)]; email != "" {
						return email
					}
				}
			}
		}
	}
	for _, field := range feishuProjectAllFieldMaps(row) {
		key := firstNonEmpty(fmt.Sprint(field["field_key"]), fmt.Sprint(field["field_alias"]))
		fieldType := strings.TrimSpace(fmt.Sprint(field["field_type_key"]))
		if key != "role_owners" && fieldType != "role_owners" {
			continue
		}
		if email := feishuProjectRoleOwnersFieldEmail(field["field_value"], userEmails); email != "" {
			return email
		}
	}
	return ""
}

func feishuProjectAllFieldMaps(row map[string]any) []map[string]any {
	var out []map[string]any
	for _, listKey := range []string{"fields", "multi_texts", "work_item_fields"} {
		fields, _ := row[listKey].([]any)
		for _, fieldAny := range fields {
			field, _ := fieldAny.(map[string]any)
			if field != nil {
				out = append(out, field)
			}
		}
	}
	return out
}

func feishuProjectIsOperatorRole(role map[string]any) bool {
	for _, key := range []string{"key", "role_key", "role_id"} {
		if strings.TrimSpace(fmt.Sprint(role[key])) == "operator" {
			return true
		}
	}
	switch strings.TrimSpace(fmt.Sprint(role["name"])) {
	case "处理人", "经办人", "负责人":
		return true
	}
	switch strings.TrimSpace(fmt.Sprint(role["role_name"])) {
	case "处理人", "经办人", "负责人":
		return true
	}
	return false
}

func feishuProjectRoleOwnersFieldEmail(value any, userEmails map[string]string) string {
	roles, _ := value.([]any)
	for _, roleAny := range roles {
		role, _ := roleAny.(map[string]any)
		if !feishuProjectRoleOwnersEntryIsOperator(role) {
			continue
		}
		owners, _ := role["owners"].([]any)
		for _, ownerAny := range owners {
			switch owner := ownerAny.(type) {
			case map[string]any:
				if email := extractEmail(fmt.Sprint(owner["email"])); email != "" {
					return email
				}
				for _, key := range []string{
					fmt.Sprint(owner["user_key"]),
					fmt.Sprint(owner["key"]),
					fmt.Sprint(owner["id"]),
					fmt.Sprint(owner["username"]),
					fmt.Sprint(owner["open_id"]),
					fmt.Sprint(owner["union_id"]),
				} {
					if email := userEmails[strings.TrimSpace(key)]; email != "" {
						return email
					}
				}
			default:
				token := strings.TrimSpace(fmt.Sprint(owner))
				if email := extractEmail(token); email != "" {
					return email
				}
				if email := userEmails[token]; email != "" {
					return email
				}
			}
		}
	}
	return ""
}

func feishuProjectRoleOwnersEntryIsOperator(role map[string]any) bool {
	raw := strings.TrimSpace(fmt.Sprint(role["role"]))
	if raw == "operator" || strings.HasSuffix(raw, "_operator") {
		return true
	}
	return feishuProjectIsOperatorRole(role)
}

func nestedMapValue(row map[string]any, mapKey, valueKey string) any {
	m, _ := row[mapKey].(map[string]any)
	if m == nil {
		return nil
	}
	return m[valueKey]
}

func feishuProjectUserEmails(row map[string]any) map[string]string {
	rows, _ := row["user_details"].([]any)
	out := make(map[string]string, len(rows))
	for _, rowAny := range rows {
		user, _ := rowAny.(map[string]any)
		email := extractEmail(fmt.Sprint(user["email"]))
		if email == "" {
			continue
		}
		for _, key := range []string{
			fmt.Sprint(user["user_key"]),
			fmt.Sprint(user["key"]),
			fmt.Sprint(user["id"]),
			fmt.Sprint(user["username"]),
			fmt.Sprint(user["open_id"]),
			fmt.Sprint(user["union_id"]),
		} {
			key = strings.TrimSpace(key)
			if key != "" && key != "<nil>" {
				out[key] = email
			}
		}
	}
	return out
}

// extractBusinessLineTokens pulls leaf + parent ID/Name pairs out of a Meego biz-line field
// value. The Meego API surfaces the value as either:
//   - a single object {option_id, option_name, parent_option_id, parent_option_name, ...},
//   - an array of such objects (multi-select), or
//   - a primitive id string (rare; we degrade gracefully to a token with just ID set).
//
// We accept any of the common key spellings (id/option_id/key, name/option_name/label) so
// the routing logic doesn't care which Meego shape arrived.
func extractBusinessLineTokens(value any) []FeishuBusinessLineToken {
	if value == nil {
		return nil
	}
	var out []FeishuBusinessLineToken
	pick := func(m map[string]any, keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k]; ok && v != nil {
				s := strings.TrimSpace(fmt.Sprint(v))
				if s != "" && s != "<nil>" {
					return s
				}
			}
		}
		return ""
	}
	visit := func(m map[string]any) {
		tok := FeishuBusinessLineToken{
			ID:         pick(m, "option_id", "id", "key", "value"),
			Name:       pick(m, "option_name", "name", "label", "text"),
			ParentID:   pick(m, "parent_option_id", "parent_id", "parent_key"),
			ParentName: pick(m, "parent_option_name", "parent_name", "parent_label"),
		}
		if tok.ID == "" && tok.Name == "" {
			return
		}
		out = append(out, tok)
	}
	switch v := value.(type) {
	case map[string]any:
		visit(v)
	case []any:
		for _, itemAny := range v {
			switch item := itemAny.(type) {
			case map[string]any:
				visit(item)
			case string:
				s := strings.TrimSpace(item)
				if s != "" {
					out = append(out, FeishuBusinessLineToken{ID: s})
				}
			}
		}
	case string:
		s := strings.TrimSpace(v)
		if s != "" {
			out = append(out, FeishuBusinessLineToken{ID: s})
		}
	}
	return out
}

// feishuFieldDisplayName extracts the Chinese display name from a field entry in the
// Meego /work_item/filter response (and the meta response — same key shape). Both
// `name` (filter response) and `field_name` (meta response) are accepted so the same
// helper works for either source. Returns "" when no usable name is present, so callers
// can early-out without falling into the fmt.Sprint(nil) → "<nil>" trap.
func feishuFieldDisplayName(field map[string]any) string {
	for _, k := range []string{"name", "field_name"} {
		if v, ok := field[k]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func feishuProjectIsOwnerField(key, displayName string) bool {
	key = strings.TrimSpace(key)
	displayName = strings.TrimSpace(displayName)
	switch key {
	case "owner", "operator":
		return true
	}
	switch displayName {
	case "处理人", "经办人", "负责人":
		return true
	}
	return false
}

// feishuProjectOwnerEmail picks the email of the work-item's assignee/handler. It tries:
//  1. The stable Meego field_key `operator` — some spaces expose the handler directly
//     under this key (rare; modern spaces use `issue_operator` + role_owners instead).
//  2. Custom fields indexed by their Chinese display name — different spaces name the
//     assignee field differently (经办人 / 处理人 / 负责人 / etc.) and may use a
//     custom field_key like `field_xxx` so a plain field_key match misses them.
//
// IMPORTANT: we do NOT consult `record["owner"]`. In Meego the field_key `owner` is
// the CREATOR (`创建者`), not the current handler — verified in partopia /field/all where
// `owner.field_name` is consistently "创建者" / "创建人" across every work_item_type.
// Treating it as assignee caused issues like partopia#7004726014, where 经办人 was empty
// in Feishu yet the synced Multica issue was assigned to the creator. When no handler
// signal exists we want OwnerEmail to be empty so the caller can fall back to the
// integration-configured fallback agent.
//
// At each step the raw value is checked for an embedded email pattern first; if none is
// found and Meego returned a user_details lookup, we treat the value as a user_key list
// and resolve against that map.
func feishuProjectOwnerEmail(record map[string]string, userEmails map[string]string) string {
	candidates := []string{
		// Stable field_key (assignee, not creator).
		record["operator"],
		// Chinese display names (fall back when the field is custom and we matched it
		// in parseFeishuProjectSearch via field["name"]).
		record["处理人"], record["经办人"], record["负责人"],
	}
	for _, raw := range candidates {
		if email := extractEmail(raw); email != "" {
			return email
		}
		for _, token := range strings.Split(raw, ",") {
			if email := userEmails[strings.TrimSpace(token)]; email != "" {
				return email
			}
		}
	}
	return ""
}

func feishuProjectOpenAPIOwnerFieldValue(value any) string {
	var values []string
	add := func(v any) {
		s := strings.TrimSpace(fmt.Sprint(v))
		if s != "" && s != "<nil>" {
			values = append(values, s)
		}
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case nil:
			return
		case string:
			add(x)
		case float64:
			add(int64(x))
		case map[string]any:
			if text, _ := x["doc_text"].(string); text != "" {
				add(text)
				return
			}
			for _, key := range []string{
				"email",
				"user_key",
				"key",
				"id",
				"username",
				"open_id",
				"union_id",
				"value",
				"name_cn",
				"name_en",
				"name",
				"label",
			} {
				if v, ok := x[key]; ok {
					add(v)
				}
			}
		case []any:
			for _, item := range x {
				walk(item)
			}
		default:
			add(x)
		}
	}
	walk(value)
	return strings.Join(values, ", ")
}

// parseFeishuProjectFieldMetas walks the /field/all response (payload["data"] is a flat
// list) and returns (field_key, field_name, field_type) triples, deduped by key. If
// workItemType is non-empty, entries are kept only when their work_item_scopes list
// contains it — /field/all is project-wide and includes fields scoped to other types
// (e.g. story-only fields) that the caller doesn't want in this dropdown.
func parseFeishuProjectFieldMetas(payload map[string]any, workItemType string) []FeishuProjectFieldMeta {
	data, _ := payload["data"].([]any)
	seen := map[string]bool{}
	out := make([]FeishuProjectFieldMeta, 0, len(data))
	want := strings.TrimSpace(workItemType)
	for _, entry := range data {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if want != "" && !feishuProjectFieldScopeMatches(m["work_item_scopes"], want) {
			continue
		}
		key := strings.TrimSpace(firstNonEmpty(fmt.Sprint(m["field_key"]), fmt.Sprint(m["field_alias"])))
		if key == "" || key == "<nil>" || seen[key] {
			continue
		}
		out = append(out, FeishuProjectFieldMeta{
			Key:  key,
			Name: firstNonEmpty(fmt.Sprint(m["field_name"]), fmt.Sprint(m["name"]), key),
			Type: firstNonEmpty(fmt.Sprint(m["field_type_key"]), fmt.Sprint(m["field_type"])),
		})
		seen[key] = true
	}
	return out
}

func feishuProjectFieldScopeMatches(scopesAny any, workItemType string) bool {
	scopes, ok := scopesAny.([]any)
	if !ok {
		return false
	}
	for _, s := range scopes {
		if strings.TrimSpace(fmt.Sprint(s)) == workItemType {
			return true
		}
	}
	return false
}

// parseFeishuProjectBusinessLineTree converts the /business/all response into a 2-level
// FeishuProjectFieldOption tree. The Meego response shape varies — top-level "data" can be
// either an array of nodes or an object containing "list"/"items" — so we walk and pick out
// nodes that look like {id, name, children?}.
func parseFeishuProjectBusinessLineTree(payload map[string]any) []FeishuProjectFieldOption {
	pickStr := func(m map[string]any, keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k]; ok && v != nil {
				s := strings.TrimSpace(fmt.Sprint(v))
				if s != "" && s != "<nil>" {
					return s
				}
			}
		}
		return ""
	}
	var toNodes func(any, string, string) []FeishuProjectFieldOption
	toNodes = func(v any, parentID, parentName string) []FeishuProjectFieldOption {
		var nodes []FeishuProjectFieldOption
		switch x := v.(type) {
		case []any:
			for _, child := range x {
				nodes = append(nodes, toNodes(child, parentID, parentName)...)
			}
		case map[string]any:
			id := pickStr(x, "id", "business_id", "option_id", "key")
			name := pickStr(x, "name", "business_name", "option_name", "label")
			if id != "" || name != "" {
				node := FeishuProjectFieldOption{
					ID:         id,
					Name:       name,
					ParentID:   parentID,
					ParentName: parentName,
				}
				if children, ok := x["children"]; ok {
					node.Children = toNodes(children, id, name)
				} else if subItems, ok := x["sub_items"]; ok {
					node.Children = toNodes(subItems, id, name)
				}
				nodes = append(nodes, node)
				return nodes
			}
			// container — recurse looking for a list inside
			for _, key := range []string{"data", "list", "items", "businesses", "business_list"} {
				if inner, ok := x[key]; ok {
					nodes = append(nodes, toNodes(inner, parentID, parentName)...)
				}
			}
		}
		return nodes
	}
	if data, ok := payload["data"]; ok {
		return toNodes(data, "", "")
	}
	return toNodes(payload, "", "")
}

func parseFeishuProjectStatusOptions(payload map[string]any) []FeishuProjectStatusOption {
	var out []FeishuProjectStatusOption
	var walk func(any) bool
	walk = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			if fmt.Sprint(x["field_key"]) == "work_item_status" || fmt.Sprint(x["field_type"]) == "_work_item_status" || fmt.Sprint(x["field_type_key"]) == "_work_item_status" {
				options, _ := x["option"].([]any)
				if len(options) == 0 {
					options, _ = x["options"].([]any)
				}
				for _, optionAny := range options {
					option, _ := optionAny.(map[string]any)
					key := firstNonEmpty(fmt.Sprint(option["option_id"]), fmt.Sprint(option["value"]))
					name := firstNonEmpty(fmt.Sprint(option["option_name"]), fmt.Sprint(option["label"]))
					if key == "" || key == "<nil>" {
						continue
					}
					out = append(out, FeishuProjectStatusOption{Key: key, Name: firstNonEmpty(name, key)})
				}
				return true
			}
			for _, child := range x {
				if walk(child) {
					return true
				}
			}
		case []any:
			for _, child := range x {
				if walk(child) {
					return true
				}
			}
		}
		return false
	}
	walk(payload)
	return out
}

func parseFeishuProjectTemplateIDs(payload map[string]any) []string {
	rows, _ := payload["data"].([]any)
	out := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, rowAny := range rows {
		row, _ := rowAny.(map[string]any)
		id := firstNonEmpty(fmt.Sprint(row["template_id"]), fmt.Sprint(row["id"]))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func parseFeishuProjectStateFlowStatuses(payload map[string]any) []FeishuProjectStatusOption {
	var out []FeishuProjectStatusOption
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if rows, _ := x["state_flow_confs"].([]any); len(rows) > 0 {
				for _, rowAny := range rows {
					row, _ := rowAny.(map[string]any)
					key := firstNonEmpty(fmt.Sprint(row["state_key"]), fmt.Sprint(row["key"]))
					name := firstNonEmpty(fmt.Sprint(row["name"]), fmt.Sprint(row["state_name"]), key)
					if key != "" {
						out = append(out, FeishuProjectStatusOption{Key: key, Name: name})
					}
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(payload)
	return appendFeishuProjectStatuses(nil, out...)
}

func appendFeishuProjectStatuses(base []FeishuProjectStatusOption, items ...FeishuProjectStatusOption) []FeishuProjectStatusOption {
	seen := make(map[string]bool, len(base)+len(items))
	out := make([]FeishuProjectStatusOption, 0, len(base)+len(items))
	for _, item := range append(base, items...) {
		key := strings.TrimSpace(item.Key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, FeishuProjectStatusOption{Key: key, Name: firstNonEmpty(item.Name, key)})
	}
	return out
}

func parseFeishuProjectWorkflowStatuses(payload map[string]any) []FeishuProjectStatusOption {
	data, _ := payload["data"].(map[string]any)
	seen := map[string]bool{}
	var out []FeishuProjectStatusOption
	add := func(key, name string) {
		key = firstNonEmpty(key)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, FeishuProjectStatusOption{Key: key, Name: firstNonEmpty(name, key)})
	}
	nodes, _ := data["state_flow_nodes"].([]any)
	for _, nodeAny := range nodes {
		node, _ := nodeAny.(map[string]any)
		add(fmt.Sprint(node["id"]), fmt.Sprint(node["name"]))
	}
	connections, _ := data["connections"].([]any)
	for _, connAny := range connections {
		conn, _ := connAny.(map[string]any)
		add(fmt.Sprint(conn["source_state_key"]), "")
		add(fmt.Sprint(conn["target_state_key"]), "")
	}
	return out
}

func feishuProjectFieldValue(field map[string]any) string {
	value, _ := field["value"].(map[string]any)
	for _, key := range []string{"long_value", "string_value"} {
		if v, ok := value[key]; ok {
			return fmt.Sprint(v)
		}
	}
	if values, _ := value["key_label_value_list"].([]any); len(values) > 0 {
		first, _ := values[0].(map[string]any)
		return firstNonEmpty(fmt.Sprint(first["label"]), fmt.Sprint(first["key"]))
	}
	if v, _ := value["key_label_value"].(map[string]any); len(v) > 0 {
		return firstNonEmpty(fmt.Sprint(v["label"]), fmt.Sprint(v["key"]))
	}
	if values, _ := value["string_value_list"].([]any); len(values) > 0 {
		return fmt.Sprint(values[0])
	}
	return ""
}

func feishuProjectOpenAPIFieldValues(value any) []string {
	seen := map[string]bool{}
	var out []string
	add := func(v any) {
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" || s == "<nil>" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case nil:
			return
		case string:
			add(x)
		case float64:
			add(int64(x))
		case json.Number:
			add(x.String())
		case map[string]any:
			if text, _ := x["doc_text"].(string); text != "" {
				add(text)
				return
			}
			for _, key := range []string{
				"label",
				"name",
				"option_name",
				"name_cn",
				"name_en",
				"email",
				"value",
				"key",
				"id",
				"option_id",
			} {
				if v, ok := x[key]; ok {
					add(v)
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, item := range x {
				walk(item)
			}
		default:
			add(x)
		}
	}
	walk(value)
	return out
}

func feishuProjectOpenAPIFieldValue(field map[string]any) string {
	value := field["field_value"]
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return fmt.Sprint(int64(v))
	case map[string]any:
		if kv, _ := v["key_label_value"].(map[string]any); len(kv) > 0 {
			return firstNonEmpty(fmt.Sprint(kv["label"]), fmt.Sprint(kv["key"]))
		}
		if list, _ := v["key_label_value_list"].([]any); len(list) > 0 {
			first, _ := list[0].(map[string]any)
			return firstNonEmpty(fmt.Sprint(first["label"]), fmt.Sprint(first["key"]))
		}
		if text, _ := v["doc_text"].(string); text != "" {
			return text
		}
		return firstNonEmpty(fmt.Sprint(v["label"]), fmt.Sprint(v["name"]), fmt.Sprint(v["email"]), fmt.Sprint(v["value"]))
	case []any:
		values := make([]string, 0, len(v))
		for _, itemAny := range v {
			switch item := itemAny.(type) {
			case string:
				values = append(values, item)
			case map[string]any:
				values = append(values, firstNonEmpty(fmt.Sprint(item["email"]), fmt.Sprint(item["name_cn"]), fmt.Sprint(item["name_en"]), fmt.Sprint(item["name"]), fmt.Sprint(item["label"]), fmt.Sprint(item["value"])))
			default:
				values = append(values, fmt.Sprint(item))
			}
		}
		return strings.Join(values, ", ")
	default:
		return fmt.Sprint(v)
	}
}

func feishuProjectMQLAttachments(row map[string]any) []FeishuProjectAttachment {
	var out []FeishuProjectAttachment
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, child := range x {
				walk(child)
			}
		case map[string]any:
			if att, ok := feishuProjectAttachmentFromMap(x); ok {
				out = append(out, att)
			}
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(row)
	return dedupeFeishuProjectAttachments(out)
}

func feishuProjectOpenAPIFieldAttachments(field map[string]any) []FeishuProjectAttachment {
	var out []FeishuProjectAttachment
	out = append(out, feishuProjectRichTextAttachments(field["field_value"])...)
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, child := range x {
				walk(child)
			}
		case map[string]any:
			if att, ok := feishuProjectAttachmentFromMap(x); ok {
				out = append(out, att)
			}
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(field["field_value"])
	return dedupeFeishuProjectAttachments(out)
}

func feishuProjectRichTextAttachments(value any) []FeishuProjectAttachment {
	m, _ := value.(map[string]any)
	if len(m) == 0 {
		return nil
	}
	var out []FeishuProjectAttachment
	if rawDoc, _ := m["doc"].(string); strings.TrimSpace(rawDoc) != "" {
		var doc any
		if err := json.Unmarshal([]byte(rawDoc), &doc); err == nil {
			var walk func(any)
			walk = func(v any) {
				switch x := v.(type) {
				case map[string]any:
					attrs, _ := x["attributes"].(map[string]any)
					if att, ok := feishuProjectRichTextImageFromAttrs(attrs); ok {
						out = append(out, att)
					}
					for _, child := range x {
						walk(child)
					}
				case []any:
					for _, child := range x {
						walk(child)
					}
				}
			}
			walk(doc)
		}
	}
	if rawHTML, _ := m["doc_html"].(string); strings.TrimSpace(rawHTML) != "" {
		out = append(out, feishuProjectRichTextImagesFromHTML(rawHTML)...)
	}
	return dedupeFeishuProjectAttachments(out)
}

func feishuProjectRichTextImageFromAttrs(attrs map[string]any) (FeishuProjectAttachment, bool) {
	if len(attrs) == 0 {
		return FeishuProjectAttachment{}, false
	}
	if fmt.Sprint(attrs["image"]) != "true" {
		return FeishuProjectAttachment{}, false
	}
	id := feishuProjectStringValue(attrs["uuid"])
	rawURL := feishuProjectStringValue(attrs["src"])
	if id == "" && rawURL == "" {
		return FeishuProjectAttachment{}, false
	}
	return FeishuProjectAttachment{
		ID:          id,
		Name:        firstNonEmpty(id, "image"),
		URL:         rawURL,
		ContentType: "image/*",
	}, true
}

func feishuProjectRichTextImagesFromHTML(rawHTML string) []FeishuProjectAttachment {
	re := regexp.MustCompile(`(?is)<img\b[^>]*>`)
	srcRe := regexp.MustCompile(`(?is)\s(src|id|data-name|data-size)=["']([^"']*)["']`)
	var out []FeishuProjectAttachment
	for _, tag := range re.FindAllString(rawHTML, -1) {
		attrs := map[string]string{}
		for _, match := range srcRe.FindAllStringSubmatch(tag, -1) {
			if len(match) == 3 {
				attrs[strings.ToLower(match[1])] = html.UnescapeString(match[2])
			}
		}
		rawURL := strings.TrimSpace(attrs["src"])
		if rawURL == "" || !looksLikeFeishuProjectFileURL(rawURL) {
			continue
		}
		name := firstNonEmpty(attrs["data-name"], attrs["id"], "image")
		out = append(out, FeishuProjectAttachment{
			ID:          attrs["id"],
			Name:        name,
			URL:         rawURL,
			ContentType: "image/*",
			SizeBytes:   feishuProjectInt64Value(attrs["data-size"]),
		})
	}
	return dedupeFeishuProjectAttachments(out)
}

func feishuProjectAttachmentFromMap(m map[string]any) (FeishuProjectAttachment, bool) {
	id := firstNonEmpty(
		feishuProjectStringValue(m["uuid"]),
		feishuProjectStringValue(m["file_token"]),
		feishuProjectStringValue(m["token"]),
		feishuProjectStringValue(m["uid"]),
		feishuProjectStringValue(m["id"]),
	)
	name := firstNonEmpty(
		feishuProjectStringValue(m["name"]),
		feishuProjectStringValue(m["filename"]),
		feishuProjectStringValue(m["file_name"]),
	)
	rawURL := firstNonEmpty(
		feishuProjectStringValue(m["tmp_url"]),
		feishuProjectStringValue(m["url"]),
		feishuProjectStringValue(m["download_url"]),
	)
	contentType := firstNonEmpty(
		feishuProjectStringValue(m["type"]),
		feishuProjectStringValue(m["mime_type"]),
		feishuProjectStringValue(m["content_type"]),
	)
	if !looksLikeAttachmentMap(m) {
		return FeishuProjectAttachment{}, false
	}
	if id == "" && rawURL == "" {
		return FeishuProjectAttachment{}, false
	}
	return FeishuProjectAttachment{
		ID:          id,
		Name:        firstNonEmpty(name, id, "attachment"),
		URL:         rawURL,
		ContentType: contentType,
		SizeBytes:   feishuProjectInt64Value(m["size"], m["size_bytes"]),
	}, true
}

func looksLikeAttachmentMap(m map[string]any) bool {
	for _, key := range []string{"uuid", "file_token", "uid", "tmp_url", "download_url", "mime_type", "content_type", "size_bytes"} {
		if m[key] != nil {
			return true
		}
	}
	return false
}

func dedupeFeishuProjectAttachments(items []FeishuProjectAttachment) []FeishuProjectAttachment {
	seen := map[string]bool{}
	out := make([]FeishuProjectAttachment, 0, len(items))
	for _, item := range items {
		key := firstNonEmpty(item.ID, item.URL, item.Name)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func feishuProjectStringValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case json.Number:
		return x.String()
	default:
		s := fmt.Sprint(x)
		if s == "<nil>" {
			return ""
		}
		return strings.TrimSpace(s)
	}
}

func feishuProjectInt64Value(values ...any) int64 {
	for _, v := range values {
		switch x := v.(type) {
		case float64:
			return int64(x)
		case int64:
			return x
		case int:
			return int64(x)
		case json.Number:
			n, _ := strconv.ParseInt(x.String(), 10, 64)
			return n
		case string:
			return feishuProjectParseSizeBytes(x)
		}
	}
	return 0
}

func feishuProjectParseSizeBytes(raw string) int64 {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	re := regexp.MustCompile(`(?i)^\s*([0-9]+(?:\.[0-9]+)?)\s*([KMGT]?I?B?|B)\s*$`)
	match := re.FindStringSubmatch(s)
	if len(match) != 3 {
		return 0
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0
	}
	unit := strings.ToUpper(match[2])
	multiplier := float64(1)
	switch unit {
	case "", "B":
		multiplier = 1
	case "K", "KB", "KIB":
		multiplier = 1 << 10
	case "M", "MB", "MIB":
		multiplier = 1 << 20
	case "G", "GB", "GIB":
		multiplier = 1 << 30
	case "T", "TB", "TIB":
		multiplier = 1 << 40
	default:
		return 0
	}
	return int64(value * multiplier)
}

func feishuProjectStatusValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		return firstNonEmpty(fmt.Sprint(x["state_key"]), fmt.Sprint(x["name"]), fmt.Sprint(x["label"]))
	default:
		return ""
	}
}

func feishuProjectTime(v any) time.Time {
	switch x := v.(type) {
	case float64:
		return time.UnixMilli(int64(x))
	case int64:
		return time.UnixMilli(x)
	case string:
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return time.UnixMilli(n)
		}
		if t, err := time.Parse(time.RFC3339Nano, x); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02", x); err == nil {
			return t
		}
	}
	return time.Time{}
}

func findTransitionID(payload map[string]any, target string) string {
	raw, _ := json.Marshal(payload)
	var walk func(any) string
	walk = func(v any) string {
		switch x := v.(type) {
		case map[string]any:
			label := firstNonEmpty(
				fmt.Sprint(x["target_state_key"]),
				fmt.Sprint(x["state_key"]),
				fmt.Sprint(x["id"]),
				fmt.Sprint(x["name"]),
				fmt.Sprint(x["state_name"]),
				fmt.Sprint(x["end_state_key_name"]),
			)
			if label == target {
				for _, key := range []string{"transition_id", "uuid"} {
					if x[key] != nil {
						return feishuProjectIDString(x[key])
					}
				}
			}
			for _, child := range x {
				if got := walk(child); got != "" {
					return got
				}
			}
		case []any:
			for _, child := range x {
				if got := walk(child); got != "" {
					return got
				}
			}
		}
		return ""
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	return walk(decoded)
}

func feishuProjectIDString(v any) string {
	switch x := v.(type) {
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return x.String()
	default:
		return fmt.Sprint(x)
	}
}

func escapeMQLIdent(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "`", "")
}

func mqlWhereClause(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return " WHERE " + s
}

func feishuProjectToolError(payload map[string]any) string {
	if payload == nil || payload["error"] == nil {
		return ""
	}
	raw, _ := json.Marshal(payload["error"])
	var parsed struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &parsed); err == nil {
		return firstNonEmpty(parsed.Message, parsed.Code, string(raw))
	}
	return string(raw)
}

func feishuProjectAPIError(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if code, ok := payload["err_code"]; ok && fmt.Sprint(code) != "0" {
		return firstNonEmpty(fmt.Sprint(payload["err_msg"]), fmt.Sprint(payload["message"]), fmt.Sprint(code))
	}
	if code, ok := payload["code"]; ok && fmt.Sprint(code) != "0" {
		return firstNonEmpty(fmt.Sprint(payload["msg"]), fmt.Sprint(payload["message"]), fmt.Sprint(code))
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "<nil>" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

var feishuProjectEmailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

func extractEmail(s string) string {
	return strings.ToLower(feishuProjectEmailRe.FindString(s))
}

func UUIDString(id pgtype.UUID) string {
	return util.UUIDToString(id)
}
