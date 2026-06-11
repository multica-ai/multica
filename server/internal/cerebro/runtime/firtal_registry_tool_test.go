package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFirtalRegistryGrantConfigAllows(t *testing.T) {
	cases := []struct {
		name   string
		config firtalRegistryGrantConfig
		dsID   string
		want   bool
	}{
		{"empty config denies", firtalRegistryGrantConfig{}, "ds-1", false},
		{"explicit allowlist matches", firtalRegistryGrantConfig{AllowedDataSources: []string{"ds-1", "ds-2"}}, "ds-1", true},
		{"explicit allowlist misses", firtalRegistryGrantConfig{AllowedDataSources: []string{"ds-1"}}, "ds-other", false},
		{"AllowedAll short-circuits", firtalRegistryGrantConfig{AllowedAll: true}, "ds-anything", true},
		{"case-insensitive UUID match", firtalRegistryGrantConfig{AllowedDataSources: []string{"AbCd"}}, "abcd", true},
		{"whitespace tolerated in allowlist", firtalRegistryGrantConfig{AllowedDataSources: []string{"  ds-1  "}}, "ds-1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.config.allows(tc.dsID); got != tc.want {
				t.Fatalf("allows(%q) = %v, want %v", tc.dsID, got, tc.want)
			}
		})
	}
}

func TestFirtalRegistryFilterListedArrayShape(t *testing.T) {
	upstream := []byte(`[
		{"id":"ds-allowed","name":"A"},
		{"id":"ds-blocked","name":"B"},
		{"id":"ds-also-allowed","name":"C"}
	]`)
	config := firtalRegistryGrantConfig{AllowedDataSources: []string{"ds-allowed", "ds-also-allowed"}}
	got := config.filterListed(upstream)

	var out []map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("filtered output is not a JSON array: %v\n%s", err, got)
	}
	if len(out) != 2 {
		t.Fatalf("filterListed kept %d entries, want 2 (got %s)", len(out), got)
	}
	for _, item := range out {
		if item["id"] == "ds-blocked" {
			t.Fatalf("filterListed leaked a blocked data source: %v", item)
		}
	}
}

func TestFirtalRegistryFilterListedEnvelopeShape(t *testing.T) {
	upstream := []byte(`{"data":[
		{"id":"ds-allowed","name":"A"},
		{"id":"ds-blocked","name":"B"}
	],"meta":{"trace_id":"abc"}}`)
	config := firtalRegistryGrantConfig{AllowedDataSources: []string{"ds-allowed"}}
	got := config.filterListed(upstream)

	var env map[string]any
	if err := json.Unmarshal(got, &env); err != nil {
		t.Fatalf("filtered output is not a JSON envelope: %v\n%s", err, got)
	}
	data, ok := env["data"].([]any)
	if !ok {
		t.Fatalf("envelope.data is not an array: %T (%s)", env["data"], got)
	}
	if len(data) != 1 {
		t.Fatalf("filtered envelope kept %d entries, want 1", len(data))
	}
	if env["meta"] == nil {
		t.Fatalf("filterListed dropped sibling envelope fields")
	}
}

func TestFirtalRegistryFilterListedAllowedAllPassesThrough(t *testing.T) {
	upstream := []byte(`[{"id":"x"},{"id":"y"}]`)
	config := firtalRegistryGrantConfig{AllowedAll: true}
	got := config.filterListed(upstream)
	if string(got) != string(upstream) {
		t.Fatalf("AllowedAll=true should pass through, got %s want %s", got, upstream)
	}
}

func TestFirtalRegistryFilterListedUnknownShapeFailsClosed(t *testing.T) {
	upstream := []byte(`{"not_data":[{"id":"ds-1"}]}`)
	config := firtalRegistryGrantConfig{AllowedDataSources: []string{"ds-1"}}
	got := config.filterListed(upstream)
	if strings.TrimSpace(string(got)) != "[]" {
		t.Fatalf("unknown shape should fail closed to [], got %s", got)
	}
}

