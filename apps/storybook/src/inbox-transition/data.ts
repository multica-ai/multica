import type { Lang } from "../voice-input/copy";

export type IssueStatus = "in_review" | "in_progress" | "todo" | "done";
export type IssuePriority = "urgent" | "high" | "medium" | "low";

export interface DemoActor {
  name: string;
  initials: string;
  isAgent?: boolean;
}

export interface DemoComment {
  id: string;
  author: DemoActor;
  time: Record<Lang, string>;
  body: Record<Lang, string>;
  isTarget?: boolean;
}

export interface DemoInboxIssue {
  id: string;
  identifier: string;
  title: Record<Lang, string>;
  project: Record<Lang, string>;
  status: IssueStatus;
  priority: IssuePriority;
  assignee: DemoActor;
  actor: DemoActor;
  reason: Record<Lang, string>;
  timeAgo: Record<Lang, string>;
  unread: boolean;
  agentRunning?: boolean;
  /** Mirrors `INBOX_IDLE_PREFETCH_LIMIT = 3` (only first 3 unread rows are pre-warmed today). */
  coldInCurrent: boolean;
  labels: Record<Lang, string[]>;
  description: Record<Lang, string[]>;
  subIssues: { id: string; done: boolean; title: Record<Lang, string> }[];
  comments: DemoComment[];
}

export const ACTORS = {
  jiayuan: { name: "Jiayuan", initials: "J" },
  bohan: { name: "Bohan", initials: "B" },
  alex: { name: "Alex", initials: "A" },
  emacs: { name: "Emacs", initials: "E", isAgent: true },
  lambda: { name: "Lambda", initials: "L", isAgent: true },
  cursor: { name: "Cursor", initials: "C", isAgent: true },
} satisfies Record<string, DemoActor>;

