package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Replays the reported Devin CLI v3000.10.21 wire contract without account access.
const fakeDevinContractScript = `#!/bin/sh
if [ "$1" = models ]; then
  printf '%s\n' "${DEVIN_TEST_CATALOG:-null}"
  exit 0
fi
while IFS= read -r line; do
  if [ -n "$DEVIN_REQUESTS_FILE" ]; then printf '%s\n' "$line" >> "$DEVIN_REQUESTS_FILE"; fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  method=$(printf '%s' "$line" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
  if [ "$method" = "$DEVIN_TEST_FAIL_RPC" ]; then
    printf '{"jsonrpc":"2.0","id":%s,"error":{"code":%s,"message":"%s"}}\n' "$id" "${DEVIN_TEST_ERROR_CODE:--32016}" "${DEVIN_TEST_ERROR_MESSAGE:-Session not found}"
    if [ "$DEVIN_TEST_IGNORE_EOF" != 1 ]; then exit 0; fi
    continue
  fi
  case "$method" in
    initialize)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentInfo":{"name":"affogato","version":"0.0.0-dev"},"agentCapabilities":{"loadSession":%s,"mcpCapabilities":{"http":false,"sse":false}},"authMethods":%s}}\n' "$id" "${DEVIN_TEST_LOAD_SUPPORTED:-true}" "${DEVIN_TEST_AUTH_METHODS:-[]}"
      ;;
    authenticate)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    session/new|session/load|session/set_config_option)
      model=${DEVIN_TEST_INITIAL_MODEL:-default-model}
      if [ "$method" = session/set_config_option ] && [ "$DEVIN_TEST_APPLY_MODEL" = 1 ]; then model=selected-model; fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"devin-session","configOptions":[{"id":"model","category":"model","name":"Model","type":"select","currentValue":"%s","options":[{"value":"default-model","name":"Default"},{"value":"selected-model","name":"Selected"}]}]}}\n' "$id" "$model"
      ;;
    session/prompt)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"devin-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"pong"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"totalTokens":10,"inputTokens":7,"outputTokens":3}}}\n' "$id"
      exit 0
      ;;
  esac
done
if [ "$DEVIN_TEST_IGNORE_EOF" = 1 ]; then while true; do sleep 1; done; fi
`

func TestDevinStoredCredentialsDoNotRequireAuthenticate(t *testing.T) {
	for _, method := range []string{"windsurf-api-key", "future-auth", "cached-login", "devin-browser"} {
		t.Run(method, func(t *testing.T) {
			result, _, requests := runDevinContract(t, ExecOptions{}, map[string]string{
				"WINDSURF_API_KEY": "", "DEVIN_TEST_AUTH_METHODS": fmt.Sprintf(`[{"id":%q}]`, method),
			})
			if result.Status != "completed" || strings.Contains(requests, `"method":"authenticate"`) {
				t.Fatalf("stored credential path was intercepted: result=%+v requests=%s", result, requests)
			}
		})
	}
}

func TestDevinExplicitKeyAuthenticatesOnlyWhenAdvertised(t *testing.T) {
	for _, method := range []string{"windsurf-api-key", "future-auth"} {
		t.Run(method, func(t *testing.T) {
			result, _, requests := runDevinContract(t, ExecOptions{}, map[string]string{
				"WINDSURF_API_KEY": "fixture-key", "DEVIN_TEST_AUTH_METHODS": fmt.Sprintf(`[{"id":%q}]`, method),
			})
			if result.Status != "completed" || strings.Contains(requests, `"method":"authenticate"`) != (method == "windsurf-api-key") {
				t.Fatalf("unexpected auth selection: result=%+v requests=%s", result, requests)
			}
			if strings.Contains(requests, "fixture-key") {
				t.Fatal("API key was copied into the RPC transcript")
			}
		})
	}
}

func TestDevinMissingCredentialsSurfaceSessionError(t *testing.T) {
	result, _, requests := runDevinContract(t, ExecOptions{}, map[string]string{
		"WINDSURF_API_KEY": "", "DEVIN_TEST_AUTH_METHODS": `[{"id":"windsurf-api-key"}]`,
		"DEVIN_TEST_FAIL_RPC": "session/new", "DEVIN_TEST_ERROR_MESSAGE": "Authentication required: run devin auth login",
	})
	if result.Status != "failed" || result.ResumeRejected || result.SessionID != "" ||
		!strings.Contains(result.Error, "run devin auth login") || strings.Contains(requests, `"method":"session/prompt"`) {
		t.Fatalf("real session auth error was lost: result=%+v requests=%s", result, requests)
	}
}

