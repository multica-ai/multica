package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// stubDataSyncQuerier is a minimal in-memory stub for dataSyncQuerier.
type stubDataSyncQuerier struct {
	workspace   db.Workspace
	issues      []db.Issue
	nextCounter int32
	created     []db.CreateIssueParams
	listCalls   []db.ListIssuesParams
}

func (s *stubDataSyncQuerier) GetWorkspace(_ context.Context, id pgtype.UUID) (db.Workspace, error) {
	s.workspace.ID = id
	return s.workspace, nil
}

func (s *stubDataSyncQuerier) ListIssues(_ context.Context, arg db.ListIssuesParams) ([]db.Issue, error) {
	s.listCalls = append(s.listCalls, arg)
	if arg.Limit <= 0 {
		return []db.Issue{}, nil
	}
	start := int(arg.Offset)
	if start >= len(s.issues) {
		return []db.Issue{}, nil
	}
	end := start + int(arg.Limit)
	if end > len(s.issues) {
		end = len(s.issues)
	}
	return s.issues[start:end], nil
}

func (s *stubDataSyncQuerier) IncrementIssueCounter(_ context.Context, _ pgtype.UUID) (int32, error) {
	s.nextCounter++
	return s.nextCounter, nil
}

func (s *stubDataSyncQuerier) CreateIssue(_ context.Context, arg db.CreateIssueParams) (db.Issue, error) {
	s.created = append(s.created, arg)
	return db.Issue{
		WorkspaceID: arg.WorkspaceID,
		Title:       arg.Title,
		Status:      arg.Status,
		Priority:    arg.Priority,
		Number:      arg.Number,
	}, nil
}

// TestBuildExportManifest_IncludesIssuesAndSchemaVersion verifies that the
// export manifest contains the workspace issues and the expected schema version.
func TestBuildExportManifest_IncludesIssuesAndSchemaVersion(t *testing.T) {
	wsID := "11111111-1111-1111-1111-111111111111"
	stub := &stubDataSyncQuerier{
		workspace: db.Workspace{Slug: "my-workspace"},
		issues: []db.Issue{
			{Title: "Issue One", Status: "backlog", Priority: "none"},
			{Title: "Issue Two", Status: "in_progress", Priority: "high"},
		},
	}

	svc := service.NewDataSyncService(stub)
	manifest, err := svc.BuildExportManifest(context.Background(), wsID)
	if err != nil {
		t.Fatalf("BuildExportManifest returned unexpected error: %v", err)
	}

	if manifest.SchemaVersion != service.ManifestSchemaVersion {
		t.Errorf("schema_version = %q, want %q", manifest.SchemaVersion, service.ManifestSchemaVersion)
	}
	if manifest.Workspace.ID != wsID {
		t.Errorf("workspace.id = %q, want %q", manifest.Workspace.ID, wsID)
	}
	if manifest.Workspace.Slug != "my-workspace" {
		t.Errorf("workspace.slug = %q, want %q", manifest.Workspace.Slug, "my-workspace")
	}
	if got := len(manifest.Data.Issues); got != 2 {
		t.Errorf("data.issues count = %d, want 2", got)
	}
	if manifest.Data.Issues[0].Title != "Issue One" {
		t.Errorf("data.issues[0].title = %q, want %q", manifest.Data.Issues[0].Title, "Issue One")
	}
	payload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if !strings.Contains(string(payload), "\"source_app_version\"") {
		t.Fatal("manifest JSON should include workspace.source_app_version")
	}
}

func TestBuildExportManifest_ExportsMoreThanThousandIssuesWithoutOffsetPaging(t *testing.T) {
	wsID := "11111111-1111-1111-1111-111111111111"
	total := 1001
	issues := make([]db.Issue, 0, total)
	for i := 0; i < total; i++ {
		issues = append(issues, db.Issue{
			Title:    "Issue",
			Status:   "backlog",
			Priority: "none",
		})
	}
	stub := &stubDataSyncQuerier{
		workspace: db.Workspace{Slug: "my-workspace"},
		issues:    issues,
	}
	svc := service.NewDataSyncService(stub)

	manifest, err := svc.BuildExportManifest(context.Background(), wsID)
	if err != nil {
		t.Fatalf("BuildExportManifest returned unexpected error: %v", err)
	}
	if len(manifest.Data.Issues) != total {
		t.Fatalf("expected %d exported issues, got %d", total, len(manifest.Data.Issues))
	}
	if len(stub.listCalls) != 1 {
		t.Fatalf("expected export to use a single query snapshot, got %d list calls", len(stub.listCalls))
	}
}

