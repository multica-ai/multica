// Package agyimage provides a test-owned CLI using the agy 1.4.12 image wire
// shapes captured on agent-gcp-01. It never invokes an installed provider.
package agyimage

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const ModeEnv = "AGY_IMAGE_TEST_MODE"
const Parent = "cc218d35-012d-40c5-8c94-1d5ac9ad500c"
const Child = "3a158fd6-159e-48b4-b396-c2667e16bdc2"
const TokenPath = ".gemini/antigravity-cli/antigravity-oauth-token"
const Token = `{"auth_method":"consumer","token":{"access_token":"test-host-only"}}`

func Run() {
	mode := os.Getenv(ModeEnv)
	if os.Getenv("MULTICA_TOKEN") != "" {
		os.Exit(14)
	}
	if slices.Contains(os.Args, "--version") {
		fmt.Println("1.4.12")
		return
	}
	count := func(value string) {
		if path := os.Getenv("AGY_IMAGE_TEST_COUNTER"); path != "" {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			must(err)
			_, err = fmt.Fprintln(f, value)
			must(err)
			must(f.Close())
		}
	}
	if slices.Contains(os.Args, "models") {
		count("probe")
		switch mode {
		case "catalog-fail":
			os.Exit(1)
		case "catalog-empty":
			return
		case "cancel":
			for {
				time.Sleep(time.Second)
			}
		}
		fmt.Println("gemini-fixture\tGemini Fixture")
		return
	}
	count("generate")
	if mode == "cancel" {
		for {
			time.Sleep(time.Second)
		}
	}
	if mode == "eof" {
		return
	}
	home := os.Getenv("HOME")
	cwd, err := os.Getwd()
	must(err)
	if home != filepath.Join(cwd, "agy-home") || os.Getenv("USERPROFILE") != home {
		os.Exit(10)
	}
	info, err := os.Lstat(filepath.Join(home, TokenPath))
	must(err)
	if info.Mode()&os.ModeSymlink == 0 {
		os.Exit(11)
	}
	for _, flag := range []string{"--disable-slash-commands", "--output-format", "--print-timeout", "--log-file", "-p"} {
		if !slices.Contains(os.Args, flag) {
			os.Exit(12)
		}
	}
	prompt := os.Args[len(os.Args)-1]
	if mode == "edit" && (!strings.Contains(prompt, "input-0") || !strings.Contains(prompt, "ImagePaths") || !strings.Contains(prompt, "subject")) {
		os.Exit(13)
	}
	brain := filepath.Join(home, ".gemini", "antigravity-cli", "brain")
	output := filepath.Join(brain, Parent, "native.png")
	must(os.MkdirAll(filepath.Dir(output), 0700))
	write := func(v any) { must(json.NewEncoder(os.Stdout).Encode(v)) }
	write(map[string]any{"event": "init", "conversation_id": Parent, "init": map[string]any{"tools": []string{"invoke_subagent", "generate_image"}}})
	write(map[string]any{"event": "step_update", "step_update": map[string]any{
		"conversation_id": Parent, "step_index": 2, "state": "DONE", "step_type": "subagent", "tool_name": "invoke_subagent",
		"subagent_info": map[string]any{"subagents": []map[string]string{{"type_name": "image-generator", "conversation_id": Child, "log_uri": "file://" + filepath.ToSlash(filepath.Join(brain, Child, ".system_generated", "logs", "transcript.jsonl"))}}},
	}})
	status, failure := "SUCCESS", ""
	if mode == "quota-result" || mode == "tail-quota" {
		status, failure = "ERROR", "RESOURCE_EXHAUSTED: quota exceeded"
	}
	if mode == "quota-log" {
		status = "ERROR"
		must(os.WriteFile(filepath.Join(cwd, "agy.log"), []byte("E1102 00:00:00 generate_image.go:170] error generating image: http 429\n"), 0600))
	}
	if mode != "quota-result" && mode != "quota-log" && mode != "prose" {
		transcript := filepath.Join(brain, Child, ".system_generated", "logs", "transcript.jsonl")
		must(os.MkdirAll(filepath.Dir(transcript), 0700))
		f, err := os.Create(transcript)
		must(err)
		e := json.NewEncoder(f)
		must(e.Encode(map[string]any{"step_index": 1, "source": "MODEL", "type": "PLANNER_RESPONSE", "status": "DONE", "tool_calls": []map[string]any{{"name": "generate_image", "args": map[string]string{"Prompt": "\"fixture\""}}}}))
		if mode == "quota-transcript" {
			must(e.Encode(map[string]any{"step_index": 2, "source": "MODEL", "type": "GENERIC", "status": "ERROR", "error": "MODEL_CAPACITY_EXHAUSTED"}))
		} else {
			if mode == "escape" {
				target, err := filepath.EvalSymlinks(filepath.Join(home, TokenPath))
				must(err)
				must(os.Symlink(target, output))
			} else {
				imageFile, err := os.Create(output)
				must(err)
				must(png.Encode(imageFile, image.NewRGBA(image.Rect(0, 0, 3, 3))))
				must(imageFile.Close())
			}
			uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(output)}).String()
			must(e.Encode(map[string]any{"step_index": 2, "source": "MODEL", "type": "GENERIC", "status": "DONE", "content": "Generated image is saved at " + output + ".", "media": []map[string]string{{"mime_type": "image/png", "uri": uri}}}))
		}
		must(f.Close())
	}
	write(map[string]any{"event": "result", "result": map[string]any{"conversation_id": Parent, "status": status, "error": failure, "response": "Generated image is saved at " + output + "."}})
	if status == "ERROR" {
		os.Exit(1)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(9)
	}
}
