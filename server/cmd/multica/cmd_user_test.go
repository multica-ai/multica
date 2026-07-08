package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

func newUserProfileGetTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newUserProfileUpdateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().String("description", "", "")
	cmd.Flags().Bool("description-stdin", false, "")
	cmd.Flags().String("description-file", "", "")
	cmd.Flags().Bool("clear", false, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func TestRunUserProfileGetPrintsMeJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/me" {
			t.Fatalf("path = %q, want /api/me", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                  "user-1",
			"name":                "Ada",
			"email":               "ada@example.com",
			"profile_description": "Maintainer",
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newUserProfileGetTestCmd()
	out, err := captureStdout(t, func() error { return runUserProfileGet(cmd, nil) })
	if err != nil {
		t.Fatalf("runUserProfileGet: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out)
	}
	if got["profile_description"] != "Maintainer" {
		t.Fatalf("stdout = %#v, want profile description", got)
	}
}

func TestRunUserProfileUpdateSendsResolvedDescription(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/api/me" {
			t.Fatalf("path = %q, want /api/me", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                  "user-1",
			"name":                "Ada",
			"email":               "ada@example.com",
			"profile_description": body["profile_description"],
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newUserProfileUpdateTestCmd()
	_ = cmd.Flags().Set("description", `Reviewer\nTypeScript`)

	if _, err := captureStdout(t, func() error { return runUserProfileUpdate(cmd, nil) }); err != nil {
		t.Fatalf("runUserProfileUpdate: %v", err)
	}
	if body["profile_description"] != "Reviewer\nTypeScript" {
		t.Fatalf("body = %#v, want decoded description", body)
	}
}

func TestRunUserProfileUpdateClearSendsEmptyDescription(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                  "user-1",
			"profile_description": body["profile_description"],
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	cmd := newUserProfileUpdateTestCmd()
	_ = cmd.Flags().Set("clear", "true")

	if _, err := captureStdout(t, func() error { return runUserProfileUpdate(cmd, nil) }); err != nil {
		t.Fatalf("runUserProfileUpdate: %v", err)
	}
	if body["profile_description"] != "" {
		t.Fatalf("profile_description = %#v, want empty string", body["profile_description"])
	}
}