func TestDevinAliasResolvedByCLIUsesCanonicalUsage(t *testing.T) {
	// The launch flag is `opus`, but ACP returns a canonical value, not the flag.
	result, _, requests := runDevinContract(t, ExecOptions{Model: "opus"}, map[string]string{
		"DEVIN_TEST_INITIAL_MODEL": "selected-model",
		"DEVIN_TEST_CATALOG":       `{"families":[{"slug":"claude-opus-5","aliases":["opus"],"variants":[{"model_uid":"selected-model"}]}]}`,
	})
	if result.Status != "completed" || result.Usage["selected-model"].InputTokens != 7 ||
		strings.Contains(requests, `"method":"session/set_config_option"`) {
		t.Fatalf("resolved alias was rejected or misattributed: result=%+v requests=%s", result, requests)
	}
}

func TestDevinResumedAliasMustBeAppliedBeforePrompt(t *testing.T) {
	for _, applied := range []bool{false, true} {
		t.Run(fmt.Sprintf("applied=%v", applied), func(t *testing.T) {
			env := map[string]string{
				"DEVIN_TEST_CATALOG": `{"families":[{"slug":"claude-opus-5","aliases":["opus"],"variants":[{"model_uid":"selected-model"}]}]}`,
			}
			if applied {
				env["DEVIN_TEST_APPLY_MODEL"] = "1"
			}
			result, _, requests := runDevinContract(t, ExecOptions{Model: "opus", ResumeSessionID: "old-session"}, env)
			if strings.Contains(requests, `"method":"session/prompt"`) != applied ||
				!strings.Contains(requests, `"value":"selected-model"`) {
				t.Fatalf("alias was not applied canonically: result=%+v requests=%s", result, requests)
			}
			if applied {
				if result.Status != "completed" || result.Usage["selected-model"].InputTokens != 7 {
					t.Fatalf("alias usage was not canonical: %+v", result)
				}
			} else if result.Status != "failed" || result.ResumeRejected || len(result.Usage) != 0 {
				t.Fatalf("silent model fallback was accepted: %+v", result)
			}
		})
	}
}

func runDevinContract(t *testing.T, opts ExecOptions, env map[string]string) (Result, []Message, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake CLI; native Windows lifecycle is covered by the shared process ownership tests")
	}
	if env == nil {
		env = make(map[string]string)
	}
	requestsFile := filepath.Join(t.TempDir(), "requests.jsonl")
	env["DEVIN_REQUESTS_FILE"] = requestsFile
	fakePath := filepath.Join(t.TempDir(), "devin")
	writeTestExecutable(t, fakePath, []byte(fakeDevinContractScript))
	backend, err := New("devin", Config{ExecutablePath: fakePath, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Env: env})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "say pong", opts)
	if err != nil {
		t.Fatal(err)
	}
	var messages []Message
	done := make(chan struct{})
	go func() {
		defer close(done)
		for message := range session.Messages {
			messages = append(messages, message)
		}
	}()
	var result Result
	select {
	case result = <-session.Result:
	case <-ctx.Done():
		t.Fatal("fake Devin did not return a result")
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("fake Devin did not close its message stream")
	}
	requests, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatal(err)
	}
	return result, messages, string(requests)
}

func TestDevinSessionRejection(t *testing.T) {
	for _, rpc := range []string{"session/load", "session/set_config_option", "session/prompt"} {
		for _, rejected := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/rejected=%v", rpc, rejected), func(t *testing.T) {
				env := map[string]string{"DEVIN_TEST_FAIL_RPC": rpc}
				if !rejected {
					env["DEVIN_TEST_ERROR_MESSAGE"] = "Authentication required"
				}
				opts := ExecOptions{ResumeSessionID: "old-session"}
				if rpc == "session/set_config_option" {
					opts.Model = "selected-model"
				}
				result, _, _ := runDevinContract(t, opts, env)
				if result.Status != "failed" || result.ResumeRejected != rejected {
					t.Fatalf("result=%+v, want rejected=%v", result, rejected)
				}
				if rejected && result.SessionID != "" {
					t.Fatalf("rejected session pointer survived: %q", result.SessionID)
				}
			})
		}
	}
}

func TestDevinModelMustBeConfirmedBeforePrompt(t *testing.T) {
	for _, resumed := range []bool{false, true} {
		for _, applied := range []bool{false, true} {
			t.Run(fmt.Sprintf("resume=%v/applied=%v", resumed, applied), func(t *testing.T) {
				opts := ExecOptions{Model: "selected-model"}
				if resumed {
					opts.ResumeSessionID = "old-session"
				}
				env := map[string]string{}
				if applied {
					env["DEVIN_TEST_APPLY_MODEL"] = "1"
				}
				result, messages, requests := runDevinContract(t, opts, env)
				promptSent := strings.Contains(requests, `"method":"session/prompt"`)
				if promptSent != applied {
					t.Fatalf("unconfirmed model reached prompt: applied=%v requests=%s", applied, requests)
				}
				if applied {
					usage, ok := result.Usage["selected-model"]
					if result.Status != "completed" || !ok || usage.InputTokens != 7 || usage.OutputTokens != 3 {
						t.Fatalf("confirmed model attribution failed: %+v", result)
					}
				} else {
					if result.Status != "failed" || result.ResumeRejected {
						t.Fatalf("model rejection was mistaken for a session rejection: %+v", result)
					}
					for _, message := range messages {
						if message.Type == MessageStatus && message.SessionID != "" {
							t.Fatal("session was pinned before setup succeeded")
						}
					}
					if !resumed && result.SessionID != "" {
						t.Fatal("never-prompted fresh session was published")
					}
				}
			})
		}
	}
}

