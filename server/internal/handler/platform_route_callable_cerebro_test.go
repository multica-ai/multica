package handler

import (
	"net/http"
	"net/url"
	"slices"
	"testing"
)

func TestExactPlatformCallableCannotSpoofAnotherOperationInFamily(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		capability string
		callable   string
		want       bool
	}{
		{name: "issue read exact", method: http.MethodGet, path: "/api/issues/abc", capability: "read_issues", callable: "get_issue", want: true},
		{name: "issue query exact", method: http.MethodPost, path: "/api/cerebro/issues/query", capability: "read_issues", callable: "search_issues", want: true},
		{name: "issue query spoofed get", method: http.MethodPost, path: "/api/cerebro/issues/query", capability: "read_issues", callable: "get_issue"},
		{name: "issue read cross route", method: http.MethodGet, path: "/api/issues/abc", capability: "read_issues", callable: "list_comments"},
		{name: "comment delete exact", method: http.MethodDelete, path: "/api/comments/abc", capability: "update_comment", callable: "delete_comment", want: true},
		{name: "comment delete spoofed update", method: http.MethodDelete, path: "/api/comments/abc", capability: "update_comment", callable: "update_comment"},
		{name: "attachment upload exact", method: http.MethodPost, path: "/api/upload-file", capability: "manage_artifacts", callable: "add_attachment", want: true},
		{name: "attachment upload spoofed create", method: http.MethodPost, path: "/api/upload-file", capability: "manage_artifacts", callable: "create_artifact"},
		{name: "folder suggestion exact", method: http.MethodPost, path: "/api/artifact-folder-suggestions/abc/accept", capability: "manage_artifacts", callable: "accept_artifact_folder_suggestion", want: true},
		{name: "folder suggestion spoofed reject", method: http.MethodPost, path: "/api/artifact-folder-suggestions/abc/accept", capability: "manage_artifacts", callable: "reject_artifact_folder_suggestion"},
		{name: "artifact list exact", method: http.MethodGet, path: "/api/issues/abc/artifacts", capability: "manage_artifacts", callable: "list_artifacts", want: true},
		{name: "artifact list cannot search", method: http.MethodGet, path: "/api/issues/abc/artifacts", capability: "manage_artifacts", callable: "search_artifacts"},
		{name: "artifact search exact", method: http.MethodGet, path: "/api/artifacts", capability: "manage_artifacts", callable: "search_artifacts", want: true},
		{name: "artifact search cannot list", method: http.MethodGet, path: "/api/artifacts", capability: "manage_artifacts", callable: "list_artifacts"},
		{name: "artifact scope move exact", method: http.MethodPut, path: "/api/artifacts/abc/scope", capability: "manage_artifacts", callable: "move_artifact", want: true},
		{name: "artifact scope cannot update", method: http.MethodPut, path: "/api/artifacts/abc/scope", capability: "manage_artifacts", callable: "update_artifact"},
		{name: "artifact folder exact", method: http.MethodPut, path: "/api/artifacts/abc/folder", capability: "manage_artifacts", callable: "set_artifact_folder", want: true},
		{name: "artifact folder cannot move scope", method: http.MethodPut, path: "/api/artifacts/abc/folder", capability: "manage_artifacts", callable: "move_artifact"},
		{name: "note read alias exact", method: http.MethodGet, path: "/api/notes/abc", capability: "manage_artifacts", callable: "get_artifact", want: true},
		{name: "note read alias cannot search", method: http.MethodGet, path: "/api/notes/abc", capability: "manage_artifacts", callable: "search_artifacts"},
		{name: "note search alias exact", method: http.MethodGet, path: "/api/notes/search", capability: "manage_artifacts", callable: "search_artifacts", want: true},
		{name: "note search alias cannot read", method: http.MethodGet, path: "/api/notes/search", capability: "manage_artifacts", callable: "get_artifact"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &http.Request{Method: tc.method, URL: &url.URL{Path: tc.path}, Header: http.Header{"X-Multica-Callable": []string{tc.callable}}}
			got, ok := exactPlatformCallable(r, tc.capability)
			if ok != tc.want || (ok && got != tc.callable) {
				t.Fatalf("exactPlatformCallable = %q/%v, want %q/%v", got, ok, tc.callable, tc.want)
			}
		})
	}
}

func TestPlatformRouteCallablesCoverMutatingSideOperations(t *testing.T) {
	tests := []struct {
		method     string
		path       string
		capability string
		want       []string
	}{
		{http.MethodDelete, "/api/attachments/abc", "manage_artifacts", []string{"delete_attachment"}},
		{http.MethodPost, "/api/comments/abc/reactions", "update_comment", []string{"add_comment_reaction"}},
		{http.MethodDelete, "/api/comments/abc/reactions", "update_comment", []string{"remove_comment_reaction"}},
		{http.MethodPost, "/api/comments/abc/move-to-subissue", "update_comment", []string{"move_comment_to_subissue"}},
		{http.MethodPut, "/api/artifact-folders/abc/visibility", "manage_artifacts", []string{"set_artifact_folder_visibility"}},
		{http.MethodPost, "/api/cerebro/workflows/abc/regenerate-token", "manage_workflows", []string{"regenerate_workflow_token"}},
		{http.MethodPost, "/api/cerebro/workflows/abc/human-checks/check-1/approve", "manage_workflows", []string{"approve_workflow_human_check"}},
		{http.MethodPost, "/api/cerebro/workflows/_test/cron-sweep", "manage_workflows", []string{"sweep_workflow_cron"}},
	}
	for _, tc := range tests {
		r := &http.Request{Method: tc.method, URL: &url.URL{Path: tc.path}}
		if got := platformRouteCallables(r, tc.capability); !slices.Equal(got, tc.want) {
			t.Errorf("%s %s = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}
