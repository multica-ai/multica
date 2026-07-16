package redact

import (
	"fmt"
	"strings"
	"testing"
)

func TestTextRedactsOAuthRefreshCredentials(t *testing.T) {
	t.Parallel()

	const synthetic = "synthetic-pi-refresh-value-for-regression-only"
	tests := map[string]string{
		"json snake case": `{"refresh_token":"` + synthetic + `","expires":4102444800000}`,
		"json camel case": `{"refreshToken":"` + synthetic + `","access":"safe-access-placeholder"}`,
		"pi auth field":   `{"openai-codex":{"type":"oauth","refresh":"` + synthetic + `"}}`,
		"log equals":      `refresh_token=` + synthetic,
		"log colon":       `refreshToken: ` + synthetic,
	}

	for name, input := range tests {
		name, input := name, input
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := Text(input)
			if strings.Contains(got, synthetic) {
				t.Fatalf("synthetic refresh credential was not redacted: %s", got)
			}
			if !strings.Contains(got, "[REDACTED OAUTH REFRESH]") {
				t.Fatalf("OAuth refresh placeholder missing: %s", got)
			}
		})
	}
}

func TestTextKeepsSafeProviderDiagnostics(t *testing.T) {
	t.Parallel()

	input := "openai-codex authentication rejected with status 401"
	if got := Text(input); got != input {
		t.Fatalf("safe diagnostic changed: %q", got)
	}
}

func TestInputMapRedactsNestedOAuthRefreshCredentials(t *testing.T) {
	t.Parallel()

	const synthetic = "synthetic-nested-refresh-value"
	got := InputMap(map[string]any{
		"provider": map[string]any{
			"oauth": map[string]any{"refreshToken": synthetic},
		},
		"events": []any{map[string]any{"refresh_token": synthetic}},
	})

	encoded := fmt.Sprintf("%#v", got)
	if strings.Contains(encoded, synthetic) {
		t.Fatalf("nested synthetic refresh credential was not redacted: %s", encoded)
	}
}
