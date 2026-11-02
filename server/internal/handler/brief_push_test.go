package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
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

// briefPushBinding inserts a chat binding for userID in ws, on an active
// installation of the same chat app.
func briefPushBinding(t *testing.T, ws, userID, channelType string) string {
	t.Helper()
	installation := briefPushInstallation(t, ws, channelType, "active", `{}`)
	return briefPushBindingOn(t, ws, userID, channelType, installation, uuid.NewString(), `{}`)
}

// briefPushInstallation inserts a chat installation in ws with the given status
// and config.
func briefPushInstallation(t *testing.T, ws, channelType, status, config string) string {
	t.Helper()
	return dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id":      ws,
		"agent_id":          uuid.NewString(),
		"channel_type":      channelType,
		"config":            []byte(config),
		"installer_user_id": testUserID,
		"status":            status,
	})
}

// briefPushBindingOn inserts a binding of userID on installationID, with the
// platform user id and config given.
func briefPushBindingOn(t *testing.T, ws, userID, channelType, installationID, channelUserID, config string) string {
	t.Helper()
	return dbfx.Insert(t, "channel_user_binding", testutil.Cols{
		"workspace_id":    ws,
		"multica_user_id": userID,
		"installation_id": installationID,
		"channel_type":    channelType,
		"channel_user_id": channelUserID,
		"config":          []byte(config),
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

// A binding on a revoked installation is refused with a stable code, and the
// preference is not saved.
func TestBriefPushPreference_RefusesABindingOnARevokedInstallation(t *testing.T) {
	env := newBriefPushEnv(t)
	revoked := briefPushInstallation(t, env.WorkspaceID, "telegram", "revoked", `{}`)
	chat := briefPushBindingOn(t, env.WorkspaceID, env.Alice, "telegram", revoked, uuid.NewString(), `{}`)

	res := env.put(t, env.Alice, map[string]any{"brief_push": true, "binding_id": chat}).Want(http.StatusBadRequest)
	if got := res.Map()["code"]; got != "brief_push_chat_inactive" {
		t.Fatalf("code = %v, want brief_push_chat_inactive: %s", got, res.Text())
	}

	var read BriefPushPreference
	env.get(t, env.Alice).Want(http.StatusOK).JSON(&read)
	if read.BriefPush || read.BindingID != nil {
		t.Fatalf("preference saved despite the refusal: %+v", read)
	}
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

func (e briefPushEnv) listBindings(t *testing.T, userID string) *testutil.Response {
	t.Helper()
	req := newRequest("GET", "/api/office/brief-push/bindings", nil)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	req = asUser(req, userID)
	return testutil.Call(t, testHandler.ListBriefPushBindings, req)
}

func TestBriefPushBindings_ListsOnlyTheCallersOwnChats(t *testing.T) {
	env := newBriefPushEnv(t)

	var alice []BriefPushBinding
	env.listBindings(t, env.Alice).Want(http.StatusOK).JSON(&alice)
	if len(alice) != 1 || alice[0].ID != env.AliceChat || alice[0].Channel != "telegram" {
		t.Fatalf("Alice's chats = %+v, want only her Telegram chat %s", alice, env.AliceChat)
	}

	var bob []BriefPushBinding
	env.listBindings(t, env.Bob).Want(http.StatusOK).JSON(&bob)
	if len(bob) != 1 || bob[0].ID != env.BobChat {
		t.Fatalf("Bob's chats = %+v, want only his Telegram chat %s", bob, env.BobChat)
	}
}

func TestBriefPushBindings_ListsOnlyThisWorkspace(t *testing.T) {
	env := newBriefPushEnv(t)
	other := dbfx.Workspace(t, "Other "+uuid.NewString(), "brief-push-list-other-"+uuid.NewString())
	briefPushBinding(t, other, env.Alice, string(briefPushChatApp))

	var alice []BriefPushBinding
	env.listBindings(t, env.Alice).Want(http.StatusOK).JSON(&alice)
	if len(alice) != 1 || alice[0].ID != env.AliceChat {
		t.Fatalf("Alice's chats in this workspace = %+v, want only %s", alice, env.AliceChat)
	}
}

func TestBriefPushBindings_EmptyListIsAnArray(t *testing.T) {
	env := newBriefPushEnv(t)
	carol := dbfx.User(t, "Carol", fmt.Sprintf("brief-push-carol-%s@example.com", uuid.NewString()))
	dbfx.Member(t, env.WorkspaceID, carol, "member")

	res := env.listBindings(t, carol).Want(http.StatusOK)
	if got := res.Text(); got != "[]" && got != "[]\n" {
		t.Fatalf("empty list body = %q, want []", got)
	}
}

func TestBriefPushBindings_LabelFallsBackToThePlatformName(t *testing.T) {
	env := newBriefPushEnv(t)

	var got []BriefPushBinding
	env.listBindings(t, env.Alice).Want(http.StatusOK).JSON(&got)
	if len(got) != 1 || got[0].Label != "telegram" {
		t.Fatalf("label with no stored name = %+v, want the platform name telegram", got)
	}
}

func TestBriefPushBindings_LabelUsesTheStoredDisplayNameThenUsername(t *testing.T) {
	env := newBriefPushEnv(t)
	dan := dbfx.User(t, "Dan", fmt.Sprintf("brief-push-dan-%s@example.com", uuid.NewString()))
	dbfx.Member(t, env.WorkspaceID, dan, "member")
	installation := briefPushInstallation(t, env.WorkspaceID, "telegram", "active", `{}`)
	named := briefPushBindingOn(t, env.WorkspaceID, dan, "telegram", installation, uuid.NewString(), `{"display_name":"Dan Chat"}`)
	handle := briefPushBindingOn(t, env.WorkspaceID, dan, "telegram", briefPushInstallation(t, env.WorkspaceID, "telegram", "active", `{}`), uuid.NewString(), `{"username":"dan_handle"}`)

	var got []BriefPushBinding
	env.listBindings(t, dan).Want(http.StatusOK).JSON(&got)
	labels := map[string]string{}
	for _, b := range got {
		labels[b.ID] = b.Label
	}
	if labels[named] != "Dan Chat" || labels[handle] != "dan_handle" {
		t.Fatalf("labels = %+v, want display name Dan Chat and username dan_handle", labels)
	}
}

// The list carries no secret and no platform identifier: the bot token in the
// installation config and the Telegram user id stay on the server.
func TestBriefPushBindings_ResponseOmitsSecretsAndPlatformIDs(t *testing.T) {
	env := newBriefPushEnv(t)
	const token = "telegram-bot-secret-token-7788"
	const platformUserID = "5550001234"
	installation := briefPushInstallation(t, env.WorkspaceID, "telegram", "active",
		`{"app_id":"9001","bot_token":"`+token+`"}`)
	binding := briefPushBindingOn(t, env.WorkspaceID, env.Alice, "telegram", installation, platformUserID, `{}`)

	res := env.listBindings(t, env.Alice).Want(http.StatusOK)
	for _, leak := range []string{token, platformUserID, installation, "bot_token"} {
		if strings.Contains(res.Text(), leak) {
			t.Fatalf("list leaks %q: %s", leak, res.Text())
		}
	}
	var got []BriefPushBinding
	res.JSON(&got)
	found := false
	for _, b := range got {
		found = found || b.ID == binding
	}
	if !found {
		t.Fatalf("list = %+v, want the new binding %s", got, binding)
	}
}

func TestBriefPushBindings_HidesInactiveInstallations(t *testing.T) {
	env := newBriefPushEnv(t)
	revoked := briefPushInstallation(t, env.WorkspaceID, "telegram", "revoked", `{}`)
	briefPushBindingOn(t, env.WorkspaceID, env.Alice, "telegram", revoked, uuid.NewString(), `{}`)

	var got []BriefPushBinding
	env.listBindings(t, env.Alice).Want(http.StatusOK).JSON(&got)
	if len(got) != 1 || got[0].ID != env.AliceChat {
		t.Fatalf("chats = %+v, want only the active Telegram chat %s", got, env.AliceChat)
	}
}

// A chat app with no notifier cannot take a brief, so its bindings are not offered.
func TestBriefPushBindings_HidesChatAppsWithoutANotifier(t *testing.T) {
	env := newBriefPushEnv(t)
	briefPushBinding(t, env.WorkspaceID, env.Alice, "slack")

	var got []BriefPushBinding
	env.listBindings(t, env.Alice).Want(http.StatusOK).JSON(&got)
	if len(got) != 1 || got[0].Channel != "telegram" {
		t.Fatalf("chats = %+v, want only the Telegram chat", got)
	}
}

func TestBriefPushBindings_NoServiceListsNothing(t *testing.T) {
	env := newBriefPushEnv(t)
	testHandler.BriefPush = nil

	res := env.listBindings(t, env.Alice).Want(http.StatusOK)
	if got := strings.TrimSpace(res.Text()); got != "[]" {
		t.Fatalf("list without a chat app = %s, want []", got)
	}
}
