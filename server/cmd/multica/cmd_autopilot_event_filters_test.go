package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func resetAutopilotFilterFlags(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		value, changed := f.Value.String(), f.Changed
		t.Cleanup(func() { _ = f.Value.Set(value); f.Changed = changed })
		if err := f.Value.Set(f.DefValue); err != nil {
			t.Fatal(err)
		}
		f.Changed = false
	})
}

func TestAutopilotEventFiltersRequests(t *testing.T) {
	const filters = `[{"event":"pull_request_review","actions":["submitted"]},{"event":"push"}]`
	for _, tc := range []struct {
		name, file, label string
		create, clear     bool
	}{
		{name: "create", create: true, file: filters},
		{name: "replace", file: filters},
		{name: "preserve on unrelated update", label: "Renamed"},
		{name: "clear explicitly", clear: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := autopilotTriggerUpdateCmd
			if tc.create {
				cmd = autopilotTriggerAddCmd
			}
			resetAutopilotFilterFlags(t, cmd)
			if tc.create {
				if err := cmd.Flags().Set("kind", "webhook"); err != nil {
					t.Fatal(err)
				}
			}
			if tc.file != "" {
				path := filepath.Join(t.TempDir(), "filters.json")
				if err := os.WriteFile(path, []byte(tc.file), 0600); err != nil {
					t.Fatal(err)
				}
				if err := cmd.Flags().Set("event-filters-file", path); err != nil {
					t.Fatal(err)
				}
			}
			if tc.clear {
				if err := cmd.Flags().Set("clear-event-filters", "true"); err != nil {
					t.Fatal(err)
				}
			}
			if tc.label != "" {
				if err := cmd.Flags().Set("label", tc.label); err != nil {
					t.Fatal(err)
				}
			}
			var got map[string]any
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				wantMethod, wantPath := http.MethodPatch, "/api/autopilots/11111111-1111-1111-1111-111111111111/triggers/22222222-2222-2222-2222-222222222222"
				if tc.create {
					wantMethod, wantPath = http.MethodPost, "/api/autopilots/11111111-1111-1111-1111-111111111111/triggers"
				}
				if r.Method != wantMethod || r.URL.Path != wantPath {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "22222222-2222-2222-2222-222222222222", "kind": "webhook", "event_filters": got["event_filters"]})
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			_, err := captureStdout(t, func() error {
				if tc.create {
					return runAutopilotTriggerAdd(cmd, []string{"11111111-1111-1111-1111-111111111111"})
				}
				return runAutopilotTriggerUpdate(cmd, []string{"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"})
			})
			if err != nil {
				t.Fatal(err)
			}
			if requests != 1 {
				t.Fatalf("requests = %d, want one write", requests)
			}
			if tc.file != "" {
				var want any
				_ = json.Unmarshal([]byte(filters), &want)
				if !reflect.DeepEqual(got["event_filters"], want) {
					t.Fatalf("filters = %#v, want %#v", got["event_filters"], want)
				}
			} else if tc.clear {
				if value, ok := got["event_filters"].([]any); !ok || len(value) != 0 {
					t.Fatalf("clear must send [], got %#v", got)
				}
			} else if _, present := got["event_filters"]; present {
				t.Fatalf("unrelated update changed filters: %#v", got)
			}
		})
	}
}

func TestAutopilotEventFiltersRejectInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name, file, want string
		clear, create    bool
	}{
		{name: "null", file: `null`, want: "array"},
		{name: "empty array", file: `[]`, want: "clear-event-filters"},
		{name: "object", file: `{"event":"push"}`, want: "event-filters-file"},
		{name: "trailing document", file: `[{"event":"push"}] []`, want: "event-filters-file"},
		{name: "unknown field", file: `[{"event":"push","action":["opened"]}]`, want: "event-filters-file"},
		{name: "missing event", file: `[{"actions":["opened"]}]`, want: "event"},
		{name: "blank event", file: `[{"event":" "}]`, want: "event"},
		{name: "blank action", file: `[{"event":"push","actions":[" "]}]`, want: "actions"},
		{name: "null row", file: `[null]`, want: "event"},
		{name: "missing file", want: "event-filters-file"},
		{name: "conflicting clear", file: `[{"event":"push"}]`, clear: true, want: "cannot"},
		{name: "schedule", file: `[{"event":"push"}]`, create: true, want: "webhook"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := autopilotTriggerUpdateCmd
			if tc.create {
				cmd = autopilotTriggerAddCmd
			}
			resetAutopilotFilterFlags(t, cmd)
			path := filepath.Join(t.TempDir(), "filters.json")
			if tc.file != "" {
				if err := os.WriteFile(path, []byte(tc.file), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := cmd.Flags().Set("event-filters-file", path); err != nil {
				t.Fatal(err)
			}
			if tc.clear {
				if err := cmd.Flags().Set("clear-event-filters", "true"); err != nil {
					t.Fatal(err)
				}
			}
			if tc.create {
				_ = cmd.Flags().Set("cron", "0 9 * * *")
			}
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; http.Error(w, "unexpected request", 500) }))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			var err error
			if tc.create {
				err = runAutopilotTriggerAdd(cmd, []string{"11111111-1111-1111-1111-111111111111"})
			} else {
				err = runAutopilotTriggerUpdate(cmd, []string{"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"})
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if requests != 0 {
				t.Fatalf("invalid input made %d requests", requests)
			}
		})
	}
}

func TestAutopilotEventFiltersServerRejection(t *testing.T) {
	cmd := autopilotTriggerUpdateCmd
	resetAutopilotFilterFlags(t, cmd)
	if err := cmd.Flags().Set("clear-event-filters", "true"); err != nil {
		t.Fatal(err)
	}
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"event_filters is only valid for webhook triggers"}`))
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	err := runAutopilotTriggerUpdate(cmd, []string{"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"})
	if err == nil || !strings.Contains(err.Error(), "event_filters is only valid for webhook triggers") {
		t.Fatalf("server rejection lost: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one rejected write", requests)
	}
}
