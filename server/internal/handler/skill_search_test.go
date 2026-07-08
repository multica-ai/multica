package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchSkillsReturnsNormalizedClawHubCandidates(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			if got := r.URL.Query().Get("q"); got != "react" {
				t.Fatalf("expected q=react, got %q", got)
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"results": []map[string]any{
					{
						"slug":        "react",
						"displayName": "React",
						"summary":     "React engineering skill",
						"ownerHandle": "ivangdavila",
					},
					{
						"slug":        "react-expert",
						"displayName": "React Expert",
						"summary":     "Advanced React review",
						"ownerHandle": "veeramanikandanr48",
					},
				},
			})
		case "/skills/react":
			writeJSON(w, http.StatusOK, map[string]any{
				"skill": map[string]any{
					"slug":        "react",
					"displayName": "React",
					"summary":     "React engineering skill",
					"stats": map[string]any{
						"installsAllTime": 62,
						"stars":           3,
					},
				},
			})
		case "/skills/react-expert":
			writeJSON(w, http.StatusOK, map[string]any{
				"skill": map[string]any{
					"slug":        "react-expert",
					"displayName": "React Expert",
					"summary":     "Advanced React review",
					"stats": map[string]any{
						"installsAllTime": 11,
						"stars":           7,
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	oldBase := clawHubAPIBase
	clawHubAPIBase = upstream.URL
	t.Cleanup(func() { clawHubAPIBase = oldBase })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/skills/search?q=react", nil)
	testHandler.SearchSkills(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchSkills: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("SearchSkills: decode response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %#v", len(got), got)
	}
	first := got[0]
	if first["name"] != "React" {
		t.Fatalf("expected normalized name, got %#v", first["name"])
	}
	if first["url"] != "https://clawhub.ai/ivangdavila/react" {
		t.Fatalf("expected importable ClawHub URL, got %#v", first["url"])
	}
	if first["source"] != "clawhub.ai" {
		t.Fatalf("expected source clawhub.ai, got %#v", first["source"])
	}
	if first["repo"] != nil {
		t.Fatalf("repo should be null when ClawHub has no GitHub repo field, got %#v", first["repo"])
	}
	if first["github_stars"] != nil {
		t.Fatalf("github_stars should not use ClawHub stars, got %#v", first["github_stars"])
	}
	if first["install_count"] != float64(62) {
		t.Fatalf("expected install_count from details stats, got %#v", first["install_count"])
	}
	if first["description"] != "React engineering skill" {
		t.Fatalf("expected description from summary, got %#v", first["description"])
	}
}

func TestSearchSkillsEmptyQueryReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/skills/search?q=", nil)
	testHandler.SearchSkills(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("SearchSkills empty query: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "query is required") {
		t.Fatalf("expected query is required error, got %s", w.Body.String())
	}
}

func TestSearchSkillsUpstreamUnavailableReturnsStructuredError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporary outage", http.StatusBadGateway)
	}))
	defer upstream.Close()

	oldBase := clawHubAPIBase
	clawHubAPIBase = upstream.URL
	t.Cleanup(func() { clawHubAPIBase = oldBase })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/skills/search?q=react", nil)
	testHandler.SearchSkills(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("SearchSkills outage: expected 502, got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got["code"] != "upstream_unavailable" {
		t.Fatalf("expected structured upstream_unavailable code, got %#v", got)
	}
}
