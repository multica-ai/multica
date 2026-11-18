import { useState, type ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { cn } from "@multica/ui/lib/utils";
import { CardStack, IssuePage, PropertiesCard } from "./parts";
import {
  BORDERS, FILLS, PRESETS, RADII, SHADOWS, describe,
  type BorderId, type FillId, type Preset, type Radius, type ShadowId, type Treatment,
} from "./treatments";

const meta = {
  title: "Issue sidebar cards",
  id: "issue-sidebar-cards",
  parameters: { layout: "fullscreen" },
} satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;

function PresetHeading({ preset }: { preset: Preset }) {
  return (
    <div className="w-[308px]">
      <div className="flex items-center gap-2">
        <span className="text-caption font-medium text-brand">{preset.letter}</span>
        <h2 className="text-body font-semibold">{preset.name}</h2>
        {preset.recommended && <span className="rounded-xs bg-foreground px-1.5 text-micro font-medium text-background">推荐</span>}
      </div>
      <p className="mt-0.5 font-mono text-micro text-muted-foreground">{describe(preset)}</p>
      <p className="mt-1.5 min-h-[54px] text-caption text-muted-foreground">{preset.note}</p>
    </div>
  );
}

function ComparePage() {
  return (
    <div className="min-h-screen bg-page-canvas p-10">
      <header className="max-w-3xl">
        <h1 className="text-title font-semibold">右侧卡片的边框和阴影：六种写法</h1>
        <p className="mt-1.5 text-body text-muted-foreground">
          背景就是任务页的画布（--page-canvas），卡片内容和尺寸完全一样，只换描边、阴影、底色和圆角。工具栏的 Theme 可以切到深色。
        </p>
      </header>
      <div className="mt-8 grid grid-cols-[repeat(3,308px)] gap-x-12 gap-y-10">
        {PRESETS.map((preset) => (
          <section key={preset.id}>
            <PresetHeading preset={preset} />
            <div className="mt-3"><CardStack t={preset} short /></div>
          </section>
        ))}
      </div>
    </div>
  );
}

/** The bottom-left corner at 4×: that is where the shadow falls, so a 1px edge and a 2px shadow show up as pixels. */
function Zoomed({ t }: { t: Treatment }) {
  return (
    <div className="relative h-[260px] w-[308px] overflow-hidden rounded-lg bg-page-canvas ring-1 ring-border">
      <div className="absolute bottom-0 left-0" style={{ zoom: 4 }}>
        <div className="w-[200px] p-3">
          <PropertiesCard t={t} />
        </div>
      </div>
    </div>
  );
}

function CloseUpPage() {
  return (
    <div className="min-h-screen bg-page-canvas p-10">
      <header className="max-w-3xl">
        <h1 className="text-title font-semibold">放大 4 倍看卡片左下角</h1>
        <p className="mt-1.5 text-body text-muted-foreground">
          阴影都往下落，所以看左下角。看三件事：描边和画布的对比够不够、阴影有多重、圆角配不配。每格都是真实渲染再放大，不是截图。
        </p>
      </header>
      <div className="mt-8 grid grid-cols-[repeat(3,308px)] gap-x-12 gap-y-10">
        {PRESETS.map((preset) => (
          <section key={preset.id}>
            <PresetHeading preset={preset} />
            <div className="mt-3"><Zoomed t={preset} /></div>
          </section>
        ))}
      </div>
    </div>
  );
}

function Segmented<T extends string | number>({
  label, value, options, onChange,
}: {
  label: string;
  value: T;
  options: { value: T; label: ReactNode }[];
  onChange: (value: T) => void;
}) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-caption text-muted-foreground">{label}</span>
      <div className="flex rounded-lg bg-muted p-0.5">
        {options.map((option) => (
          <button
            key={String(option.value)}
            type="button"
            aria-pressed={option.value === value}
            onClick={() => onChange(option.value)}
            className={cn(
              "h-6 rounded-md px-2 text-caption text-muted-foreground transition-colors",
              option.value === value && "bg-surface font-medium text-foreground shadow-[var(--surface-shadow)] ring-1 ring-border",
            )}
          >
            {option.label}
          </button>
        ))}
      </div>
    </div>
  );
}

function TryItPage() {
  const [t, setT] = useState<Treatment>(PRESETS.find((p) => p.recommended)!);
  const set = <K extends keyof Treatment>(key: K) => (value: Treatment[K]) => setT((prev) => ({ ...prev, [key]: value }));
  const match = PRESETS.find((p) => p.border === t.border && p.shadow === t.shadow && p.fill === t.fill && p.radius === t.radius);
  return (
    <div className="flex h-screen flex-col bg-app-shell">
      <div className="flex flex-wrap items-center gap-x-5 gap-y-2 border-b bg-page-canvas px-4 py-2.5">
        <div className="flex items-center gap-1">
          {PRESETS.map((p) => (
            <button
              key={p.id}
              type="button"
              title={p.name}
              aria-pressed={match?.id === p.id}
              onClick={() => setT(p)}
              className={cn(
                "size-7 rounded-md text-caption font-medium text-muted-foreground transition-colors hover:bg-accent",
                match?.id === p.id && "bg-foreground text-background hover:bg-foreground",
              )}
            >
              {p.letter}
            </button>
          ))}
        </div>
        <Segmented<BorderId> label="描边" value={t.border} onChange={set("border")}
          options={(Object.keys(BORDERS) as BorderId[]).map((id) => ({ value: id, label: BORDERS[id].label }))} />
        <Segmented<ShadowId> label="阴影" value={t.shadow} onChange={set("shadow")}
          options={(Object.keys(SHADOWS) as ShadowId[]).map((id) => ({ value: id, label: SHADOWS[id].label }))} />
        <Segmented<FillId> label="底色" value={t.fill} onChange={set("fill")}
          options={(Object.keys(FILLS) as FillId[]).map((id) => ({ value: id, label: FILLS[id].label }))} />
        <Segmented<Radius> label="圆角" value={t.radius} onChange={set("radius")}
          options={RADII.map((r) => ({ value: r, label: `${r}` }))} />
        <span className="ml-auto text-caption text-muted-foreground">{match ? `${match.letter} · ${match.name}` : "自定义"}</span>
      </div>
      <div className="min-h-0 flex-1 p-2">
        <IssuePage t={t} />
      </div>
    </div>
  );
}

export const Compare: Story = { name: "Compare all", render: () => <ComparePage /> };
export const CloseUp: Story = { name: "Close-up 4×", render: () => <CloseUpPage /> };
export const TryIt: Story = { name: "Try it", render: () => <TryItPage /> };
