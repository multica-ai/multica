package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const envOutputTestSecret = "ghp_DO_NOT_ECHO_THIS_TOKEN"

func newAgentEnvOutputTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agents/agent-123/env" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if r.Method == http.MethodPut {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode PUT body: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"agent_id":   "agent-123",
			"custom_env": map[string]any{"GITHUB_TOKEN": envOutputTestSecret},
		})
	}))
	t.Cleanup(srv.Close)
	setCLITestServerEnv(t, srv.URL)
	return srv
}

func TestAgentEnvOutputMasksValuesByDefault(t *testing.T) {
	for _, output := range []string{"json", "table"} {
		t.Run("get "+output, func(t *testing.T) {
			newAgentEnvOutputTestServer(t)
			cmd := &cobra.Command{Use: "get"}
			cmd.Flags().String("output", output, "")
			got, err := captureStdout(t, func() error { return runAgentEnvGet(cmd, []string{"agent-123"}) })
			if err != nil {
				t.Fatalf("runAgentEnvGet: %v", err)
			}
			if strings.Contains(got, envOutputTestSecret) {
				t.Fatalf("env get %s output echoed the secret: %s", output, got)
			}
			if !strings.Contains(got, "GITHUB_TOKEN") {
				t.Fatalf("env get %s output lost the key name: %s", output, got)
			}
		})
	}

	newAgentEnvOutputTestServer(t)
	cmd := &cobra.Command{Use: "set"}
	cmd.Flags().String("custom-env", "", "")
	cmd.Flags().Bool("custom-env-stdin", false, "")
	cmd.Flags().String("custom-env-file", "", "")
	cmd.Flags().String("output", "json", "")
	if err := cmd.Flags().Set("custom-env", `{"GITHUB_TOKEN":"`+envOutputTestSecret+`"}`); err != nil {
		t.Fatalf("set custom-env flag: %v", err)
	}
	got, err := captureStdout(t, func() error { return runAgentEnvSet(cmd, []string{"agent-123"}) })
	if err != nil {
		t.Fatalf("runAgentEnvSet: %v", err)
	}
	if strings.Contains(got, envOutputTestSecret) {
		t.Fatalf("env set output echoed the secret: %s", got)
	}
	if !strings.Contains(got, "GITHUB_TOKEN") {
		t.Fatalf("env set output lost the key name: %s", got)
	}
}

func TestAgentEnvOutputCanRevealValuesOnRequest(t *testing.T) {
	newAgentEnvOutputTestServer(t)
	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().String("output", "json", "")
	cmd.Flags().Bool("show-values", false, "")
	if err := cmd.Flags().Set("show-values", "true"); err != nil {
		t.Fatalf("set show-values flag: %v", err)
	}
	got, err := captureStdout(t, func() error { return runAgentEnvGet(cmd, []string{"agent-123"}) })
	if err != nil {
		t.Fatalf("runAgentEnvGet: %v", err)
	}
	if !strings.Contains(got, envOutputTestSecret) {
		t.Fatalf("--show-values did not reveal the value: %s", got)
	}
}

func TestAgentEnvSetWarnsOnMaskedInput(t *testing.T) {
	var buf bytes.Buffer
	warnAgentEnvMaskedInput(&buf, "agent-123", map[string]string{
		"B_TOKEN": agentEnvMaskedValue,
		"A_TOKEN": agentEnvMaskedValue,
		"PLAIN":   envOutputTestSecret,
	})
	got := buf.String()
	if !strings.Contains(got, "A_TOKEN, B_TOKEN") {
		t.Fatalf("warning should list masked keys in sorted order: %s", got)
	}
	if !strings.Contains(got, "--show-values") {
		t.Fatalf("warning should point at --show-values: %s", got)
	}
	if strings.Contains(got, "PLAIN") || strings.Contains(got, envOutputTestSecret) {
		t.Fatalf("warning should mention only masked keys and never values: %s", got)
	}

	buf.Reset()
	warnAgentEnvMaskedInput(&buf, "agent-123", map[string]string{"PLAIN": envOutputTestSecret})
	if buf.Len() != 0 {
		t.Fatalf("no masked values should produce no warning, got: %s", buf.String())
	}
}
