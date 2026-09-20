package lark

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	documentTestWorkspaceID  = mustDocumentUUID("10000000-0000-4000-8000-000000000001")
	documentTestAgentID      = mustDocumentUUID("10000000-0000-4000-8000-000000000002")
	documentTestRuntimeID    = mustDocumentUUID("10000000-0000-4000-8000-000000000003")
	documentTestTaskID       = mustDocumentUUID("10000000-0000-4000-8000-000000000004")
	documentTestInstallID    = mustDocumentUUID("10000000-0000-4000-8000-000000000005")
	documentTestInstalledAt  = time.Unix(1_700_000_000, 123)
	documentTestInstallation = Installation{
		ID:          documentTestInstallID,
		WorkspaceID: documentTestWorkspaceID,
		AgentID:     documentTestAgentID,
		AppID:       "cli_pikachu",
		Status:      "active",
		Region:      string(RegionFeishu),
		UpdatedAt:   pgtype.Timestamptz{Time: documentTestInstalledAt, Valid: true},
	}
	documentTestScope = DocumentTaskScope{
		TaskID:               documentTestTaskID,
		WorkspaceID:          documentTestWorkspaceID,
		AgentID:              documentTestAgentID,
		RuntimeID:            documentTestRuntimeID,
		DaemonID:             "daemon-a",
		InstallationID:       documentTestInstallID,
		InstallationRevision: documentTestInstalledAt.UnixNano(),
	}
)

func TestDocumentServiceUsesScopedInstallationCredentials(t *testing.T) {
	client := &fakeDocumentClient{fetches: []DocumentSnapshot{{RevisionID: 7, Content: "hello"}}}
	service := NewDocumentService(
		&fakeDocumentInstallStore{installation: documentTestInstallation},
		fakeDocumentCredentials{secret: "pikachu-secret"},
		client,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	result, toolErr := service.Execute(context.Background(), documentTestScope, DocumentOperationFetch, DocumentToolInput{DocumentURL: testDocumentURL})
	if toolErr != nil {
		t.Fatal(toolErr)
	}
	if result.Content != "hello" || client.lastCredentials.AppID != "cli_pikachu" || client.lastCredentials.AppSecret != "pikachu-secret" {
		t.Fatalf("result=%+v credentials=%+v", result, client.lastCredentials)
	}
}

func TestDocumentServiceRejectsStaleInstallationCapability(t *testing.T) {
	for _, mutate := range []func(*DocumentTaskScope){
		func(scope *DocumentTaskScope) {
			scope.InstallationID = mustDocumentUUID("20000000-0000-4000-8000-000000000005")
		},
		func(scope *DocumentTaskScope) { scope.InstallationRevision++ },
		func(scope *DocumentTaskScope) {
			scope.AgentID = mustDocumentUUID("20000000-0000-4000-8000-000000000002")
		},
	} {
		scope := documentTestScope
		mutate(&scope)
		client := &fakeDocumentClient{}
		_, toolErr := newTestDocumentService(client).Execute(context.Background(), scope, DocumentOperationFetch, DocumentToolInput{DocumentURL: testDocumentURL})
		if toolErr == nil || toolErr.Code != DocumentErrorCapabilityDenied || client.fetchCalls != 0 {
			t.Fatalf("error=%+v fetches=%d", toolErr, client.fetchCalls)
		}
	}
}

func TestDocumentServiceReplaceTextRequiresOneMatch(t *testing.T) {
	for _, content := range []string{"no match", "old and old"} {
		client := &fakeDocumentClient{fetches: []DocumentSnapshot{{RevisionID: 7, Content: content}}}
		_, toolErr := newTestDocumentService(client).Execute(context.Background(), documentTestScope, DocumentOperationReplaceText, DocumentToolInput{
			DocumentURL: testDocumentURL,
			Pattern:     "old",
			Content:     "new",
		})
		if toolErr == nil || toolErr.Code != DocumentErrorConflict || len(client.updates) != 0 {
			t.Fatalf("content=%q error=%+v updates=%d", content, toolErr, len(client.updates))
		}
	}
}

func TestDocumentServiceWriteTimeoutReadsBeforeAnyRetry(t *testing.T) {
	client := &fakeDocumentClient{
		fetches:   []DocumentSnapshot{{RevisionID: 7, Content: "before"}, {RevisionID: 8, Content: "after"}},
		updateErr: context.DeadlineExceeded,
	}
	_, toolErr := newTestDocumentService(client).Execute(context.Background(), documentTestScope, DocumentOperationAppend, DocumentToolInput{
		DocumentURL: testDocumentURL,
		Content:     "marker",
	})
	if toolErr == nil || toolErr.Code != DocumentErrorTimeoutUncertain || len(client.updates) != 1 || client.fetchCalls != 2 {
		t.Fatalf("error=%+v updates=%d fetches=%d", toolErr, len(client.updates), client.fetchCalls)
	}
}

func TestDocumentServiceRefusesUnversionedWrite(t *testing.T) {
	client := &fakeDocumentClient{fetches: []DocumentSnapshot{{RevisionID: 0, Content: "before"}}}
	_, toolErr := newTestDocumentService(client).Execute(context.Background(), documentTestScope, DocumentOperationAppend, DocumentToolInput{
		DocumentURL: testDocumentURL,
		Content:     "marker",
	})
	if toolErr == nil || toolErr.Code != DocumentErrorConflict || len(client.updates) != 0 {
		t.Fatalf("error=%+v updates=%d", toolErr, len(client.updates))
	}
}

func TestDocumentServiceStructuralWriteFetchesBeforeAndAfter(t *testing.T) {
	client := &fakeDocumentClient{fetches: []DocumentSnapshot{
		{RevisionID: 7, BlockIDs: []string{"block-a"}},
		{RevisionID: 8, BlockIDs: []string{"block-a"}},
	}}
	_, toolErr := newTestDocumentService(client).Execute(context.Background(), documentTestScope, DocumentOperationReplaceBlock, DocumentToolInput{
		DocumentURL: testDocumentURL,
		BlockID:     "block-a",
		Content:     "new",
	})
	if toolErr != nil || !reflect.DeepEqual(client.calls, []string{"fetch", "update:7", "fetch"}) {
		t.Fatalf("error=%+v calls=%v", toolErr, client.calls)
	}
}

func TestDocumentServiceAuditContainsNoSensitiveMaterial(t *testing.T) {
	var logs bytes.Buffer
	installation := documentTestInstallation
	installation.AppID = "SENSITIVE-APP-ID"
	installation.AppSecretEncrypted = []byte("SENSITIVE-CIPHERTEXT")
	client := &fakeDocumentClient{fetches: []DocumentSnapshot{{RevisionID: 7, Content: "SENSITIVE-DOCUMENT-CONTENT"}}}
	service := NewDocumentService(
		&fakeDocumentInstallStore{installation: installation},
		fakeDocumentCredentials{secret: "SENSITIVE-APP-SECRET"},
		client,
		slog.New(slog.NewTextHandler(&logs, nil)),
	)
	_, toolErr := service.Execute(context.Background(), documentTestScope, DocumentOperationFetch, DocumentToolInput{DocumentURL: testDocumentURL})
	if toolErr != nil {
		t.Fatal(toolErr)
	}
	client.fetchErrs = []error{nil, errors.New("SENSITIVE-UPSTREAM-BODY")}
	_, toolErr = service.Execute(context.Background(), documentTestScope, DocumentOperationFetch, DocumentToolInput{DocumentURL: testDocumentURL})
	if toolErr == nil || toolErr.Code != DocumentErrorUpstreamUnavailable {
		t.Fatalf("error=%+v", toolErr)
	}
	for _, secret := range []string{testDocumentURL, "AbcdefghijkLMNop", "SENSITIVE-DOCUMENT-CONTENT", "SENSITIVE-APP-ID", "SENSITIVE-CIPHERTEXT", "SENSITIVE-APP-SECRET", "SENSITIVE-UPSTREAM-BODY"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("logs leaked %q: %s", secret, logs.String())
		}
	}
	for _, field := range []string{"operation=", "result_class=", "duration_ms=", "document_fingerprint="} {
		if !strings.Contains(logs.String(), field) {
			t.Fatalf("missing %s in %s", field, logs.String())
		}
	}
}

