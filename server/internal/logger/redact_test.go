package logger

import "testing"

func TestRedactValueRedactsSensitiveKeys(t *testing.T) {
	input := map[string]any{
		"email": "person@example.com",
		"name":  "Ada",
		"nested": map[string]any{
			"access_token": "mul_secret",
		},
	}

	output := RedactValue(input).(map[string]any)
	if output["email"] != "[REDACTED]" {
		t.Fatalf("expected email to be redacted, got %#v", output["email"])
	}
	if output["name"] != "Ada" {
		t.Fatalf("expected non-sensitive field to remain, got %#v", output["name"])
	}
	nested := output["nested"].(map[string]any)
	if nested["access_token"] != "[REDACTED]" {
		t.Fatalf("expected nested token to be redacted, got %#v", nested["access_token"])
	}
}

func TestRedactStringRedactsBearerTokens(t *testing.T) {
	if got := RedactString("Bearer abc"); got != "[REDACTED]" {
		t.Fatalf("expected bearer token to be redacted, got %q", got)
	}
}
