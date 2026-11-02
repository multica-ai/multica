package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil/agyimage"
)

func TestAntigravityImageProcess(t *testing.T) {
	if os.Getenv(agyimage.ModeEnv) == "" {
		return
	}
	agyimage.Run()
	os.Exit(0)
}

func antigravityImageTestCommand(t *testing.T, mode string) Command {
	t.Helper()
	t.Setenv(agyimage.ModeEnv, mode)
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return NewCommand(path, []string{"-test.run=^TestAntigravityImageProcess$", "--"})
}

func antigravityImageTestDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func antigravityImageTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, AntigravityOAuthTokenPath)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(agyimage.Token), 0600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestProbeAntigravityImages(t *testing.T) {
	for _, mode := range []string{"ready", "catalog-fail", "catalog-empty", "missing", "directory", "invalid", "empty-token", "enterprise", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			command := antigravityImageTestCommand(t, mode)
			home := antigravityImageTestHome(t)
			auth := filepath.Join(home, AntigravityOAuthTokenPath)
			counter := filepath.Join(t.TempDir(), "calls")
			t.Setenv("AGY_IMAGE_TEST_COUNTER", counter)
			switch mode {
			case "missing", "directory", "symlink":
				if err := os.Remove(auth); err != nil {
					t.Fatal(err)
				}
				if mode == "directory" {
					if err := os.Mkdir(auth, 0700); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "symlink" {
					target := filepath.Join(antigravityImageTestHome(t), AntigravityOAuthTokenPath)
					if err := os.Symlink(target, auth); err != nil {
						t.Skipf("symlink unavailable: %v", err)
					}
				}
			case "invalid":
				os.WriteFile(auth, []byte("invalid"), 0600)
			case "empty-token":
				os.WriteFile(auth, []byte(`{"token":{"access_token":"  "}}`), 0600)
			case "enterprise":
				os.WriteFile(auth, []byte(`{"auth_method":"enterprise","token":{"access_token":"fake"}}`), 0600)
			}
			err := ProbeAntigravityImages(t.Context(), command, home)
			if (err == nil) != (mode == "ready" || mode == "symlink") {
				t.Fatalf("probe mode=%s error=%v", mode, err)
			}
			calls, _ := os.ReadFile(counter)
			want := ""
			if mode == "ready" || mode == "symlink" || strings.HasPrefix(mode, "catalog-") {
				want = "probe\n"
			}
			if string(calls) != want {
				t.Fatalf("probe invoked before login gate: %q want %q", calls, want)
			}
		})
	}
}

