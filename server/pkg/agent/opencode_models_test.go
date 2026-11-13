package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDiscoverOpenCodeV2Models(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	// Schema-shaped fixture for OpenCode 2.0.32 (843a6c514e60):
	// packages/schema/src/model.ts and Location.response in location.ts.
	fixture, err := os.ReadFile("testdata/opencode_v2_models.json")
	if err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(t.TempDir(), "opencode")
	writeTestExecutable(t, fake, []byte(fmt.Sprintf(`#!/bin/sh
case "$1" in
 --version) echo 'opencode v2.0.32' ;;
 api) cat <<'JSON'
%s
JSON
 ;;
 models)
  if [ "$2" = "--verbose" ]; then
   echo 'Unrecognized flag: --verbose in command opencode models' >&2
   exit 1
  fi
  printf 'opencode-go/deepseek-v4.1-flash\nopencode-go/plain\n'
 ;;
 *) exit 1 ;;
esac
`, fixture)))
	models, err := discoverOpenCodeModels(context.Background(), Command{Path: fake})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %+v", models)
	}
	if models[0].Thinking == nil {
		t.Fatal("structured discovery lost reasoning variants")
	}
	if models[1].Thinking != nil {
		t.Fatalf("plain model acquired thinking: %+v", models[1])
	}
}

func TestParseOpenCodeV2Models(t *testing.T) {
	fixture, err := os.ReadFile("testdata/opencode_v2_models.json")
	if err != nil {
		t.Fatal(err)
	}
	models := parseOpenCodeV2Models(fixture)
	want := []Model{
		{ID: "opencode-go/deepseek-v4.1-flash", Label: "opencode-go/deepseek-v4.1-flash", Provider: "opencode-go",
			Thinking: &ModelThinking{SupportedLevels: []ThinkingLevel{
				{Value: "low", Label: "Low"}, {Value: "high", Label: "High"}, {Value: "fast-mode", Label: "Fast Mode"},
			}}},
		{ID: "opencode-go/plain", Label: "opencode-go/plain", Provider: "opencode-go"},
	}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %#v, want %#v", models, want)
	}
	// No default is advertised by the API. Preserve runtime-native variant IDs
	// through the daemon/UI wire format and the existing v2 argument helper.
	raw, err := json.Marshal(models)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip []Model
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roundTrip, want) {
		t.Fatalf("catalog wire round trip = %+v", roundTrip)
	}
	for _, level := range roundTrip[0].Thinking.SupportedLevels {
		if !IsKnownThinkingValue("opencode", level.Value) {
			t.Fatalf("API would reject variant %q for persistence", level.Value)
		}
		valid, err := ValidateThinkingLevelWith(func() (Catalog, error) {
			return Catalog{Models: roundTrip}, nil
		}, "opencode", roundTrip[0].ID, level.Value)
		if err != nil || !valid {
			t.Fatalf("execution guard dropped variant %q: valid=%v, err=%v", level.Value, valid, err)
		}
		arg, ok := opencodeModelArg(roundTrip[0].ID, level.Value)
		if !ok || arg != roundTrip[0].ID+"#"+level.Value {
			t.Fatalf("variant %q did not reach model argument: %q", level.Value, arg)
		}
	}
}

func TestParseOpenCodeV2ModelsInvalidAndEmpty(t *testing.T) {
	for _, input := range []string{
		``, `not json`, `{"data":`, `[]`, `{}`, `null`,
		`{"data":null}`, `{"data":[]}`, `{"error":"unavailable"}`,
		`{"data":[{"id":"missing-provider"},{"providerID":"missing-model"}]}`,
		`{"data":[{"id":"m","providerID":"p","variants":{"high":{}}}]}`,
	} {
		t.Run(input, func(t *testing.T) {
			if got := parseOpenCodeV2Models([]byte(input)); len(got) != 0 {
				t.Fatalf("invalid/empty response produced models: %+v", got)
			}
		})
	}
	models := parseOpenCodeV2Models([]byte(`{"data":[
		{"providerID":"p","id":"m","variants":[{"id":""},{"id":"custom"},{"id":"custom"}]},
		{"providerID":"p","id":"m","variants":[]},
		{"providerID":"p","id":"plain","variants":[{"id":""}]}
	]}`))
	if len(models) != 2 || models[0].Thinking == nil ||
		!reflect.DeepEqual(models[0].Thinking.SupportedLevels, []ThinkingLevel{{Value: "custom", Label: "Custom"}}) ||
		models[1].Thinking != nil {
		t.Fatalf("duplicate/empty variants: %+v", models)
	}
}

