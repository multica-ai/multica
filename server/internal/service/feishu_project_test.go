package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
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

func TestFeishuProjectPluginTokenCachesAndRefreshes(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open_api/authen/plugin_token" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		call := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token-` + strconv.Itoa(int(call)) + `"}}`))
	}))
	defer server.Close()

	client := &FeishuProjectClient{
		HTTPClient: server.Client(),
		BaseURL:    server.URL,
	}
	token1, err := client.pluginToken(context.Background(), "plugin-id", "plugin-secret")
	if err != nil {
		t.Fatalf("pluginToken first call: %v", err)
	}
	token2, err := client.pluginToken(context.Background(), "plugin-id", "plugin-secret")
	if err != nil {
		t.Fatalf("pluginToken cached call: %v", err)
	}
	if token1 != "plugin-token-1" || token2 != token1 {
		t.Fatalf("tokens = %q, %q; want cached first token", token1, token2)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("plugin token calls = %d, want 1", got)
	}

	client.pluginTokenMu.Lock()
	client.pluginTokenTill = time.Now().Add(-time.Second)
	client.pluginTokenMu.Unlock()
	token3, err := client.pluginToken(context.Background(), "plugin-id", "plugin-secret")
	if err != nil {
		t.Fatalf("pluginToken after expiry: %v", err)
	}
	if token3 != "plugin-token-2" {
		t.Fatalf("token after expiry = %q, want refreshed token", token3)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("plugin token calls after refresh = %d, want 2", got)
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
	}, "issue", false)
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
	}, "issue", false)
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

func TestFeishuProjectManualQueryWorkItemsUsesThirtyDayUpdatedAtFilter(t *testing.T) {
	var request map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/work_item/filter":
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode filter request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":[],"pagination":{"page_num":1,"page_size":100,"total":0}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{
		HTTPClient: server.Client(),
		BaseURL:    server.URL,
	}
	_, err := client.QueryWorkItems(context.Background(), db.FeishuProjectIntegration{
		ProjectKey:   "project-key",
		PluginID:     "plugin-id",
		PluginSecret: "plugin-secret",
		StatusMapping: []byte(`{
			"OPEN": "todo"
		}`),
		LastSyncedAt: pgtype.Timestamptz{Time: time.Date(2026, 5, 16, 4, 7, 12, 0, time.UTC), Valid: true},
	}, "issue", true)
	if err != nil {
		t.Fatalf("QueryWorkItems: %v", err)
	}
	updatedAt, _ := request["updated_at"].(map[string]any)
	start, _ := updatedAt["start"].(float64)
	if start == 0 {
		t.Fatalf("manual sync missing updated_at.start: %#v", request["updated_at"])
	}
	got := time.UnixMilli(int64(start))
	want := time.Now().Add(-feishuProjectManualLookback)
	if got.Before(want.Add(-time.Minute)) || got.After(want.Add(time.Minute)) {
		t.Fatalf("manual sync updated_at.start = %s, want around %s", got, want)
	}
}

func TestFeishuProjectQueryWorkItemsResolvesOwnerEmailFromUserDetails(t *testing.T) {
	// End-to-end: the assignee is in `issue_operator` (经办人), resolved through
	// user_details. The `owner` and `current_status_operator` fields also reference
	// the same person but must NOT be the source of OwnerEmail — `owner` is the
	// CREATOR in Meego and `current_status_operator` is a workflow-position concept.
	// Here all three happen to coincide, which is realistic when an issue's creator
	// is also assigned to themselves; the point is which field we pull from.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/work_item/filter":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"err_code": 0,
				"data": [{
					"id": 6994401497,
					"name": "test-wenxue",
					"work_item_status": {"state_key": "OPEN", "name": "新建"},
					"fields": [
						{"field_key": "current_status_operator", "field_alias": "current_status_operator", "name": "当前负责人", "field_value": ["7052496113189830658"]},
						{"field_key": "owner", "field_alias": "owner", "name": "创建者", "field_value": "7052496113189830658"},
						{"field_key": "issue_operator", "field_alias": "issue_operator", "name": "经办人", "field_value": ["7052496113189830658"]}
					],
					"user_details": [{
						"user_key": "7052496113189830658",
						"username": "7052496113189830658",
						"email": "beastpu@lilith.com",
						"name_cn": "朴文学"
					}]
				}],
				"pagination": {"page_num": 1, "page_size": 100, "total": 1}
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
			"OPEN": "todo"
		}`),
	}, "issue", false)
	if err != nil {
		t.Fatalf("QueryWorkItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].OwnerEmail != "beastpu@lilith.com" {
		t.Fatalf("OwnerEmail = %q, want beastpu@lilith.com (resolved from 经办人 via user_details)", items[0].OwnerEmail)
	}
}

