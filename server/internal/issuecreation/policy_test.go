package issuecreation

import "testing"

func TestDecideCreationPolicy(t *testing.T) {
	tests := []struct {
		name       string
		input      Input
		wantStatus string
		wantStart  bool
	}{
		{"bugfix defaults to start", Input{Title: "修复登录报错", HasAgent: true}, "todo", true},
		{"product defaults to start", Input{Title: "产品任务：新增筛选", HasAgent: true}, "todo", true},
		{"technical defaults to start", Input{Title: "实现导出 API", HasAgent: true}, "todo", true},
		{"analysis parks", Input{Title: "分析导出 API 的方案", HasAgent: true}, "backlog", false},
		{"research parks", Input{Title: "调研竞品", HasAgent: true}, "backlog", false},
		{"business parks", Input{Title: "客户合作跟进", HasAgent: true}, "backlog", false},
		{"explicit start overrides category", Input{Title: "分析导出方案，现在开始", HasAgent: true}, "todo", true},
		{"explicit defer wins", Input{Title: "修复登录报错，先别开始", HasAgent: true}, "backlog", false},
		{"explicit status conflict parks", Input{Title: "修复登录报错，现在开始", RequestedStatus: "backlog", StatusExplicit: true, HasAgent: true}, "backlog", false},
		{"unknown parks", Input{Title: "视频规划", HasAgent: true}, "backlog", false},
		{"missing agent parks runnable work", Input{Title: "修复登录报错"}, "backlog", false},
		{"explicit todo and agent starts", Input{Title: "分析这个需求", RequestedStatus: "todo", StatusExplicit: true, HasAgent: true}, "todo", true},
		{"custom status is unchanged", Input{Title: "修复登录报错", RequestedStatus: "in_review", HasAgent: true}, "in_review", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Decide(tt.input)
			if got.Status != tt.wantStatus || got.AutoStart != tt.wantStart {
				t.Fatalf("Decide() = %+v, want status=%q start=%t", got, tt.wantStatus, tt.wantStart)
			}
		})
	}
}

func TestDecideSameSemanticInputForBothEntrances(t *testing.T) {
	cases := []struct {
		name  string
		input Input
		want  Decision
	}{
		{"bugfix default", Input{Title: "修复登录报错", HasAgent: true}, Decision{Status: "todo", AutoStart: true}},
		{"product default", Input{Title: "产品任务：新增筛选", HasAgent: true}, Decision{Status: "todo", AutoStart: true}},
		{"technical default", Input{Title: "实现导出 API", HasAgent: true}, Decision{Status: "todo", AutoStart: true}},
		{"analysis parks", Input{Title: "分析导出 API 的方案", HasAgent: true}, Decision{Status: "backlog"}},
		{"research parks", Input{Title: "调研竞品", HasAgent: true}, Decision{Status: "backlog"}},
		{"business parks", Input{Title: "客户合作跟进", HasAgent: true}, Decision{Status: "backlog"}},
		{"other parks", Input{Title: "视频规划", HasAgent: true}, Decision{Status: "backlog"}},
		{"explicit start", Input{Title: "现在开始分析方案", HasAgent: true}, Decision{Status: "todo", AutoStart: true}},
		{"defer wins", Input{Title: "修复登录报错，暂不执行", HasAgent: true}, Decision{Status: "backlog"}},
		{"conflict parks", Input{Title: "现在开始修复登录报错，但先别开始", HasAgent: true}, Decision{Status: "backlog"}},
		{"uncertain parks", Input{Title: "视频规划", HasAgent: true}, Decision{Status: "backlog"}},
		{"unroutable parks", Input{Title: "修复登录报错"}, Decision{Status: "backlog"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			manual := Decide(tt.input)
			quick := Decide(tt.input)
			if manual != quick {
				t.Fatalf("same semantic input diverged: manual=%+v quick=%+v", manual, quick)
			}
			if manual != tt.want {
				t.Fatalf("Decide() = %+v, want %+v", manual, tt.want)
			}
		})
	}
	// Explicit immediate-start language still requires a routable execution
	// target; the policy must not claim a run when the target is absent.
	if got := Decide(Input{Title: "现在开始分析方案"}); got != (Decision{Status: "backlog"}) {
		t.Fatalf("explicit start without agent = %+v, want backlog", got)
	}
	if got := Decide(Input{Title: "客户合作跟进", StatusExplicit: true, RequestedStatus: "todo", HasAgent: true}); got != (Decision{Status: "todo", AutoStart: true}) {
		t.Fatalf("explicit todo with agent = %+v, want todo/start", got)
	}
	if got := Decide(Input{Title: "客户合作跟进", StatusExplicit: true, RequestedStatus: "backlog", HasAgent: true}); got != (Decision{Status: "backlog"}) {
		t.Fatalf("explicit backlog = %+v, want backlog", got)
	}
	if got := Decide(Input{Title: "修复登录报错", StatusExplicit: true, RequestedStatus: "todo", HasAgent: true, Description: "先别开始"}); got != (Decision{Status: "backlog"}) {
		t.Fatalf("defer must beat explicit todo = %+v, want backlog", got)
	}
	if got := Decide(Input{Title: "修复登录报错", StatusExplicit: true, RequestedStatus: "backlog", HasAgent: true, Description: "现在开始"}); got != (Decision{Status: "backlog"}) {
		t.Fatalf("conflicting status and start = %+v, want backlog", got)
	}
	if got := Decide(Input{Title: "修复登录报错", RequestedStatus: "in_review", HasAgent: true}); got != (Decision{Status: "in_review"}) {
		t.Fatalf("custom status changed = %+v, want unchanged", got)
	}

	for _, input := range []Input{
		{Title: "分析导出方案", HasAgent: true},
		{Title: "调研竞品", HasAgent: true},
		{Title: "客户合作跟进", HasAgent: true},
	} {
		if got := Decide(input); got.AutoStart {
			t.Fatalf("passive input unexpectedly starts: input=%+v decision=%+v", input, got)
		}
	}
}
