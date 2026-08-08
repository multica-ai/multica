package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeLoginCodePusher struct {
	enabled   bool
	err       error
	calls     int
	userKey   string
	code      string
	testCalls int
}

func (p *fakeLoginCodePusher) Enabled() bool { return p.enabled }

func (p *fakeLoginCodePusher) SendLoginCode(_ context.Context, userKey, code string) error {
	p.calls++
	p.userKey = userKey
	p.code = code
	return p.err
}

func (p *fakeLoginCodePusher) SendTestNotification(_ context.Context, userKey string) error {
	p.testCalls++
	p.userKey = userKey
	return p.err
}

func pushoverLoginUser() db.User {
	return db.User{
		PushoverUserKey:           pgtype.Text{String: "ZYXWVUTSRQPONMLKJIHGFEDCBA4321", Valid: true},
		PushoverLoginCodesEnabled: true,
	}
}

func TestDeliverLoginCodeUsesEmailAndPushover(t *testing.T) {
	emailCalls := 0
	pusher := &fakeLoginCodePusher{enabled: true}
	err := deliverLoginCode(
		context.Background(),
		"ada@example.com",
		"123456",
		pushoverLoginUser(),
		true,
		func(to, code string) error {
			emailCalls++
			if to != "ada@example.com" || code != "123456" {
				t.Fatalf("email delivery = %q, %q", to, code)
			}
			return nil
		},
		pusher,
	)
	if err != nil {
		t.Fatalf("deliverLoginCode: %v", err)
	}
	if emailCalls != 1 || pusher.calls != 1 {
		t.Fatalf("email calls = %d, Pushover calls = %d", emailCalls, pusher.calls)
	}
	if pusher.userKey != "ZYXWVUTSRQPONMLKJIHGFEDCBA4321" || pusher.code != "123456" {
		t.Fatalf("Pushover delivery = %q, %q", pusher.userKey, pusher.code)
	}
}

func TestDeliverLoginCodeSucceedsWhenEitherChannelSucceeds(t *testing.T) {
	t.Run("Pushover succeeds during email outage", func(t *testing.T) {
		pusher := &fakeLoginCodePusher{enabled: true}
		err := deliverLoginCode(
			context.Background(), "ada@example.com", "123456", pushoverLoginUser(), true,
			func(string, string) error { return errors.New("email unavailable") }, pusher,
		)
		if err != nil {
			t.Fatalf("deliverLoginCode: %v", err)
		}
	})

	t.Run("email succeeds during Pushover outage", func(t *testing.T) {
		pusher := &fakeLoginCodePusher{enabled: true, err: errors.New("Pushover unavailable")}
		err := deliverLoginCode(
			context.Background(), "ada@example.com", "123456", pushoverLoginUser(), true,
			func(string, string) error { return nil }, pusher,
		)
		if err != nil {
			t.Fatalf("deliverLoginCode: %v", err)
		}
	})
}

func TestDeliverLoginCodeFailsWhenEveryAttemptedChannelFails(t *testing.T) {
	pusher := &fakeLoginCodePusher{enabled: true, err: errors.New("Pushover unavailable")}
	err := deliverLoginCode(
		context.Background(), "ada@example.com", "123456", pushoverLoginUser(), true,
		func(string, string) error { return errors.New("email unavailable") }, pusher,
	)
	if err == nil {
		t.Fatal("deliverLoginCode: expected an error")
	}
}

func TestDeliverLoginCodeSkipsPushoverWithoutOptIn(t *testing.T) {
	user := pushoverLoginUser()
	user.PushoverLoginCodesEnabled = false
	pusher := &fakeLoginCodePusher{enabled: true}
	err := deliverLoginCode(
		context.Background(), "ada@example.com", "123456", user, true,
		func(string, string) error { return nil }, pusher,
	)
	if err != nil {
		t.Fatalf("deliverLoginCode: %v", err)
	}
	if pusher.calls != 0 {
		t.Fatalf("Pushover calls = %d, want 0", pusher.calls)
	}
}

func TestSendMyPushoverTestNotification(t *testing.T) {
	const userKey = "ZYXWVUTSRQPONMLKJIHGFEDCBA4321"
	if _, err := testPool.Exec(
		context.Background(),
		`UPDATE "user" SET pushover_user_key = $1 WHERE id = $2`,
		userKey,
		testUserID,
	); err != nil {
		t.Fatalf("configure Pushover user key: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(
			context.Background(),
			`UPDATE "user" SET pushover_user_key = NULL, pushover_login_codes_enabled = FALSE WHERE id = $1`,
			testUserID,
		)
	})

	original := testHandler.PushoverService
	pusher := &fakeLoginCodePusher{enabled: true}
	testHandler.PushoverService = pusher
	t.Cleanup(func() { testHandler.PushoverService = original })

	recorder := httptest.NewRecorder()
	testHandler.SendMyPushoverTestNotification(
		recorder,
		newRequestAs(testUserID, http.MethodPost, "/api/me/pushover/test", nil),
	)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if pusher.testCalls != 1 || pusher.userKey != userKey {
		t.Fatalf("test notification calls = %d, user key = %q", pusher.testCalls, pusher.userKey)
	}
}
