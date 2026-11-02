package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// briefPushChatApp is a chat app whose notifier the test registers, so the
// preference API accepts a binding of that type.
const briefPushChatApp = channel.Type("telegram")

// noopBriefNotifier is never sent to: the preference API only checks that a
// notifier is registered.
type noopBriefNotifier struct{}

func (noopBriefNotifier) Send(context.Context, channel.Binding, channel.Message) error { return nil }

// briefPushEnv is a workspace with two members, each with a Telegram binding,
// and a service with the Telegram notifier registered on the test handler.
type briefPushEnv struct {
	WorkspaceID string
	Alice       string
	Bob         string
	AliceChat   string
	BobChat     string
}

func newBriefPushEnv(t *testing.T) briefPushEnv {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ws := dbfx.Workspace(t, "Brief push "+uuid.NewString(), "brief-push-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	alice := dbfx.User(t, "Alice", fmt.Sprintf("brief-push-alice-%s@example.com", uuid.NewString()))
	bob := dbfx.User(t, "Bob", fmt.Sprintf("brief-push-bob-%s@example.com", uuid.NewString()))
	dbfx.Member(t, ws, alice, "member")
	dbfx.Member(t, ws, bob, "member")

	env := briefPushEnv{WorkspaceID: ws, Alice: alice, Bob: bob}
	env.AliceChat = briefPushBinding(t, ws, alice, string(briefPushChatApp))
	env.BobChat = briefPushBinding(t, ws, bob, string(briefPushChatApp))

	// Give the test handler a service with a notifier, and restore the nil one
	// afterwards so other tests see the deployment default.
	prev := testHandler.BriefPush
	svc := service.NewBriefPushService(testHandler.Queries, "")
	svc.RegisterNotifier(briefPushChatApp, noopBriefNotifier{})
	testHandler.BriefPush = svc
	t.Cleanup(func() { testHandler.BriefPush = prev })
	return env
}

// briefPushBinding inserts a chat binding for userID in ws.
func briefPushBinding(t *testing.T, ws, userID, channelType string) string {
	t.Helper()
	return dbfx.Insert(t, "channel_user_binding", testutil.Cols{
		"workspace_id":    ws,
		"multica_user_id": userID,
		"installation_id": uuid.NewString(),
		"channel_type":    channelType,
		"channel_user_id": uuid.NewString(),
	})
}

func (e briefPushEnv) get(t *testing.T, userID string) *testutil.Response {
	t.Helper()
	req := newRequest("GET", "/api/office/brief-push", nil)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	req = asUser(req, userID)
	return testutil.Call(t, testHandler.GetBriefPushPreference, req)
}

func (e briefPushEnv) put(t *testing.T, userID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := newRequest("PUT", "/api/office/brief-push", body)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	req = asUser(req, userID)
	return testutil.Call(t, testHandler.UpdateBriefPushPreference, req)
}

func TestBriefPushPreference_DefaultIsOff(t *testing.T) {
	env := newBriefPushEnv(t)

	var got BriefPushPreference
	env.get(t, env.Alice).Want(http.StatusOK).JSON(&got)
	if got.BriefPush || got.BindingID != nil || got.QuietHours != nil {
		t.Fatalf("default preference = %+v, want off with no chat and no quiet hours", got)
	}
}

func TestBriefPushPreference_SaveAndReadBack(t *testing.T) {
	env := newBriefPushEnv(t)

	var saved BriefPushPreference
	env.put(t, env.Alice, map[string]any{
		"brief_push":  true,
		"binding_id":  env.AliceChat,
		"quiet_hours": map[string]any{"start": 22, "end": 7},
	}).Want(http.StatusOK).JSON(&saved)
	if !saved.BriefPush || saved.BindingID == nil || *saved.BindingID != env.AliceChat {
		t.Fatalf("saved = %+v", saved)
	}
	if saved.QuietHours == nil || saved.QuietHours.Start != 22 || saved.QuietHours.End != 7 {
		t.Fatalf("quiet hours = %+v", saved.QuietHours)
	}

	var read BriefPushPreference
	env.get(t, env.Alice).Want(http.StatusOK).JSON(&read)
	if read.BriefPush != saved.BriefPush || *read.BindingID != env.AliceChat ||
		read.QuietHours == nil || *read.QuietHours != *saved.QuietHours {
		t.Fatalf("read back %+v, saved %+v", read, saved)
	}
}

func TestBriefPushPreference_ClearingTurnsItOff(t *testing.T) {
	env := newBriefPushEnv(t)
	env.put(t, env.Alice, map[string]any{"brief_push": true, "binding_id": env.AliceChat}).Want(http.StatusOK)

	var cleared BriefPushPreference
	env.put(t, env.Alice, map[string]any{"brief_push": false}).Want(http.StatusOK).JSON(&cleared)
	if cleared.BriefPush || cleared.BindingID != nil || cleared.QuietHours != nil {
		t.Fatalf("cleared = %+v, want off with no chat", cleared)
	}
}

// Each member reads and writes only their own row. No request names a member,
// so a second member's save cannot reach the first member's preference.
func TestBriefPushPreference_IsolatedPerMember(t *testing.T) {
	env := newBriefPushEnv(t)
	env.put(t, env.Alice, map[string]any{"brief_push": true, "binding_id": env.AliceChat}).Want(http.StatusOK)

	// Bob names Alice's user id and binding in the body. Neither is read.
	env.put(t, env.Bob, map[string]any{
		"brief_push": false,
		"member_id":  env.Alice,
	}).Want(http.StatusOK)

	var alice BriefPushPreference
	env.get(t, env.Alice).Want(http.StatusOK).JSON(&alice)
	if !alice.BriefPush || alice.BindingID == nil || *alice.BindingID != env.AliceChat {
		t.Fatalf("Alice's preference changed by Bob's save: %+v", alice)
	}
	var bob BriefPushPreference
	env.get(t, env.Bob).Want(http.StatusOK).JSON(&bob)
	if bob.BriefPush || bob.BindingID != nil {
		t.Fatalf("Bob's preference = %+v, want his own default", bob)
	}
}

func TestBriefPushPreference_RefusesAnotherMembersBinding(t *testing.T) {
	env := newBriefPushEnv(t)
	env.put(t, env.Alice, map[string]any{"brief_push": true, "binding_id": env.BobChat}).Want(http.StatusBadRequest)
	env.put(t, env.Alice, map[string]any{"brief_push": false, "binding_id": env.BobChat}).Want(http.StatusBadRequest)
}

func TestBriefPushPreference_RefusesABindingFromAnotherWorkspace(t *testing.T) {
	env := newBriefPushEnv(t)
	other := dbfx.Workspace(t, "Other "+uuid.NewString(), "brief-push-other-"+uuid.NewString())
	foreign := briefPushBinding(t, other, env.Alice, string(briefPushChatApp))

	env.put(t, env.Alice, map[string]any{"brief_push": true, "binding_id": foreign}).Want(http.StatusBadRequest)
}

func TestBriefPushPreference_RefusesAnUnknownBinding(t *testing.T) {
	env := newBriefPushEnv(t)
	env.put(t, env.Alice, map[string]any{"brief_push": true, "binding_id": uuid.NewString()}).Want(http.StatusBadRequest)
	env.put(t, env.Alice, map[string]any{"brief_push": true, "binding_id": "not-a-uuid"}).Want(http.StatusBadRequest)
}

func TestBriefPushPreference_PushNeedsAChat(t *testing.T) {
	env := newBriefPushEnv(t)
	env.put(t, env.Alice, map[string]any{"brief_push": true}).Want(http.StatusBadRequest)
}

func TestBriefPushPreference_RefusesAChatAppWithoutANotifier(t *testing.T) {
	env := newBriefPushEnv(t)
	slack := briefPushBinding(t, env.WorkspaceID, env.Alice, "slack")
	env.put(t, env.Alice, map[string]any{"brief_push": true, "binding_id": slack}).Want(http.StatusBadRequest)
}

func TestBriefPushPreference_QuietHoursValidation(t *testing.T) {
	env := newBriefPushEnv(t)
	for _, qh := range []map[string]any{
		{"start": 22, "end": 22},
		{"start": 24, "end": 7},
		{"start": -1, "end": 7},
		{"start": 22, "end": 7 + 24},
	} {
		env.put(t, env.Alice, map[string]any{
			"brief_push": true, "binding_id": env.AliceChat, "quiet_hours": qh,
		}).Want(http.StatusBadRequest)
	}
}

func TestBriefPushPreference_RefusesAnAgent(t *testing.T) {
	env := newBriefPushEnv(t)
	runtime := dbfx.Runtime(t, "Brief push runtime "+uuid.NewString(), testutil.Cols{"workspace_id": env.WorkspaceID})
	agent := dbfx.Agent(t, "Brief push agent "+uuid.NewString(), runtime, testutil.Cols{"workspace_id": env.WorkspaceID})
	taskID := dbfx.Task(t, agent, testutil.Cols{"status": "running", "runtime_id": runtime})

	req := newRequest("GET", "/api/office/brief-push", nil)
	req.Header.Set("X-Workspace-ID", env.WorkspaceID)
	req = asAgent(asUser(req, env.Alice), agent, taskID)
	testutil.Call(t, testHandler.GetBriefPushPreference, req).Want(http.StatusForbidden)
}

func TestBriefPushPreference_MalformedBody(t *testing.T) {
	env := newBriefPushEnv(t)
	req := newRequest("PUT", "/api/office/brief-push", nil)
	req.Header.Set("X-Workspace-ID", env.WorkspaceID)
	req = asUser(req, env.Alice)
	testutil.Call(t, testHandler.UpdateBriefPushPreference, req).Want(http.StatusBadRequest)
}

// Without a chat app wired in, the preference API refuses every binding rather
// than saving a preference nothing can deliver.
func TestBriefPushPreference_NoServiceRefusesBindings(t *testing.T) {
	env := newBriefPushEnv(t)
	testHandler.BriefPush = nil
	env.put(t, env.Alice, map[string]any{"brief_push": true, "binding_id": env.AliceChat}).Want(http.StatusBadRequest)
}
