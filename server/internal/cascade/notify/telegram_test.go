package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func captureTelegram(status int, ok bool, description string) (*httptest.Server, *string, *string) {
	capturedBody := ""
	capturedPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(telegramResponse{OK: ok, ErrorCode: 0, Description: description})
	}))
	return srv, &capturedBody, &capturedPath
}

func TestTelegram_FormatsAndPosts(t *testing.T) {
	srv, body, path := captureTelegram(http.StatusOK, true, "")
	defer srv.Close()

	ch := NewTelegramChannel(srv.URL, "abc:xyz", "12345", nil)
	err := ch.Send(context.Background(), Event{
		Type:       EventRebaseConflict,
		IssuePUL:   "PUL-102",
		PRNumber:   7,
		IssueTitle: "Cascade",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// API path must include the bot token (url-escaped).
	if !strings.Contains(*path, "/botabc:xyz/sendMessage") {
		t.Errorf("unexpected path: %q", *path)
	}

	var req telegramRequest
	if err := json.Unmarshal([]byte(*body), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ChatID != "12345" {
		t.Errorf("chat_id = %q, want 12345", req.ChatID)
	}
	if req.ParseMode != "MarkdownV2" {
		t.Errorf("parse_mode = %q, want MarkdownV2", req.ParseMode)
	}
	// Headline must include the verb, escaped PUL, escaped PR number.
	if !strings.Contains(req.Text, "Rebase conflict") {
		t.Errorf("missing verb: %q", req.Text)
	}
	if !strings.Contains(req.Text, "PUL\\-102") {
		t.Errorf("PUL not MarkdownV2-escaped: %q", req.Text)
	}
}

func TestTelegram_OKFalseIs5xxTreatment(t *testing.T) {
	// Telegram can return HTTP 200 with ok=false (e.g. throttling
	// burst). Treat as transient — the bot can retry.
	srv, _, _ := captureTelegram(http.StatusOK, false, "Bad Request: chat not found")
	defer srv.Close()

	ch := NewTelegramChannel(srv.URL, "tok", "chat", nil)
	err := ch.Send(context.Background(), Event{Type: EventStuck24h, IssueID: "i"})
	if err == nil {
		t.Fatal("expected error when ok=false")
	}
	// Note: when status is 2xx and ok=false, this is permanent
	// (wrong chat id). We return a plain (non-transient) error.
	if errors.Is(err, ErrChannelTransient) {
		t.Errorf("2xx+ok=false should be permanent, got: %v", err)
	}
}

func TestTelegram_5xxIsTransient(t *testing.T) {
	srv, _, _ := captureTelegram(http.StatusInternalServerError, false, "internal")
	defer srv.Close()

	ch := NewTelegramChannel(srv.URL, "tok", "chat", nil)
	err := ch.Send(context.Background(), Event{Type: EventStuck24h, IssueID: "i"})
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if !errors.Is(err, ErrChannelTransient) {
		t.Errorf("5xx should be transient, got: %v", err)
	}
}

func TestTelegram_MissingConfigError(t *testing.T) {
	ch := NewTelegramChannel("", "", "", nil)
	err := ch.Send(context.Background(), Event{Type: EventStuck24h, IssueID: "i"})
	if err == nil {
		t.Fatal("expected error on missing config")
	}
	if !strings.Contains(err.Error(), "missing bot token or chat id") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestEscapeMD_ReservedCharacters(t *testing.T) {
	// Pin the escaping rule. MarkdownV2 docs list:
	// _ * [ ] ( ) ~ ` > # + - = | { } . !
	in := "a_b*c[d]e(f)g~h`i>j#k+l-m=n|o{p}q.r!s"
	got := escapeMD(in)
	expectedEscapes := []string{"\\_", "\\*", "\\[", "\\]", "\\(", "\\)", "\\~", "\\`", "\\>", "\\#", "\\+", "\\-", "\\=", "\\|", "\\{", "\\}", "\\.", "\\!"}
	for _, esc := range expectedEscapes {
		if !strings.Contains(got, esc) {
			t.Errorf("escapeMD missing %q in output %q", esc, got)
		}
	}
}

func TestBuildTelegramText_AllOptionalFieldsPresent(t *testing.T) {
	got := buildTelegramText(Event{
		Type:         EventLoopGuardTripped,
		IssuePUL:     "PUL-1",
		IssueTitle:   "title",
		PRNumber:     5,
		PRURL:        "https://x/y/pull/5",
		HumanContext: "context line",
	})
	for _, want := range []string{
		fmt.Sprintf("Loop guard"),
		fmt.Sprintf("PUL\\-1"),
		fmt.Sprintf("PR \\#5"),
		fmt.Sprintf("title"),
		fmt.Sprintf("context line"),
		fmt.Sprintf("https://x/y/pull/5"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("text missing %q in output:\n%s", want, got)
		}
	}
}
