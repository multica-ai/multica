package hindsight_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/hindsight"
)

var noopLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

// --- ConfigFromEnv ---

func TestConfigFromEnv_DisabledWhenNoURL(t *testing.T) {
	t.Setenv("MULTICA_HINDSIGHT_URL", "")
	if cfg := hindsight.ConfigFromEnv(); cfg != nil {
		t.Errorf("expected nil config when URL is unset, got %+v", cfg)
	}
}

func TestConfigFromEnv_DefaultBankID(t *testing.T) {
	t.Setenv("MULTICA_HINDSIGHT_URL", "http://localhost:8888")
	t.Setenv("MULTICA_HINDSIGHT_BANK_ID", "")
	cfg := hindsight.ConfigFromEnv()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.BankID != "multica" {
		t.Errorf("expected default bank ID 'multica', got %q", cfg.BankID)
	}
}

func TestConfigFromEnv_CustomBankID(t *testing.T) {
	t.Setenv("MULTICA_HINDSIGHT_URL", "http://localhost:8888")
	t.Setenv("MULTICA_HINDSIGHT_BANK_ID", "my-workspace")
	cfg := hindsight.ConfigFromEnv()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.BankID != "my-workspace" {
		t.Errorf("expected bank ID 'my-workspace', got %q", cfg.BankID)
	}
}

func TestConfigFromEnv_TrailingSlashStripped(t *testing.T) {
	t.Setenv("MULTICA_HINDSIGHT_URL", "http://localhost:8888/")
	cfg := hindsight.ConfigFromEnv()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if strings.HasSuffix(cfg.APIURL, "/") {
		t.Errorf("expected trailing slash stripped, got %q", cfg.APIURL)
	}
}

// --- Recall ---

func TestRecall_ReturnsFormattedList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/memories/recall") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "1", "text": "Workspace uses trunk-based development"},
				{"id": "2", "text": "Tests require a running PostgreSQL instance"},
			},
		})
	}))
	defer srv.Close()

	cfg := &hindsight.Config{APIURL: srv.URL, BankID: "test"}
	result := hindsight.Recall(context.Background(), cfg, "workspace context", noopLogger)
	if !strings.Contains(result, "trunk-based development") {
		t.Errorf("expected recall result to contain memory text, got %q", result)
	}
	if !strings.Contains(result, "1.") {
		t.Errorf("expected numbered list format, got %q", result)
	}
}

func TestRecall_EmptyResultsReturnsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer srv.Close()

	cfg := &hindsight.Config{APIURL: srv.URL, BankID: "test"}
	result := hindsight.Recall(context.Background(), cfg, "anything", noopLogger)
	if result != "" {
		t.Errorf("expected empty string for empty results, got %q", result)
	}
}

func TestRecall_NilConfigReturnsEmpty(t *testing.T) {
	result := hindsight.Recall(context.Background(), nil, "query", noopLogger)
	if result != "" {
		t.Errorf("expected empty string for nil config, got %q", result)
	}
}

func TestRecall_EmptyQueryReturnsEmpty(t *testing.T) {
	cfg := &hindsight.Config{APIURL: "http://localhost:8888", BankID: "test"}
	result := hindsight.Recall(context.Background(), cfg, "", noopLogger)
	if result != "" {
		t.Errorf("expected empty string for empty query, got %q", result)
	}
}

func TestRecall_ServerErrorReturnsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := &hindsight.Config{APIURL: srv.URL, BankID: "test"}
	result := hindsight.Recall(context.Background(), cfg, "query", noopLogger)
	if result != "" {
		t.Errorf("expected empty string on server error, got %q", result)
	}
}

func TestRecall_SendsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer srv.Close()

	cfg := &hindsight.Config{APIURL: srv.URL, BankID: "test", APIKey: "hsk_secret"}
	hindsight.Recall(context.Background(), cfg, "query", noopLogger)
	if gotAuth != "Bearer hsk_secret" {
		t.Errorf("expected Bearer token, got %q", gotAuth)
	}
}

// --- Retain ---

func TestRetain_PostsToCorrectEndpoint(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &hindsight.Config{APIURL: srv.URL, BankID: "test"}
	hindsight.Retain(context.Background(), cfg, "task completed successfully", noopLogger)

	if !strings.HasSuffix(gotPath, "/memories") {
		t.Errorf("expected path to end with /memories, got %s", gotPath)
	}
	items, _ := gotBody["items"].([]any)
	if len(items) != 1 {
		t.Errorf("expected 1 item in retain request, got %d", len(items))
	}
}

func TestRetain_NilConfigIsNoop(t *testing.T) {
	// Should not panic or make any network calls.
	hindsight.Retain(context.Background(), nil, "content", noopLogger)
}

func TestRetain_EmptyContentIsNoop(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &hindsight.Config{APIURL: srv.URL, BankID: "test"}
	hindsight.Retain(context.Background(), cfg, "   ", noopLogger)
	if called {
		t.Error("expected no HTTP call for empty content")
	}
}

// --- FormatMemoriesBlock ---

func TestFormatMemoriesBlock_EmptyReturnsEmpty(t *testing.T) {
	if block := hindsight.FormatMemoriesBlock(""); block != "" {
		t.Errorf("expected empty string, got %q", block)
	}
}

func TestFormatMemoriesBlock_WrapsContent(t *testing.T) {
	block := hindsight.FormatMemoriesBlock("1. Some memory")
	if !strings.Contains(block, "<hindsight_memories>") {
		t.Errorf("missing opening tag in %q", block)
	}
	if !strings.Contains(block, "</hindsight_memories>") {
		t.Errorf("missing closing tag in %q", block)
	}
	if !strings.Contains(block, "Some memory") {
		t.Errorf("missing memory content in %q", block)
	}
}

// --- BuildRetainContent ---

func TestBuildRetainContent_IncludesAllFields(t *testing.T) {
	content := hindsight.BuildRetainContent("ws-123", "issue-456", "bot", "completed", "Fixed the bug. PR: https://...")
	for _, want := range []string{"ws-123", "issue-456", "bot", "completed", "Fixed the bug"} {
		if !strings.Contains(content, want) {
			t.Errorf("expected %q in retain content, got:\n%s", want, content)
		}
	}
}
