package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/dingtalk"
	"github.com/multica-ai/multica/server/internal/integrations/wecom"
)

// registerDingTalkAndWecomNotifiers registers the two notifiers the way the router
// does, under the channel types their installations store.
func registerDingTalkAndWecomNotifiers(t *testing.T) {
	t.Helper()
	testHandler.BriefPush.RegisterNotifier(dingtalk.TypeDingTalk, dingtalk.NewNotifier(testHandler.Queries, nil, nil))
	testHandler.BriefPush.RegisterNotifier(wecom.TypeWecom, wecom.NewNotifier(wecom.NewSendersRegistry()))
}

// A DingTalk or WeCom chat is offered in the list only once its notifier is
// registered, as Lark is.
func TestBriefPushBindings_DingTalkAndWecomAppearOnceTheirNotifiersAreRegistered(t *testing.T) {
	env := newBriefPushEnv(t)
	dingtalkChat := briefPushBinding(t, env.WorkspaceID, env.Alice, string(dingtalk.TypeDingTalk))
	wecomChat := briefPushBinding(t, env.WorkspaceID, env.Alice, string(wecom.TypeWecom))

	var before []BriefPushBinding
	env.listBindings(t, env.Alice).Want(http.StatusOK).JSON(&before)
	for _, b := range before {
		if b.ID == dingtalkChat || b.ID == wecomChat {
			t.Fatalf("a chat listed before its notifier is registered: %+v", before)
		}
	}

	registerDingTalkAndWecomNotifiers(t)

	var after []BriefPushBinding
	env.listBindings(t, env.Alice).Want(http.StatusOK).JSON(&after)
	found := map[string]string{}
	for _, b := range after {
		found[b.ID] = b.Channel
	}
	if found[dingtalkChat] != "dingtalk" {
		t.Fatalf("DingTalk chat missing or mislabelled once registered: %+v", after)
	}
	if found[wecomChat] != "wecom" {
		t.Fatalf("WeCom chat missing or mislabelled once registered: %+v", after)
	}
}

// A member can turn brief push on for a DingTalk or WeCom chat once its notifier
// is registered.
func TestBriefPushPreference_DingTalkAndWecomChatsAreAcceptedOnceRegistered(t *testing.T) {
	env := newBriefPushEnv(t)
	registerDingTalkAndWecomNotifiers(t)
	for _, channelType := range []channel.Type{dingtalk.TypeDingTalk, wecom.TypeWecom} {
		chat := briefPushBinding(t, env.WorkspaceID, env.Alice, string(channelType))
		env.put(t, env.Alice, map[string]any{"brief_push": true, "binding_id": chat}).Want(http.StatusOK)
	}
}
