package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

const kiroEffortModelOptions = `{"options":[
  {"value":"claude-opus-4.7","reasoning":{"thinking":"alwaysOn","effortLevels":["low","medium","high","xhigh","max"],"defaultEffort":"high"}},
  {"value":"gpt-5.6-sol","reasoning":{"effortLevels":["none","high","max"],"defaultEffort":"high"}},
  {"value":"claude-haiku-4.5","reasoning":{"effortLevels":[]}},
  {"value":"internal-default","reasoning":{"effortLevels":["default"]}}
],"hasMore":false}`

func TestParseKiroModelThinking(t *testing.T) {
	t.Parallel()
	got, err := parseKiroModelThinking(json.RawMessage(kiroEffortModelOptions))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("models with effort = %v, want only Opus and Sol", got)
	}
	if levels := thinkingValues(got["claude-opus-4.7"]); !reflect.DeepEqual(levels, []string{"low", "medium", "high", "xhigh", "max"}) {
		t.Errorf("Opus effort = %v", levels)
	}
	if levels := thinkingValues(got["gpt-5.6-sol"]); !reflect.DeepEqual(levels, []string{"none", "high", "max"}) {
		t.Errorf("Sol effort = %v", levels)
	}
	for id, thinking := range got {
		if thinking.DefaultLevel != "high" {
			t.Errorf("%s default = %q, want high", id, thinking.DefaultLevel)
		}
		for _, level := range thinking.SupportedLevels {
			if !IsKnownThinkingValue("kiro", level.Value) {
				t.Errorf("advertised Kiro effort %q cannot be saved", level.Value)
			}
		}
	}
	cleaned, err := parseKiroModelThinking(json.RawMessage(`{"options":[{"value":"m","reasoning":{"effortLevels":["","default","low","low","bad value","future-level"],"defaultEffort":"missing"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if levels := thinkingValues(cleaned["m"]); !reflect.DeepEqual(levels, []string{"low", "future-level"}) || cleaned["m"].DefaultLevel != "" {
		t.Errorf("cleaned catalog = %+v", cleaned["m"])
	}
	if _, err := parseKiroModelThinking(json.RawMessage(`{"options":"invalid"}`)); err == nil {
		t.Error("malformed options must not produce a catalog")
	}
}

func TestDiscoverKiroModelsIncludesPerModelEffort(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI is a shell script")
	}
	t.Parallel()
	for _, tc := range []struct {
		name      string
		response  string
		wantLevel bool
	}{
		{"native catalog", `"result":` + kiroEffortModelOptions, true},
		{"older CLI", `"error":{"code":-32601,"message":"Method not found"}`, false},
		{"malformed metadata", `"result":{"options":"invalid"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := strings.ReplaceAll(tc.response, "\n", "")
			script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$id" ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"discovery","models":{"currentModelId":"gpt-5.6-sol","availableModels":[{"modelId":"claude-opus-4.7"},{"modelId":"gpt-5.6-sol"},{"modelId":"claude-haiku-4.5"},{"modelId":"unknown"}]}}}\n' "$id" ;;
    *'"method":"_kiro.dev/commands/options"'*)
      case "$line" in
        *'"command":"model"'*'"sessionId":"discovery"'*)
          printf '{"jsonrpc":"2.0","id":%s,` + response + `}\n' "$id" ;;
        *) exit 1 ;;
      esac ;;
  esac
