package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ManifestSchemaVersion is the shared schema version used by export, import,
// and backup manifests (5.1 / 5.2 / 5.3).
const ManifestSchemaVersion = "2026-05-31"

const exportQueryLimit int32 = 2147483647
const importIssueLimit = 100
const sourceAppVersion = "dev"

var validIssueStatuses = map[string]struct{}{
	"backlog":     {},
	"todo":        {},
	"in_progress": {},
	"in_review":   {},
	"done":        {},
	"blocked":     {},
	"cancelled":   {},
}

var validIssuePriorities = map[string]struct{}{
	"urgent": {},
	"high":   {},
	"medium": {},
	"low":    {},
	"none":   {},
}

// dataSyncQuerier is the minimal DB interface required by DataSyncService.
// Using an interface keeps the service testable without a real database.
type dataSyncQuerier interface {
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
	ListIssues(ctx context.Context, arg db.ListIssuesParams) ([]db.Issue, error)
	IncrementIssueCounter(ctx context.Context, id pgtype.UUID) (int32, error)
	CreateIssue(ctx context.Context, arg db.CreateIssueParams) (db.Issue, error)
	EnsureDefaultIssueTypes(ctx context.Context, workspaceID pgtype.UUID) error
	GetIssueTypeByKey(ctx context.Context, arg db.GetIssueTypeByKeyParams) (db.IssueType, error)
}

// DataSyncService implements the workspace export / import pipeline.
// It is the single shared backend for 5.1 (export) and 5.2 (import).
type DataSyncService struct {
	Queries dataSyncQuerier
}

// NewDataSyncService creates a DataSyncService backed by q.
func NewDataSyncService(q dataSyncQuerier) *DataSyncService {
	return &DataSyncService{Queries: q}
}

// --- Export types ---

// WorkspaceExportManifest is the canonical JSON format produced by export and
// consumed by import / backup restore.
type WorkspaceExportManifest struct {
	SchemaVersion string            `json:"schema_version"`
	Workspace     ManifestWorkspace `json:"workspace"`
	Data          ManifestData      `json:"data"`
}

// ManifestWorkspace holds workspace identity metadata inside the manifest.
type ManifestWorkspace struct {
	ID               string `json:"id"`
	Slug             string `json:"slug"`
	ExportedAt       string `json:"exported_at"`
	SourceAppVersion string `json:"source_app_version"`
}

// ManifestData holds the exported entity collections.
type ManifestData struct {
	Issues []ManifestIssue `json:"issues"`
}

// ManifestIssue is a portable issue representation shared by export and import.
type ManifestIssue struct {
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
}

// --- Import types ---

// WorkspaceImportPayload is the unified import request body for all source
// types (canonical-json, issue-csv, external-adapter).
type WorkspaceImportPayload struct {
	SchemaVersion string `json:"schema_version"`
	// SourceType identifies the adapter to use: "canonical-json", "issue-csv",
	// or "external-adapter".
	SourceType string `json:"source_type"`
	// WorkspaceID is the ID embedded in canonical-json exports. It must match
	// the target workspace to prevent cross-workspace clobbering.
	WorkspaceID string          `json:"workspace_id,omitempty"`
	Issues      []ManifestIssue `json:"issues,omitempty"`
}

// WorkspaceImportResult summarises the outcome of a dry-run or apply call.
type WorkspaceImportResult struct {
	Summary  string                 `json:"summary"`
	Warnings []string               `json:"warnings,omitempty"`
	Errors   []WorkspaceImportError `json:"errors,omitempty"`
	Created  int                    `json:"created"`
	Skipped  int                    `json:"skipped"`
	Failed   int                    `json:"failed"`
}