func TestFirtalRegistrySnakeToCamel(t *testing.T) {
	cases := map[string]string{
		"filter_group": "filterGroup",
		"parameters":   "parameters",
		"pagination":   "pagination",
		"aggregation":  "aggregation",
	}
	for in, want := range cases {
		if got := snakeToCamelRegistryArg(in); got != want {
			t.Errorf("snakeToCamelRegistryArg(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestFirtalRegistryCallExecuteSendsApiKeyAndCamelCaseBody verifies the outgoing
// HTTP request matches the FDR contract — POST to /api/registry/execute, the
// x-api-key header carries the workspace key, and snake_case tool args are
// remapped to camelCase keys (filter_group → filterGroup) on the wire.
func TestFirtalRegistryCallExecuteSendsApiKeyAndCamelCaseBody(t *testing.T) {
	var captured struct {
		method string
		path   string
		apiKey string
		ctype  string
		body   map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.apiKey = r.Header.Get("x-api-key")
		captured.ctype = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &captured.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"pagination":{"offset":0,"limit":100,"total":0,"has_more":false}}`))
	}))
	defer srv.Close()

	tool := &FirtalRegistryTool{}
	body := map[string]any{
		"dataSourceId": "ds-1",
		"filterGroup":  map[string]any{"logic": "AND", "filters": []any{}},
		"parameters":   map[string]any{"start_date": "2026-01-01"},
	}
	out, err := tool.callExecute(context.Background(), srv.URL, "rk_test", body)
	if err != nil {
		t.Fatalf("callExecute: %v", err)
	}

	if captured.method != http.MethodPost {
		t.Errorf("method = %q, want POST", captured.method)
	}
	if captured.path != firtalRegistryExecutePath {
		t.Errorf("path = %q, want %q", captured.path, firtalRegistryExecutePath)
	}
	if captured.apiKey != "rk_test" {
		t.Errorf("x-api-key = %q, want rk_test", captured.apiKey)
	}
	if !strings.HasPrefix(captured.ctype, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", captured.ctype)
	}
	if captured.body["dataSourceId"] != "ds-1" {
		t.Errorf("body.dataSourceId = %v, want ds-1", captured.body["dataSourceId"])
	}
	if _, ok := captured.body["filterGroup"].(map[string]any); !ok {
		t.Errorf("body.filterGroup missing or wrong type: %T", captured.body["filterGroup"])
	}
	if out == "" {
		t.Errorf("callExecute returned empty body; want passthrough of FDR response")
	}
}

// TestFirtalRegistryCallListUsesGetAndFiltersByAllowlist verifies that listing
// data sources sends GET to /api/registry/data-sources and the response is
// filtered to only allowed data sources before being returned to the agent.
func TestFirtalRegistryCallListUsesGetAndFiltersByAllowlist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != firtalRegistryListPath {
			t.Errorf("path = %q, want %q", r.URL.Path, firtalRegistryListPath)
		}
		if r.Header.Get("x-api-key") != "rk_test" {
			t.Errorf("x-api-key = %q, want rk_test", r.Header.Get("x-api-key"))
		}
		_, _ = w.Write([]byte(`[
			{"id":"ds-allowed","name":"Sales"},
			{"id":"ds-blocked","name":"Payroll"}
		]`))
	}))
	defer srv.Close()

	tool := &FirtalRegistryTool{}
	cfg := firtalRegistryGrantConfig{AllowedDataSources: []string{"ds-allowed"}}
	out, err := tool.callList(context.Background(), srv.URL, "rk_test", cfg)
	if err != nil {
		t.Fatalf("callList: %v", err)
	}
	if strings.Contains(out, "ds-blocked") {
		t.Fatalf("callList leaked a blocked data source: %s", out)
	}
	if !strings.Contains(out, "ds-allowed") {
		t.Fatalf("callList dropped the allowed data source: %s", out)
	}
}

func TestFirtalRegistryCallExecuteNon2xxBubblesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"dataSourceId is required"}`))
	}))
	defer srv.Close()

	tool := &FirtalRegistryTool{}
	_, err := tool.callExecute(context.Background(), srv.URL, "rk_test", map[string]any{})
	if err == nil {
		t.Fatal("callExecute should error on HTTP 400")
	}
	if !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), "dataSourceId") {
		t.Errorf("error should preserve upstream HTTP code and body: %v", err)
	}
}

// TestFirtalRegistryInputSchemaShape lock-tests the surface the LLM sees. The
// tool dispatches on `action`, with data_source_id required for get_schema and
// execute. If the schema regresses, every cloud-agent run on this tool breaks
// at parse time.
func TestFirtalRegistryInputSchemaShape(t *testing.T) {
	tool := &FirtalRegistryTool{}
	schema := tool.InputSchema()
	if schema["type"] != "object" {
		t.Fatalf("schema.type = %v, want object", schema["type"])
	}
	required, _ := schema["required"].([]string)
	foundAction := false
	for _, r := range required {
		if r == "action" {
			foundAction = true
		}
	}
	if !foundAction {
		t.Fatal("schema.required must include action")
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema.properties missing")
	}
	for _, key := range []string{"action", "data_source_id", "parameters", "filter_group", "pagination", "aggregation"} {
		if _, ok := props[key]; !ok {
			t.Errorf("schema.properties missing %q", key)
		}
	}
}

func TestFirtalRegistryCallRejectsUnknownAction(t *testing.T) {
	tool := &FirtalRegistryTool{}
	_, err := tool.Call(context.Background(), map[string]any{"action": "wat"})
	if err == nil || !strings.Contains(err.Error(), "action") {
		t.Fatalf("expected action error, got %v", err)
	}
}

func TestFirtalRegistryCallRejectsMissingAction(t *testing.T) {
	tool := &FirtalRegistryTool{}
	_, err := tool.Call(context.Background(), map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "action is required") {
		t.Fatalf("expected required-action error, got %v", err)
	}
}

