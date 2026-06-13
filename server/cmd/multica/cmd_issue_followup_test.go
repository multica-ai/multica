package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newIssueFollowupCreateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("title", "", "")
	cmd.Flags().String("kind", "", "")
	cmd.Flags().String("disposition", "", "")
	cmd.Flags().String("recommended-worker", "none", "")
	cmd.Flags().String("risk-level", "medium", "")
	cmd.Flags().String("approval-ask", "", "")
	cmd.Flags().String("info-ask", "", "")
	cmd.Flags().String("dedupe-key", "", "")
	cmd.Flags().String("done-condition", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().Bool("description-stdin", false, "")
	cmd.Flags().String("description-file", "", "")
	cmd.Flags().String("linked-pr-url", "", "")
	cmd.Flags().String("linked-comment-id", "", "")
	cmd.Flags().StringSlice("label", nil, "")
	cmd.Flags().String("assignee", "", "")
	cmd.Flags().String("assignee-id", "", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().String("parent-comment", "", "")
	cmd.Flags().Bool("no-parent-comment", false, "")
	cmd.Flags().Bool("allow-mention", false, "")
	cmd.Flags().Bool("plan-first", false, "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().Bool("quiet", false, "")
	return cmd
}

func newApprovalsListTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("kind", "all", "")
	cmd.Flags().String("assignee", "", "")
	cmd.Flags().String("assignee-id", "", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("since", "", "")
	cmd.Flags().Int("limit", 50, "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().Bool("full-id", false, "")
	return cmd
}

func captureStdoutString(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = old
	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	_ = r.Close()
	return string(out), runErr
}

func TestRunIssueFollowupCreateHappyPath(t *testing.T) {
	var createBody map[string]any
	metadataWrites := map[string]any{}
	commentBodies := []map[string]any{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/ITT-1":
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "parent-1",
				"identifier": "ITT-1",
				"title":      "Parent",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues":
			if got := r.URL.Query().Get("metadata"); got == "" {
				t.Errorf("dedupe lookup missing metadata filter")
			}
			json.NewEncoder(w).Encode(map[string]any{"issues": []map[string]any{}, "total": 0})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues":
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id":         "child-1",
				"identifier": "ITT-2",
				"title":      createBody["title"],
				"status":     createBody["status"],
				"metadata":   map[string]any{},
			})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/issues/child-1/metadata/"):
			key := strings.TrimPrefix(r.URL.Path, "/api/issues/child-1/metadata/")
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode metadata body: %v", err)
			}
			metadataWrites[key] = body["value"]
			json.NewEncoder(w).Encode(map[string]any{"metadata": metadataWrites})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/parent-1/comments":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode comment body: %v", err)
			}
			commentBodies = append(commentBodies, body)
			json.NewEncoder(w).Encode(map[string]any{"id": "comment-1", "content": body["content"]})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueFollowupCreateTestCmd()
	_ = cmd.Flags().Set("title", "Follow-up title")
	_ = cmd.Flags().Set("kind", "implementation")
	_ = cmd.Flags().Set("disposition", "auto-continue")
	_ = cmd.Flags().Set("recommended-worker", "Codex")
	_ = cmd.Flags().Set("risk-level", "low")
	_ = cmd.Flags().Set("dedupe-key", "followup:ITT-1:implementation:test")
	_ = cmd.Flags().Set("done-condition", "- 테스트 통과")
	_ = cmd.Flags().Set("plan-first", "true")

	out, err := captureStdoutString(t, func() error {
		return runIssueFollowupCreate(cmd, []string{"ITT-1"})
	})
	if err != nil {
		t.Fatalf("runIssueFollowupCreate: %v", err)
	}
	if createBody["parent_issue_id"] != "parent-1" {
		t.Fatalf("parent_issue_id = %#v", createBody["parent_issue_id"])
	}
	if createBody["status"] != "todo" {
		t.Fatalf("status = %#v, want todo", createBody["status"])
	}
	desc, _ := createBody["description"].(string)
	if !strings.Contains(desc, "## Execution Mode\nPlan-first.") {
		t.Fatalf("description missing Plan-first section: %s", desc)
	}
	if metadataWrites["followup_disposition"] != "auto-continue" {
		t.Fatalf("metadata followup_disposition = %#v", metadataWrites["followup_disposition"])
	}
	if metadataWrites["source_issue_id"] != "parent-1" {
		t.Fatalf("metadata source_issue_id = %#v", metadataWrites["source_issue_id"])
	}
	if len(commentBodies) != 1 {
		t.Fatalf("expected one parent comment, got %d", len(commentBodies))
	}
	content, _ := commentBodies[0]["content"].(string)
	if strings.Contains(content, "mention://agent/") || strings.Contains(content, "mention://member/") {
		t.Fatalf("auto parent comment should not include agent/member mention: %s", content)
	}
	var result followupCreateResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if result.Deduped {
		t.Fatalf("happy path should not be deduped")
	}
}