// TestDryRunImport_RejectsWorkspaceMismatch verifies that a canonical-json
// payload whose embedded workspace ID differs from the target workspace is
// rejected without touching the database.
func TestDryRunImport_RejectsWorkspaceMismatch(t *testing.T) {
	targetWS := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	otherWS := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	svc := service.NewDataSyncService(&stubDataSyncQuerier{})
	payload := service.WorkspaceImportPayload{
		SchemaVersion: service.ManifestSchemaVersion,
		SourceType:    "canonical-json",
		WorkspaceID:   otherWS, // intentional mismatch
	}

	result, err := svc.DryRunImport(context.Background(), targetWS, payload)
	if err != nil {
		t.Fatalf("expected no hard error for workspace mismatch dry-run, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected dry-run result, got nil")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected exactly one dry-run error, got %d", len(result.Errors))
	}
	if result.Errors[0].Code != "workspace_mismatch" {
		t.Fatalf("expected workspace_mismatch code, got %q", result.Errors[0].Code)
	}
}

func TestDryRunImport_RejectsUnsupportedSchemaVersion(t *testing.T) {
	svc := service.NewDataSyncService(&stubDataSyncQuerier{})

	result, err := svc.DryRunImport(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", service.WorkspaceImportPayload{
		SchemaVersion: "wrong-version",
		SourceType:    "issue-csv",
		Issues:        []service.ManifestIssue{{Title: "Issue One"}},
	})
	if err != nil {
		t.Fatalf("expected no hard error, got %v", err)
	}
	if len(result.Errors) != 1 || result.Errors[0].Code != "unsupported_schema_version" {
		t.Fatalf("expected unsupported_schema_version, got %+v", result.Errors)
	}
}

func TestDryRunImport_RejectsUnsupportedSourceType(t *testing.T) {
	svc := service.NewDataSyncService(&stubDataSyncQuerier{})

	result, err := svc.DryRunImport(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", service.WorkspaceImportPayload{
		SchemaVersion: service.ManifestSchemaVersion,
		SourceType:    "bogus",
		Issues:        []service.ManifestIssue{{Title: "Issue One"}},
	})
	if err != nil {
		t.Fatalf("expected no hard error, got %v", err)
	}
	if len(result.Errors) != 1 || result.Errors[0].Code != "unsupported_source_type" {
		t.Fatalf("expected unsupported_source_type, got %+v", result.Errors)
	}
}

func TestDryRunImport_RejectsTooManyIssues(t *testing.T) {
	issues := make([]service.ManifestIssue, 101)
	for i := range issues {
		issues[i] = service.ManifestIssue{Title: "Issue"}
	}
	svc := service.NewDataSyncService(&stubDataSyncQuerier{})

	result, err := svc.DryRunImport(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", service.WorkspaceImportPayload{
		SchemaVersion: service.ManifestSchemaVersion,
		SourceType:    "issue-csv",
		Issues:        issues,
	})
	if err != nil {
		t.Fatalf("expected no hard error, got %v", err)
	}
	if len(result.Errors) != 1 || result.Errors[0].Code != "too_many_issues" {
		t.Fatalf("expected too_many_issues, got %+v", result.Errors)
	}
}

func TestDryRunImport_AllowsCanonicalImportToRoundTripLargeExport(t *testing.T) {
	issues := make([]service.ManifestIssue, 101)
	for i := range issues {
		issues[i] = service.ManifestIssue{Title: "Issue", Status: "backlog", Priority: "none"}
	}
	svc := service.NewDataSyncService(&stubDataSyncQuerier{})

	result, err := svc.DryRunImport(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", service.WorkspaceImportPayload{
		SchemaVersion: service.ManifestSchemaVersion,
		SourceType:    "canonical-json",
		WorkspaceID:   "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		Issues:        issues,
	})
	if err != nil {
		t.Fatalf("expected no hard error, got %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("expected canonical import to accept exported issue count, got %+v", result.Errors)
	}
}

func TestDryRunImport_RejectsInvalidStatusAndPriority(t *testing.T) {
	svc := service.NewDataSyncService(&stubDataSyncQuerier{})

	result, err := svc.DryRunImport(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", service.WorkspaceImportPayload{
		SchemaVersion: service.ManifestSchemaVersion,
		SourceType:    "issue-csv",
		Issues: []service.ManifestIssue{
			{Title: "Issue", Status: "invalid", Priority: "wrong"},
		},
	})
	if err != nil {
		t.Fatalf("expected no hard error, got %v", err)
	}
	if len(result.Errors) != 2 {
		t.Fatalf("expected invalid status and priority errors, got %+v", result.Errors)
	}
	if result.Errors[0].Code != "invalid_status" && result.Errors[1].Code != "invalid_status" {
		t.Fatalf("expected invalid_status error, got %+v", result.Errors)
	}
	if result.Errors[0].Code != "invalid_priority" && result.Errors[1].Code != "invalid_priority" {
		t.Fatalf("expected invalid_priority error, got %+v", result.Errors)
	}
}