// Regression for partopia#7004726014: when 经办人 (operator role) is empty, the synced
// Multica issue must NOT be auto-assigned to the creator. Modelled on the real Meego
// payload — only `owner` (= creator), `current_status_operator`, and an empty
// role_owners[operator] are populated; no `issue_operator` / 经办人 / 处理人 / 负责人.
func TestFeishuProjectQueryWorkItemsLeavesOwnerEmailEmptyWhenOperatorIsBlank(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/work_item/filter":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"err_code": 0,
				"data": [{
					"id": 7004726014,
					"name": "operator-empty-item",
					"work_item_status": {"state_key": "OPEN", "name": "新建"},
					"fields": [
						{"field_key": "owner", "field_alias": "owner", "name": "创建者", "field_value": "7052790644598751234"},
						{"field_key": "current_status_operator", "field_alias": "current_status_operator", "name": "当前负责人", "field_value": ["7052790644598751234"]},
						{"field_key": "role_owners", "field_type_key": "role_owners", "field_value": [
							{"role": "operator", "owners": null},
							{"role": "reporter", "owners": ["7052790644598751234"]}
						]}
					],
					"user_details": [{
						"user_key": "7052790644598751234",
						"email": "jinnanxu@lilith.com",
						"name_cn": "徐晋楠"
					}]
				}],
				"pagination": {"page_num": 1, "page_size": 100, "total": 1}
			}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{HTTPClient: server.Client(), BaseURL: server.URL}
	items, err := client.QueryWorkItems(context.Background(), db.FeishuProjectIntegration{
		ProjectKey:    "project-key",
		PluginID:      "plugin-id",
		PluginSecret:  "plugin-secret",
		StatusMapping: []byte(`{"OPEN": "todo"}`),
	}, "issue", false)
	if err != nil {
		t.Fatalf("QueryWorkItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].OwnerEmail != "" {
		t.Fatalf("OwnerEmail = %q, want empty (creator must not leak as assignee)", items[0].OwnerEmail)
	}
}

func TestFeishuProjectExternalIssueIdentityUsesBugID(t *testing.T) {
	item := FeishuProjectWorkItem{
		ID:          "6991773150",
		Type:        "issue",
		Title:       "【1.0.0】【邮件】GMT发送批量多语言邮件后，使用撤回功能，没有把发出的邮件进行撤回",
		Description: "details",
		URL:         "https://project.feishu.cn/project-key/issue/detail/6991773150",
	}

	if got := externalIdentifier(item); got != "BUG-6991773150" {
		t.Fatalf("externalIdentifier = %q, want BUG-6991773150", got)
	}
	if got := externalTitle(item); got != "[BUG-6991773150] 【1.0.0】【邮件】GMT发送批量多语言邮件后，使用撤回功能，没有把发出的邮件进行撤回" {
		t.Fatalf("externalTitle = %q", got)
	}
	desc := externalDescription(item, "")
	if want := "External-Id: BUG-6991773150"; !strings.Contains(desc, want) {
		t.Fatalf("externalDescription missing %q: %q", want, desc)
	}
}

func TestFeishuProjectExternalTitleDoesNotDuplicateBugID(t *testing.T) {
	cases := []string{
		"[BUG-6991773150] title",
		"BUG-6991773150 title",
		"BUG-6991773150: title",
	}
	for _, title := range cases {
		item := FeishuProjectWorkItem{ID: "6991773150", Type: "issue", Title: title}
		if got := externalTitle(item); got != "[BUG-6991773150] title" {
			t.Fatalf("externalTitle(%q) = %q", title, got)
		}
	}
}

func TestFeishuProjectOpenAPIFieldAttachments(t *testing.T) {
	field := map[string]any{
		"field_key": "attachment",
		"field_value": []any{
			map[string]any{
				"uuid":       "file-uuid",
				"name":       "screenshot.png",
				"type":       "image/png",
				"size":       float64(1234),
				"tmp_url":    "https://example.com/tmp/screenshot.png",
				"irrelevant": "ignored",
			},
			map[string]any{
				"id":      "user-1",
				"name_cn": "Not Attachment",
			},
		},
	}
	attachments := feishuProjectOpenAPIFieldAttachments(field)
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1: %#v", len(attachments), attachments)
	}
	got := attachments[0]
	if got.ID != "file-uuid" || got.Name != "screenshot.png" || got.ContentType != "image/png" || got.URL != "https://example.com/tmp/screenshot.png" || got.SizeBytes != 1234 {
		t.Fatalf("attachment = %#v", got)
	}
}

func TestFeishuProjectOpenAPIFieldAttachmentsExtractsMultiFileUID(t *testing.T) {
	field := map[string]any{
		"field_key":      "multi_attachment",
		"field_type_key": "multi_file",
		"field_value": []any{
			map[string]any{
				"uid":  "file-uid",
				"name": "20260511-182223.mp4",
				"type": "video/mp4",
				"size": "2.3MB",
				"url":  "https://project.feishu.cn/goapi/v5/platform/file/stream/download/file-uid",
			},
		},
	}

	attachments := feishuProjectOpenAPIFieldAttachments(field)
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1: %#v", len(attachments), attachments)
	}
	got := attachments[0]
	if got.ID != "file-uid" || got.Name != "20260511-182223.mp4" || got.ContentType != "video/mp4" || got.URL == "" || got.SizeBytes != 2411724 {
		t.Fatalf("attachment = %#v", got)
	}
}

func TestFeishuProjectAttachmentTooLarge(t *testing.T) {
	if feishuProjectAttachmentTooLarge(FeishuProjectAttachment{Name: "small.log", SizeBytes: 5 << 20}) {
		t.Fatal("5MB attachment should be allowed")
	}
	if !feishuProjectAttachmentTooLarge(FeishuProjectAttachment{Name: "large.log", SizeBytes: (5 << 20) + 1}) {
		t.Fatal("attachment larger than 5MB should be skipped")
	}
}