func TestAntigravityNativeImageExecution(t *testing.T) {
	t.Setenv("MULTICA_TOKEN", "fixture-platform-token-must-not-reach-agy")
	for _, mode := range []string{"ready", "edit", "quota-result", "quota-log", "quota-transcript", "tail-quota", "escape", "eof", "prose"} {
		t.Run(mode, func(t *testing.T) {
			command := antigravityImageTestCommand(t, mode)
			home := antigravityImageTestHome(t)
			dir := antigravityImageTestDir(t)
			counter := filepath.Join(t.TempDir(), "calls")
			t.Setenv("AGY_IMAGE_TEST_COUNTER", counter)
			var inputs []string
			if mode == "edit" {
				inputs = []string{filepath.Join(dir, "input-0.jpg")}
			}
			result, err := GenerateAntigravityImage(t.Context(), command, home, dir, "fixture subject image", inputs)
			wantErr := mode == "escape" || mode == "eof" || mode == "prose"
			if (err != nil) != wantErr {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if result.RateLimited != strings.HasPrefix(mode, "quota-") {
				t.Fatalf("quota mapping: %+v %v", result, err)
			}
			if !wantErr && !result.RateLimited && !strings.Contains(result.Path, agyimage.Parent) {
				t.Fatalf("missing scoped image: %+v", result)
			}
			calls, _ := os.ReadFile(counter)
			if string(calls) != "generate\n" {
				t.Fatalf("native replay: %q", calls)
			}
			if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(filepath.Join(home, AntigravityOAuthTokenPath))
			if err != nil || string(content) != agyimage.Token {
				t.Fatal("host authentication removed or changed")
			}
		})
	}
}

func TestAntigravityNativeImageCancellation(t *testing.T) {
	command := antigravityImageTestCommand(t, "cancel")
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := GenerateAntigravityImage(ctx, command, antigravityImageTestHome(t), t.TempDir(), "fixture", nil)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 4*time.Second {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestAntigravityImageQuotaErrorSources(t *testing.T) {
	for _, message := range []string{"RESOURCE_EXHAUSTED", "MODEL_CAPACITY_EXHAUSTED", "individual quota reached", "quota exceeded", "quota exhausted", "usage limit", "rate limit", "rate_limit", "too many requests", "code 429", "http: 429", "status=429"} {
		if !antigravityImageQuotaError(message) {
			t.Errorf("missing quota discriminator: %s", message)
		}
	}
	for _, message := range []string{"image failed to generate: no image generated in response", "connection reset", "http 4290", "quota remaining: 1", "no rate limiting"} {
		if antigravityImageQuotaError(message) {
			t.Errorf("false quota discriminator: %s", message)
		}
	}
	dir := antigravityImageTestDir(t)
	path := filepath.Join(dir, "agy.log")
	for _, line := range []string{"I1102 agent.go:10] prompt: quota exceeded", "E1102 agent.go:10] prompt: quota exceeded", "assistant: agent executor error: quota exceeded"} {
		os.WriteFile(path, []byte(line), 0600)
		if antigravityImageLogLimited(dir, path) {
			t.Fatalf("untrusted quota text: %q", line)
		}
	}
}

func TestAntigravityImageRecordedContracts(t *testing.T) {
	for _, name := range []string{"generate", "edit"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "antigravity-image-"+name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			root := antigravityImageTestDir(t)
			raw = bytes.ReplaceAll(raw, []byte("__APP_DATA__"), []byte(filepath.ToSlash(root)))
			var fixture struct {
				Events     []json.RawMessage `json:"events"`
				Transcript []json.RawMessage `json:"transcript"`
				Parent     string            `json:"parent"`
				Child      string            `json:"child"`
				File       string            `json:"file"`
			}
			if err := json.Unmarshal(raw, &fixture); err != nil {
				t.Fatal(err)
			}
			artifact := filepath.Join(root, "brain", fixture.Parent, fixture.File)
			os.MkdirAll(filepath.Dir(artifact), 0700)
			os.WriteFile(artifact, []byte("image fixture"), 0600)
			transcriptPath := filepath.Join(root, "brain", fixture.Child, ".system_generated", "logs", "transcript.jsonl")
			os.MkdirAll(filepath.Dir(transcriptPath), 0700)
			transcript := antigravityImageJSONLines(fixture.Transcript)
			os.WriteFile(transcriptPath, transcript, 0600)
			state, err := parseAntigravityImageStream(antigravityImageJSONLines(fixture.Events))
			if err != nil {
				t.Fatal(err)
			}
			result, err := state.receipt(root)
			if err != nil || result.Path != artifact || result.RateLimited {
				t.Fatalf("recorded receipt=%+v error=%v", result, err)
			}
			// A view_file GENERIC record also contains media. It must never be
			// mistaken for an image generated by the native tool.
			noGeneration := bytes.ReplaceAll(transcript, []byte(`"generate_image"`), []byte(`"view_file"`))
			result, err = parseAntigravityImageTranscript(noGeneration, filepath.Dir(artifact))
			if err != nil || result.Path != "" {
				t.Fatalf("accepted read-image receipt: %+v %v", result, err)
			}
		})
	}
}

func antigravityImageJSONLines(records []json.RawMessage) []byte {
	var out bytes.Buffer
	for _, record := range records {
		_ = json.NewEncoder(&out).Encode(record)
	}
	return out.Bytes()
}

func TestAntigravityImageTranscriptBoundaries(t *testing.T) {
	for _, mode := range []string{"valid", "last-success", "content-receipt", "outside", "symlink", "directory", "empty", "foreign-parent", "unpaired", "wrong-index", "system-source", "remote-uri", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			root := antigravityImageTestDir(t)
			output := filepath.Join(root, "output.png")
			os.WriteFile(output, []byte("image"), 0600)
			path := output
			switch mode {
			case "outside", "foreign-parent":
				path = filepath.Join(t.TempDir(), "old.png")
				os.WriteFile(path, []byte("old image"), 0600)
			case "symlink":
				os.Remove(output)
				outside := filepath.Join(t.TempDir(), "secret.png")
				os.WriteFile(outside, []byte("host secret"), 0600)
				if err := os.Symlink(outside, output); err != nil {
					t.Skip(err)
				}
			case "directory":
				os.Remove(output)
				os.Mkdir(output, 0700)
			case "empty":
				os.WriteFile(output, nil, 0600)
			}
			uri := "file://" + filepath.ToSlash(path)
			if mode == "remote-uri" {
				uri = "https://example.test/image.png"
			}
			call := map[string]any{"step_index": 1, "source": "MODEL", "type": "PLANNER_RESPONSE", "status": "DONE", "tool_calls": []map[string]string{{"name": "generate_image"}}}
			receipt := map[string]any{"step_index": 2, "source": "MODEL", "type": "GENERIC", "status": "DONE", "media": []map[string]string{{"mime_type": "image/png", "uri": uri}}}
			if mode == "unpaired" {
				call["tool_calls"] = []map[string]string{{"name": "view_file"}}
			}
			if mode == "wrong-index" {
				receipt["step_index"] = 4
			}
			if mode == "system-source" {
				receipt["source"] = "SYSTEM"
			}
			if mode == "content-receipt" {
				delete(receipt, "media")
				receipt["content"] = "Created At: fixture\nGenerated image is saved at " + path + "."
			}
			var raw bytes.Buffer
			e := json.NewEncoder(&raw)
			e.Encode(call)
			e.Encode(receipt)
			if mode == "last-success" {
				path = filepath.Join(root, "last.png")
				os.WriteFile(path, []byte("last image"), 0600)
				call["step_index"] = 3
				receipt["step_index"] = 4
				receipt["media"] = []map[string]string{{"mime_type": "image/png", "uri": "file://" + filepath.ToSlash(path)}}
				e.Encode(call)
				e.Encode(receipt)
			}
			if mode == "truncated" {
				raw.WriteString(`{"step_index":`)
			}
			result, err := parseAntigravityImageTranscript(raw.Bytes(), root)
			switch mode {
			case "valid", "last-success", "content-receipt":
				if err != nil || result.Path != path {
					t.Fatalf("receipt=%+v err=%v", result, err)
				}
			case "unpaired", "wrong-index", "system-source":
				if err != nil || result.Path != "" {
					t.Fatalf("untrusted native receipt: %+v %v", result, err)
				}
			default:
				if err == nil {
					t.Fatalf("invalid receipt accepted: %+v", result)
				}
			}
		})
	}
}

