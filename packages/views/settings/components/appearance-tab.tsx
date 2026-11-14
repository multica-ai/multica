"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useShallow } from "zustand/react/shallow";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  BookOpen,
  ChevronDown,
  Copy,
  FileArchive,
  FolderInput,
  FolderOpen,
  Monitor,
  Moon,
  RotateCcw,
  Sun,
  TriangleAlert,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
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
  exportTheme,
  isDefaultAppearance,
  isLocalFont,
  isSafeFontValue,
  isSafeThemeValue,
  localFontFamily,
  localizedText,
  readThemePackage,
  selectAppearance,
  themeCopyTarget,
  themeFromFiles,
  themeModeVariables,
  themeOptionValue,
  uiFontStack,
  useAppearanceStore,
  useLocalCjkFonts,
  useInstalledThemesStore,
  useThemeFolderStatus,
  useThemeSafeMode,
  writeZip,
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
import { storeThemeState, storeThemesOptions } from "@multica/core/appearance/theme-store";
import { useConfigStore } from "@multica/core/config";
import { useLocale, useT } from "../../i18n";
import { SegmentedToggle } from "../../common/segmented-toggle";
import { docsLocalePrefix } from "../../common/docs-locale";
import { openExternal } from "../../platform/open-external";
import { themeFolderAPI } from "../../platform/theme-folder";
import { useThemeInstallDialog } from "../../common/theme-install-dialog";
import { ThemesButton, useStoreThemeInstall } from "./themes-dialog";
import { ThemeSwatch, useAppliedMode, useColorThemes, useThemeLabel, type PreviewMode } from "./theme-swatch";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

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
  const safeMode = useThemeSafeMode();
  const css = activeTheme({ theme }, safeMode.active).css;
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

function ColorSection() {
  const { t } = useT("settings");
  const { theme: mode, setTheme: setMode } = useTheme();
  const appliedMode = useAppliedMode();
  const theme = useAppearanceStore((s) => s.theme);
  const set = useAppearanceStore((s) => s.set);
  const installed = useInstalledThemesStore((s) => s.themes);
  const folders = useInstalledThemesStore((s) => s.folders);
  const folderProblems = useThemeFolderStatus((s) => s.problems);
  const safeMode = useThemeSafeMode();
  const themeLabelFor = useThemeLabel();
  const modeValue: ModeChoice = mode === "light" || mode === "dark" ? mode : "system";
  const modeLabel = t(($) => $.appearance.mode.title);
  const themeLabel = t(($) => $.appearance.theme.title);
  const imported = installed.filter((manifest) => !(manifest.id in folders));
  const fromFolder = installed.filter((manifest) => manifest.id in folders);
  const { themes, selected } = useColorThemes();
  const items = themes.map((manifest) => ({
    value: manifest.id,
    label: themeLabelFor(manifest),
  }));

  const option = (manifest: Theme, author?: string) => (
    <SelectItem key={manifest.id} value={manifest.id}>
      <span className="flex min-w-0 flex-1 items-center gap-2.5">
        <ThemeSwatch theme={manifest} mode={appliedMode} />
        <span className="truncate">{themeLabelFor(manifest)}</span>
        {author ? (
          <span className="ml-auto shrink-0 pl-3 text-caption text-muted-foreground">{author}</span>
        ) : null}
      </span>
    </SelectItem>
  );

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
          <div className="flex items-center gap-2">
            <Select
              items={items}
              value={selected.id}
              onValueChange={(next) => {
                if (next) set({ theme: next });
              }}
            >
              <SelectTrigger size="sm" className="w-48" aria-label={themeLabel}>
                <SelectValue>
                  <span className="flex min-w-0 items-center gap-2">
                    <ThemeSwatch theme={selected} mode={appliedMode} />
                    <span className="truncate">{themeLabelFor(selected)}</span>
                  </span>
                </SelectValue>
              </SelectTrigger>
              <SelectContent align="end" className="min-w-64">
                <SelectGroup>
                  <SelectLabel>{t(($) => $.appearance.theme.built_in)}</SelectLabel>
                  {BUILT_IN_THEMES.map((manifest) => option(manifest))}
                </SelectGroup>
                {imported.length > 0 ? (
                  <>
                    <SelectSeparator />
                    <SelectGroup>
                      <SelectLabel>{t(($) => $.appearance.theme.installed)}</SelectLabel>
                      {imported.map((manifest) => option(manifest, manifest.author))}
                    </SelectGroup>
                  </>
                ) : null}
                {fromFolder.length > 0 ? (
                  <>
                    <SelectSeparator />
                    <SelectGroup>
                      <SelectLabel>{t(($) => $.appearance.theme.folder)}</SelectLabel>
                      {fromFolder.map((manifest) => option(manifest, folders[manifest.id]))}
                    </SelectGroup>
                  </>
                ) : null}
              </SelectContent>
            </Select>
            <ThemesButton />
          </div>
        </SettingsRow>
        <StoreThemesRow />
        <OwnThemeRow theme={activeTheme({ theme }, safeMode.active)} />
        {folderProblems.length > 0 ? (
          <div role="status" className="space-y-1.5 px-4 py-3 text-caption">
            <div className="flex items-center gap-2">
              <TriangleAlert aria-hidden="true" className="size-3.5 shrink-0 text-warning" />
              <span>{t(($) => $.appearance.theme.folder_problems)}</span>
            </div>
            <ul className="space-y-1 pl-5.5 font-mono text-muted-foreground">
              {folderProblems.flatMap(({ folder, errors }) =>
                errors.map((error, i) => (
                  <li key={`${folder}:${i}`} className="break-words">
                    <span className="text-foreground">{folder}</span> {error}
                  </li>
                )),
              )}
            </ul>
          </div>
        ) : null}
        {safeMode.active ? (
          <div role="status" className="flex items-center gap-3 px-4 py-3 text-caption">
            <TriangleAlert aria-hidden="true" className="size-3.5 shrink-0 text-warning" />
            <span className="min-w-0 flex-1">{t(($) => $.appearance.theme.safe_mode)}</span>
            <Button variant="outline" size="xs" onClick={safeMode.exit}>
              {t(($) => $.appearance.theme.safe_mode_exit)}
            </Button>
          </div>
        ) : null}
      </SettingsCard>
    </SettingsSection>
  );
}