func TestFeishuProjectOwnerAgentAssignableStatus(t *testing.T) {
	cases := []struct {
		name           string
		externalStatus string
		localStatus    string
		want           bool
	}{
		{name: "open key without mapping", externalStatus: "OPEN", want: false},
		{name: "reopened key without mapping", externalStatus: "REOPENED", want: false},
		{name: "new label without mapping", externalStatus: "新建", want: false},
		{name: "reopened label without mapping", externalStatus: "重新打开", want: false},
		{name: "mapped todo custom key", externalStatus: "BVtdwq9Vd", localStatus: "todo", want: true},
		{name: "in progress", externalStatus: "IN PROGRESS", localStatus: "in_progress", want: false},
		{name: "unmapped unknown", externalStatus: "custom", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFeishuProjectOwnerAgentAssignableStatus(tc.externalStatus, tc.localStatus); got != tc.want {
				t.Fatalf("isFeishuProjectOwnerAgentAssignableStatus(%q, %q) = %v, want %v", tc.externalStatus, tc.localStatus, got, tc.want)
			}
		})
	}
}

func TestFeishuProjectParseSizeBytes(t *testing.T) {
	cases := map[string]int64{
		"1234":  1234,
		"2.3MB": 2411724,
		"5 MB":  5 << 20,
		"1GiB":  1 << 30,
	}
	for raw, want := range cases {
		if got := feishuProjectParseSizeBytes(raw); got != want {
			t.Fatalf("feishuProjectParseSizeBytes(%q) = %d, want %d", raw, got, want)
		}
	}
}

func TestFeishuProjectExternalDescriptionIncludesAttachmentMarkdown(t *testing.T) {
	item := FeishuProjectWorkItem{ID: "6991773150", Type: "issue", Title: "title", Description: "body"}
	desc := externalDescription(item, "![screenshot.png](/uploads/screenshot.png)")
	if !strings.Contains(desc, "body\n\n![screenshot.png](/uploads/screenshot.png)\n\nExternal-Id: BUG-6991773150") {
		t.Fatalf("externalDescription = %q", desc)
	}
}

func TestNormalizeFeishuProjectDescriptionExtractsProtectedImages(t *testing.T) {
	raw := "before\n\n![](https://project.feishu.cn/goapi/v5/platform/file/stream/download/token-a)<!-- 1D7DB00E-509C-4AD5-9F10-F59C6B6C1272 -->\n\n![](https://example.com/public.png)\n\nafter"
	desc, attachments := normalizeFeishuProjectDescription(raw)
	if desc != "before\n\n![](https://example.com/public.png)\n\nafter" {
		t.Fatalf("desc = %q", desc)
	}
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1: %#v", len(attachments), attachments)
	}
	got := attachments[0]
	if got.ID != "1D7DB00E-509C-4AD5-9F10-F59C6B6C1272" || got.Name != got.ID || got.URL != "https://project.feishu.cn/goapi/v5/platform/file/stream/download/token-a" || got.ContentType != "image/*" {
		t.Fatalf("attachment = %#v", got)
	}
}

func TestFeishuProjectOpenAPIFieldAttachmentsExtractsRichTextImages(t *testing.T) {
	field := map[string]any{
		"field_key": "description",
		"field_value": map[string]any{
			"doc_text": "body\n[图片]\n",
			"doc":      `{"0":{"ops":[{"attributes":{"image":"true","uuid":"IMG-1","src":"https://project.feishu.cn/goapi/v5/platform/file/stream/download/token"},"insert":" "}],"zoneId":"0"}}`,
			"doc_html": `<img id="IMG-1" src="https://project.feishu.cn/goapi/v5/platform/file/stream/download/token" data-name="shot.jpg" data-size="1234">`,
		},
	}

	attachments := feishuProjectOpenAPIFieldAttachments(field)
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1: %#v", len(attachments), attachments)
	}
	if attachments[0].ID != "IMG-1" || attachments[0].URL == "" || attachments[0].ContentType != "image/*" {
		t.Fatalf("attachment = %#v", attachments[0])
	}

	desc, _ := normalizeFeishuProjectDescription("body\n[图片]\n")
	if strings.Contains(desc, "[图片]") {
		t.Fatalf("description should remove image placeholders: %q", desc)
	}
}

func jsonEqual(a, b any) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return string(ra) == string(rb)
}

