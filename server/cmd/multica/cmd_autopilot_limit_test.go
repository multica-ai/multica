package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func scheduleLimitCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "trigger"}
	cmd.Flags().String("kind", "schedule", "")
	cmd.Flags().String("cron", "* * * * *", "")
	cmd.Flags().String("timezone", "", "")
	cmd.Flags().String("label", "", "")
	cmd.Flags().Bool("enabled", true, "")
	cmd.Flags().Int64("execution-limit", 0, "")
	cmd.Flags().Bool("unlimited", false, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func TestAutopilotScheduleLimitCLIRequests(t *testing.T) {
	const apID = "11111111-1111-1111-1111-111111111111"
	const trigID = "22222222-2222-2222-2222-222222222222"
	for _, tc := range []struct {
		name    string
		flags   []string
		value   any
		present bool
		add     bool
	}{
		{"set", []string{"--execution-limit", "4"}, float64(4), true, false},
		{"clear", []string{"--unlimited"}, nil, true, false},
		{"preserve", []string{"--label", "renamed"}, nil, false, false},
		{"create-once", []string{"--execution-limit", "1"}, float64(1), true, true},
		{"create-default", nil, nil, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					http.NotFound(w, r)
					return
				}
				called = true
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				got, ok := body["execution_limit"]
				if ok != tc.present || got != tc.value {
					t.Errorf("execution_limit=%v present=%v; want %v present=%v", got, ok, tc.value, tc.present)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"id":"` + trigID + `"}`))
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			t.Setenv("MULTICA_TOKEN", "mat_schedule_limit_test")
			cmd := scheduleLimitCommand()
			if err := cmd.ParseFlags(tc.flags); err != nil {
				t.Fatal(err)
			}
			_, err := captureStdout(t, func() error {
				if tc.add {
					return runAutopilotTriggerAdd(cmd, []string{apID})
				}
				return runAutopilotTriggerUpdate(cmd, []string{apID, trigID})
			})
			if err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("no request sent")
			}
		})
	}
}

func TestAutopilotScheduleLimitCLIRejectsInvalid(t *testing.T) {
	for _, flags := range [][]string{{"--execution-limit", "0"}, {"--execution-limit", "-1"}, {"--execution-limit", "2147483648"}, {"--execution-limit", "1", "--unlimited"}} {
		cmd := scheduleLimitCommand()
		if err := cmd.ParseFlags(flags); err != nil {
			t.Fatal(err)
		}
		if err := applyAutopilotExecutionLimitFlags(cmd, map[string]any{}); err == nil {
			t.Fatalf("accepted invalid flags %v", flags)
		}
	}
}

func TestAutopilotScheduleLimitCLIListsExhaustion(t *testing.T) {
	const apID = "11111111-1111-1111-1111-111111111111"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"triggers":[{"id":"22222222-2222-2222-2222-222222222222","kind":"schedule","enabled":true,"execution_limit":1,"execution_count":1,"exhausted":true}]}`))
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_schedule_limit_test")
	out, err := captureStdout(t, func() error { return runAutopilotTriggerList(newAutopilotTriggerListTestCmd("table"), []string{apID}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "EXECUTIONS") || !strings.Contains(out, "1/1 (completed)") {
		t.Fatalf("missing exhaustion: %s", out)
	}
}
