import type { IssueRowModel } from "../ui/components/IssueRow";
import type { AgentListRow } from "../ui/screens/agents/AgentsView";
import type { ChatItem, PendingRun } from "../ui/screens/chat/ChatView";
import type { AgentChoice } from "../ui/screens/chat/NewChatView";
import type { InboxRow } from "../ui/screens/inbox/InboxView";
import type { AgentRunModel, IssueModel, PullRequestModel, TimelineModel } from "../ui/screens/issue/IssueView";

const lambda = { kind: "agent", name: "Lambda" } as const;
const claude = { kind: "agent", name: "Claude" } as const;
const codex = { kind: "agent", name: "Codex" } as const;

export const inboxRows: InboxRow[] = [
  { id: "1", issueId: "issue-1", identifier: null, title: "Multica iOS", summary: "Lambda 等你回复：设计语言 v2 已出，参考 Linear 和 Cursor", time: "2 分钟", actor: lambda, badge: { kind: "status", status: "in_review" }, unread: true },
  { id: "2", issueId: "issue-2", identifier: null, title: "日文下侧边栏标签被截断", summary: "李想 评论：PR 已就绪，CI 全部通过，可以合并吗？", time: "18 分钟", actor: { kind: "member", name: "李想" }, badge: { kind: "icon", icon: "Quote" }, unread: true },
  { id: "3", issueId: "issue-3", identifier: null, title: "每个后端 PR 编译 darwin", summary: "Claude 运行失败：go test ./internal/daemon 超时", time: "1 小时", actor: claude, badge: { kind: "status", status: "blocked" }, unread: true },
  { id: "4", issueId: "issue-4", identifier: null, title: "0.9.1 发布清单", summary: "王悦 提到了你", time: "3 小时", actor: { kind: "member", name: "王悦" }, badge: { kind: "icon", icon: "AtSign" }, unread: false },
  { id: "5", issueId: "issue-5", identifier: null, title: "迁移会话续期到 core", summary: "Lambda 完成了任务，PR #10498 已合并", time: "昨天", actor: lambda, badge: { kind: "status", status: "done" }, unread: false },
  { id: "6", issueId: "issue-6", identifier: null, title: "收件箱支持批量归档", summary: "你设置的提醒到期", time: "昨天", actor: { kind: "member", name: "Jiayuan Zhang" }, badge: { kind: "icon", icon: "Clock" }, unread: false },
  { id: "7", issueId: "issue-7", identifier: null, title: "看板列宽在窄屏溢出", summary: "Codex 提交审核", time: "2 天", actor: codex, badge: { kind: "status", status: "in_review" }, unread: false },
];

const jiayuan = { kind: "member", name: "Jiayuan Zhang" } as const;
const st = (key: string) => ({ key, color: null, icon: null });

export const myIssues: IssueRowModel[] = [
  { id: "a", identifier: "MUL-9202", title: "Multica iOS", status: st("in_progress"), assignee: lambda },
  { id: "b", identifier: "MUL-9188", title: "日文下侧边栏标签被截断", status: st("in_review"), assignee: codex },
  { id: "c", identifier: "MUL-9131", title: "收件箱支持批量归档", status: st("in_progress"), assignee: jiayuan },
  { id: "d", identifier: "MUL-9150", title: "0.9.1 发布清单", status: st("todo"), assignee: jiayuan },
  { id: "e", identifier: "MUL-9172", title: "邀请链接过期后的提示", status: st("todo"), assignee: claude },
  { id: "f", identifier: "MUL-9104", title: "iPad 键盘快捷键", status: st("backlog"), assignee: jiayuan },
  { id: "g", identifier: "MUL-9099", title: "离线时的评论草稿", status: st("backlog"), assignee: jiayuan },
  { id: "h", identifier: "MUL-9119", title: "迁移会话续期到 core", status: st("done"), assignee: lambda },
  { id: "i", identifier: "MUL-9090", title: "工作区切换动画", status: st("done"), assignee: jiayuan },
  { id: "j", identifier: "MUL-9077", title: "推送通知（APNs）", status: st("blocked"), assignee: jiayuan },
];

export const issue: IssueModel = {
  identifier: "MUL-9202",
  title: "Multica iOS",
  description: "删掉旧 iOS 代码重写：先把非 UI 的核心做完，UI 先出 mockup 来选。",
  status: { key: "in_review", label: "审核中", color: null, icon: null },
  priority: "high",
  assignee: lambda,
  project: "移动端",
};

