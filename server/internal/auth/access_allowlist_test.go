package auth

import (
	"context"
	"errors"
	"testing"
)

func TestParseAccessAllowlist(t *testing.T) {
	empty, err := ParseAccessAllowlist(" , ")
	if err != nil || empty != nil {
		t.Fatalf("blank list must disable the gate, got %v, %v", empty, err)
	}
	for _, bad := range []string{"devv.ai", "@devv.ai", "*@", "a*@devv.ai", "*@*.devv.ai", "a@b@c.com", "*"} {
		if _, err := ParseAccessAllowlist("ok@devv.ai," + bad); err == nil {
			t.Errorf("expected %q to be rejected", bad)
		}
	}
}

func TestAccessAllowlistAllowsEmail(t *testing.T) {
	a, err := ParseAccessAllowlist(" *@Devv.ai , xxx@google.com ")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		"alice@devv.ai":      true,
		" BOB@DEVV.AI ":      true,
		"xxx@google.com":     true,
		"XXX@Google.com":     true,
		"yyy@google.com":     false,
		"alice@sub.devv.ai":  false,
		"alice@devv.ai.evil": false,
		"devv.ai":            false,
		"":                   false,
	}
	for email, want := range cases {
		if got := a.AllowsEmail(email); got != want {
			t.Errorf("AllowsEmail(%q) = %t, want %t", email, got, want)
		}
	}
	var none *AccessAllowlist
	if !none.AllowsEmail("anyone@anywhere.com") {
		t.Fatal("a nil allowlist must allow everyone")
	}
}

func TestCheckUserAccess(t *testing.T) {
	t.Cleanup(func() { SetAccessAllowlist(nil, nil) })

	if err := CheckUserAccess(context.Background(), "u1", ""); err != nil {
		t.Fatalf("no allowlist must allow everyone, got %v", err)
	}

	a, err := ParseAccessAllowlist("*@devv.ai")
	if err != nil {
		t.Fatal(err)
	}
	lookups := map[string]int{}
	lookupErr := errors.New("db down")
	failing := true
	SetAccessAllowlist(a, func(_ context.Context, userID string) (string, error) {
		lookups[userID]++
		switch userID {
		case "flaky":
			if failing {
				return "", lookupErr
			}
			return "ok@devv.ai", nil
		case "in":
			return "member@devv.ai", nil
		case "out":
			return "member@other.com", nil
		}
		return "", nil
	})
	ctx := context.Background()

	if err := CheckUserAccess(ctx, "jwt-user", "member@devv.ai"); err != nil {
		t.Fatalf("email claim in list must pass, got %v", err)
	}
	if err := CheckUserAccess(ctx, "jwt-user", "member@other.com"); !errors.Is(err, ErrAccessNotAllowed) {
		t.Fatalf("email claim outside list must be denied, got %v", err)
	}
	if lookups["jwt-user"] != 0 {
		t.Fatal("a credential carrying an email must not trigger a lookup")
	}

	for range 2 {
		if err := CheckUserAccess(ctx, "in", ""); err != nil {
			t.Fatalf("listed user must pass, got %v", err)
		}
		if err := CheckUserAccess(ctx, "out", ""); !errors.Is(err, ErrAccessNotAllowed) {
			t.Fatalf("unlisted user must be denied, got %v", err)
		}
	}
	if lookups["in"] != 1 || lookups["out"] != 1 {
		t.Fatalf("decisions must be cached per user, lookups=%v", lookups)
	}
	if err := CheckUserAccess(ctx, "deleted", ""); !errors.Is(err, ErrAccessNotAllowed) {
		t.Fatalf("missing user must be denied, got %v", err)
	}

	if err := CheckUserAccess(ctx, "flaky", ""); !errors.Is(err, lookupErr) {
		t.Fatalf("lookup failure must surface, got %v", err)
	}
	failing = false
	if err := CheckUserAccess(ctx, "flaky", ""); err != nil {
		t.Fatalf("lookup failures must not be cached, got %v", err)
	}
}