func TestFeishuProjectDownloadAttachmentRetriesOn30019(t *testing.T) {
	t.Parallel()
	var downloadCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/work_item/issue/123/file/download":
			n := downloadCalls.Add(1)
			if n == 1 {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"err_code":30019,"err_msg":"internal error"}`))
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Disposition", `attachment; filename="shot.png"`)
			_, _ = w.Write([]byte("PNGDATA"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{
		HTTPClient: server.Client(),
		BaseURL:    server.URL,
	}
	prevDelay := feishuProjectDownloadRetryInitialDelay
	feishuProjectDownloadRetryInitialDelay = time.Millisecond
	defer func() { feishuProjectDownloadRetryInitialDelay = prevDelay }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, filename, contentType, err := client.DownloadAttachment(
		ctx,
		db.FeishuProjectIntegration{ProjectKey: "project-key", PluginID: "id", PluginSecret: "secret"},
		FeishuProjectWorkItem{ID: "123", Type: "issue"},
		FeishuProjectAttachment{ID: "uuid-1", Name: "shot.png"},
	)
	if err != nil {
		t.Fatalf("DownloadAttachment: %v", err)
	}
	if string(data) != "PNGDATA" {
		t.Fatalf("data = %q", data)
	}
	if filename != "shot.png" || contentType != "image/png" {
		t.Fatalf("filename = %q contentType = %q", filename, contentType)
	}
	if got := downloadCalls.Load(); got != 2 {
		t.Fatalf("downloadCalls = %d, want 2 (1 transient + 1 success)", got)
	}
}

func TestFeishuProjectDownloadAttachmentDoesNotRetry4xx(t *testing.T) {
	t.Parallel()
	var downloadCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/work_item/issue/123/file/download":
			downloadCalls.Add(1)
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`forbidden`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{HTTPClient: server.Client(), BaseURL: server.URL}
	_, _, _, err := client.DownloadAttachment(
		context.Background(),
		db.FeishuProjectIntegration{ProjectKey: "project-key", PluginID: "id", PluginSecret: "secret"},
		FeishuProjectWorkItem{ID: "123", Type: "issue"},
		FeishuProjectAttachment{ID: "uuid-1", Name: "shot.png"},
	)
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("error should mention 403, got: %v", err)
	}
	if got := downloadCalls.Load(); got != 1 {
		t.Fatalf("downloadCalls = %d, want 1 (no retry for 4xx)", got)
	}
}

func TestFeishuProjectDownloadAttachmentSkipsGoapiFallback(t *testing.T) {
	t.Parallel()
	var downloadCalls, fallbackCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open_api/authen/plugin_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case r.URL.Path == "/open_api/project-key/work_item/issue/123/file/download":
			downloadCalls.Add(1)
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`not found`))
		case strings.HasPrefix(r.URL.Path, "/goapi/v5/platform/file/stream/download/"):
			// Without a session cookie this would return 401, masking the real 404.
			// The fallback must skip goapi URLs so we should never get here.
			fallbackCalls.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{HTTPClient: server.Client(), BaseURL: server.URL}
	_, _, _, err := client.DownloadAttachment(
		context.Background(),
		db.FeishuProjectIntegration{ProjectKey: "project-key", PluginID: "id", PluginSecret: "secret"},
		FeishuProjectWorkItem{ID: "123", Type: "issue"},
		FeishuProjectAttachment{
			ID:   "uuid-1",
			Name: "shot.png",
			URL:  server.URL + "/goapi/v5/platform/file/stream/download/token",
		},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("error should surface the original 404, got: %v", err)
	}
	if got := fallbackCalls.Load(); got != 0 {
		t.Fatalf("fallback should be skipped for goapi URLs, got %d calls", got)
	}
	if got := downloadCalls.Load(); got != 1 {
		t.Fatalf("downloadCalls = %d, want 1 (no retry for 404)", got)
	}
}

func TestFeishuProjectRateLimitResetDelayHonorsHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"empty header → no delay", "", 0},
		{"valid 3s header", "3", 3 * time.Second},
		{"capped at 60s", "120", feishuProjectRateLimitMaxSleep},
		{"negative ignored", "-5", 0},
		{"non-numeric ignored", "soon", 0},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			if tt.header != "" {
				h.Set("x-ogw-ratelimit-reset", tt.header)
			}
			if got := feishuProjectRateLimitResetDelay(h); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFeishuProjectRetryableAPIErrorRecognizesRateLimit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		payload     map[string]any
		retryable   bool
		rateLimited bool
		quota       bool
	}{
		{
			name:        "99991400 frequency limit",
			payload:     map[string]any{"err_code": 99991400, "err_msg": "request trigger frequency limit"},
			retryable:   true,
			rateLimited: true,
		},
		{
			name:        "string-form 99991400",
			payload:     map[string]any{"err_code": "99991400", "err_msg": "request trigger frequency limit"},
			retryable:   true,
			rateLimited: true,
		},
		{
			name:      "99991403 quota exhausted",
			payload:   map[string]any{"err_code": 99991403, "err_msg": "monthly quota exceeded"},
			retryable: false,
			quota:     true,
		},
		{
			name:      "50007 gateway timeout (still transient, not rate limit)",
			payload:   map[string]any{"err_code": 50007, "err_msg": "gateway timeout"},
			retryable: true,
		},
		{
			name:    "non-retryable business error",
			payload: map[string]any{"err_code": 1234, "err_msg": "validation failed"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := feishuProjectRetryableAPIError(tc.payload); got != tc.retryable {
				t.Errorf("retryable: got %v, want %v", got, tc.retryable)
			}
			if got := feishuProjectIsRateLimited(tc.payload); got != tc.rateLimited {
				t.Errorf("rateLimited: got %v, want %v", got, tc.rateLimited)
			}
			if got := feishuProjectIsQuotaExhausted(tc.payload); got != tc.quota {
				t.Errorf("quota: got %v, want %v", got, tc.quota)
			}
		})
	}
}

// On HTTP 429 the openAPI loop should sleep for the duration named in the
// gateway reset header rather than the default linear 1s backoff, then retry.
func TestFeishuProjectOpenAPIHonorsRateLimitResetOn429(t *testing.T) {
	t.Parallel()
	var apiCalls atomic.Int32
	var firstAt, secondAt time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/probe":
			n := apiCalls.Add(1)
			if n == 1 {
				firstAt = time.Now()
				w.Header().Set("x-ogw-ratelimit-reset", "1")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"err_code":99991400,"err_msg":"request trigger frequency limit"}`))
				return
			}
			secondAt = time.Now()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{HTTPClient: server.Client(), BaseURL: server.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.openAPI(ctx, db.FeishuProjectIntegration{
		ProjectKey: "project-key", PluginID: "id", PluginSecret: "secret",
	}, http.MethodPost, "/open_api/project-key/probe", map[string]any{}); err != nil {
		t.Fatalf("openAPI: %v", err)
	}
	if got := apiCalls.Load(); got != 2 {
		t.Fatalf("apiCalls = %d, want 2 (one 429 then retry)", got)
	}
	gap := secondAt.Sub(firstAt)
	if gap < 900*time.Millisecond {
		t.Fatalf("retry gap = %v, want ≥ ~1s from x-ogw-ratelimit-reset", gap)
	}
}

