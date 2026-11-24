package repocache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const initiatorAuthorInclude = "\n# Multica run initiator author.\n[include]\n\tpath = multica-initiator-author.config\n"

func validInitiatorAuthor(params WorktreeParams) (string, string) {
	name := strings.TrimSpace(params.InitiatorName)
	email := strings.TrimSpace(params.InitiatorEmail)
	if !params.CommitAuthorIsInitiatorEnabled || params.InitiatorType != "member" || name == "" || !strings.Contains(email, "@") {
		return "", ""
	}
	if strings.ContainsAny(name+email, "<>\"\\&") || strings.IndexFunc(params.InitiatorName+params.InitiatorEmail, unicode.IsControl) >= 0 {
		return "", ""
	}
	return name, email
}

// Use checkout-private config, never --local on a linked worktree (which writes
// to the shared cache). A managed include preserves pre-existing author values
// so disabling the setting removes only the override installed by this feature.
func applyInitiatorAuthorContext(ctx context.Context, checkoutPath string, params WorktreeParams) error {
	out, err := runGitOutputContext(ctx, "-C", checkoutPath, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	gitDir := strings.TrimSpace(string(out))
	configName := "config"
	if isGitWorktree(checkoutPath) {
		configName = "config.worktree"
	}
	configPath := filepath.Join(gitDir, configName)
	authorPath := filepath.Join(gitDir, "multica-initiator-author.config")
	name, email := validInitiatorAuthor(params)
	if name == "" {
		contents, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}
		if !strings.Contains(string(contents), initiatorAuthorInclude) {
			return nil
		}
		if err := editGitConfigFile(configPath, func(contents []byte, _ string) ([]byte, error) {
			return []byte(removeInitiatorAuthorInclude(string(contents))), nil
		}); err != nil {
			return err
		}
		if err := os.Remove(authorPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	contents := fmt.Sprintf("[author]\n\tname = %s\n\temail = %s\n", quoteGitConfig(name), quoteGitConfig(email))
	if err := editGitConfigFile(authorPath, func(_ []byte, _ string) ([]byte, error) {
		return []byte(contents), nil
	}); err != nil {
		return err
	}
	return editGitConfigFile(configPath, func(contents []byte, _ string) ([]byte, error) {
		original := removeInitiatorAuthorInclude(string(contents))
		return []byte(original + initiatorAuthorInclude), nil
	})
}

// removeInitiatorAuthorInclude drops the managed include entry. Git appends a
// later `include.path` to the last [include] section, which may be ours, so the
// section header stays whenever other entries follow the managed one.
func removeInitiatorAuthorInclude(contents string) string {
	for {
		i := strings.Index(contents, initiatorAuthorInclude)
		if i < 0 {
			return contents
		}
		rest := contents[i+len(initiatorAuthorInclude):]
		replacement := ""
		if sectionHasEntries(rest) {
			replacement = "\n[include]\n"
		}
		contents = contents[:i] + replacement + rest
	}
}

// sectionHasEntries reports whether text before the next section header holds
// any key, ignoring blank lines and comments.
func sectionHasEntries(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			return false
		}
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, ";") {
			return true
		}
	}
	return false
}
