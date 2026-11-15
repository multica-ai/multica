import type { Lang } from "../voice-input/copy";

export type Text = Record<Lang, string>;

export type UpdateId = "inbox" | "views" | "no-project" | "notifications";

export interface Update {
  id: UpdateId;
  date: Text;
  title: Text;
  body: Text;
  bullets: Text[];
  cta: Text;
}

export const UPDATES: Update[] = [
  {
    id: "inbox",
    date: { zh: "11月15日", en: "Nov 15" },
    title: { zh: "收件箱搬进了「工作」", en: "The Inbox moved into Work" },
    body: {
      zh: "通知现在在工作面板的最上方，不用再切换空间。",
      en: "Notifications now sit at the top of the Work panel, so there is no space to switch to.",
    },
    bullets: [
      { zh: "读过的通知会留着，归档或任务完成后才离开", en: "Read notifications stay until you archive them or the issue is done" },
      { zh: "筛选和显示设置在收件箱的标题栏上", en: "Filters and display settings sit on the Inbox header" },
      { zh: "未读数显示在「工作」图标上", en: "The unread count shows on the Work icon" },
    ],
    cta: { zh: "看看收件箱", en: "Show me the Inbox" },
  },
  {
    id: "views",
    date: { zh: "11月15日", en: "Nov 15" },
    title: { zh: "新的内置视图", en: "New built-in views" },
    body: {
      zh: "「我的工作」「执行中」「已订阅」在侧栏里一点就开。",
      en: "My work, Working and Subscribed open from the sidebar in one click.",
    },
    bullets: [
      { zh: "我的工作：分配给我和我的智能体的任务", en: "My work: issues assigned to me or my agents" },
      { zh: "执行中：智能体正在运行的任务", en: "Working: issues an agent is running on" },
      { zh: "已订阅：我订阅的所有任务", en: "Subscribed: every issue I subscribe to" },
    ],
    cta: { zh: "打开我的工作", en: "Open My work" },
  },
  {
    id: "no-project",
    date: { zh: "11月15日", en: "Nov 15" },
    title: { zh: "「无项目」有了自己的位置", en: "No project gets its own row" },
    body: {
      zh: "不属于任何项目的任务，集中在项目区块的第一行。",
      en: "Issues outside every project gather in the first row of Projects.",
    },
    bullets: [
      { zh: "可以拖到任意项目之间", en: "Drag it anywhere among your projects" },
      { zh: "点 + 直接新建无项目的任务", en: "Its + creates an issue with no project" },
      { zh: "不需要的话，可以在项目显示设置里隐藏", en: "Hide it from Project display if you don't need it" },
    ],
    cta: { zh: "查看无项目任务", en: "See issues with no project" },
  },
  {
    id: "notifications",
    date: { zh: "11月12日", en: "Nov 12" },
    title: { zh: "通知设置重新分组", en: "Notification settings, regrouped" },
    body: {
      zh: "按通知怎么找到你来分组：关于你、你订阅的、智能体。",
      en: "Grouped by how a notification reaches you: about you, what you subscribe to, agents.",
    },
    bullets: [
      { zh: "每一类都可以单独关掉", en: "Turn off any kind on its own" },
      { zh: "写清楚了通知什么时候离开收件箱", en: "Spells out when a notification leaves the Inbox" },
    ],
    cta: { zh: "打开通知设置", en: "Open notification settings" },
  },
];

export const UI = {
  tag: { zh: "新功能", en: "New" },
  next: { zh: "下一条", en: "Next" },
  prev: { zh: "上一条", en: "Previous" },
  done: { zh: "知道了", en: "Got it" },
  close: { zh: "关闭", en: "Close" },
  closeAll: { zh: "全部关闭", en: "Dismiss all" },
  markAllRead: { zh: "全部标为已读", en: "Mark all as read" },
  view: { zh: "查看", en: "View" },
  back: { zh: "返回", en: "Back" },
  moreUpdates: { zh: "{n} 条更新", en: "{n} updates" },
  oneUpdate: { zh: "1 条更新", en: "1 update" },
  later: { zh: "更多更新在「新功能」里", en: "More in What's new" },
  count: { zh: "卡片数量", en: "Cards" },
  restart: { zh: "重新开始", en: "Restart" },
  allRead: { zh: "都看完了。关掉的更新可以在「新功能」里再找到。", en: "All caught up. Dismissed updates stay in What's new." },
};

export function countLabel(n: number, lang: Lang): string {
  return n === 1 ? UI.oneUpdate[lang] : UI.moreUpdates[lang].replace("{n}", String(n));
}