export function createSeedIssues(): DemoInboxIssue[] {
  return [
    {
      id: "mul-9865",
      identifier: "MUL-9865",
      title: {
        zh: "优化 Inbox 处理完上一条后自动切换下一条的页面跳动交互",
        en: "Smooth out page jump when Inbox auto-advances to the next item",
      },
      project: { zh: "收件箱体验", en: "Inbox Experience" },
      status: "in_review",
      priority: "high",
      assignee: ACTORS.emacs,
      actor: ACTORS.jiayuan,
      reason: { zh: "Jiayuan 提及了你", en: "Jiayuan mentioned you" },
      timeAgo: { zh: "刚刚", en: "just now" },
      unread: true,
      coldInCurrent: false,
      labels: { zh: ["交互细节", "收件箱"], en: ["Interaction", "Inbox"] },
      description: {
        zh: [
          "连续处理收件箱通知时，按 E 归档或者改完状态自动归档切下一条，整个详情区域会出现多段跳动：InboxBar 短暂消失、骨架屏闪现、右侧属性栏闪白，然后滚动条又突然从顶部跳到评论位置。",
          "需要把底层布局抖动与上层切页节奏拆开梳理：一方面消除 40px 纵向塌陷与预取盲区，另一方面设计一套既不拖慢高频 E E E 清理节奏、又不会在自动归档时产生「抽地毯」失控感的切换过渡。",
          "这里用真实尺度的工作区布局对比五种方案，包含长正文与折叠线以下的评论锚点，可以直接连续归档、标记完成、发送 @智能体 评论和按 Z 撤销来感受差别。",
        ],
        en: [
          "When working through Inbox notifications, pressing E to archive or completing an issue with auto-advance causes a multi-stage jump: the 40px InboxBar drops out, a skeleton flashes, the right sidebar blanks, and scroll teleports from the top down to the comment anchor.",
          "We need to separate layout-shift fixes from transition choreography: eliminate the 40px bounce and prefetch gaps first, then pick a pacing that keeps rapid E-E-E triage instant while avoiding the rug-pull feeling on auto-archive.",
          "This full-scale workbench lets you test five directions with long descriptions and below-the-fold comment anchors—try archiving, marking Done, sending an @agent comment, and pressing Z to undo.",
        ],
      },
      subIssues: [
        { id: "MUL-9866", done: true, title: { zh: "排查 InboxBar 40px 跳动与预取盲区", en: "Audit 40px InboxBar shift and prefetch miss" } },
        { id: "MUL-9867", done: false, title: { zh: "在 Storybook 搭建五种切换节奏原型", en: "Build 5 transition prototypes in Storybook" } },
      ],
      comments: [
        {
          id: "c1",
          author: ACTORS.emacs,
          time: { zh: "12 分钟前", en: "12m ago" },
          body: {
            zh: "已定位代码里 5 处叠在一起的跳动来源：leaveRow 在 push 前同步卸载 InboxBar、Skeleton 缺少 40px 占位、idle 预取只覆盖前 3 条未读、landingChromeHeld 清空侧栏，以及自动归档无缓冲直接抽走页面。",
            en: "Traced 5 stacked glitches in source: synchronous leaveRow unmounting InboxBar before push commits, skeleton missing the 40px bar slot, idle prefetch capped at first 3 unread rows, landingChromeHeld blanking the sidebar, and unbuffered auto-archive navigation.",
          },
        },
        {
          id: "c2",
          author: ACTORS.bohan,
          time: { zh: "8 分钟前", en: "8m ago" },
          body: {
            zh: "尤其是评论在页面靠下位置时，先闪一帧标题再瞬移下来非常明显。如果目标评论能对齐完再一起呈现，体感会稳很多。",
            en: "Especially when the notification comment is far down the thread, flashing the title for a frame before teleporting down feels very jarring. Aligning before reveal will calm it down a lot.",
          },
        },
        {
          id: "c3",
          author: ACTORS.jiayuan,
          time: { zh: "2 分钟前", en: "2m ago" },
          isTarget: true,
          body: {
            zh: "你在 Storybook 中来做一下吧，小窗口里没看出来太大区别——最好能在真实页面尺度下连续点几条、同时试一下标记完成后的自动切换。",
            en: "Build it in Storybook—couldn't tell much difference in the tiny HTML mockup. Let's feel it at full app scale across multiple items, including auto-advance after marking Done.",
          },
        },
      ],
    },
    {
      id: "mul-9848",
      identifier: "MUL-9848",
      title: {
        zh: "简化 Inbox Zero 空状态文案与撤销按钮样式",
        en: "Simplify Inbox Zero empty state copy and inline Undo styling",
      },
      project: { zh: "收件箱体验", en: "Inbox Experience" },
      status: "in_review",
      priority: "medium",
      assignee: ACTORS.lambda,
      actor: ACTORS.lambda,
      reason: { zh: "Lambda 运行已完成", en: "Lambda finished running" },
      timeAgo: { zh: "14 分钟", en: "14m" },
      unread: true,
      coldInCurrent: false,
      labels: { zh: ["UI 精修"], en: ["UI Polish"] },
      description: {
        zh: [
          "清完收件箱后的空状态之前堆了过多入口按钮（前往我的工作、查看运行中的智能体、查看已归档），视觉重心分散，反而破坏了清零之后的平静感。",
          "本轮收敛为只保留核心状态：居中完成标志、「收件箱已清空」标题、刚清空那一条的轻量内联撤销（支持按 Z），以及有延后通知时的一行极淡摘要。",
          "请确认最终截图和文案在浅色/深色模式下都足够克制，无冗余边框或多余操作。",
        ],
        en: [
          "The Inbox Zero screen previously stacked too many navigation buttons (My work, working agents, View archived), distracting from the calm relief of clearing the queue.",
          "Trimmed down to the core Inbox-owned state: subtle check tile, cleared heading, quiet inline Undo (Z) for the last cleared item, and a single muted line for snoozed items if any exist.",
          "Please verify light and dark mode contrast and confirm no duplicate subtitles remain.",
        ],
      },
      subIssues: [
        { id: "MUL-9849", done: true, title: { zh: "移除多余的导航 CTA 和快捷键提示", en: "Remove redundant navigation CTAs and shortcut hints" } },
      ],
      comments: [
        {
          id: "c1",
          author: ACTORS.alex,
          time: { zh: "42 分钟前", en: "42m ago" },
          body: {
            zh: "之前空状态底下那排按钮确实有点吵，特别是每次清完最后一条都会跳出来三个大按钮。",
            en: "Agreed—the row of three large buttons at the bottom felt noisy right after clearing the final item.",
          },
        },
        {
          id: "c2",
          author: ACTORS.lambda,
          time: { zh: "14 分钟前", en: "14m ago" },
          isTarget: true,
          body: {
            zh: "PR #11193 已提交并通过全部单测。内联撤销按钮已经改成和 InboxBar 一致的 ghost 样式，按 Z 可在 toast 消失后继续撤销最后一条。",
            en: "PR #11193 is up and all unit tests pass. Inline Undo now matches InboxBar's ghost button weight, and pressing Z continues to undo the last cleared row after the toast fades.",
          },
        },
      ],
    },
    {
      id: "mul-9822",
      identifier: "MUL-9822",
      title: {
        zh: "支持在 Inbox Zero 页面直接按 Z 撤销刚清空的通知",
        en: "Allow pressing Z on Inbox Zero to undo the last cleared notification",
      },
      project: { zh: "收件箱体验", en: "Inbox Experience" },
      status: "in_progress",
      priority: "medium",
      assignee: ACTORS.bohan,
      actor: ACTORS.bohan,
      reason: { zh: "Bohan 评论了", en: "Bohan commented" },
      timeAgo: { zh: "38 分钟", en: "38m" },
      unread: true,
      agentRunning: true,
      coldInCurrent: false,
      labels: { zh: ["快捷键"], en: ["Shortcuts"] },
      description: {
        zh: [
          "连续按 E 清完收件箱最后一条时，如果误按多了一下，toast 默认 10 秒后就会自动消失。此时页面上虽然还留着内联撤销按钮，但按 Z 却没反应。",
          "把单键快捷键保护逻辑抽成共享的 useInboxUndoShortcut hook，在 toast 存在时由 toast 接管 Z，toast 消失后无缝交给 Inbox Zero 内联撤销接管。",
          "同时要保证在搜索框、评论编辑器或任意可编辑节点内输入字母 z 时绝对不会误触发撤销。",
        ],
        en: [
          "When rapidly pressing E through the end of the Inbox, an accidental extra press lands on Inbox Zero. Once the 10s toast expires, the inline Undo button stays visible but pressing Z stopped working.",
          "Shared the single-key shortcut guard in useInboxUndoShortcut so an active toast owns Z first, and Inbox Zero seamlessly takes over once the toast dismisses.",
          "Verified editable targets, IME composition, and command bar inputs remain completely unaffected.",
        ],
      },
      subIssues: [],
      comments: [
        {
          id: "c1",
          author: ACTORS.cursor,
          time: { zh: "1 小时前", en: "1h ago" },
          body: {
            zh: "已检查所有单键监听（E / H / Shift+E / Z），现在都走同一套 isInboxShortcut 守卫，避免了中文输入法候选态误触。",
            en: "Audited all single-key listeners (E / H / Shift+E / Z); they now share isInboxShortcut including IME composition guards.",
          },
        },
        {
          id: "c2",
          author: ACTORS.bohan,
          time: { zh: "38 分钟前", en: "38m ago" },
          isTarget: true,
          body: {
            zh: "刚测了连续按 E 到最后一条再按 Z：现在能直接把任务恢复并跳回原任务页，但如果页面能给一个反向落回的微动效就更直觉了。",
            en: "Just tested pressing E to the end and hitting Z: it restores and reopens the issue right away. A subtle reverse motion on undo would make the spatial return feel even clearer.",
          },
        },
      ],
    },
    {
      id: "mul-9806",
      identifier: "MUL-9806",
      title: {
        zh: "Settled InboxBar 展示触发自动归档的具体操作原因",
        en: "Name the action that triggered auto-archive in the settled InboxBar",
      },
      project: { zh: "收件箱体验", en: "Inbox Experience" },
      status: "in_review",
      priority: "high",
      assignee: ACTORS.alex,
      actor: ACTORS.alex,
      reason: { zh: "Alex 请求审查", en: "Alex requested review" },
      timeAgo: { zh: "1 小时", en: "1h" },
      unread: true,
      coldInCurrent: true,
      labels: { zh: ["自动归档", "文案"], en: ["Auto-archive", "Copy"] },
      description: {
        zh: [
          "（注意：这是列表第 4 条——在「现状 A」下超出前 3 条预取上限，切到这里会触发真实的冷加载骨架屏 + 40px 跳动！）",
          "当用户在 InboxBar 修改状态、改派负责人或触发快捷指令并自动归档后，原本 settled bar 只显示干巴巴的「通知已归档」，容易让用户不确定刚刚的操作是否生效。",
          "现在会明确显示「已改为已完成 · 通知已归档」或「已改派给 Emacs · 通知已归档」，并在同位置保留属性操作与撤销/下一条入口。",
        ],
        en: [
          "(Note: This is item #4—beyond the current 3-item unread prefetch cap, so Direction A faithfully reproduces the cold-cache Skeleton + 40px jump here!)",
          "When an action in InboxBar auto-archives a notification, the settled bar previously only said 'Notification archived', leaving users unsure whether their status or assignee write took effect.",
          "It now names the exact action ('Changed to Done · Notification archived') while keeping issue controls, Undo, and Next in the same 40px slot.",
        ],
      },
      subIssues: [
        { id: "MUL-9807", done: true, title: { zh: "记录 InboxBar 发起的状态/负责人/快捷操作", en: "Track status, assignee, and quick-action causes from InboxBar" } },
        { id: "MUL-9808", done: true, title: { zh: "覆盖 8 个语言包的 settled 文案", en: "Add settled copy across all 8 locale bundles" } },
      ],
      comments: [
        {
          id: "c1",
          author: ACTORS.emacs,
          time: { zh: "2 小时前", en: "2h ago" },
          body: {
            zh: "这里有个边界情况：如果指派智能体弹了二次确认框但用户点了取消，reflects() 会对比实际 issue 状态，避免误显示「已改派」。",
            en: "Edge case covered: if assigning an agent opens a confirmation dialog that gets dismissed, reflects() compares against actual issue state so we never show a stale cause.",
          },
        },
        {
          id: "c2",
          author: ACTORS.alex,
          time: { zh: "1 小时前", en: "1h ago" },
          isTarget: true,
          body: {
            zh: "在 `after_auto_archive = next` 模式下，因为一回来就切走了，这条「已改为已完成」的反馈用户根本来不及看。如果配合一个 1.4s 缓冲进度条，这个文案的价值就完全体现出来了。",
            en: "Under `after_auto_archive = next`, the instant jump meant users never even saw the 'Changed to Done' confirmation. Pairing it with a short 1.4s buffer bar makes that confirmation genuinely useful.",
          },
        },
      ],
    },
    {
      id: "mul-9790",
      identifier: "MUL-9790",
      title: {
        zh: "将任务页顶部通知操作收敛到标题栏下方的统一 InboxBar",
        en: "Consolidate notification actions into a unified 40px InboxBar under the header",
      },
      project: { zh: "工作区壳层", en: "Workspace Shell" },
      status: "todo",
      priority: "medium",
      assignee: ACTORS.cursor,
      actor: ACTORS.jiayuan,
      reason: { zh: "Jiayuan 评论了（已读条目）", en: "Jiayuan commented (already read)" },
      timeAgo: { zh: "3 小时", en: "3h" },
      unread: false,
      coldInCurrent: true,
      labels: { zh: ["架构"], en: ["Architecture"] },
      description: {
        zh: [
          "（注意：这是已读条目——线上 `useInboxIssuePrefetch` 只预取 `read !== true` 的行，所以切到已读通知时同样会冷加载闪骨架屏。）",
          "把原本分散在顶部面包屑右侧的归档/延后按钮、以及正文上方的通知提示条统一收敛为 40px 高的 InboxBar，让「为什么收到这条通知」和「怎么处理它」在同一条视觉基线上。",
          "正因为 InboxBar 固定占 40px，任何在路由切换瞬间卸载它、或骨架屏里漏掉它的行为，都会导致整页正文上下猛跳 40px。",
        ],
        en: [
          "(Note: This row is already read—today's `useInboxIssuePrefetch` skips read rows, so auto-advancing here also hits a cold skeleton flash in Direction A.)",
          "Consolidated archive/snooze buttons and notification context into a single 40px InboxBar right under the breadcrumb header so context and actions sit on one baseline.",
          "Because InboxBar owns a fixed 40px strip, unmounting it mid-navigation or omitting it from IssueDetailSkeleton immediately produces a jarring 40px vertical snap.",
        ],
      },
      subIssues: [],
      comments: [
        {
          id: "c1",
          author: ACTORS.jiayuan,
          time: { zh: "3 小时前", en: "3h ago" },
          isTarget: true,
          body: {
            zh: "InboxBar 收拢之后整条任务页干净很多，现在唯一扎眼的就是切下一条时这条 bar 会闪没一下再蹦出来。",
            en: "The unified InboxBar cleaned up the issue header nicely—the only eyesore left is watching the bar disappear and pop back in when moving to the next item.",
          },
        },
      ],
    },
    {
      id: "mul-9788",
      identifier: "MUL-9788",
      title: {
        zh: "完成任务或触发智能体运行时默认自动归档收件箱通知",
        en: "Default auto-archive to completing an issue or triggering an agent",
      },
      project: { zh: "收件箱体验", en: "Inbox Experience" },
      status: "in_review",
      priority: "low",
      assignee: ACTORS.emacs,
      actor: ACTORS.emacs,
      reason: { zh: "Emacs 完成了智能体运行", en: "Emacs completed a run" },
      timeAgo: { zh: "5 小时", en: "5h" },
      unread: true,
      coldInCurrent: true,
      labels: { zh: ["自动归档"], en: ["Auto-archive"] },
      description: {
        zh: [
          "把默认自动归档规则设为「完成任务或触发智能体运行时自动归档」，纯整理类操作（改标签、改优先级、不唤醒智能体的普通评论）留在原地。",
          "这是列表的最后一条通知——归档或标记完成后，可以直接体验进入「收件箱已清空（Inbox Zero）」界面的完整收尾节奏，并按 Z 撤销回来。",
        ],
        en: [
          "Defaulted auto-archive to completing an issue or triggering an agent run, leaving tidy-up edits (labels, priority, plain comments) in place.",
          "This is the final notification in the queue—archiving or completing it transitions cleanly into the calm Inbox Zero screen, where pressing Z brings it right back.",
        ],
      },
      subIssues: [],
      comments: [
        {
          id: "c1",
          author: ACTORS.emacs,
          time: { zh: "5 小时前", en: "5h ago" },
          isTarget: true,
          body: {
            zh: "最后一条通知处理完后进入 Inbox Zero，内联撤销和 MulticaBleedMark 动画都已就位。可以试试在这里点「归档 (E)」或「标记完成」。",
            en: "Clearing this final item lands on Inbox Zero with the bleed mark and inline Undo ready. Try clicking Archive (E) or Mark Done here.",
          },
        },
      ],
    },
  ];
}
