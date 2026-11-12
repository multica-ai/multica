//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestACPDiscoveryRejectsAgentRequests(t *testing.T) {
	for _, tc := range []struct {
		name, requestID, method, params string
		wantDenial                      bool
	}{
		{"permission", `0`, "session/request_permission", `{"options":[{"optionId":"approve","kind":"allow_once"},{"optionId":"deny","kind":"reject_once"}]}`, true},
		{"colliding request ID", `3`, "session/request_permission", `{"options":[{"optionId":"deny","kind":"reject_once"}]}`, true},
		{"string request ID", `"ask-1"`, "session/request_permission", `{"options":[{"optionId":"deny","kind":"reject_once"}]}`, true},
		{"grant only", `0`, "session/request_permission", `{"options":[{"optionId":"approve","kind":"allow_once"}]}`, false},
		{"unsupported filesystem request", `0`, "fs/read_text_file", `{"path":"/private/file"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := filepath.Join(t.TempDir(), "response.json")
			bin := filepath.Join(t.TempDir(), "hermes")
			script := fmt.Sprintf(`#!/bin/sh
while IFS= read -r line; do
 id=$(printf '%%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
 case "$line" in
  *'"method":"session/new"'*)
   printf '{"jsonrpc":"2.0","id":%%s,"result":{"sessionId":"s","models":{"currentModelId":"a","availableModels":[{"modelId":"a"},{"modelId":"b"}]},"configOptions":[{"id":"model","currentValue":"a","options":[{"value":"a"},{"value":"b"}]}]}}\n' "$id" ;;
  *'"method":"session/set_model"'*)
   printf '%%s\n' '{"jsonrpc":"2.0","id":%s,"method":"%s","params":%s}'
   IFS= read -r response || exit
   printf '%%s\n' "$response" > '%s'
   printf '{"jsonrpc":"2.0","id":%%s,"error":{"code":-32000,"message":"model_switch_cancelled"}}\n' "$id" ;;
  *) printf '{"jsonrpc":"2.0","id":%%s,"result":{}}\n' "$id" ;;
 esac
done
`, tc.requestID, tc.method, tc.params, record)
			writeTestExecutable(t, bin, []byte(script))
			catalog, err := discovered(discoverACPModels(context.Background(), Command{Path: bin}, acpDiscoveryProvider{
				strictErrors: true, probeModelEffort: true, timeout: 5 * time.Second,
			}))
			if err != nil || len(catalog.Models) != 2 {
				t.Fatalf("agent request lost the catalog: models=%+v, err=%v", catalog.Models, err)
			}
			if !catalog.Fallback || catalog.Verified() {
				t.Fatal("a rejected model switch must not claim its effort capabilities are verified")
			}
			raw, err := os.ReadFile(record)
			if err != nil {
				t.Fatalf("agent never received a response: %v", err)
			}
			var response struct {
				ID     json.RawMessage `json:"id"`
				Result struct {
					Outcome struct {
						Outcome  string `json:"outcome"`
						OptionID string `json:"optionId"`
					} `json:"outcome"`
				} `json:"result"`
				Error *struct {
					Code int `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(raw, &response); err != nil {
				t.Fatal(err)
			}
			if string(response.ID) != tc.requestID {
				t.Fatalf("response ID=%s, want %s", response.ID, tc.requestID)
			}
			if tc.wantDenial {
				if response.Error != nil || response.Result.Outcome.Outcome != "selected" || response.Result.Outcome.OptionID != "deny" {
					t.Fatalf("discovery must select the offered rejection: %s", raw)
				}
			} else if response.Error == nil || response.Error.Code != -32601 {
				t.Fatalf("discovery must refuse unsupported actions without granting permission: %s", raw)
			}
		})
	}
}

func TestACPDiscoveryKeepsCatalogAfterEffortTimeout(t *testing.T) {
	for _, callerCancels := range []bool{false, true} {
		t.Run(fmt.Sprintf("caller_cancels=%t", callerCancels), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "hermes")
			probing := filepath.Join(t.TempDir(), "probing")
			writeTestExecutable(t, bin, []byte(fmt.Sprintf(`#!/bin/sh
while IFS= read -r line; do
 id=$(printf '%%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
 case "$line" in
  *'"method":"session/new"'*) printf '{"jsonrpc":"2.0","id":%%s,"result":{"sessionId":"s","models":{"currentModelId":"a","availableModels":[{"modelId":"a"},{"modelId":"b"},{"modelId":"c"}]},"configOptions":[{"id":"effort","currentValue":"low","options":[{"value":"low"}]}]}}\n' "$id" ;;
  *'"modelId":"b"'*) printf '{"jsonrpc":"2.0","id":%%s,"result":{"configOptions":[{"id":"effort","currentValue":"high","options":[{"value":"high"}]}]}}\n' "$id" ;;
  *'"modelId":"c"'*) : > '%s'; while IFS= read -r _; do :; done ;;
  *) printf '{"jsonrpc":"2.0","id":%%s,"result":{}}\n' "$id" ;;
 esac
done
`, probing)))
			ctx := context.Background()
			timeout := 3 * time.Second
			if callerCancels {
				cancelCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				ctx = cancelCtx
				done := make(chan struct{})
				go func() {
					defer close(done)
					ticker := time.NewTicker(5 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-cancelCtx.Done():
							return
						case <-ticker.C:
							if _, err := os.Stat(probing); err == nil {
								cancel()
								return
							}
						}
					}
				}()
				defer func() { cancel(); <-done }()
				timeout = 15 * time.Second
			}
			catalog, err := discovered(discoverACPModels(ctx, Command{Path: bin}, acpDiscoveryProvider{
				strictErrors: true, probeModelEffort: true, timeout: timeout,
				annotate: annotateACPThinkingForSessionModel,
			}))
			if _, err := os.Stat(probing); err != nil {
				t.Fatalf("discovery did not reach the hanging model probe: %v", err)
			}
			models := catalog.Models
			if callerCancels {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("caller cancellation must still fail discovery: models=%+v, err=%v", models, err)
				}
				return
			}
			if err != nil || len(models) != 3 {
				t.Fatalf("optional effort timeout lost the catalog: models=%+v, err=%v", models, err)
			}
			if !catalog.Fallback || catalog.Verified() {
				t.Fatal("incomplete capability discovery must not reject saved settings or enter the catalog cache")
			}
			if models[0].Thinking == nil || models[0].Thinking.DefaultLevel != "low" || models[1].Thinking == nil || models[1].Thinking.DefaultLevel != "high" || models[2].Thinking != nil {
				t.Fatalf("only confirmed effort metadata should survive: %+v", models)
			}
		})
	}
}
