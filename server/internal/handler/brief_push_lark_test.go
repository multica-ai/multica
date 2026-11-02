package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/lark"
)

// registerLarkNotifier registers the Lark notifier the way the router does, under
// the channel type its installations store: feishu.
func registerLarkNotifier(t *testing.T) {
	t.Helper()
	testHandler.BriefPush.RegisterNotifier(channel.TypeFeishu, lark.NewNotifier(testHandler.Queries, nil, nil))
}

// A Lark chat is offered in the list only once its notifier is registered, under
// the channel name its installation stores.
func TestBriefPushBindings_LarkAppearsOnceItsNotifierIsRegistered(t *testing.T) {
	env := newBriefPushEnv(t)
	chat := briefPushBinding(t, env.WorkspaceID, env.Alice, string(channel.TypeFeishu))

	var before []BriefPushBinding
	env.listBindings(t, env.Alice).Want(http.StatusOK).JSON(&before)
	for _, b := range before {
		if b.ID == chat {
			t.Fatalf("Lark chat listed before its notifier is registered: %+v", before)
		}
	}

	registerLarkNotifier(t)

	var after []BriefPushBinding
	env.listBindings(t, env.Alice).Want(http.StatusOK).JSON(&after)
	for _, b := range after {
		if b.ID == chat {
			if b.Channel != "feishu" {
				t.Fatalf("channel = %q, want feishu", b.Channel)
			}
			return
		}
	}
	t.Fatalf("Lark chat %s missing once its notifier is registered: %+v", chat, after)
}

// A Lark chat with a stored display name is labelled by that name, as Slack and
// Telegram chats are.
func TestBriefPushBindings_LarkLabelUsesTheStoredDisplayNameThenThePlatform(t *testing.T) {
	env := newBriefPushEnv(t)
	registerLarkNotifier(t)
	chat := briefPushBindingOn(t, env.WorkspaceID, env.Alice, string(channel.TypeFeishu),
		briefPushInstallation(t, env.WorkspaceID, string(channel.TypeFeishu), "active", `{}`),
		"ou_brief_label", `{"display_name":"Alice on Lark"}`)

	var list []BriefPushBinding
	env.listBindings(t, env.Alice).Want(http.StatusOK).JSON(&list)
	for _, b := range list {
		if b.ID == chat {
			if b.Label != "Alice on Lark" {
				t.Fatalf("label = %q, want the stored display name", b.Label)
			}
			return
		}
	}
	t.Fatalf("Lark chat %s missing from the list: %+v", chat, list)
}

// A member can turn brief push on for a Lark chat once its notifier is registered.
func TestBriefPushPreference_LarkChatIsAcceptedOnceRegistered(t *testing.T) {
	env := newBriefPushEnv(t)
	registerLarkNotifier(t)
	chat := briefPushBinding(t, env.WorkspaceID, env.Alice, string(channel.TypeFeishu))

	env.put(t, env.Alice, map[string]any{"brief_push": true, "binding_id": chat}).Want(http.StatusOK)
}