done
`
			fake := filepath.Join(t.TempDir(), "kiro-cli")
			writeTestExecutable(t, fake, []byte(script))
			catalog, err := ListModels(context.Background(), "kiro", Command{Path: fake})
			if err != nil {
				t.Fatal(err)
			}
			if len(catalog.Models) != 4 || !catalog.Verified() {
				t.Fatalf("catalog = %+v, want four discovered models", catalog)
			}
			for i, model := range catalog.Models {
				wantThinking := tc.wantLevel && i < 2
				if (model.Thinking != nil) != wantThinking {
					t.Errorf("%s thinking = %+v, want present %v", model.ID, model.Thinking, wantThinking)
				}
			}
			if tc.wantLevel {
				for _, check := range []struct {
					model, level string
					want         bool
				}{
					{"claude-opus-4.7", "xhigh", true},
					{"gpt-5.6-sol", "none", true},
					{"gpt-5.6-sol", "xhigh", false},
					{"claude-haiku-4.5", "high", false},
					{"", "none", true},
				} {
					got, err := ValidateThinkingLevelWith(func() (Catalog, error) { return catalog, nil }, "kiro", check.model, check.level)
					if err != nil || got != check.want {
						t.Errorf("validate(%q, %q) = %v, %v; want %v", check.model, check.level, got, err, check.want)
					}
				}
			}
		})
	}
}

func TestKiroBackendAppliesEffortBeforePrompt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI is a shell script")
	}
	t.Parallel()
	for _, tc := range []struct {
		name, model, resume, level, response, warning string
		customArgs                                    []string
	}{
		{name: "fresh", model: "auto", level: "max", response: `"result":{"success":true}`},
		{name: "resumed", model: "auto", resume: "ses_loaded", level: "low", response: `"result":{"success":true}`},
		{name: "default model", level: "max", response: `"result":{"success":true}`},
		{name: "resumed without model override", resume: "ses_loaded", level: "low", response: `"result":{"success":true}`},
		{name: "runtime default", customArgs: []string{}},
		{name: "custom effort separate value", customArgs: []string{"--effort", "high"}},
		{name: "custom effort inline value", customArgs: []string{"--effort=low"}},
		{name: "resumed custom effort", resume: "ses_loaded", customArgs: []string{"--effort", "high"}},
		{name: "rejected", level: "max", response: `"result":{"success":false,"message":"unsupported effort"}`, warning: "unsupported effort"},
		{name: "unsupported command", level: "high", response: `"error":{"code":-32601,"message":"Method not found"}`, warning: "Method not found"},
		{name: "unconfirmed", level: "high", response: `"result":{}`, warning: "did not apply"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			requestsFile := filepath.Join(dir, "requests.jsonl")
			argsFile := filepath.Join(dir, "args.txt")
			fake := filepath.Join(dir, "kiro-cli")
			script := strings.Replace(fakeKiroACPScript(), `    *'"method":"session/prompt"'*)`, `    *'"method":"_kiro.dev/commands/execute"'*)
      printf '{"jsonrpc":"2.0","id":%s,`+tc.response+`}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)`, 1)
			writeTestExecutable(t, fake, []byte(script))
			var logs strings.Builder
			backend, err := New("kiro", Config{
				ExecutablePath: fake,
				Logger:         slog.New(slog.NewTextHandler(&logs, nil)),
				Env: map[string]string{
					"KIRO_REQUESTS_FILE": requestsFile,
					"KIRO_ARGS_FILE":     argsFile,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			customArgs := tc.customArgs
			if customArgs == nil {
				customArgs = []string{"--effort", "high", "--effort=low"}
			}
			session, err := backend.Execute(ctx, "task", ExecOptions{
				ResumeSessionID: tc.resume,
				Model:           tc.model,
				ThinkingLevel:   tc.level,
				Timeout:         5 * time.Second,
				CustomArgs:      customArgs,
			})
			if err != nil {
				t.Fatal(err)
			}
			for range session.Messages {
			}
			result := <-session.Result
			if result.Status != "completed" {
				t.Fatalf("result = %+v", result)
			}
			if tc.warning != "" && !strings.Contains(logs.String(), tc.warning) {
				t.Errorf("missing diagnostic %q in %s", tc.warning, logs.String())
			}
			argv, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatal(err)
			}
			wantArgs := []string{"acp", "--trust-all-tools"}
			if tc.level == "" {
				wantArgs = append(wantArgs, customArgs...)
			}
			if got := strings.Split(strings.TrimSpace(string(argv)), "\n"); !reflect.DeepEqual(got, wantArgs) {
				t.Errorf("launch args = %q, want %q", got, wantArgs)
			}
			raw, err := os.ReadFile(requestsFile)
			if err != nil {
				t.Fatal(err)
			}
			var methods []string
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				var request struct {
					Method string `json:"method"`
					Params struct {
						SessionID string `json:"sessionId"`
						ModelID   string `json:"modelId"`
						Command   struct {
							Command string            `json:"command"`
							Args    map[string]string `json:"args"`
						} `json:"command"`
					} `json:"params"`
				}
				if err := json.Unmarshal([]byte(line), &request); err != nil {
					t.Fatal(err)
				}
				methods = append(methods, request.Method)
				if request.Method == "session/set_model" && request.Params.ModelID != "auto" {
					t.Errorf("model selection did not initialize the effective model: %s", line)
				}
				if request.Method == "_kiro.dev/commands/execute" {
					wantID := "ses_new"
					if tc.resume != "" {
						wantID = tc.resume
					}
					if request.Params.SessionID != wantID || request.Params.Command.Command != "effort" || request.Params.Command.Args["level"] != tc.level {
						t.Errorf("effort request = %s", line)
					}
				}
			}
			setup := "session/new"
			if tc.resume != "" {
				setup = "session/load"
			}
			want := []string{"initialize", setup}
			if tc.model != "" || tc.resume == "" && tc.level != "" {
				want = append(want, "session/set_model")
			}
			if tc.level != "" {
				want = append(want, "_kiro.dev/commands/execute")
			}
			want = append(want, "session/prompt")
			if !reflect.DeepEqual(methods, want) {
				t.Errorf("methods = %v, want %v", methods, want)
			}
		})
	}
}
