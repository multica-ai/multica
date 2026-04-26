package memoryguard

import "testing"

func TestInspectAllowsNormalMemory(t *testing.T) {
	t.Parallel()

	report := Inspect("Project style", "Use shadcn components for dashboard UI.")
	if !report.Allowed {
		t.Fatalf("expected normal memory to pass, got findings: %#v", report.Findings)
	}
}

func TestInspectBlocksSecrets(t *testing.T) {
	t.Parallel()

	report := Inspect("token", "OPENAI_API_KEY=sk-proj-abc123def456ghi789jkl012mno345")
	if report.Allowed {
		t.Fatal("expected secret content to be blocked")
	}
	if report.Findings[0].Type != "secret_or_local_path" {
		t.Fatalf("unexpected finding: %#v", report.Findings)
	}
}

func TestInspectBlocksPII(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		typ  string
	}{
		{name: "email", text: "Contact person is user@example.com", typ: "email"},
		{name: "phone", text: "Phone: +90 532 123 45 67", typ: "phone"},
		{name: "ssn", text: "SSN 123-45-6789", typ: "ssn"},
		{name: "tckn", text: "TCKN 10000000146", typ: "tckn"},
		{name: "iban", text: "TR330006100519786457841326", typ: "iban"},
		{name: "payment_card", text: "Card 4111 1111 1111 1111", typ: "payment_card"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := Inspect(tt.text)
			if report.Allowed {
				t.Fatalf("expected %s to be blocked", tt.typ)
			}
			found := false
			for _, finding := range report.Findings {
				if finding.Type == tt.typ {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s finding: %#v", tt.typ, report.Findings)
			}
		})
	}
}
