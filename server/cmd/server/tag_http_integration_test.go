package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/tagaccess"
)

func TestRouterFencesMirroredWorkspaceAndInvitationWriters(t *testing.T) {
	const (
		vibesUserID      = "router-vibes-user-289"
		vibesWorkspaceID = "router-vibes-workspace-289"
		projectionKeyID  = "router-projection-key"
		assertionKeyID   = "router-assertion-key"
	)
	projectionKey := []byte("router-projection-authentication-key-32-bytes")
	assertionKey := []byte("router-gateway-assertion-key-at-least-32-bytes")
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM vibes_user_mirror WHERE vibes_user_id = $1`, vibesUserID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM vibes_workspace_mirror WHERE vibes_workspace_id = $1`, vibesWorkspaceID)
	})
	if _, err := testPool.Exec(t.Context(), `
		INSERT INTO vibes_user_mirror (vibes_user_id, multica_user_id, profile_email)
		VALUES ($1, $2::uuid, '')
	`, vibesUserID, testUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(t.Context(), `
		INSERT INTO vibes_workspace_mirror (vibes_workspace_id, multica_workspace_id)
		VALUES ($1, $2::uuid)
	`, vibesWorkspaceID, testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	access, err := tagaccess.NewAuthenticatedAccess(
		tagaccess.NewMemoryStore(), tagaccess.SystemClock{}, map[string][]byte{projectionKeyID: projectionKey}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	event := tagaccess.ProjectionEvent{
		EventID: "router-projection-event-1", VIBESUserID: vibesUserID, WorkspaceID: vibesWorkspaceID,
		Role: tagaccess.RoleOwner, Status: tagaccess.StatusActive, AccountEpoch: 1,
		MembershipGeneration: 1, AuthorityVersion: 1,
	}
	envelope := tagaccess.AuthorityEnvelope{
		SchemaVersion: tagaccess.AuthorityEnvelopeSchemaVersion,
		DeliveryID:    "router-delivery-1",
		CorrelationID: "router-correlation-1",
		Delivery: tagaccess.ProjectionDelivery{
			Kind: tagaccess.DeliveryIncremental, BaselineAuthorityVersion: 0,
			Projections: []tagaccess.ProjectionEvent{event},
		},
		Authentication: tagaccess.AuthorityEnvelopeAuthentication{KeyID: projectionKeyID},
	}
	payload, err := tagaccess.CanonicalAuthorityEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, projectionKey)
	_, _ = mac.Write(payload)
	envelope.Authentication.MAC = mac.Sum(nil)
	if _, err := access.Ingress.Deliver(t.Context(), envelope); err != nil {
		t.Fatal(err)
	}
	if err := access.Gate.GrantSession(t.Context(), tagaccess.SessionGrant{
		TagSessionID:   tagaccess.BrowserTagSessionID(vibesUserID, "router-vibes-session-1"),
		VIBESSessionID: "router-vibes-session-1", VIBESUserID: vibesUserID, WorkspaceID: vibesWorkspaceID,
		AccountEpoch: 1, SessionWorkspaceGeneration: 1, MembershipGeneration: 1, AuthorityVersion: 1,
		SessionExpiresAt: time.Now().Add(time.Hour), GrantExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	verifier, err := tagaccess.NewHTTPAssertionVerifier(
		map[string][]byte{assertionKeyID: assertionKey}, tagaccess.SystemClock{},
	)
	if err != nil {
		t.Fatal(err)
	}
	setAssertion := func(t *testing.T, request *http.Request, body []byte, requestID string) {
		t.Helper()
		path, query, err := tagaccess.CanonicalHTTPRequestTarget(request)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(body)
		bodySHA256 := ""
		if request.Method != http.MethodGet && request.Method != http.MethodHead && request.Method != http.MethodOptions && request.Method != http.MethodTrace {
			bodySHA256 = hex.EncodeToString(digest[:])
		}
		assertion := tagaccess.HTTPAssertion{
			SchemaVersion: tagaccess.HTTPAssertionSchemaVersion, KeyID: assertionKeyID,
			Issuer: tagaccess.HTTPAssertionIssuer, Audience: tagaccess.HTTPAssertionAudience,
			Method: request.Method, Path: path, Query: query,
			BodySHA256: bodySHA256, UserID: vibesUserID, WorkspaceID: vibesWorkspaceID,
			SessionID: "router-vibes-session-1", AccountEpoch: 1, SessionWorkspaceGeneration: 1,
			AuthorityVersion: 1, MembershipGeneration: 1,
			IssuedAt: time.Now().Add(-time.Second).UnixMilli(), ExpiresAt: time.Now().Add(5 * time.Second).UnixMilli(),
			RequestID: requestID, Nonce: "nonce-" + requestID,
		}
		assertionPayload, err := json.Marshal(assertion)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := tagaccess.CanonicalHTTPAssertion(assertion)
		if err != nil {
			t.Fatal(err)
		}
		assertionMAC := hmac.New(sha256.New, assertionKey)
		_, _ = assertionMAC.Write(canonical)
		request.Header.Set(tagaccess.HTTPAssertionHeader, base64.RawURLEncoding.EncodeToString(assertionPayload))
		request.Header.Set(tagaccess.HTTPAssertionSignatureHeader, base64.RawURLEncoding.EncodeToString(assertionMAC.Sum(nil)))
		request.Header.Set(tagaccess.HTTPAssertionKeyIDHeader, assertionKeyID)
	}
	replay, err := tagaccess.NewMemoryHTTPAssertionReplayStore(tagaccess.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	uploadDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", uploadDir)
	t.Setenv("ATTACHMENT_DOWNLOAD_MODE", "proxy")
	hub := realtime.NewHub()
	go hub.Run()
	router, routerHandler := NewRouterWithOptions(testPool, hub, events.New(), analytics.NoopClient{}, nil, RouterOptions{
		TagAuthorityAccess: access,
		TagHTTPVerifier:    verifier,
		TagHTTPReplay:      replay,
	})
	routerHandler.Storage = storage.NewLocalStorageFromEnv()
	server := httptest.NewServer(router)
	defer server.Close()

	for name, testCase := range map[string]struct {
		method string
		path   string
		body   []byte
	}{
		"workspace":  {method: http.MethodPatch, path: "/api/workspaces/" + testWorkspaceID, body: []byte(`{"name":"blocked"}`)},
		"invitation": {method: http.MethodPost, path: "/api/workspaces/" + testWorkspaceID + "/members", body: []byte(`{"email":"blocked@example.test","role":"member"}`)},
		"onboarding": {method: http.MethodPost, path: "/api/me/onboarding/runtime-bootstrap", body: []byte(`{"workspace_id":"00000000-0000-0000-0000-000000000099"}`)},
	} {
		t.Run(name, func(t *testing.T) {
			request, err := http.NewRequest(testCase.method, server.URL+testCase.path, bytes.NewReader(testCase.body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/json")
			setAssertion(t, request, testCase.body, "router-request-"+name)

			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d", response.StatusCode)
			}
		})
	}

	content := []byte("same workspace attachment")
	if err := os.WriteFile(filepath.Join(uploadDir, "same.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	foreignSlug := "tag-289-foreign-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	var foreignWorkspaceID string
	if err := testPool.QueryRow(t.Context(), `
		INSERT INTO workspace (name, slug)
		VALUES ('Tag 289 foreign workspace', $1)
		RETURNING id::text
	`, foreignSlug).Scan(&foreignWorkspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1::uuid`, foreignWorkspaceID)
	})
	if _, err := testPool.Exec(t.Context(), `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1::uuid, $2::uuid, 'owner')
	`, foreignWorkspaceID, testUserID); err != nil {
		t.Fatal(err)
	}

	workspaceListRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/workspaces", nil)
	if err != nil {
		t.Fatal(err)
	}
	setAssertion(t, workspaceListRequest, nil, "router-workspace-list")
	workspaceListResponse, err := http.DefaultClient.Do(workspaceListRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer workspaceListResponse.Body.Close()
	var workspaceList []struct {
		ID string `json:"id"`
	}
	if workspaceListResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(workspaceListResponse.Body)
		t.Fatalf("workspace list status = %d, body=%s", workspaceListResponse.StatusCode, body)
	}
	if err := json.NewDecoder(workspaceListResponse.Body).Decode(&workspaceList); err != nil {
		t.Fatal(err)
	}
	if len(workspaceList) != 1 || workspaceList[0].ID != testWorkspaceID {
		t.Fatalf("mirrored Workspace list escaped asserted scope: %#v", workspaceList)
	}

	insertAttachment := func(t *testing.T, workspaceID, filename string, size int) string {
		t.Helper()
		var attachmentID string
		if err := testPool.QueryRow(t.Context(), `
			INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
			VALUES ($1::uuid, 'member', $2::uuid, $3, '/uploads/' || $3, 'text/plain', $4)
			RETURNING id::text
		`, workspaceID, testUserID, filename, size).Scan(&attachmentID); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1::uuid`, attachmentID)
		})
		return attachmentID
	}
	sameWorkspaceAttachmentID := insertAttachment(t, testWorkspaceID, "same.txt", len(content))
	foreignWorkspaceAttachmentID := insertAttachment(t, foreignWorkspaceID, "foreign.txt", 7)

	for name, testCase := range map[string]struct {
		attachmentID string
		status       int
		body         string
	}{
		"same workspace":    {attachmentID: sameWorkspaceAttachmentID, status: http.StatusOK, body: string(content)},
		"foreign workspace": {attachmentID: foreignWorkspaceAttachmentID, status: http.StatusNotFound},
	} {
		t.Run("attachment "+name, func(t *testing.T) {
			path := "/api/attachments/" + testCase.attachmentID + "/download"
			request, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			setAssertion(t, request, nil, "router-attachment-"+name)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			responseBody, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if response.StatusCode != testCase.status {
				t.Fatalf("status = %d, body = %s", response.StatusCode, responseBody)
			}
			if testCase.body != "" && string(responseBody) != testCase.body {
				t.Fatalf("body = %q", responseBody)
			}
		})
	}
}
