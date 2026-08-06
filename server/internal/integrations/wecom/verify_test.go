package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestVerifyCredentialsAcceptsGoodCredentials also pins that the probe sends
// the submitted bot id / secret verbatim — a probe that authenticated with
// anything else would report "valid" for credentials the supervisor then
// cannot connect with.
func TestVerifyCredentialsAcceptsGoodCredentials(t *testing.T) {
	type gotBody struct {
		botID  string
		secret string
	}
	received := make(chan gotBody, 1)
	fs := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		f, ok := readFrame(t, c)
		if !ok {
			return
		}
		if f.Cmd != CmdSubscribe {
			t.Errorf("first frame cmd = %q, want %q", f.Cmd, CmdSubscribe)
			return
		}
		var body SubscribeBody
		if err := json.Unmarshal(f.Body, &body); err != nil {
			t.Errorf("decode subscribe body: %v", err)
			return
		}
		received <- gotBody{botID: body.BotID, secret: body.Secret}
		writeResponse(t, c, f.Headers.ReqID, 0, "")
	})

	err := VerifyCredentials(context.Background(), VerifyCredentialsConfig{
		DialURL: fs.url,
		BotID:   "bot-1",
		Secret:  "sec-1",
	})
	if err != nil {
		t.Fatalf("VerifyCredentials: %v", err)
	}
	select {
	case got := <-received:
		if got.botID != "bot-1" || got.secret != "sec-1" {
			t.Fatalf("subscribe body = %+v, want bot-1/sec-1", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never received a subscribe frame")
	}
}

// TestVerifyCredentialsRejectsBadCredentials is the whole point of the probe:
// a non-zero errcode must come back as *AuthFailedError so the caller can say
// "wrong secret" instead of "WeCom unreachable".
func TestVerifyCredentialsRejectsBadCredentials(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		reqID, ok := awaitSubscribe(t, c)
		if !ok {
			return
		}
		writeResponse(t, c, reqID, 40001, "invalid secret")
	})

	err := VerifyCredentials(context.Background(), VerifyCredentialsConfig{
		DialURL: fs.url,
		BotID:   "bot-1",
		Secret:  "wrong",
	})
	var authErr *AuthFailedError
	if !errors.As(err, &authErr) {
		t.Fatalf("err = %v, want *AuthFailedError", err)
	}
	if authErr.Code != 40001 {
		t.Fatalf("errcode = %d, want 40001", authErr.Code)
	}
}

// TestVerifyCredentialsUnreachableIsNotAnAuthFailure: an endpoint we could not
// reach says nothing about the credentials. Classifying it as auth failure
// would tell a user their secret is wrong during a network blip.
func TestVerifyCredentialsUnreachableIsNotAnAuthFailure(t *testing.T) {
	err := VerifyCredentials(context.Background(), VerifyCredentialsConfig{
		// Port 1 on loopback refuses connections.
		DialURL:          "ws://127.0.0.1:1",
		BotID:            "bot-1",
		Secret:           "sec-1",
		SubscribeTimeout: time.Second,
	})
	if err == nil {
		t.Fatal("expected a dial error")
	}
	var authErr *AuthFailedError
	if errors.As(err, &authErr) {
		t.Fatalf("dial failure was classified as auth failure: %v", err)
	}
	if !strings.Contains(err.Error(), "dial") {
		t.Fatalf("err = %v, want a dial error", err)
	}
}

// TestVerifyCredentialsSilentServerTimesOut keeps the probe bounded: a server
// that accepts the socket and never answers must not hold the HTTP request open.
func TestVerifyCredentialsSilentServerTimesOut(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		if _, ok := awaitSubscribe(t, c); !ok {
			return
		}
		// Deliberately never respond; the probe's read deadline must fire.
		<-time.After(3 * time.Second)
	})

	start := time.Now()
	err := VerifyCredentials(context.Background(), VerifyCredentialsConfig{
		DialURL:          fs.url,
		BotID:            "bot-1",
		Secret:           "sec-1",
		SubscribeTimeout: 300 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	var authErr *AuthFailedError
	if errors.As(err, &authErr) {
		t.Fatalf("timeout was classified as auth failure: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("probe took %s, want it bounded by SubscribeTimeout", elapsed)
	}
}

func TestVerifyCredentialsRequiresBotIDAndSecret(t *testing.T) {
	if err := VerifyCredentials(context.Background(), VerifyCredentialsConfig{Secret: "s"}); err == nil {
		t.Fatal("expected an error for a missing bot id")
	}
	if err := VerifyCredentials(context.Background(), VerifyCredentialsConfig{BotID: "b"}); err == nil {
		t.Fatal("expected an error for a missing secret")
	}
}
