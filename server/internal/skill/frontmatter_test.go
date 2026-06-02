package skill

import "testing"

func TestParseSkillFrontmatter(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantName string
		wantDesc string
	}{
		{
			name:     "single line",
			content:  "---\nname: foo\ndescription: bar\n---\nbody",
			wantName: "foo",
			wantDesc: "bar",
		},
		{
			name:     "double quoted",
			content:  "---\nname: \"foo\"\ndescription: \"hello world\"\n---\nbody",
			wantName: "foo",
			wantDesc: "hello world",
		},
		{
			name:     "single quoted",
			content:  "---\nname: 'foo'\ndescription: 'hello world'\n---\nbody",
			wantName: "foo",
			wantDesc: "hello world",
		},
		{
			name:     "literal block scalar keeps newlines",
			content:  "---\nname: foo\ndescription: |\n  line1\n  line2\n---\nbody",
			wantName: "foo",
			wantDesc: "line1\nline2\n",
		},
		{
			name:     "literal strip chomping drops trailing newline",
			content:  "---\nname: foo\ndescription: |-\n  line1\n  line2\n---\nbody",
			wantName: "foo",
			wantDesc: "line1\nline2",
		},
		{
			name:     "folded block scalar joins with spaces",
			content:  "---\nname: foo\ndescription: >\n  line1\n  line2\n---\nbody",
			wantName: "foo",
			wantDesc: "line1 line2\n",
		},
		{
			name:     "CRLF line endings",
			content:  "---\r\nname: foo\r\ndescription: bar\r\n---\r\nbody",
			wantName: "foo",
			wantDesc: "bar",
		},
		{
			name:     "no frontmatter returns empty",
			content:  "no frontmatter here",
			wantName: "",
			wantDesc: "",
		},
		{
			name:     "unterminated frontmatter returns empty",
			content:  "---\nname: foo\ndescription: bar\n",
			wantName: "",
			wantDesc: "",
		},
		{
			name:     "invalid YAML falls back to empty",
			content:  "---\n: : : not valid\n---\nbody",
			wantName: "",
			wantDesc: "",
		},
		{
			name:     "name only",
			content:  "---\nname: foo\n---\nbody",
			wantName: "foo",
			wantDesc: "",
		},
		{
			name:     "description only",
			content:  "---\ndescription: bar\n---\nbody",
			wantName: "",
			wantDesc: "bar",
		},
		{
			name:     "leading blank line is not frontmatter",
			content:  "\n---\nname: foo\ndescription: bar\n---\nbody",
			wantName: "",
			wantDesc: "",
		},
		{
			// The non-greedy capture stops at the first closing fence, so a
			// later "---" in the body must not extend the frontmatter block.
			name:     "triple dash in body stops at first fence",
			content:  "---\nname: foo\ndescription: bar\n---\nintro\n---\nmore",
			wantName: "foo",
			wantDesc: "bar",
		},
		{
			// Parity with the TS coercion: non-string scalars render as their
			// literal form rather than being dropped.
			name:     "non-string scalars coerce to literal",
			content:  "---\nname: 123\ndescription: 456\n---\nbody",
			wantName: "123",
			wantDesc: "456",
		},
		{
			// A structured value where a scalar belongs (authoring mistake) must
			// not discard the sibling name; the value is JSON-encoded, matching
			// the TS parseFrontmatter behaviour.
			name:     "sequence description keeps name and is JSON-encoded",
			content:  "---\nname: my-skill\ndescription:\n  - first feature\n  - second feature\n---\nbody",
			wantName: "my-skill",
			wantDesc: `["first feature","second feature"]`,
		},
		{
			name:     "mapping description keeps name and is JSON-encoded",
			content:  "---\nname: my-skill\ndescription:\n  a: 1\n  b: 2\n---\nbody",
			wantName: "my-skill",
			wantDesc: `{"a":1,"b":2}`,
		},
		{
			// Reproduction for issue #3495: Chinese block scalar.
			name: "issue 3495 chinese literal block scalar",
			content: "---\n" +
				"name: requirements-workshop\n" +
				"description: |\n" +
				"  当用户想要开发新功能、讨论需求、梳理业务逻辑时触发。通过多轮对话将模糊的想法转化为结构化的需求文档，为后续技术方案设计提供输入。\n" +
				"  适用场景：用户说\"我想实现XX功能\"、\"讨论一下这个需求\"、\"帮我分析一下怎么做\"、\"这个需求该怎么设计\"、\"写个需求文档\"等。\n" +
				"  本skill只负责产出需求文档，不进入技术设计或代码实现阶段。\n" +
				"---\nbody",
			wantName: "requirements-workshop",
			wantDesc: "当用户想要开发新功能、讨论需求、梳理业务逻辑时触发。通过多轮对话将模糊的想法转化为结构化的需求文档，为后续技术方案设计提供输入。\n" +
				"适用场景：用户说\"我想实现XX功能\"、\"讨论一下这个需求\"、\"帮我分析一下怎么做\"、\"这个需求该怎么设计\"、\"写个需求文档\"等。\n" +
				"本skill只负责产出需求文档，不进入技术设计或代码实现阶段。\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotDesc := ParseSkillFrontmatter(tt.content)
			if gotName != tt.wantName {
				t.Errorf("name: got %q, want %q", gotName, tt.wantName)
			}
			if gotDesc != tt.wantDesc {
				t.Errorf("description: got %q, want %q", gotDesc, tt.wantDesc)
			}
		})
	}
}
