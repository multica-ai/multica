package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/redis/go-redis/v9"
)

// Simulate an unexpired positive cache entry without requiring a Redis server.
type positiveMembershipRedis struct{ redis.UniversalClient }

func (positiveMembershipRedis) Get(ctx context.Context, key string) *redis.StringCmd {
	return redis.NewStringResult("1", nil)
}

func TestAttachmentCapability_MemberRemovalRevokesOutstandingLinks(t *testing.T) {
	store := installProxyModeStorage(t)
	userID := dbfx.User(t, "Capability reader", "capability-reader@example.test")
	memberID := dbfx.Member(t, testWorkspaceID, userID, "member")
	body := []byte("private attachment bytes")
	store.files = map[string][]byte{"downloads/revocable.png": body}
	id := dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"uploader_type": "member",
		"uploader_id":   testUserID,
		"filename":      "revocable.png",
		"url":           "https://cdn.example.com/downloads/revocable.png",
		"content_type":  "image/png",
		"size_bytes":    len(body),
	})
	req := testutil.WithURLParams(httptest.NewRequest(http.MethodGet, "/api/attachments/"+id, nil), "id", id)
	testutil.WithHeaders(req, "X-User-ID", userID, "X-Workspace-ID", testWorkspaceID)
	var resp AttachmentResponse
	testutil.Call(t, testHandler.GetAttachmentByID, req).Want(http.StatusOK).JSON(&resp)

	for _, path := range []string{resp.DownloadURL, resp.AttachmentDownloadURL} {
		redeem := testutil.WithURLParams(httptest.NewRequest(http.MethodGet, path, nil), "id", id)
		got := testutil.Call(t, testHandler.DownloadAttachmentWithCapability, redeem).Want(http.StatusOK)
		if got.Text() != string(body) {
			t.Fatalf("redeemed body = %q", got.Text())
		}
	}
	// A populated membership cache must not extend a capability after revocation.
	origCache := testHandler.MembershipCache
	testHandler.MembershipCache = auth.NewMembershipCache(positiveMembershipRedis{})
	t.Cleanup(func() { testHandler.MembershipCache = origCache })
	if !testHandler.MembershipCache.Get(req.Context(), userID, testWorkspaceID) {
		t.Fatal("expected a stale positive cache entry")
	}
	dbfx.Exec(t, "DELETE FROM member WHERE id = $1", memberID)
	reads, _ := store.streamCopyCalls()
	for _, path := range []string{resp.DownloadURL, resp.AttachmentDownloadURL} {
		redeem := testutil.WithURLParams(httptest.NewRequest(http.MethodGet, path, nil), "id", id)
		// Another member's credentials or workspace selection cannot override the signed user.
		testutil.WithHeaders(redeem, "X-User-ID", testUserID, "X-Workspace-ID", testWorkspaceID)
		redeem.Header.Set("Range", "bytes=1-")
		testutil.Call(t, testHandler.DownloadAttachmentWithCapability, redeem).Want(http.StatusNotFound)
	}
	if after, _ := store.streamCopyCalls(); after != reads {
		t.Fatalf("revoked links opened storage: reads %d -> %d", reads, after)
	}
}

func TestAttachmentCapability_UserBindingAndLegacyRejection(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, intent := range []string{"", attachmentCapabilityDownloadIntent} {
		path, err := url.Parse(attachmentCapabilityIntentPath(capabilityTestAttachmentID, testUserID, now, intent))
		if err != nil {
			t.Fatal(err)
		}
		query := path.Query()
		for _, userID := range []string{"", "invalid", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", testUserID + "|extra"} {
			if verifyAttachmentCapability(capabilityTestAttachmentID, userID, query.Get("exp"), query.Get("sig"), intent, now) {
				t.Fatalf("capability verified with a different/missing user %q, intent %q", userID, intent)
			}
		}
		// Even adding a valid uid to an old v1 URL cannot upgrade its signature.
		legacy := "v1|" + capabilityTestAttachmentID + "|" + query.Get("exp")
		if intent != "" {
			legacy += "|" + intent
		}
		mac := hmac.New(sha256.New, attachmentCapabilitySigningKey())
		mac.Write([]byte(legacy))
		if verifyAttachmentCapability(capabilityTestAttachmentID, testUserID, query.Get("exp"), hex.EncodeToString(mac.Sum(nil)), intent, now) {
			t.Fatalf("legacy unbound capability verified under intent %q", intent)
		}
	}
}

