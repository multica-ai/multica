import type { Lang } from "../voice-input/copy";

export type TabId = "inbox" | "snoozed" | "archived";
export const TABS: readonly TabId[] = ["inbox", "snoozed", "archived"];

export const TAB_LABELS: Record<Lang, Record<TabId, string>> = {
  zh: { inbox: "收件箱", snoozed: "已延后", archived: "已归档" },
  en: { inbox: "Inbox", snoozed: "Snoozed", archived: "Archived" },
};

export type RowStatus = "todo" | "in_progress" | "in_review" | "done" | "blocked";

export interface Row {
  key: string;
  identifier: string;
  title: Record<Lang, string>;
  status: RowStatus;
  time: Record<Lang, string>;
  unread: boolean;
}

const TITLES: { zh: string; en: string; status: RowStatus }[] = [
  { zh: "Work 面板 Inbox 标签在默认宽度下被截断", en: "Work panel Inbox tabs are cut off at the default width", status: "in_review" },
  { zh: "自动切下一条时评论区跳动", en: "Comments jump when the Inbox advances to the next issue", status: "in_progress" },
  { zh: "Desktop 更新后侧栏宽度被重置", en: "Desktop update resets the sidebar width", status: "todo" },
  { zh: "Squad 成员在移动端无法改派 issue", en: "Squad members can't reassign issues on mobile", status: "blocked" },
  { zh: "评论里的 mermaid 图在暗色模式下看不清", en: "Mermaid diagrams in comments are unreadable in dark mode", status: "in_progress" },
  { zh: "Runtime 断线后 agent 任务没有重试", en: "Agent tasks aren't retried after a runtime disconnects", status: "todo" },
  { zh: "Autopilot 每周汇总漏掉了已关闭 issue", en: "Autopilot weekly digest misses closed issues", status: "in_review" },
  { zh: "收件箱延后到明早 9 点时区不对", en: "Snoozing until 9am tomorrow uses the wrong timezone", status: "done" },
  { zh: "项目列表把已完成的项目排在最前", en: "Projects list puts completed projects first", status: "todo" },
  { zh: "Skill 导入时忽略了子目录", en: "Skill import skips nested folders", status: "in_progress" },
  { zh: "PR 合并后 issue 状态没有变成 Merged", en: "Issue doesn't move to Merged after the PR merges", status: "done" },
  { zh: "Billing 页座位数和发票不一致", en: "Seat count on Billing doesn't match the invoice", status: "blocked" },
  { zh: "搜索结果里高亮错位", en: "Search highlights are offset in results", status: "todo" },
  { zh: "Chat 窗口长回复滚动卡顿", en: "Long chat replies scroll with jank", status: "in_review" },
];

const TIMES: Record<Lang, string>[] = [
  { zh: "2 分钟", en: "2m" },
  { zh: "14 分钟", en: "14m" },
  { zh: "1 小时", en: "1h" },
  { zh: "3 小时", en: "3h" },
  { zh: "昨天", en: "1d" },
  { zh: "2 天", en: "2d" },
  { zh: "5 天", en: "5d" },
  { zh: "1 周", en: "1w" },
];

function rows(prefix: string, count: number, offset: number, unreadUntil: number): Row[] {
  return Array.from({ length: count }, (_, i) => {
    const t = TITLES[(i + offset) % TITLES.length]!;
    return {
      key: `${prefix}-${i}`,
      identifier: `MUL-${9930 - offset * 7 - i * 3}`,
      title: { zh: t.zh, en: t.en },
      status: t.status,
      time: TIMES[Math.min(TIMES.length - 1, Math.floor((i * TIMES.length) / count))]!,
      unread: i < unreadUntil,
    };
  });
}

/** Real volumes: the Inbox past its limit, a short snooze list, a long archive. */
export const ROWS: Record<TabId, Row[]> = {
  inbox: rows("inbox", 23, 0, 6),
  snoozed: rows("snoozed", 3, 5, 0),
  archived: rows("archived", 40, 9, 0),
};

export const SECTION_LIMIT = 8;

export const NEXT_SECTIONS: { label: Record<Lang, string>; rows: Record<Lang, string>[] }[] = [
  {
    label: { zh: "我的 Issue", en: "My issues" },
    rows: [
      { zh: "Inbox 标签切换动效", en: "Motion for switching Inbox tabs" },
      { zh: "侧栏 Section 折叠记忆", en: "Remember collapsed sidebar sections" },
      { zh: "Storybook 原型：语音输入", en: "Storybook prototype: voice input" },
      { zh: "e2e：区块头标签不截断", en: "e2e: section header tabs never clip" },
    ],
  },
  {
    label: { zh: "项目", en: "Projects" },
    rows: [
      { zh: "Inbox 2.0", en: "Inbox 2.0" },
      { zh: "Desktop 体验打磨", en: "Desktop polish" },
      { zh: "Squads", en: "Squads" },
    ],
  },
];
