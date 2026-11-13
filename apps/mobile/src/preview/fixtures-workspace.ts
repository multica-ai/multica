import type { IssueGroupModel } from "../ui/components/IssueGroups";
import type { IssueRowModel } from "../ui/components/IssueRow";
import type { AutopilotRunModel, TriggerModel } from "../ui/model/autopilots";
import type { ProjectSection } from "../ui/model/projects";
import type { AssigneeOption, ProjectOption } from "../ui/screens/autopilots/AutopilotEditorView";
import type { AutopilotSection } from "../ui/screens/autopilots/AutopilotsView";
import type { AutopilotModel } from "../ui/screens/autopilots/AutopilotView";
import type { NextRunsModel } from "../ui/screens/autopilots/ScheduleForm";
import type { RosterEntry } from "../ui/model/roster";
import type { ViewRowModel } from "../ui/model/views";
import type { SquadDraft } from "../ui/screens/squads/SquadEditorView";
import type { SquadRowModel } from "../ui/screens/squads/SquadsView";
import type { SquadMemberModel, SquadModel } from "../ui/screens/squads/SquadView";
import type { FilterChipModel } from "../ui/screens/views/SavedViewView";
import type { ProjectDraft } from "../ui/screens/projects/ProjectEditorView";
import type { ProjectModel, ProjectProgressModel, ProjectResourceModel } from "../ui/screens/projects/ProjectView";

// Fixture data for the workspace screens: projects, views, squads, autopilots.

export const people = {
  lambda: { kind: "agent", name: "Lambda" },
  codex: { kind: "agent", name: "Codex" },
  claude: { kind: "agent", name: "Claude" },
  me: { kind: "member", name: "Jiayuan Zhang" },
  wang: { kind: "member", name: "王悦" },
  li: { kind: "member", name: "李想" },
} as const;

const row = (identifier: string, title: string, status: string, assignee: IssueRowModel["assignee"]): IssueRowModel => ({
  id: identifier,
  identifier,
  title,
  status: { key: status, color: null, icon: null },
  assignee,
});

export const savedViewName = "iOS 冲刺";

export const projectSections: ProjectSection[] = [
  {
    status: "in_progress",
    projects: [
      { id: "p1", icon: "📱", title: "iOS 客户端", status: "in_progress", lead: people.lambda, done: 18, total: 30, due: "11月30日" },
      { id: "p2", icon: "🧭", title: "导航与视图重构", status: "in_progress", lead: people.wang, done: 9, total: 12, due: "12月6日" },
      { id: "p3", icon: "⚡️", title: "自动化 2.0", status: "in_progress", lead: people.codex, done: 4, total: 21, due: null },
    ],
  },
  {
    status: "planned",
    projects: [
      { id: "p4", icon: "🔐", title: "SSO 与细粒度权限", status: "planned", lead: people.li, done: 0, total: 8, due: "1月15日" },
      { id: "p5", icon: null, title: "繁体中文与日文", status: "planned", lead: null, done: 1, total: 6, due: null },
    ],
  },
  {
    status: "completed",
    projects: [{ id: "p6", icon: "📦", title: "0.9 发布", status: "completed", lead: people.me, done: 24, total: 24, due: null }],
  },
];

export const project: ProjectModel = {
  icon: "📱",
  title: "iOS 客户端",
  description: "用 Expo 重写 iOS 客户端，产品逻辑全部来自 @multica/core。先覆盖收件箱、任务和智能体对话。",
  status: "in_progress",
  priority: "high",
  lead: people.lambda,
  dates: "11月1日 – 11月30日",
};

export const projectProgress: ProjectProgressModel = { done: 18, started: 7, unstarted: 5 };

export const projectResources: ProjectResourceModel[] = [
  { id: "r1", icon: "FolderGit", label: "multica-ai/multica", detail: "GitHub 仓库", url: "https://github.com/multica-ai/multica" },
  { id: "r2", icon: "Folder", label: "multica", detail: "本地目录 · ~/code/multica", url: null },
];

