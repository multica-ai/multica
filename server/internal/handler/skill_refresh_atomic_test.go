package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestRefreshSkill_GitHubBundleIsAtomic(t *testing.T) {
	for _, tc := range []struct {
		name         string
		treeStatus   int
		crawl        bool
		listingError string
		fileStatus   int
		shortBody    bool
		submodule    bool
		want         int
	}{
		{name: "complete tree", want: 200},
		{name: "complete crawl", crawl: true, want: 200},
		{name: "tree skips submodule", submodule: true, want: 200},
		{name: "crawl skips submodule", crawl: true, submodule: true, want: 200},
		{name: "tree unavailable complete crawl", treeStatus: 403, crawl: true, want: 200},
		{name: "tree and crawl unavailable", treeStatus: 403, listingError: "root status", want: 503},
		{name: "root listing unavailable", crawl: true, listingError: "root status", want: 503},
		{name: "root listing malformed", crawl: true, listingError: "root json", want: 503},
		{name: "root listing null", crawl: true, listingError: "root null", want: 503},
		{name: "nested listing unavailable", crawl: true, listingError: "nested status", want: 503},
		{name: "nested listing malformed", crawl: true, listingError: "nested json", want: 503},
		{name: "nested listing null", crawl: true, listingError: "nested null", want: 503},
		{name: "missing download url", crawl: true, listingError: "download url", want: 503},
		{name: "tree file unavailable", fileStatus: 503, want: 502},
		{name: "tree file missing", fileStatus: 404, want: 502},
		{name: "tree file body interrupted", shortBody: true, want: 502},
		{name: "crawl file unavailable", crawl: true, fileStatus: 503, want: 502},
		{name: "crawl file body interrupted", crawl: true, shortBody: true, want: 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userID := dbfx.User(t, "Refresh member", fmt.Sprintf("refresh-%d@example.invalid", time.Now().UnixNano()))
			dbfx.Member(t, testWorkspaceID, userID, "member")
			skill := dbfx.Insert(t, "skill", testutil.Cols{
				"workspace_id": testWorkspaceID, "created_by": userID, "name": "foo", "description": "old description", "content": "# Original",
				"config": `{"keep":"original","origin":{"type":"github","source_url":"https://github.com/acme/skills/tree/main/skills/foo"}}`,
			})
			dbfx.Insert(t, "skill_file", testutil.Cols{"skill_id": skill, "path": "old.md", "content": "old auxiliary content"})
			agent := dbfx.Agent(t, "Bound refresh agent", testRuntimeID, testutil.Cols{"workspace_id": testWorkspaceID, "owner_id": userID})
			dbfx.InsertNoID(t, "agent_skill", testutil.Cols{"agent_id": agent, "skill_id": skill}, "agent_id=$1 AND skill_id=$2", agent, skill)
			snapshot := func() string {
				var state string
				dbfx.QueryRow(t, `SELECT jsonb_build_object('skill',to_jsonb(s),
					'files',(SELECT jsonb_agg(to_jsonb(f) ORDER BY f.path) FROM skill_file f WHERE f.skill_id=s.id),
					'bindings',(SELECT jsonb_agg(to_jsonb(b) ORDER BY b.agent_id) FROM agent_skill b WHERE b.skill_id=s.id))::text
					FROM skill s WHERE s.id=$1`, skill).Scan(&state)
				return state
			}
			before := snapshot()
			client, requests := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/commits/"):
					if strings.HasSuffix(r.URL.Path, "/commits/main") {
						w.WriteHeader(http.StatusOK)
					} else {
						http.NotFound(w, r)
					}
				case strings.Contains(r.URL.Path, "/git/trees/"):
					if tc.treeStatus != 0 {
						w.WriteHeader(tc.treeStatus)
						return
					}
					tree := []githubTreeEntry{
						{Path: "skills/foo/SKILL.md", Type: "blob"},
						{Path: "skills/foo/refs/guide.md", Type: "blob", Size: 11},
						{Path: "skills/foo/image.png", Type: "blob"},
					}
					if tc.submodule {
						tree = append(tree, githubTreeEntry{Path: "skills/foo/vendor", Type: "commit"})
					}
					writeJSON(w, 200, githubTreeResponse{Truncated: tc.crawl, Tree: tree})
				case strings.HasSuffix(r.URL.Path, "/contents/skills/foo"):
					switch tc.listingError {
					case "root status":
						w.WriteHeader(403)
					case "root json":
						fmt.Fprint(w, "{invalid")
					case "root null":
						fmt.Fprint(w, "null")
					default:
						entries := []githubContentEntry{{Name: "refs", Path: "skills/foo/refs", Type: "dir"}, {Name: "image.png", Path: "skills/foo/image.png", Type: "file"}}
						if tc.submodule {
							// Directory listings report submodules as type "file" with a
							// null download_url and a git_url pointing at a tree.
							entries = append(entries, githubContentEntry{Name: "vendor", Path: "skills/foo/vendor", Type: "file",
								GitURL: "https://api.github.com/repos/acme/vendor/git/trees/855827c583bc30645ba427885caa40c5b81764d2"})
						}
						writeJSON(w, 200, entries)
					}
				case strings.HasSuffix(r.URL.Path, "/contents/skills/foo/refs"):
					switch tc.listingError {
					case "nested status":
						w.WriteHeader(503)
					case "nested json":
						fmt.Fprint(w, "{invalid")
					case "nested null":
						fmt.Fprint(w, "null")
					default:
						fileURL := "https://raw.githubusercontent.com/acme/skills/main/skills/foo/refs/guide.md"
						if tc.listingError == "download url" {
							fileURL = ""
						}
						writeJSON(w, 200, []githubContentEntry{{Name: "guide.md", Path: "skills/foo/refs/guide.md", Type: "file", DownloadURL: fileURL}})
					}
				case strings.HasSuffix(r.URL.Path, "/SKILL.md"):
					fmt.Fprint(w, "---\nname: foo\ndescription: refreshed\n---\n# Refreshed")
				case strings.HasSuffix(r.URL.Path, "/refs/guide.md"):
					if tc.fileStatus != 0 {
						w.WriteHeader(tc.fileStatus)
						return
					}
					if tc.shortBody {
						w.Header().Set("Content-Length", "100")
					}
					fmt.Fprint(w, "fresh guide")
				default:
					t.Errorf("unexpected upstream request: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			previous := http.DefaultTransport
			http.DefaultTransport = client.Transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			request := func(id string) *http.Request {
				return testutil.WithURLParams(newRequestAsUser(userID, "POST", "/api/skills/"+id+"/refresh", nil), "id", id)
			}
			if tc.want != http.StatusOK {
				testutil.Call(t, testHandler.RefreshSkill, request(skill)).Want(tc.want)
				if after := snapshot(); after != before {
					t.Fatalf("failed refresh changed skill, files or bindings:\nbefore=%s\nafter=%s", before, after)
				}
			} else {
				var response SkillWithFilesResponse
				testutil.Call(t, testHandler.RefreshSkill, request(skill)).Want(200).JSON(&response)
				if response.CreatedBy == nil || *response.CreatedBy != userID || response.Content != "---\nname: foo\ndescription: refreshed\n---\n# Refreshed" || len(response.Files) != 1 || response.Files[0].Path != "refs/guide.md" || response.Files[0].Content != "fresh guide" {
					t.Fatalf("incomplete or incorrectly authorized refresh: %+v", response)
				}
				var bound bool
				dbfx.QueryRow(t, "SELECT EXISTS(SELECT 1 FROM agent_skill WHERE agent_id=$1 AND skill_id=$2)", agent, skill).Scan(&bound)
				if !bound || skillFilePaths(t, skill)["old.md"] {
					t.Fatal("successful refresh lost binding or retained obsolete file")
				}
			}
			foreign := dbfx.Insert(t, "skill", testutil.Cols{"workspace_id": testWorkspaceID, "created_by": testUserID, "name": "foreign", "content": "foreign original"})
			requestCount := len(*requests)
			testutil.Call(t, testHandler.RefreshSkill, request(foreign)).Want(403)
			if len(*requests) != requestCount {
				t.Fatal("unauthorized refresh contacted upstream")
			}
		})
	}
}

