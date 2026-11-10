"use client";

import { useMemo } from "react";
import { useShallow } from "zustand/react/shallow";
import {
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
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Input } from "@multica/ui/components/ui/input";
import { Slider } from "@multica/ui/components/ui/slider";
import { Switch } from "@multica/ui/components/ui/switch";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import {
  BUILT_IN_THEMES,
  CJK_FONTS,
  CODE_FONTS,
  CONTENT_LEADINGS,
  CONTENT_SIZES,
  DEFAULT_APPEARANCE,
  DEFAULT_FONT_SIZE,
  DENSITIES,
  FOLLOW_UI,
  FONT_SIZES,
  UI_FONTS,
  activeTheme,
  cjkFontStack,
  isDefaultAppearance,
  isLocalFont,
  isSafeFontValue,
  isSafeThemeValue,
  localFontFamily,
  localizedText,
  selectAppearance,
  themeModeVariables,
  themeOptionValue,
  uiFontStack,
  useAppearanceStore,
  useLocalCjkFonts,
  type CjkFontChoice,
  type CodeFont,
  type ContentSize,
  type FontSize,
  type Theme,
  type ThemeOption,
  type ThemeOptionValue,
  type ThemedColor,
  type UiFont,
} from "@multica/core/appearance";
import { useLocale, useT } from "../../i18n";
import { SegmentedToggle } from "../../common/segmented-toggle";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

type PreviewMode = "light" | "dark";
type ModeChoice = PreviewMode | "system";

