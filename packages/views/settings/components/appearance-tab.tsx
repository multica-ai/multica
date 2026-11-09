"use client";

import { useId, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { useShallow } from "zustand/react/shallow";
import {
  Check,
  Monitor,
  Moon,
  RotateCcw,
  Sun,
  TriangleAlert,
  Upload,
  X,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
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
import { Slider } from "@multica/ui/components/ui/slider";
import { Switch } from "@multica/ui/components/ui/switch";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import {
  ACCENT_COLORS,
  ACCENTS,
  BUILT_IN_THEMES,
  CJK_FONTS,
  CODE_FONTS,
  CONTENT_LEADINGS,
  CONTENT_SIZES,
  DEFAULT_FONT_SIZE,
  DENSITIES,
  FOLLOW_UI,
  FONT_SIZES,
  THEME_PACKAGE_MAX_BYTES,
  UI_FONTS,
  activeTheme,
  cjkFontStack,
  isDefaultAppearance,
  isLocalFont,
  localFontFamily,
  localizedText,
  parseThemePackage,
  selectAppearance,
  themeOptionValue,
  uiFontStack,
  useAppearanceStore,
  useInstalledThemesStore,
  useThemeSafeMode,
  type CjkFontChoice,
  type CodeFont,
  type ContentSize,
  type FontSize,
  type ThemeManifest,
  type ThemeOption,
  type ThemeTokens,
  type UiFont,
} from "@multica/core/appearance";
import { useLocale, useT } from "../../i18n";
import { SegmentedToggle } from "../../common/segmented-toggle";
import {
  SETTINGS_ANCHOR_ATTR,
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";
import { useLocalCjkFonts } from "./local-fonts";

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
/** Select value that asks for the device's fonts instead of choosing one. */
const LOAD_LOCAL_FONTS = "__load_local_fonts";

/**
 * Appearance: colour (mode, theme, accent), the active theme's options,
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

/** A card row whose control sits under the label rather than beside it. */
function SettingsBlock({
  label,
  description,
  anchor,
  action,
  children,
}: {
  label: string;
  description?: string;
  anchor: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div {...{ [SETTINGS_ANCHOR_ATTR]: anchor }} className="scroll-mt-6 space-y-3 px-4 py-3.5">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="text-body font-medium">{label}</div>
          {description ? (
            <div className="mt-0.5 text-caption leading-5 text-muted-foreground">{description}</div>
          ) : null}
        </div>
        {action}
      </div>
      {children}
    </div>
  );
}

function useThemeLabel() {
  const { t } = useT("settings");
  return (theme: ThemeManifest) =>
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
  const accent = useAppearanceStore((s) => s.accent);
  const set = useAppearanceStore((s) => s.set);
  const installed = useInstalledThemesStore((s) => s.themes);
  const uninstall = useInstalledThemesStore((s) => s.uninstall);
  const safeMode = useThemeSafeMode();
  const themeLabelFor = useThemeLabel();
  const modeValue: ModeChoice = mode === "light" || mode === "dark" ? mode : "system";
  const modeLabel = t(($) => $.appearance.mode.title);
  const themeLabel = t(($) => $.appearance.theme.title);
  const accentLabel = t(($) => $.appearance.accent.title);
  const current = activeTheme({ theme }, safeMode.active);
  const themeBrand = current.tokens[appliedMode].brand ?? PREVIEW_BASE[appliedMode].brand;
  const themes = [...BUILT_IN_THEMES, ...installed];

  const remove = (id: string) => {
    if (theme === id) set({ theme: "default" });
    uninstall(id);
  };

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
          action={<ImportThemeButton />}
        >
          {safeMode.active ? (
            <div
              role="status"
              className="flex items-center gap-3 rounded-md bg-muted px-3 py-2 text-caption"
            >
              <TriangleAlert aria-hidden="true" className="size-3.5 shrink-0 text-warning" />
              <span className="min-w-0 flex-1">{t(($) => $.appearance.theme.safe_mode)}</span>
              <Button variant="outline" size="xs" onClick={safeMode.exit}>
                {t(($) => $.appearance.theme.safe_mode_exit)}
              </Button>
            </div>
          ) : null}
          <div role="group" aria-label={themeLabel} className="grid grid-cols-2 gap-3 sm:grid-cols-3">
            {themes.map((manifest) => {
              const label = themeLabelFor(manifest);
              const removable = installed.includes(manifest);
              return (
                <div key={manifest.id} className="relative min-w-0">
                  <OptionCard
                    label={label}
                    caption={removable ? manifest.author : undefined}
                    selected={theme === manifest.id}
                    onSelect={() => set({ theme: manifest.id })}
                    preview={<ThemePreview theme={manifest} mode={appliedMode} />}
                  />
                  {removable ? (
                    <Button
                      variant="secondary"
                      size="icon-xs"
                      className="absolute top-1.5 right-1.5 shadow-sm"
                      aria-label={t(($) => $.appearance.theme.remove, { name: label })}
                      title={t(($) => $.appearance.theme.remove, { name: label })}
                      onClick={() => remove(manifest.id)}
                    >
                      <X />
                    </Button>
                  ) : null}
                </div>
              );
            })}
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

/**
 * Reads a theme file, shows what it is and that it can restyle every page,
 * and installs it on confirmation. Problems are listed as the checks report
 * them, which is what someone writing a theme needs.
 */
function ImportThemeButton() {
  const { t } = useT("settings");
  const inputRef = useRef<HTMLInputElement>(null);
  const [pending, setPending] = useState<ThemeManifest | null>(null);
  const [errors, setErrors] = useState<string[] | null>(null);
  const installed = useInstalledThemesStore((s) => s.themes);
  const install = useInstalledThemesStore((s) => s.install);
  const uninstall = useInstalledThemesStore((s) => s.uninstall);
  const set = useAppearanceStore((s) => s.set);
  const previous = pending ? installed.find((theme) => theme.id === pending.id) : undefined;

  const readFile = async (file: File) => {
    if (file.size > THEME_PACKAGE_MAX_BYTES) {
      setErrors([t(($) => $.appearance.import.too_large, { size: THEME_PACKAGE_MAX_BYTES / 1024 })]);
      return;
    }
    let json: unknown;
    try {
      json = JSON.parse(await file.text());
    } catch {
      setErrors([t(($) => $.appearance.import.not_json)]);
      return;
    }
    const result = parseThemePackage(json);
    if (result.ok) setPending(result.theme);
    else setErrors(result.errors);
  };

  const confirm = () => {
    if (!pending) return;
    const replaced = previous;
    try {
      install(pending);
      set({ theme: pending.id });
    } catch {
      if (replaced) install(replaced);
      else uninstall(pending.id);
      setErrors([t(($) => $.appearance.import.storage_full)]);
    }
    setPending(null);
  };

  return (
    <>
      <Button variant="outline" size="sm" onClick={() => inputRef.current?.click()}>
        <Upload />
        {t(($) => $.appearance.import.button)}
      </Button>
      <input
        ref={inputRef}
        type="file"
        accept=".json,application/json"
        className="hidden"
        onChange={(event) => {
          const file = event.target.files?.[0];
          event.target.value = "";
          if (file) void readFile(file);
        }}
      />
      <AlertDialog open={pending !== null} onOpenChange={(open) => !open && setPending(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.appearance.import.confirm_title, { name: pending?.name ?? "" })}
            </AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.appearance.import.warning)}</AlertDialogDescription>
          </AlertDialogHeader>
          {pending ? (
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 rounded-md bg-muted px-3 py-2.5 text-caption">
              {pending.author ? (
                <>
                  <dt className="text-muted-foreground">{t(($) => $.appearance.import.author)}</dt>
                  <dd className="min-w-0 truncate">{pending.author}</dd>
                </>
              ) : null}
              {pending.version ? (
                <>
                  <dt className="text-muted-foreground">{t(($) => $.appearance.import.version)}</dt>
                  <dd>
                    {previous?.version
                      ? t(($) => $.appearance.import.replaces, { from: previous.version, to: pending.version })
                      : pending.version}
                  </dd>
                </>
              ) : null}
              <dt className="text-muted-foreground">{t(($) => $.appearance.import.includes)}</dt>
              <dd>
                {[
                  t(($) => $.appearance.import.has_tokens),
                  pending.css ? t(($) => $.appearance.import.has_css) : null,
                  pending.options ? t(($) => $.appearance.import.has_options, { count: pending.options.length }) : null,
                ]
                  .filter(Boolean)
                  .join(" · ")}
              </dd>
            </dl>
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.appearance.import.cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={confirm}>{t(($) => $.appearance.import.confirm)}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={errors !== null} onOpenChange={(open) => !open && setErrors(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.appearance.import.error_title)}</AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.appearance.import.error_description)}</AlertDialogDescription>
          </AlertDialogHeader>
          <ul className="max-h-60 list-disc space-y-1 overflow-y-auto rounded-md bg-muted py-2.5 pr-3 pl-7 font-mono text-caption">
            {errors?.map((error, i) => <li key={i}>{error}</li>)}
          </ul>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.appearance.import.close)}</AlertDialogCancel>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

/** The options the active theme declares, rendered from its manifest. */
function ThemeOptionsSection() {
  const { t } = useT("settings");
  const locale = useLocale();
  const prefs = useAppearanceStore(useShallow(selectAppearance));
  const setThemeOption = useAppearanceStore((s) => s.setThemeOption);
  const set = useAppearanceStore((s) => s.set);
  useInstalledThemesStore((s) => s.themes);
  const safeMode = useThemeSafeMode((s) => s.active);
  const themeLabelFor = useThemeLabel();
  const theme = activeTheme(prefs, safeMode);
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
      <SettingsCard>
        {options.map((option) => (
          <ThemeOptionRow
            key={option.id}
            option={option}
            locale={locale}
            value={themeOptionValue(option, chosen?.[option.id])}
            onChange={(value) => setThemeOption(theme.id, option.id, value)}
          />
        ))}
      </SettingsCard>
    </SettingsSection>
  );
}

function ThemeOptionRow({
  option,
  locale,
  value,
  onChange,
}: {
  option: ThemeOption;
  locale: string;
  value: string | boolean;
  onChange: (value: string | boolean) => void;
}) {
  const label = localizedText(option.label, locale);
  const description = localizedText(option.description, locale) || undefined;
  if (option.type === "toggle") {
    return (
      <SettingsRow label={label} description={description}>
        <Switch checked={value === true} onCheckedChange={onChange} aria-label={label} />
      </SettingsRow>
    );
  }
  const swatches = option.choices.every((choice) => choice.swatch);
  return (
    <SettingsRow label={label} description={description}>
      {swatches ? (
        <div role="radiogroup" aria-label={label} className="flex items-center gap-2.5">
          {option.choices.map((choice) => (
            <AccentSwatch
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

function InterfaceTextSection() {
  const { t } = useT("settings");
  const { uiFont, uiCjkFont, fontSize, set } = useAppearanceStore(
    useShallow((s) => ({ uiFont: s.uiFont, uiCjkFont: s.uiCjkFont, fontSize: s.fontSize, set: s.set })),
  );
  const sizeLabel = t(($) => $.appearance.font_size.title);

  return (
    <SettingsSection
      title={t(($) => $.appearance.ui_text.title)}
      description={t(($) => $.appearance.ui_text.description)}
      anchor="ui-text"
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.appearance.latin_font.title)} anchor="ui-latin-font" size="select">
          <LatinFontSelect value={uiFont} onChange={(next) => set({ uiFont: next as UiFont })} />
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
  const codeOptions = CODE_FONTS.map((id) => ({ value: id, label: CODE_FONT_NAME[id] }));
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
                <span style={{ fontFamily: CODE_FONT_SAMPLE[codeFont] }}>{CODE_FONT_NAME[codeFont]}</span>
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
  onChange,
}: {
  value: string;
  follow?: boolean;
  onChange: (value: string) => void;
}) {
  const { t } = useT("settings");
  const label = t(($) => $.appearance.latin_font.title);
  const followLabel = t(($) => $.appearance.follow_ui);
  const name = (id: string) => (id === FOLLOW_UI ? followLabel : UI_FONT_NAME[id as UiFont]);
  const family = (id: string) => (id === FOLLOW_UI ? undefined : uiFontStack(id as UiFont));
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

function OptionCard({
  label,
  caption,
  selected,
  onSelect,
  preview,
}: {
  label: string;
  caption?: string;
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
      className="group/option flex w-full min-w-0 flex-col gap-2 rounded-xl text-left outline-none"
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
        {caption ? <span className="truncate text-caption text-muted-foreground">{caption}</span> : null}
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

function previewVariables(theme: ThemeManifest, mode: PreviewMode): CSSProperties {
  const tokens = { ...PREVIEW_BASE[mode], ...theme.tokens[mode] };
  const frame = theme.frame?.[mode];
  const ink = tokens.foreground;
  const canvas = tokens["page-canvas"];
  const radiusRem = Number.parseFloat(tokens.radius) || 0.5;
  return {
    "--pv-shell": tokens["app-shell"],
    "--pv-rail": `color-mix(in oklab, ${frame?.foreground ?? ink} 28%, ${tokens["app-shell"]})`,
    "--pv-canvas": canvas,
    "--pv-line": tokens["surface-border"],
    "--pv-ink": ink,
    "--pv-faint": `color-mix(in oklab, ${ink} 16%, ${canvas})`,
    "--pv-selected": tokens["surface-selected"],
    "--pv-band": tokens.muted,
    "--pv-dot-a": "oklch(0.75 0.15 75)",
    "--pv-dot-b": `color-mix(in oklab, ${ink} 40%, ${canvas})`,
    "--pv-primary": tokens["button-primary"] ?? tokens.primary,
    "--pv-primary-line": tokens["button-primary-border"] ?? "transparent",
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
            <span className="h-[3.6cqw] w-[11cqw] rounded-[0.9cqw] border border-(--pv-primary-line) bg-(--pv-primary)" />
          </span>
          <PreviewGroup dot="a" rows={["w-3/4", "w-1/2"]} />
          <PreviewGroup dot="b" rows={["w-2/3", "w-3/5"]} />
        </span>
      </span>
    </span>
  );
}

function PreviewGroup({ dot, rows }: { dot: "a" | "b"; rows: string[] }) {
  return (
    <>
      <span className="flex h-[4.2cqw] items-center gap-[1.2cqw] rounded-[0.9cqw] bg-(--pv-band) px-[1.4cqw]">
        <span className={cn("size-[2cqw] rounded-full", dot === "a" ? "bg-(--pv-dot-a)" : "bg-(--pv-dot-b)")} />
        <span className="h-[1.6cqw] w-[22%] rounded-full bg-(--pv-ink)" />
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