func TestRefreshSkill_ClawHubDownloadFailurePreservesBundle(t *testing.T) {
	for _, failure := range []string{"unavailable", "missing", "interrupted body"} {
		t.Run(failure, func(t *testing.T) {
			userID := dbfx.User(t, "Refresh member", fmt.Sprintf("refresh-%d@example.invalid", time.Now().UnixNano()))
			dbfx.Member(t, testWorkspaceID, userID, "member")
			skill := dbfx.Insert(t, "skill", testutil.Cols{"workspace_id": testWorkspaceID, "created_by": userID, "name": "old name", "content": "# Original", "config": clawhubOriginConfig("atomic")})
			dbfx.Insert(t, "skill_file", testutil.Cols{"skill_id": skill, "path": "old.md", "content": "old guide"})
			var before string
			dbfx.QueryRow(t, "SELECT to_jsonb(s)::text FROM skill s WHERE id=$1", skill).Scan(&before)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/versions/1.0.0"):
					fmt.Fprint(w, `{"version":{"files":[{"path":"SKILL.md"},{"path":"guide.md"}]}}`)
				case strings.HasSuffix(r.URL.Path, "/file"):
					if r.URL.Query().Get("path") == "SKILL.md" {
						fmt.Fprint(w, "# Fresh")
					} else if failure == "interrupted body" {
						w.Header().Set("Content-Length", "100")
						fmt.Fprint(w, "short")
					} else if failure == "missing" {
						w.WriteHeader(404)
					} else {
						w.WriteHeader(503)
					}
				default:
					fmt.Fprint(w, `{"skill":{"displayName":"new name","tags":{"latest":"1.0.0"}}}`)
				}
			}))
			t.Cleanup(srv.Close)
			previous := clawHubAPIBase
			clawHubAPIBase = srv.URL
			t.Cleanup(func() { clawHubAPIBase = previous })
			testutil.Call(t, testHandler.RefreshSkill, testutil.WithURLParams(newRequestAsUser(userID, "POST", "/api/skills/"+skill+"/refresh", nil), "id", skill)).Want(502)
			var after, guide string
			dbfx.QueryRow(t, "SELECT to_jsonb(s)::text FROM skill s WHERE id=$1", skill).Scan(&after)
			dbfx.QueryRow(t, "SELECT content FROM skill_file WHERE skill_id=$1 AND path='old.md'", skill).Scan(&guide)
			if after != before || guide != "old guide" {
				t.Fatal("failed ClawHub download changed the existing bundle")
			}
		})
	}
}
