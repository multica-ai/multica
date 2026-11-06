package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
			"status_changes": "muted",
		}),
	)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("seed preference: status=%d body=%s", putRecorder.Code, putRecorder.Body.String())
	}

	patchRecorder := httptest.NewRecorder()
	testHandler.PatchNotificationPreferences(
		patchRecorder,
		notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
			"comments": "muted",
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
	if response.Preferences["status_changes"] != "muted" {
		t.Fatalf("status_changes was replaced: %#v", response.Preferences)
	}
	if response.Preferences["comments"] != "muted" {
		t.Fatalf("comments patch missing: %#v", response.Preferences)
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
	if preferences["status_changes"] != "all" || preferences["comments"] != "muted" {
		t.Fatalf("unexpected persisted preferences: %#v", preferences)
	}
}

// TestPatchNotificationPreferencesAcceptsMentions covers the group added by
// #6468. The whitelist rejects unknown keys, so a client shipped ahead of the
// server would 400 on this key — the reason the server must deploy first.
func TestPatchNotificationPreferencesAcceptsMentions(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM notification_preference
			WHERE workspace_id = $1 AND user_id = $2
		`, testWorkspaceID, testUserID)
	})

	recorder := httptest.NewRecorder()
	testHandler.PatchNotificationPreferences(
		recorder,
		notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
			"mentions": "muted",
		}),
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
	if response.Preferences["mentions"] != "muted" {
		t.Fatalf("mentions not persisted: %#v", response.Preferences)
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
	}{
		{
			name:    "invalid host",
			webhook: "https://example.com/v1/spaces/AAAA/messages?key=k&token=t",
		},
		{
			name:    "confirmation fails",
			webhook: handlerGoogleChatWebhook,
			sendErr: errors.New("connection refused"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetNotificationPreference(t)
			sender := &fakeGoogleChatSender{err: test.sendErr}
			previousSender := testHandler.GoogleChatSender
			testHandler.GoogleChatSender = sender
			t.Cleanup(func() { testHandler.GoogleChatSender = previousSender })

			testutil.Call(
				t,
				testHandler.PatchNotificationPreferences,
				notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
					"google_chat_webhook": test.webhook,
				}),
			).Want(http.StatusBadRequest)

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
