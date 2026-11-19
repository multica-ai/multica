/** Edge and elevation choices for the issue sidebar cards (MUL-9592). */

export const BORDERS = {
  none: { label: "无描边", css: null },
  border: { label: "--border", css: "var(--border)" },
  surface: { label: "--surface-border", css: "var(--surface-border)" },
} as const;

export const SHADOWS = {
  none: { label: "无阴影", css: null },
  surface: { label: "--surface-shadow", css: "var(--surface-shadow)" },
  soft: {
    label: "柔和投影",
    css: "0 1px 3px color-mix(in oklab, var(--foreground) 7%, transparent), 0 6px 16px -6px color-mix(in oklab, var(--foreground) 8%, transparent)",
  },
  menu: { label: "--menu-shadow", css: "var(--menu-shadow)" },
} as const;

export const FILLS = {
  surface: { label: "--surface", css: "var(--surface)" },
  muted: { label: "--muted", css: "var(--muted)" },
} as const;

export const RADII = [6, 8, 10, 12] as const;

export type BorderId = keyof typeof BORDERS;
export type ShadowId = keyof typeof SHADOWS;
export type FillId = keyof typeof FILLS;
export type Radius = (typeof RADII)[number];

export interface Treatment {
  border: BorderId;
  shadow: ShadowId;
  fill: FillId;
  radius: Radius;
}

export interface Preset extends Treatment {
  id: string;
  letter: string;
  name: string;
  note: string;
  recommended?: boolean;
}

export const PRESETS: Preset[] = [
  {
    id: "hairline",
    letter: "A",
    name: "细描边，无阴影",
    note: "只有 1px --border。最平，卡片像印在画布上，浅色模式下和画布几乎分不开。",
    border: "border", shadow: "none", fill: "surface", radius: 8,
  },
  {
    id: "linear",
    letter: "B",
    name: "细描边 + 贴地阴影",
    note: "1px --border 加 --surface-shadow（1–2px 的淡投影）。和 Linear 截图量出来的一致：边框 #EFEFEF 左右，阴影只在下沿。",
    border: "border", shadow: "surface", fill: "surface", radius: 8, recommended: true,
  },
  {
    id: "shadow-only",
    letter: "C",
    name: "只有阴影",
    note: "不画描边，靠柔和投影分出卡片。浅色好看，深色模式下投影几乎看不见，卡片边缘会糊。",
    border: "none", shadow: "soft", fill: "surface", radius: 10,
  },
  {
    id: "raised",
    letter: "D",
    name: "浮起",
    note: "描边加菜单阴影（--menu-shadow）。卡片像弹出层，比正文的评论卡片更抢眼。",
    border: "border", shadow: "menu", fill: "surface", radius: 10,
  },
  {
    id: "first-draft",
    letter: "E",
    name: "深描边（第一版 mock）",
    note: "--surface-border 比评论卡片的边框深一档，再加 12px 圆角，卡片显得重。",
    border: "surface", shadow: "surface", fill: "surface", radius: 12,
  },
  {
    id: "tinted",
    letter: "F",
    name: "灰底，无描边",
    note: "不画边也不加阴影，卡片用 --muted 底色和画布区分。像分组列表，不像浮在画布上。",
    border: "none", shadow: "none", fill: "muted", radius: 8,
  },
];

export function cardStyle(t: Treatment) {
  const border = BORDERS[t.border].css;
  return {
    background: FILLS[t.fill].css,
    border: `1px solid ${border ?? "transparent"}`,
    boxShadow: SHADOWS[t.shadow].css ?? "none",
    borderRadius: t.radius,
  };
}

export function describe(t: Treatment): string {
  return [
    BORDERS[t.border].css ? `1px ${BORDERS[t.border].label}` : BORDERS[t.border].label,
    SHADOWS[t.shadow].label,
    t.fill === "surface" ? null : `底色 ${FILLS[t.fill].label}`,
    `圆角 ${t.radius}px`,
  ].filter(Boolean).join(" · ");
}