// TestApplyImport_IssueCsvCreatesIssues verifies that an issue-csv import
// calls the shared issue creation path for every item in the payload.
func TestApplyImport_IssueCsvCreatesIssues(t *testing.T) {
	wsID := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	stub := &stubDataSyncQuerier{
		workspace: db.Workspace{
			ID:   util.ParseUUID(wsID),
			Slug: "test-ws",
		},
	}

	svc := service.NewDataSyncService(stub)
	payload := service.WorkspaceImportPayload{
		SchemaVersion: service.ManifestSchemaVersion,
		SourceType:    "issue-csv",
		Issues: []service.ManifestIssue{
			{Title: "Alpha", Status: "backlog", Priority: "none"},
			{Title: "Beta", Status: "todo", Priority: "high"},
		},
	}

	result, err := svc.ApplyImport(context.Background(), wsID, "member", "11111111-1111-1111-1111-111111111111", payload)
	if err != nil {
		t.Fatalf("ApplyImport returned unexpected error: %v", err)
	}
	if result.Created != 2 {
		t.Errorf("result.Created = %d, want 2", result.Created)
	}
	if len(stub.created) != 2 {
		t.Errorf("CreateIssue called %d times, want 2", len(stub.created))
	}
	if stub.created[0].Title != "Alpha" {
		t.Errorf("first created issue title = %q, want %q", stub.created[0].Title, "Alpha")
	}
	if stub.created[1].Title != "Beta" {
		t.Errorf("second created issue title = %q, want %q", stub.created[1].Title, "Beta")
	}
}

func TestApplyImport_CanonicalJSONRejectsWorkspaceMismatch(t *testing.T) {
	targetWS := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	otherWS := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	stub := &stubDataSyncQuerier{}
	svc := service.NewDataSyncService(stub)

	result, err := svc.ApplyImport(context.Background(), targetWS, "member", "11111111-1111-1111-1111-111111111111", service.WorkspaceImportPayload{
		SchemaVersion: service.ManifestSchemaVersion,
		SourceType:    "canonical-json",
		WorkspaceID:   otherWS,
		Issues: []service.ManifestIssue{
			{Title: "Should not import"},
		},
	})
	if err != nil {
		t.Fatalf("expected no hard error, got %v", err)
	}
	if len(result.Errors) != 1 || result.Errors[0].Code != "workspace_mismatch" {
		t.Fatalf("expected workspace_mismatch apply error, got %+v", result.Errors)
	}
	if len(stub.created) != 0 {
		t.Fatalf("expected no issue creation on workspace mismatch, got %d", len(stub.created))
	}
}

func TestApplyImport_IssueCSVValidationIsAllOrNothing(t *testing.T) {
	wsID := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	stub := &stubDataSyncQuerier{}
	svc := service.NewDataSyncService(stub)

	result, err := svc.ApplyImport(context.Background(), wsID, "member", "11111111-1111-1111-1111-111111111111", service.WorkspaceImportPayload{
		SchemaVersion: service.ManifestSchemaVersion,
		SourceType:    "issue-csv",
		Issues: []service.ManifestIssue{
			{Title: ""},
			{Title: "Valid title"},
		},
	})
	if err != nil {
		t.Fatalf("expected no hard error, got %v", err)
	}
	if len(result.Errors) != 1 || result.Errors[0].Code != "title_required" {
		t.Fatalf("expected title_required validation error, got %+v", result.Errors)
	}
	if result.Created != 0 {
		t.Fatalf("expected 0 created on validation failure, got %d", result.Created)
	}
	if len(stub.created) != 0 {
		t.Fatalf("expected no writes when validation fails, got %d", len(stub.created))
	}
}

func TestApplyImport_RejectsUnsupportedSourceTypeAsValidationError(t *testing.T) {
	svc := service.NewDataSyncService(&stubDataSyncQuerier{})

	result, err := svc.ApplyImport(context.Background(), "cccccccc-cccc-cccc-cccc-cccccccccccc", "member", "11111111-1111-1111-1111-111111111111", service.WorkspaceImportPayload{
		SchemaVersion: service.ManifestSchemaVersion,
		SourceType:    "bogus",
		Issues:        []service.ManifestIssue{{Title: "Issue"}},
	})
	if err != nil {
		t.Fatalf("expected no hard error, got %v", err)
	}
	if len(result.Errors) != 1 || result.Errors[0].Code != "unsupported_source_type" {
		t.Fatalf("expected unsupported_source_type, got %+v", result.Errors)
	}
}
