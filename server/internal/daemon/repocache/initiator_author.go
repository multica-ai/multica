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
			return []byte(strings.ReplaceAll(string(contents), initiatorAuthorInclude, "")), nil
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
		original := strings.ReplaceAll(string(contents), initiatorAuthorInclude, "")
		return []byte(original + initiatorAuthorInclude), nil
	})
}
