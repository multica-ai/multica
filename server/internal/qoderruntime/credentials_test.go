package qoderruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryCredentialRequestOnly(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "token")
	if err := os.WriteFile(file, []byte("test-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	b := &Bridge{cfg: Config{GitHubTokenFiles: map[string]string{"https://github.com/Owner/Repo.git": file}}}
	r := &run{Resources: []map[string]any{{"type": "github_repository", "url": "https://github.com/owner/repo", "checkout": map[string]string{"type": "branch", "name": "main"}}}}
	request, _, rotating, err := b.sessionResources(context.Background(), r.Resources)
	if err != nil {
		t.Fatal(err)
	}
	if request[0]["authorization_token"] != "test-secret" {
		t.Fatal("missing request credential")
	}
	if rotating {
		t.Fatal("manual credential marked rotating")
	}
	state := filepath.Join(dir, "state.json")
	if err := save(state, &journal{Runs: []*run{r}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "test-secret") || strings.Contains(string(data), "authorization_token") {
		t.Fatal("credential persisted")
	}
	if err := os.WriteFile(file, []byte("rotated-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	request, _, _, err = b.sessionResources(context.Background(), r.Resources)
	if err != nil || request[0]["authorization_token"] != "rotated-secret" {
		t.Fatal("credential rotation not picked up")
	}
	r.Resources[0]["url"] = "https://github.com/owner/another"
	if _, _, _, err := b.sessionResources(context.Background(), r.Resources); err == nil {
		t.Fatal("credential crossed repository boundary")
	}
}

// A GitHub App credential backs the same boundary guarantees: single
// repository scope, a stable fingerprint across token rotation, and a hard
// block once the workspace installation no longer covers the repository.
func TestGitHubAppCredentialRotationAndFingerprint(t *testing.T) {
	resource := func(url string) map[string]any {
		return map[string]any{"type": "github_repository", "url": url, "checkout": map[string]string{"type": "branch", "name": "main"}}
	}
	var issued []string
	var requested [][2]string
	b := &Bridge{cfg: Config{GitHubAppToken: func(ctx context.Context, owner, repo string) (string, int64, error) {
		requested = append(requested, [2]string{owner, repo})
		issued = append(issued, fmt.Sprintf("ghs_%d", len(issued)))
		return issued[len(issued)-1], 424242, nil
	}}}
	resources := []map[string]any{resource("https://github.com/acme/app")}
	first, fingerprint, rotating, err := b.sessionResources(context.Background(), resources)
	if err != nil {
		t.Fatal(err)
	}
	if !rotating || first[0]["authorization_token"] != "ghs_0" {
		t.Fatalf("app credential not issued: rotating=%v", rotating)
	}
	if len(requested) != 1 || requested[0] != [2]string{"acme", "app"} {
		t.Fatalf("issuer scoped to wrong repository: %v", requested)
	}
	second, repeat, _, err := b.sessionResources(context.Background(), resources)
	if err != nil {
		t.Fatal(err)
	}
	if second[0]["authorization_token"] != "ghs_1" {
		t.Fatal("short-lived token not rotated between resolutions")
	}
	if repeat != fingerprint {
		t.Fatal("rotating credential changed the session fingerprint")
	}
	// A manual token outranks the App integration and restores the static
	// fingerprint behaviour.
	manual := &Bridge{cfg: Config{GitHubTokens: map[string]string{"https://github.com/acme/app": "pat"}, GitHubAppToken: func(context.Context, string, string) (string, int64, error) {
		t.Fatal("manual token must outrank the GitHub App")
		return "", 0, nil
	}}}
	request, manualPrint, rotating, err := manual.sessionResources(context.Background(), resources)
	if err != nil || request[0]["authorization_token"] != "pat" || rotating {
		t.Fatalf("manual priority broken: %v %v", request, err)
	}
	if manualPrint == fingerprint {
		t.Fatal("manual credential shares the app credential fingerprint")
	}
	// Losing coverage — an unbound installation — must block the run.
	revoked := &Bridge{cfg: Config{GitHubAppToken: func(context.Context, string, string) (string, int64, error) {
		return "", 0, ErrGitHubAppNotCovered
	}}}
	if _, _, _, err := revoked.sessionResources(context.Background(), resources); err == nil || err.Error() != "no GitHub credential configured for project repository" {
		t.Fatalf("revoked installation not blocked: %v", err)
	}
}

func TestGitHubAppCredentialIssuerScope(t *testing.T) {
	var requested [][2]string
	b := &Bridge{cfg: Config{GitHubAppToken: func(ctx context.Context, owner, repo string) (string, int64, error) {
		requested = append(requested, [2]string{owner, repo})
		return "ghs_x", 7, nil
	}}}
	if _, _, _, err := b.sessionResources(context.Background(), []map[string]any{{"url": "https://github.com/Acme/App"}}); err != nil {
		t.Fatal(err)
	}
	if len(requested) != 1 || requested[0] != [2]string{"acme", "app"} {
		t.Fatalf("issuer not scoped to the target repository: %v", requested)
	}
}

// An App that is installed without the repository write permissions surfaces
// the operator-actionable message, not the generic "no credential" one.
func TestGitHubAppPermissionErrorIsActionable(t *testing.T) {
	b := &Bridge{cfg: Config{GitHubAppToken: func(context.Context, string, string) (string, int64, error) {
		return "", 0, fmt.Errorf("issue token: %w", ErrGitHubAppPermissions)
	}}}
	_, _, _, err := b.sessionResources(context.Background(), []map[string]any{{"url": "https://github.com/acme/app"}})
	if !errors.Is(err, ErrGitHubAppPermissions) {
		t.Fatalf("sentinel lost: %v", err)
	}
	if !strings.Contains(err.Error(), "approve Contents and Pull requests write access") || strings.Contains(err.Error(), "no GitHub credential configured") {
		t.Fatalf("not actionable or misclassified: %v", err)
	}
}

func TestRepositoryCredentialValidation(t *testing.T) {
	for _, raw := range []string{"https://github.com/owner", "https://github.com:443/a/b", "https://github.com/a/b?token=x", "https://user@github.com/a/b", "https://github.com/a/b/extra", "https://github.com/a/%62", "https://github.com/a/.."} {
		if _, err := repositoryKey(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	file := filepath.Join(t.TempDir(), "token")
	for _, secret := range []string{"", "first\nsecond", strings.Repeat("x", 16385)} {
		if err := os.WriteFile(file, []byte(secret), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readRepositoryToken(file); err == nil {
			t.Fatal("accepted invalid credential")
		}
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readRepositoryToken(file); err == nil {
		t.Fatal("accepted public file")
	}
}