func TestGetAttachmentByID_AllModesMintDownloadCapabilities(t *testing.T) {
	for _, mode := range []string{"proxy", "presign", "cloudfront", "auto"} {
		t.Run(mode, func(t *testing.T) {
			store := installProxyModeStorage(t)
			testHandler.cfg.AttachmentDownloadMode = mode
			if mode == "cloudfront" {
				testHandler.CFSigner = testCloudFrontSigner(t)
			}
			body := []byte("0123456789")
			store.files = map[string][]byte{"downloads/modes.png": body}
			id := dbfx.Insert(t, "attachment", testutil.Cols{
				"workspace_id":  testWorkspaceID,
				"uploader_type": "member",
				"uploader_id":   testUserID,
				"filename":      "modes.png",
				"url":           "https://cdn.example.com/downloads/modes.png",
				"content_type":  "image/png",
				"size_bytes":    len(body),
			})
			req := testutil.WithURLParams(httptest.NewRequest(http.MethodGet, "/api/attachments/"+id, nil), "id", id)
			testutil.WithHeaders(req, "X-User-ID", testUserID, "X-Workspace-ID", testWorkspaceID)
			var resp AttachmentResponse
			metadata := testutil.Call(t, testHandler.GetAttachmentByID, req).Want(http.StatusOK).JSON(&resp)
			if metadata.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("user-bound capability metadata must not be cached")
			}
			download, err := url.Parse(resp.AttachmentDownloadURL)
			if err != nil {
				t.Fatal(err)
			}
			if download.IsAbs() || download.Path != "/api/attachments/"+id+"/signed-download" || download.Query().Get("dl") != "1" {
				t.Fatalf("download button URL = %q, want a forced-attachment capability", resp.AttachmentDownloadURL)
			}
			if download.Query().Get("uid") != testUserID {
				t.Fatalf("download capability must bind the requesting user: %q", resp.AttachmentDownloadURL)
			}
			if resp.DownloadURL == resp.AttachmentDownloadURL || strings.Contains(resp.MarkdownURL, "signed-download") {
				t.Fatalf("preview and persistent URLs must stay separate: %+v", resp)
			}
			redeem := testutil.WithURLParams(httptest.NewRequest(http.MethodGet, resp.AttachmentDownloadURL, nil), "id", id)
			redeem.Header.Set("Range", "bytes=2-5")
			got := testutil.Call(t, testHandler.DownloadAttachmentWithCapability, redeem).Want(http.StatusPartialContent)
			if got.Text() != "2345" || got.Header().Get("Content-Range") != "bytes 2-5/10" {
				t.Fatalf("range response: headers=%v body=%q", got.Header(), got.Text())
			}
			if got.Header().Get("Location") != "" || !strings.HasPrefix(got.Header().Get("Content-Disposition"), "attachment;") {
				t.Fatalf("download must stream as an attachment, not redirect: %v", got.Header())
			}
			if got.Header().Get("Cache-Control") != "no-store" || got.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatalf("capability response must not cache or leak its query: %v", got.Header())
			}
		})
	}
}

func TestAttachmentCapability_RedemptionUsesAttachmentWorkspace(t *testing.T) {
	store := installProxyModeStorage(t)
	otherWorkspace := dbfx.Workspace(t, "Other capability workspace", "capability-other-workspace")
	id := dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id":  otherWorkspace,
		"uploader_type": "member",
		"uploader_id":   testUserID,
		"filename":      "private.txt",
		"url":           "https://cdn.example.com/private.txt",
		"content_type":  "text/plain",
		"size_bytes":    7,
	})
	store.files = map[string][]byte{"private.txt": []byte("private")}
	// A valid signature alone does not prove current membership in the row's
	// workspace, even when the signed user belongs to the request's workspace.
	path := attachmentDownloadCapabilityPath(id, testUserID, time.Now()) + "&workspace_id=" + testWorkspaceID
	req := testutil.WithURLParams(httptest.NewRequest(http.MethodGet, path, nil), "id", id)
	testutil.WithHeaders(req, "X-Workspace-ID", testWorkspaceID, "X-User-ID", testUserID)
	testutil.Call(t, testHandler.DownloadAttachmentWithCapability, req).Want(http.StatusNotFound)
	if reads, _ := store.streamCopyCalls(); reads != 0 {
		t.Fatal("cross-workspace denial opened the object")
	}

	dbfx.Member(t, otherWorkspace, testUserID, "member")
	testutil.Call(t, testHandler.DownloadAttachmentWithCapability, req).Want(http.StatusOK)
	queries := testHandler.Queries
	testHandler.Queries = db.New(failQueryDBTX{
		DBTX:   testPool,
		failOn: "-- name: GetMemberByUserAndWorkspace",
		err:    errors.New("injected membership lookup failure"),
	})
	t.Cleanup(func() { testHandler.Queries = queries })
	before, _ := store.streamCopyCalls()
	testutil.Call(t, testHandler.DownloadAttachmentWithCapability, req).Want(http.StatusNotFound)
	if after, _ := store.streamCopyCalls(); after != before {
		t.Fatal("failed membership query opened the object")
	}
}

