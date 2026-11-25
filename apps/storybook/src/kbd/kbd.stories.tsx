import { useState, type CSSProperties, type ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import {
  ArrowBigUp,
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ArrowRightToLine,
  ArrowUp,
  Command,
  CornerDownLeft,
  Delete,
  MessageSquare,
  Option,
  PanelLeft,
  PanelRight,
  Plus,
  Search,
  Space,
  type IconComponent,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import type { Lang } from "../voice-input/copy";
import { useStoryEnv } from "../voice-input/env";
import { OpticalKbdGlyph, type OpticalGlyphName } from "./kbd-glyphs";
import {
  chord,
  DemoLiteralKbd,
  DemoShortcutKeycaps,
  type DemoChord,
  type DemoPlatform,
  type DemoSize,
  type DemoVariant,
} from "./variants";

const meta = {
  title: "Kbd",
  id: "kbd",
  parameters: { layout: "fullscreen" },
} satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;

type VariantMeta = {
  id: DemoVariant;
  badge: string;
  name: Record<Lang, string>;
  summary: Record<Lang, string>;
};

const VARIANTS: VariantMeta[] = [
  {
    id: "current",
    badge: "A",
    name: { zh: "线上现状", en: "Current" },
    summary: {
      zh: "24px 通用图标硬缩 12px · Control 裸 Unicode「⌃」· ⌘ 排首位 · 固定高度不随 --font-scale 缩放",
      en: "24px UI icons shrunk to 12px · raw Unicode ⌃ · ⌘ first · fixed heights ignore --font-scale",
    },
  },
  {
    id: "quick-win",
    badge: "B",
    name: { zh: "Step 1 · Quick Win", en: "Step 1 · Quick Win" },
    summary: {
      zh: "修正 ⌃⌥⇧⌘ 顺序 · 开启 OpenType case · em 相对缩放 · 补 ⌃/⌦ 矢量字形 · Space 用文字",
      en: "Canonical ⌃⌥⇧⌘ order · OpenType case · em scaling · vector ⌃/⌦ · Space text",
    },
  },
  {
    id: "optical-pill",
    badge: "C",
    name: { zh: "Step 2 · 专用字形 + 单胶囊", en: "Step 2 · Optical glyphs + Pill" },
    summary: {
      zh: "16px 比例字宽专用键帽字形（对齐 Inter 500 笔宽与 cap-height）· 组合收进单胶囊",
      en: "16px proportional-width optical glyphs matched to Inter 500 · unified single pill",
    },
  },
  {
    id: "optical-split",
    badge: "C′",
    name: { zh: "Step 2 · 专用字形 + 拆键", en: "Step 2 · Optical glyphs + Split" },
    summary: {
      zh: "同一套光学字形，保留一键一帽（适合 Settings 快捷键录制页，或对比单胶囊 vs 拆键）",
      en: "Same optical glyphs in individual keycaps (for Settings recording or split comparison)",
    },
  },
];

const COMPARE_GROUPS: {
  title: Record<Lang, string>;
  items: { label: Record<Lang, string>; shortcut: DemoChord; note?: Record<Lang, string> }[];
}[] = [
  {
    title: { zh: "修饰键顺序与多键组合", en: "Modifier order & multi-modifier chords" },
    items: [
      { label: { zh: "打开搜索", en: "Open search" }, shortcut: chord("K", { primary: true }) },
      {
        label: { zh: "新建对话", en: "New chat" },
        shortcut: chord("J", { primary: true, shift: true }),
        note: { zh: "⌘⇧J → ⇧⌘J", en: "⌘⇧J → ⇧⌘J" },
      },
      {
        label: { zh: "切换全屏", en: "Full screen" },
        shortcut: chord("F", { control: true, primary: true }),
        note: { zh: "裸 Unicode ⌃ → 居中矢量", en: "Raw ⌃ → centered vector" },
      },
      {
        label: { zh: "全修饰键", en: "All modifiers" },
        shortcut: chord("S", { control: true, alt: true, shift: true, primary: true }),
        note: { zh: "⌃ ⌥ ⇧ ⌘ S 齐平检验", en: "⌃ ⌥ ⇧ ⌘ S alignment check" },
      },
      {
        label: { zh: "强制退出", en: "Force quit" },
        shortcut: chord("Escape", { alt: true, primary: true }),
      },
    ],
  },
  {
    title: { zh: "标点与符号键（OpenType case）", en: "Punctuation & symbol keys (OpenType case)" },
    items: [
      { label: { zh: "后退", en: "Go back" }, shortcut: chord("[", { primary: true }) },
      { label: { zh: "前进", en: "Go forward" }, shortcut: chord("]", { primary: true }) },
      { label: { zh: "右侧栏", en: "Right sidebar" }, shortcut: chord("/", { primary: true }) },
      { label: { zh: "设置", en: "Settings" }, shortcut: chord(",", { primary: true }) },
      { label: { zh: "放大", en: "Zoom in" }, shortcut: chord("Plus", { primary: true }) },
      { label: { zh: "缩小", en: "Zoom out" }, shortcut: chord("Minus", { primary: true }) },
    ],
  },
  {
    title: { zh: "功能键与方向键", en: "Action & navigation keys" },
    items: [
      { label: { zh: "发送", en: "Send" }, shortcut: chord("Enter", { primary: true }) },
      { label: { zh: "换行", en: "Newline" }, shortcut: chord("Enter", { shift: true }) },
      { label: { zh: "按词退格", en: "Delete word" }, shortcut: chord("Backspace", { alt: true }) },
      {
        label: { zh: "前向删除", en: "Forward delete" },
        shortcut: chord("Delete", { alt: true }),
        note: { zh: "区分 ⌫ 与 ⌦", en: "Distinguishes ⌫ vs ⌦" },
      },
      {
        label: { zh: "按住说话", en: "Push to talk" },
        shortcut: chord("Space", { alt: true }),
        note: { zh: "下沉的 ␣ → Space", en: "Low ␣ → Space" },
      },
      { label: { zh: "反向缩进", en: "Outdent" }, shortcut: chord("Tab", { shift: true }) },
      { label: { zh: "向上移动", en: "Move up" }, shortcut: chord("Up", { alt: true }) },
      { label: { zh: "跳至行首", en: "Line start" }, shortcut: chord("Left", { primary: true }) },
    ],
  },
];

function Segmented<T extends string | number>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: readonly { value: T; label: string }[];
  onChange: (value: T) => void;
}) {
  return (
    <div className="inline-flex items-center gap-2">
      <span className="text-caption text-muted-foreground">{label}</span>
      <div className="inline-flex rounded-md border border-border bg-card p-0.5">
        {options.map((opt) => (
          <button
            key={String(opt.value)}
            type="button"
            onClick={() => onChange(opt.value)}
            className={cn(
              "rounded-xs px-2 py-0.5 text-caption font-medium transition-colors",
              value === opt.value
                ? "bg-surface-selected text-foreground shadow-2xs"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {opt.label}
          </button>
        ))}
      </div>
    </div>
  );
}

function ComparePage() {
  const { lang } = useStoryEnv();
  const zh = lang === "zh";
  const [fontScale, setFontScale] = useState<number>(1);
  const [size, setSize] = useState<DemoSize>("sm");
  const [platform, setPlatform] = useState<DemoPlatform>("macos");
  const [zoom, setZoom] = useState<number>(1);

  return (
    <div
      className="min-h-screen bg-background p-10"
      style={{ "--font-scale": fontScale } as CSSProperties}
    >
      <div className="mx-auto max-w-[1280px]">
        <header className="flex flex-wrap items-end justify-between gap-4">
          <div className="max-w-2xl">
            <h1 className="text-title font-semibold">
              {zh ? "Kbd 光学平衡对比：现状 vs Step 1 vs Step 2" : "Kbd optical balance: Current vs Step 1 vs Step 2"}
            </h1>
            <p className="mt-1.5 text-body text-muted-foreground">
              {zh
                ? "试着切换 --font-scale 和 2× 放大镜：注意观察 ⌃ 高度、⇧ 胖瘦、Space 居中、⌘[ 基线，以及字号放大后图文是否脱节。"
                : "Toggle --font-scale and 2× loupe to inspect ⌃ vertical balance, ⇧ proportion, Space centering, ⌘[ baseline, and icon/text scaling."}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Segmented
              label="--font-scale"
              value={fontScale}
              onChange={setFontScale}
              options={[
                { value: 0.9, label: "0.9×" },
                { value: 1, label: "1.0×" },
                { value: 1.15, label: "1.15×" },
                { value: 1.3, label: "1.3×" },
              ]}
            />
            <Segmented
              label={zh ? "尺寸" : "Size"}
              value={size}
              onChange={setSize}
              options={[
                { value: "sm", label: "sm" },
                { value: "md", label: "md" },
              ]}
            />
            <Segmented
              label={zh ? "平台" : "Platform"}
              value={platform}
              onChange={setPlatform}
              options={[
                { value: "macos", label: "macOS" },
                { value: "windows", label: "Windows" },
              ]}
            />
            <Segmented
              label={zh ? "放大" : "Loupe"}
              value={zoom}
              onChange={setZoom}
              options={[
                { value: 1, label: "1×" },
                { value: 1.6, label: "1.6×" },
              ]}
            />
          </div>
        </header>

        <div className="mt-8 overflow-hidden rounded-xl border border-border bg-card shadow-xs">
          <div className="grid grid-cols-[200px_repeat(4,minmax(0,1fr))] border-b border-border bg-muted/40">
            <div className="px-4 py-3 text-caption font-medium text-muted-foreground">
              {zh ? "快捷键" : "Shortcut"}
            </div>
            {VARIANTS.map((v) => (
              <div key={v.id} className="border-l border-border px-4 py-3">
                <div className="flex items-center gap-1.5">
                  <span className="inline-flex h-4.5 min-w-4.5 items-center justify-center rounded-xs bg-brand/12 px-1 text-micro font-semibold text-brand">
                    {v.badge}
                  </span>
                  <span className="text-label font-semibold">{v.name[lang]}</span>
                </div>
                <p className="mt-1 text-micro leading-snug text-muted-foreground">
                  {v.summary[lang]}
                </p>
              </div>
            ))}
          </div>

          {COMPARE_GROUPS.map((group) => (
            <div key={group.title.en}>
              <div className="border-y border-border bg-muted/25 px-4 py-2 text-caption font-medium text-muted-foreground">
                {group.title[lang]}
              </div>
              <div className="divide-y divide-border/70">
                {group.items.map((item) => (
                  <div
                    key={item.label.en}
                    className="grid grid-cols-[200px_repeat(4,minmax(0,1fr))] items-center"
                  >
                    <div className="px-4 py-3">
                      <div className="text-label font-medium">{item.label[lang]}</div>
                      {item.note && (
                        <div className="mt-0.5 text-micro text-faint-foreground">
                          {item.note[lang]}
                        </div>
                      )}
                    </div>
                    {VARIANTS.map((v) => (
                      <div
                        key={v.id}
                        className="flex items-center border-l border-border/70 px-4 py-3"
                      >
                        <div
                          style={
                            zoom !== 1
                              ? { transform: `scale(${zoom})`, transformOrigin: "left center" }
                              : undefined
                          }
                        >
                          <DemoShortcutKeycaps
                            shortcut={item.shortcut}
                            variant={v.id}
                            platform={platform}
                            size={size}
                          />
                        </div>
                      </div>
                    ))}
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

const SPECIMEN_ROWS: DemoChord[][] = [
  [
    chord("Left"),
    chord("D", { alt: true, primary: true }),
    chord(",", { primary: true }),
    chord("Fn"),
    chord("T", { shift: true, primary: true }),
    chord("J", { primary: true }),
    chord("Down", { control: true }),
  ],
  [
    chord("4", { shift: true, primary: true }),
    chord("I", { primary: true }),
    chord("F12"),
    chord("Backspace", { alt: true }),
    chord("/", { primary: true }),
    chord("Q", { control: true, primary: true }),
    chord("Right"),
  ],
  [
    chord("L", { primary: true }),
    chord("Enter", { shift: true }),
    chord("Z", { primary: true }),
    chord("PageDown"),
    chord("Right", { alt: true }),
    chord("R", { primary: true }),
  ],
  [
    chord("Escape"),
    chord("Down", { alt: true }),
    chord("A", { primary: true }),
    chord("Tab"),
    chord("Tab", { control: true, shift: true }),
    chord("[", { primary: true }),
  ],
  [
    chord("P", { shift: true, primary: true }),
    chord("/"),
    chord("K", { primary: true }),
    chord("Escape", { alt: true, primary: true }),
    chord("Up"),
    chord("S", { primary: true }),
  ],
  [
    chord("Down"),
    chord("Enter", { primary: true }),
    chord("Space"),
    chord("Tab", { shift: true }),
    chord("J", { shift: true, primary: true }),
    chord("Delete", { alt: true }),
  ],
  [
    chord("]", { primary: true }),
    chord("S", { control: true, alt: true, shift: true, primary: true }),
    chord("Backspace"),
    chord("Left", { primary: true }),
    chord("F", { control: true, primary: true }),
    chord("H", { primary: true }),
  ],
];

function SpecimenWallPage() {
  const { lang } = useStoryEnv();
  const zh = lang === "zh";
  const [variant, setVariant] = useState<DemoVariant>("optical-pill");
  const [size, setSize] = useState<DemoSize>("md");

  return (
    <div className="min-h-screen bg-background p-10">
      <div className="mx-auto max-w-[1040px]">
        <header className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h1 className="text-title font-semibold">
              {zh ? "键帽标本墙（参考 @sunniekapar 构图）" : "Keycap specimen wall (inspired by @sunniekapar)"}
            </h1>
            <p className="mt-1 text-body text-muted-foreground">
              {zh
                ? "在整墙高密度排布里直接切换 A / B / C / C′，可以最直观感受光学均衡度与单胶囊的呼吸感。顶部工具栏可切 Dark 模式。"
                : "Switch A / B / C / C′ across a full wall of shortcuts to judge optical balance and visual calm. Use the toolbar to toggle Dark mode."}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Segmented
              label={zh ? "方案" : "Variant"}
              value={variant}
              onChange={setVariant}
              options={[
                { value: "current", label: "A · Current" },
                { value: "quick-win", label: "B · Step 1" },
                { value: "optical-pill", label: "C · Optical Pill" },
                { value: "optical-split", label: "C′ · Optical Split" },
              ]}
            />
            <Segmented
              label={zh ? "尺寸" : "Size"}
              value={size}
              onChange={setSize}
              options={[
                { value: "sm", label: "sm" },
                { value: "md", label: "md" },
              ]}
            />
          </div>
        </header>

        <div className="mt-8 rounded-2xl border border-border bg-page-canvas px-8 py-10 shadow-xs">
          <div className="flex flex-col items-center gap-4">
            {SPECIMEN_ROWS.map((row, rowIdx) => (
              <div
                key={rowIdx}
                className="flex flex-wrap items-center justify-center gap-3.5"
              >
                {row.map((item, idx) => (
                  <DemoShortcutKeycaps
                    key={`${rowIdx}-${idx}`}
                    shortcut={item}
                    variant={variant}
                    size={size}
                  />
                ))}
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function TooltipPreview({
  label,
  shortcut,
}: {
  label: string;
  shortcut: ReactNode;
}) {
  return (
    <div className="dark inline-flex items-center gap-1.5 rounded-md bg-(--tooltip) py-1 pr-1.5 pl-2.5 text-caption text-white shadow-(--tooltip-shadow)">
      <span>{label}</span>
      {shortcut}
    </div>
  );
}

function SurfaceColumn({
  title,
  badge,
  variant,
  lang,
}: {
  title: string;
  badge: string;
  variant: DemoVariant;
  lang: Lang;
}) {
  const zh = lang === "zh";
  const settingsVariant: DemoVariant =
    variant === "optical-pill" ? "optical-split" : variant;

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2">
        <span className="inline-flex h-5 min-w-5 items-center justify-center rounded-xs bg-brand/12 px-1.5 text-caption font-semibold text-brand">
          {badge}
        </span>
        <h2 className="text-body font-semibold">{title}</h2>
      </div>

      {/* 1. Command Palette */}
      <section className="overflow-hidden rounded-xl border border-border bg-popover shadow-md">
        <div className="flex items-center gap-2 border-b border-border px-3 py-2.5">
          <Search className="size-4 text-muted-foreground" />
          <span className="flex-1 text-body text-faint-foreground">
            {zh ? "搜索议题、项目或命令…" : "Search issues, projects, or commands…"}
          </span>
          <DemoShortcutKeycaps shortcut={chord("Escape")} variant={variant} />
        </div>
        <div className="p-1.5">
          <div className="px-2 pt-1 pb-1 text-caption font-medium text-muted-foreground">
            {zh ? "常用命令" : "Commands"}
          </div>
          {[
            {
              icon: Plus,
              label: zh ? "新建议题" : "Create issue",
              sc: chord("C"),
              selected: true,
            },
            {
              icon: MessageSquare,
              label: zh ? "开始新对话" : "Start new chat",
              sc: chord("J", { primary: true, shift: true }),
            },
            {
              icon: PanelLeft,
              label: zh ? "切换左侧边栏" : "Toggle sidebar",
              sc: chord("B", { primary: true }),
            },
            {
              icon: PanelRight,
              label: zh ? "切换右侧详情栏" : "Toggle right sidebar",
              sc: chord("/", { primary: true }),
            },
            {
              icon: ArrowLeft,
              label: zh ? "返回上一页" : "Go back",
              sc: chord("[", { primary: true }),
            },
          ].map((row) => (
            <div
              key={row.label}
              className={cn(
                "flex h-9 items-center gap-2.5 rounded-md px-2 text-body",
                row.selected && "bg-accent",
              )}
            >
              <row.icon className="size-4 text-muted-foreground" />
              <span className="flex-1 truncate">{row.label}</span>
              <DemoShortcutKeycaps shortcut={row.sc} variant={variant} />
            </div>
          ))}
        </div>
        <div className="flex items-center gap-3 border-t border-border px-3 py-2 text-caption text-muted-foreground">
          <span className="inline-flex items-center gap-1.5">
            <span className="inline-flex gap-0.5">
              <DemoShortcutKeycaps shortcut={chord("Up")} variant={variant} />
              <DemoShortcutKeycaps shortcut={chord("Down")} variant={variant} />
            </span>
            <span>{zh ? "切换" : "Navigate"}</span>
          </span>
          <span className="inline-flex items-center gap-1.5">
            <DemoShortcutKeycaps shortcut={chord("Enter")} variant={variant} />
            <span>{zh ? "打开" : "Select"}</span>
          </span>
          <span className="ml-auto inline-flex items-center gap-2.5">
            <span className="inline-flex items-center gap-1">
              <DemoLiteralKbd variant={variant}>&gt;</DemoLiteralKbd>
              <span>{zh ? "命令" : "Commands"}</span>
            </span>
            <span className="inline-flex items-center gap-1">
              <DemoLiteralKbd variant={variant}>#</DemoLiteralKbd>
              <span>{zh ? "议题" : "Issues"}</span>
            </span>
          </span>
        </div>
      </section>

      {/* 2. Tooltips & Primary Buttons */}
      <section className="rounded-xl border border-border bg-card p-5 shadow-xs">
        <div className="text-caption font-medium text-muted-foreground">
          {zh ? "Tooltip 与主按钮反色" : "Tooltips & inverted primary buttons"}
        </div>
        <div className="mt-3.5 flex flex-wrap items-center gap-2.5">
          <TooltipPreview
            label={zh ? "新对话" : "New chat"}
            shortcut={
              <DemoShortcutKeycaps
                shortcut={chord("J", { primary: true, shift: true })}
                variant={variant}
                tone="tooltip"
              />
            }
          />
          <TooltipPreview
            label={zh ? "右侧栏" : "Right sidebar"}
            shortcut={
              <DemoShortcutKeycaps
                shortcut={chord("/", { primary: true })}
                variant={variant}
                tone="tooltip"
              />
            }
          />
          <TooltipPreview
            label={zh ? "按住说话" : "Voice input"}
            shortcut={
              <DemoShortcutKeycaps
                shortcut={chord("Space", { alt: true })}
                variant={variant}
                tone="tooltip"
              />
            }
          />
          <TooltipPreview
            label={zh ? "新窗口打开" : "Open in new window"}
            shortcut={
              <DemoShortcutKeycaps
                shortcut={chord(zh ? "点击" : "Click", { shift: true })}
                variant={variant}
                tone="tooltip"
              />
            }
          />
        </div>
        <div className="mt-4 flex flex-wrap items-center gap-3">
          <Button size="sm">
            <span>{zh ? "创建议题" : "Create Issue"}</span>
            <DemoShortcutKeycaps
              shortcut={chord("Enter", { primary: true })}
              variant={variant}
              tone="on-primary"
              className="ml-1"
            />
          </Button>
          <Button size="sm" variant="outline">
            <span>{zh ? "搜索" : "Search"}</span>
            <DemoShortcutKeycaps
              shortcut={chord("K", { primary: true })}
              variant={variant}
              className="ml-1"
            />
          </Button>
        </div>
      </section>

      {/* 3. Settings · Keyboard shortcuts */}
      <section className="rounded-xl border border-border bg-card shadow-xs">
        <div className="flex items-center justify-between border-b border-border px-4 py-2.5">
          <span className="text-caption font-medium text-muted-foreground">
            {zh ? "设置 · 固定快捷键（md 尺寸）" : "Settings · Fixed shortcuts (md)"}
          </span>
          {variant === "optical-pill" && (
            <span className="text-micro text-faint-foreground">
              {zh ? "设置页保留拆键（C′）" : "Split kept in Settings (C′)"}
            </span>
          )}
        </div>
        <div className="divide-y divide-border/70">
          {[
            { label: zh ? "打开设置" : "Open settings", sc: chord(",", { primary: true }) },
            { label: zh ? "后退 / 前进" : "Back / Forward", sc: chord("[", { primary: true }) },
            { label: zh ? "放大界面" : "Zoom in", sc: chord("Plus", { primary: true }) },
            { label: zh ? "缩小界面" : "Zoom out", sc: chord("Minus", { primary: true }) },
            { label: zh ? "全屏切换" : "Toggle full screen", sc: chord("F", { control: true, primary: true }) },
          ].map((row) => (
            <div key={row.label} className="flex items-center justify-between px-4 py-2.5">
              <span className="text-body">{row.label}</span>
              <DemoShortcutKeycaps
                shortcut={row.sc}
                variant={settingsVariant}
                size="md"
              />
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

function RealSurfacesPage() {
  const { lang } = useStoryEnv();
  const zh = lang === "zh";

  return (
    <div className="min-h-screen bg-background p-10">
      <div className="mx-auto max-w-[1200px]">
        <header className="max-w-3xl">
          <h1 className="text-title font-semibold">
            {zh ? "真实产品界面对比：现状 vs Step 2" : "Real product surfaces: Current vs Step 2"}
          </h1>
          <p className="mt-1.5 text-body text-muted-foreground">
            {zh
              ? "把现状（左）和 Step 2 方案（右：高密度场景用单胶囊 C，设置页保留拆键 C′，反色用正式 tone 变体）放进 Multica 真实容器里直接对比。"
              : "Side-by-side in real Multica chrome: Current (left) vs Step 2 (right: pill C in dense surfaces, split C′ in Settings, formal inverted tones)."}
          </p>
        </header>

        <div className="mt-8 grid grid-cols-1 gap-8 lg:grid-cols-2">
          <SurfaceColumn
            badge="A"
            title={zh ? "线上现状" : "Current"}
            variant="current"
            lang={lang}
          />
          <SurfaceColumn
            badge="C"
            title={zh ? "Step 2 · 光学字形 + 场景分层" : "Step 2 · Optical glyphs + layered grouping"}
            variant="optical-pill"
            lang={lang}
          />
        </div>
      </div>
    </div>
  );
}

const ANATOMY_ITEMS: {
  name: OpticalGlyphName;
  label: string;
  legacyIcon?: IconComponent;
  legacyText?: string;
  note: Record<Lang, string>;
}[] = [
  {
    name: "command",
    label: "Command",
    legacyIcon: Command,
    note: { zh: "圆角环更舒展，小字号不糊中心孔", en: "More open corner loops stay crisp at 11px" },
  },
  {
    name: "control",
    label: "Control",
    legacyText: "⌃",
    note: { zh: "消除裸 Unicode 上漂，居中在 cap-height 中线", en: "Replaces high floating Unicode ⌃ with centered vector" },
  },
  {
    name: "option",
    label: "Option",
    legacyIcon: Option,
    note: { zh: "比例宽画布（17:16），斜杠与横杠配重均衡", en: "Proportional 17:16 box balances open diagonal weight" },
  },
  {
    name: "shift",
    label: "Shift",
    legacyIcon: ArrowBigUp,
    note: { zh: "由瘦长高箭头改为贴合大写高度的宽比例 ⇧", en: "Shorter, wider keycap ⇧ instead of tall ArrowBigUp" },
  },
  {
    name: "enter",
    label: "Return",
    legacyIcon: CornerDownLeft,
    note: { zh: "箭头重心抬升至中线，不再底部下坠", en: "Visual center lifted to cap midline" },
  },
  {
    name: "backspace",
    label: "Backspace",
    legacyIcon: Delete,
    note: { zh: "18.5:16 比例宽，外框收高抵消封闭体量", en: "18.5:16 box; slightly tighter height offsets closed silhouette" },
  },
  {
    name: "forwardDelete",
    label: "Forward Delete",
    legacyIcon: Delete,
    note: { zh: "现状与 Backspace 混用同一图标，现补齐 ⌦", en: "Previously shared Backspace icon; now distinct ⌦" },
  },
  {
    name: "tab",
    label: "Tab",
    legacyIcon: ArrowRightToLine,
    note: { zh: "竖线对齐 cap-height，箭杆居中", en: "Stop bar aligns with cap-height; centered shaft" },
  },
  {
    name: "arrowUp",
    label: "Up",
    legacyIcon: ArrowUp,
    note: { zh: "收窄水平字宽（13:16），组合里不留多余空白", en: "Narrower 13:16 advance avoids excess side-bearing" },
  },
  {
    name: "arrowDown",
    label: "Down",
    legacyIcon: ArrowDown,
    note: { zh: "与 Up 完全对称，笔宽对齐 Inter 500", en: "Symmetric with Up; stroke matches Inter 500" },
  },
  {
    name: "arrowLeft",
    label: "Left",
    legacyIcon: ArrowLeft,
    note: { zh: "与 ⌘ 并排时中线平齐", en: "Aligns on midline next to ⌘" },
  },
  {
    name: "arrowRight",
    label: "Right",
    legacyIcon: ArrowRight,
    note: { zh: "与 ⌥ 并排时中线平齐", en: "Aligns on midline next to ⌥" },
  },
  {
    name: "space",
    label: "Space",
    legacyIcon: Space,
    note: { zh: "现状贴在底部，现垂直居中（且默认推荐 Space 文字）", en: "Was bottom-sunk; now centered (text Space preferred by default)" },
  },
];

function SpecimenCell({
  showGuides,
  children,
}: {
  showGuides: boolean;
  children: ReactNode;
}) {
  return (
    <div className="relative inline-flex items-center justify-center rounded-md border border-border/70 bg-page-canvas px-4 py-2.5">
      {showGuides && (
        <>
          <span
            aria-hidden="true"
            className="pointer-events-none absolute inset-x-1 top-1/2 -translate-y-[0.365em] border-t border-dashed border-brand/45"
            style={{ fontSize: "36px" }}
          />
          <span
            aria-hidden="true"
            className="pointer-events-none absolute inset-x-1 top-1/2 border-t border-dotted border-destructive/45"
          />
          <span
            aria-hidden="true"
            className="pointer-events-none absolute inset-x-1 top-1/2 translate-y-[0.365em] border-t border-dashed border-brand/45"
            style={{ fontSize: "36px" }}
          />
        </>
      )}
      <div
        className="relative inline-flex items-center gap-2 font-sans leading-none font-medium text-foreground"
        style={{ fontSize: "36px", fontFeatureSettings: '"case" 1' }}
      >
        <span className="text-faint-foreground/60">H</span>
        {children}
        <span className="text-faint-foreground/60">K</span>
      </div>
    </div>
  );
}

function GlyphAnatomyPage() {
  const { lang } = useStoryEnv();
  const zh = lang === "zh";
  const [showGuides, setShowGuides] = useState(true);

  return (
    <div className="min-h-screen bg-background p-10">
      <div className="mx-auto max-w-[1160px]">
        <header className="flex flex-wrap items-end justify-between gap-4">
          <div className="max-w-2xl">
            <h1 className="text-title font-semibold">
              {zh ? "字形解剖（3× 放大 + Cap-height 标线）" : "Glyph anatomy (3× zoom + cap-height guides)"}
            </h1>
            <p className="mt-1.5 text-body text-muted-foreground">
              {zh
                ? "把每个符号夹在 Inter 500 大写字母 H 与 K 中间放大观察：蓝虚线为大写字高（cap-height），红点线为光学中线。"
                : "Every symbol flanked by Inter 500 capitals H and K at 3×: dashed blue marks cap-height, dotted red marks optical midline."}
            </p>
          </div>
          <Segmented
            label={zh ? "参考线" : "Guides"}
            value={showGuides ? "on" : "off"}
            onChange={(v) => setShowGuides(v === "on")}
            options={[
              { value: "on", label: zh ? "显示" : "On" },
              { value: "off", label: zh ? "隐藏" : "Off" },
            ]}
          />
        </header>

        <div className="mt-8 grid grid-cols-1 gap-4 md:grid-cols-2">
          {ANATOMY_ITEMS.map((item) => {
            const LegacyIcon = item.legacyIcon;
            return (
              <div
                key={item.name}
                className="rounded-xl border border-border bg-card p-4 shadow-xs"
              >
                <div className="flex items-baseline justify-between gap-2">
                  <span className="text-body font-semibold">{item.label}</span>
                  <span className="text-caption text-muted-foreground">
                    {item.note[lang]}
                  </span>
                </div>
                <div className="mt-3 grid grid-cols-2 gap-3">
                  <div>
                    <div className="mb-1.5 text-micro text-muted-foreground">
                      {zh ? "A · 现状（24px 图标缩放）" : "A · Current (24px icon scaled)"}
                    </div>
                    <SpecimenCell showGuides={showGuides}>
                      {LegacyIcon ? (
                        <LegacyIcon
                          strokeWidth={1.8}
                          style={{ width: "39px", height: "39px" }}
                        />
                      ) : (
                        <span>{item.legacyText}</span>
                      )}
                    </SpecimenCell>
                  </div>
                  <div>
                    <div className="mb-1.5 text-micro font-medium text-brand">
                      {zh ? "C · 新版（16px 光学字形）" : "C · New (16px optical glyph)"}
                    </div>
                    <SpecimenCell showGuides={showGuides}>
                      <OpticalKbdGlyph name={item.name} />
                    </SpecimenCell>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

export const Compare: Story = {
  name: "Compare all",
  render: () => <ComparePage />,
};

export const SpecimenWall: Story = {
  name: "Specimen wall",
  render: () => <SpecimenWallPage />,
};

export const RealSurfaces: Story = {
  name: "Real surfaces",
  render: () => <RealSurfacesPage />,
};

export const GlyphAnatomy: Story = {
  name: "Glyph anatomy",
  render: () => <GlyphAnatomyPage />,
};
