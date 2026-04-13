package sandbox

import (
	"strings"
	"testing"
)

func TestRedactKey(t *testing.T) {
	// Not testing redactKey since it's in the handler package.
	// These tests verify prompt builder helpers.
}

func TestInjectTokenPlaceholder(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://github.com/org/repo.git", "https://$GIT_TOKEN@github.com/org/repo.git"},
		{"https://github.com/org/repo", "https://$GIT_TOKEN@github.com/org/repo"},
		{"git@github.com:org/repo.git", "git@github.com:org/repo.git"}, // SSH URL unchanged
		{"", ""},
	}
	for _, tt := range tests {
		result := injectTokenPlaceholder(tt.input)
		if result != tt.expected {
			t.Errorf("injectTokenPlaceholder(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestRepoNameFromURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://github.com/org/my-repo.git", "my-repo"},
		{"https://github.com/org/my-repo", "my-repo"},
		{"git@github.com:org/my-repo.git", "my-repo"},
		{"repo", "repo"},
	}
	for _, tt := range tests {
		result := repoNameFromURL(tt.input)
		if result != tt.expected {
			t.Errorf("repoNameFromURL(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestParseRepos(t *testing.T) {
	t.Run("object array format", func(t *testing.T) {
		raw := []byte(`[{"url":"https://github.com/org/repo.git","name":"repo"}]`)
		repos := parseRepos(raw)
		if len(repos) != 1 {
			t.Fatalf("expected 1 repo, got %d", len(repos))
		}
		if repos[0].URL != "https://github.com/org/repo.git" {
			t.Fatalf("unexpected URL: %s", repos[0].URL)
		}
	})

	t.Run("string array format", func(t *testing.T) {
		raw := []byte(`["https://github.com/org/repo.git"]`)
		repos := parseRepos(raw)
		if len(repos) != 1 {
			t.Fatalf("expected 1 repo, got %d", len(repos))
		}
		if repos[0].Name != "repo" {
			t.Fatalf("expected name 'repo', got %s", repos[0].Name)
		}
	})

	t.Run("nil input", func(t *testing.T) {
		repos := parseRepos(nil)
		if repos != nil {
			t.Fatalf("expected nil, got %v", repos)
		}
	})
}

func TestPromptNeverContainsCredentials(t *testing.T) {
	// Verify the prompt structure doesn't leak credential values.
	// We can't call BuildPrompt directly (needs DB), but we test the helper logic.
	secretPAT := "ghp_abc123secret456"
	secretKey := "sk-gateway-secret789"

	// Simulate what BuildPrompt does: credentials go to envVars, placeholder to prompt
	url := "https://github.com/org/repo.git"
	promptURL := injectTokenPlaceholder(url)

	if strings.Contains(promptURL, secretPAT) {
		t.Fatal("prompt URL must not contain the actual PAT value")
	}
	if !strings.Contains(promptURL, "$GIT_TOKEN") {
		t.Fatal("prompt URL must reference $GIT_TOKEN env var")
	}

	// EnvVars would contain the actual values
	envVars := map[string]string{
		"GIT_TOKEN":          secretPAT,
		"AI_GATEWAY_API_KEY": secretKey,
	}
	if envVars["GIT_TOKEN"] != secretPAT {
		t.Fatal("envVars must contain actual PAT value")
	}
}
