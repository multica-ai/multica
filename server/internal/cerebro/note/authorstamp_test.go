package note

import "testing"

func TestBodyOnlyAddsAuthorStamps(t *testing.T) {
	cases := []struct {
		name     string
		server   string
		incoming string
		want     bool
	}{
		{
			name:     "single stamp added on a plain line",
			server:   "hej med dig",
			incoming: "**JEH** hej med dig",
			want:     true,
		},
		{
			name:     "stamp added after a bullet marker",
			server:   "- punkt et\n- punkt to",
			incoming: "- **JEH** punkt et\n- punkt to",
			want:     true,
		},
		{
			name:     "stamp added after an ordered-list marker",
			server:   "1. punkt et",
			incoming: "1. **MOP** punkt et",
			want:     true,
		},
		{
			name:     "stamps added on several lines",
			server:   "linje et\nlinje to\nlinje tre",
			incoming: "**JEH** linje et\nlinje to\n**JEH** linje tre",
			want:     true,
		},
		{
			name:     "identical bodies are not stamp-only (no-op path)",
			server:   "hej",
			incoming: "hej",
			want:     false,
		},
		{
			name:     "real text change still conflicts",
			server:   "hej med dig",
			incoming: "hej med jer",
			want:     false,
		},
		{
			name:     "stamp plus a text change still conflicts",
			server:   "hej med dig",
			incoming: "**JEH** hej med jer",
			want:     false,
		},
		{
			name:     "added line still conflicts",
			server:   "linje et",
			incoming: "linje et\nlinje to",
			want:     false,
		},
		{
			name:     "removed stamp still conflicts",
			server:   "**JEH** hej med dig",
			incoming: "hej med dig",
			want:     false,
		},
		{
			name:     "unbolded token is not a stamp",
			server:   "hej med dig",
			incoming: "JEH hej med dig",
			want:     false,
		},
		{
			name:     "long bold word is not a stamp",
			server:   "vigtig ting",
			incoming: "**VIGTIGT** vigtig ting",
			want:     false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bodyOnlyAddsAuthorStamps(tc.server, tc.incoming); got != tc.want {
				t.Fatalf("bodyOnlyAddsAuthorStamps(%q, %q) = %v, want %v",
					tc.server, tc.incoming, got, tc.want)
			}
		})
	}
}
