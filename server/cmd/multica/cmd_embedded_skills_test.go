package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mattn/go-shellwords"
	"github.com/spf13/cobra"
)

func executeEmbeddedSkills(args ...string) (string, error) {
	root := &cobra.Command{Use: "multica", SilenceErrors: true, SilenceUsage: true}
	root.AddGroup(&cobra.Group{ID: groupAdditional, Title: "Additional"})
	root.PersistentFlags().String("profile", "", "")
	root.PersistentFlags().String("server-url", "", "")
	root.AddCommand(newEmbeddedSkillsCommand())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(append([]string{"skills"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestEmbeddedSkillsOfflineCatalogAndReads(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	// A broken profile and unreachable server must not prevent local guidance.
	configDir := filepath.Join(home, ".multica", "profiles", "broken")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICA_TOKEN", "")
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:1")
	out, err := executeEmbeddedSkills("list", "--output", "json", "--profile", "broken")
	if err != nil {
		t.Fatal(err)
	}
	var catalog []struct {
		Name       string   `json:"name"`
		CLIVersion string   `json:"cli_version"`
		Files      []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 1 || catalog[0].Name != "multica-cli" || catalog[0].CLIVersion != version {
		t.Fatalf("unexpected catalog: %s", out)
	}
	listed := make(map[string]bool)
	for _, file := range catalog[0].Files {
		listed[file] = true
		want, err := embeddedCLISkills.ReadFile("skills/multica-cli/" + file)
		if err != nil {
			t.Fatal(err)
		}
		got, err := executeEmbeddedSkills("read", "multica-cli", file, "--profile", "broken")
		if err != nil || got != string(want) {
			t.Fatalf("read %s: err=%v; output differs from embedded content", file, err)
		}
		encoded, err := executeEmbeddedSkills("read", "multica-cli", file, "--output", "json")
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			Name       string `json:"name"`
			Path       string `json:"path"`
			CLIVersion string `json:"cli_version"`
			Content    string `json:"content"`
		}
		if err := json.Unmarshal([]byte(encoded), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Name != "multica-cli" || envelope.Path != file || envelope.CLIVersion != version || envelope.Content != got {
			t.Fatalf("incorrect JSON envelope for %s", file)
		}
	}
	if err := fs.WalkDir(embeddedCLISkills, "skills/multica-cli", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !listed[strings.TrimPrefix(path, "skills/multica-cli/")] {
			t.Errorf("embedded file missing from catalog: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	main, err := executeEmbeddedSkills("read", "multica-cli")
	want, _ := embeddedCLISkills.ReadFile("skills/multica-cli/SKILL.md")
	if err != nil || main != string(want) {
		t.Fatalf("default read did not return SKILL.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("reading bundled guides created agent installation state: %v", err)
	}
}

func TestEmbeddedSkillsInvalidRequests(t *testing.T) {
	for _, args := range [][]string{
		{"list", "extra"}, {"list", "--output", "xml"}, {"read"},
		{"read", "unknown"}, {"read", "../multica-cli"},
		{"read", "multica-cli", "missing.md"},
		{"read", "multica-cli", "../SKILL.md"},
		{"read", "multica-cli", "/etc/passwd"},
		{"read", "multica-cli", "references/../SKILL.md"},
		{"read", "multica-cli", `references\workspaces.md`},
		{"read", "multica-cli", "SKILL.md", "extra"},
		{"read", "multica-cli", "--output", "xml"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, err := executeEmbeddedSkills(args...)
			if err == nil || out != "" {
				t.Fatalf("want error and no content, got err=%v output=%q", err, out)
			}
		})
	}
}

func TestEmbeddedSkillsSeparateFromWorkspaceSkills(t *testing.T) {
	for _, path := range [][]string{{"skill", "list"}, {"skills", "list"}, {"skills", "read"}} {
		cmd, remaining, err := rootCmd.Find(path)
		if err != nil || len(remaining) != 0 || cmd.CommandPath() != "multica "+strings.Join(path, " ") {
			t.Fatalf("command %v is not registered independently: %v", path, err)
		}
	}
}

func TestEmbeddedSkillExamplesMatchCLI(t *testing.T) {
	placeholder := regexp.MustCompile(`<[^>]+>`)
	err := fs.WalkDir(embeddedCLISkills, "skills", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := embeddedCLISkills.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(content), "\n") {
			if !strings.HasPrefix(line, "multica ") {
				continue
			}
			args, err := shellwords.Parse(placeholder.ReplaceAllString(line, "example"))
			if err != nil {
				t.Errorf("%s:%d: %v", path, i+1, err)
				continue
			}
			cmd, rest, err := rootCmd.Find(args[1:])
			if err != nil || cmd == rootCmd || !cmd.Runnable() {
				t.Errorf("%s:%d: unknown command in %q: %v", path, i+1, line, err)
				continue
			}
			for _, arg := range rest {
				if !strings.HasPrefix(arg, "--") {
					continue
				}
				name := strings.SplitN(strings.TrimPrefix(arg, "--"), "=", 2)[0]
				if cmd.Flags().Lookup(name) == nil && cmd.InheritedFlags().Lookup(name) == nil {
					t.Errorf("%s:%d: %s has no --%s flag", path, i+1, cmd.CommandPath(), name)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
