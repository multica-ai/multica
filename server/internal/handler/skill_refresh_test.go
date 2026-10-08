package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// createImportTargetSkillWithConfig is createImportTargetSkill with an explicit
// config blob, used to seed provenance (config.origin) for refresh tests.
func createImportTargetSkillWithConfig(t *testing.T, name, ownerID, configJSON string, files map[string]string) string {
	t.Helper()

	var skillID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, $2, 'original description', '# original', $3::jsonb, $4)
		RETURNING id
	`, testWorkspaceID, name, configJSON, ownerID).Scan(&skillID); err != nil {
		t.Fatalf("create target skill: %v", err)
	}
	for path, content := range files {
		if _, err := testPool.Exec(context.Background(), `
			INSERT INTO skill_file (skill_id, path, content) VALUES ($1, $2, $3)
		`, skillID, path, content); err != nil {
			t.Fatalf("create skill file: %v", err)
		}
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, skillID)
	})
	return skillID
}

// installClawHubFixture serves a complete successful ClawHub bundle for slug
// and swaps clawHubAPIBase to it. The returned counter tracks how many
// requests reached the fixture, letting tests assert that a rejected refresh
// never contacted the upstream.
func installClawHubFixture(t *testing.T, slug, displayName, summary string, files map[string]string) *int {
	t.Helper()

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch {
		case r.URL.Path == "/api/v1/skills/"+slug:
			writeJSON(w, http.StatusOK, map[string]any{
				"skill": map[string]any{
					"slug": slug, "displayName": displayName, "summary": summary,
					"tags": map[string]string{"latest": "1.0.0"},
				},
			})
		case r.URL.Path == "/api/v1/skills/"+slug+"/versions/1.0.0":
			fileList := make([]map[string]any, 0, len(files))
			for p, c := range files {
				fileList = append(fileList, map[string]any{"path": p, "size": len(c)})
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"version": map[string]any{"version": "1.0.0", "files": fileList},
			})
		case r.URL.Path == "/api/v1/skills/"+slug+"/file":
			if content, ok := files[r.URL.Query().Get("path")]; ok {
				w.Write([]byte(content))
				return
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	prev := clawHubAPIBase
	clawHubAPIBase = srv.URL + "/api/v1"
	t.Cleanup(func() { clawHubAPIBase = prev; srv.Close() })
	return &requests
}

func clawhubOriginConfig(slug string) string {
	return fmt.Sprintf(`{"origin": {"type": "clawhub", "source_url": "https://clawhub.ai/acme/%s", "slug": "%s"}}`, slug, slug)
}

func doRefreshSkill(t *testing.T, userID, skillID string) *httptest.ResponseRecorder {
	t.Helper()

	w := httptest.NewRecorder()
	req := withURLParams(
		newRequestAsUser(userID, http.MethodPost, "/api/skills/"+skillID+"/refresh", nil),
		"id", skillID,
	)
	testHandler.RefreshSkill(w, req)
	return w
}

func skillCreatedAt(t *testing.T, skillID string) string {
	t.Helper()

	var createdAt string
	if err := testPool.QueryRow(context.Background(),
		`SELECT created_at::text FROM skill WHERE id = $1`, skillID).Scan(&createdAt); err != nil {
		t.Fatalf("get skill created_at: %v", err)
	}
	return createdAt
}

func skillFilePaths(t *testing.T, skillID string) map[string]bool {
	t.Helper()

	rows, err := testPool.Query(context.Background(),
		`SELECT path FROM skill_file WHERE skill_id = $1`, skillID)
	if err != nil {
		t.Fatalf("list skill files: %v", err)
	}
	defer rows.Close()
	paths := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan skill file path: %v", err)
		}
		paths[p] = true
	}
	return paths
}

func TestRefreshSkill_UpdatesContentPreservingIdentityAndBindings(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	slug := fmt.Sprintf("refresh-keep-%d", time.Now().UnixNano())
	oldName := slug + "-old"
	newName := slug + "-renamed"
	// Custom config key alongside origin: refresh must preserve it (merge, not
	// wholesale replace).
	config := fmt.Sprintf(`{"custom_key": "keep-me", "origin": {"type": "clawhub", "source_url": "https://clawhub.ai/acme/%s", "slug": "%s"}}`, slug, slug)
	skillID := createImportTargetSkillWithConfig(t, oldName, testUserID, config, map[string]string{
		"prune.md": "should be removed",
	})
	bindAgentToSkill(t, skillID)
	createdAtBefore := skillCreatedAt(t, skillID)

	installClawHubFixture(t, slug, newName, "fresh summary", map[string]string{
		"SKILL.md": "# refreshed body",
		"new.md":   "brand new file",
	})

	w := doRefreshSkill(t, testUserID, skillID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp SkillWithFilesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Identity preserved.
	if resp.ID != skillID {
		t.Fatalf("refresh must preserve UUID: got %q, want %q", resp.ID, skillID)
	}
	if resp.CreatedBy == nil || *resp.CreatedBy != testUserID {
		t.Fatalf("created_by not preserved: %v", resp.CreatedBy)
	}
	if got := skillCreatedAt(t, skillID); got != createdAtBefore {
		t.Fatalf("created_at changed: %q -> %q", createdAtBefore, got)
	}
	if n := countAgentSkillBindings(t, skillID); n != 1 {
		t.Fatalf("expected agent binding to survive refresh, got %d", n)
	}

	// Upstream rename adopted; content and description replaced.
	if resp.Name != newName {
		t.Fatalf("name = %q, want upstream name %q", resp.Name, newName)
	}
	if n := countSkillsByName(t, oldName); n != 0 {
		t.Fatalf("old name must be gone after rename adoption, found %d", n)
	}
	if resp.Description != "fresh summary" {
		t.Fatalf("description = %q, want %q", resp.Description, "fresh summary")
	}
	if resp.Content != "# refreshed body" {
		t.Fatalf("content = %q, want refreshed body", resp.Content)
	}

	// Files fully replaced: prune.md gone, new.md present.
	paths := skillFilePaths(t, skillID)
	if paths["prune.md"] || !paths["new.md"] {
		t.Fatalf("files = %v, want new.md without prune.md", paths)
	}

	// Config merged: origin rewritten, custom key preserved.
	configMap, ok := resp.Config.(map[string]any)
	if !ok {
		t.Fatalf("config is not an object: %T", resp.Config)
	}
	if configMap["custom_key"] != "keep-me" {
		t.Fatalf("custom config key not preserved: %v", configMap)
	}
	origin, ok := configMap["origin"].(map[string]any)
	if !ok || origin["type"] != "clawhub" || origin["slug"] != slug {
		t.Fatalf("origin not rewritten from fresh fetch: %v", configMap["origin"])
	}
}

func TestRefreshSkill_UpstreamRenameCollisionReturns409(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	slug := fmt.Sprintf("refresh-collision-%d", time.Now().UnixNano())
	oldName := slug + "-old"
	takenName := slug + "-taken"
	skillID := createImportTargetSkillWithConfig(t, oldName, testUserID, clawhubOriginConfig(slug), nil)
	createImportTargetSkill(t, takenName, testUserID, nil)

	installClawHubFixture(t, slug, takenName, "incoming", map[string]string{"SKILL.md": "# incoming"})

	w := doRefreshSkill(t, testUserID, skillID)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	// Whole refresh rolled back: name, description, and content unchanged.
	name, desc, content, _ := getSkillRow(t, skillID)
	if name != oldName || desc != "original description" || content != "# original" {
		t.Fatalf("collision must not mutate the skill: name=%q desc=%q content=%q", name, desc, content)
	}
}

func TestRefreshSkill_AdminCanRefresh(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Skill created by a plain member; the caller (testUserID) is the
	// workspace owner. Refresh follows canManageSkill, so this must succeed —
	// unlike the creator-only import-overwrite path.
	creatorID := createRuntimeLocalSkillTestMember(t, "member")
	slug := fmt.Sprintf("refresh-admin-%d", time.Now().UnixNano())
	skillID := createImportTargetSkillWithConfig(t, slug, creatorID, clawhubOriginConfig(slug), nil)

	installClawHubFixture(t, slug, slug, "admin refreshed", map[string]string{"SKILL.md": "# refreshed"})

	w := doRefreshSkill(t, testUserID, skillID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// Creator still preserved even when an admin refreshes.
	if _, desc, _, createdBy := getSkillRow(t, skillID); desc != "admin refreshed" || createdBy != creatorID {
		t.Fatalf("desc=%q createdBy=%q, want refreshed desc with original creator %q", desc, createdBy, creatorID)
	}
}

func TestRefreshSkill_PlainMemberNonCreatorForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	memberID := createRuntimeLocalSkillTestMember(t, "member")
	slug := fmt.Sprintf("refresh-forbidden-%d", time.Now().UnixNano())
	skillID := createImportTargetSkillWithConfig(t, slug, testUserID, clawhubOriginConfig(slug), nil)

	requests := installClawHubFixture(t, slug, slug, "incoming", map[string]string{"SKILL.md": "# incoming"})

	w := doRefreshSkill(t, memberID, skillID)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if _, desc, _, _ := getSkillRow(t, skillID); desc != "original description" {
		t.Fatalf("forbidden refresh must not mutate the skill, description = %q", desc)
	}
	// Permission is checked before the fetch: the upstream must never be hit.
	if *requests != 0 {
		t.Fatalf("forbidden refresh must not contact the upstream, got %d requests", *requests)
	}
}

func TestRefreshSkill_ManualSkillNotRefreshable(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	name := fmt.Sprintf("refresh-manual-%d", time.Now().UnixNano())
	skillID := createImportTargetSkill(t, name, testUserID, nil)

	w := doRefreshSkill(t, testUserID, skillID)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if _, desc, _, _ := getSkillRow(t, skillID); desc != "original description" {
		t.Fatalf("non-refreshable refresh must not mutate the skill, description = %q", desc)
	}
}

func TestRefreshSkill_RuntimeLocalOriginNotRefreshable(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	name := fmt.Sprintf("refresh-runtime-local-%d", time.Now().UnixNano())
	config := `{"origin": {"type": "runtime_local", "runtime_id": "r-1", "provider": "claude", "source_path": "~/.claude/skills/x"}}`
	skillID := createImportTargetSkillWithConfig(t, name, testUserID, config, nil)

	w := doRefreshSkill(t, testUserID, skillID)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRefreshSkill_OriginHostMismatchRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	name := fmt.Sprintf("refresh-mismatch-%d", time.Now().UnixNano())
	// A hand-edited origin: claims github but points at clawhub. Must be
	// rejected without any fetch.
	config := `{"origin": {"type": "github", "source_url": "https://clawhub.ai/acme/x"}}`
	skillID := createImportTargetSkillWithConfig(t, name, testUserID, config, nil)

	w := doRefreshSkill(t, testUserID, skillID)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRefreshSkill_UpstreamGoneReturns502(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	slug := fmt.Sprintf("refresh-gone-%d", time.Now().UnixNano())
	skillID := createImportTargetSkillWithConfig(t, slug, testUserID, clawhubOriginConfig(slug), nil)

	// Fixture knows no routes: the skill lookup 404s upstream.
	srv := httptest.NewServer(http.NotFoundHandler())
	prev := clawHubAPIBase
	clawHubAPIBase = srv.URL + "/api/v1"
	t.Cleanup(func() { clawHubAPIBase = prev; srv.Close() })

	w := doRefreshSkill(t, testUserID, skillID)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
	if _, desc, _, _ := getSkillRow(t, skillID); desc != "original description" {
		t.Fatalf("failed refresh must not mutate the skill, description = %q", desc)
	}
}

func TestRefreshSkill_CapExceededReturns413(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	slug := fmt.Sprintf("refresh-cap-%d", time.Now().UnixNano())
	skillID := createImportTargetSkillWithConfig(t, slug, testUserID, clawhubOriginConfig(slug), nil)

	installClawHubFixture(t, slug, slug, "too big now", map[string]string{
		"SKILL.md": "# ok",
		"big.md":   strings.Repeat("a", maxImportFileSize+1),
	})

	w := doRefreshSkill(t, testUserID, skillID)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body.String())
	}
	if _, desc, _, _ := getSkillRow(t, skillID); desc != "original description" {
		t.Fatalf("cap-exceeded refresh must not mutate the skill, description = %q", desc)
	}
}

func TestParseSkillOrigin(t *testing.T) {
	cases := []struct {
		name   string
		config string
		wantOK bool
		want   skillOriginRef
	}{
		{name: "nil config", config: "", wantOK: false},
		{name: "empty object", config: "{}", wantOK: false},
		{name: "origin not an object", config: `{"origin": "github"}`, wantOK: false},
		{name: "origin without type", config: `{"origin": {"source_url": "https://github.com/a/b"}}`, wantOK: false},
		{
			name:   "origin without source_url",
			config: `{"origin": {"type": "github"}}`,
			wantOK: true,
			want:   skillOriginRef{Type: "github", SourceURL: ""},
		},
		{
			name:   "full origin with whitespace",
			config: `{"origin": {"type": "skills_sh", "source_url": " https://skills.sh/a/b/c "}}`,
			wantOK: true,
			want:   skillOriginRef{Type: "skills_sh", SourceURL: "https://skills.sh/a/b/c"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var raw []byte
			if tc.config != "" {
				raw = []byte(tc.config)
			}
			got, ok := parseSkillOrigin(raw)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Fatalf("origin = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestFetchImportedSkillFromOrigin_RejectsNonRefreshable(t *testing.T) {
	client := &http.Client{}
	cases := []skillOriginRef{
		{Type: "manual", SourceURL: "https://github.com/a/b"},
		{Type: "runtime_local", SourceURL: ""},
		{Type: "github", SourceURL: ""},
		{Type: "github", SourceURL: "https://clawhub.ai/acme/x"},  // host mismatch
		{Type: "clawhub", SourceURL: "https://github.com/acme/x"}, // host mismatch
		{Type: "skills_sh", SourceURL: "https://example.com/a/b"}, // unsupported host
	}
	for _, origin := range cases {
		if _, err := fetchImportedSkillFromOrigin(t.Context(), client, origin); !errors.Is(err, errSkillNotRefreshable) {
			t.Fatalf("origin %+v: err = %v, want errSkillNotRefreshable", origin, err)
		}
	}
}

func TestFetchImportedSkillFromOrigin_GitHubDispatch(t *testing.T) {
	client, _ := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills/commits/main":
				w.Write([]byte("deadbeef"))
			case "/repos/acme/skills/contents/foo":
				writeJSON(w, http.StatusOK, []githubContentEntry{})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			if r.URL.Path == "/acme/skills/main/foo/SKILL.md" {
				w.Write([]byte("---\nname: foo\ndescription: refreshed\n---\nbody"))
				return
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	origin := skillOriginRef{Type: "github", SourceURL: "https://github.com/acme/skills/tree/main/foo"}
	imported, err := fetchImportedSkillFromOrigin(t.Context(), client, origin)
	if err != nil {
		t.Fatalf("fetchImportedSkillFromOrigin: %v", err)
	}
	if imported.name != "foo" || imported.description != "refreshed" {
		t.Fatalf("imported = %q / %q, want foo / refreshed", imported.name, imported.description)
	}
	// The fresh origin re-records the re-resolved ref and path.
	if imported.origin["type"] != "github" || imported.origin["ref"] != "main" || imported.origin["path"] != "foo" {
		t.Fatalf("origin = %v, want github/main/foo", imported.origin)
	}
}