export const projectIssueGroups: IssueGroupModel[] = [
  {
    key: "in_review",
    label: "审核中",
    color: null,
    icon: null,
    finished: false,
    issues: [row("MUL-9202", "Multica iOS", "in_review", people.lambda)],
  },
  {
    key: "in_progress",
    label: "进行中",
    color: null,
    icon: null,
    finished: false,
    issues: [row("MUL-9240", "推送通知：APNs 设备注册", "in_progress", people.codex), row("MUL-9241", "项目与视图页面", "in_progress", people.lambda)],
  },
  {
    key: "todo",
    label: "待办",
    color: null,
    icon: null,
    finished: false,
    issues: [row("MUL-9243", "iPad 双栏布局", "todo", null), row("MUL-9244", "离线时的评论草稿", "todo", people.me)],
  },
  {
    key: "done",
    label: "已完成",
    color: null,
    icon: null,
    finished: true,
    issues: [row("MUL-9119", "迁移会话续期到 core", "done", people.lambda)],
  },
];

export const starredViews: ViewRowModel[] = [
  { id: "builtin:my_issues", label: "我的任务", detail: "内置", starred: true },
  { id: "builtin:my_agents", label: "我的智能体在做", detail: "内置", starred: true },
  { id: "builtin:all_issues", label: "全部任务", detail: "内置", starred: true },
  { id: "view:ios", label: savedViewName, detail: "我的视图 · 3 个状态 · 另有 1 个条件", starred: true },
];

export const otherViews: ViewRowModel[] = [
  { id: "builtin:unread", label: "未读", detail: "内置", starred: false },
  { id: "view:review", label: "待审核 PR", detail: "工作区视图 · 1 个状态", starred: false },
  { id: "view:due", label: "本周到期", detail: "工作区视图 · 另有 1 个条件", starred: false },
  { id: "view:blocked", label: "受阻任务", detail: "我的视图 · 1 个状态", starred: false },
];

export const savedViewFilters: FilterChipModel[] = [
  { icon: "Filter", label: "进行中、审核中、待办" },
  { icon: "FolderKanban", label: "iOS 客户端" },
];

export const savedViewGroups: IssueGroupModel[] = projectIssueGroups.filter((group) => !group.finished);

export const squads: SquadRowModel[] = [
  { id: "s1", name: "移动端小队", description: "iOS 客户端的开发、评审和发布", leader: people.lambda, members: [people.lambda, people.codex, people.claude, people.me], memberCount: 4 },
  { id: "s2", name: "发布小队", description: "整理变更、写 changelog、跑发布检查清单", leader: people.claude, members: [people.claude, people.codex, people.wang], memberCount: 3 },
  { id: "s3", name: "Bug 分诊", description: "新 issue 先到这里，复现后指派给合适的人", leader: people.codex, members: [people.codex, people.lambda, people.li, people.wang, people.me], memberCount: 7 },
];

export const squad: SquadModel = {
  name: "移动端小队",
  description: "iOS 客户端的开发、评审和发布。把任务指派给小队，队长会拆分并分派给合适的成员。",
  instructions: "实现交给 Codex，评审交给 Claude；涉及设计的先在任务里贴截图再合并。",
  leader: people.lambda,
  archived: false,
};

export const squadMembers: SquadMemberModel[] = [
  { key: "agent:lambda", actor: people.lambda, role: "leader", leader: true },
  { key: "agent:codex", actor: people.codex, role: "实现 · 原生模块和构建", leader: false },
  { key: "agent:claude", actor: people.claude, role: "评审 · 代码和设计走查", leader: false },
  { key: "member:me", actor: people.me, role: "负责人", leader: false },
];

export const squadRecent: IssueRowModel[] = [
  row("MUL-9241", "项目与视图页面", "in_progress", people.lambda),
  row("MUL-9240", "推送通知：APNs 设备注册", "in_progress", people.codex),
  row("MUL-9202", "Multica iOS", "in_review", people.lambda),
];

export const squadDraft: SquadDraft = { name: "", description: "", instructions: "", leader: "lambda", members: ["agent:codex", "agent:claude", "member:me"] };

