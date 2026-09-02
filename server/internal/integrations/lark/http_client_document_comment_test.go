package lark

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestHTTPClientDocumentCommentContext(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok", 7200)
	fake.mux.HandleFunc("/open-apis/drive/v1/metas/batch_query", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"metas": []any{map[string]any{"title": "Roadmap", "url": "https://example/docx/token"}}}})
	})
	fake.mux.HandleFunc("/open-apis/drive/v1/files/token/comments/batch_query", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("user_id_type") != "open_id" {
			t.Errorf("user_id_type = %q", r.URL.Query().Get("user_id_type"))
		}
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"items": []any{map[string]any{
			"comment_id": "c1", "is_whole": false, "quote": "Q3 target",
			"reply_list": map[string]any{"replies": []any{map[string]any{"reply_id": "r1", "user_id": map[string]any{"open_id": "ou_user"}, "content": map[string]any{"elements": []any{map[string]any{"type": "text_run", "text_run": map[string]any{"text": "Please quantify"}}}}}}},
		}}}})
	})

	client := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL()}).(DocumentCommentAPI)
	got, err := client.GetDocumentCommentContext(context.Background(), testCreds(), DocumentCommentParams{FileToken: "token", FileType: "docx", CommentID: "c1", ReplyID: "r1"})
	if err != nil {
		t.Fatalf("GetDocumentCommentContext: %v", err)
	}
	if got.Title != "Roadmap" || got.Quote != "Q3 target" || len(got.Timeline) != 1 || got.Timeline[0].Text != "Please quantify" {
		t.Fatalf("context = %+v", got)
	}
}

func TestHTTPClientReplyAndVerifyDocumentComment(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok", 7200)
	calls := 0
	fake.mux.HandleFunc("/open-apis/drive/v1/files/token/comments/c1/replies", func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.Method {
		case http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"reply_id": "reply-created"}})
		case http.MethodGet:
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"items": []any{map[string]any{"reply_id": "reply-created"}}}})
		default:
			t.Fatalf("method = %s", r.Method)
		}
	})

	client := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL()}).(DocumentCommentAPI)
	id, err := client.ReplyDocumentComment(context.Background(), testCreds(), DocumentCommentReplyParams{DocumentCommentParams: DocumentCommentParams{FileToken: "token", FileType: "docx", CommentID: "c1"}, Text: "Done"})
	if err != nil {
		t.Fatalf("ReplyDocumentComment: %v", err)
	}
	if id != "reply-created" {
		t.Fatalf("reply id = %q", id)
	}
	if err := client.VerifyDocumentCommentReply(context.Background(), testCreds(), DocumentCommentParams{FileToken: "token", FileType: "docx", CommentID: "c1"}, id); err != nil {
		t.Fatalf("VerifyDocumentCommentReply: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestHTTPClientWholeDocumentCommentUsesNewCommentsEndpoint(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok", 7200)
	fake.mux.HandleFunc("/open-apis/drive/v1/files/token/new_comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"comment_id": "whole-comment"}})
	})

	client := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL()}).(DocumentCommentAPI)
	id, err := client.ReplyDocumentComment(context.Background(), testCreds(), DocumentCommentReplyParams{DocumentCommentParams: DocumentCommentParams{FileToken: "token", FileType: "docx", CommentID: "c1"}, Text: "Done", IsWhole: true})
	if err != nil || id != "whole-comment" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}
