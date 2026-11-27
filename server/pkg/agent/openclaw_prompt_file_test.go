package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOpenclawPromptFile(t *testing.T) {
	prompt := "<request>Привет\r\n\"quoted\" & %PATH%</request>"
	original := []string{"agent", "--json", "--message", prompt}
	for _, tc := range []struct {
		name, platform, version string
		file                    bool
	}{
		{"windows_supported", "windows", "2026.6.11", true},
		{"windows_newer", "windows", "2026.7.1", true},
		{"windows_older", "windows", "2026.5.5", false},
		{"unix_unchanged", "darwin", "2026.7.1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, cleanup, err := prepareOpenclawPromptFile(original, tc.platform, tc.version)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			if !reflect.DeepEqual(original, []string{"agent", "--json", "--message", prompt}) {
				t.Fatal("input args mutated")
			}
			if !tc.file {
				if !reflect.DeepEqual(args, original) {
					t.Fatalf("args = %v", args)
				}
				return
			}
			if len(args) != 4 || args[2] != "--message-file" {
				t.Fatalf("args = %v", args)
			}
			data, err := os.ReadFile(args[3])
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != prompt {
				t.Fatal("prompt bytes changed")
			}
			cleanup()
			if _, err := os.Stat(args[3]); !os.IsNotExist(err) {
				t.Fatalf("prompt file remains: %v", err)
			}
		})
	}
}

func runFakeOpenclawPromptFile() {
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("OpenClaw 2026.7.1")
		return
	}
	var path string
	for i, arg := range args {
		if arg == "--message-file" && i+1 < len(args) {
			path = args[i+1]
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	record, err := json.Marshal(struct {
		Args   []string
		Path   string
		Prompt string
	}{args, path, string(data)})
	if err != nil {
		os.Exit(1)
	}
	if err := os.WriteFile(os.Getenv("MULTICA_OPENCLAW_PROMPT_TEST_RECORD"), record, 0o600); err != nil {
		os.Exit(1)
	}
	fmt.Println(`{"payloads":[{"text":"ok"}]}`)
}

func TestOpenclawPromptFileRejectsCustomTransport(t *testing.T) {
	args := buildOpenclawArgs("owned", "session", ExecOptions{CustomArgs: []string{"-m", "foreign", "--message-file", "foreign.txt"}}, slog.Default())
	for _, arg := range args {
		if arg == "foreign" || arg == "foreign.txt" || arg == "--message-file" || arg == "-m" {
			t.Fatalf("custom transport survived: %v", args)
		}
	}
	if args[len(args)-1] != "owned" {
		t.Fatal("daemon prompt lost")
	}
}

func TestOpenclawPromptFileCreationFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, missing)
	}
	_, cleanup, err := prepareOpenclawPromptFile([]string{"agent", "--message", "prompt"}, "windows", "2026.7.1")
	cleanup()
	if err == nil {
		t.Fatal("expected missing temp directory error")
	}
}
