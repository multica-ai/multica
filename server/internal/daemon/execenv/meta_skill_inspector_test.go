package execenv

// CEREBRO-PATCH(prompt-inspector): verify private GitHub repo prompt fetch auth.

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestInspectMetaSkill_layersIncludeRepoAndSidecar(t *testing.T) {
	repo := &RepoPromptSnapshot{
		CLAUDE: RepoPromptFile{
			Content:   "# Repo rules\n\nBe careful.\n",
			SourceURL: "https://github.com/acme/app/blob/main/CLAUDE.md",
			Found:     true,
		},
	}
	ctx := TaskContextForEnv{
		AgentName:         "Preview",
		AgentInstructions: "Do the thing.",
		IssueID:           "00000000-0000-0000-0000-000000000001",
	}
	ins := InspectMetaSkill("claude", ctx, repo)
	if len(ins.Layers) < 3 {
		t.Fatalf("expected at least 3 layers, got %d", len(ins.Layers))
	}
	if ins.Layers[0].ID != "repo_claude_md" {
		t.Fatalf("first layer = %q, want repo_claude_md", ins.Layers[0].ID)
	}
	if !strings.Contains(ins.FullMarkdown, "Repo rules") {
		t.Fatal("full markdown should include repo CLAUDE.md content")
	}
	if !strings.Contains(ins.FullMarkdown, runtimeMarkerBegin) {
		t.Fatal("full markdown should include Multica runtime markers")
	}
	foundRuntime := false
	for _, layer := range ins.Layers {
		if layer.ID == "multica_runtime_brief" {
			foundRuntime = true
			if len(layer.Sections) == 0 {
				t.Fatal("runtime layer should carry parsed sections")
			}
		}
		if layer.ID == "sidecar_issue_context" && layer.SourceKind != "sidecar" {
			t.Fatalf("sidecar layer source_kind = %q", layer.SourceKind)
		}
	}
	if !foundRuntime {
		t.Fatal("missing multica_runtime_brief layer")
	}
	if ins.ComposedTokens == 0 {
		t.Fatal("expected non-zero composed tokens")
	}
	if ins.Duplication.TotalTokens != ins.ComposedTokens {
		t.Fatalf("duplication total %d != composed %d", ins.Duplication.TotalTokens, ins.ComposedTokens)
	}
}

func TestLoadRepoPromptSnapshot_GitHubFetchUsesAuth(t *testing.T) {
	t.Setenv("CEREBRO_SKILLS_SYNC_TOKEN", "private-token")
	oldClient := repoPromptHTTPClient
	defer func() { repoPromptHTTPClient = oldClient }()
	var seen []string
	repoPromptHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = append(seen, req.URL.Host+req.URL.Path+":"+req.Header.Get("Authorization"))
		if req.URL.Host == "api.github.com" {
			return stringResponse(http.StatusOK, `{"default_branch":"trunk"}`), nil
		}
		if strings.HasSuffix(req.URL.Path, "/CLAUDE.md") {
			return stringResponse(http.StatusOK, "# Repo instructions\n"), nil
		}
		return stringResponse(http.StatusNotFound, ""), nil
	})}

	snap := LoadRepoPromptSnapshot(context.Background(), "https://github.com/firtal-group/firtal-cerebro", "")

	if !snap.CLAUDE.Found {
		t.Fatal("expected CLAUDE.md to be fetched")
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 GitHub requests, got %d: %#v", len(seen), seen)
	}
	for _, req := range seen {
		if !strings.HasSuffix(req, ":Bearer private-token") {
			t.Fatalf("request missing auth header: %s", req)
		}
	}
	if !strings.Contains(snap.CLAUDE.SourceURL, "/blob/trunk/CLAUDE.md") {
		t.Fatalf("unexpected source URL: %s", snap.CLAUDE.SourceURL)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func stringResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
