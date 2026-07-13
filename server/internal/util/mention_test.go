package util

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestParseMentions(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []Mention
	}{
		{
			name:    "simple agent mention",
			content: "[@Agent](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) please fix",
			want:    []Mention{{Type: "agent", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "agent name with square brackets",
			content: "[@David[TF]](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) please fix",
			want:    []Mention{{Type: "agent", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "agent name with nested brackets",
			content: "[@Bot[v2][beta]](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) help",
			want:    []Mention{{Type: "agent", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "multiple mentions with brackets",
			content: "[@A[1]](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) and [@B[2]](mention://agent/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb)",
			want: []Mention{
				{Type: "agent", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"},
				{Type: "agent", ID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"},
			},
		},
		{
			name:    "issue mention without @",
			content: "[MUL-123](mention://issue/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) is related",
			want:    []Mention{{Type: "issue", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "member mention",
			content: "[@Bob](mention://member/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) look",
			want:    []Mention{{Type: "member", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "all mention",
			content: "[@All](mention://all/all) heads up",
			want:    []Mention{{Type: "all", ID: "all"}},
		},
		{
			name:    "deduplicate same mention",
			content: "[@A](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) and again [@A](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa)",
			want:    []Mention{{Type: "agent", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "no mentions",
			content: "just a plain comment",
			want:    nil,
		},
		{
			name:    "escaped brackets in label",
			content: `[@David\[TF\]](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) hi`,
			want:    []Mention{{Type: "agent", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "skill mention",
			content: "[@SkillName](mention://skill/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) triggered",
			want:    []Mention{{Type: "skill", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "project mention without @",
			content: "[MyProject](mention://project/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb) linked",
			want:    []Mention{{Type: "project", ID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"}},
		},
		{
			name:    "squad mention",
			content: "[@DevTeam](mention://squad/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) review",
			want:    []Mention{{Type: "squad", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "deduplicate skill mention",
			content: "[@Skill](mention://skill/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) and [@Skill](mention://skill/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa)",
			want:    []Mention{{Type: "skill", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}},
		},
		{
			name:    "mixed skill and agent mentions",
			content: "[@Skill](mention://skill/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa) then [@Agent](mention://agent/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb)",
			want: []Mention{
				{Type: "skill", ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"},
				{Type: "agent", ID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseMentions(tt.content)
			if len(got) != len(tt.want) {
				t.Fatalf("ParseMentions() returned %d mentions, want %d\ngot:  %+v\nwant: %+v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i].Type != tt.want[i].Type || got[i].ID != tt.want[i].ID {
					t.Errorf("mention[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestHasMentionAll(t *testing.T) {
	tests := []struct {
		name     string
		mentions []Mention
		want     bool
	}{
		{"empty", nil, false},
		{"no all", []Mention{{Type: "agent", ID: "x"}}, false},
		{"has all", []Mention{{Type: "all", ID: "all"}}, true},
		{"mixed", []Mention{{Type: "agent", ID: "x"}, {Type: "all", ID: "all"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasMentionAll(tt.mentions); got != tt.want {
				t.Errorf("HasMentionAll() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestValidMentionTypesMatchesFixture reads the JSON fixture and verifies it
// equals ValidMentionTypes. This keeps the Go constant list and the fixture
// (consumed by the TS-side sync test) in lockstep.
func TestValidMentionTypesMatchesFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/valid-mention-types.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var fromFile []string
	if err := json.Unmarshal(data, &fromFile); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	if !reflect.DeepEqual(fromFile, ValidMentionTypes) {
		t.Errorf("fixture mismatch\nfile:    %v\nconstant: %v", fromFile, ValidMentionTypes)
	}
}

// TestMentionReCaptureGroups verifies that MentionRe has exactly 3 capture
// groups and that each group extracts the expected part of a mention string.
func TestMentionReCaptureGroups(t *testing.T) {
	input := `[@SkillName](mention://skill/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa)`
	m := MentionRe.FindStringSubmatch(input)
	if m == nil {
		t.Fatal("MentionRe did not match a valid skill mention")
	}
	// m[0] = full match, m[1] = label, m[2] = type, m[3] = id
	if len(m) != 4 {
		t.Fatalf("expected 4 submatch groups (full + 3 captures), got %d", len(m))
	}
	if m[1] != "SkillName" {
		t.Errorf("group 1 (label) = %q, want %q", m[1], "SkillName")
	}
	if m[2] != "skill" {
		t.Errorf("group 2 (type) = %q, want %q", m[2], "skill")
	}
	if m[3] != "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" {
		t.Errorf("group 3 (id) = %q, want %q", m[3], "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	}
}

// TestMentionReMatchesAllTypes checks that MentionRe matches a representative
// string for every type in ValidMentionTypes.
func TestMentionReMatchesAllTypes(t *testing.T) {
	uuid := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	for _, typ := range ValidMentionTypes {
		id := uuid
		if typ == "all" {
			id = "all"
		}
		input := "[@Label](mention://" + typ + "/" + id + ")"
		if !MentionRe.MatchString(input) {
			t.Errorf("MentionRe failed to match type %q with input %s", typ, input)
		}
	}
}
