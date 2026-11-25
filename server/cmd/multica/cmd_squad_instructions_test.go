package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSquadUpdateInstructionsFile(t *testing.T) {
	t.Chdir(t.TempDir())
	const content = "Rule one\r\n\r\nRègle deux ✓\n"
	outside := filepath.Join(t.TempDir(), "outside.md")
	for path, body := range map[string]string{"rules.md": content, "empty.md": "", outside: "outside"} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name          string
		flags         []string
		want, wantErr string
	}{
		{"verbatim", []string{"--instructions-file", "rules.md"}, content, ""},
		{"clear inline", []string{"--instructions="}, "", ""},
		{"both flags", []string{"--instructions=", "--instructions-file", "rules.md"}, "", "none of the others can be"},
		{"empty file", []string{"--instructions-file", "empty.md"}, "", "empty"},
		{"outside", []string{"--instructions-file", outside}, "", "current working directory"},
		{"outside allowed", []string{"--instructions-file", outside, "--allow-external-file"}, "outside", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				value, ok := body["instructions"]
				if r.Method != "PUT" || r.URL.Path != "/api/squads/test-id" || !ok || value != tc.want {
					t.Errorf("unexpected %s %s body %#v", r.Method, r.URL.Path, body)
				}
				_, _ = w.Write([]byte("{}"))
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			t.Setenv("MULTICA_TOKEN", "mat_synthetic_squad_test")
			cmd := newSquadUpdateCmd()
			cmd.SetArgs(append([]string{"test-id"}, tc.flags...))
			err := cmd.Execute()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || requests != 0 {
					t.Fatalf("expected %q with no request, got %v and %d requests", tc.wantErr, err, requests)
				}
			} else if err != nil || requests != 1 {
				t.Fatalf("expected one successful update, got %v and %d requests", err, requests)
			}
		})
	}
}