export const roster: RosterEntry[] = [
  { value: "member:me", type: "member", id: "me", actor: people.me, detail: "jiayuan@multica.ai" },
  { value: "member:wang", type: "member", id: "wang", actor: people.wang, detail: "wang@multica.ai" },
  { value: "member:li", type: "member", id: "li", actor: people.li, detail: "li@multica.ai" },
  { value: "agent:lambda", type: "agent", id: "lambda", actor: people.lambda, detail: "主力编码智能体" },
  { value: "agent:codex", type: "agent", id: "codex", actor: people.codex, detail: "代码评审" },
  { value: "agent:claude", type: "agent", id: "claude", actor: people.claude },
];

export const projectDraft: ProjectDraft = {
  icon: "📱",
  title: "iOS 客户端",
  description: "用 Expo 重写 iOS 客户端，产品逻辑全部来自 @multica/core。",
  status: "in_progress",
  priority: "high",
  lead: "agent:lambda",
  start: "2026-11-01",
  due: "2026-11-30",
};

const squadActor = { kind: "agent", name: "移动端小队" } as const;

export const autopilotSections: AutopilotSection[] = [
  {
    key: "active",
    title: "启用中",
    rows: [
      { id: "a1", title: "每日站会摘要", paused: false, assignee: people.lambda, triggers: "定时", triggerIcon: "CalendarClock", next: "明天 09:00", lastFailed: false },
      { id: "a2", title: "新 issue 分诊", paused: false, assignee: squadActor, triggers: "Webhook", triggerIcon: "Webhook", next: null, lastFailed: false },
      { id: "a3", title: "依赖升级检查", paused: false, assignee: people.codex, triggers: "定时 · Webhook", triggerIcon: "CalendarClock", next: "周一 10:00", lastFailed: true },
    ],
  },
  {
    key: "paused",
    title: "已暂停",
    rows: [{ id: "a4", title: "周报草稿", paused: true, assignee: people.claude, triggers: "定时", triggerIcon: "CalendarClock", next: null, lastFailed: false }],
  },
];

export const autopilot: AutopilotModel = {
  title: "每日站会摘要",
  description: "汇总昨天合并的 PR、进行中的任务和阻塞项，整理成一条站会摘要发到 MUL-9202。",
  paused: false,
  assignee: people.lambda,
  mode: "创建一个任务",
  project: "iOS 客户端",
};

export const autopilotTriggers: TriggerModel[] = [
  { id: "t1", kind: "schedule", icon: "CalendarClock", label: "工作日 09:00", detail: "Asia/Shanghai · 下次 明天 09:00", enabled: true },
  { id: "t2", kind: "webhook", icon: "Webhook", label: "Webhook", detail: "URL 收到请求时触发", enabled: false },
];

export const autopilotRuns: AutopilotRunModel[] = [
  { id: "r1", status: "running", when: "今天 09:00 · 定时", detail: "运行中", issueId: "i1" },
  { id: "r2", status: "issue_created", when: "昨天 09:00 · 定时", detail: "创建了任务", issueId: "i2" },
  { id: "r3", status: "failed", when: "11月12日 09:00 · 定时", detail: "失败 · 智能体离线", issueId: null },
  { id: "r4", status: "completed", when: "11月11日 15:42 · 手动", detail: "已完成", issueId: "i3" },
];

export const assigneeOptions: AssigneeOption[] = [
  { value: "agent:lambda", actor: people.lambda, detail: "智能体" },
  { value: "agent:codex", actor: people.codex, detail: "智能体" },
  { value: "agent:claude", actor: people.claude, detail: "智能体" },
  { value: "squad:s1", actor: squadActor, detail: "小队" },
  { value: "squad:s2", actor: { kind: "agent", name: "发布小队" }, detail: "小队" },
];

export const projectOptions: ProjectOption[] = [
  { id: "p1", title: "iOS 客户端", icon: "📱" },
  { id: "p2", title: "Agent 运行时", icon: "🤖" },
];

export const nextRuns: NextRunsModel = { state: "ready", items: ["明天 09:00", "周二 09:00", "周三 09:00"] };
