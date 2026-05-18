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
		{name: "open key", externalStatus: "OPEN", want: true},
		{name: "reopened key", externalStatus: "REOPENED", want: true},
		{name: "new label", externalStatus: "新建", want: true},
		{name: "reopened label", externalStatus: "重新打开", want: true},
		{name: "mapped todo custom key", externalStatus: "BVtdwq9Vd", localStatus: "todo", want: true},
		{name: "in progress", externalStatus: "IN PROGRESS", localStatus: "in_progress", want: false},
		{name: "unmapped unknown falls back elsewhere", externalStatus: "custom", want: false},
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
