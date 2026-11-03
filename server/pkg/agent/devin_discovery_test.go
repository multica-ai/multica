package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func devinDiscoveryFixture(t *testing.T, env map[string]string) (Command, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake CLI")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "devin")
	writeTestExecutable(t, path, []byte(fakeDevinContractScript))
	requests := filepath.Join(dir, "requests.jsonl")
	t.Setenv("DEVIN_REQUESTS_FILE", requests)
	t.Setenv("WINDSURF_API_KEY", "")
	t.Setenv("DEVIN_TEST_AUTH_METHODS", `[{"id":"windsurf-api-key"}]`)
	for key, value := range env {
		t.Setenv(key, value)
	}
	return Command{Path: path}, requests
}

func readDevinRequests(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestDiscoverDevinModelsUsesSessionCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, key, methods string
		auth               bool
	}{
		{"API key", "fixture-key", `[{"id":"windsurf-api-key"}]`, true},
		{"stored login", "", `[{"id":"windsurf-api-key"}]`, false},
		{"unadvertised key method", "fixture-key", `[]`, false},
		{"unknown method", "fixture-key", `[{"id":"future-auth"}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd, requestsFile := devinDiscoveryFixture(t, map[string]string{
				"WINDSURF_API_KEY": tc.key, "DEVIN_TEST_AUTH_METHODS": tc.methods,
				"DEVIN_TEST_CATALOG_ERROR": "Not logged in. Run devin auth login to authenticate.",
			})
			models, err := discoverDevinModels(context.Background(), cmd)
			if err != nil || len(models) != 2 || models[0].ID != "default-model" || !models[0].Default || models[1].ID != "selected-model" {
				t.Fatalf("models=%+v err=%v", models, err)
			}
			requests := readDevinRequests(t, requestsFile)
			initAt := strings.Index(requests, `"method":"initialize"`)
			authAt := strings.Index(requests, `"method":"authenticate"`)
			sessionAt := strings.Index(requests, `"method":"session/new"`)
			if initAt < 0 || sessionAt <= initAt || (authAt >= 0) != tc.auth || (tc.auth && !(initAt < authAt && authAt < sessionAt)) {
				t.Fatalf("wrong discovery handshake: %s", requests)
			}
			if tc.auth && (!strings.Contains(requests, `"headless":true`) || !strings.Contains(requests, `"methodId":"windsurf-api-key"`)) {
				t.Fatalf("missing headless key authentication: %s", requests)
			}
			if strings.Contains(requests, "fixture-key") || strings.Contains(requests, "models list") || strings.Contains(requests, `"method":"session/prompt"`) {
				t.Fatalf("discovery sent credentials, queried aliases, or prompted: %s", requests)
			}
		})
	}
}

func TestDiscoverDevinModelsRejectsEmptyAndUnrecognizedCatalogs(t *testing.T) {
	for _, tc := range []struct {
		name, result, want string
	}{
		{"no credentials", `{"configOptions":[{"id":"model","options":[]}]}`, "WINDSURF_API_KEY"},
		{"missing options", `{"configOptions":[{"id":"model"}]}`, "WINDSURF_API_KEY"},
		{"null options", `{"configOptions":[{"id":"model","options":null}]}`, "WINDSURF_API_KEY"},
		{"empty overrides legacy", `{"configOptions":[{"id":"model","options":[]}],"models":{"availableModels":[{"modelId":"forbidden"}]}}`, "model allowlist"},
		{"missing options overrides legacy", `{"configOptions":[{"id":"model"}],"models":{"availableModels":[{"modelId":"forbidden"}]}}`, "model allowlist"},
		{"legacy only", `{"models":{"availableModels":[{"modelId":"legacy"}]}}`, "unrecognized Devin model catalog"},
		{"missing model", `{"configOptions":[{"id":"mode","options":[]}]}`, "unrecognized Devin model catalog"},
		{"missing model id", `{"configOptions":[{"category":"model","options":[]}]}`, "unrecognized Devin model catalog"},
		{"malformed model", `{"configOptions":[{"id":"model","options":"bad"}]}`, "unrecognized Devin model catalog"},
		{"malformed choice", `{"configOptions":[{"id":"model","options":[{}]}]}`, "unrecognized Devin model catalog"},
		{"malformed response", `[]`, "unrecognized Devin model catalog"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd, requestsFile := devinDiscoveryFixture(t, map[string]string{"DEVIN_TEST_SESSION_RESULT": tc.result})
			models, err := discoverDevinModels(context.Background(), cmd)
			if err == nil || len(models) != 0 || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("models=%+v err=%v, want %q", models, err, tc.want)
			}
			if strings.Contains(tc.want, "unrecognized") && strings.Contains(err.Error(), "WINDSURF_API_KEY") {
				t.Fatalf("malformed catalog was misreported as an authentication problem: %v", err)
			}
			if strings.Contains(readDevinRequests(t, requestsFile), `"method":"session/prompt"`) {
				t.Fatal("failed discovery sent a prompt")
			}
		})
	}
}

func TestDiscoverDevinModelsUsesGroupedSessionChoicesOverLegacy(t *testing.T) {
	cmd, _ := devinDiscoveryFixture(t, map[string]string{
		"DEVIN_TEST_SESSION_RESULT": `{"configOptions":[{"id":"unrelated","currentValue":true},{"id":"model","currentValue":"b","options":[{"value":"a","name":"Model A"},{"group":"recommended","options":[{"value":"b","name":"Model B"},{"group":"nested","options":[{"value":"c"},{"value":"b"}]}]}]}],"models":{"availableModels":[{"modelId":"forbidden"}]}}`,
	})
	models, err := discoverDevinModels(context.Background(), cmd)
	if err != nil || len(models) != 3 || models[0].ID != "a" || models[1].ID != "b" || models[1].Label != "Model B" || !models[1].Default || models[2].ID != "c" || models[2].Label != "c" {
		t.Fatalf("models=%+v err=%v", models, err)
	}
}

func TestDiscoverDevinModelsReportsSafeRPCFailures(t *testing.T) {
	for _, stage := range []string{"initialize", "authenticate", "session/new"} {
		t.Run(stage, func(t *testing.T) {
			wireError, err := json.Marshal(map[string]any{
				"code": -32000, "message": "invalid api key: opaque-fixture-key",
				"data": map[string]string{"token": "private-token", "detail": strings.Repeat("界", 3000) + " opaque-fixture-key"},
			})
			if err != nil {
				t.Fatal(err)
			}
			cmd, requestsFile := devinDiscoveryFixture(t, map[string]string{
				"WINDSURF_API_KEY": "opaque-fixture-key", "DEVIN_TEST_FAIL_RPC": stage, "DEVIN_TEST_RPC_ERROR": string(wireError),
			})
			_, err = discoverDevinModels(context.Background(), cmd)
			if err == nil || !strings.Contains(err.Error(), stage) || !strings.Contains(err.Error(), "invalid api key") {
				t.Fatalf("lost failure stage or reason: %v", err)
			}
			message := err.Error()
			if strings.Contains(message, "opaque-fixture-key") || strings.Contains(message, "private-token") || len(message) > 4500 || !utf8.ValidString(message) {
				t.Fatalf("unsafe or unbounded diagnostic (%d bytes)", len(message))
			}
			requests := readDevinRequests(t, requestsFile)
			if strings.Contains(requests, `"method":"session/prompt"`) || (stage != "session/new" && strings.Contains(requests, `"method":"session/new"`)) {
				t.Fatalf("discovery continued after failure: %s", requests)
			}
		})
	}
}

func TestDevinDiscoveredModelExecutesWithoutAliasCatalog(t *testing.T) {
	cmd, requestsFile := devinDiscoveryFixture(t, map[string]string{
		"WINDSURF_API_KEY": "fixture-key", "DEVIN_TEST_APPLY_MODEL": "1",
		"DEVIN_TEST_CATALOG_ERROR": "Not logged in. Run devin auth login to authenticate.",
	})
	catalog, err := ListModels(context.Background(), "devin", cmd)
	if err != nil || len(catalog.Models) != 2 {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}
	result, _, requests := runDevinContractAt(t, cmd.Path, ExecOptions{Model: catalog.Models[1].ID}, map[string]string{})
	if result.Status != "completed" || result.Usage["selected-model"].InputTokens != 7 || !strings.Contains(requests, `"value":"selected-model"`) {
		t.Fatalf("discovered non-default model did not execute: %+v requests=%s", result, requests)
	}
	if strings.Contains(requests, "models list") || strings.Contains(readDevinRequests(t, requestsFile), "models list") {
		t.Fatal("canonical session ID required alias discovery")
	}
}

func TestDevinAliasFailureIncludesSafeStderr(t *testing.T) {
	key := "opaque-fixture-key"
	result, _, requests := runDevinContract(t, ExecOptions{Model: "opus"}, map[string]string{
		"WINDSURF_API_KEY":         key,
		"DEVIN_TEST_CATALOG_ERROR": "Authorization: Bearer private-bearer\n" + strings.Repeat("界", 3000) + "\nNot logged in. Run devin auth login; credential " + key,
	})
	if result.Status != "failed" || !strings.Contains(result.Error, "Not logged in") || !strings.Contains(result.Error, "model picker") || !strings.Contains(requests, "models list") || strings.Contains(requests, `"method":"session/prompt"`) {
		t.Fatalf("alias failure lost its remedy: %+v requests=%s", result, requests)
	}
	if strings.Contains(result.Error, key) || strings.Contains(result.Error, "private-bearer") || len(result.Error) > 2500 || !utf8.ValidString(result.Error) {
		t.Fatalf("unsafe alias diagnostic (%d bytes)", len(result.Error))
	}
}

func TestDiscoverDevinModelsHonorsCancellation(t *testing.T) {
	cmd, _ := devinDiscoveryFixture(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := discoverDevinModels(ctx, cmd)
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("cancellation was not honored: %v", err)
	}
}