func TestAntigravityImageStreamIsolation(t *testing.T) {
	for _, mode := range []string{"foreign-result", "foreign-child", "traversal", "multiple-children", "conflicting-init", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			var raw bytes.Buffer
			e := json.NewEncoder(&raw)
			e.Encode(map[string]any{"event": "init", "conversation_id": agyimage.Parent})
			parent := agyimage.Parent
			id := agyimage.Child
			if mode == "foreign-result" || mode == "foreign-child" {
				parent = agyimage.Child
			}
			if mode == "traversal" {
				id = "../../host"
			}
			e.Encode(map[string]any{"event": "step_update", "step_update": map[string]any{"conversation_id": parent, "state": "DONE", "step_type": "subagent", "tool_name": "invoke_subagent", "subagent_info": map[string]any{"subagents": []map[string]string{{"type_name": "image-generator", "conversation_id": id, "log_uri": "file:///host/secret"}}}}})
			e.Encode(map[string]any{"event": "result", "result": map[string]string{"conversation_id": parent, "status": "ERROR", "error": "RESOURCE_EXHAUSTED"}})
			if mode == "multiple-children" {
				e.Encode(map[string]any{"event": "step_update", "step_update": map[string]any{"conversation_id": agyimage.Parent, "state": "DONE", "step_type": "subagent", "tool_name": "invoke_subagent", "subagent_info": map[string]any{"subagents": []map[string]string{{"type_name": "image-generator", "conversation_id": "11111111-1111-4111-8111-111111111111"}}}}})
			}
			if mode == "conflicting-init" {
				e.Encode(map[string]any{"event": "init", "conversation_id": agyimage.Child})
			}
			if mode == "malformed" {
				raw.WriteString("not-json\n")
			}
			state, err := parseAntigravityImageStream(raw.Bytes())
			if strings.HasPrefix(mode, "foreign-") {
				if err != nil || state.child != "" || state.rateLimited {
					t.Fatalf("foreign event accepted: %+v %v", state, err)
				}
			} else if err == nil {
				t.Fatalf("unsafe stream accepted: %+v", state)
			}
		})
	}
}

