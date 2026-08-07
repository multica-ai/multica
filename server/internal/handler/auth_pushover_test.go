package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeLoginCodePusher struct {
	enabled bool
	err     error
	calls   int
	userKey string
	code    string
}

func (p *fakeLoginCodePusher) Enabled() bool { return p.enabled }

func (p *fakeLoginCodePusher) SendLoginCode(_ context.Context, userKey, code string) error {
	p.calls++
	p.userKey = userKey
	p.code = code
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
