package agentcli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultNPMRegistriesUseTheOfficialRegistry(t *testing.T) {
	if len(DefaultNPMRegistries) != 1 || DefaultNPMRegistries[0] != "https://registry.npmjs.org" {
		t.Fatalf("DefaultNPMRegistries = %#v, want the official npm registry only", DefaultNPMRegistries)
	}
}

func TestNPMPackageSources(t *testing.T) {
	pkg, ok := NPMPackage(CLICodex)
	if !ok || pkg != "@openai/codex" {
		t.Fatalf("codex package = %q ok=%v", pkg, ok)
	}
	pkg, ok = NPMPackage(CLIClaude)
	if !ok || pkg != "@anthropic-ai/claude-code" {
		t.Fatalf("claude package = %q ok=%v", pkg, ok)
	}
	if pkg, ok = NPMPackage(CLIGrok); ok || pkg != "" {
		t.Fatalf("grok package = %q ok=%v; want no npm package", pkg, ok)
	}
	if got := npmLatestURL("https://registry.npmjs.org", "@openai/codex"); got != "https://registry.npmjs.org/@openai%2Fcodex/latest" {
		t.Fatalf("npm url = %s", got)
	}
}

func TestLatestLookupFallsBackAndCaches(t *testing.T) {
	var primaryHits, fallbackHits atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits.Add(1)
		http.Error(w, "mirror down", http.StatusBadGateway)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		// httptest decodes %2F in Path; prefer RawPath when present.
		path := r.URL.Path
		if r.URL.RawPath != "" {
			path = r.URL.RawPath
		}
		switch path {
		case "/@openai%2Fcodex/latest", "/@openai/codex/latest",
			"/@anthropic-ai%2Fclaude-code/latest", "/@anthropic-ai/claude-code/latest":
		default:
			t.Errorf("unexpected path %s (RawPath=%q)", r.URL.Path, r.URL.RawPath)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"9.8.7"}`))
	}))
	defer fallback.Close()

	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	lookup := NewLatestLookup(primary.Client())
	lookup.Registries = []string{primary.URL, fallback.URL}
	lookup.Now = func() time.Time { return now }
	lookup.SuccessTTL = time.Hour
	lookup.FailureTTL = time.Minute

	version, ok := lookup.Version(context.Background(), CLICodex)
	if !ok || version != "9.8.7" {
		t.Fatalf("codex latest = %q ok=%v", version, ok)
	}
	if primaryHits.Load() != 1 || fallbackHits.Load() != 1 {
		t.Fatalf("hits primary=%d fallback=%d, want one each", primaryHits.Load(), fallbackHits.Load())
	}
	version, ok = lookup.Version(context.Background(), CLICodex)
	if !ok || version != "9.8.7" {
		t.Fatalf("cached codex latest = %q ok=%v", version, ok)
	}
	if primaryHits.Load() != 1 || fallbackHits.Load() != 1 {
		t.Fatalf("cache miss: primary=%d fallback=%d", primaryHits.Load(), fallbackHits.Load())
	}
	if _, ok := lookup.Version(context.Background(), CLIGrok); ok {
		t.Fatal("grok latest must stay unknown")
	}
	if primaryHits.Load() != 1 {
		t.Fatal("grok must not query a registry")
	}
}

func TestLatestLookupCachesFailure(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	lookup := NewLatestLookup(srv.Client())
	lookup.Registries = []string{srv.URL}
	lookup.Now = func() time.Time { return now }
	lookup.FailureTTL = time.Minute

	if _, ok := lookup.Version(context.Background(), CLIClaude); ok {
		t.Fatal("failed lookup reported a version")
	}
	if _, ok := lookup.Version(context.Background(), CLIClaude); ok {
		t.Fatal("cached failure reported a version")
	}
	if hits.Load() != 1 {
		t.Fatalf("failure was not cached: hits=%d", hits.Load())
	}
	now = now.Add(2 * time.Minute)
	if _, ok := lookup.Version(context.Background(), CLIClaude); ok {
		t.Fatal("expired failure reported a version")
	}
	if hits.Load() != 2 {
		t.Fatalf("expired failure did not retry: hits=%d", hits.Load())
	}
}