// err_code 99991403 must propagate ErrFeishuProjectQuotaExhausted on the very
// first response — no retries, since the monthly bucket only refills on the 1st.
func TestFeishuProjectOpenAPIQuotaExhaustedShortCircuits(t *testing.T) {
	t.Parallel()
	var apiCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/probe":
			apiCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":99991403,"err_msg":"monthly quota exceeded"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{HTTPClient: server.Client(), BaseURL: server.URL}
	_, err := client.openAPI(context.Background(), db.FeishuProjectIntegration{
		ProjectKey: "project-key", PluginID: "id", PluginSecret: "secret",
	}, http.MethodPost, "/open_api/project-key/probe", map[string]any{})
	if err == nil {
		t.Fatal("expected error on quota-exhausted response")
	}
	if !errors.Is(err, ErrFeishuProjectQuotaExhausted) {
		t.Fatalf("want errors.Is(err, ErrFeishuProjectQuotaExhausted), got %v", err)
	}
	if got := apiCalls.Load(); got != 1 {
		t.Fatalf("apiCalls = %d, want 1 (must not retry quota exhaustion)", got)
	}
}

// The attachment-download path must mirror the openAPI quota-exhausted behavior:
// surface ErrFeishuProjectQuotaExhausted immediately without burning retries.
func TestFeishuProjectDownloadAttachmentQuotaExhaustedShortCircuits(t *testing.T) {
	t.Parallel()
	var downloadCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/work_item/issue/123/file/download":
			downloadCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":99991403,"err_msg":"monthly quota exceeded"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{HTTPClient: server.Client(), BaseURL: server.URL}
	_, _, _, err := client.DownloadAttachment(
		context.Background(),
		db.FeishuProjectIntegration{ProjectKey: "project-key", PluginID: "id", PluginSecret: "secret"},
		FeishuProjectWorkItem{ID: "123", Type: "issue"},
		FeishuProjectAttachment{ID: "uuid-1", Name: "shot.png"},
	)
	if err == nil {
		t.Fatal("expected error on quota-exhausted attachment response")
	}
	if !errors.Is(err, ErrFeishuProjectQuotaExhausted) {
		t.Fatalf("want errors.Is(err, ErrFeishuProjectQuotaExhausted), got %v", err)
	}
	if got := downloadCalls.Load(); got != 1 {
		t.Fatalf("downloadCalls = %d, want 1 (must not retry quota exhaustion)", got)
	}
}

func TestFeishuProjectSinceUnixMilliForTrigger(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	withWatermark := db.FeishuProjectIntegration{
		LastSeenUpdatedAtMs: pgtype.Int8{
			Int64: time.Date(2026, 5, 28, 11, 55, 0, 0, time.UTC).UnixMilli(),
			Valid: true,
		},
	}
	noWatermarkButSynced := db.FeishuProjectIntegration{
		LastSyncedAt: pgtype.Timestamptz{
			Time:  time.Date(2026, 5, 28, 11, 50, 0, 0, time.UTC),
			Valid: true,
		},
	}
	bareIntegration := db.FeishuProjectIntegration{}

	tests := []struct {
		name    string
		cfg     db.FeishuProjectIntegration
		trigger string
		// Predicate over the resulting unix-millis; lets tests assert
		// "in the right ballpark" without pinning exact wall-clock math.
		check func(t *testing.T, got int64)
	}{
		{
			name:    "manual picks 30d lookback regardless of watermark",
			cfg:     withWatermark,
			trigger: "manual",
			check: func(t *testing.T, got int64) {
				want := now.Add(-feishuProjectManualLookback).UnixMilli()
				if got != want {
					t.Fatalf("got %d want %d (manual 30d)", got, want)
				}
			},
		},
		{
			name:    "scheduled uses watermark - 10min when set",
			cfg:     withWatermark,
			trigger: "scheduled",
			check: func(t *testing.T, got int64) {
				want := withWatermark.LastSeenUpdatedAtMs.Int64 - feishuProjectIncrementalReplay.Milliseconds()
				if got != want {
					t.Fatalf("got %d want %d (watermark - replay)", got, want)
				}
			},
		},
		{
			name:    "scheduled falls back to LastSyncedAt - 10min when watermark unset",
			cfg:     noWatermarkButSynced,
			trigger: "scheduled",
			check: func(t *testing.T, got int64) {
				want := noWatermarkButSynced.LastSyncedAt.Time.Add(-feishuProjectIncrementalReplay).UnixMilli()
				if got != want {
					t.Fatalf("got %d want %d (legacy fallback)", got, want)
				}
			},
		},
		{
			name:    "scheduled falls back to 24h initial lookback when neither set",
			cfg:     bareIntegration,
			trigger: "scheduled",
			check: func(t *testing.T, got int64) {
				want := now.Add(-feishuProjectInitialLookback).UnixMilli()
				if got != want {
					t.Fatalf("got %d want %d (initial 24h)", got, want)
				}
			},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, feishuProjectSinceUnixMilliForTrigger(tc.cfg, tc.trigger, now))
		})
	}
}

