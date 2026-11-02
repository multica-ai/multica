package channel

import "testing"

func TestParseChatCommandMatchesWholeLinesOnly(t *testing.T) {
	cases := map[string]string{
		"/brief":                    CommandBrief,
		"/decisions":                CommandDecisions,
		"\n  /brief  \n":            CommandBrief,
		"\n\n/decisions\nmore text": CommandDecisions,
		"/briefing":                 "",
		"/brief me on the pilot":    "",
		"Please /brief me":          "",
		"/Brief":                    "",
		"":                          "",
		"first line\n/brief":        "",
	}
	for text, want := range cases {
		got, ok := ParseChatCommand(text)
		if want == "" && ok {
			t.Errorf("ParseChatCommand(%q) = %q, want no command", text, got)
		}
		if want != "" && (!ok || got != want) {
			t.Errorf("ParseChatCommand(%q) = %q, %v; want %q", text, got, ok, want)
		}
	}
}
