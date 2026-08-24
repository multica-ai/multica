package issueguard

import "testing"

func TestNormalizeTitleStripsGeneratedTimestampSuffix(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "hyphen date minute",
			title: "LAS Jira manual issue scan - 2026-08-18 00:50",
			want:  "las jira manual issue scan",
		},
		{
			name:  "slash date seconds",
			title: "LAS Jira manual issue scan | 2026/08/18 00:50:10",
			want:  "las jira manual issue scan",
		},
		{
			name:  "plain date is preserved",
			title: "LAS Jira manual issue scan - 2026-08-18",
			want:  "las jira manual issue scan - 2026-08-18",
		},
		{
			name:  "internal timestamp is preserved",
			title: "Review 2026-08-18 00:50 scan output",
			want:  "review 2026-08-18 00:50 scan output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeTitle(tt.title); got != tt.want {
				t.Fatalf("NormalizeTitle(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}