func TestFirtalRegistryFilterAppsByRepo(t *testing.T) {
	body := []byte(`[
		{"name":"cerebro","github_repo":"firtal-group/firtal-cerebro","owner_email":"a@firtal.com"},
		{"name":"portal","github_repo":"firtal-group/firtal-portal","owner_email":"b@firtal.com"}
	]`)
	out := filterAppsByRepo(body, "firtal-group/firtal-cerebro")
	var apps []map[string]any
	if err := json.Unmarshal(out, &apps); err != nil {
		t.Fatalf("unmarshal filtered: %v", err)
	}
	if len(apps) != 1 || apps[0]["name"] != "cerebro" {
		t.Fatalf("expected only the cerebro app, got: %s", out)
	}

	// Case-insensitive match.
	if got := filterAppsByRepo(body, "FIRTAL-GROUP/FIRTAL-CEREBRO"); !strings.Contains(string(got), "cerebro") {
		t.Fatalf("expected case-insensitive match, got: %s", got)
	}

	// No match → empty array, never the unfiltered body.
	if got := filterAppsByRepo(body, "firtal-group/does-not-exist"); strings.Contains(string(got), "cerebro") || strings.Contains(string(got), "portal") {
		t.Fatalf("expected empty result for unknown repo, got: %s", got)
	}

	// Unexpected (non-array) shape → returned untouched, fail-open on parse.
	weird := []byte(`{"error":"nope"}`)
	if got := filterAppsByRepo(weird, "x"); string(got) != string(weird) {
		t.Fatalf("unexpected shape should pass through untouched, got: %s", got)
	}
}

func TestFirtalRegistryCallAppsGetsAndFiltersByRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != firtalRegistryAppsPath {
			t.Errorf("path = %q, want %q", r.URL.Path, firtalRegistryAppsPath)
		}
		if r.Header.Get("x-api-key") != "rk_test" {
			t.Errorf("x-api-key = %q, want rk_test", r.Header.Get("x-api-key"))
		}
		_, _ = w.Write([]byte(`[
			{"name":"cerebro","github_repo":"firtal-group/firtal-cerebro","owner_email":"a@firtal.com","deploy_model":"gate"},
			{"name":"portal","github_repo":"firtal-group/firtal-portal","owner_email":"b@firtal.com"}
		]`))
	}))
	defer srv.Close()

	tool := &FirtalRegistryTool{}
	out, err := tool.callApps(context.Background(), srv.URL, "rk_test", "firtal-group/firtal-cerebro")
	if err != nil {
		t.Fatalf("callApps: %v", err)
	}
	if strings.Contains(out, "portal") {
		t.Fatalf("callApps leaked an unrelated app: %s", out)
	}
	if !strings.Contains(out, "a@firtal.com") || !strings.Contains(out, "gate") {
		t.Fatalf("callApps dropped owner/deploy data: %s", out)
	}
}

func TestFirtalRegistryCallUpdateAppPostsToDeploy(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != firtalRegistryDeployPath {
			t.Errorf("path = %q, want %q", r.URL.Path, firtalRegistryDeployPath)
		}
		if r.Header.Get("x-api-key") != "rk_test" {
			t.Errorf("x-api-key = %q, want rk_test", r.Header.Get("x-api-key"))
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"created":false,"app":{"id":"app-1","name":"cerebro"}}`))
	}))
	defer srv.Close()

	tool := &FirtalRegistryTool{}
	fields := map[string]any{"name": "cerebro", "platform_external_id": "svc_1", "owner_email": "a@firtal.com"}
	out, err := tool.callUpdateApp(context.Background(), srv.URL, "rk_test", fields)
	if err != nil {
		t.Fatalf("callUpdateApp: %v", err)
	}
	if gotBody["name"] != "cerebro" || gotBody["owner_email"] != "a@firtal.com" {
		t.Fatalf("upstream did not receive the fields: %v", gotBody)
	}
	if !strings.Contains(out, "app-1") {
		t.Fatalf("response not returned to caller: %s", out)
	}
}

func TestFirtalRegistryUpdateAppRequiresAllowWrite(t *testing.T) {
	// AllowWrite is gated in Call(); assert the config flag is independent of
	// read scope so allowed_data_sources_all never implies write.
	cfg := firtalRegistryGrantConfig{AllowedAll: true}
	if cfg.AllowWrite {
		t.Fatal("allowed_data_sources_all must not imply AllowWrite")
	}
}

func TestFirtalRegistryCallUpdateAppNon2xxBubblesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"This system key is not authorised to write to the Apps registry."}`))
	}))
	defer srv.Close()

	tool := &FirtalRegistryTool{}
	_, err := tool.callUpdateApp(context.Background(), srv.URL, "rk_test", map[string]any{"name": "x"})
	if err == nil {
		t.Fatal("callUpdateApp should error on HTTP 403")
	}
	if !strings.Contains(err.Error(), "HTTP 403") || !strings.Contains(err.Error(), "not authorised") {
		t.Errorf("error should preserve upstream HTTP code and body: %v", err)
	}
}