type WorkspaceImportError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func validateImportPayload(targetWorkspaceID string, payload WorkspaceImportPayload) []WorkspaceImportError {
	errors := make([]WorkspaceImportError, 0)

	if payload.SchemaVersion != ManifestSchemaVersion {
		errors = append(errors, WorkspaceImportError{
			Code:    "unsupported_schema_version",
			Message: fmt.Sprintf("schema_version %q is not supported", payload.SchemaVersion),
		})
	}

	switch payload.SourceType {
	case "canonical-json", "issue-csv":
	default:
		errors = append(errors, WorkspaceImportError{
			Code:    "unsupported_source_type",
			Message: fmt.Sprintf("source_type %q is not supported", payload.SourceType),
		})
	}

	if payload.SourceType == "canonical-json" && payload.WorkspaceID != targetWorkspaceID {
		errors = append(errors, WorkspaceImportError{
			Code:    "workspace_mismatch",
			Message: fmt.Sprintf("manifest workspace_id %q does not match target workspace %q", payload.WorkspaceID, targetWorkspaceID),
		})
	}

	if payload.SourceType == "issue-csv" && len(payload.Issues) > importIssueLimit {
		errors = append(errors, WorkspaceImportError{
			Code:    "too_many_issues",
			Message: fmt.Sprintf("too many issues: limit is %d", importIssueLimit),
		})
	}

	for idx, item := range payload.Issues {
		if strings.TrimSpace(item.Title) == "" {
			errors = append(errors, WorkspaceImportError{
				Code:    "title_required",
				Message: fmt.Sprintf("row %d title is required", idx+1),
			})
		}
		if item.Status != "" {
			if _, ok := validIssueStatuses[item.Status]; !ok {
				errors = append(errors, WorkspaceImportError{
					Code:    "invalid_status",
					Message: fmt.Sprintf("row %d status %q is invalid", idx+1, item.Status),
				})
			}
		}
		if item.Priority != "" {
			if _, ok := validIssuePriorities[item.Priority]; !ok {
				errors = append(errors, WorkspaceImportError{
					Code:    "invalid_priority",
					Message: fmt.Sprintf("row %d priority %q is invalid", idx+1, item.Priority),
				})
			}
		}
	}

	return errors
}

// --- Service methods ---

// BuildExportManifest fetches all issues for workspaceID and wraps them in a
// canonical WorkspaceExportManifest.
func (s *DataSyncService) BuildExportManifest(ctx context.Context, workspaceID string) (*WorkspaceExportManifest, error) {
	wsUUID := util.ParseUUID(workspaceID)
	ws, err := s.Queries.GetWorkspace(ctx, wsUUID)
	if err != nil {
		return nil, fmt.Errorf("get workspace: %w", err)
	}

	issues, err := s.Queries.ListIssues(ctx, db.ListIssuesParams{
		WorkspaceID: wsUUID,
		Limit:       exportQueryLimit,
		Offset:      0,
	})
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	manifestIssues := make([]ManifestIssue, 0, len(issues))
	for _, iss := range issues {
		mi := ManifestIssue{
			Title:    iss.Title,
			Status:   iss.Status,
			Priority: iss.Priority,
		}
		if iss.Description.Valid {
			desc := iss.Description.String
			mi.Description = &desc
		}
		manifestIssues = append(manifestIssues, mi)
	}

	return &WorkspaceExportManifest{
		SchemaVersion: ManifestSchemaVersion,
		Workspace: ManifestWorkspace{
			ID:               workspaceID,
			Slug:             ws.Slug,
			ExportedAt:       time.Now().UTC().Format(time.RFC3339),
			SourceAppVersion: sourceAppVersion,
		},
		Data: ManifestData{Issues: manifestIssues},
	}, nil
}

// DryRunImport validates the import payload against targetWorkspaceID without
// writing anything to the database.
//
// For canonical-json payloads the embedded workspace_id must match the target
// workspace — any mismatch is a hard error to prevent accidental cross-
// workspace clobbering.
func (s *DataSyncService) DryRunImport(ctx context.Context, targetWorkspaceID string, payload WorkspaceImportPayload) (*WorkspaceImportResult, error) {
	_ = ctx
	validationErrors := validateImportPayload(targetWorkspaceID, payload)
	if len(validationErrors) > 0 {
		return &WorkspaceImportResult{
			Summary: "dry-run: blocked by validation errors",
			Errors:  validationErrors,
			Failed:  len(validationErrors),
		}, nil
	}

	result := &WorkspaceImportResult{
		Summary: fmt.Sprintf("dry-run: %d issues would be created", len(payload.Issues)),
		Created: len(payload.Issues),
	}
	return result, nil
}