export const run: AgentRunModel = {
  id: "t1",
  agent: lambda,
  phase: "running",
  time: "12 分钟",
  steps: [
    { id: "s1", label: "Read apps/mobile/AGENTS.md", done: true },
    { id: "s2", label: "Bash pnpm --filter @multica/mobile typecheck", done: true },
    { id: "s3", label: "Edit InboxView.tsx", done: false },
  ],
  hiddenSteps: 9,
};

export const pullRequests: PullRequestModel[] = [
  { id: "p1", title: "rebuild the iOS app on @multica/core", state: "draft", additions: 1726, deletions: 43665, files: 434, url: "https://github.com" },
];

export const timeline: TimelineModel[] = [
  { kind: "activity", id: "e1", actor: jiayuan, text: "创建了任务", time: "8 小时" },
  { kind: "activity", id: "e2", actor: lambda, text: "把状态改为 审核中", time: "1 小时" },
  { kind: "comment", id: "e3", actor: jiayuan, body: "UI 有点太丑，参考 Mobbin 上 **Linear** 和 **Cursor** 重新 mockup，先确定风格。", time: "5 分钟", deleted: false },
];

export const agentsRunning: AgentListRow[] = [
  { id: "r1", title: "Multica iOS", state: { text: "运行中", tone: "warning" }, meta: "Lambda · MUL-9202", time: "12 分钟", unread: false, target: { kind: "issue", issueId: "a" } },
  { id: "r2", title: "修复看板列宽溢出", state: { text: "排队中", tone: "warning" }, meta: "Codex · MUL-9160", time: "2 分钟", unread: false, target: { kind: "issue", issueId: "b" } },
];

export const agentsConversations: AgentListRow[] = [
  { id: "c1", title: "每个后端 PR 编译 darwin", meta: "Claude · 已经加到 CI 矩阵里了，跑一次大约 4 分钟", time: "1 小时", unread: true, target: { kind: "chat", sessionId: "c1" } },
  { id: "c2", title: "日文下侧边栏标签被截断", meta: "Codex · PR 已就绪，等你合并", time: "18 分钟", unread: true, target: { kind: "chat", sessionId: "c2" } },
  { id: "c3", title: "迁移会话续期到 core", meta: "Lambda · 已合并 #10498", time: "昨天", unread: false, target: { kind: "chat", sessionId: "c3" } },
  { id: "c4", title: "0.9.0 changelog", meta: "发布小队 · 无改动", time: "昨天", unread: false, target: { kind: "chat", sessionId: "c4" } },
  { id: "c5", title: "主题商店改为默认开启", meta: "Lambda · +40 −212", time: "3 天", unread: false, target: { kind: "chat", sessionId: "c5" } },
];

export const chatItems: ChatItem[] = [
  { kind: "user", id: "m1", text: "UI 有点太丑，参考 Linear 和 Cursor 重新出一版，先定风格。" },
  {
    kind: "assistant",
    id: "m2",
    taskId: "t0",
    failed: false,
    text: "完成。提炼出 8 条规则，三条最关键：\n\n- **单色画布** — 颜色只留给状态、头像和 diff\n- **浮动控件** — 圆形按钮和纯图标标签栏\n- **正文式输出** — 智能体回复不用气泡，见 `theme.ts`",
  },
  { kind: "user", id: "m3", text: "按这一版实现 iOS 吧。" },
];

export const pendingRun: PendingRun = {
  steps: [
    { id: "x1", label: "Write src/ui/theme/colors.ts", done: true },
    { id: "x2", label: "Bash pnpm preview:export", done: false },
  ],
  hiddenSteps: 14,
};

export const agentChoices: AgentChoice[] = [
  { id: "a1", actor: lambda, description: "Multica 全栈工程师，处理 issue 和 PR" },
  { id: "a2", actor: claude, description: "后端与 daemon" },
  { id: "a3", actor: codex, description: "前端修复" },
  { id: "a4", actor: { kind: "agent", name: "发布小队" }, description: "发版检查清单与 changelog" },
];

export const searchResults: IssueRowModel[] = [
  { ...myIssues[0]!, detail: "删掉旧 iOS 代码重写：先把非 UI 的核心做完" },
  { id: "s2", identifier: "MUL-8810", title: "iOS 推送通知设置页", status: st("todo"), assignee: null, detail: "通知分组与 iOS 系统设置联动" },
  { id: "s3", identifier: "MUL-8432", title: "iOS 深链接打开任务", status: st("done"), assignee: lambda },
];
