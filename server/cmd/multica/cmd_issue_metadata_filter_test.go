package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Each argv gets a fresh production command tree, including its flag types.
func TestIssueMetadataFilterArgvProcess(t *testing.T) {
	if os.Getenv("MULTICA_TEST_METADATA_ARGV") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing CLI argv")
	}
	// TestMain clears inherited credentials; use only this local fake API.
	t.Setenv("MULTICA_SERVER_URL", os.Getenv("MULTICA_TEST_METADATA_SERVER"))
	t.Setenv("MULTICA_TOKEN", "mat_SYNTHETIC_ONLY")
	t.Setenv("MULTICA_WORKSPACE_ID", "11111111-1111-4111-8111-111111111111")
	rootCmd.SetArgs(os.Args[separator+1:])
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestIssueMetadataFilterLiteralArgv(t *testing.T) {
	const since = "2026-11-05T10:00:00.123456789+08:00"
	const until = "2026-11-05T02:01:00.987654321Z"
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name           string
		flags          []string
		want           map[string]any
		wantErr        string
		wantQuery      url.Values
		wantProperties map[string][]any
	}{
		{name: "numeric string", flags: []string{"--metadata", `external_id="42"`}, want: map[string]any{"external_id": "42"}},
		{name: "literal comma", flags: []string{"--metadata", "note=Smith, John"}, want: map[string]any{"note": "Smith, John"}},
		{name: "comma and equals", flags: []string{"--metadata", "note=a,b=c"}, want: map[string]any{"note": "a,b=c"}},
		{name: "JSON string with comma", flags: []string{"--metadata", `note="Smith, John"`}, want: map[string]any{"note": "Smith, John"}},
		{name: "literal quote", flags: []string{"--metadata", `note=say"hi`}, want: map[string]any{"note": `say"hi`}},
		{name: "flag equals form", flags: []string{`--metadata=note="Smith, John"`}, want: map[string]any{"note": "Smith, John"}},
		{name: "repeated flags AND", flags: []string{"--metadata", "note=Smith, John", "--metadata", "phase=ready"}, want: map[string]any{"note": "Smith, John", "phase": "ready"}},
		{name: "existing scalar types", flags: []string{"--metadata", "count=42", "--metadata", "blocked=true", "--metadata", "ready=false", "--metadata", "phase=waiting"}, want: map[string]any{"count": float64(42), "blocked": true, "ready": false, "phase": "waiting"}},
		{name: "old CSV grouping is a literal value", flags: []string{"--metadata", "phase=ready,blocked=true"}, want: map[string]any{"phase": "ready,blocked=true"}},
		{name: "migrated CSV grouping", flags: []string{"--metadata", "phase=ready", "--metadata", "blocked=true"}, want: map[string]any{"phase": "ready", "blocked": true}},
		{name: "duplicate key", flags: []string{"--metadata", "phase=ready", "--metadata", "phase=waiting"}, wantErr: `key "phase" given more than once`},
		{name: "missing equals", flags: []string{"--metadata", "phase"}, wantErr: "must be in key=value form"},
		{name: "empty key", flags: []string{"--metadata", "=ready"}, wantErr: "must be in key=value form"},
		{
			name:           "literal metadata with updated range and property filters",
			flags:          []string{"--metadata", `external_id="42"`, "--metadata", "note=a,b=c", "--metadata", "empty=null", "--updated-since", since, "--updated-until", until, "--property", "Notes=Smith, John", "--property", "Score>=3"},
			want:           map[string]any{"external_id": "42", "note": "a,b=c", "empty": "null"},
			wantQuery:      url.Values{"date_field": {"updated_at"}, "date_start": {since}, "date_end": {until}},
			wantProperties: map[string][]any{testNotesDefID: {"Smith, John"}, testScoreDefID: {opMember("gte", "3")}},
		},
		{name: "literal metadata with missing updated bound", flags: []string{"--metadata", "note=a,b=c", "--updated-since", since}, wantErr: "--updated-since and --updated-until must be set together"},
		{name: "literal metadata with reversed updated bounds", flags: []string{"--metadata", "note=a,b=c", "--updated-since", until, "--updated-until", since}, wantErr: "--updated-since must be before --updated-until"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var filters []map[string]any
			var queries []url.Values
			var propertyCalls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/api/properties" && tc.wantProperties != nil {
					mu.Lock()
					propertyCalls++
					mu.Unlock()
					_ = json.NewEncoder(w).Encode(map[string]any{"properties": propertyFilterTestCatalog()})
					return
				}
				if r.Method != http.MethodGet || r.URL.Path != "/api/issues" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				var filter map[string]any
				if err := json.Unmarshal([]byte(r.URL.Query().Get("metadata")), &filter); err != nil {
					t.Errorf("decode metadata query: %v", err)
				}
				mu.Lock()
				filters = append(filters, filter)
				queries = append(queries, r.URL.Query())
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"issues":[],"total":0}`))
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			argv := append([]string{"-test.run=^TestIssueMetadataFilterArgvProcess$", "--", "issue", "list", "--output", "json"}, tc.flags...)
			child := exec.CommandContext(ctx, executable, argv...)
			child.Dir = t.TempDir()
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "MULTICA_") {
					child.Env = append(child.Env, entry)
				}
			}
			child.Env = append(child.Env, "MULTICA_TEST_METADATA_ARGV=1", "MULTICA_TEST_METADATA_SERVER="+server.URL)
			output, err := child.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("CLI timed out: %s", output)
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(string(output), tc.wantErr) {
					t.Errorf("err=%v, want %q; output: %s", err, tc.wantErr, output)
				}
			} else if err != nil {
				t.Errorf("CLI failed: %v; output: %s", err, output)
			}
			mu.Lock()
			defer mu.Unlock()
			var want []map[string]any
			if tc.wantErr == "" {
				want = []map[string]any{tc.want}
			}
			if !reflect.DeepEqual(filters, want) {
				t.Errorf("sent metadata filters = %#v, want %#v", filters, want)
			}
			wantPropertyCalls := 0
			if tc.wantProperties != nil {
				wantPropertyCalls = 1
				if got := decodePropertiesParam(t, queries); !reflect.DeepEqual(got, tc.wantProperties) {
					t.Errorf("sent property filters = %#v, want %#v", got, tc.wantProperties)
				}
			}
			if propertyCalls != wantPropertyCalls {
				t.Errorf("property catalog calls = %d, want %d", propertyCalls, wantPropertyCalls)
			}
			for _, query := range queries {
				query.Del("metadata")
				query.Del("properties")
				wantQuery := url.Values{"workspace_id": {"11111111-1111-4111-8111-111111111111"}, "limit": {"50"}}
				for key, values := range tc.wantQuery {
					wantQuery[key] = values
				}
				if !reflect.DeepEqual(query, wantQuery) {
					t.Errorf("other query parameters = %v, want %v", query, wantQuery)
				}
			}
		})
	}
}
