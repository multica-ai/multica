package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestFeishuProjectIssueStatusOptionsUsesTemplateStateFlow(t *testing.T) {
	var sawTemplateDetail bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/template_list/issue":
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s, want GET", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"err_code": 0,
				"data": [{"template_id": "template-1"}]
			}`))
		case "/open_api/project-key/template_detail/template-1":
			sawTemplateDetail = true
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s, want GET", r.Method)
			}
			if got := r.Header.Get("X-PLUGIN-TOKEN"); got != "plugin-token" {
				t.Fatalf("X-PLUGIN-TOKEN = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"err_code": 0,
				"data": {
					"state_flow_confs": [
						{"state_key": "OPEN", "name": "新建"},
						{"state_key": "IN PROGRESS", "name": "处理中"}
					]
				}
			}`))
		case "/open_api/project-key/work_item/issue/meta":
			t.Fatal("IssueStatusOptions must prefer template state flow metadata")
		case "/open_api/project-key/work_item/issue/search/params":
			t.Fatal("IssueStatusOptions must not search work items")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{
		HTTPClient: server.Client(),
		BaseURL:    server.URL,
	}
	statuses, err := client.IssueStatusOptions(context.Background(), db.FeishuProjectIntegration{
		ProjectKey:   "project-key",
		PluginID:     "plugin-id",
		PluginSecret: "plugin-secret",
	})
	if err != nil {
		t.Fatalf("IssueStatusOptions: %v", err)
	}
	if !sawTemplateDetail {
		t.Fatal("template detail API was not called")
	}
	if len(statuses) != 2 {
		t.Fatalf("len(statuses) = %d, want 2", len(statuses))
	}
	if statuses[0].Key != "OPEN" || statuses[0].Name != "新建" {
		t.Fatalf("first status = %#v", statuses[0])
	}
	if statuses[1].Key != "IN PROGRESS" || statuses[1].Name != "处理中" {
		t.Fatalf("second status = %#v", statuses[1])
	}
}

func TestFeishuProjectIssueStatusOptionsFallsBackToFieldMetadata(t *testing.T) {
	var sawMetadataAPI bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/template_list/issue":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":[]}`))
		case "/open_api/project-key/work_item/issue/meta":
			sawMetadataAPI = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"err_code": 0,
				"data": {
					"fields": [{
						"field_type": "_work_item_status",
						"options": [
							{"value": "OPEN", "label": "新建"},
							{"value": "CLOSED", "label": "Closed"}
						]
					}]
				}
			}`))
		case "/open_api/project-key/work_item/issue/search/params":
			t.Fatal("IssueStatusOptions must not search work items")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{
		HTTPClient: server.Client(),
		BaseURL:    server.URL,
	}
	statuses, err := client.IssueStatusOptions(context.Background(), db.FeishuProjectIntegration{
		ProjectKey:   "project-key",
		PluginID:     "plugin-id",
		PluginSecret: "plugin-secret",
	})
	if err != nil {
		t.Fatalf("IssueStatusOptions: %v", err)
	}
	if !sawMetadataAPI {
		t.Fatal("metadata API was not called")
	}
	if len(statuses) != 2 || statuses[0].Key != "OPEN" || statuses[1].Key != "CLOSED" {
		t.Fatalf("statuses = %#v", statuses)
	}
}

func TestFeishuProjectQueryWorkItemsRequiresMappedStatusScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("QueryWorkItems must not call remote search without mapped statuses: %s", r.URL.Path)
	}))
	defer server.Close()

	client := &FeishuProjectClient{
		HTTPClient: server.Client(),
		BaseURL:    server.URL,
	}
	_, err := client.QueryWorkItems(context.Background(), db.FeishuProjectIntegration{
		ProjectKey:    "project-key",
		PluginID:      "plugin-id",
		PluginSecret:  "plugin-secret",
		StatusMapping: []byte(`{}`),
	}, "issue")
	if !errors.Is(err, ErrFeishuProjectSyncScopeRequired) {
		t.Fatalf("err = %v, want ErrFeishuProjectSyncScopeRequired", err)
	}
}

func TestFeishuProjectQueryWorkItemsBuildsBoundedFilterAndPaginatesByTotal(t *testing.T) {
	var requests []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/work_item/filter":
			if got := r.Header.Get("X-PLUGIN-TOKEN"); got != "plugin-token" {
				t.Fatalf("X-PLUGIN-TOKEN = %q", got)
			}
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode filter request: %v", err)
			}
			requests = append(requests, req)
			page := len(requests)
			id := "1"
			if page == 2 {
				id = "2"
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"err_code": 0,
				"data": [{
					"id": ` + id + `,
					"name": "issue ` + id + `",
					"work_item_status": {"state_key": "OPEN", "name": "新建"},
					"updated_at": 1778933232000
				}],
				"pagination": {"page_num": ` + strconv.Itoa(page) + `, "page_size": 100, "total": 101}
			}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{
		HTTPClient: server.Client(),
		BaseURL:    server.URL,
	}
	items, err := client.QueryWorkItems(context.Background(), db.FeishuProjectIntegration{
		ProjectKey:   "project-key",
		PluginID:     "plugin-id",
		PluginSecret: "plugin-secret",
		StatusMapping: []byte(`{
			"OPEN": "todo",
			"IN PROGRESS": "in_progress"
		}`),
		LastSyncedAt: pgtype.Timestamptz{Time: time.Date(2026, 5, 16, 4, 7, 12, 0, time.UTC), Valid: true},
	}, "issue")
	if err != nil {
		t.Fatalf("QueryWorkItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if len(requests) != 2 {
		t.Fatalf("len(requests) = %d, want 2", len(requests))
	}
	first := requests[0]
	if first["page_num"] != float64(1) || first["page_size"] != float64(100) {
		t.Fatalf("first pagination = page_num %#v page_size %#v", first["page_num"], first["page_size"])
	}
	if got := first["work_item_type_keys"]; !jsonEqual(got, []any{"issue"}) {
		t.Fatalf("work_item_type_keys = %#v", got)
	}
	if got := first["work_item_status"]; !jsonEqual(got, []any{map[string]any{"state_key": "IN PROGRESS"}, map[string]any{"state_key": "OPEN"}}) {
		t.Fatalf("work_item_status = %#v", got)
	}
	updatedAt, _ := first["updated_at"].(map[string]any)
	if updatedAt["start"] != float64(time.Date(2026, 5, 16, 3, 57, 12, 0, time.UTC).UnixMilli()) {
		t.Fatalf("updated_at.start = %#v", updatedAt["start"])
	}
	if requests[1]["page_num"] != float64(2) {
		t.Fatalf("second page_num = %#v, want 2", requests[1]["page_num"])
	}
}

func jsonEqual(a, b any) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return string(ra) == string(rb)
}
