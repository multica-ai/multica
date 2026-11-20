package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func notificationPreferenceRequest(
	t *testing.T,
	method string,
	preferences map[string]string,
) *http.Request {
	t.Helper()

	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(
		context.Background(),
		db.GetMemberByUserAndWorkspaceParams{
			UserID:      parseUUID(testUserID),
			WorkspaceID: parseUUID(testWorkspaceID),
		},
	)
	if err != nil {
		t.Fatalf("load test member: %v", err)
	}

	req := newRequest(method, "/api/notification-preferences", map[string]any{
		"preferences": preferences,
	})
	return req.WithContext(
		middleware.SetMemberContext(req.Context(), testWorkspaceID, member),
	)
}

func TestPatchNotificationPreferencesMergesWithoutReplacing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
		DELETE FROM notification_preference
		WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("reset notification preference: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM notification_preference
			WHERE workspace_id = $1 AND user_id = $2
		`, testWorkspaceID, testUserID)
	})

	putRecorder := httptest.NewRecorder()
	testHandler.UpdateNotificationPreferences(
		putRecorder,
		notificationPreferenceRequest(t, http.MethodPut, map[string]string{
			"updates": "muted",
		}),
	)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("seed preference: status=%d body=%s", putRecorder.Code, putRecorder.Body.String())
	}

	patchRecorder := httptest.NewRecorder()
	testHandler.PatchNotificationPreferences(
		patchRecorder,
		notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
			"comments":     "muted",
			"chat_replies": "muted",
		}),
	)
	if patchRecorder.Code != http.StatusOK {
		t.Fatalf("patch preference: status=%d body=%s", patchRecorder.Code, patchRecorder.Body.String())
	}

	var response struct {
		WorkspaceID string            `json:"workspace_id"`
		Preferences map[string]string `json:"preferences"`
	}
	if err := json.NewDecoder(patchRecorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if response.WorkspaceID != testWorkspaceID {
		t.Fatalf("workspace_id = %q, want %q", response.WorkspaceID, testWorkspaceID)
	}
	if response.Preferences["updates"] != "muted" {
		t.Fatalf("updates was replaced: %#v", response.Preferences)
	}
	if response.Preferences["comments"] != "muted" {
		t.Fatalf("comments patch missing: %#v", response.Preferences)
	}
	if response.Preferences["chat_replies"] != "muted" {
		t.Fatalf("chat_replies patch missing: %#v", response.Preferences)
	}

	enableRecorder := httptest.NewRecorder()
	testHandler.PatchNotificationPreferences(
		enableRecorder,
		notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
			"status_changes": "all",
		}),
	)
	if enableRecorder.Code != http.StatusOK {
		t.Fatalf("enable preference: status=%d body=%s", enableRecorder.Code, enableRecorder.Body.String())
	}

	var persisted []byte
	if err := testPool.QueryRow(ctx, `
		SELECT preferences
		FROM notification_preference
		WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, testUserID).Scan(&persisted); err != nil {
		t.Fatalf("load persisted preference: %v", err)
	}
	var preferences map[string]string
	if err := json.Unmarshal(persisted, &preferences); err != nil {
		t.Fatalf("decode persisted preference: %v", err)
	}
	if preferences["status_changes"] != "all" || preferences["comments"] != "muted" || preferences["updates"] != "muted" {
		t.Fatalf("unexpected persisted preferences: %#v", preferences)
	}
}

