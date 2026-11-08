"use client";

import { useId, type CSSProperties, type ReactNode } from "react";
import { useShallow } from "zustand/react/shallow";
import {
  Bot,
  Check,
  ChevronRight,
  ListTodo,
  Monitor,
  Moon,
  RotateCcw,
  Sun,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Slider } from "@multica/ui/components/ui/slider";
import { Switch } from "@multica/ui/components/ui/switch";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import {
  ACCENT_COLORS,
  ACCENTS,
  BUILT_IN_THEMES,
  CJK_FONTS,
  CODE_FONTS,
  DEFAULT_FONT_SIZE,
  DENSITIES,
  FONT_SIZES,
  UI_FONTS,
  isDefaultAppearance,
  selectAppearance,
  useAppearanceStore,
  type CodeFont,
  type FontSize,
  type ThemeManifest,
  type ThemeTokens,
  type UiFont,
} from "@multica/core/appearance";
import { useT } from "../../i18n";
import { StatusIcon } from "../../issues/components/status-icon";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { SegmentedToggle } from "../../common/segmented-toggle";
import {
  SETTINGS_ANCHOR_ATTR,
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

type PreviewMode = "light" | "dark";
type ModeChoice = PreviewMode | "system";

const CJK_STACK = "var(--font-cjk, var(--font-cjk-sans))";

/** Each interface font drawn in its own face, followed by the chosen CJK face. */
const UI_FONT_SAMPLE: Record<UiFont, string> = {
  inter: `var(--font-inter, "Inter Variable"), "Inter", ${CJK_STACK}`,
  system: `-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, ${CJK_STACK}`,
  serif: `var(--font-ui-serif), ${CJK_STACK}`,
  mono: `var(--font-ui-mono), ${CJK_STACK}`,
};
const UI_FONT_NAME: Record<UiFont, string> = {
  inter: "Inter",
  system: "SF Pro / Segoe UI",
  serif: "Source Serif 4",
  mono: "Geist Mono",
};
const CJK_SAMPLE: Record<(typeof CJK_FONTS)[number], string> = {
  sans: "var(--font-cjk-sans)",
  serif: "var(--font-cjk-serif)",
  kai: "var(--font-cjk-kai)",
};
const CODE_FONT_SAMPLE: Record<CodeFont, string> = {
  geist: "var(--font-ui-mono), monospace",
  jetbrains: "var(--font-code-jetbrains), monospace",
  system: 'ui-monospace, "SF Mono", Menlo, Consolas, monospace',
};
const CODE_FONT_NAME: Record<CodeFont, string> = {
  geist: "Geist Mono",
  jetbrains: "JetBrains Mono",
  system: "SF Mono / Consolas",
};

/**
 * Appearance: colour (mode, theme, accent), text (faces and size), and
 * layout and accessibility. Every change applies at once, so the page itself
 * is part of the preview; the sample on the right adds what the settings page
 * has none of — an issue with code, sub-issues and a comment.
 */
export function AppearanceTab() {
  const { t } = useT("settings");
  const prefs = useAppearanceStore(useShallow(selectAppearance));
  const reset = useAppearanceStore((s) => s.reset);
  return (
    <SettingsTab
      title={t(($) => $.page.tabs.appearance)}
      scope="device"
      actions={
        <Button
          variant="ghost"
          size="sm"
          disabled={isDefaultAppearance(prefs)}
          onClick={reset}
        >
          <RotateCcw />
          {t(($) => $.appearance.reset)}
        </Button>
      }
    >
      <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_20rem] lg:items-start">
        <div className="min-w-0 space-y-8">
          <ColorSection />
          <TextSection />
          <LayoutSection />
        </div>
        <PreviewPanel />
      </div>
    </SettingsTab>
  );
}

function useAppliedMode(): PreviewMode {
  const { resolvedTheme } = useTheme();
  return resolvedTheme === "dark" ? "dark" : "light";
}