func newTestDocumentService(client *fakeDocumentClient) *DocumentService {
	return NewDocumentService(
		&fakeDocumentInstallStore{installation: documentTestInstallation},
		fakeDocumentCredentials{secret: "pikachu-secret"},
		client,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

type fakeDocumentInstallStore struct {
	installation Installation
	err          error
}

func (s *fakeDocumentInstallStore) GetActiveLarkInstallationForAgent(_ context.Context, _, _ pgtype.UUID) (Installation, error) {
	return s.installation, s.err
}

type fakeDocumentCredentials struct {
	secret string
	err    error
}

func (c fakeDocumentCredentials) DecryptAppSecret(Installation) (string, error) {
	return c.secret, c.err
}

type fakeDocumentClient struct {
	lastCredentials InstallationCredentials
	fetches         []DocumentSnapshot
	fetchErrs       []error
	fetchCalls      int
	updates         []DocumentUpdateParams
	updateResult    DocumentUpdateResult
	updateErr       error
	calls           []string
}

func (c *fakeDocumentClient) FetchDocument(_ context.Context, creds InstallationCredentials, _ DocumentFetchParams) (DocumentSnapshot, error) {
	c.lastCredentials = creds
	c.fetchCalls++
	c.calls = append(c.calls, "fetch")
	index := c.fetchCalls - 1
	if index < len(c.fetchErrs) && c.fetchErrs[index] != nil {
		return DocumentSnapshot{}, c.fetchErrs[index]
	}
	if index < len(c.fetches) {
		return c.fetches[index], nil
	}
	return DocumentSnapshot{}, nil
}

func (c *fakeDocumentClient) UpdateDocument(_ context.Context, creds InstallationCredentials, p DocumentUpdateParams) (DocumentUpdateResult, error) {
	c.lastCredentials = creds
	c.updates = append(c.updates, p)
	c.calls = append(c.calls, "update:"+formatDocumentRevision(p.RevisionID))
	return c.updateResult, c.updateErr
}

func mustDocumentUUID(value string) pgtype.UUID {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		panic(err)
	}
	return id
}

func TestDocumentServiceNormalizesNotConnected(t *testing.T) {
	service := NewDocumentService(
		&fakeDocumentInstallStore{err: pgx.ErrNoRows},
		fakeDocumentCredentials{},
		&fakeDocumentClient{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	_, toolErr := service.Execute(context.Background(), documentTestScope, DocumentOperationFetch, DocumentToolInput{DocumentURL: testDocumentURL})
	if toolErr == nil || toolErr.Code != DocumentErrorNotConnected {
		t.Fatalf("error=%+v", toolErr)
	}
}