// Status changes are muted by default (MUL-9449). Responses say so explicitly,
// because installed clients read a missing key as "all" and would show the
// switch on; storage stays sparse.
func TestNotificationPreferencesReportStatusChangesMutedByDefault(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	reset := func() {
		_, _ = testPool.Exec(ctx, `
			DELETE FROM notification_preference
			WHERE workspace_id = $1 AND user_id = $2
		`, testWorkspaceID, testUserID)
	}
	reset()
	t.Cleanup(reset)

	call := func(method string, prefs map[string]string) map[string]string {
		t.Helper()
		recorder := httptest.NewRecorder()
		switch method {
		case http.MethodGet:
			testHandler.GetNotificationPreferences(recorder, notificationPreferenceRequest(t, method, nil))
		case http.MethodPatch:
			testHandler.PatchNotificationPreferences(recorder, notificationPreferenceRequest(t, method, prefs))
		}
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status=%d body=%s", method, recorder.Code, recorder.Body.String())
		}
		var response struct {
			Preferences map[string]string `json:"preferences"`
		}
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatalf("%s: decode response: %v", method, err)
		}
		return response.Preferences
	}

	if got := call(http.MethodGet, nil); len(got) != 1 || got["status_changes"] != "muted" {
		t.Fatalf("GET without a row = %#v, want status_changes muted", got)
	}

	if got := call(http.MethodPatch, map[string]string{"comments": "muted"}); got["status_changes"] != "muted" || got["comments"] != "muted" {
		t.Fatalf("PATCH comments = %#v, want comments and the default status_changes muted", got)
	}
	var stored map[string]string
	var raw []byte
	if err := testPool.QueryRow(ctx, `
		SELECT preferences FROM notification_preference
		WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, testUserID).Scan(&raw); err != nil {
		t.Fatalf("load persisted preference: %v", err)
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("decode persisted preference: %v", err)
	}
	if _, ok := stored["status_changes"]; ok {
		t.Fatalf("the default was persisted: %#v", stored)
	}

	call(http.MethodPatch, map[string]string{"status_changes": "all"})
	if got := call(http.MethodGet, nil); got["status_changes"] != "all" || got["comments"] != "muted" {
		t.Fatalf("GET after turning status changes on = %#v", got)
	}
}

// A client built before PATCH (MUL-4798) replaces the whole object and leaves
// a group out to mean "on". Its status changes stay on through persistence,
// read-back and delivery, while an explicit "muted" still turns them off
// (MUL-9449).
func TestLegacyPutKeepsOmittedStatusChangesOn(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	reset := func() {
		_, _ = testPool.Exec(ctx, `
			DELETE FROM notification_preference
			WHERE workspace_id = $1 AND user_id = $2
		`, testWorkspaceID, testUserID)
	}
	t.Cleanup(reset)

	for _, tc := range []struct {
		name      string
		sent      map[string]string
		stored    string
		delivered bool
	}{
		{"nothing muted", map[string]string{}, "all", true},
		{"another group muted", map[string]string{"comments": "muted"}, "all", true},
		{"status changes muted", map[string]string{"status_changes": "muted"}, "muted", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reset()

			recorder := httptest.NewRecorder()
			testHandler.UpdateNotificationPreferences(recorder, notificationPreferenceRequest(t, http.MethodPut, tc.sent))
			if recorder.Code != http.StatusOK {
				t.Fatalf("PUT: status=%d body=%s", recorder.Code, recorder.Body.String())
			}

			var raw []byte
			if err := testPool.QueryRow(ctx, `
				SELECT preferences FROM notification_preference
				WHERE workspace_id = $1 AND user_id = $2
			`, testWorkspaceID, testUserID).Scan(&raw); err != nil {
				t.Fatalf("load persisted preference: %v", err)
			}
			var stored map[string]string
			if err := json.Unmarshal(raw, &stored); err != nil {
				t.Fatalf("decode persisted preference: %v", err)
			}
			if stored["status_changes"] != tc.stored {
				t.Fatalf("persisted %#v, want status_changes %q", stored, tc.stored)
			}
			for group, value := range tc.sent {
				if stored[group] != value {
					t.Fatalf("persisted %#v, want the sent %s=%q kept", stored, group, value)
				}
			}

			get := httptest.NewRecorder()
			testHandler.GetNotificationPreferences(get, notificationPreferenceRequest(t, http.MethodGet, nil))
			var response struct {
				Preferences map[string]string `json:"preferences"`
			}
			if err := json.NewDecoder(get.Body).Decode(&response); err != nil {
				t.Fatalf("decode GET response: %v", err)
			}
			if response.Preferences["status_changes"] != tc.stored {
				t.Fatalf("GET = %#v, want status_changes %q", response.Preferences, tc.stored)
			}

			issueID := dbfx.Issue(t, "Legacy PUT status change", testutil.Cols{"workspace_id": testWorkspaceID})
			dbfx.Cleanup(t, `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
			_, inserted, err := inbox.Deliver(ctx, testHandler.Queries, nil, inbox.Notification{Params: db.CreateInboxItemParams{
				WorkspaceID:   parseUUID(testWorkspaceID),
				RecipientType: "member",
				RecipientID:   parseUUID(testUserID),
				Type:          "status_changed",
				Severity:      "info",
				IssueID:       parseUUID(issueID),
				Title:         "Legacy PUT status change",
				Details:       []byte(`{}`),
			}})
			if err != nil {
				t.Fatalf("deliver status_changed: %v", err)
			}
			if inserted != tc.delivered {
				t.Fatalf("status_changed delivered = %v, want %v", inserted, tc.delivered)
			}
		})
	}
}