/** A card row whose control sits under the label rather than beside it. */
function SettingsBlock({
  label,
  description,
  anchor,
  children,
}: {
  label: string;
  description?: string;
  anchor: string;
  children: ReactNode;
}) {
  return (
    <div {...{ [SETTINGS_ANCHOR_ATTR]: anchor }} className="scroll-mt-6 space-y-3 px-4 py-3.5">
      <div>
        <div className="text-body font-medium">{label}</div>
        {description ? (
          <div className="mt-0.5 text-caption leading-5 text-muted-foreground">{description}</div>
        ) : null}
      </div>
      {children}
    </div>
  );
}

function ColorSection() {
  const { t } = useT("settings");
  const { theme: mode, setTheme: setMode } = useTheme();
  const appliedMode = useAppliedMode();
  const theme = useAppearanceStore((s) => s.theme);
  const accent = useAppearanceStore((s) => s.accent);
  const set = useAppearanceStore((s) => s.set);
  const modeValue: ModeChoice = mode === "light" || mode === "dark" ? mode : "system";
  const modeLabel = t(($) => $.appearance.mode.title);
  const themeLabel = t(($) => $.appearance.theme.title);
  const accentLabel = t(($) => $.appearance.accent.title);
  const currentTheme = BUILT_IN_THEMES.find((m) => m.id === theme) ?? BUILT_IN_THEMES[0]!;
  const themeBrand = currentTheme.tokens[appliedMode].brand ?? PREVIEW_BASE[appliedMode].brand;

  return (
    <SettingsSection title={t(($) => $.appearance.color.title)} anchor="color">
      <SettingsCard>
        <SettingsRow label={modeLabel} anchor="mode">
          <div role="group" aria-label={modeLabel}>
            <SegmentedToggle<ModeChoice>
              value={modeValue}
              onChange={setMode}
              buttonClassName="flex items-center gap-1.5 px-2.5 py-1 text-label"
              options={[
                ["light", <><Sun aria-hidden="true" className="size-3.5" />{t(($) => $.appearance.mode.light)}</>],
                ["dark", <><Moon aria-hidden="true" className="size-3.5" />{t(($) => $.appearance.mode.dark)}</>],
                ["system", <><Monitor aria-hidden="true" className="size-3.5" />{t(($) => $.appearance.mode.system)}</>],
              ]}
            />
          </div>
        </SettingsRow>
        <SettingsBlock
          label={themeLabel}
          description={t(($) => $.appearance.theme.description)}
          anchor="theme"
        >
          <div role="group" aria-label={themeLabel} className="grid grid-cols-2 gap-3 sm:grid-cols-3">
            {BUILT_IN_THEMES.map((manifest) => (
              <OptionCard
                key={manifest.id}
                label={
                  manifest.id === "default"
                    ? t(($) => $.appearance.theme.default)
                    : manifest.id === "pop"
                      ? t(($) => $.appearance.theme.pop)
                      : manifest.name
                }
                selected={theme === manifest.id}
                onSelect={() => set({ theme: manifest.id })}
                preview={<ThemePreview theme={manifest} mode={appliedMode} />}
              />
            ))}
          </div>
        </SettingsBlock>
        <SettingsRow
          label={accentLabel}
          description={t(($) => $.appearance.accent.description)}
          anchor="accent"
        >
          <div role="radiogroup" aria-label={accentLabel} className="flex flex-wrap items-center gap-2.5">
            {ACCENTS.map((id) => (
              <AccentSwatch
                key={id}
                label={t(($) => $.appearance.accent[id])}
                color={id === "theme" ? themeBrand : ACCENT_COLORS[id][appliedMode]}
                selected={accent === id}
                onSelect={() => set({ accent: id })}
              />
            ))}
          </div>
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}

function AccentSwatch({
  label,
  color,
  selected,
  onSelect,
}: {
  label: string;
  color: string;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      aria-label={label}
      title={label}
      onClick={onSelect}
      style={{ backgroundColor: color }}
      className={cn(
        "flex size-5 items-center justify-center rounded-full text-white outline-none ring-offset-2 ring-offset-surface transition-shadow focus-visible:ring-2 focus-visible:ring-ring",
        selected && "ring-2 ring-foreground",
      )}
    >
      <span className="sr-only">{label}</span>
    </button>
  );
}

function TextSection() {
  const { t } = useT("settings");
  const { uiFont, cjkFont, codeFont, fontSize, set } = useAppearanceStore(
    useShallow((s) => ({
      uiFont: s.uiFont,
      cjkFont: s.cjkFont,
      codeFont: s.codeFont,
      fontSize: s.fontSize,
      set: s.set,
    })),
  );
  const uiLabel = t(($) => $.appearance.ui_font.title);
  const cjkLabel = t(($) => $.appearance.cjk_font.title);
  const codeLabel = t(($) => $.appearance.code_font.title);
  const sizeLabel = t(($) => $.appearance.font_size.title);
  const codeOptions = CODE_FONTS.map((id) => ({ value: id, label: CODE_FONT_NAME[id] }));

  return (
    <SettingsSection title={t(($) => $.appearance.text.title)} anchor="text">
      <SettingsCard>
        <SettingsBlock
          label={uiLabel}
          description={t(($) => $.appearance.ui_font.description)}
          anchor="ui-font"
        >
          <div role="group" aria-label={uiLabel} className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            {UI_FONTS.map((id) => (
              <FontTile
                key={id}
                label={UI_FONT_NAME[id]}
                hint={t(($) => $.appearance.ui_font[id])}
                sample={t(($) => $.appearance.ui_font.sample)}
                fontFamily={UI_FONT_SAMPLE[id]}
                selected={uiFont === id}
                onSelect={() => set({ uiFont: id })}
              />
            ))}
          </div>
        </SettingsBlock>
        <SettingsRow
          label={cjkLabel}
          description={t(($) => $.appearance.cjk_font.description)}
          anchor="cjk-font"
        >
          <div role="group" aria-label={cjkLabel}>
            <SegmentedToggle
              value={cjkFont}
              onChange={(cjk) => set({ cjkFont: cjk })}
              buttonClassName="px-3 py-1 text-body"
              options={CJK_FONTS.map(
                (id) =>
                  [
                    id,
                    <span key={id} style={{ fontFamily: CJK_SAMPLE[id] }}>
                      {t(($) => $.appearance.cjk_font[id])}
                    </span>,
                  ] as const,
              )}
            />
          </div>
        </SettingsRow>
        <SettingsRow
          label={codeLabel}
          description={t(($) => $.appearance.code_font.description)}
          anchor="code-font"
          size="select"
        >
          <Select
            items={codeOptions}
            value={codeFont}
            onValueChange={(next) => {
              if (next) set({ codeFont: next as CodeFont });
            }}
          >
            <SelectTrigger size="sm" className="w-full" aria-label={codeLabel}>
              <SelectValue>
                <span style={{ fontFamily: CODE_FONT_SAMPLE[codeFont] }}>
                  {CODE_FONT_NAME[codeFont]}
                </span>
              </SelectValue>
            </SelectTrigger>
            <SelectContent align="end">
              {CODE_FONTS.map((id) => (
                <SelectItem key={id} value={id}>
                  <span className="flex min-w-0 flex-1 items-baseline justify-between gap-3">
                    <span style={{ fontFamily: CODE_FONT_SAMPLE[id] }}>{CODE_FONT_NAME[id]}</span>
                    <span className="text-caption text-muted-foreground">
                      {t(($) => $.appearance.code_font[id])}
                    </span>
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </SettingsRow>
        <SettingsRow
          label={sizeLabel}
          description={t(($) => $.appearance.font_size.description)}
          anchor="font-size"
        >
          <div className="flex w-56 items-center gap-3">
            <span aria-hidden="true" className="text-caption text-muted-foreground">
              A
            </span>
            <Slider
              aria-label={sizeLabel}
              min={FONT_SIZES[0]}
              max={FONT_SIZES[FONT_SIZES.length - 1]}
              step={1}
              value={fontSize}
              onValueChange={(next) => {
                const size = Array.isArray(next) ? next[0] : next;
                if ((FONT_SIZES as readonly number[]).includes(size as number)) {
                  set({ fontSize: size as FontSize });
                }
              }}
              className="flex-1"
            />
            <span aria-hidden="true" className="text-title-sm text-muted-foreground">
              A
            </span>
            <span className="w-24 shrink-0 text-right text-caption tabular-nums text-muted-foreground">
              {fontSize === DEFAULT_FONT_SIZE
                ? t(($) => $.appearance.font_size.default, { size: fontSize })
                : t(($) => $.appearance.font_size.value, { size: fontSize })}
            </span>
          </div>
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}

function FontTile({
  label,
  hint,
  sample,
  fontFamily,
  selected,
  onSelect,
}: {
  label: string;
  hint: string;
  sample: string;
  fontFamily: string;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      aria-label={`${label} · ${hint}`}
      onClick={() => {
        if (!selected) onSelect();
      }}
      className={cn(
        "relative flex min-w-0 flex-col items-start gap-1.5 rounded-lg bg-surface px-3 py-2.5 text-left outline-none ring-1 ring-surface-border transition-shadow focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring",
        selected ? "ring-2 ring-foreground" : "hover:ring-foreground/30",
      )}
    >
      <span aria-hidden="true" className="truncate text-display-sm leading-8" style={{ fontFamily }}>
        {sample}
      </span>
      <span className="min-w-0">
        <span className="block truncate text-caption font-medium">{label}</span>
        <span className="block truncate text-micro text-muted-foreground">{hint}</span>
      </span>
      {selected ? (
        <span
          aria-hidden="true"
          className="absolute top-2 right-2 flex size-4 items-center justify-center rounded-full bg-foreground text-background"
        >
          <Check className="size-2.5" />
        </span>
      ) : null}
    </button>
  );
}

function LayoutSection() {
  const { t } = useT("settings");
  const { density, reduceMotion, highContrast, set } = useAppearanceStore(
    useShallow((s) => ({
      density: s.density,
      reduceMotion: s.reduceMotion,
      highContrast: s.highContrast,
      set: s.set,
    })),
  );
  const densityLabel = t(($) => $.appearance.density.title);
  return (
    <SettingsSection title={t(($) => $.appearance.layout.title)} anchor="layout">
      <SettingsCard>
        <SettingsRow
          label={densityLabel}
          description={t(($) => $.appearance.density.description)}
          anchor="density"
        >
          <div role="group" aria-label={densityLabel}>
            <SegmentedToggle
              value={density}
              onChange={(next) => set({ density: next })}
              buttonClassName="px-3 py-1 text-label"
              options={DENSITIES.map((id) => [id, t(($) => $.appearance.density[id])] as const)}
            />
          </div>
        </SettingsRow>
        <SettingsRow
          label={t(($) => $.appearance.reduce_motion.title)}
          description={t(($) => $.appearance.reduce_motion.description)}
          anchor="reduce-motion"
        >
          <Switch
            checked={reduceMotion}
            onCheckedChange={(checked) => set({ reduceMotion: checked })}
            aria-label={t(($) => $.appearance.reduce_motion.title)}
          />
        </SettingsRow>
        <SettingsRow
          label={t(($) => $.appearance.high_contrast.title)}
          description={t(($) => $.appearance.high_contrast.description)}
          anchor="high-contrast"
        >
          <Switch
            checked={highContrast}
            onCheckedChange={(checked) => set({ highContrast: checked })}
            aria-label={t(($) => $.appearance.high_contrast.title)}
          />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}

function PreviewPanel() {
  const { t } = useT("settings");
  return (
    <aside className="hidden space-y-3 lg:sticky lg:top-0 lg:block">
      <div className="flex items-center justify-between gap-2 px-0.5">
        <h3 className="text-body font-semibold">{t(($) => $.appearance.preview.title)}</h3>
        <span className="flex items-center gap-1.5 text-caption text-muted-foreground">
          <span aria-hidden="true" className="size-1.5 rounded-full bg-success" />
          {t(($) => $.appearance.preview.live)}
        </span>
      </div>
      <AppearanceSample />
    </aside>
  );
}

/** Sample data, not copy: identifiers and names read the same in every locale. */
const SAMPLE_ISSUE = "MUL-128";
const SAMPLE_AGENT = "Lambda";
const SAMPLE_CODE = "--font-scale";

/** A real-looking issue drawn with the live tokens. Decorative: not focusable. */
function AppearanceSample() {
  const { t } = useT("settings");
  const rows: [string, string, string][] = [
    ["done", "MUL-129", t(($) => $.appearance.preview.row_1)],
    ["in_review", "MUL-130", t(($) => $.appearance.preview.row_2)],
    ["todo", "MUL-131", t(($) => $.appearance.preview.row_3)],
  ];
  const chip =
    "inline-flex h-6 items-center gap-1.5 rounded-full border border-input bg-surface px-2 text-caption";
  return (
    <div
      aria-hidden="true"
      inert
      className="space-y-3 overflow-hidden rounded-xl bg-background p-4 shadow-(--surface-shadow) ring-1 ring-surface-border"
    >
      <div className="flex items-center gap-1.5 text-caption text-muted-foreground">
        <ListTodo className="size-3.5" />
        {t(($) => $.appearance.preview.breadcrumb)}
        <ChevronRight className="size-3 text-faint-foreground" />
        {SAMPLE_ISSUE}
      </div>
      <h4 className="font-heading text-title-lg font-semibold">
        {t(($) => $.appearance.preview.issue_title)}
      </h4>
      <div className="flex flex-wrap gap-1.5">
        <span className={chip}>
          <StatusIcon status="in_progress" className="size-3.5" />
          {t(($) => $.appearance.preview.status)}
        </span>
        <span className={chip}>
          <PriorityIcon priority="high" className="size-3.5" />
          {t(($) => $.appearance.preview.priority)}
        </span>
        <span className={chip}>
          <AgentMark />
          {SAMPLE_AGENT}
        </span>
      </div>
      <p className="text-body leading-relaxed">
        {t(($) => $.appearance.preview.body)}{" "}
        <span className="text-brand underline decoration-brand/40 underline-offset-2">
          {t(($) => $.appearance.preview.link)}
        </span>
      </p>
      <pre className="overflow-hidden rounded-md bg-muted px-3 py-2 font-mono text-caption leading-relaxed">
        <span className="text-muted-foreground">{"// 0Oo 1lI {} => !="}</span>
        {"\n"}
        {'theme.set({ font: "mono" });'}
      </pre>
      <div className="overflow-hidden rounded-lg bg-surface ring-1 ring-surface-border">
        <div className="border-b border-border px-3 py-1.5 text-caption font-medium text-muted-foreground">
          {t(($) => $.appearance.preview.subissues)}
        </div>
        {rows.map(([status, id, title]) => (
          <div
            key={id}
            className="flex h-[var(--issue-row-height)] items-center gap-2 border-b border-border px-3 text-body last:border-b-0"
          >
            <StatusIcon status={status} className="size-3.5 shrink-0" />
            <span className="shrink-0 text-caption tabular-nums text-muted-foreground">{id}</span>
            <span className="min-w-0 truncate">{title}</span>
          </div>
        ))}
      </div>
      <div className="flex gap-2">
        <AgentMark className="mt-0.5 size-6" />
        <div className="min-w-0 flex-1 rounded-lg bg-surface px-3 py-2 ring-1 ring-surface-border">
          <div className="flex items-baseline gap-2 text-label font-medium">
            {SAMPLE_AGENT}
            <span className="text-caption font-normal text-muted-foreground">
              {t(($) => $.appearance.preview.comment_time)}
            </span>
          </div>
          <p className="mt-0.5 text-body">
            {t(($) => $.appearance.preview.comment)}{" "}
            <code className="rounded-sm bg-muted px-1 font-mono text-caption">{SAMPLE_CODE}</code>
          </p>
        </div>
      </div>
      <div className="flex justify-end gap-2">
        <Button variant="outline" size="sm" tabIndex={-1}>
          {t(($) => $.appearance.preview.cancel)}
        </Button>
        <Button size="sm" tabIndex={-1}>
          {t(($) => $.appearance.preview.submit)}
        </Button>
      </div>
    </div>
  );
}

function AgentMark({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        "flex size-4 shrink-0 items-center justify-center rounded-sm bg-brand text-brand-foreground",
        className,
      )}
    >
      <Bot className="size-3" />
    </span>
  );
}

function OptionCard({
  label,
  selected,
  onSelect,
  preview,
}: {
  label: string;
  selected: boolean;
  onSelect: () => void;
  preview: ReactNode;
}) {
  const labelId = useId();
  return (
    <button
      type="button"
      aria-labelledby={labelId}
      aria-pressed={selected}
      onClick={() => {
        if (!selected) onSelect();
      }}
      className="group/option flex min-w-0 flex-col gap-2 rounded-xl text-left outline-none"
    >
      <span
        className={cn(
          "@container block w-full overflow-hidden rounded-lg ring-1 ring-surface-border transition-shadow group-focus-visible/option:outline-2 group-focus-visible/option:outline-offset-2 group-focus-visible/option:outline-ring",
          selected ? "ring-2 ring-foreground" : "group-hover/option:ring-foreground/30",
        )}
      >
        {preview}
      </span>
      <span className="flex min-w-0 items-center gap-2 px-0.5">
        <span
          aria-hidden="true"
          className={cn(
            "flex size-3.5 shrink-0 items-center justify-center rounded-full border",
            selected ? "border-foreground bg-foreground text-background" : "border-input",
          )}
        >
          {selected ? <Check className="size-2.5" /> : null}
        </span>
        <span id={labelId} className={cn("block truncate text-label", selected && "font-medium")}>
          {label}
        </span>
      </span>
    </button>
  );
}

/**
 * The tokens a miniature needs, as tokens.css sets them for the default
 * theme. Theme manifests only list overrides, so previews start from these.
 */
const PREVIEW_BASE: Record<PreviewMode, Required<Pick<ThemeTokens,
  "app-shell" | "page-canvas" | "surface-border" | "foreground" | "surface-selected" | "muted" | "primary" | "radius" | "brand"
>>> = {
  light: {
    "app-shell": "oklch(0.964 0.001 286)",
    "page-canvas": "oklch(0.988 0 0)",
    "surface-border": "oklch(0.92 0.004 286)",
    foreground: "oklch(0.141 0.005 286)",
    "surface-selected": "oklch(0.95 0.002 286)",
    muted: "oklch(0.967 0.001 286)",
    primary: "oklch(0.21 0.006 286)",
    radius: "0.5rem",
    brand: "oklch(0.561 0.194 256.4)",
  },
  dark: {
    "app-shell": "oklch(0.155 0.005 286)",
    "page-canvas": "oklch(0.18 0.005 286)",
    "surface-border": "oklch(0.3 0.006 286)",
    foreground: "oklch(0.985 0 0)",
    "surface-selected": "oklch(0.3 0.006 286)",
    muted: "oklch(0.274 0.006 286)",
    primary: "oklch(0.92 0.004 286)",
    radius: "0.5rem",
    brand: "oklch(0.65 0.16 255)",
  },
};

/** Status block colours from appearance.css, for themes with that trait. */
const STATUS_BLOCKS = { a: "oklch(0.84 0.13 68)", b: "oklch(0.88 0.035 250)" };

function previewVariables(theme: ThemeManifest, mode: PreviewMode): CSSProperties {
  const tokens = { ...PREVIEW_BASE[mode], ...theme.tokens[mode] };
  const frame = theme.frame?.[mode];
  const ink = tokens.foreground;
  const canvas = tokens["page-canvas"];
  const blocks = theme.traits?.includes("status-blocks");
  const shadow = theme.traits?.includes("offset-shadow");
  const radiusRem = Number.parseFloat(tokens.radius) || 0.5;
  return {
    "--pv-shell": tokens["app-shell"],
    "--pv-rail": `color-mix(in oklab, ${frame?.foreground ?? ink} 28%, ${tokens["app-shell"]})`,
    "--pv-canvas": canvas,
    "--pv-line": tokens["surface-border"],
    "--pv-ink": ink,
    "--pv-faint": `color-mix(in oklab, ${ink} 16%, ${canvas})`,
    "--pv-selected": tokens["surface-selected"],
    "--pv-band-a": blocks ? STATUS_BLOCKS.a : tokens.muted,
    "--pv-band-b": blocks ? STATUS_BLOCKS.b : tokens.muted,
    "--pv-band-line": blocks ? tokens["surface-border"] : "transparent",
    "--pv-band-ink": blocks ? "oklch(0.145 0 0)" : ink,
    "--pv-dot-a": blocks ? "oklch(0.145 0 0)" : "oklch(0.75 0.15 75)",
    "--pv-dot-b": blocks ? "oklch(0.145 0 0)" : `color-mix(in oklab, ${ink} 40%, ${canvas})`,
    "--pv-primary": tokens["button-primary"] ?? tokens.primary,
    "--pv-primary-line": tokens["button-primary-border"] ?? "transparent",
    "--pv-primary-shadow": shadow ? `0.8cqw 0.8cqw 0 ${tokens["surface-border"]}` : "none",
    "--pv-brand": tokens.brand,
    "--pv-radius": `${(radiusRem / 0.5) * 2}cqw`,
  } as CSSProperties;
}

/** A miniature app in a theme: rail, sidebar, and a list with two status groups. */
function ThemePreview({ theme, mode }: { theme: ThemeManifest; mode: PreviewMode }) {
  return (
    <span
      aria-hidden="true"
      data-appearance-preview={theme.id}
      data-preview-mode={mode}
      style={previewVariables(theme, mode)}
      className="flex aspect-[16/10] w-full gap-[2.5cqw] bg-(--pv-shell) p-[2.5cqw] pl-[2cqw]"
    >
      <span className="flex w-[5cqw] shrink-0 flex-col items-center gap-[2.5cqw] pt-[0.5cqw]">
        <span className="size-[4.5cqw] rounded-[1.2cqw] bg-(--pv-canvas)" />
        {[0, 1, 2].map((i) => (
          <span key={i} className="size-[3.2cqw] rounded-[0.9cqw] bg-(--pv-rail)" />
        ))}
      </span>
      <span className="flex min-w-0 flex-1 overflow-hidden rounded-(--pv-radius) border border-(--pv-line) bg-(--pv-canvas)">
        <span className="flex w-[24cqw] shrink-0 flex-col gap-[2.2cqw] border-r border-(--pv-line) p-[2.8cqw]">
          <span className="h-[2cqw] w-3/5 rounded-full bg-(--pv-ink)" />
          <span className="h-[4.2cqw] rounded-[0.9cqw] bg-(--pv-selected)" />
          {["w-4/5", "w-3/5", "w-2/3"].map((width) => (
            <span key={width} className={cn("h-[1.6cqw] rounded-full bg-(--pv-faint)", width)} />
          ))}
        </span>
        <span className="flex min-w-0 flex-1 flex-col gap-[2.2cqw] p-[3cqw]">
          <span className="flex items-center justify-between">
            <span className="h-[2cqw] w-[28%] rounded-full bg-(--pv-ink)" />
            <span className="h-[3.6cqw] w-[11cqw] rounded-[0.9cqw] border border-(--pv-primary-line) bg-(--pv-primary) shadow-(--pv-primary-shadow)" />
          </span>
          <PreviewGroup band="a" rows={["w-3/4", "w-1/2"]} />
          <PreviewGroup band="b" rows={["w-2/3", "w-3/5"]} />
        </span>
      </span>
    </span>
  );
}

function PreviewGroup({ band, rows }: { band: "a" | "b"; rows: string[] }) {
  return (
    <>
      <span
        className={cn(
          "flex h-[4.2cqw] items-center gap-[1.2cqw] rounded-[0.9cqw] px-[1.4cqw] ring-1 ring-inset ring-(--pv-band-line)",
          band === "a" ? "bg-(--pv-band-a)" : "bg-(--pv-band-b)",
        )}
      >
        <span
          className={cn("size-[2cqw] rounded-full", band === "a" ? "bg-(--pv-dot-a)" : "bg-(--pv-dot-b)")}
        />
        <span className="h-[1.6cqw] w-[22%] rounded-full bg-(--pv-band-ink)" />
      </span>
      {rows.map((width, i) => (
        <span key={width} className="flex items-center gap-[1.4cqw] px-[1.4cqw]">
          <span
            className={cn(
              "h-[1.6cqw] w-[9%] rounded-full",
              i === 0 ? "bg-(--pv-brand)" : "bg-(--pv-faint)",
            )}
          />
          <span className={cn("h-[1.6cqw] rounded-full bg-(--pv-faint)", width)} />
        </span>
      ))}
    </>
  );
}
