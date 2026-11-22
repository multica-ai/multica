import type { Lang } from "../voice-input/copy";
import type { MockImageKind } from "./mock-images";

export type IssueStatus = "in_review" | "in_progress" | "todo" | "done";
export type IssuePriority = "urgent" | "high" | "medium" | "low";

export interface DemoActor {
  name: string;
  initials: string;
  isAgent?: boolean;
}

export interface DemoCommentImage {
  kind: MockImageKind;
  caption: Record<Lang, string>;
  /** Extra decode delay relative to base network latency. */
  delayMultiplier: number;
}

export interface DemoComment {
  id: string;
  author: DemoActor;
  time: Record<Lang, string>;
  body: Record<Lang, string[]>;
  code?: string;
  runBadge?: Record<Lang, string>;
  image?: DemoCommentImage;
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

/**
 * Generates realistic multi-comment history (12–16 comments per issue, ~3,000–4,500px tall)
 * with embedded screenshots placed BOTH above and inside the target comment so scroll
 * landing, async image CLS, and network request latency behave like real Multica issues.
 */
function buildLongTimeline(opts: {
  issueTag: string;
  agent: DemoActor;
  reviewer: DemoActor;
  leadBodyZh: string;
  leadBodyEn: string;
  targetBodyZh: string;
  targetBodyEn: string;
  targetTime: Record<Lang, string>;
  targetAuthor: DemoActor;
  followUpZh?: string;
  followUpEn?: string;
  images: [MockImageKind, MockImageKind, MockImageKind];
  totalBeforeTarget: number;
}): DemoComment[] {
  const out: DemoComment[] = [];
  const { issueTag, agent, reviewer, images, totalBeforeTarget } = opts;

  for (let i = 1; i <= totalBeforeTarget; i++) {
    const isAgentTurn = i % 2 === 1;
    const author = isAgentTurn ? agent : i % 4 === 0 ? ACTORS.jiayuan : reviewer;
    const minsAgo = (totalBeforeTarget - i + 2) * 14;
    const time = {
      zh: minsAgo >= 60 ? `${Math.floor(minsAgo / 60)} 小时前` : `${minsAgo} 分钟前`,
      en: minsAgo >= 60 ? `${Math.floor(minsAgo / 60)}h ago` : `${minsAgo}m ago`,
    };

    if (i === 1) {
      out.push({
        id: `${issueTag}-c${i}`,
        author: agent,
        time,
        runBadge: { zh: "运行 48s · 修改 5 个文件", en: "Ran 48s · 5 files" },
        body: {
          zh: [opts.leadBodyZh, "先贴一下本地复现的火焰图和关键指标，下面逐项拆解影响路径与边界条件。"],
          en: [opts.leadBodyEn, "Attaching the local trace and key latency metrics below before breaking down the edge cases."],
        },
        image: {
          kind: images[0],
          caption: { zh: `图 1 · ${issueTag} 复现采集与耗时分布`, en: `Fig 1 · ${issueTag} reproduction trace` },
          delayMultiplier: 0.9,
        },
      });
      continue;
    }

    if (i === Math.floor(totalBeforeTarget * 0.45)) {
      out.push({
        id: `${issueTag}-c${i}`,
        author: agent,
        time,
        runBadge: { zh: "运行 1m 12s · 新增 6 条单测", en: "Ran 1m 12s · 6 tests added" },
        body: {
          zh: [
            "补了一轮真机对比截图，重点观察 40px InboxBar 槽位在切页间隙的变化以及长评论页里的滚动锚点表现。",
            "同时检查了 React Query 缓存失效链路：在 `inbox:batch-archived` 到达后，旧列表引用会被重新计算一次，需要避免触发二次卸载。",
          ],
          en: [
            "Captured a before/after UI comparison focused on the 40px InboxBar slot during route transitions and deep-thread scroll alignment.",
            "Also audited the TanStack Query invalidation path after `inbox:batch-archived` to make sure cache patches never trigger a redundant unmount.",
          ],
        },
        code: `// ${issueTag}: keep next + next+1 warm around openKey\nconst targets = [nextInboxRow(rows, openKey), nextInboxRow(rows, nextKey)].filter(Boolean);\nawait Promise.all(targets.map((row) => prefetchIssuePage(qc, wsId, row.issue_id)));`,
        image: {
          kind: images[1],
          caption: { zh: `图 2 · ${issueTag} 切页前后布局对比截图`, en: `Fig 2 · ${issueTag} layout comparison` },
          delayMultiplier: 1.45,
        },
      });
      continue;
    }

    if (i === totalBeforeTarget - 2) {
      out.push({
        id: `${issueTag}-c${i}`,
        author: agent,
        time,
        runBadge: { zh: "运行 34s · E2E 全绿", en: "Ran 34s · E2E green" },
        body: {
          zh: [
            "在弱网（250ms / 600ms）和带 3 张大图的长讨论串上跑了 50 轮回归测试，确认没有出现主线程长任务阻塞，也没有回退到顶部的二次跳动。",
            "附上最新一轮 Playwright 采样和网络瀑布图，方便评审确认。",
          ],
          en: [
            "Ran 50 regression rounds under 250ms and 600ms throttled networks on long multi-image threads—zero main-thread stalls and zero top-scroll regressions.",
            "Attached the latest Playwright verification output and waterfall.",
          ],
        },
        image: {
          kind: images[2],
          caption: { zh: `图 3 · ${issueTag} 弱网与多图回归快照`, en: `Fig 3 · ${issueTag} throttled multi-image run` },
          delayMultiplier: 1.9,
        },
      });
      continue;
    }

    out.push({
      id: `${issueTag}-c${i}`,
      author,
      time,
      runBadge: isAgentTurn ? { zh: `运行 ${18 + i * 3}s`, en: `Ran ${18 + i * 3}s` } : undefined,
      body: {
        zh: isAgentTurn
          ? [
              `第 ${i} 轮排查更新：核对了 \`${issueTag}\` 在桌面端 Tab 切换、Web 端 Next.js 异步路由和侧边栏收起态下的表现。`,
              "发现在评论区包含多张未指定宽高比的图片时，即使首帧准确滚到了目标评论，图片解码展开后仍会把视口往下推 200–500px。",
            ]
          : [
              `看了第 ${i - 1} 楼的结果，方向是对的。再确认一下快速连按 E 超过预取窗口时的降级体验——不能一连按就满屏闪骨架屏。`,
            ],
        en: isAgentTurn
          ? [
              `Pass #${i} update on \`${issueTag}\`: verified desktop tabs, Next.js async navigation on web, and collapsed sidebar states.`,
              "Confirmed that unreserved images above the target comment still push the viewport down by 200–500px once decoded, even when initial scroll landing is exact.",
            ]
          : [
              `Reviewed comment #${i - 1}—looks solid. Double-check the fallback when someone mashes E faster than prefetch so it degrades calmly instead of flashing blank skeletons.`,
            ],
      },
    });
  }

  // Deep notification target comment (sits ~2,800–4,200px down!)
  out.push({
    id: `${issueTag}-target`,
    author: opts.targetAuthor,
    time: opts.targetTime,
    isTarget: true,
    body: {
      zh: [opts.targetBodyZh],
      en: [opts.targetBodyEn],
    },
    image: {
      kind: images[1],
      caption: { zh: `附图 · ${issueTag} 通知目标评论内嵌截图`, en: `Attachment · ${issueTag} target comment screenshot` },
      delayMultiplier: 1.15,
    },
  });

  if (opts.followUpZh && opts.followUpEn) {
    out.push({
      id: `${issueTag}-tail`,
      author: agent,
      time: { zh: "1 分钟前", en: "1m ago" },
      body: {
        zh: [opts.followUpZh],
        en: [opts.followUpEn],
      },
    });
  }

  return out;
}

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
      reason: { zh: "Jiayuan 提及了你 · 14 条评论", en: "Jiayuan mentioned you · 14 comments" },
      timeAgo: { zh: "刚刚", en: "just now" },
      unread: true,
      coldInCurrent: false,
      labels: { zh: ["交互细节", "收件箱"], en: ["Interaction", "Inbox"] },
      description: {
        zh: [
          "连续处理收件箱通知时，真实的任务往往包含十几条长评论、多张内嵌图片和 200–600ms 的接口请求耗时。当前实现把 40px InboxBar 塌陷、冷缓存骨架屏、侧栏闪白、顶部闪现再瞬移到评论、以及图片无占位展开导致的视口偏移全部叠在了一起。",
          "这版 Storybook 原型为每条任务构造了 12–16 条长评论（3,500px+ 高度）和 3 张异步加载图片，并在顶部提供 0ms / 250ms / 600ms 三档真实请求延迟与实时预热状态——既可以体验正常阅读节奏下的预取命中，也可以狂按 E 冲出预取窗口看真实加载态的表现。",
        ],
        en: [
          "Real Multica issues have 12–20+ long agent comments, multiple embedded screenshots, and 200–600ms network requests. Today's transition stacks a 40px InboxBar drop, cold Skeleton flash, right-sidebar blanking, top-of-page scroll teleport, and late image CLS.",
          "This upgraded Storybook workbench gives every issue 12–16 long comments (3,500px+ tall), 3 async-loading screenshots above the target comment, and selectable 0ms / 250ms / 600ms network latency with live anchor prefetch.",
        ],
      },
      subIssues: [
        { id: "MUL-9866", done: true, title: { zh: "排查 InboxBar 40px 跳动、预取盲区与图片偏移", en: "Audit 40px InboxBar shift, prefetch gaps, and image CLS" } },
        { id: "MUL-9867", done: false, title: { zh: "在 Storybook 还原多评论 + 图片 + 请求耗时场景", en: "Simulate long threads, images, and request latency in Storybook" } },
      ],
      comments: buildLongTimeline({
        issueTag: "MUL-9865",
        agent: ACTORS.emacs,
        reviewer: ACTORS.bohan,
        leadBodyZh: "已定位代码里叠在一起的 6 处跳动来源，其中在长讨论串里最扎眼的是「顶部闪一下 → 瞬移 3500px 到底部评论 → 上方图片晚 250ms 加载完又把评论顶出屏幕」。",
        leadBodyEn: "Traced 6 stacked glitches; on long threads the worst sequence is flashing top-of-page, teleporting 3,500px down, and then having late images above push the target comment back off-screen.",
        targetAuthor: ACTORS.jiayuan,
        targetTime: { zh: "2 分钟前", en: "2m ago" },
        targetBodyZh: "现在这个 Storybook 已经还原了真实长评论、异步大图和网络请求延迟：建议切到 A（现状）连按 E 感受 250ms/600ms 下的多段跳动与图片顶偏，再切到 B/C/D/E 对比稳态预取 + 宽高比占位 + 轻过渡的体感。",
        targetBodyEn: "This workbench now simulates long threads, async images, and real request latency: try A (Current) at 250ms/600ms to feel the multi-stage jump & image push, then compare B/C/D/E.",
        followUpZh: "提示：在 B/C/D/E 下稍微停 300ms 看顶部变成「下一条已预热」后按 E 是 0ms 秒切；如果连续狂按 E E E 超过预热速度，可以直接看到真实的加载降级态（顶栏不闪、40px Bar 锁稳、评论对齐后淡入）。",
        followUpEn: "Tip: Wait ~300ms for the green 'Next pre-warmed' pill to feel 0ms warm switches, or mash E-E-E rapidly to outrun prefetch and test the live loading fallback.",
        images: ["perf-chart", "ui-compare", "trace-waterfall"],
        totalBeforeTarget: 12,
      }),
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
      reason: { zh: "Lambda 运行已完成 · 13 条评论", en: "Lambda finished running · 13 comments" },
      timeAgo: { zh: "14 分钟", en: "14m" },
      unread: true,
      coldInCurrent: false,
      labels: { zh: ["UI 精修"], en: ["UI Polish"] },
      description: {
        zh: [
          "清完收件箱后的空状态之前堆了过多入口按钮，本轮收敛为只保留核心状态：完成标志、「收件箱已清空」标题、单行内联撤销（支持按 Z）和延后摘要。",
          "本条同样包含 12 条长讨论和 3 张设计对比截图，目标评论位于页面底部 ~3,400px 处。",
        ],
        en: [
          "Trimmed Inbox Zero down to the core state: check tile, cleared heading, quiet inline Undo (Z), and snoozed summary.",
          "Includes 12 long comments and 3 screenshots with the target comment ~3,400px down.",
        ],
      },
      subIssues: [
        { id: "MUL-9849", done: true, title: { zh: "移除多余的导航 CTA 和快捷键提示", en: "Remove redundant navigation CTAs and shortcut hints" } },
      ],
      comments: buildLongTimeline({
        issueTag: "MUL-9848",
        agent: ACTORS.lambda,
        reviewer: ACTORS.alex,
        leadBodyZh: "整理了浅色/深色模式下 Inbox Zero 的视觉重心，移除了与侧栏重复的三个快捷按钮。",
        leadBodyEn: "Audited Inbox Zero visual hierarchy in light and dark modes and removed the three redundant navigation buttons.",
        targetAuthor: ACTORS.lambda,
        targetTime: { zh: "14 分钟前", en: "14m ago" },
        targetBodyZh: "PR #11193 已合并。注意观察切到这一条时：在 A 模式下，上方 3 张截图延迟展开会把这条评论直接往下顶 480px；而在 B/C/D/E 下图片容器预留了宽高比，这条评论的位置 0px 偏移。",
        targetBodyEn: "PR #11193 merged. Notice how in Direction A the 3 screenshots above expand late and push this comment down by 480px, while B/C/D/E hold it at 0px shift.",
        images: ["ui-compare", "ci-matrix", "perf-chart"],
        totalBeforeTarget: 11,
      }),
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
      reason: { zh: "Bohan 评论了 · 15 条评论", en: "Bohan commented · 15 comments" },
      timeAgo: { zh: "38 分钟", en: "38m" },
      unread: true,
      agentRunning: true,
      coldInCurrent: false,
      labels: { zh: ["快捷键"], en: ["Shortcuts"] },
      description: {
        zh: [
          "把单键快捷键保护逻辑抽成共享的 `useInboxUndoShortcut` hook，在 toast 存在时由 toast 接管 `Z`，toast 消失后无缝交给 Inbox Zero 内联撤销接管。",
          "这条讨论串包含 15 条评论与多段代码评审，目标评论在倒数第 2 条。",
        ],
        en: [
          "Unified single-key shortcut guards into `useInboxUndoShortcut` so toast owns `Z` first and Inbox Zero seamlessly takes over afterward.",
          "15-comment engineering thread with target comment near the bottom.",
        ],
      },
      subIssues: [],
      comments: buildLongTimeline({
        issueTag: "MUL-9822",
        agent: ACTORS.cursor,
        reviewer: ACTORS.bohan,
        leadBodyZh: "核对了 E / H / Shift+E / Z 在中文 IME 组合输入、评论框、命令面板中的拦截边界。",
        leadBodyEn: "Audited E / H / Shift+E / Z guards across IME composition, editors, and command bar.",
        targetAuthor: ACTORS.bohan,
        targetTime: { zh: "38 分钟前", en: "38m ago" },
        targetBodyZh: "试着在这里点「标记完成」或「快捷发送 @Emacs 评论」：在 A 下等 250–600ms 网络回包后会突然抽走页面；在 D 下 InboxBar 立刻给 1.4s 细进度条缓冲，期间滚动一下正文就会自动停住。",
        targetBodyEn: "Try clicking Mark Done or Send @Emacs comment here: A yanks the page after network round-trip, while D settles InboxBar with a 1.4s cancellable progress line.",
        followUpZh: "补充：此时按 Z 撤销可以同时感受反向落回过渡与左侧行平滑展开。",
        followUpEn: "Also try pressing Z right after to feel the reverse motion and smooth row re-expansion.",
        images: ["trace-waterfall", "ci-matrix", "ui-compare"],
        totalBeforeTarget: 13,
      }),
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
      reason: { zh: "Alex 请求审查 · 第 4 条（现状冷缓存）", en: "Alex requested review · Item #4 (cold in Current)" },
      timeAgo: { zh: "1 小时", en: "1h" },
      unread: true,
      coldInCurrent: true,
      labels: { zh: ["自动归档", "冷缓存"], en: ["Auto-archive", "Cold cache"] },
      description: {
        zh: [
          "这是列表第 4 条——超出线上 `INBOX_IDLE_PREFETCH_LIMIT = 3` 的预取上限！在「A · 现状」下切到这里会完整经历：`InboxBar 闪消 (-40px) → 缺 bar 骨架屏 → Issue JSON 回来 (+40px + 侧栏闪白 + 停在顶部) → Timeline 回来瞬移 3,800px → 3 张图陆续顶偏 +480px`。",
          "在 B/C/D/E 下，当你阅读前一条时锚点预取已经在后台把这一条预热好了；即使你连按 E 冲出预热窗口，顶栏与 40px InboxBar 也会立刻用列表元数据填满不闪跳。",
        ],
        en: [
          "Item #4—beyond today's 3-item unread prefetch cap! In Direction A you feel the full cascade: -40px bar drop → barless skeleton → +40px jump at scrollTop=0 → 3,800px teleport → +480px image CLS.",
          "In B/C/D/E anchor prefetch warms this row while you read #3; even if you mash E past prefetch, top chrome + 40px InboxBar populate immediately from list metadata.",
        ],
      },
      subIssues: [
        { id: "MUL-9807", done: true, title: { zh: "记录 InboxBar 发起的状态/负责人/快捷操作", en: "Track status, assignee, and quick-action causes from InboxBar" } },
        { id: "MUL-9808", done: true, title: { zh: "覆盖 8 个语言包的 settled 文案", en: "Add settled copy across all 8 locale bundles" } },
      ],
      comments: buildLongTimeline({
        issueTag: "MUL-9806",
        agent: ACTORS.emacs,
        reviewer: ACTORS.alex,
        leadBodyZh: "实现了 InboxBar 归档归因文案：区分「已改为已完成」「已改派给某人」「已运行快捷操作」三种来源。",
        leadBodyEn: "Implemented action-specific settled copy in InboxBar for status, assignee, and quick-action causes.",
        targetAuthor: ACTORS.alex,
        targetTime: { zh: "1 小时前", en: "1h ago" },
        targetBodyZh: "对比一下把网络延迟切到 600ms 后快速连按 E 到这一条：A 会连续跳 4 次，而 B/C/D/E 整个顶部框架纹丝不动，数据回来直接定位到这条评论平滑呈现。",
        targetBodyEn: "Switch latency to 600ms and rapidly press E to reach this item: A jumps 4 times, while B/C/D/E keep the entire top frame locked and fade in aligned right on this comment.",
        images: ["perf-chart", "trace-waterfall", "ci-matrix"],
        totalBeforeTarget: 14,
      }),
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
      reason: { zh: "Jiayuan 评论了（已读 · 现状不预取）", en: "Jiayuan commented (read · skipped by Current)" },
      timeAgo: { zh: "3 小时", en: "3h" },
      unread: false,
      coldInCurrent: true,
      labels: { zh: ["架构", "已读条目"], en: ["Architecture", "Read row"] },
      description: {
        zh: [
          "这是一条**已读**通知——线上 `useInboxIssuePrefetch` 只预取 `read !== true` 的行，所以切到已读条目每次都会冷加载。",
          "在 B/C/D/E 的锚点预取下，无论已读还是未读，只要它在当前条目的下一位就会被提前预热。",
        ],
        en: [
          "This row is already **read**—today's `useInboxIssuePrefetch` skips read rows, causing a cold skeleton every time.",
          "With anchor prefetch in B/C/D/E, the next row is warmed regardless of read status.",
        ],
      },
      subIssues: [],
      comments: buildLongTimeline({
        issueTag: "MUL-9790",
        agent: ACTORS.cursor,
        reviewer: ACTORS.jiayuan,
        leadBodyZh: "将分散在标题栏右侧和正文上方的通知按钮统一收敛到 40px 的 InboxBar。",
        leadBodyEn: "Consolidated notification actions into a single 40px InboxBar under the breadcrumb header.",
        targetAuthor: ACTORS.jiayuan,
        targetTime: { zh: "3 小时前", en: "3h ago" },
        targetBodyZh: "InboxBar 收拢之后整条任务页干净很多，现在最重要的是把切下一条时这 40px bar 的闪烁、冷加载骨架屏和长评论多图跳动一起治干净。",
        targetBodyEn: "The unified InboxBar looks clean—now we just need to eliminate the 40px flicker, cold skeleton jump, and multi-image scroll shift when advancing.",
        images: ["ui-compare", "perf-chart", "trace-waterfall"],
        totalBeforeTarget: 11,
      }),
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
      reason: { zh: "Emacs 完成了运行 · 最后一条", en: "Emacs finished run · Final item" },
      timeAgo: { zh: "5 小时", en: "5h" },
      unread: true,
      coldInCurrent: true,
      labels: { zh: ["自动归档"], en: ["Auto-archive"] },
      description: {
        zh: [
          "列表最后一条通知：处理完之后进入 Inbox Zero 清空状态，可以按 `Z` 撤销直接回到这条长讨论串的原评论位置。",
        ],
        en: [
          "Final item in the queue: clearing it transitions to Inbox Zero, and pressing `Z` returns right back to this thread's target comment.",
        ],
      },
      subIssues: [],
      comments: buildLongTimeline({
        issueTag: "MUL-9788",
        agent: ACTORS.emacs,
        reviewer: ACTORS.bohan,
        leadBodyZh: "默认自动归档策略改为「完成任务或触发智能体运行」，纯字段整理留在收件箱。",
        leadBodyEn: "Defaulted auto-archive to issue completion or agent trigger, keeping tidy-up edits in place.",
        targetAuthor: ACTORS.emacs,
        targetTime: { zh: "5 小时前", en: "5h ago" },
        targetBodyZh: "这是最后一条通知：归档或标记完成后直接进入 Inbox Zero；按 Z 撤销可以体验从空状态返回长评论页的过渡。",
        targetBodyEn: "Last item in the queue: archive or mark Done to reach Inbox Zero, then press Z to undo back into this long thread.",
        images: ["ci-matrix", "ui-compare", "perf-chart"],
        totalBeforeTarget: 10,
      }),
    },
  ];
}
