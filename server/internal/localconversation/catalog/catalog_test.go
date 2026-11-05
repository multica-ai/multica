package catalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeCLI writes a shell script that answers each request line containing
// a key with the matching reply, and records its arguments.
func fakeCLI(t *testing.T, replies map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	var cases strings.Builder
	for key, reply := range replies {
		cases.WriteString("    *'" + key + "'*) printf '%s\\n' '" + reply + "' ;;\n")
	}
	script := "#!/bin/sh\necho \"$@\" >> '" + argsFile + "'\nwhile IFS= read -r line; do\n  case \"$line\" in\n" + cases.String() + "  esac\ndone\n"
	path := filepath.Join(dir, "cli")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path, argsFile
}

func finder(paths map[string]string) Find {
	return func(_ context.Context, provider string) (string, error) {
		if p, ok := paths[provider]; ok {
			return p, nil
		}
		return "", errors.New("not installed")
	}
}

const claudeInit = `{"type":"control_response","response":{"subtype":"success","request_id":"1","response":{"models":[{"value":"sonnet","displayName":"Sonnet","supportsEffort":true,"supportedEffortLevels":["low","xhigh","bogus"]},{"value":"haiku"}]}}}`
const claudeUsage = `{"type":"control_response","response":{"subtype":"success","request_id":"2","response":{"subscription_type":"max","rate_limits":{"five_hour":{"utilization":42,"resets_at":1793000000},"seven_day":{"utilization":120}},"session":{"model_usage":{"claude-sonnet":{"contextWindow":200000}}},"token":"secret"}}}`

