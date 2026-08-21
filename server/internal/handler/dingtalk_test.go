package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	dingtalkintegration "github.com/multica-ai/multica/server/internal/integrations/dingtalk"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestPublishDingTalkInstallationCreated(t *testing.T) {
	bus := events.New()
	h := &Handler{Bus: bus}

	const (
		wsID   = "11111111-1111-1111-1111-111111111111"
		instID = "22222222-2222-2222-2222-222222222222"
	)

	var got events.Event
	fired := 0
	bus.Subscribe(protocol.EventDingTalkInstallationCreated, func(e events.Event) {
		got = e
		fired++
	})

	h.publishDingTalkInstallationCreated(db.ChannelInstallation{
		ID:          parseUUID(instID),
		WorkspaceID: parseUUID(wsID),
	}, "user-1")

	if fired != 1 {
		t.Fatalf("expected dingtalk_installation:created published once, got %d", fired)
	}
	if got.WorkspaceID != wsID || got.ActorType != "user" || got.ActorID != "user-1" {
		t.Errorf("event envelope = %+v", got)
	}
	payload, ok := got.Payload.(map[string]any)
	if !ok || payload["id"] != instID {
		t.Errorf("payload = %v, want installation id %s", got.Payload, instID)
	}
}

func TestRedeemDingTalkBindingTokenPublishesAccountBindingUpdateAfterCommit(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newDingTalkGroupRouteHandlerFixture(t)
	bindingService := dingtalkintegration.NewBindingTokenService(testHandler.Queries, testPool)
	token, err := bindingService.Mint(
		context.Background(),
		parseUUID(testWorkspaceID),
		parseUUID(fx.installationID),
		"staff-binding-event",
	)
	if err != nil {
		t.Fatalf("mint DingTalk binding token: %v", err)
	}

	bus := events.New()
	h := &Handler{
		Bus:                   bus,
		DingTalkBindingTokens: bindingService,
	}

	var (
		published      events.Event
		eventCount     int
		bindingAtEvent db.ChannelUserBinding
		queryErr       error
	)
	bus.Subscribe(protocol.EventDingTalkAccountBindingUpdated, func(event events.Event) {
		eventCount++
		published = event
		// The synchronous subscriber reads through a separate pool connection,
		// proving the binding transaction committed before the event fired.
		bindingAtEvent, queryErr = testHandler.Queries.GetChannelUserBindingByUserID(
			context.Background(),
			db.GetChannelUserBindingByUserIDParams{
				InstallationID: parseUUID(fx.installationID),
				ChannelUserID:  "staff-binding-event",
			},
		)
	})

	recorder := httptest.NewRecorder()
	h.RedeemDingTalkBindingToken(recorder, newRequest(
		http.MethodPost,
		"/api/dingtalk/binding/redeem",
		RedeemDingTalkBindingTokenRequest{Token: token.Raw},
	))

	if recorder.Code != http.StatusOK {
		t.Fatalf("redeem status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if eventCount != 1 {
		t.Fatalf("binding update event count = %d, want 1", eventCount)
	}
	if queryErr != nil {
		t.Fatalf("binding was not visible when event fired: %v", queryErr)
	}
	if bindingAtEvent.MulticaUserID != parseUUID(testUserID) {
		t.Errorf("binding user = %v, want %s", bindingAtEvent.MulticaUserID, testUserID)
	}
	if published.WorkspaceID != testWorkspaceID || published.ActorType != "user" || published.ActorID != testUserID {
		t.Errorf("event envelope = %+v", published)
	}
	payload, ok := published.Payload.(map[string]any)
	if !ok || payload["id"] != fx.installationID {
		t.Errorf("payload = %v, want installation id %s", published.Payload, fx.installationID)
	}
}