const UI_FONT_NAME: Record<UiFont, string> = {
  inter: "Inter",
  system: "SF Pro / Segoe UI",
  serif: "Source Serif 4",
  mono: "Geist Mono",
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
 * Whether the active theme sets a font variable. The default font choice
 * writes nothing, so on such a theme it shows the theme's font, not its own.
 */
function useThemeSetsFont(variable: "font-ui" | "font-mono"): boolean {
  const appliedMode = useAppliedMode();
  const theme = useAppearanceStore((s) => s.theme);
  const css = activeTheme({ theme }).css;
  return useMemo(() => variable in themeModeVariables(css)[appliedMode], [css, appliedMode, variable]);
}

/** Select value that asks for the device's fonts instead of choosing one. */
const LOAD_LOCAL_FONTS = "__load_local_fonts";

/**
 * Appearance: colour (mode and theme), the active theme's options,
 * interface text, content text, and layout and accessibility. Every change
 * applies at once, so the page itself is the preview.
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
      <div className="space-y-8">
        <ColorSection />
        <ThemeOptionsSection />
        <InterfaceTextSection />
        <ContentTextSection />
        <LayoutSection />
      </div>
    </SettingsTab>
  );
}

function useAppliedMode(): PreviewMode {
  const { resolvedTheme } = useTheme();
  return resolvedTheme === "dark" ? "dark" : "light";
}

function useThemeLabel() {
  const { t } = useT("settings");
  return (theme: Theme) =>
    theme.id === "default"
      ? t(($) => $.appearance.theme.default)
      : theme.id === "pop"
        ? t(($) => $.appearance.theme.pop)
        : theme.name;
}

function ColorSection() {
  const { t } = useT("settings");
  const { theme: mode, setTheme: setMode } = useTheme();
  const appliedMode = useAppliedMode();
  const theme = useAppearanceStore((s) => s.theme);
  const set = useAppearanceStore((s) => s.set);
  const themeLabelFor = useThemeLabel();
  const modeValue: ModeChoice = mode === "light" || mode === "dark" ? mode : "system";
  const modeLabel = t(($) => $.appearance.mode.title);
  const themeLabel = t(($) => $.appearance.theme.title);
  const selected = BUILT_IN_THEMES.find((manifest) => manifest.id === theme);
  const items = BUILT_IN_THEMES.map((manifest) => ({
    value: manifest.id,
    label: themeLabelFor(manifest),
  }));

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
        <SettingsRow label={themeLabel} description={t(($) => $.appearance.theme.description)} anchor="theme">
          <Select
            items={items}
            value={selected ? theme : "default"}
            onValueChange={(next) => {
              if (next) set({ theme: next });
            }}
          >
            <SelectTrigger size="sm" className="w-48" aria-label={themeLabel}>
              <SelectValue>
                {selected ? (
                  <span className="flex min-w-0 items-center gap-2">
                    <ThemeSwatch theme={selected} mode={appliedMode} />
                    <span className="truncate">{themeLabelFor(selected)}</span>
                  </span>
                ) : null}
              </SelectValue>
            </SelectTrigger>
            <SelectContent align="end" className="min-w-48">
              {BUILT_IN_THEMES.map((manifest) => (
                <SelectItem key={manifest.id} value={manifest.id}>
                  <span className="flex min-w-0 flex-1 items-center gap-2.5">
                    <ThemeSwatch theme={manifest} mode={appliedMode} />
                    <span className="truncate">{themeLabelFor(manifest)}</span>
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}

/** The options the active theme declares, rendered from its manifest. */
function ThemeOptionsSection() {
  const { t } = useT("settings");
  const locale = useLocale();
  const prefs = useAppearanceStore(useShallow(selectAppearance));
  const setThemeOption = useAppearanceStore((s) => s.setThemeOption);
  const set = useAppearanceStore((s) => s.set);
  const themeLabelFor = useThemeLabel();
  const theme = activeTheme(prefs);
  const options = theme.options ?? [];
  if (options.length === 0) return null;
  const chosen = prefs.themeOptions[theme.id];

  return (
    <SettingsSection
      title={t(($) => $.appearance.theme_options.title)}
      description={t(($) => $.appearance.theme_options.description, { name: themeLabelFor(theme) })}
      anchor="theme-options"
      action={
        <Button
          variant="ghost"
          size="xs"
          disabled={!chosen}
          onClick={() => {
            const { [theme.id]: _removed, ...rest } = prefs.themeOptions;
            set({ themeOptions: rest });
          }}
        >
          {t(($) => $.appearance.theme_options.reset)}
        </Button>
      }
    >
      <div className="space-y-5">
        {optionGroups(options).map((group, index) => (
          <div key={group.heading?.id ?? index} className="space-y-2">
            {group.heading ? (
              <div className="px-1">
                <h3 className="text-label font-medium">{localizedText(group.heading.label, locale)}</h3>
                {group.heading.description ? (
                  <p className="text-caption text-muted-foreground">
                    {localizedText(group.heading.description, locale)}
                  </p>
                ) : null}
              </div>
            ) : null}
            {group.options.length > 0 ? (
              <SettingsCard>
                {group.options.map((option) => (
                  <ThemeOptionRow
                    key={option.id}
                    option={option}
                    locale={locale}
                    value={themeOptionValue(option, chosen?.[option.id])}
                    onChange={(value) => setThemeOption(theme.id, option.id, value)}
                  />
                ))}
              </SettingsCard>
            ) : null}
          </div>
        ))}
      </div>
    </SettingsSection>
  );
}

type HeadingOption = Extract<ThemeOption, { type: "heading" }>;

/** Splits options at each heading. */
function optionGroups(options: readonly ThemeOption[]) {
  const groups: { heading?: HeadingOption; options: Exclude<ThemeOption, HeadingOption>[] }[] = [];
  for (const option of options) {
    if (option.type === "heading") groups.push({ heading: option, options: [] });
    else if (groups.length === 0) groups.push({ options: [option] });
    else groups[groups.length - 1]!.options.push(option);
  }
  return groups;
}

function ThemeOptionRow({
  option,
  locale,
  value,
  onChange,
}: {
  option: Exclude<ThemeOption, HeadingOption>;
  locale: string;
  value: ThemeOptionValue;
  onChange: (value: ThemeOptionValue) => void;
}) {
  const { t } = useT("settings");
  const label = localizedText(option.label, locale);
  const description = localizedText(option.description, locale) || undefined;
  switch (option.type) {
    case "toggle":
      return (
        <SettingsRow label={label} description={description}>
          <Switch checked={value === true} onCheckedChange={onChange} aria-label={label} />
        </SettingsRow>
      );
    case "select": {
      const swatches = option.choices.every((choice) => choice.swatch);
      return (
        <SettingsRow label={label} description={description}>
          {swatches ? (
            <div role="radiogroup" aria-label={label} className="flex items-center gap-2.5">
              {option.choices.map((choice) => (
                <ColorSwatch
                  key={choice.value}
                  label={localizedText(choice.label, locale)}
                  color={choice.swatch!}
                  selected={value === choice.value}
                  onSelect={() => onChange(choice.value)}
                />
              ))}
            </div>
          ) : (
            <div role="group" aria-label={label}>
              <SegmentedToggle
                value={String(value)}
                onChange={onChange}
                buttonClassName="px-3 py-1 text-label"
                options={option.choices.map((choice) => [choice.value, localizedText(choice.label, locale)] as const)}
              />
            </div>
          )}
        </SettingsRow>
      );
    }
    case "variable-select": {
      const items = option.choices.map((choice) => ({ value: choice.value, label: localizedText(choice.label, locale) }));
      return (
        <SettingsRow label={label} description={description}>
          <Select items={items} value={String(value)} onValueChange={(next) => next && onChange(next)}>
            <SelectTrigger size="sm" className="w-40" aria-label={label}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent align="end">
              {items.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </SettingsRow>
      );
    }
    case "color": {
      const color = value as ThemedColor;
      return (
        <SettingsRow label={label} description={description}>
          <div role="group" aria-label={label} className="flex items-center gap-3">
            {(["light", "dark"] as const).map((mode) => {
              const modeLabel = t(($) => $.appearance.mode[mode]);
              return (
                <label key={mode} className="flex items-center gap-1.5 text-caption text-muted-foreground">
                  <input
                    type="color"
                    aria-label={`${label} (${modeLabel})`}
                    value={color[mode].length === 4 ? expandHex(color[mode]) : color[mode].slice(0, 7)}
                    onChange={(event) => onChange({ ...color, [mode]: event.target.value })}
                    className="size-6 cursor-pointer rounded-md border border-input bg-transparent p-0.5 [&::-webkit-color-swatch]:rounded-xs [&::-webkit-color-swatch]:border-0 [&::-webkit-color-swatch-wrapper]:p-0"
                  />
                  {modeLabel}
                </label>
              );
            })}
          </div>
        </SettingsRow>
      );
    }
    case "slider":
      return (
        <SettingsRow label={label} description={description} size="text">
          <div className="flex w-full items-center gap-3">
            <Slider
              aria-label={label}
              min={option.min}
              max={option.max}
              step={option.step ?? 1}
              value={value as number}
              onValueChange={(next) => {
                const n = Array.isArray(next) ? next[0] : next;
                if (typeof n === "number") onChange(n);
              }}
              className="flex-1"
            />
            <span className="w-14 shrink-0 text-right text-label tabular-nums text-muted-foreground">
              {String(value)}
              {option.unit}
            </span>
          </div>
        </SettingsRow>
      );
    case "number":
      return (
        <SettingsRow label={label} description={description}>
          <div className="flex items-center gap-1.5">
            <Input
              type="number"
              aria-label={label}
              className="h-8 w-24 text-right tabular-nums"
              min={option.min}
              max={option.max}
              step={option.step ?? "any"}
              defaultValue={String(value)}
              key={String(value)}
              onBlur={(event) => {
                const n = event.target.valueAsNumber;
                if (Number.isFinite(n)) onChange(n);
                else event.target.value = String(value);
              }}
            />
            {option.unit ? <span className="text-label text-muted-foreground">{option.unit}</span> : null}
          </div>
        </SettingsRow>
      );
    case "text":
    case "font": {
      const valid = option.type === "font" ? isSafeFontValue : isSafeThemeValue;
      return (
        <SettingsRow label={label} description={description} size="text">
          <Input
            aria-label={label}
            className="h-8"
            defaultValue={String(value)}
            key={String(value)}
            spellCheck={false}
            onKeyDown={(event) => {
              if (event.key === "Enter") event.currentTarget.blur();
            }}
            onBlur={(event) => {
              const next = event.target.value.trim();
              if (next !== value && valid(next)) onChange(next);
              else event.target.value = String(value);
            }}
          />
        </SettingsRow>
      );
    }
  }
}

/** `#abc` to `#aabbcc`, which is all a colour input accepts. */
function expandHex(hex: string): string {
  return `#${[...hex.slice(1)].map((c) => c + c).join("")}`;
}

function ColorSwatch({
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
        // eslint-disable-next-line no-restricted-syntax -- color swatch
        "flex size-5 items-center justify-center rounded-full text-white outline-none ring-offset-2 ring-offset-surface transition-shadow focus-visible:ring-2 focus-visible:ring-ring",
        selected && "ring-2 ring-foreground",
      )}
    >
      <span className="sr-only">{label}</span>
    </button>
  );
}

