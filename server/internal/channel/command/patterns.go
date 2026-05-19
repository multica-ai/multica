// This file owns the deterministic command rule corpus used after slash or
// source-command expansion.
package command

import (
	"regexp"
	"strings"

	chaction "github.com/multica-ai/multica/server/internal/channel/action"
)

// issueKeyPattern matches workspace-scoped keys like STA-2.
const issueKeyPattern = `[A-Za-z]{2,}-\d+`

func keyParam(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

type rule struct {
	kind       chaction.Kind
	confidence float64
	re         *regexp.Regexp
	params     func(sub []string) map[string]string
}

func defaultRules() []rule {
	key := issueKeyPattern
	return []rule{
		{
			kind: chaction.KindConfirmAction, confidence: 1,
			re: regexp.MustCompile(`^确认操作\s*([A-Za-z0-9]{4,16})$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"code": strings.ToUpper(sub[1])}
			},
		},
		{
			kind: chaction.KindCancelAction, confidence: 1,
			re: regexp.MustCompile(`^取消操作\s*([A-Za-z0-9]{4,16})$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"code": strings.ToUpper(sub[1])}
			},
		},
		{
			kind: chaction.KindIssueDetail, confidence: 1,
			re: regexp.MustCompile(`^查看详情\s*(?:\[)?(` + key + `)(?:\])?$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1])}
			},
		},
		{
			kind: chaction.KindIssueTimeline, confidence: 1,
			re: regexp.MustCompile(`^查看动态\s*(?:\[)?(` + key + `)(?:\])?(?:\s+([1-9][0-9]*))?$`),
			params: func(sub []string) map[string]string {
				page := "1"
				if len(sub) > 2 && sub[2] != "" {
					page = sub[2]
				}
				return map[string]string{"issue_key": keyParam(sub[1]), "page": page}
			},
		},
		{
			kind: chaction.KindIssueLogs, confidence: 1,
			re: regexp.MustCompile(`^查看日志\s*(?:\[)?(` + key + `)(?:\])?(?:\s+([1-9][0-9]*))?$`),
			params: func(sub []string) map[string]string {
				page := "1"
				if len(sub) > 2 && sub[2] != "" {
					page = sub[2]
				}
				return map[string]string{"issue_key": keyParam(sub[1]), "page": page}
			},
		},
		{
			kind: chaction.KindUnsupported, confidence: 1,
			re: regexp.MustCompile(`^删除\s*(?:\[)?(` + key + `)(?:\])?$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1])}
			},
		},
		{
			kind: chaction.KindUnsupported, confidence: 1,
			re: regexp.MustCompile(`^上传.+图(?:给\s*)?(?:\[)?(` + key + `)(?:\])?`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1])}
			},
		},
		{
			kind: chaction.KindAddComment, confidence: 1,
			re: regexp.MustCompile(`^在\s*(?:\[)?(` + key + `)(?:\])?\s*上(?:加一条)?评论\s*[:：]\s*(.+)$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1]), "comment": sub[2]}
			},
		},
		{
			kind: chaction.KindAddComment, confidence: 1,
			re: regexp.MustCompile(`^(` + key + `)\s*评论\s*[:：]\s*(.+)$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1]), "comment": sub[2]}
			},
		},
		{
			kind: chaction.KindSetStatus, confidence: 1,
			re: regexp.MustCompile(`^把\s*(?:\[)?(` + key + `)(?:\])?\s*标成\s*([\w-]+)$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1]), "status": sub[2]}
			},
		},
		{
			kind: chaction.KindSetStatus, confidence: 1,
			re: regexp.MustCompile(`^(` + key + `)\s*完成了$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1]), "status": "done"}
			},
		},
		{
			kind: chaction.KindSetStatus, confidence: 1,
			re: regexp.MustCompile(`^(` + key + `)\s*改成\s*([\w_]+)$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1]), "status": sub[2]}
			},
		},
		{
			kind: chaction.KindSetAssignee, confidence: 1,
			re: regexp.MustCompile(`^把\s*(?:\[)?(` + key + `)(?:\])?\s*指派给\s*@?(.+)$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1]), "assignee": sub[2]}
			},
		},
		{
			kind: chaction.KindSetAssignee, confidence: 1,
			re: regexp.MustCompile(`^(` + key + `)\s*指派给\s*@?(.+)$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1]), "assignee": sub[2]}
			},
		},
		{
			kind: chaction.KindSetPriority, confidence: 1,
			re: regexp.MustCompile(`^把\s*(?:\[)?(` + key + `)(?:\])?\s*改优先级\s*([\w_]+)$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1]), "priority": sub[2]}
			},
		},
		{
			kind: chaction.KindSetPriority, confidence: 1,
			re: regexp.MustCompile(`^(` + key + `)\s*改优先级\s*([\w_]+)$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1]), "priority": sub[2]}
			},
		},
		{
			kind: chaction.KindSetLabel, confidence: 1,
			re: regexp.MustCompile(`^把\s*(?:\[)?(` + key + `)(?:\])?\s*加标签\s*([\w-]+)$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1]), "label": sub[2], "op": "add"}
			},
		},
		{
			kind: chaction.KindSetLabel, confidence: 1,
			re: regexp.MustCompile(`^(` + key + `)\s*加标签\s*([\w-]+)$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1]), "label": sub[2], "op": "add"}
			},
		},
		{
			kind: chaction.KindSetLabel, confidence: 1,
			re: regexp.MustCompile(`^把\s*(?:\[)?(` + key + `)(?:\])?\s*去掉标签\s*([\w-]+)$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1]), "label": sub[2], "op": "remove"}
			},
		},
		{
			kind: chaction.KindSetLabel, confidence: 1,
			re: regexp.MustCompile(`^(` + key + `)\s*去掉标签\s*([\w-]+)$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1]), "label": sub[2], "op": "remove"}
			},
		},
		{
			kind: chaction.KindQueryProgress, confidence: 1,
			re: regexp.MustCompile(`^(?:各|所有|全部)?项目(?:的)?(?:进展|情况|状态)(?:怎么样|如何)?[？?]?$`),
			params: func(_ []string) map[string]string {
				return map[string]string{"scope": "projects"}
			},
		},
		{
			kind: chaction.KindQueryProgress, confidence: 1,
			re: regexp.MustCompile(`^(?:\[)?(` + key + `)(?:\])?\s*到哪了[？?]?$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"scope": "issue", "issue_key": keyParam(sub[1])}
			},
		},
		{
			kind: chaction.KindQueryProgress, confidence: 1,
			re: regexp.MustCompile(`^(?:\[)?(` + key + `)(?:\])?\s*(?:这个\s*(?i:issue)\s*)?(?:怎么样了?|什么情况|进展(?:怎么样|如何)?|状态(?:怎么样|如何)?|现在状态)[？?]?$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"scope": "issue", "issue_key": keyParam(sub[1])}
			},
		},
		{
			kind: chaction.KindQueryIssue, confidence: 1,
			re: regexp.MustCompile(`^(?:\[)?(` + key + `)(?:\])?\s*到哪了[？?]?$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1])}
			},
		},
		{
			kind: chaction.KindQueryIssue, confidence: 1,
			re: regexp.MustCompile(`^(?:\[)?(` + key + `)(?:\])?\s*(?:这个\s*(?i:issue)\s*)?(?:怎么样了?|什么情况|进展(?:怎么样|如何)?|状态(?:怎么样|如何)?|现在状态)[？?]?$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"issue_key": keyParam(sub[1])}
			},
		},
		{
			kind: chaction.KindQueryIssue, confidence: 1,
			re: regexp.MustCompile(`^(?:我的待办|待办列表|看一下待办|我有哪些待办)$`),
			params: func(_ []string) map[string]string {
				return map[string]string{}
			},
		},
		{
			kind: chaction.KindCreateIssue, confidence: 1,
			re: regexp.MustCompile(`(?i)^创建一个\s*Issue\s*[:：]?\s*(.+?)\s*(?:，|,|\s)+(?:指派给|分配给)\s*@?(.+)$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"title": strings.TrimSpace(sub[1]), "assignee": strings.TrimSpace(sub[2])}
			},
		},
		{
			kind: chaction.KindCreateIssue, confidence: 1,
			re: regexp.MustCompile(`^帮我记一个\s+(.+)$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"title": sub[1]}
			},
		},
		{
			kind: chaction.KindCreateIssue, confidence: 1,
			re: regexp.MustCompile(`(?i)^创建一个\s*Issue\s*[:：]\s*(.+)$`),
			params: func(sub []string) map[string]string {
				return map[string]string{"title": sub[1]}
			},
		},
		{
			kind: chaction.KindUnknown, confidence: 1,
			re: regexp.MustCompile(`(?i)^(?:在么|在吗|你好|您好|hi|hello)(?:啊)?[？?!！.]?$`),
			params: func(_ []string) map[string]string {
				return map[string]string{}
			},
		},
	}
}