// TestPatchNotificationPreferencesAcceptsAddedGroups covers groups added after
// the original set: mentions (#6468), reactions (MUL-9328) and
// auto_archive_completed (MUL-9336). The whitelist rejects unknown keys, so a
// client shipped ahead of the server would 400 on these keys — the reason the
// server must deploy first.
func TestPatchNotificationPreferencesAcceptsAddedGroups(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	for _, groups := range [][]string{
		{"mentions"},
		{"reactions"},
		{"auto_archive_completed"},
		{"reactions", "auto_archive_completed"},
	} {
		t.Run(strings.Join(groups, "+"), func(t *testing.T) {
			t.Cleanup(func() {
				_, _ = testPool.Exec(context.Background(), `
					DELETE FROM notification_preference
					WHERE workspace_id = $1 AND user_id = $2
				`, testWorkspaceID, testUserID)
			})

			patch := map[string]string{}
			for _, group := range groups {
				patch[group] = "muted"
			}
			recorder := httptest.NewRecorder()
			testHandler.PatchNotificationPreferences(
				recorder,
				notificationPreferenceRequest(t, http.MethodPatch, patch),
			)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}

			var response struct {
				Preferences map[string]string `json:"preferences"`
			}
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			for _, group := range groups {
				if response.Preferences[group] != "muted" {
					t.Fatalf("%s not persisted: %#v", group, response.Preferences)
				}
			}
		})
	}
}

func TestPatchNotificationPreferencesRejectsUnknownGroups(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	recorder := httptest.NewRecorder()
	testHandler.PatchNotificationPreferences(
		recorder,
		notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
			"unknown_group": "muted",
		}),
	)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

type fakeGoogleChatSender struct {
	calls      int
	webhookURL string
	text       string
	err        error
}

func (s *fakeGoogleChatSender) Send(_ context.Context, webhookURL, text string) error {
	s.calls++
	s.webhookURL = webhookURL
	s.text = text
	return s.err
}

const handlerGoogleChatWebhook = "https://chat.googleapis.com/v1/spaces/AAAA_123/messages?key=test-key&token=test-token"

func resetNotificationPreference(t *testing.T) {
	t.Helper()
	dbfx.Exec(t, `
		DELETE FROM notification_preference
		WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, testUserID)
	dbfx.Cleanup(t, `
		DELETE FROM notification_preference
		WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, testUserID)
}