func TestAttachmentCapability_AuthenticatedMintAndUnauthenticatedRedemption(t *testing.T) {
	store := installProxyModeStorage(t)
	userID := dbfx.User(t, "Download caller", "capability-caller@example.test")
	memberID := dbfx.Member(t, testWorkspaceID, userID, "member")
	id := dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"uploader_type": "member",
		"uploader_id":   testUserID,
		"filename":      "native.png",
		"url":           "https://cdn.example.com/native.png",
		"content_type":  "image/png",
		"size_bytes":    3,
	})
	store.files = map[string][]byte{"native.png": []byte("png")}
	user, err := testHandler.Queries.GetUser(context.Background(), parseUUID(userID))
	if err != nil {
		t.Fatal(err)
	}
	token, err := testHandler.issueJWT(user)
	if err != nil {
		t.Fatal(err)
	}
	// Match the production route boundaries: authenticated + workspace-checked
	// metadata, authenticated legacy download, capability-only redemption.
	router := chi.NewRouter()
	authMiddleware := middleware.Auth(testHandler.Queries, nil, nil, nil)
	router.With(authMiddleware, middleware.RequireWorkspaceMember(testHandler.Queries)).Get("/api/attachments/{id}", testHandler.GetAttachmentByID)
	router.With(authMiddleware).Get("/api/attachments/{id}/download", testHandler.DownloadAttachment)
	router.Get("/api/attachments/{id}/signed-download", testHandler.DownloadAttachmentWithCapability)

	metadataPath := "/api/attachments/" + id + "?uid=" + testUserID
	unauthenticated := testutil.WithHeaders(httptest.NewRequest(http.MethodGet, metadataPath, nil), "X-User-ID", testUserID, "X-Workspace-ID", testWorkspaceID)
	testutil.Call(t, router.ServeHTTP, unauthenticated).Want(http.StatusUnauthorized)
	testutil.Call(t, router.ServeHTTP, httptest.NewRequest(http.MethodGet, "/api/attachments/"+id+"/download", nil)).Want(http.StatusUnauthorized)

	req := testutil.WithHeaders(httptest.NewRequest(http.MethodGet, metadataPath, nil), "Authorization", "Bearer "+token, "X-User-ID", testUserID, "X-Workspace-ID", testWorkspaceID)
	var resp AttachmentResponse
	testutil.Call(t, router.ServeHTTP, req).Want(http.StatusOK).JSON(&resp)
	download, err := url.Parse(resp.AttachmentDownloadURL)
	if err != nil {
		t.Fatal(err)
	}
	if download.Query().Get("uid") != userID {
		t.Fatal("capability must bind the authenticated caller, not the uploader, query or spoofed header")
	}
	testutil.Call(t, router.ServeHTTP, httptest.NewRequest(http.MethodGet, resp.AttachmentDownloadURL, nil)).Want(http.StatusOK)
	for _, replacement := range []string{"", testUserID, "bad-user-id"} {
		query := download.Query()
		query.Set("uid", replacement)
		tampered := download.Path + "?" + query.Encode()
		testutil.Call(t, router.ServeHTTP, httptest.NewRequest(http.MethodGet, tampered, nil)).Want(http.StatusForbidden)
	}
	dbfx.Exec(t, "DELETE FROM member WHERE id = $1", memberID)
	testutil.Call(t, router.ServeHTTP, req.Clone(req.Context())).Want(http.StatusNotFound)
	testutil.Call(t, router.ServeHTTP, httptest.NewRequest(http.MethodGet, resp.AttachmentDownloadURL, nil)).Want(http.StatusNotFound)

	dbfx.Exec(t, "DELETE FROM attachment WHERE id = $1", id)
	testutil.Call(t, router.ServeHTTP, httptest.NewRequest(http.MethodGet, resp.AttachmentDownloadURL, nil)).Want(http.StatusNotFound)
}