interface ThemeImport {
  chooseZip: () => void;
  chooseFolder: () => void;
  /** The file pickers and the confirm and error dialogs; render it once. */
  element: React.ReactNode;
}

/** Reads a theme from a .zip or a folder and offers it to install. */
function useThemeImport(): ThemeImport {
  const { t } = useT("settings");
  const zipRef = useRef<HTMLInputElement>(null);
  const folderRef = useRef<HTMLInputElement>(null);
  const dialog = useThemeInstallDialog();

  useEffect(() => {
    // Not in React's input props; it makes the picker choose a folder.
    folderRef.current?.setAttribute("webkitdirectory", "");
  }, []);

  const read = async (files: File[]) => {
    if (files.length === 0) return;
    dialog.offer(await readThemePackage(files));
  };

  const picker = (ref: React.RefObject<HTMLInputElement | null>, props: React.ComponentProps<"input">) => (
    <input
      ref={ref}
      type="file"
      className="hidden"
      onChange={(event) => {
        const files = Array.from(event.target.files ?? []);
        event.target.value = "";
        void read(files);
      }}
      {...props}
    />
  );

  const element = (
    <>
      {picker(zipRef, { accept: ".zip,application/zip", "aria-label": t(($) => $.appearance.import.from_zip) })}
      {picker(folderRef, { multiple: true, "aria-label": t(($) => $.appearance.import.from_folder) })}
      {dialog.element}
    </>
  );
  return { chooseZip: () => zipRef.current?.click(), chooseFolder: () => folderRef.current?.click(), element };
}

function themeGuideUrl(language: string): string {
  return `https://multica.ai/docs${docsLocalePrefix(language)}/developers/themes`;
}