func TestGoogleChatWebhookIsConfirmedMaskedAndPreserved(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	resetNotificationPreference(t)

	sender := &fakeGoogleChatSender{}
	previousSender := testHandler.GoogleChatSender
	testHandler.GoogleChatSender = sender
	t.Cleanup(func() { testHandler.GoogleChatSender = previousSender })

	var response struct {
		Preferences map[string]string `json:"preferences"`
	}
	testutil.Call(
		t,
		testHandler.PatchNotificationPreferences,
		notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
			"google_chat_webhook": handlerGoogleChatWebhook,
		}),
	).Want(http.StatusOK).JSON(&response)

	masked := response.Preferences["google_chat_webhook"]
	if masked == "" ||
		strings.Contains(masked, "test-key") ||
		strings.Contains(masked, "test-token") {
		t.Fatalf("response did not mask webhook: %q", masked)
	}
	if sender.calls != 1 || sender.webhookURL != handlerGoogleChatWebhook {
		t.Fatalf("confirmation calls=%d url=%q", sender.calls, sender.webhookURL)
	}
	if !strings.Contains(sender.text, "Handler Tests") {
		t.Fatalf("confirmation text = %q", sender.text)
	}

	var stored []byte
	dbfx.QueryRow(t, `
		SELECT preferences
		FROM notification_preference
		WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, testUserID).Scan(&stored)
	var storedPreferences map[string]string
	if err := json.Unmarshal(stored, &storedPreferences); err != nil {
		t.Fatalf("decode stored preferences: %v", err)
	}
	if storedPreferences["google_chat_webhook"] != handlerGoogleChatWebhook {
		t.Fatalf("stored webhook = %q", storedPreferences["google_chat_webhook"])
	}

	response.Preferences = nil
	testutil.Call(
		t,
		testHandler.PatchNotificationPreferences,
		notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
			"google_chat_webhook": masked,
		}),
	).Want(http.StatusOK).JSON(&response)
	if sender.calls != 1 {
		t.Fatalf("masked echo sent another confirmation: calls=%d", sender.calls)
	}

	response.Preferences = nil
	testutil.Call(
		t,
		testHandler.UpdateNotificationPreferences,
		notificationPreferenceRequest(t, http.MethodPut, map[string]string{
			"comments": "muted",
		}),
	).Want(http.StatusOK).JSON(&response)
	if response.Preferences["google_chat_webhook"] != masked {
		t.Fatalf("replace-all PUT dropped or changed webhook: %#v", response.Preferences)
	}

	response.Preferences = nil
	testutil.Call(
		t,
		testHandler.GetNotificationPreferences,
		notificationPreferenceRequest(t, http.MethodGet, map[string]string{}),
	).Want(http.StatusOK).JSON(&response)
	if response.Preferences["google_chat_webhook"] != masked {
		t.Fatalf("GET returned unmasked webhook: %#v", response.Preferences)
	}

	response.Preferences = nil
	testutil.Call(
		t,
		testHandler.PatchNotificationPreferences,
		notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
			"google_chat_webhook": "",
		}),
	).Want(http.StatusOK).JSON(&response)
	if response.Preferences["google_chat_webhook"] != "" {
		t.Fatalf("clear response = %#v", response.Preferences)
	}
	if sender.calls != 1 {
		t.Fatalf("clear sent a confirmation: calls=%d", sender.calls)
	}
}

func TestGoogleChatWebhookRejectsInvalidOrUnreachableValuesWithoutStoring(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name    string
		webhook string
		sendErr error
		code    string
	}{
		{
			name:    "invalid host",
			webhook: "https://example.com/v1/spaces/AAAA/messages?key=k&token=t",
			code:    "google_chat_invalid_url",
		},
		{
			name:    "missing credentials",
			webhook: "https://chat.googleapis.com/v1/spaces/AAAA/messages?key=k",
			code:    "google_chat_missing_credentials",
		},
		{
			name:    "confirmation fails",
			webhook: handlerGoogleChatWebhook,
			sendErr: errors.New("connection refused"),
			code:    "google_chat_connection_failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetNotificationPreference(t)
			sender := &fakeGoogleChatSender{err: test.sendErr}
			previousSender := testHandler.GoogleChatSender
			testHandler.GoogleChatSender = sender
			t.Cleanup(func() { testHandler.GoogleChatSender = previousSender })

			var response struct {
				Code string `json:"code"`
			}
			testutil.Call(
				t,
				testHandler.PatchNotificationPreferences,
				notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
					"google_chat_webhook": test.webhook,
				}),
			).Want(http.StatusBadRequest).JSON(&response)
			if response.Code != test.code {
				t.Fatalf("error code = %q, want %q", response.Code, test.code)
			}

			if got := dbfx.Count(t, `
				SELECT count(*)
				FROM notification_preference
				WHERE workspace_id = $1 AND user_id = $2
			`, testWorkspaceID, testUserID); got != 0 {
				t.Fatalf("stored preference rows = %d, want 0", got)
			}
		})
	}
}

func TestPatchNotificationPreferencesValidatesOthersIssuesValues(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM notification_preference
			WHERE workspace_id = $1 AND user_id = $2
		`, testWorkspaceID, testUserID)
	})

	for value, want := range map[string]int{
		"all":         http.StatusOK,
		"key_updates": http.StatusOK,
		"muted":       http.StatusBadRequest,
	} {
		recorder := httptest.NewRecorder()
		testHandler.PatchNotificationPreferences(
			recorder,
			notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
				"others_issues": value,
			}),
		)
		if recorder.Code != want {
			t.Errorf("others_issues=%q: status=%d want %d body=%s",
				value, recorder.Code, want, recorder.Body.String())
		}
	}

	// key_updates stays exclusive to others_issues.
	recorder := httptest.NewRecorder()
	testHandler.PatchNotificationPreferences(
		recorder,
		notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
			"comments": "key_updates",
		}),
	)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("comments=key_updates: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