func TestAntigravityImageTranscriptDiscovery(t *testing.T) {
	for _, mode := range []string{"parent", "child", "escaped-transcript", "escaped-brain", "tail-quota"} {
		t.Run(mode, func(t *testing.T) {
			root := antigravityImageTestDir(t)
			brain := filepath.Join(root, "brain")
			if mode == "escaped-brain" {
				if err := os.Symlink(t.TempDir(), brain); err != nil {
					t.Skip(err)
				}
			}
			output := filepath.Join(brain, agyimage.Parent, "generated.png")
			if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(output, []byte("image"), 0600); err != nil {
				t.Fatal(err)
			}
			state := antigravityImageStream{parent: agyimage.Parent, completed: true}
			producer := state.parent
			if mode != "parent" {
				state.child = agyimage.Child
				producer = state.child
			}
			transcript := filepath.Join(brain, producer, ".system_generated", "logs", "transcript.jsonl")
			if err := os.MkdirAll(filepath.Dir(transcript), 0700); err != nil {
				t.Fatal(err)
			}
			var raw bytes.Buffer
			e := json.NewEncoder(&raw)
			e.Encode(map[string]any{"step_index": 1, "source": "MODEL", "type": "PLANNER_RESPONSE", "status": "DONE", "tool_calls": []map[string]string{{"name": "generate_image"}}})
			e.Encode(map[string]any{"step_index": 2, "source": "MODEL", "type": "GENERIC", "status": "DONE", "content": "Generated image is saved at " + output + "."})
			if mode == "tail-quota" {
				e.Encode(map[string]any{"step_index": 3, "source": "MODEL", "type": "PLANNER_RESPONSE", "status": "DONE", "tool_calls": []map[string]string{{"name": "generate_image"}}})
				e.Encode(map[string]any{"step_index": 4, "source": "MODEL", "type": "GENERIC", "status": "ERROR", "error": "RESOURCE_EXHAUSTED"})
			}
			if mode == "escaped-transcript" {
				target := filepath.Join(t.TempDir(), "host-transcript.jsonl")
				if err := os.WriteFile(target, raw.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, transcript); err != nil {
					t.Skip(err)
				}
			} else if err := os.WriteFile(transcript, raw.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			result, err := state.receipt(root)
			if strings.HasPrefix(mode, "escaped-") {
				if err == nil {
					t.Fatal("read a transcript outside the private home")
				}
			} else if err != nil || result.Path != output || result.RateLimited {
				t.Fatalf("receipt=%+v err=%v", result, err)
			}
		})
	}
}
