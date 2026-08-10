package agent

import "testing"

// CEREBRO-PATCH(agent-model-runnable-custom-prefix).
func TestModelCatalogIDMatchCustomPrefix(t *testing.T) {
	cases := []struct {
		catalog, requested string
		want               bool
	}{
		{"custom:firtal-gateway:deepseek/deepseek-v4-flash-0731", "custom:firtal-gateway:deepseek/deepseek-v4-flash-0731", true},
		{"firtal-gateway:deepseek/deepseek-v4-flash-0731", "custom:firtal-gateway:deepseek/deepseek-v4-flash-0731", true},
		{"deepseek/deepseek-v4-flash-0731", "custom:firtal-gateway:deepseek/deepseek-v4-flash-0731", true},
		{"custom:firtal-gateway:moonshotai/kimi-k3", "custom:firtal-gateway:moonshotai/kimi-k3", true},
		{"moonshotai/kimi-k3", "custom:firtal-gateway:moonshotai/kimi-k3", true},
		{"deepseek/deepseek-v4-flash", "custom:firtal-gateway:deepseek/deepseek-v4-flash-0731", false},
		{"", "custom:firtal-gateway:x", false},
	}
	for _, tc := range cases {
		if got := modelCatalogIDMatch(tc.catalog, tc.requested); got != tc.want {
			t.Fatalf("modelCatalogIDMatch(%q, %q)=%v want %v", tc.catalog, tc.requested, got, tc.want)
		}
	}
}

func TestModelSlug(t *testing.T) {
	if got := modelSlug("custom:firtal-gateway:deepseek/deepseek-v4-flash-0731"); got != "deepseek/deepseek-v4-flash-0731" {
		t.Fatalf("slug custom triple: %q", got)
	}
	if got := modelSlug("opencode-go:kimi-k3"); got != "kimi-k3" {
		t.Fatalf("slug provider:model: %q", got)
	}
	if got := modelSlug("deepseek/deepseek-v4-flash-0731"); got != "deepseek/deepseek-v4-flash-0731" {
		t.Fatalf("slug bare org/model: %q", got)
	}
}
