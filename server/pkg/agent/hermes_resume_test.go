package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResolveHermesResumedSessionIDPrefersTopLevelID(t *testing.T) {
	t.Parallel()

	id, landed := resolveHermesResumedSessionID("ses_old", json.RawMessage(`{"sessionId":"ses_old"}`))
	if id != "ses_old" || !landed {
		t.Fatalf("got (%q, %v), want (ses_old, true)", id, landed)
	}
}

func TestResolveHermesResumedSessionIDReadsProvenanceMeta(t *testing.T) {
	t.Parallel()

	// Hermes' real session/resume shape: no top-level sessionId (the ACP
	// ResumeSessionResponse schema has no such field), and the session it
	// actually served the turn with reported under _meta.
	response := json.RawMessage(`{
		"_meta":{"hermes":{"sessionProvenance":{
			"acpSessionId":"ses_new",
			"currentHermesSessionId":"ses_new",
			"sessionKind":"root"
		}}},
		"models":{"currentModelId":"openai:gpt-5"}
	}`)

	id, landed := resolveHermesResumedSessionID("ses_old", response)
	if id != "ses_new" {
		t.Fatalf("session id = %q, want ses_new (the session hermes silently rebound to)", id)
	}
	if landed {
		t.Fatal("landed = true, want false: the requested session was not the one resumed")
	}
}

func TestResolveHermesResumedSessionIDProvenanceMatchingIsALanding(t *testing.T) {
	t.Parallel()

	response := json.RawMessage(`{"_meta":{"hermes":{"sessionProvenance":{"acpSessionId":"ses_old"}}}}`)

	id, landed := resolveHermesResumedSessionID("ses_old", response)
	if id != "ses_old" || !landed {
		t.Fatalf("got (%q, %v), want (ses_old, true)", id, landed)
	}
}

func TestResolveHermesResumedSessionIDSilentResponseKeepsRequested(t *testing.T) {
	t.Parallel()

	// A runtime that reports neither shape (an older Hermes, or a custom ACP
	// server) must not be read as "history lost" on every turn — that would
	// staple a continuity notice onto every single turn it ever runs. Keep the
	// requested id and assume the resume landed.
	//
	// This is the one case the backend genuinely cannot resolve, and it is why
	// the daemon-side gate matters: sessionHomeReachable stops a dead id from
	// reaching a store with no transcript in the first place, so this fallback
	// only has to cover a store that DOES hold history and a runtime that will
	// not say which session it used.
	for _, response := range []string{`{}`, `{"models":{}}`, `not json`} {
		id, landed := resolveHermesResumedSessionID("ses_old", json.RawMessage(response))
		if id != "ses_old" || !landed {
			t.Fatalf("response %q: got (%q, %v), want (ses_old, true)", response, id, landed)
		}
	}
}

func TestHermesTurnTextPrependsNoticeOnlyWhenResumeMissed(t *testing.T) {
	t.Parallel()

	const notice = "NOTICE: prior conversation unavailable.\n\n"
	tests := []struct {
		name           string
		resumeExpected bool
		resumeLanded   bool
		want           string
	}{
		{name: "resume landed", resumeExpected: true, resumeLanded: true, want: "do the thing"},
		{name: "resume missed", resumeExpected: true, resumeLanded: false, want: notice + "do the thing"},
		{name: "no resume expected", resumeExpected: false, resumeLanded: false, want: "do the thing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hermesTurnText("do the thing", tt.resumeExpected, tt.resumeLanded, notice); got != tt.want {
				t.Fatalf("hermesTurnText() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestHermesBackendSurfacesSilentSessionRebind is the end-to-end shape of
// GH #6806: hermes answers session/resume for a session it no longer holds by
// creating a fresh one and replying with an ordinary success frame. The turn
// must run under the NEW id and carry the continuity notice, instead of
// reporting the dead id back as if the conversation had continued.
func TestHermesBackendSurfacesSilentSessionRebind(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	tests := []struct {
		name          string
		servedID      string
		wantSessionID string
		wantNotice    bool
	}{
		{
			name:          "rebound session surfaces the loss",
			servedID:      "ses_new",
			wantSessionID: "ses_new",
			wantNotice:    true,
		},
		{
			name:          "real resume says nothing",
			servedID:      "ses_old",
			wantSessionID: "ses_old",
			wantNotice:    false,
		},
	}

	const notice = "CONTINUITY NOTICE: the prior session is gone.\n\n"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			fakePath := filepath.Join(tempDir, "hermes")
			capturePath := filepath.Join(tempDir, "prompt.json")
			script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"_meta":{"hermes":{"sessionProvenance":{"acpSessionId":"'"$HERMES_TEST_SERVED_ID"'"}}}}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '%s' "$line" > "$HERMES_TEST_CAPTURE"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
			writeTestExecutable(t, fakePath, []byte(script))

			backend, err := New("hermes", Config{
				ExecutablePath: fakePath,
				Logger:         slog.Default(),
				Env: map[string]string{
					"HERMES_TEST_CAPTURE":   capturePath,
					"HERMES_TEST_SERVED_ID": tt.servedID,
				},
			})
			if err != nil {
				t.Fatalf("new hermes backend: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "do the thing", ExecOptions{
				ResumeSessionID:        "ses_old",
				ResumeExpected:         true,
				ResumeContinuityNotice: notice,
				Timeout:                10 * time.Second,
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			for range session.Messages {
			}

			select {
			case result := <-session.Result:
				if result.SessionID != tt.wantSessionID {
					t.Fatalf("result.SessionID = %q, want %q", result.SessionID, tt.wantSessionID)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("timeout waiting for terminal result")
			}

			captured, err := os.ReadFile(capturePath)
			if err != nil {
				t.Fatalf("read captured prompt: %v", err)
			}
			var frame struct {
				Params struct {
					Prompt []struct {
						Text string `json:"text"`
					} `json:"prompt"`
				} `json:"params"`
			}
			if err := json.Unmarshal(captured, &frame); err != nil {
				t.Fatalf("parse captured prompt %q: %v", captured, err)
			}
			if len(frame.Params.Prompt) != 1 {
				t.Fatalf("prompt blocks = %d, want 1", len(frame.Params.Prompt))
			}
			text := frame.Params.Prompt[0].Text
			if gotNotice := strings.HasPrefix(text, notice); gotNotice != tt.wantNotice {
				t.Fatalf("prompt carries notice = %v, want %v (prompt %q)", gotNotice, tt.wantNotice, text)
			}
			if !strings.HasSuffix(text, "do the thing") {
				t.Fatalf("prompt %q lost the original task text", text)
			}
		})
	}
}