func TestDiscoverOpenCodeModelContractsAndFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	for _, tc := range []struct {
		name, version, api, apiExit, verbose, wantCalls string
		wantThinking                                    bool
	}{
		{"v1 metadata", "1.18.31", "", "1", "valid", "--version\nmodels --verbose\n", true},
		{"unknown version keeps v1", "nightly", "", "1", "valid", "--version\nmodels --verbose\n", true},
		{"version probe fails keeps v1", "", "", "1", "valid", "--version\nmodels --verbose\n", true},
		{"v1 partial output", "1.18.31", "", "1", "partial", "--version\nmodels --verbose\n", true},
		{"v2 malformed", "opencode v2.0.32", "{broken", "0", "", "--version\napi\nmodels\n", false},
		{"v2 empty", "opencode v2.0.32", `{"location":{"directory":"/tmp"},"data":[]}`, "0", "", "--version\napi\nmodels\n", false},
		{"v2 failure", "opencode v2.0.32", `{"data":[{"providerID":"p","id":"m","variants":[{"id":"high"}]}]}`, "1", "", "--version\napi\nmodels\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			trace := filepath.Join(dir, "trace")
			t.Setenv("OPENCODE_TEST_TRACE", trace)
			fake := filepath.Join(dir, "opencode")
			writeTestExecutable(t, fake, []byte(fmt.Sprintf(`#!/bin/sh
case "$1" in
 --version)
  echo '--version' >> "$OPENCODE_TEST_TRACE"
  version='%s'
  [ -n "$version" ] || exit 1
  echo "$version"
  ;;
 api)
  echo api >> "$OPENCODE_TEST_TRACE"
  echo '%s'
  exit %s
  ;;
 models)
  echo "$*" >> "$OPENCODE_TEST_TRACE"
  if [ "$2" = "--verbose" ]; then
   [ '%s' = '' ] && exit 1
   printf 'p/m\n{"reasoning":true,"variants":{"high":{}}}\n'
   [ '%s' = partial ] && exit 1
  else
   echo p/m
  fi
  exit 0
  ;;
 *) exit 1 ;;
esac
`, tc.version, tc.api, tc.apiExit, tc.verbose, tc.verbose)))
			models, err := discoverOpenCodeModels(context.Background(), Command{Path: fake})
			if err != nil || len(models) != 1 || models[0].ID != "p/m" ||
				(models[0].Thinking != nil) != tc.wantThinking {
				t.Fatalf("models = %+v, err = %v", models, err)
			}
			raw, err := os.ReadFile(trace)
			if err != nil || string(raw) != tc.wantCalls {
				t.Fatalf("calls = %q, want %q (err=%v)", raw, tc.wantCalls, err)
			}
		})
	}
}

func TestDiscoverOpenCodeV2ModelsPreservesProfileAndLocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	for _, connection := range [][]string{nil, {"--server", "http://localhost:4567"}, {"--server=http://localhost:4567"}, {"--standalone"}} {
		t.Run(strings.Join(connection, " "), func(t *testing.T) {
			dir := t.TempDir()
			trace := filepath.Join(dir, "trace")
			t.Setenv("OPENCODE_TEST_TRACE", trace)
			t.Setenv("OPENCODE_TEST_PROFILE", "profile-environment")
			fake := filepath.Join(dir, "wrapper")
			writeTestExecutable(t, fake, []byte(`#!/bin/sh
[ "$1" = profile ] && [ "$2" = test ] || exit 1
shift 2
case "$1" in
 --server) shift 2 ;;
 --server=*|--standalone) shift ;;
esac
[ "$OPENCODE_TEST_PROFILE" = profile-environment ] || exit 1
if [ "$1" = "--version" ]; then echo 'opencode v2.0.32'; exit 0; fi
[ "$1" = api ] || exit 1
printf '%s\n' "$@" > "$OPENCODE_TEST_TRACE"
echo '{"location":{"directory":"/unused"},"data":[{"providerID":"p","id":"m","variants":[{"id":"high"}]}]}'
`))
			prefix := append([]string{"profile", "test"}, connection...)
			models, err := discoverOpenCodeModels(context.Background(), NewCommand(fake, prefix))
			if err != nil || len(models) != 1 || models[0].Thinking == nil {
				t.Fatalf("profile discovery = %+v, %v", models, err)
			}
			raw, err := os.ReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(strings.TrimSpace(string(raw)), "\n")
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"api", "GET", "/api/model?" + url.Values{"location[directory]": {cwd}}.Encode()}
			if len(connection) == 0 {
				want = append(want, "--standalone")
			}
			if !reflect.DeepEqual(args, want) {
				t.Fatalf("API args = %q, want %q", args, want)
			}
			if !reflect.DeepEqual(prefix, append([]string{"profile", "test"}, connection...)) {
				t.Fatal("discovery mutated runtime prefix")
			}
		})
	}
}

func TestDiscoverOpenCodeV2ModelsDeadline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	for _, hangAt := range []string{"version", "api"} {
		t.Run(hangAt, func(t *testing.T) {
			fake := filepath.Join(t.TempDir(), "opencode")
			writeTestExecutable(t, fake, []byte(fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then
 [ '%s' = version ] && exec sleep 30
 echo 'opencode v2.0.32'
 exit 0
fi
exec sleep 30
`, hangAt)))
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			start := time.Now()
			models, err := discoverOpenCodeModels(ctx, Command{Path: fake})
			if err != nil || len(models) != 0 {
				t.Fatalf("timed-out discovery = %+v, %v", models, err)
			}
			if elapsed := time.Since(start); elapsed > 3*time.Second {
				t.Fatalf("discovery exceeded caller deadline: %v", elapsed)
			}
		})
	}
}
