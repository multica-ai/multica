package github

import "testing"

func TestExtractIdentifier(t *testing.T) {
	cases := []struct {
		name     string
		headRef  string
		body     string
		title    string
		want     Identifier
		wantOK   bool
	}{
		{
			name:    "branch with identifier wins",
			headRef: "agent/bmad-agent-dev/TIM-42-9adf130d",
			body:    "Implements MUL-99",
			title:   "WIJ-7: do thing",
			want:    Identifier{Prefix: "TIM", Number: 42},
			wantOK:  true,
		},
		{
			name:    "legacy branch falls through to body",
			headRef: "agent/bmad-agent-dev/9adf130d",
			body:    "Implements TIM-9 (Story 1.4)",
			title:   "feat: do thing",
			want:    Identifier{Prefix: "TIM", Number: 9},
			wantOK:  true,
		},
		{
			name:    "legacy branch + empty body falls to title",
			headRef: "agent/dev/653ba68f",
			body:    "",
			title:   "TIM-9: read endpoints",
			want:    Identifier{Prefix: "TIM", Number: 9},
			wantOK:  true,
		},
		{
			name:    "no identifier anywhere returns false",
			headRef: "agent/dev/653ba68f",
			body:    "Closes #",
			title:   "feat: do thing",
			wantOK:  false,
		},
		{
			name:    "lowercase prefix is not an identifier",
			headRef: "feature/tim-42-foo",
			body:    "",
			title:   "",
			wantOK:  false,
		},
		{
			name:    "underscore separator does not match",
			headRef: "feature/TIM_42",
			body:    "",
			title:   "",
			wantOK:  false,
		},
		{
			name:    "long prefix MUL-1128 matches",
			headRef: "agent/dev/MUL-1128-fix",
			body:    "",
			title:   "",
			want:    Identifier{Prefix: "MUL", Number: 1128},
			wantOK:  true,
		},
		{
			name:    "trailing letters break word boundary",
			headRef: "TIM-42abc",
			body:    "",
			title:   "",
			wantOK:  false,
		},
		{
			name:    "first identifier in body wins over later ones",
			headRef: "",
			body:    "Implements TIM-1 (depends on TIM-2)",
			title:   "",
			want:    Identifier{Prefix: "TIM", Number: 1},
			wantOK:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExtractIdentifier(tc.headRef, tc.body, tc.title)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v; want %v (got=%+v)", ok, tc.wantOK, got)
			}
			if !ok {
				return
			}
			if got != tc.want {
				t.Fatalf("got %+v; want %+v", got, tc.want)
			}
		})
	}
}
