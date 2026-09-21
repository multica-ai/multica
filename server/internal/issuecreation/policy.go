package issuecreation

import "strings"

// Input is the small, transport-independent part of a new issue used to
// decide whether creation should also start the assigned agent.
type Input struct {
	Title           string
	Description     string
	RequestedStatus string
	StatusExplicit  bool
	HasAgent        bool
}

// Decision is deliberately limited to the existing built-in statuses. The
// caller keeps custom statuses unchanged, so this policy cannot alter the
// behavior of existing workflows.
type Decision struct {
	Status    string
	AutoStart bool
}

// Decide is the shared creation policy for manual and quick-create inputs.
// It is intentionally conservative: parking wins over starting, and a
// runnable classification without an executable assignee parks as well.
func Decide(in Input) Decision {
	requested := in.RequestedStatus
	if requested != "" && requested != "todo" && requested != "backlog" {
		return Decision{Status: requested}
	}

	text := strings.ToLower(strings.TrimSpace(in.Title + "\n" + in.Description))
	if containsAny(text, deferSignals) {
		return parked()
	}
	if in.StatusExplicit && requested == "backlog" {
		return parked()
	}
	if containsAny(text, startSignals) || (in.StatusExplicit && requested == "todo") {
		return startOrPark(in.HasAgent)
	}

	switch classify(text) {
	case categoryBugfix, categoryProduct, categoryTechnical:
		return startOrPark(in.HasAgent)
	default:
		return parked()
	}
}

type category uint8

const (
	categoryOther category = iota
	categoryBugfix
	categoryProduct
	categoryTechnical
	categoryAnalysis
	categoryResearch
	categoryBusiness
)

// classify only accepts unambiguous lexical signals. In particular, analysis
// and research win before technical markers so "technical research" parks.
func classify(text string) category {
	switch {
	case containsAny(text, analysisSignals):
		return categoryAnalysis
	case containsAny(text, researchSignals):
		return categoryResearch
	case containsAny(text, businessSignals):
		return categoryBusiness
	case containsAny(text, bugfixSignals):
		return categoryBugfix
	case containsAny(text, productSignals):
		return categoryProduct
	case containsAny(text, technicalSignals):
		return categoryTechnical
	default:
		return categoryOther
	}
}

func startOrPark(hasAgent bool) Decision {
	if !hasAgent {
		return parked()
	}
	return Decision{Status: "todo", AutoStart: true}
}

func parked() Decision { return Decision{Status: "backlog"} }

func containsAny(text string, signals []string) bool {
	for _, signal := range signals {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

var deferSignals = []string{
	"只记录", "仅记录", "待规划", "以后做", "以后再做", "先别开始", "暂不开工",
	"暂不开始", "暂时不开始", "暂不执行", "先不执行", "不是待执行", "先别做",
	"先不做", "暂缓", "先放着", "record only", "just record", "do not start",
	"don't start", "not ready", "do later", "not now", "defer", "park this", "backlog",
}

var startSignals = []string{
	"马上开始", "现在开始", "立即开始", "立刻开始", "开始执行", "开始做", "开始处理",
	"现在做", "直接做", "直接推进", "立即推进", "马上推进", "创建后执行", "直接执行",
	"现在执行", "马上执行", "立即开工", "立刻开工", "start now", "start immediately",
	"begin immediately", "do it now", "execute now", "work on it now",
}

var analysisSignals = []string{
	"分析", "评估", "可行性", "方案", "拆解", "头脑风暴", "analysis", "assess", "evaluate",
}

var researchSignals = []string{
	"调研", "研究", "对比", "竞品", "research", "investigate", "explore", "benchmark",
}

var businessSignals = []string{
	"商务", "销售", "客户", "合作", "报价", "合同", "商机", "business", "sales",
	"partnership", "customer",
}

var bugfixSignals = []string{
	"bugfix", "bug fix", "bug", "修复", "缺陷", "故障", "报错", "fix ", "fix:", "fix/",
}

var productSignals = []string{
	"产品任务", "产品需求", "产品问题", "功能需求", "需求开发", "feature request", "product task",
}

var technicalSignals = []string{
	"技术任务", "技术改造", "技术问题", "实现", "开发", "编码", "代码", "接口", "api", "backend",
	"frontend", "前端", "后端", "重构", "refactor", "implement",
}