// ApplyImport writes the import payload into the target workspace. For source
// type "issue-csv" it routes through the same DB path as BulkCreateIssues in
// the handler, ensuring consistent validation and counter increments.
func (s *DataSyncService) ApplyImport(ctx context.Context, targetWorkspaceID, creatorType, creatorID string, payload WorkspaceImportPayload) (*WorkspaceImportResult, error) {
	validationErrors := validateImportPayload(targetWorkspaceID, payload)
	if len(validationErrors) > 0 {
		return &WorkspaceImportResult{
			Summary: "apply: blocked by validation errors",
			Errors:  validationErrors,
			Failed:  len(validationErrors),
		}, nil
	}

	wsUUID := util.ParseUUID(targetWorkspaceID)
	creatorUUID := util.ParseUUID(creatorID)

	var created, failed int
	var errs []WorkspaceImportError

	switch payload.SourceType {
	case "issue-csv", "canonical-json":
		if err := s.Queries.EnsureDefaultIssueTypes(ctx, wsUUID); err != nil {
			return &WorkspaceImportResult{
				Summary: "apply: blocked by validation errors",
				Errors: []WorkspaceImportError{{
					Code:    "issue_type_seed_failed",
					Message: fmt.Sprintf("seed issue types: %v", err),
				}},
				Created: created,
				Failed:  len(payload.Issues),
			}, nil
		}
		defaultIssueType, err := s.Queries.GetIssueTypeByKey(ctx, db.GetIssueTypeByKeyParams{
			WorkspaceID: wsUUID,
			Key:         "task",
		})
		if err != nil {
			return &WorkspaceImportResult{
				Summary: "apply: blocked by validation errors",
				Errors: []WorkspaceImportError{{
					Code:    "issue_type_default_missing",
					Message: fmt.Sprintf("load default issue type: %v", err),
				}},
				Created: created,
				Failed:  len(payload.Issues),
			}, nil
		}
		for _, item := range payload.Issues {
			number, err := s.Queries.IncrementIssueCounter(ctx, wsUUID)
			if err != nil {
				failed++
				errs = append(errs, WorkspaceImportError{
					Code:    "counter_increment_failed",
					Message: fmt.Sprintf("increment counter: %v", err),
				})
				continue
			}

			status := item.Status
			if status == "" {
				status = "backlog"
			}
			priority := item.Priority
			if priority == "" {
				priority = "none"
			}

			params := db.CreateIssueParams{
				WorkspaceID: wsUUID,
				Title:       strings.TrimSpace(item.Title),
				Status:      status,
				Priority:    priority,
				CreatorType: creatorType,
				CreatorID:   creatorUUID,
				IssueTypeID: defaultIssueType.ID,
				Number:      number,
			}
			if item.Description != nil {
				params.Description = pgtype.Text{String: *item.Description, Valid: true}
			}

			if _, err := s.Queries.CreateIssue(ctx, params); err != nil {
				failed++
				errs = append(errs, WorkspaceImportError{
					Code:    "create_issue_failed",
					Message: fmt.Sprintf("create issue %q: %v", item.Title, err),
				})
				continue
			}
			created++
		}

	default:
		return &WorkspaceImportResult{
			Summary: "apply: blocked by validation errors",
			Errors: []WorkspaceImportError{
				{
					Code:    "unsupported_source_type",
					Message: fmt.Sprintf("source_type %q is not supported", payload.SourceType),
				},
			},
			Failed: 1,
		}, nil
	}

	result := &WorkspaceImportResult{
		Summary: fmt.Sprintf("apply: %d created, %d failed", created, failed),
		Errors:  errs,
		Created: created,
		Failed:  failed,
	}
	return result, nil
}