func TestDevinDefaultModelAndAbsoluteCwd(t *testing.T) {
	result, _, requests := runDevinContract(t, ExecOptions{}, nil)
	if result.Status != "completed" || result.Usage["default-model"].InputTokens != 7 {
		t.Fatalf("configOptions default was not attributed: %+v", result)
	}
	for _, line := range strings.Split(strings.TrimSpace(requests), "\n") {
		var request struct {
			Method string `json:"method"`
			Params struct {
				Cwd string `json:"cwd"`
			} `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			t.Fatal(err)
		}
		if request.Method == "session/new" && !filepath.IsAbs(request.Params.Cwd) {
			t.Fatalf("relative cwd sent to Devin: %q", request.Params.Cwd)
		}
	}
}

func TestDevinUnadvertisedModelFailsBeforePrompt(t *testing.T) {
	result, _, requests := runDevinContract(t, ExecOptions{Model: "unavailable-model"}, nil)
	if result.Status != "failed" || result.SessionID != "" || result.ResumeRejected || len(result.Usage) != 0 || strings.Contains(requests, `"method":"session/prompt"`) {
		t.Fatalf("unavailable model was not rejected before prompt: %+v requests=%s", result, requests)
	}
}

func TestDevinEarlyFailureCancelsBeforeWait(t *testing.T) {
	started := time.Now()
	result, _, _ := runDevinContract(t, ExecOptions{}, map[string]string{
		"DEVIN_TEST_FAIL_RPC": "initialize", "DEVIN_TEST_IGNORE_EOF": "1",
	})
	if result.Status != "failed" || time.Since(started) > 3*time.Second {
		t.Fatalf("early failure waited for task timeout: %+v elapsed=%s", result, time.Since(started))
	}
}

func TestDevinLoadCapability(t *testing.T) {
	result, _, requests := runDevinContract(t, ExecOptions{ResumeSessionID: "old-session"}, map[string]string{
		"DEVIN_TEST_LOAD_SUPPORTED": "false",
	})
	if !result.ResumeRejected || strings.Contains(requests, `"method":"session/load"`) {
		t.Fatalf("unsupported load was sent: %+v requests=%s", result, requests)
	}
}

func TestDevinGroupedModelOptions(t *testing.T) {
	raw := json.RawMessage(`{"configOptions":[{"id":"unrelated","currentValue":true},{"id":"model","category":"model","currentValue":"a","options":[{"group":"recommended","name":"Recommended","options":[{"value":"b","name":"Model B"}]}]}]}`)
	requested := false
	actual, err := confirmDevinModel(context.Background(), func(_ context.Context, method string, params any) (json.RawMessage, error) {
		requested = method == "session/set_config_option" && params.(map[string]any)["value"] == "b"
		return json.RawMessage(`{"configOptions":[{"id":"model","category":"model","currentValue":"b"}]}`), nil
	}, "session", "b", raw, nil)
	if err != nil || actual != "b" || !requested {
		t.Fatalf("grouped choice was not applied: model=%q err=%v", actual, err)
	}
}

func TestDevinMcpFollowsAdvertisedTransports(t *testing.T) {
	result, _, requests := runDevinContract(t, ExecOptions{McpConfig: json.RawMessage(`{"mcpServers":{"local":{"command":"fake-mcp","args":["serve"]},"remote":{"type":"http","url":"https://example.invalid/mcp"}}}`)}, nil)
	if result.Status != "completed" || !strings.Contains(requests, "fake-mcp") || strings.Contains(requests, "example.invalid") {
		t.Fatalf("unsupported remote MCP reached session/new: result=%+v requests=%s", result, requests)
	}
}

func TestDevinMinimumBinaryVersion(t *testing.T) {
	for _, tc := range []struct {
		version  string
		rejected bool
	}{
		{"3000.3.21", true}, {"3000.3.22", false}, {"3000.10.21", false}, {"0.0.0-dev", true},
	} {
		if err := CheckMinVersion("devin", tc.version); (err != nil) != tc.rejected {
			t.Errorf("binary version %q: err=%v rejected=%v", tc.version, err, tc.rejected)
		}
	}
}