// /work_item/filter must receive opts.SinceUnixMilli verbatim when set,
// so callers (Sync) can pick the lookback policy in one place.
func TestFeishuProjectQueryWorkItemsHonorsExplicitSinceFromOpts(t *testing.T) {
	t.Parallel()
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/work_item/filter":
			if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
				t.Fatalf("decode: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":[],"pagination":{"page_num":1,"page_size":100,"total":0}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{HTTPClient: server.Client(), BaseURL: server.URL}
	const explicit int64 = 1748000000000
	err := client.QueryWorkItemPagesWithOptions(
		context.Background(),
		db.FeishuProjectIntegration{
			ProjectKey:    "project-key",
			PluginID:      "id",
			PluginSecret:  "secret",
			StatusMapping: []byte(`{"OPEN":"todo"}`),
		},
		"issue",
		false,
		FeishuProjectSyncOptions{SinceUnixMilli: explicit},
		func(FeishuProjectWorkItemPage) error { return nil },
	)
	if err != nil {
		t.Fatalf("QueryWorkItemPagesWithOptions: %v", err)
	}
	updatedAt, _ := captured["updated_at"].(map[string]any)
	got, _ := updatedAt["start"].(float64)
	if int64(got) != explicit {
		t.Fatalf("updated_at.start = %v, want %d", got, explicit)
	}
}

// Regression for the BUG-7004679644 incident: when a user types a work_item_id
// in the "立即同步" box, Meego applies work_item_status and updated_at filters
// with AND semantics, so keeping them drops any target whose status has moved
// off the mapped set or that hasn't been updated in 30 days. Targeted syncs
// must trust the user's intent and omit both filters.
func TestFeishuProjectQueryWorkItemsTargetedIDBypassesStatusAndUpdatedAtFilters(t *testing.T) {
	t.Parallel()
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/work_item/filter":
			if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
				t.Fatalf("decode: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":[],"pagination":{"page_num":1,"page_size":100,"total":0}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{HTTPClient: server.Client(), BaseURL: server.URL}
	err := client.QueryWorkItemPagesWithOptions(
		context.Background(),
		db.FeishuProjectIntegration{
			ProjectKey:    "project-key",
			PluginID:      "id",
			PluginSecret:  "secret",
			StatusMapping: []byte(`{"OPEN":"todo"}`),
		},
		"issue",
		true,
		FeishuProjectSyncOptions{WorkItemID: "7004679644"},
		func(FeishuProjectWorkItemPage) error { return nil },
	)
	if err != nil {
		t.Fatalf("QueryWorkItemPagesWithOptions: %v", err)
	}
	ids, _ := captured["work_item_ids"].([]any)
	if len(ids) != 1 || ids[0] != "7004679644" {
		t.Fatalf("work_item_ids = %#v, want [\"7004679644\"]", captured["work_item_ids"])
	}
	if _, present := captured["work_item_status"]; present {
		t.Fatalf("work_item_status must be omitted for targeted sync, got %#v", captured["work_item_status"])
	}
	if _, present := captured["updated_at"]; present {
		t.Fatalf("updated_at must be omitted for targeted sync, got %#v", captured["updated_at"])
	}
}

// A targeted (id-scoped) sync should not be blocked by an empty status_mapping —
// the user is asking for that exact row, Meego doesn't need a state filter to
// find it. Only the unscoped (scheduled / full) path keeps the ErrFeishuProjectSyncScopeRequired
// guardrail so it doesn't unbounded-scan.
func TestFeishuProjectQueryWorkItemsTargetedIDSkipsStatusMappingGuard(t *testing.T) {
	t.Parallel()
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/work_item/filter":
			hits++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"err_code":0,"data":[],"pagination":{"page_num":1,"page_size":100,"total":0}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{HTTPClient: server.Client(), BaseURL: server.URL}
	err := client.QueryWorkItemPagesWithOptions(
		context.Background(),
		db.FeishuProjectIntegration{
			ProjectKey:    "project-key",
			PluginID:      "id",
			PluginSecret:  "secret",
			StatusMapping: []byte(`{}`),
		},
		"issue",
		true,
		FeishuProjectSyncOptions{WorkItemID: "7004679644"},
		func(FeishuProjectWorkItemPage) error { return nil },
	)
	if err != nil {
		t.Fatalf("targeted sync should not require status mapping: %v", err)
	}
	if hits != 1 {
		t.Fatalf("expected /work_item/filter to be called once, got %d", hits)
	}
}

// Regression: a custom plugin/radio field like "BUG提单助手" (field_c1f194) is
// silently dropped from /work_item/{type}/meta but appears in /field/all with its
// inline options (是/否). Before the fix, the missing-from-/meta path fell through
// to /business/all and rendered the space-wide business-line tree as if those were
// the radio field's options.
func TestListFieldOptionsCustomRadioReturnsInlineOptionsFromFieldAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/work_item/issue/meta":
			// /meta intentionally omits field_c1f194 — this is the real Meego behavior.
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"fields":[{"field_key":"title","field_type_key":"_text"}]}}`))
		case "/open_api/project-key/field/all":
			_, _ = w.Write([]byte(`{
				"err_code": 0,
				"data": [
					{"field_key":"title","field_name":"标题","field_type_key":"_text","work_item_scopes":["issue"]},
					{"field_key":"field_c1f194","field_name":"BUG提单助手","field_type_key":"radio","is_custom_field":true,"work_item_scopes":["issue"],
					 "options":[{"option_id":"yes","option_name":"是"},{"option_id":"no","option_name":"否"}]}
				]
			}`))
		case "/open_api/project-key/business/all":
			t.Fatal("/business/all must not be called — the /business/all fallback was removed")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{HTTPClient: server.Client(), BaseURL: server.URL}
	opts, err := client.ListFieldOptions(context.Background(), db.FeishuProjectIntegration{
		ProjectKey:   "project-key",
		PluginID:     "plugin-id",
		PluginSecret: "plugin-secret",
	}, "issue", "field_c1f194")
	if err != nil {
		t.Fatalf("ListFieldOptions: %v", err)
	}
	if len(opts) != 2 || opts[0].Name != "是" || opts[1].Name != "否" {
		t.Fatalf("expected [是, 否] from /field/all, got %#v", opts)
	}
}

// The space-wide business-line tree lives behind its own service method so that
// callers who actually want the biz-line tree ask for it explicitly — and callers
// who just want a field's options can never accidentally end up with the biz-line
// tree (the regression class this whole refactor is preventing).
func TestListSpaceBusinessLinesHitsOnlyBusinessAll(t *testing.T) {
	var sawMeta, sawFieldAll bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/work_item/issue/meta":
			sawMeta = true
		case "/open_api/project-key/field/all":
			sawFieldAll = true
		case "/open_api/project-key/business/all":
			_, _ = w.Write([]byte(`{"err_code":0,"data":[
				{"id":"biz-1","name":"玩家服务组","children":[{"id":"biz-1a","name":"活动中心"}]},
				{"id":"biz-2","name":"TD基建"}
			]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{HTTPClient: server.Client(), BaseURL: server.URL}
	tree, err := client.ListSpaceBusinessLines(context.Background(), db.FeishuProjectIntegration{
		ProjectKey:   "project-key",
		PluginID:     "plugin-id",
		PluginSecret: "plugin-secret",
	})
	if err != nil {
		t.Fatalf("ListSpaceBusinessLines: %v", err)
	}
	if sawMeta || sawFieldAll {
		t.Fatalf("ListSpaceBusinessLines must not touch field endpoints (meta=%v fieldAll=%v)", sawMeta, sawFieldAll)
	}
	if len(tree) != 2 || tree[0].Name != "玩家服务组" || len(tree[0].Children) != 1 || tree[1].Name != "TD基建" {
		t.Fatalf("unexpected tree: %#v", tree)
	}
}

