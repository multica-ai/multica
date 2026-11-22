import type { Lang } from "../voice-input/copy";

export type DirectionId = "current" | "crossfade" | "glide" | "buffer" | "card-stack";

export interface TransitionDirection {
  id: DirectionId;
  letter: string;
  recommended?: boolean;
  name: Record<Lang, string>;
  shortName: Record<Lang, string>;
  pitch: Record<Lang, string>;
  bestFor: Record<Lang, string>;
  tradeoff: Record<Lang, string>;
  mechanics: Record<Lang, string[]>;
}

export const DIRECTIONS: TransitionDirection[] = [
  {
    id: "current",
    letter: "A",
    name: { zh: "线上现状（基准对照）", en: "Current (baseline)" },
    shortName: { zh: "现状", en: "Current" },
    pitch: {
      zh: "完整还原线上 5 处跳动：InboxBar 闪消 40px、第 4 条与已读条冷加载骨架屏、侧栏闪白、先闪顶部再瞬移到评论、自动归档 380ms 后无预告抽走页面。",
      en: "Reproduces all 5 production glitches: 40px InboxBar drop, cold Skeleton on item #4+ and read rows, right sidebar blanking, top-to-comment teleport, and unbuffered 380ms auto-archive yank.",
    },
    bestFor: { zh: "对照体验问题真因", en: "Baseline for comparing fixes" },
    tradeoff: { zh: "每次切页都有多段跳动与失控感", en: "Multi-stage layout jumps and rug-pull feel" },
    mechanics: {
      zh: [
        "归档前 leaveRow 同步卸载 InboxBar → 正文上跳 40px",
        "第 4 条起（及已读条）未预取 → 闪无 InboxBar 的骨架屏",
        "landingChromeHeld 期间右侧栏闪白，主区先渲染 scrollTop=0 再瞬移到评论",
        "自动归档等网络回包 ~380ms 后无预告直接抽走当前页",
      ],
      en: [
        "leaveRow unmounts InboxBar before navigation → 40px upward snap",
        "Item #4+ and read rows miss prefetch → Skeleton without InboxBar",
        "Right sidebar blanks during landingChromeHeld; body flashes scrollTop=0 then teleports",
        "Auto-archive yanks the page ~380ms after action with zero preview",
      ],
    },
  },
  {
    id: "crossfade",
    letter: "B",
    name: { zh: "稳态底座 + 纯淡入淡出", en: "Steady chrome + pure crossfade" },
    shortName: { zh: "纯淡入淡出", en: "Crossfade" },
    pitch: {
      zh: "修掉所有 40px 跳动、预取盲区、侧栏闪白和滚动瞬移；内容区仅做 130ms 零位移淡入淡出。用来验证「只要足够稳，是否根本不需要滑动」。",
      en: "Fixes every layout shift, prefetch miss, sidebar flicker, and scroll teleport; content does a 130ms zero-translation fade. Tests whether pure stability is enough without slide motion.",
    },
    bestFor: { zh: "追求绝对克制、零晕动、极速连按", en: "Maximum restraint, zero motion, rapid triage" },
    tradeoff: { zh: "方向感偏弱；自动归档后仍然立刻切走", en: "Less directional cue; auto-archive still leaves right away" },
    mechanics: {
      zh: [
        "锚点预取下一条 & 下下一条（含已读），锁定 40px InboxBar 与右侧栏不闪",
        "目标评论在淡入前已精确对齐视口，彻底消灭「闪顶部 → 瞬移」",
        "左侧列表行 160ms 平滑收拢高度；右侧正文 130ms 纯 opacity 过渡（0px 位移）",
        "手动与自动归档均立即切换",
      ],
      en: [
        "Anchor-prefetches next + next+1 (including read rows); locks 40px InboxBar and sidebar",
        "Target comment aligns before reveal—zero top-of-page flash",
        "Left row collapses smoothly in 160ms; body uses pure 130ms opacity fade (0px shift)",
        "Both manual and auto-archive advance immediately",
      ],
    },
  },
  {
    id: "glide",
    letter: "C",
    name: { zh: "稳态底座 + 轻方向滑入", en: "Steady chrome + directional glide" },
    shortName: { zh: "轻方向滑入", en: "Glide" },
    pitch: {
      zh: "在稳态底座上叠加 12px 轻垂直滑入（160ms），与左侧列表「下一条在下方」的方向隐喻一致；按 Z 撤销时从上方反向轻落回。",
      en: "Adds a restrained 12px vertical glide (160ms) matching the sidebar's top-to-bottom progression; pressing Z to undo glides cleanly back down from above.",
    },
    bestFor: { zh: "既有明确「推进到下一条」的方向感，又保持 Linear 级干脆", en: "Clear forward momentum while staying Linear-fast" },
    tradeoff: { zh: "触发自动归档（点完成 / @Agent 评论）时仍会立刻离开当前页", en: "Auto-archive actions still leave the page immediately" },
    mechanics: {
      zh: [
        "外框（BreadcrumbHeader / 40px InboxBar / 右侧栏）纹丝不动",
        "左侧归档行 160ms 平滑收拢；右侧正文从下方 +12px 轻滑淡入（160ms）",
        "按 Z 撤销时左侧行平滑展开，右侧正文从上方 -12px 镜像落回",
        "连续快速按 E 立即打断，零排队拖沓",
      ],
      en: [
        "Outer chrome (BreadcrumbHeader / 40px InboxBar / sidebar) stays anchored",
        "Left row collapses in 160ms; body glides up +12px → 0 (160ms)",
        "Undo (Z) expands the row back and glides the issue down from -12px",
        "Rapid E presses interrupt cleanly with zero queue buildup",
      ],
    },
  },
  {
    id: "buffer",
    letter: "D",
    recommended: true,
    name: { zh: "手动快切 + 自动归档缓冲条", en: "Instant manual + auto-archive buffer" },
    shortName: { zh: "缓冲条（推荐）", en: "Buffer (Rec)" },
    pitch: {
      zh: "把「手动归档 (E/H)」和「任务操作触发自动归档（完成 / @智能体）」拆开：手动 0 等待走 C；自动归档在 InboxBar 给 1.4s 细进度条缓冲，滚动或点击即停住，按 E 立刻切走。",
      en: "Splits explicit triage (E/H) from action-triggered auto-archive (Done / @agent): manual advances immediately via C; auto-archive shows a 1.4s 2px progress line in InboxBar that pauses on any interaction or skips on E.",
    },
    bestFor: { zh: "兼顾 E-E-E 极速清收件箱与「确认改完状态/发出评论再走」的掌控感", en: "Fast E-E-E triage plus calm confirmation after Done or @agent comment" },
    tradeoff: { zh: "自动归档后多停 1.4s（但随时按 E 可跳过）", en: "1.4s pause on auto-archive (skippable anytime with E)" },
    mechanics: {
      zh: [
        "按 E / H 手动处理：0 等待，走 C 的稳态 + 12px 轻方向滑入",
        "点「标记完成」、改派智能体或发 @Agent 评论：InboxBar 立刻变 settled 态并启动底部 2px 进度线（1.4s）",
        "缓冲期间滚动正文、点进页面、聚焦输入框或点「留在这里 / Esc」自动取消切页",
        "确认无误无需操作，1.4s 后平滑推进到下一条（或按 E 立即跳过）",
      ],
      en: [
        "Manual E / H: zero wait, uses Direction C's steady 12px directional glide",
        "Mark Done, assign agent, or send @agent comment: InboxBar settles immediately with a 1.4s 2px bottom progress bar",
        "Scrolling, clicking the body, focusing composer, or pressing Esc cancels auto-advance",
        "Otherwise glides smoothly to the next item after 1.4s (or press E to skip)",
      ],
    },
  },
  {
    id: "card-stack",
    letter: "E",
    name: { zh: "卡片叠层翻页", en: "Card stack turnover" },
    shortName: { zh: "卡片叠层", en: "Card stack" },
    pitch: {
      zh: "更强的物理卡片隐喻：旧任务整页轻微缩小上滑离场（scale 0.985 / y -18px），新任务从下方叠层推入（y +24px，220ms），类似 Superhuman / Apple Mail。",
      en: "Stronger physical deck metaphor: outgoing issue lifts and scales subtly back (0.985 / -18px) while the next sheet rises cleanly from underneath (+24px, 220ms).",
    },
    bestFor: { zh: "希望每次切换都有明确「收走一张、翻出下一张」的强空间感知", en: "Unmistakable card-filed-away spatial metaphor" },
    tradeoff: { zh: "大页面位移幅度较大，高频连续按 E 时视觉体量比 C/D 更重", en: "Larger displacement feels heavier during rapid E-E-E bursts" },
    mechanics: {
      zh: [
        "外框稳态不变，阅读区双层叠加呈现离场页与入场页",
        "旧页 190ms 轻缩上退，新页 220ms 从下方 24px 推入",
        "按 Z 撤销时方向反转（旧页下沉退场，撤销页从上方落回）",
        "自动归档采用同样卡片切换（可与 D 的缓冲条组合）",
      ],
      en: [
        "Outer chrome stays steady while leaving and entering sheets overlap briefly",
        "Outgoing sheet lifts -18px & scales to 0.985 (190ms); incoming rises from +24px (220ms)",
        "Undo (Z) reverses the deck cleanly",
        "Auto-archive uses the same card turnover (combinable with D's buffer)",
      ],
    },
  },
];

export const DIRECTION_BY_ID: Record<DirectionId, TransitionDirection> = Object.fromEntries(
  DIRECTIONS.map((d) => [d.id, d]),
) as Record<DirectionId, TransitionDirection>;