func TestRunIssueFollowupCreateReturnsExistingOnDedupe(t *testing.T) {
	postCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/ITT-1":
			json.NewEncoder(w).Encode(map[string]any{"id": "parent-1", "identifier": "ITT-1"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues":
			filter, _ := url.QueryUnescape(r.URL.Query().Get("metadata"))
			if !strings.Contains(filter, "followup_dedupe_key") {
				t.Fatalf("metadata filter missing dedupe key: %s", filter)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"issues": []map[string]any{{
					"id":         "child-1",
					"identifier": "ITT-2",
					"title":      "Existing",
					"status":     "in_progress",
					"metadata": map[string]any{
						"followup_disposition": "auto-continue",
						"followup_lifecycle":   "running",
						"followup_dedupe_key":  "followup:ITT-1:implementation:test",
					},
				}},
				"total": 1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues":
			postCalled = true
			http.Error(w, "must not create", http.StatusTeapot)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newIssueFollowupCreateTestCmd()
	_ = cmd.Flags().Set("title", "Follow-up title")
	_ = cmd.Flags().Set("kind", "implementation")
	_ = cmd.Flags().Set("disposition", "auto-continue")
	_ = cmd.Flags().Set("dedupe-key", "followup:ITT-1:implementation:test")

	out, err := captureStdoutString(t, func() error {
		return runIssueFollowupCreate(cmd, []string{"ITT-1"})
	})
	if err != nil {
		t.Fatalf("runIssueFollowupCreate: %v", err)
	}
	if postCalled {
		t.Fatal("dedupe match should not create a new issue")
	}
	var result followupCreateResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if !result.Deduped {
		t.Fatalf("dedupe match should report deduped=true")
	}
}

func TestFollowupInputGuards(t *testing.T) {
	base := followupCreateOptions{
		ParentRef:   resolvedID{ID: "parent-1", Display: "ITT-1"},
		Title:       "Title",
		Kind:        "implementation",
		Disposition: "auto-continue",
		RiskLevel:   "low",
	}
	t.Run("rejects unicode escape literal", func(t *testing.T) {
		opts := base
		opts.Description = `Korean literal: \uac00`
		if err := validateFollowupTextInputs(opts); err == nil || !strings.Contains(err.Error(), "raw UTF-8") {
			t.Fatalf("expected raw UTF-8 error, got %v", err)
		}
	})
	t.Run("accepts raw UTF-8", func(t *testing.T) {
		opts := base
		opts.Description = "한국어 본문"
		if err := validateFollowupTextInputs(opts); err != nil {
			t.Fatalf("raw UTF-8 should pass: %v", err)
		}
	})
	t.Run("rejects agent mention without allow flag", func(t *testing.T) {
		opts := base
		opts.ParentComment = "Ping [@Codex](mention://agent/cbe053f4-b53e-4786-81de-6554ddb86fad)"
		if err := validateFollowupTextInputs(opts); err == nil || !strings.Contains(err.Error(), "mentions are blocked") {
			t.Fatalf("expected mention guard error, got %v", err)
		}
	})
}

func TestRunApprovalsList(t *testing.T) {
	queries := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/issues" {
			http.NotFound(w, r)
			return
		}
		queries = append(queries, r.URL.RawQuery)
		filter, _ := url.QueryUnescape(r.URL.Query().Get("metadata"))
		switch {
		case strings.Contains(filter, "needs-approval"):
			json.NewEncoder(w).Encode(map[string]any{
				"issues": []map[string]any{{
					"id":         "approval-1",
					"identifier": "ITT-10",
					"title":      "Approve",
					"status":     "blocked",
					"updated_at": "2026-05-28T09:00:00Z",
					"metadata": map[string]any{
						"followup_disposition": "needs-approval",
						"risk_level":           "high",
						"approval_ask":         "진행할까요?",
						"source_issue_id":      "parent-1",
					},
				}},
				"total": 1,
			})
		case strings.Contains(filter, "needs-info"):
			json.NewEncoder(w).Encode(map[string]any{
				"issues": []map[string]any{{
					"id":         "info-1",
					"identifier": "ITT-11",
					"title":      "Info",
					"status":     "blocked",
					"updated_at": "2026-05-28T08:00:00Z",
					"metadata": map[string]any{
						"followup_disposition": "needs-info",
						"risk_level":           "medium",
						"info_ask":             "무엇이 필요합니다.",
						"source_issue_id":      "parent-1",
					},
				}},
				"total": 1,
			})
		default:
			t.Fatalf("unexpected metadata filter: %s", filter)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newApprovalsListTestCmd()
	out, err := captureStdoutString(t, func() error {
		return runApprovalsList(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runApprovalsList: %v", err)
	}
	if len(queries) != 2 {
		t.Fatalf("expected two disposition queries, got %d", len(queries))
	}
	var result approvalsListResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if result.Counts["needs-approval"] != 1 || result.Counts["needs-info"] != 1 {
		t.Fatalf("counts = %#v", result.Counts)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(result.Items))
	}
}