func TestClaudeModelsAndUsageComeFromTheCLI(t *testing.T) {
	t.Parallel()

	exe, args := fakeCLI(t, map[string]string{`"subtype":"initialize"`: claudeInit, `"subtype":"get_usage"`: claudeUsage})
	claudeDir := t.TempDir()
	c := New(finder(map[string]string{"claude": exe}), claudeDir, t.TempDir())
	ctx := context.Background()

	models, err := c.Models(ctx, "claude", false)
	if err != nil || len(models.Models) != 2 || models.Models[1].Label != "haiku" || models.Models[1].Thinking != nil {
		t.Fatalf("models = %+v, %v", models, err)
	}
	if levels := models.Models[0].Thinking.SupportedLevels; len(levels) != 2 || levels[1].Label != "Extra high" {
		t.Fatalf("levels = %+v", levels)
	}
	if _, err := c.Models(ctx, "claude", false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(args); strings.Count(string(data), "\n") != 1 {
		t.Fatalf("the cached catalog started the CLI again: %q", data)
	}

	projects := filepath.Join(claudeDir, "projects", "-repo")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	transcript := `{"type":"assistant","timestamp":"2026-10-29T10:00:00Z","message":{"model":"claude-sonnet","usage":{"input_tokens":10,"cache_read_input_tokens":90}}}
{"type":"assistant","isSidechain":true,"message":{"model":"claude-sonnet","usage":{"input_tokens":99999}}}`
	if err := os.WriteFile(filepath.Join(projects, "abc-123.jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	if size, ok := c.ClaudeTranscriptSize("abc-123"); !ok || size != int64(len(transcript)) {
		t.Fatalf("transcript size = %d, %v", size, ok)
	}
	if _, ok := c.ClaudeTranscriptSize("missing"); ok {
		t.Fatal("a session with no file has no size")
	}
	usage, err := c.Usage(ctx, "claude", &Session{ProviderSessionID: "abc-123", Cwd: t.TempDir()}, false)
	if err != nil {
		t.Fatal(err)
	}
	if usage.Account.Status != "available" || *usage.Account.Plan != "max" || len(usage.Account.Windows) != 2 || usage.Account.Windows[1].UsedPercent != 100 {
		t.Fatalf("account = %+v", usage.Account)
	}
	if *usage.Context.UsedTokens != 100 || *usage.Context.LimitTokens != 200000 || *usage.Context.Model != "claude-sonnet" {
		t.Fatalf("context = %+v", usage.Context)
	}
	if data, _ := os.ReadFile(args); !strings.Contains(string(data), "--resume abc-123") {
		t.Fatalf("usage probe args = %q", data)
	}
}

func TestCodexUsageAndModels(t *testing.T) {
	t.Parallel()

	exe, _ := fakeCLI(t, map[string]string{
		`"method":"initialize"`:              `{"id":1,"result":{}}`,
		`"method":"config/read"`:             `{"id":2,"result":{"config":{"model":"gpt-5"}}}`,
		`"method":"account/rateLimits/read"`: `{"id":2,"result":{"rateLimits":{"planType":"plus","primary":{"usedPercent":5,"windowDurationMins":300,"resetsAt":1793000000},"secondary":{"usedPercent":7,"windowDurationMins":10080}}}}`,
	})
	codexDir := t.TempDir()
	cache := `{"models":[{"slug":"gpt-5","display_name":"GPT-5","default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"medium","description":"Balanced"}]},{"slug":"hidden","visibility":"hide"}]}`
	if err := os.WriteFile(filepath.Join(codexDir, "models_cache.json"), []byte(cache), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(finder(map[string]string{"codex": exe}), t.TempDir(), codexDir)
	models, _ := c.Models(context.Background(), "codex", false)
	if len(models.Models) != 1 || models.Models[0].Thinking.DefaultLevel != "medium" || models.Models[0].Thinking.SupportedLevels[0].Description != "Balanced" {
		t.Fatalf("codex models = %+v", models)
	}
	usage, err := c.Usage(context.Background(), "codex", nil, false)
	if err != nil || usage.Account.Status != "available" || *usage.Account.Plan != "plus" {
		t.Fatalf("codex usage = %+v, %v", usage, err)
	}
	if usage.Account.Windows[0].Label != "5h" || usage.Account.Windows[1].Label != "7d" {
		t.Fatalf("windows = %+v", usage.Account.Windows)
	}
}

func TestCodexDefaultIsAvailableBeforeAnyConversationRuns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, config string
	}{
		{"configured", `{"config":{"model":"b"}}`},
		{"runtime default", `{"config":{"model":null}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exe, args := fakeCLI(t, map[string]string{
				`"method":"initialize"`:  `{"id":1,"result":{}}`,
				`"method":"config/read"`: `{"id":2,"result":` + tc.config + `}`,
				`"method":"model/list"`:  `{"id":3,"result":{"data":[{"id":"b","isDefault":true}]}}`,
			})
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "models_cache.json"), []byte(`{"models":[{"slug":"a"},{"slug":"b","supported_reasoning_levels":[{"effort":"high"}]}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			c := New(finder(map[string]string{"codex": exe}), t.TempDir(), dir)
			for _, force := range []bool{false, false, true} {
				models, err := c.Models(context.Background(), "codex", force)
				if err != nil || len(models.Models) != 2 || models.Models[0].IsDefault || !models.Models[1].IsDefault || models.Models[1].Thinking.SupportedLevels[0].Value != "high" {
					t.Fatalf("models = %+v, %v", models, err)
				}
			}
			if data, _ := os.ReadFile(args); strings.Count(string(data), "\n") != 2 {
				t.Fatalf("expected one cached probe and one forced probe: %q", data)
			}
		})
	}
}

func TestCodexDefaultDiscoveryFailureKeepsExplicitModels(t *testing.T) {
	t.Parallel()
	exe, _ := fakeCLI(t, map[string]string{
		`"method":"initialize"`:  `{"id":1,"result":{}}`,
		`"method":"config/read"`: `{"id":2,"error":{"code":-32603,"message":"unavailable"}}`,
	})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "models_cache.json"), []byte(`{"models":[{"slug":"a","supported_reasoning_levels":[{"effort":"high"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(finder(map[string]string{"codex": exe}), t.TempDir(), dir)
	models, err := c.Models(context.Background(), "codex", false)
	if err != nil || len(models.Models) != 1 || models.Models[0].IsDefault || models.Models[0].Thinking.SupportedLevels[0].Value != "high" {
		t.Fatalf("models = %+v, %v", models, err)
	}
}

func TestUnavailableAndFailingProviders(t *testing.T) {
	t.Parallel()

	silent, _ := fakeCLI(t, map[string]string{})
	c := New(finder(map[string]string{"claude": silent}), t.TempDir(), t.TempDir())
	providers := c.Providers(context.Background())
	if providers[0].ID != "codex" || providers[0].Available || !providers[1].Available {
		t.Fatalf("providers = %+v", providers)
	}
	if usage, _ := c.Usage(context.Background(), "codex", nil, false); usage.Account.Status != "unavailable" {
		t.Fatalf("missing provider usage = %+v", usage.Account)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := c.Models(ctx, "claude", true); !errors.Is(err, errNoAnswer) {
		t.Fatalf("silent CLI = %v", err)
	}
	if usage, _ := c.Usage(ctx, "claude", nil, false); usage.Account.Status != "error" {
		t.Fatalf("silent CLI usage = %+v", usage.Account)
	}
	if _, err := c.Models(context.Background(), "gemini", false); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("unknown provider = %v", err)
	}
}

func TestClaudeDefaultRowMarksTheRowItResolvesTo(t *testing.T) {
	t.Parallel()

	models := claudeModels([]byte(`{"models":[{"value":"default","resolvedModel":"claude-opus-5[1m]","displayName":"Default"},{"value":"sonnet","resolvedModel":"claude-sonnet-5"},{"value":"opus[1m]","resolvedModel":"claude-opus-5[1m]","displayName":"Opus 1M"}]}`)).Models
	if len(models) != 2 || models[0].ID != "sonnet" || models[0].IsDefault || models[1].ID != "opus[1m]" || !models[1].IsDefault {
		t.Fatalf("models = %+v", models)
	}
}

func TestClaudeDefaultRowBecomesARowOnlyWhenNothingElseNamesIt(t *testing.T) {
	t.Parallel()

	models := claudeModels([]byte(`{"models":[{"value":"default","resolvedModel":"org-model","displayName":"Org"},{"value":"sonnet"}]}`)).Models
	if len(models) != 2 || models[1].ID != "org-model" || !models[1].IsDefault {
		t.Fatalf("restricted install = %+v", models)
	}
	for name, raw := range map[string]string{
		"disabled":   `{"models":[{"value":"default","resolvedModel":"org-model","disabled":true}]}`,
		"unresolved": `{"models":[{"value":"default"}]}`,
	} {
		if models := claudeModels([]byte(raw)).Models; len(models) != 0 {
			t.Errorf("%s: %+v", name, models)
		}
	}
}

func TestCodexDefaultComesFromWhatARunningProcessReported(t *testing.T) {
	t.Parallel()

	codexDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexDir, "models_cache.json"), []byte(`{"models":[{"slug":"a"},{"slug":"b"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(finder(nil), t.TempDir(), codexDir)
	models, _ := c.Models(context.Background(), "codex", false)
	if models.Models[0].IsDefault || models.Models[1].IsDefault {
		t.Fatalf("nothing reported yet: %+v", models)
	}
	c.NoteCodexDefault("b")
	models, _ = c.Models(context.Background(), "codex", false)
	if models.Models[0].IsDefault || !models.Models[1].IsDefault {
		t.Fatalf("after report: %+v", models)
	}
}
