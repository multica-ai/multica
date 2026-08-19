package slack

import "testing"

func TestParseListsCommand(t *testing.T) {
	cases := []struct {
		in       string
		wantCmd  ListsCommand
		wantRest string
	}{
		{"", ListsCommandNone, ""},
		{"hello", ListsCommandNone, "hello"},
		{"/ideal camera", ListsCommandNone, "/ideal camera"},
		{"/idea", ListsCommandIdea, ""},
		{"  /IDEA  morning light  ", ListsCommandIdea, "morning light"},
		{"/idea\tneed a weekly digest", ListsCommandIdea, "need a weekly digest"},
		{"/feature", ListsCommandFeature, ""},
		{"/Feature Daykee calendar heat-map", ListsCommandFeature, "Daykee calendar heat-map"},
		{"please /idea later", ListsCommandNone, "please /idea later"},
	}
	for _, tc := range cases {
		cmd, rest := ParseListsCommand(tc.in)
		if cmd != tc.wantCmd || rest != tc.wantRest {
			t.Errorf("ParseListsCommand(%q) = (%q, %q), want (%q, %q)", tc.in, cmd, rest, tc.wantCmd, tc.wantRest)
		}
	}
}

func TestListsPolicyCommandMapping(t *testing.T) {
	p := listsPolicyFromEnv(func(string) string { return "" })
	if p.ListIDFor(ListsCommandIdea) != defaultIdeaListID {
		t.Errorf("idea mapping = %q", p.ListIDFor(ListsCommandIdea))
	}
	if p.ListIDFor(ListsCommandFeature) != defaultFeatureListID {
		t.Errorf("feature mapping = %q", p.ListIDFor(ListsCommandFeature))
	}

	p = listsPolicyFromEnv(func(k string) string {
		switch k {
		case "MULTICA_SLACK_LISTS_IDEA_LIST_ID":
			return "FIDEA"
		case "MULTICA_SLACK_LISTS_FEATURE_LIST_ID":
			return "-"
		default:
			return ""
		}
	})
	if p.ListIDFor(ListsCommandIdea) != "FIDEA" {
		t.Fatalf("override idea = %q", p.ListIDFor(ListsCommandIdea))
	}
	if p.ListIDFor(ListsCommandFeature) != "" {
		t.Fatal("disabled feature mapping must be empty")
	}
}

func TestListIDAllowedFailClosed(t *testing.T) {
	allow := []string{"F0BR8PBUAQH", "F0BRAH9R068"}
	if !listIDAllowed(allow, "F0BR8PBUAQH") {
		t.Fatal("exact id must match")
	}
	if listIDAllowed(allow, "f0br8pbuaqh") {
		t.Fatal("allowlist is case-sensitive")
	}
	if listIDAllowed(allow, "F0BR8PBUAQ") {
		t.Fatal("prefix must not match")
	}
	if listIDAllowed(nil, "F0BR8PBUAQH") {
		t.Fatal("empty allowlist is fail-closed")
	}
	if listIDAllowed([]string{"  "}, "F0BR8PBUAQH") {
		t.Fatal("blank entries are not a match")
	}
}
