package engineering

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNaturalSections(t *testing.T) {
	content := "导言\r\n\r\n\r\n# 总览\n总览正文\n### 起步\n开始\n## 风险   与\t回滚 ###\n父正文\n### 回滚\n子正文\n### 回滚\n再次\n## [草案] 标题\n正文\n### 子标题\n最后"
	got, err := Parse("overview", content)
	want := []Section{
		{"_preamble", "", "导言"}, {"总览", "总览", "总览正文"}, {"总览 / 起步", "起步", "开始"},
		{"风险 与 回滚", "风险 与 回滚", "父正文"}, {"风险 与 回滚 / 回滚", "回滚", "子正文"},
		{"风险 与 回滚 / 回滚 (2)", "回滚", "再次"}, {"[草案] 标题", "[草案] 标题", "正文"}, {"[草案] 标题 / 子标题", "子标题", "最后"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("sections=%+v err=%v", got, err)
	}
}

func TestExplicitKeysInEveryChapterAndLevel(t *testing.T) {
	for _, chapter := range Chapters {
		for _, heading := range []string{"#", "##", "###"} {
			for _, key := range []string{"X-1", "Rule-中文", "决策-7", "自定义_2-序号"} {
				t.Run(chapter+heading+key, func(t *testing.T) {
					before := heading + " [" + key + "] Old\nBody"
					after := "## New parent\n" + heading + " [" + key + "] New\nBody"
					d, err := Compute(chapter, before, after, 4)
					if err != nil || len(d.Modified) != 1 || d.Modified[0].Anchor != key || d.Modified[0].Before != "Old\nBody" || d.Modified[0].After != "New\nBody" || d.BaseRevisionNumber != 4 {
						t.Fatalf("diff=%+v err=%v", d, err)
					}
				})
			}
		}
	}
}

func TestDuplicateKeysRemainUnique(t *testing.T) {
	got, err := Parse("overview", "## Same\n### Child\n## Same\n### Child\n# Same (2)\n# [X-1] One\n# [X-1] Two\n# _preamble")
	var keys []string
	for _, s := range got {
		keys = append(keys, s.Anchor)
	}
	want := []string{"Same", "Same / Child", "Same (2)", "Same (2) / Child", "Same (2) (2)", "X-1", "X-1 (2)", "_preamble"}
	if err != nil || !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys=%v err=%v", keys, err)
	}
}

func TestFencesAndNonSectionHeadings(t *testing.T) {
	body := "Body\n#### Detail\n#\n##\nSetext\n===\n---\n    ## Indented\n```md\n## Example\n~~~\n### Still fenced\n```\n~~~~\n# Another example\n~~~\n# Still fenced too\n~~~~"
	got, err := Parse("architecture", "   ## Real\n"+body+"\n## Next\nEnd")
	if err != nil || len(got) != 2 || got[0].Body != body || got[1].Anchor != "Next" {
		t.Fatalf("sections=%+v err=%v", got, err)
	}
}

func TestDiffChangesWithoutWarnings(t *testing.T) {
	before := "前言\n### [Rule-1] Keep\n一二三四五六七八九十\n### [Rule-2] Remove\nRequired\n## Old name\nBody"
	after := "前言\n### [Rule-1] Keep\n一二三\n## New name\nBody\n### [Defer-1] Later\nRule-2 deferred without issue"
	d, err := Compute("domain_rules", before, after, 4)
	if err != nil || len(d.Modified) != 1 || len(d.Removed) != 2 || len(d.Added) != 2 || d.UnchangedCount != 1 || d.Warnings == nil || len(d.Warnings) != 0 {
		t.Fatalf("diff=%+v err=%v", d, err)
	}
	raw, _ := json.Marshal(d)
	if !strings.Contains(string(raw), `"warnings":[]`) || strings.Contains(string(raw), "_chars") {
		t.Fatalf("wire=%s", raw)
	}
	d, err = Compute("domain_rules", before, strings.ReplaceAll(strings.ReplaceAll(before, "\n", "\r\n"), "前言", "前言  \r\n\r\n"), 1)
	if err != nil || len(d.Modified) != 0 || d.UnchangedCount != 4 {
		t.Fatalf("normalized=%+v err=%v", d, err)
	}
}

func TestDiffValidation(t *testing.T) {
	for _, tt := range []struct{ chapter, content, code string }{
		{"wrong", "", "invalid_request"}, {"overview", strings.Repeat("字", 21846), "baseline_too_large"},
		{"overview", "\x00", "baseline_content_invalid"}, {"overview", string([]byte{0xff}), "baseline_content_invalid"},
	} {
		_, err := Parse(tt.chapter, tt.content)
		var e *Error
		if !errors.As(err, &e) || e.Code != tt.code {
			t.Fatalf("error=%v want=%s", err, tt.code)
		}
	}
	for _, text := range []string{"", strings.Repeat("x", 65536), "### [Stage-] title", "### [X-" + strings.Repeat("字", 65) + "] title"} {
		if _, err := Parse("overview", text); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPreambleKeyCollision(t *testing.T) {
	got, err := Parse("overview", "Intro\n# _preamble\nHeading body")
	want := []Section{{"_preamble", "", "Intro"}, {"_preamble (2)", "_preamble", "Heading body"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("sections=%+v err=%v", got, err)
	}
}
