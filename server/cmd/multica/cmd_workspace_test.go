package main

import (
	"strings"
	"testing"
)

// resetWorkspaceUpdateFlags clears every flag on workspaceUpdateCmd and marks
// each as not-Changed. The cobra.Command instance is a process-wide singleton,
// so previous subtests leak state into the next one without this guard.
func resetWorkspaceUpdateFlags(t *testing.T) {
	t.Helper()
	flags := workspaceUpdateCmd.Flags()
	for _, name := range []string{"name", "description", "context", "issue-prefix"} {
		_ = flags.Set(name, "")
		if f := flags.Lookup(name); f != nil {
			f.Changed = false
		}
	}
	for _, name := range []string{"description-stdin", "context-stdin"} {
		_ = flags.Set(name, "false")
		if f := flags.Lookup(name); f != nil {
			f.Changed = false
		}
	}
}

func setStringFlag(t *testing.T, name, value string) {
	t.Helper()
	if err := workspaceUpdateCmd.Flags().Set(name, value); err != nil {
		t.Fatalf("set --%s: %v", name, err)
	}
}

func setBoolFlag(t *testing.T, name string, value bool) {
	t.Helper()
	v := "false"
	if value {
		v = "true"
	}
	if err := workspaceUpdateCmd.Flags().Set(name, v); err != nil {
		t.Fatalf("set --%s: %v", name, err)
	}
}

func TestBuildWorkspaceUpdateBody(t *testing.T) {
	t.Run("only changed flags appear in body", func(t *testing.T) {
		resetWorkspaceUpdateFlags(t)
		setStringFlag(t, "name", "Acme Eng")

		body, err := buildWorkspaceUpdateBody(workspaceUpdateCmd)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got, _ := body["name"].(string); got != "Acme Eng" {
			t.Errorf("name = %v, want Acme Eng", body["name"])
		}
		for _, key := range []string{"description", "context", "issue_prefix"} {
			if _, present := body[key]; present {
				t.Errorf("%s should not appear when its flag was not set, got %v", key, body)
			}
		}
	})

	t.Run("multiple fields combine into one PATCH body", func(t *testing.T) {
		resetWorkspaceUpdateFlags(t)
		setStringFlag(t, "name", "Acme")
		setStringFlag(t, "description", `line1\nline2`)
		setStringFlag(t, "issue-prefix", "ENG")

		body, err := buildWorkspaceUpdateBody(workspaceUpdateCmd)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if body["name"] != "Acme" {
			t.Errorf("name = %v, want Acme", body["name"])
		}
		// resolveTextFlag decodes \n in inline values.
		if body["description"] != "line1\nline2" {
			t.Errorf("description = %q, want decoded newline", body["description"])
		}
		if body["issue_prefix"] != "ENG" {
			t.Errorf("issue_prefix = %v, want ENG", body["issue_prefix"])
		}
	})

	t.Run("inline + stdin is rejected for description", func(t *testing.T) {
		resetWorkspaceUpdateFlags(t)
		setStringFlag(t, "description", "inline")
		setBoolFlag(t, "description-stdin", true)

		if _, err := buildWorkspaceUpdateBody(workspaceUpdateCmd); err == nil {
			t.Fatalf("expected mutually-exclusive error for --description and --description-stdin")
		}
	})

	t.Run("context-stdin reads from stdin", func(t *testing.T) {
		resetWorkspaceUpdateFlags(t)
		setBoolFlag(t, "context-stdin", true)

		stdinBody := "first\nsecond line with literal \\n\n"
		var got map[string]any
		pipeStdin(t, stdinBody, func() {
			b, err := buildWorkspaceUpdateBody(workspaceUpdateCmd)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			got = b
		})
		want := "first\nsecond line with literal \\n"
		if got["context"] != want {
			t.Errorf("context = %q, want %q", got["context"], want)
		}
	})

	t.Run("empty issue-prefix is rejected", func(t *testing.T) {
		resetWorkspaceUpdateFlags(t)
		setStringFlag(t, "issue-prefix", "")
		// Force Changed=true so the flag is treated as "explicitly passed".
		if f := workspaceUpdateCmd.Flags().Lookup("issue-prefix"); f != nil {
			f.Changed = true
		}

		_, err := buildWorkspaceUpdateBody(workspaceUpdateCmd)
		if err == nil {
			t.Fatalf("expected error when --issue-prefix is empty")
		}
		if !strings.Contains(err.Error(), "cannot be empty") {
			t.Errorf("error = %q, want it to mention 'cannot be empty'", err)
		}
	})

	t.Run("whitespace-only issue-prefix is rejected", func(t *testing.T) {
		resetWorkspaceUpdateFlags(t)
		setStringFlag(t, "issue-prefix", "   ")
		if f := workspaceUpdateCmd.Flags().Lookup("issue-prefix"); f != nil {
			f.Changed = true
		}
		if _, err := buildWorkspaceUpdateBody(workspaceUpdateCmd); err == nil {
			t.Fatalf("expected error when --issue-prefix is whitespace-only")
		}
	})

	t.Run("no flags set produces empty body", func(t *testing.T) {
		resetWorkspaceUpdateFlags(t)
		body, err := buildWorkspaceUpdateBody(workspaceUpdateCmd)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if len(body) != 0 {
			t.Errorf("body = %v, want empty", body)
		}
	})
}