// A field with no inline options anywhere returns nil — the /business/all fallback
// is gone, so the UI falls through to a free-text input instead of being fed the
// unrelated space-wide biz-line tree.
func TestListFieldOptionsReturnsNilWhenNoInlineOptionsAnywhere(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/open_api/authen/plugin_token":
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"plugin_token":"plugin-token"}}`))
		case "/open_api/project-key/work_item/issue/meta":
			_, _ = w.Write([]byte(`{"err_code":0,"data":{"fields":[{"field_key":"summary","field_type_key":"_text"}]}}`))
		case "/open_api/project-key/field/all":
			_, _ = w.Write([]byte(`{
				"err_code": 0,
				"data": [
					{"field_key":"summary","field_name":"摘要","field_type_key":"_text","work_item_scopes":["issue"]}
				]
			}`))
		case "/open_api/project-key/business/all":
			t.Fatal("/business/all must not be called — the /business/all fallback was removed")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &FeishuProjectClient{HTTPClient: server.Client(), BaseURL: server.URL}
	opts, err := client.ListFieldOptions(context.Background(), db.FeishuProjectIntegration{
		ProjectKey:   "project-key",
		PluginID:     "plugin-id",
		PluginSecret: "plugin-secret",
	}, "issue", "summary")
	if err != nil {
		t.Fatalf("ListFieldOptions: %v", err)
	}
	if opts != nil {
		t.Fatalf("expected nil, got %#v", opts)
	}
}

// fakeFeishuTaskService records reconcileSyncedIssueTasks side effects so the
// cancel-only-on-terminal-status contract can be verified without a database.
type fakeFeishuTaskService struct {
	cancelled []pgtype.UUID
	enqueued  []pgtype.UUID
}

func (f *fakeFeishuTaskService) CancelTasksForIssue(_ context.Context, issueID pgtype.UUID) error {
	f.cancelled = append(f.cancelled, issueID)
	return nil
}

func (f *fakeFeishuTaskService) EnqueueTaskForIssue(_ context.Context, issue db.Issue, _ ...pgtype.UUID) (db.AgentTaskQueue, error) {
	f.enqueued = append(f.enqueued, issue.ID)
	return db.AgentTaskQueue{}, nil
}

func issueWithID(seed byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = seed + byte(i)
	}
	u.Valid = true
	return u
}

func TestReconcileSyncedIssueTasksCancelsOnTerminalStatus(t *testing.T) {
	for _, status := range []string{"done", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			fake := &fakeFeishuTaskService{}
			svc := &FeishuProjectSyncService{TaskService: fake}
			issue := db.Issue{ID: issueWithID(1), Status: status}

			svc.reconcileSyncedIssueTasks(context.Background(), issue)

			if len(fake.cancelled) != 1 {
				t.Fatalf("terminal status %q: cancelled %d times, want 1", status, len(fake.cancelled))
			}
			if len(fake.enqueued) != 0 {
				t.Fatalf("terminal status %q: enqueued %d times, want 0", status, len(fake.enqueued))
			}
		})
	}
}

// Regression: a non-terminal sync (e.g. an assignee change) must NOT cancel
// running tasks. Previously reconcileSyncedIssueTasks cancelled tasks whenever
// the assignee differed from the prior value, killing in-progress agent work.
func TestReconcileSyncedIssueTasksDoesNotCancelOnNonTerminalStatus(t *testing.T) {
	for _, status := range []string{"todo", "in_progress", "backlog"} {
		t.Run(status, func(t *testing.T) {
			fake := &fakeFeishuTaskService{}
			svc := &FeishuProjectSyncService{TaskService: fake}
			// Assignee is a member (not an agent), so enqueue short-circuits
			// without touching the database — isolating the cancel decision.
			issue := db.Issue{
				ID:           issueWithID(2),
				Status:       status,
				AssigneeType: pgtype.Text{String: "member", Valid: true},
				AssigneeID:   issueWithID(9),
			}

			svc.reconcileSyncedIssueTasks(context.Background(), issue)

			if len(fake.cancelled) != 0 {
				t.Fatalf("non-terminal status %q: cancelled %d times, want 0", status, len(fake.cancelled))
			}
		})
	}
}

func TestResolveAttachmentContentType(t *testing.T) {
	pngBytes := []byte("\x89PNG\r\n\x1a\nrest-of-png-bytes")
	pdfBytes := []byte("%PDF-1.4\n1 0 obj\n")
	tests := []struct {
		name     string
		declared string
		filename string
		data     []byte
		want     string
	}{
		{
			// The production bug: Feishu's file/download endpoint returns a
			// generic text/plain header for a PNG, which must not win over the
			// real bytes.
			name:     "generic text/plain over png resolves by extension",
			declared: "text/plain; charset=utf-8",
			filename: "客户端截图.png",
			data:     pngBytes,
			want:     "image/png",
		},
		{
			name:     "empty declared resolves by extension",
			declared: "",
			filename: "shot.PNG",
			data:     pngBytes,
			want:     "image/png",
		},
		{
			name:     "specific declared type is trusted",
			declared: "image/jpeg",
			filename: "weird.png",
			data:     pngBytes,
			want:     "image/jpeg",
		},
		{
			name:     "octet-stream with unknown extension falls back to sniffing",
			declared: "application/octet-stream",
			filename: "noext",
			data:     pdfBytes,
			want:     "application/pdf",
		},
		{
			name:     "log file stays text/plain",
			declared: "text/plain",
			filename: "client.log",
			data:     []byte("2026-06-03 boot ok\n"),
			want:     "text/plain",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveAttachmentContentType(tt.declared, tt.filename, tt.data)
			if !strings.HasPrefix(got, tt.want) {
				t.Fatalf("resolveAttachmentContentType(%q, %q) = %q, want prefix %q", tt.declared, tt.filename, got, tt.want)
			}
		})
	}
}

func TestFeishuProjectFileTokenFromURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"stream download url", "https://project.feishu.cn/goapi/v5/platform/file/stream/download/AbC_1-23==", "AbC_1-23=="},
		{"strips query and fragment", "https://project.feishu.cn/goapi/v5/platform/file/stream/download/tok?x=1#f", "tok"},
		{"non-file url", "https://example.com/public.png", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := feishuProjectFileTokenFromURL(tt.url); got != tt.want {
				t.Fatalf("feishuProjectFileTokenFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestFeishuProjectInlineImageUsesURLTokenAsExternalID(t *testing.T) {
	// Description inline image with no <!-- id --> comment: the external ID must
	// fall back to the durable file token in the stream URL so it dedups across
	// syncs instead of re-downloading every time.
	_, attachments := normalizeFeishuProjectDescription(
		"shot\n\n![](https://project.feishu.cn/goapi/v5/platform/file/stream/download/TOKEN-123==)\n")
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1: %#v", len(attachments), attachments)
	}
	if attachments[0].ID != "TOKEN-123==" {
		t.Fatalf("normalize ID = %q, want token from URL", attachments[0].ID)
	}

	// Rich-text doc op carrying a src URL but no uuid attribute.
	att, ok := feishuProjectRichTextImageFromAttrs(map[string]any{
		"image": "true",
		"src":   "https://project.feishu.cn/goapi/v5/platform/file/stream/download/OP-TOKEN==",
	})
	if !ok || att.ID != "OP-TOKEN==" {
		t.Fatalf("rich-text attrs: ok=%v att=%#v, want ID=OP-TOKEN==", ok, att)
	}

	// HTML <img> with src but no id attribute.
	html := feishuProjectRichTextImagesFromHTML(
		`<img src="https://project.feishu.cn/goapi/v5/platform/file/stream/download/HTML-TOKEN==">`)
	if len(html) != 1 || html[0].ID != "HTML-TOKEN==" {
		t.Fatalf("html images = %#v, want one with ID=HTML-TOKEN==", html)
	}
}
