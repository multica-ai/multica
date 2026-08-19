package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListsClientGetSchemaUsesItemsAPIsNotFilesInfo(t *testing.T) {
	var sawAuth, sawCreateBody string
	var hitFilesInfo bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if strings.Contains(r.URL.Path, "xoxb-") {
			t.Errorf("token leaked into URL: %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/files.info":
			hitFilesInfo = true
			http.NotFound(w, r)
		case "/slackLists.items.list":
			_, _ = io.ReadAll(r.Body)
			_, _ = w.Write([]byte(`{"ok":true,"items":[{"id":"Rec1","list_id":"F0BR8PBUAQH"}]}`))
		case "/slackLists.items.info":
			_, _ = io.ReadAll(r.Body)
			_, _ = w.Write([]byte(`{
			  "ok": true,
			  "list": {
			    "id": "F0BR8PBUAQH",
			    "title": "Daykee 灵感池",
			    "list_metadata": {
			      "schema": [
			        {"id":"ColTitle","name":"Title","key":"title","type":"rich_text","is_primary_column":true},
			        {"id":"ColNotes","name":"Notes","key":"notes","type":"rich_text"},
			        {"id":"ColPri","name":"优先级","key":"priority","type":"select","options":{"choices":[{"id":"OptG49SVGXE","label":"中"}]}}
			      ]
			    }
			  }
			}`))
		case "/slackLists.items.create":
			raw, _ := io.ReadAll(r.Body)
			sawCreateBody = string(raw)
			_, _ = w.Write([]byte(`{
			  "ok": true,
			  "item": {"id":"RecABC","list_id":"F0BR8PBUAQH","fields":[{"text":"camera motion","column_id":"ColTitle"}]}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := &ListsClient{HTTP: srv.Client(), BaseURL: srv.URL}
	schema, err := c.GetSchema(context.Background(), "xoxb-secret-token-value", "F0BR8PBUAQH")
	if err != nil {
		t.Fatalf("GetSchema: %v", err)
	}
	if hitFilesInfo {
		t.Fatal("GetSchema must not call files.info")
	}
	if schema.Title != "Daykee 灵感池" || len(schema.Columns) != 3 || schema.Columns[0].ID != "ColTitle" {
		t.Fatalf("schema = %+v", schema)
	}
	if len(schema.Columns[2].Options) != 1 || schema.Columns[2].Options[0].ID != "OptG49SVGXE" {
		t.Fatalf("select options = %+v", schema.Columns[2].Options)
	}
	if !strings.HasPrefix(sawAuth, "Bearer xoxb-") {
		t.Fatalf("Authorization = %q", sawAuth)
	}

	fields, title, err := EncodeListsFields([]ListsField{
		{Column: "Title", Text: "camera motion"},
		{Column: "Notes", Text: "hallway at night"},
		{Column: "优先级", Text: "中"},
	}, schema)
	if err != nil {
		t.Fatalf("EncodeListsFields: %v", err)
	}
	if title != "camera motion" || fields[0]["column_id"] != "ColTitle" {
		t.Fatalf("encoded title=%q fields=%v", title, fields)
	}
	if _, ok := fields[0]["rich_text"]; !ok {
		t.Fatal("text columns must encode as rich_text")
	}
	if got, ok := fields[2]["select"].([]string); !ok || len(got) != 1 || got[0] != "OptG49SVGXE" {
		t.Fatalf("select encode = %#v", fields[2]["select"])
	}

	item, err := c.CreateItem(context.Background(), "xoxb-secret-token-value", "F0BR8PBUAQH", fields)
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if item.ID != "RecABC" || item.Title != "camera motion" {
		t.Fatalf("item = %+v", item)
	}
	if strings.Contains(sawCreateBody, "xoxb-") {
		t.Fatal("create body must not carry the bot token")
	}
	var posted map[string]any
	if err := json.Unmarshal([]byte(sawCreateBody), &posted); err != nil {
		t.Fatal(err)
	}
	if posted["list_id"] != "F0BR8PBUAQH" {
		t.Fatalf("posted list_id = %v", posted["list_id"])
	}
}

func TestListsClientGetSchemaEmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/slackLists.items.list" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"items":[]}`))
	}))
	defer srv.Close()
	c := &ListsClient{HTTP: srv.Client(), BaseURL: srv.URL}
	_, err := c.GetSchema(context.Background(), "xoxb-secret", "F1")
	if err == nil || !strings.Contains(err.Error(), "list_schema_unavailable") {
		t.Fatalf("empty list schema err = %v", err)
	}
}

func TestListsClientSlackAPIErrorAndNoTokenLeak(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"missing_scope"}`))
	}))
	defer srv.Close()
	c := &ListsClient{HTTP: srv.Client(), BaseURL: srv.URL}
	_, err := c.GetSchema(context.Background(), "xoxb-should-never-leak", "F1")
	if err == nil {
		t.Fatal("expected Slack API error")
	}
	if !strings.Contains(err.Error(), "missing_scope") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "xoxb-") {
		t.Fatalf("token leaked in error: %v", err)
	}
}

func TestRedactSlackSecrets(t *testing.T) {
	in := "decode failed for xoxb-1234567890-abcdef and xapp-1-2-3-aaaaaa"
	out := redactSlackSecrets(in)
	if strings.Contains(out, "xoxb-") || strings.Contains(out, "xapp-") {
		t.Fatalf("not redacted: %s", out)
	}
}

func TestEncodeListsFieldsUnknownAndReadOnly(t *testing.T) {
	schema := ListsSchema{Columns: []ListsColumn{
		{ID: "Col1", Name: "Title", Type: "rich_text", IsPrimaryColumn: true},
		{ID: "ColAuto", Name: "提出时间", Type: "created_time"},
	}}
	if _, _, err := EncodeListsFields([]ListsField{{Column: "Nope", Text: "x"}}, schema); err == nil {
		t.Fatal("expected unknown column")
	}
	if _, _, err := EncodeListsFields([]ListsField{{Column: "提出时间", Text: "2026-08-19"}}, schema); err == nil {
		t.Fatal("expected read-only column")
	}
}
