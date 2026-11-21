package collab

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSignTicketMatchesCollabService(t *testing.T) {
	// Signed by apps/collab/src/ticket.ts signTicket with the same inputs.
	const want = "eyJkIjoiZG9jOjAxOTNhMWIyLWMzZDQtN2U1Zi04YTliLTBjMWQyZTNmNGE1YiIsInciOiJ3cy0xIiwidCI6Im1lbWJlciIsImkiOiIxMTExMTExMS0xMTExLTExMTEtMTExMS0xMTExMTExMTExMTEiLCJuIjoiQWRhIiwicm8iOmZhbHNlLCJleHAiOjQxMDI0NDQ4MDB9.4GoYej7TRhHnGZD6EdDcUlZ64JITKjEe94E_7h_okKY"
	got := SignTicket("s3cret", Ticket{
		Document:    DocumentName("0193a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"),
		WorkspaceID: "ws-1",
		Actor:       Actor{Type: "member", ID: "11111111-1111-1111-1111-111111111111", Name: "Ada"},
		ExpiresAt:   time.Unix(4102444800, 0),
	})
	if got != want {
		t.Fatalf("ticket mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestApplyTextEdits(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		edits   []TextEdit
		want    string
		failure *EditFailure
	}{
		{"single", "Status: draft", []TextEdit{{OldText: "draft", NewText: "approved"}}, "Status: approved", nil},
		{"sequential", "a b", []TextEdit{{OldText: "a", NewText: "c"}, {OldText: "c b", NewText: "done"}}, "done", nil},
		{"replace all", "x x x", []TextEdit{{OldText: "x", NewText: "y", ReplaceAll: true}}, "y y y", nil},
		{"not found", "abc", []TextEdit{{OldText: "zzz", NewText: "y"}}, "", &EditFailure{Reason: "not_found"}},
		{"ambiguous", "x x", []TextEdit{{OldText: "x", NewText: "y"}}, "", &EditFailure{Reason: "ambiguous", Matches: 2}},
		{"empty", "abc", []TextEdit{{OldText: "a", NewText: "b"}, {OldText: "", NewText: "y"}}, "", &EditFailure{Reason: "empty", Index: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, failure := ApplyTextEdits(tc.text, tc.edits)
			if (failure == nil) != (tc.failure == nil) || (failure != nil && *failure != *tc.failure) {
				t.Fatalf("failure = %+v, want %+v", failure, tc.failure)
			}
			if got != tc.want {
				t.Fatalf("text = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewClientDisabledWithoutSettings(t *testing.T) {
	for _, c := range [][2]string{{"", "s"}, {"http://x", ""}, {"", ""}} {
		client, err := NewClient(c[0], c[1])
		if client != nil || err != nil {
			t.Fatalf("NewClient(%q, %q) = %v, %v; want nil, nil", c[0], c[1], client, err)
		}
	}
	if _, err := NewClient("ftp://x", "s"); err == nil {
		t.Fatal("expected an error for a non-http URL")
	}
}

func TestClientMapsServiceResponses(t *testing.T) {
	var gotPath, gotSecret string
	var gotBody map[string]any
	status, body := http.StatusOK, `{"status":"ok","revision":7,"content":"x","merged":true}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotSecret = r.URL.Path, r.Header.Get(SecretHeader)
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	client, err := NewClient(srv.URL, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	content := "new"
	result, err := client.Write(ctx, "abc", WriteRequest{Actor: Actor{Type: "agent", ID: "a"}, Content: &content})
	if err != nil || result.Status != StatusOK || result.Revision != 7 || !result.Merged {
		t.Fatalf("write = %+v, %v", result, err)
	}
	if gotPath != "/internal/documents/doc:abc/write" || gotSecret != "s3cret" || gotBody["content"] != "new" {
		t.Fatalf("request = %s %q %v", gotPath, gotSecret, gotBody)
	}

	status, body = http.StatusConflict, `{"error":"edit_failed","status":"edit_failed","revision":3,"content":"c","failure":{"reason":"ambiguous","index":0,"matches":2}}`
	result, err = client.Edit(ctx, "abc", EditRequest{Edits: []TextEdit{{OldText: "x", NewText: "y"}}})
	if err != nil || result.Status != StatusEditFailed || result.Failure == nil || result.Failure.Matches != 2 {
		t.Fatalf("edit = %+v, %v", result, err)
	}

	status, body = http.StatusNotFound, `{}`
	if result, err = client.Write(ctx, "abc", WriteRequest{}); err != nil || result.Status != StatusGone {
		t.Fatalf("gone = %+v, %v", result, err)
	}

	status, body = http.StatusRequestEntityTooLarge, `{"error":"content_too_large","status":"too_large","revision":3}`
	if result, err = client.Edit(ctx, "abc", EditRequest{}); err != nil || result.Status != StatusTooLarge {
		t.Fatalf("too large = %+v, %v", result, err)
	}

	// A refused request must not be written around the running service.
	status, body = http.StatusBadRequest, `{"error":"bad"}`
	if _, err = client.Write(ctx, "abc", WriteRequest{}); err == nil || errors.Is(err, ErrUnavailable) {
		t.Fatalf("400 err = %v, want an error other than ErrUnavailable", err)
	}

	status, body = http.StatusInternalServerError, `{"error":"boom"}`
	if _, err = client.Write(ctx, "abc", WriteRequest{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("500 err = %v, want ErrUnavailable", err)
	}

	srv.Close()
	if err = client.Flush(ctx, "abc"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unreachable err = %v, want ErrUnavailable", err)
	}
}
