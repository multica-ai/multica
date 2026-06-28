package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const testLabelUUID = "11111111-1111-1111-1111-111111111111"

func newLabelCreateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("color", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newLabelUpdateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("color", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newLabelDeleteTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "delete"}
	cmd.Flags().String("output", "json", "")
	return cmd
}

func setCLITestServerEnv(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("MULTICA_SERVER_URL", serverURL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
}

func TestRunLabelCreateSendsExpectedRequest(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/labels" {
			t.Fatalf("path = %q, want /api/labels", r.URL.Path)
		}
		if r.Header.Get("X-Workspace-ID") != "ws-1" {
			t.Fatalf("X-Workspace-ID = %q, want ws-1", r.Header.Get("X-Workspace-ID"))
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    testLabelUUID,
			"name":  body["name"],
			"color": body["color"],
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newLabelCreateTestCmd()
	_ = cmd.Flags().Set("name", "Bug")
	_ = cmd.Flags().Set("color", "#ef4444")

	out, err := captureStdout(t, func() error { return runLabelCreate(cmd, nil) })
	if err != nil {
		t.Fatalf("runLabelCreate: %v", err)
	}
	if body["name"] != "Bug" || body["color"] != "#ef4444" {
		t.Fatalf("body = %#v, want name/color", body)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out)
	}
	if got["id"] != testLabelUUID || got["name"] != "Bug" {
		t.Fatalf("stdout = %#v, want created label", got)
	}
}

func TestRunLabelUpdateSendsOnlyChangedFields(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/api/labels/"+testLabelUUID {
			t.Fatalf("path = %q, want label path", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    testLabelUUID,
			"name":  body["name"],
			"color": "#3b82f6",
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newLabelUpdateTestCmd()
	_ = cmd.Flags().Set("name", "Feature")

	if _, err := captureStdout(t, func() error { return runLabelUpdate(cmd, []string{testLabelUUID}) }); err != nil {
		t.Fatalf("runLabelUpdate: %v", err)
	}
	if len(body) != 1 || body["name"] != "Feature" {
		t.Fatalf("body = %#v, want only name field", body)
	}
}

func TestRunLabelDeletePrintsJsonConfirmation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/api/labels/"+testLabelUUID {
			t.Fatalf("path = %q, want label path", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newLabelDeleteTestCmd()
	_ = cmd.Flags().Set("output", "json")

	out, err := captureStdout(t, func() error { return runLabelDelete(cmd, []string{testLabelUUID}) })
	if err != nil {
		t.Fatalf("runLabelDelete: %v", err)
	}
	if !strings.Contains(out, `"deleted": true`) || !strings.Contains(out, testLabelUUID) {
		t.Fatalf("stdout = %q, want deleted JSON confirmation", out)
	}
}

func TestRunLabelCreateRequiresNameAndColor(t *testing.T) {
	cmd := newLabelCreateTestCmd()
	if err := runLabelCreate(cmd, nil); err == nil || !strings.Contains(err.Error(), "--name is required") {
		t.Fatalf("runLabelCreate error = %v, want missing name", err)
	}

	_ = cmd.Flags().Set("name", "Bug")
	if err := runLabelCreate(cmd, nil); err == nil || !strings.Contains(err.Error(), "--color is required") {
		t.Fatalf("runLabelCreate error = %v, want missing color", err)
	}
}