function InterfaceTextSection() {
  const { t } = useT("settings");
  const { uiFont, uiCjkFont, fontSize, set } = useAppearanceStore(
    useShallow((s) => ({ uiFont: s.uiFont, uiCjkFont: s.uiCjkFont, fontSize: s.fontSize, set: s.set })),
  );
  const themeUiFont = useThemeSetsFont("font-ui");
  const sizeLabel = t(($) => $.appearance.font_size.title);

  return (
    <SettingsSection
      title={t(($) => $.appearance.ui_text.title)}
      description={t(($) => $.appearance.ui_text.description)}
      anchor="ui-text"
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.appearance.latin_font.title)} anchor="ui-latin-font" size="select">
          <LatinFontSelect value={uiFont} themeFont={themeUiFont} onChange={(next) => set({ uiFont: next as UiFont })} />
        </SettingsRow>
        <SettingsRow
          label={t(($) => $.appearance.cjk_font.title)}
          description={t(($) => $.appearance.cjk_font.description)}
          anchor="ui-cjk-font"
          size="select"
        >
          <CjkFontSelect value={uiCjkFont} onChange={(next) => set({ uiCjkFont: next as CjkFontChoice })} />
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

function ContentTextSection() {
  const { t } = useT("settings");
  const { contentFont, contentCjkFont, contentSize, contentLeading, codeFont, fontSize, set } =
    useAppearanceStore(
      useShallow((s) => ({
        contentFont: s.contentFont,
        contentCjkFont: s.contentCjkFont,
        contentSize: s.contentSize,
        contentLeading: s.contentLeading,
        codeFont: s.codeFont,
        fontSize: s.fontSize,
        set: s.set,
      })),
    );
  const codeLabel = t(($) => $.appearance.code_font.title);
  const sizeLabel = t(($) => $.appearance.content_size.title);
  const leadingLabel = t(($) => $.appearance.content_leading.title);
  const themeCodeFont = useThemeSetsFont("font-mono");
  const themeFontLabel = t(($) => $.appearance.theme_font);
  const fromTheme = (id: CodeFont) => themeCodeFont && id === DEFAULT_APPEARANCE.codeFont;
  const codeName = (id: CodeFont) => (fromTheme(id) ? themeFontLabel : CODE_FONT_NAME[id]);
  const codeSample = (id: CodeFont) => (fromTheme(id) ? "var(--font-mono)" : CODE_FONT_SAMPLE[id]);
  const codeOptions = CODE_FONTS.map((id) => ({ value: id, label: codeName(id) }));
  const sizeName = (size: ContentSize) =>
    size === FOLLOW_UI
      ? t(($) => $.appearance.content_size.follow, { size: fontSize })
      : t(($) => $.appearance.font_size.value, { size });
  const sizeOptions = ([FOLLOW_UI, ...CONTENT_SIZES] as ContentSize[]).map((size) => ({
    value: String(size),
    label: sizeName(size),
  }));

  return (
    <SettingsSection
      title={t(($) => $.appearance.content_text.title)}
      description={t(($) => $.appearance.content_text.description)}
      anchor="content-text"
    >
      <SettingsCard>
        <div className="rich-text-editor bg-page-canvas px-4 py-3.5 text-body" data-appearance-content-sample="">
          <p>{t(($) => $.appearance.content_text.sample)}</p>
        </div>
        <SettingsRow label={t(($) => $.appearance.latin_font.title)} anchor="content-latin-font" size="select">
          <LatinFontSelect value={contentFont} follow onChange={(next) => set({ contentFont: next as UiFont })} />
        </SettingsRow>
        <SettingsRow
          label={t(($) => $.appearance.cjk_font.title)}
          description={t(($) => $.appearance.cjk_font.description)}
          anchor="content-cjk-font"
          size="select"
        >
          <CjkFontSelect
            value={contentCjkFont}
            follow
            onChange={(next) => set({ contentCjkFont: next as CjkFontChoice })}
          />
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
                <span style={{ fontFamily: codeSample(codeFont) }}>{codeName(codeFont)}</span>
              </SelectValue>
            </SelectTrigger>
            <SelectContent align="end">
              {CODE_FONTS.map((id) => (
                <SelectItem key={id} value={id}>
                  <span className="flex min-w-0 flex-1 items-baseline justify-between gap-3">
                    <span style={{ fontFamily: codeSample(id) }}>{codeName(id)}</span>
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
          description={t(($) => $.appearance.content_size.description)}
          anchor="content-size"
          size="select"
        >
          <Select
            items={sizeOptions}
            value={String(contentSize)}
            onValueChange={(next) => {
              if (!next) return;
              const size = next === FOLLOW_UI ? FOLLOW_UI : Number(next);
              if (size === FOLLOW_UI || (CONTENT_SIZES as readonly number[]).includes(size)) {
                set({ contentSize: size as ContentSize });
              }
            }}
          >
            <SelectTrigger size="sm" className="w-full" aria-label={sizeLabel}>
              <SelectValue>{sizeName(contentSize)}</SelectValue>
            </SelectTrigger>
            <SelectContent align="end">
              {sizeOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </SettingsRow>
        <SettingsRow label={leadingLabel} anchor="content-leading">
          <div role="group" aria-label={leadingLabel}>
            <SegmentedToggle
              value={contentLeading}
              onChange={(next) => set({ contentLeading: next })}
              buttonClassName="px-3 py-1 text-label"
              options={CONTENT_LEADINGS.map((id) => [id, t(($) => $.appearance.content_leading[id])] as const)}
            />
          </div>
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}

/** Latin faces, each drawn in itself; `follow` adds "same as interface" for content. */
function LatinFontSelect({
  value,
  follow = false,
  themeFont = false,
  onChange,
}: {
  value: string;
  follow?: boolean;
  /** The active theme sets the interface font, which the default choice keeps. */
  themeFont?: boolean;
  onChange: (value: string) => void;
}) {
  const { t } = useT("settings");
  const label = t(($) => $.appearance.latin_font.title);
  const followLabel = t(($) => $.appearance.follow_ui);
  const themeFontLabel = t(($) => $.appearance.theme_font);
  const fromTheme = (id: string) => themeFont && id === DEFAULT_APPEARANCE.uiFont;
  const name = (id: string) =>
    id === FOLLOW_UI ? followLabel : fromTheme(id) ? themeFontLabel : UI_FONT_NAME[id as UiFont];
  const family = (id: string) =>
    id === FOLLOW_UI ? undefined : fromTheme(id) ? "var(--font-ui)" : uiFontStack(id as UiFont);
  const ids: string[] = [...(follow ? [FOLLOW_UI] : []), ...UI_FONTS];
  return (
    <Select
      items={ids.map((id) => ({ value: id, label: name(id) }))}
      value={value}
      onValueChange={(next) => {
        if (next) onChange(next);
      }}
    >
      <SelectTrigger size="sm" className="w-full" aria-label={label}>
        <SelectValue>
          <span style={{ fontFamily: family(value) }}>{name(value)}</span>
        </SelectValue>
      </SelectTrigger>
      <SelectContent align="end">
        {ids.map((id) => (
          <SelectItem key={id} value={id}>
            <span className="flex min-w-0 flex-1 items-baseline justify-between gap-3">
              <span style={{ fontFamily: family(id) }}>{name(id)}</span>
              {id === FOLLOW_UI ? null : (
                <span className="text-caption text-muted-foreground">
                  {t(($) => $.appearance.ui_font[id as UiFont])}
                </span>
              )}
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

/**
 * Built-in CJK faces, then families installed on this device (listed on
 * request, since reading them can ask for permission). Every option shows a
 * sample in its own face.
 */
function CjkFontSelect({
  value,
  follow = false,
  onChange,
}: {
  value: string;
  follow?: boolean;
  onChange: (value: string) => void;
}) {
  const { t } = useT("settings");
  const local = useLocalCjkFonts();
  const label = t(($) => $.appearance.cjk_font.title);
  const sample = t(($) => $.appearance.cjk_font.sample);
  const followLabel = t(($) => $.appearance.follow_ui);
  const localValues = local.families.map((family) => `local:${family}`);
  if (isLocalFont(value) && !localValues.includes(value)) localValues.unshift(value);

  const name = (id: string) =>
    id === FOLLOW_UI
      ? followLabel
      : isLocalFont(id)
        ? localFontFamily(id)
        : t(($) => $.appearance.cjk_font[id as (typeof CJK_FONTS)[number]]);
  const family = (id: string) => (id === FOLLOW_UI ? undefined : cjkFontStack(id as CjkFontChoice));
  const builtIn: string[] = [...(follow ? [FOLLOW_UI] : []), ...CJK_FONTS];
  const items = [...builtIn, ...localValues].map((id) => ({ value: id, label: name(id) }));

  const row = (id: string) => (
    <SelectItem key={id} value={id}>
      <span className="flex min-w-0 flex-1 items-baseline justify-between gap-4">
        <span className="truncate" style={{ fontFamily: family(id) }}>
          {name(id)}
        </span>
        {id === FOLLOW_UI ? null : (
          <span className="shrink-0 text-caption text-muted-foreground" style={{ fontFamily: family(id) }}>
            {sample}
          </span>
        )}
      </span>
    </SelectItem>
  );

  const localStatus =
    local.status === "loading"
      ? t(($) => $.appearance.cjk_font.loading)
      : local.status === "ready" && local.families.length === 0
        ? t(($) => $.appearance.cjk_font.none)
        : local.status === "error"
          ? t(($) => $.appearance.cjk_font.load_failed)
          : null;

  return (
    <Select
      items={items}
      value={value}
      onValueChange={(next) => {
        if (!next) return;
        if (next === LOAD_LOCAL_FONTS) void local.load();
        else onChange(next);
      }}
    >
      <SelectTrigger size="sm" className="w-full" aria-label={label}>
        <SelectValue>
          <span style={{ fontFamily: family(value) }}>{name(value)}</span>
        </SelectValue>
      </SelectTrigger>
      <SelectContent align="end" className="min-w-80">
        <SelectGroup>
          <SelectLabel>{t(($) => $.appearance.cjk_font.built_in)}</SelectLabel>
          {builtIn.map(row)}
        </SelectGroup>
        {local.status !== "unavailable" || localValues.length > 0 ? (
          <>
            <SelectSeparator />
            <SelectGroup>
              <SelectLabel>{t(($) => $.appearance.cjk_font.local)}</SelectLabel>
              {localValues.map(row)}
              {local.status === "idle" || local.status === "error" ? (
                <SelectItem value={LOAD_LOCAL_FONTS}>
                  <span className="text-muted-foreground">{t(($) => $.appearance.cjk_font.load_local)}</span>
                </SelectItem>
              ) : null}
              {localStatus ? (
                <div className="px-2 py-1.5 text-caption text-muted-foreground">{localStatus}</div>
              ) : null}
            </SelectGroup>
          </>
        ) : null}
      </SelectContent>
    </Select>
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

type SwatchTokens = Record<"app-shell" | "page-canvas" | "surface-border" | "foreground" | "brand", string>;

/**
 * The tokens a theme swatch needs, as tokens.css sets them for the default
 * theme. A theme only overrides some of them, so swatches start from these.
 */
const THEME_SWATCH_BASE: Record<PreviewMode, SwatchTokens> = {
  light: {
    "app-shell": "oklch(0.964 0.001 286)",
    "page-canvas": "oklch(0.988 0 0)",
    "surface-border": "oklch(0.92 0.004 286)",
    foreground: "oklch(0.141 0.005 286)",
    brand: "oklch(0.561 0.194 256.4)",
  },
  dark: {
    "app-shell": "oklch(0.155 0.005 286)",
    "page-canvas": "oklch(0.18 0.005 286)",
    "surface-border": "oklch(0.3 0.006 286)",
    foreground: "oklch(0.985 0 0)",
    brand: "oklch(0.65 0.16 255)",
  },
};

const swatchCache = new WeakMap<Theme, ReturnType<typeof themeModeVariables>>();

/**
 * Resolves `var()` references a swatch can know without the theme applied:
 * the theme's own variables for the mode and its colour options' defaults.
 */
function resolveSwatchValue(value: string, theme: Theme, variables: Record<string, string>, mode: PreviewMode, depth = 0): string {
  return value.replace(/var\(--([a-z0-9-]+)\)/g, (reference, name: string) => {
    const option = theme.options?.find((o) => o.id === name);
    if (option?.type === "color") return option.default[mode];
    const own = variables[name];
    return own && depth < 4 ? resolveSwatchValue(own, theme, variables, mode, depth + 1) : reference;
  });
}

function swatchTokens(theme: Theme, mode: PreviewMode): SwatchTokens {
  let variables = swatchCache.get(theme);
  if (!variables) {
    variables = themeModeVariables(theme.css);
    swatchCache.set(theme, variables);
  }
  const tokens = { ...THEME_SWATCH_BASE[mode] };
  for (const name of Object.keys(tokens) as (keyof SwatchTokens)[]) {
    const own = variables[mode][name];
    if (own) tokens[name] = resolveSwatchValue(own, theme, variables[mode], mode);
  }
  return tokens;
}

/** A theme in miniature: its frame, page, ink and brand colour. */
function ThemeSwatch({ theme, mode }: { theme: Theme; mode: PreviewMode }) {
  const tokens = swatchTokens(theme, mode);
  return (
    <span
      aria-hidden="true"
      data-theme-swatch={theme.id}
      data-swatch-mode={mode}
      className="flex h-4 w-6 shrink-0 items-end overflow-hidden rounded-xs p-[2px] ring-1 ring-foreground/10"
      style={{ background: tokens["app-shell"] }}
    >
      <span
        className="flex size-full flex-col justify-center gap-[2px] rounded-[calc(var(--radius-xs)/2)] px-[2px]"
        style={{ background: tokens["page-canvas"], boxShadow: `inset 0 0 0 0.5px ${tokens["surface-border"]}` }}
      >
        <span className="h-[2px] w-3/5 rounded-full" style={{ background: tokens.foreground }} />
        <span className="h-[2px] w-2/5 rounded-full" style={{ background: tokens.brand }} />
      </span>
    </span>
  );
}