function downloadFile(data: Uint8Array<ArrayBuffer>, name: string, type: string) {
  const url = URL.createObjectURL(new Blob([data], { type }));
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

/**
 * Themes installed from the store, and their updates. The store is only
 * asked when this is clicked or the Manage window has been opened, and not
 * at all when the server does not serve it.
 */
function StoreThemesRow() {
  const { t } = useT("settings");
  const enabled = useConfigStore((s) => s.themeStoreEnabled);
  const themes = useInstalledThemesStore((s) => s.themes);
  const fromStore = useInstalledThemesStore((s) => s.fromStore);
  const listing = useQuery(storeThemesOptions(false));
  const install = useStoreThemeInstall();
  const [updating, setUpdating] = useState(false);
  const count = themes.filter((theme) => theme.id in fromStore).length;
  if (!enabled || count === 0) return null;
  const updates = (listing.data ?? []).filter((theme) => storeThemeState(theme, { themes, fromStore }) === "update");

  const check = async () => {
    const result = await listing.refetch();
    if (result.isError || !result.data) {
      toast.error(t(($) => $.appearance.store.error));
      return;
    }
    const current = useInstalledThemesStore.getState();
    if (!result.data.some((theme) => storeThemeState(theme, current) === "update")) {
      toast.success(t(($) => $.appearance.updates.up_to_date));
    }
  };
  const updateAll = async () => {
    setUpdating(true);
    try {
      for (const theme of updates) await install(theme);
    } finally {
      setUpdating(false);
    }
  };

  return (
    <SettingsRow
      label={t(($) => $.appearance.updates.title)}
      description={t(($) => $.appearance.updates.description, { count })}
      anchor="theme-updates"
    >
      {updates.length > 0 ? (
        <Button size="sm" disabled={updating} onClick={() => void updateAll()}>
          {updating
            ? t(($) => $.appearance.store.installing)
            : t(($) => $.appearance.updates.update_all, { count: updates.length })}
        </Button>
      ) : (
        <Button variant="outline" size="sm" disabled={listing.isFetching} onClick={() => void check()}>
          {listing.isFetching ? t(($) => $.appearance.updates.checking) : t(($) => $.appearance.updates.check)}
        </Button>
      )}
    </SettingsRow>
  );
}

/**
 * Making a theme: import one, or duplicate the active theme as a starting
 * point. On Desktop the copy is written into the themes folder, applied, and
 * edits to it apply as they are saved; on web it downloads as a .zip to edit
 * and import.
 */
function OwnThemeRow({ theme }: { theme: Theme }) {
  const { t } = useT("settings");
  const locale = useLocale();
  const themeLabelFor = useThemeLabel();
  const importer = useThemeImport();
  const install = useInstalledThemesStore((s) => s.install);
  const set = useAppearanceStore((s) => s.set);
  const folder = themeFolderAPI();

  const duplicate = async () => {
    const taken = [...BUILT_IN_THEMES, ...useInstalledThemesStore.getState().themes].map((t) => t.id);
    if (!folder) {
      const target = themeCopyTarget(theme, taken);
      const file = `${target.id}.zip`;
      downloadFile(writeZip(exportTheme(theme, target)), file, "application/zip");
      toast.success(t(($) => $.appearance.tools.duplicated_web, { file }));
      return;
    }
    for (let attempt = 0; attempt < 10; attempt += 1) {
      const target = themeCopyTarget(theme, taken);
      const files = exportTheme(theme, target);
      const result = await folder.writeThemeFolder(target.id, files);
      if (result.ok) {
        const copy = themeFromFiles(files);
        if (copy.ok) install(copy.theme);
        set({ theme: target.id });
        void folder.openThemeFolder(target.id);
        toast.success(t(($) => $.appearance.tools.duplicated_desktop, { name: target.name }));
        return;
      }
      if (result.reason !== "exists") {
        toast.error(t(($) => $.appearance.tools.duplicate_failed, { error: result.error ?? result.reason }));
        return;
      }
      taken.push(target.id);
    }
    toast.error(t(($) => $.appearance.tools.duplicate_failed, { error: "exists" }));
  };

  return (
    <SettingsRow
      label={
        <span className="flex items-center gap-2">
          {t(($) => $.appearance.own.title)}
          <Badge variant="secondary">{t(($) => $.appearance.import.beta)}</Badge>
        </span>
      }
      description={t(($) => $.appearance.own.description)}
      anchor="theme-import"
    >
      <div className="flex items-center gap-2">
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground"
          onClick={() => openExternal(themeGuideUrl(locale))}
          aria-label={t(($) => $.appearance.tools.guide)}
          title={t(($) => $.appearance.tools.guide)}
        >
          <BookOpen />
        </Button>
        {folder ? (
          <Button
            variant="ghost"
            size="icon-sm"
            className="text-muted-foreground"
            onClick={() => void folder.openThemeFolder()}
            aria-label={t(($) => $.appearance.tools.open_folder)}
            title={t(($) => $.appearance.tools.open_folder)}
          >
            <FolderOpen />
          </Button>
        ) : null}
        <DropdownMenu>
          <DropdownMenuTrigger render={<Button variant="outline" size="sm" />}>
            {t(($) => $.appearance.import.button)}
            <ChevronDown data-icon="inline-end" className="text-muted-foreground" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="min-w-56">
            <DropdownMenuItem onClick={importer.chooseZip}>
              <FileArchive />
              {t(($) => $.appearance.import.from_zip)}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={importer.chooseFolder}>
              <FolderInput />
              {t(($) => $.appearance.import.from_folder)}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => void duplicate()}>
              <Copy />
              {t(($) => $.appearance.tools.duplicate, { name: themeLabelFor(theme) })}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        {importer.element}
      </div>
    </SettingsRow>
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
          <FontSizeSlider
            label={sizeLabel}
            sizes={FONT_SIZES}
            value={fontSize}
            isDefault={fontSize === DEFAULT_FONT_SIZE}
            onChange={(size) => set({ fontSize: size as FontSize })}
          />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}

/** The small-A to large-A slider both text sections use for their size. */
function FontSizeSlider({
  label,
  sizes,
  value,
  isDefault,
  onChange,
}: {
  label: string;
  sizes: readonly number[];
  value: number;
  isDefault: boolean;
  onChange: (size: number) => void;
}) {
  const { t } = useT("settings");
  return (
    <div className="flex w-56 items-center gap-3">
      <span aria-hidden="true" className="text-caption text-muted-foreground">
        A
      </span>
      <Slider
        aria-label={label}
        min={sizes[0]}
        max={sizes[sizes.length - 1]}
        step={1}
        value={value}
        onValueChange={(next) => {
          const size = Array.isArray(next) ? next[0] : next;
          if (typeof size === "number" && sizes.includes(size)) onChange(size);
        }}
        className="flex-1"
      />
      <span aria-hidden="true" className="text-title-sm text-muted-foreground">
        A
      </span>
      <span className="min-w-24 shrink-0 whitespace-nowrap text-right text-caption tabular-nums text-muted-foreground">
        {isDefault
          ? t(($) => $.appearance.font_size.default, { size: value })
          : t(($) => $.appearance.font_size.value, { size: value })}
      </span>
    </div>
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
  const followSizeLabel = t(($) => $.appearance.content_size.follow);
  const leadingLabel = t(($) => $.appearance.content_leading.title);
  const themeCodeFont = useThemeSetsFont("font-mono");
  const themeFontLabel = t(($) => $.appearance.theme_font);
  const fromTheme = (id: CodeFont) => themeCodeFont && id === DEFAULT_APPEARANCE.codeFont;
  const codeName = (id: CodeFont) => (fromTheme(id) ? themeFontLabel : CODE_FONT_NAME[id]);
  const codeSample = (id: CodeFont) => (fromTheme(id) ? "var(--font-mono)" : CODE_FONT_SAMPLE[id]);
  const codeOptions = CODE_FONTS.map((id) => ({ value: id, label: codeName(id) }));

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
        >
          <FontSizeSlider
            label={sizeLabel}
            sizes={CONTENT_SIZES}
            value={contentSize === FOLLOW_UI ? fontSize : contentSize}
            isDefault={contentSize === FOLLOW_UI}
            onChange={(size) => set({ contentSize: size as ContentSize })}
          />
        </SettingsRow>
        <SettingsRow label={followSizeLabel} anchor="content-size-follow">
          <Switch
            checked={contentSize === FOLLOW_UI}
            onCheckedChange={(checked) => set({ contentSize: checked ? FOLLOW_UI : fontSize })}
            aria-label={followSizeLabel}
          />
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
