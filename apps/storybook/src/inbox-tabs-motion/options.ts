import type { Lang } from "../voice-input/copy";

export type OptionId = "none" | "slide" | "inplace" | "direction";

export interface MotionOption {
  id: OptionId;
  letter: string;
  recommended?: boolean;
  name: Record<Lang, string>;
  pitch: Record<Lang, string>;
  mechanics: Record<Lang, string[]>;
  /** The header's open tab: a highlight that travels, or one per tab. */
  pill: "travel" | "fade" | "instant";
  /** The list under the header: held height and a fade, plus travel. */
  list: "instant" | "fade" | "direction";
}

export const OPTIONS: MotionOption[] = [
  {
    id: "none",
    letter: "0",
    name: { zh: "现状：无动效", en: "Today: no motion" },
    pitch: {
      zh: "已上线的版本。标签和列表都是瞬间切换。",
      en: "What shipped. Tabs and list swap instantly.",
    },
    mechanics: {
      zh: [
        "名字从一个标签瞬移到另一个，图标同时出现和消失",
        "已归档第一次打开要请求数据，加载期间列表高度变成 0，下面的区块先上跳、再被推回去",
      ],
      en: [
        "The name jumps to the other tab while the icons pop in and out",
        "Archived loads on first open: the list is 0px tall meanwhile, so the sections below jump up and back",
      ],
    },
    pill: "instant",
    list: "instant",
  },
  {
    id: "slide",
    letter: "A",
    recommended: true,
    name: { zh: "底块滑动 + 名字展开", en: "Sliding highlight + label unfold" },
    pitch: {
      zh: "选中底块从旧标签滑到新标签，名字在新位置展开，旧标签收回成图标。列表淡入，高度平滑过渡。",
      en: "The highlight slides to the new tab, its name unfolds there and the old tab folds back into an icon. The list fades in and its height eases.",
    },
    mechanics: {
      zh: [
        "名字和图标的宽度 200ms ease-out 展开或收起；底块也是 200ms，逐帧追着标签的实时位置走，和名字同时落定",
        "列表旧内容 100ms 淡出，新内容 150ms 淡入",
        "加载期间保持原有高度，数据到了再过渡到新高度，下面的区块不会来回跳",
      ],
      en: [
        "Label and icon widths unfold or fold over 200ms ease-out; the highlight takes the same 200ms, tracking the tab's live box each frame, and lands with the label",
        "Old rows fade out in 100ms, new rows fade in over 150ms",
        "The list keeps its height while loading and eases to the new one, so the sections below never bounce",
      ],
    },
    pill: "travel",
    list: "fade",
  },
  {
    id: "inplace",
    letter: "B",
    name: { zh: "原地展开", en: "Unfold in place" },
    pitch: {
      zh: "底块不移动，只在原地淡出淡入；名字在原地展开收起。列表保持现状。改动最小。",
      en: "The highlight fades out and in where it is, and the name unfolds in place. The list stays as today. Smallest change.",
    },
    mechanics: {
      zh: [
        "底色 150ms 交叉淡化，宽度 150ms 展开或收起",
        "列表仍然瞬间切换，加载期间的高度跳动也还在",
      ],
      en: [
        "Background crossfades and widths change over 150ms",
        "The list still swaps instantly, and the height bounce while loading remains",
      ],
    },
    pill: "fade",
    list: "instant",
  },
  {
    id: "direction",
    letter: "C",
    name: { zh: "A + 列表方向滑入", en: "A + directional list" },
    pitch: {
      zh: "在 A 的基础上，列表按标签顺序左右滑入：往右边的标签切，新列表从右边进来。",
      en: "A, plus the list slides in by tab order: switching to a tab on the right brings the new list in from the right.",
    },
    mechanics: {
      zh: [
        "旧列表向反方向滑出 16px 并淡出，新列表从切换方向滑入 16px",
        "空间感最强，但这个区块要来回切，横向位移在一列竖排的区块里容易显得忙",
      ],
      en: [
        "Old rows slide 16px away and fade; new rows slide in 16px from the side you switched toward",
        "The strongest sense of place, but horizontal travel inside a vertical stack of sections can feel busy when switched often",
      ],
    },
    pill: "travel",
    list: "direction",
  },
];

export const OPTION_BY_ID = Object.fromEntries(OPTIONS.map((o) => [o.id, o])) as Record<OptionId, MotionOption>;
