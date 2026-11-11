"use client";

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ExternalLink, Search } from "@multica/ui/icons";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Switch } from "@multica/ui/components/ui/switch";
import { useConfigStore } from "@multica/core/config";
import { useAppearanceStore, useInstalledThemesStore, type Theme } from "@multica/core/appearance";
import {
  readStoreTheme,
  storeThemeScreenshotOptions,
  storeThemeState,
  storeThemesOptions,
  themeStoreKeys,
  type StoreTheme,
  type StoreThemeMode,
} from "@multica/core/appearance/theme-store";
import { useLocale, useT } from "../../i18n";
import { SegmentedToggle } from "../../common/segmented-toggle";
import { openExternal } from "../../platform/open-external";
import { ThemePreview, useAppliedMode, useThemeLabel } from "./theme-swatch";

/**
 * Settings → Appearance → Manage: the themes installed on this device and,
 * when the server serves the community theme store (beta), the store's
 * themes. Built-in themes are only in the theme picker. A deployment that
 * turned the store off makes no store requests, and with nothing installed
 * there is nothing to manage.
 */
export function ThemesButton() {
  const { t } = useT("settings");
  const [open, setOpen] = useState(false);
  const storeEnabled = useConfigStore((s) => s.themeStoreEnabled);
  const hasInstalled = useInstalledThemesStore((s) => s.themes.length > 0);
  if (!storeEnabled && !hasInstalled && !open) return null;
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        {t(($) => $.appearance.store.manage)}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="flex max-h-[min(44rem,calc(100dvh-2rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
          {open ? <Themes /> : null}
        </DialogContent>
      </Dialog>
    </>
  );
}

/**
 * Installs the listed release of a store theme, or updates to it. A new
 * install is applied at once; an update leaves the theme in use as it is.
 */
export function useStoreThemeInstall() {
  const { t } = useT("settings");
  const queryClient = useQueryClient();
  return useCallback(
    async (theme: StoreTheme) => {
      const themes = useInstalledThemesStore.getState();
      const update = storeThemeState(theme, themes) === "update";
      try {
        const result = await readStoreTheme(theme);
        void queryClient.invalidateQueries({ queryKey: themeStoreKeys.themes() });
        if (!result.ok) {
          toast.error(t(($) => $.appearance.store.install_failed, { name: theme.name }), {
            description: result.errors.join("\n"),
          });
          return;
        }
        const previous = themes.themes.find((installed) => installed.id === theme.id);
        const previousVersion = themes.fromStore[theme.id];
        try {
          themes.installFromStore(result.theme, theme.version);
        } catch {
          if (previous && previousVersion !== undefined) themes.installFromStore(previous, previousVersion);
          else if (previous) themes.install(previous);
          else themes.uninstall(theme.id);
          toast.error(t(($) => $.appearance.import.storage_full));
          return;
        }
        if (update) {
          toast.success(t(($) => $.appearance.store.updated, { name: theme.name, version: theme.version }));
        } else {
          useAppearanceStore.getState().set({ theme: theme.id });
          toast.success(t(($) => $.appearance.store.installed_toast, { name: theme.name }));
        }
      } catch (error) {
        toast.error(t(($) => $.appearance.store.install_failed, { name: theme.name }), {
          description: error instanceof Error ? error.message : undefined,
        });
      }
    },
    [t, queryClient],
  );
}

type Entry =
  | { kind: "local"; key: string; name: string; author: string; installed: boolean; theme: Theme }
  | { kind: "store"; key: string; name: string; author: string; installed: boolean; theme: StoreTheme };

function Themes() {
  const { t } = useT("settings");
  const storeEnabled = useConfigStore((s) => s.themeStoreEnabled);
  const listing = useQuery(storeThemesOptions(storeEnabled));
  const installed = useInstalledThemesStore((s) => s.themes);
  const fromStore = useInstalledThemesStore((s) => s.fromStore);
  const themeLabelFor = useThemeLabel();
  const [query, setQuery] = useState("");
  const [installedOnly, setInstalledOnly] = useState(false);
  const needle = query.trim().toLowerCase();

  // Installed themes, then what the store offers. A theme installed from the
  // store is shown as the store lists it.
  const listed = new Map((listing.data ?? []).map((theme) => [theme.id, theme]));
  const local = (theme: Theme): Entry => {
    const store = theme.id in fromStore ? listed.get(theme.id) : undefined;
    if (store) listed.delete(theme.id);
    return store
      ? { kind: "store", key: store.id, name: store.name, author: store.author, installed: true, theme: store }
      : { kind: "local", key: theme.id, name: themeLabelFor(theme), author: theme.author ?? "", installed: true, theme };
  };
  const entries = installed.map(local);
  for (const theme of [...listed.values()].sort((a, b) => b.downloads - a.downloads)) {
    // A listed theme whose id is taken on this device is shown next to the one that has it.
    const taken = entries.some((entry) => entry.key === theme.id);
    entries.push({
      kind: "store",
      key: taken ? `store:${theme.id}` : theme.id,
      name: theme.name,
      author: theme.author,
      installed: false,
      theme,
    });
  }
  // A card keeps its place while the window is open, so installing or
  // removing a theme does not move it from under the pointer.
  const order = useRef(new Map<string, number>()).current;
  for (const entry of entries) if (!order.has(entry.key)) order.set(entry.key, order.size);
  entries.sort((a, b) => order.get(a.key)! - order.get(b.key)!);

  const shown = entries.filter(
    (entry) =>
      (!installedOnly || entry.installed) &&
      (!needle || entry.name.toLowerCase().includes(needle) || entry.author.toLowerCase().includes(needle)),
  );
  const loadingStore = storeEnabled && listing.isPending && !installedOnly;

  return (
    <>
      <DialogHeader className="gap-1.5 border-b border-surface-border px-5 pt-5 pb-4">
        <DialogTitle className="flex items-center gap-2">
          {t(($) => $.appearance.store.title)}
          <Badge variant="secondary">{t(($) => $.appearance.import.beta)}</Badge>
        </DialogTitle>
        <DialogDescription className="text-caption">
          {storeEnabled ? t(($) => $.appearance.store.description) : t(($) => $.appearance.store.description_local)}
        </DialogDescription>
        <div className="mt-2 flex items-center gap-3">
          <div className="relative min-w-0 flex-1">
            <Search
              aria-hidden="true"
              className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
            />
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t(($) => $.appearance.store.search)}
              aria-label={t(($) => $.appearance.store.search)}
              className="pl-8"
            />
          </div>
          {storeEnabled ? (
            <label className="flex shrink-0 items-center gap-2 text-label">
              <Switch
                checked={installedOnly}
                onCheckedChange={setInstalledOnly}
                aria-label={t(($) => $.appearance.store.installed_only)}
              />
              <span aria-hidden="true">{t(($) => $.appearance.store.installed_only)}</span>
            </label>
          ) : null}
        </div>
      </DialogHeader>
      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
        {shown.length === 0 && !loadingStore ? (
          <p className="py-12 text-center text-caption text-muted-foreground">
            {needle
              ? t(($) => $.appearance.store.no_results, { query: query.trim() })
              : t(($) => $.appearance.store.none_installed)}
          </p>
        ) : (
          <>
            <p className="mb-2.5 text-caption text-muted-foreground">
              {t(($) => $.appearance.store.showing, { count: shown.length })}
            </p>
            <ul className="grid gap-3 sm:grid-cols-3">
              {shown.map((entry) => (
                <li key={entry.key}>
                  {entry.kind === "store" ? (
                    <StoreThemeCard theme={entry.theme} />
                  ) : (
                    <LocalThemeCard theme={entry.theme} name={entry.name} />
                  )}
                </li>
              ))}
              {loadingStore
                ? [0, 1, 2].map((i) => (
                    <li key={`loading:${i}`} aria-label={i === 0 ? t(($) => $.appearance.store.loading) : undefined}>
                      <Skeleton className="aspect-[16/14] rounded-lg" />
                    </li>
                  ))
                : null}
            </ul>
          </>
        )}
        {storeEnabled && listing.isError && !installedOnly ? (
          <div role="alert" className="flex flex-col items-center gap-3 py-8 text-center text-caption text-muted-foreground">
            {t(($) => $.appearance.store.error)}
            <Button variant="outline" size="sm" onClick={() => void listing.refetch()}>
              {t(($) => $.appearance.store.retry)}
            </Button>
          </div>
        ) : null}
      </div>
      {storeEnabled ? (
        <p className="border-t border-surface-border px-5 py-3 text-caption text-muted-foreground">
          {t(($) => $.appearance.store.warning)}
        </p>
      ) : null}
    </>
  );
}

/** The card every theme is shown as: a preview, what it is, and its actions. */
function ThemeCard({
  preview,
  name,
  version,
  badge,
  trailing,
  subtitle,
  description,
  children,
}: {
  preview: ReactNode;
  name: string;
  version?: string;
  badge?: ReactNode;
  trailing?: ReactNode;
  subtitle: string;
  description?: string;
  children: ReactNode;
}) {
  return (
    <article className="flex h-full flex-col overflow-hidden rounded-lg border border-surface-border bg-surface">
      <div className="relative">{preview}</div>
      <div className="flex flex-1 flex-col gap-0.5 px-3 pt-2 pb-2.5">
        <div className="flex min-h-[var(--button-height-xs)] min-w-0 items-center gap-1.5">
          <h3 className="truncate text-label font-medium">{name}</h3>
          {badge}
          {version ? <span className="shrink-0 text-caption text-muted-foreground">{version}</span> : null}
          {trailing}
        </div>
        <p className="truncate text-caption text-muted-foreground">{subtitle}</p>
        {description ? <p className="line-clamp-2 text-caption text-muted-foreground">{description}</p> : null}
        <div className="mt-auto flex items-center justify-end gap-1.5 pt-2">{children}</div>
      </div>
    </article>
  );
}

function StateBadge({ inUse, installed }: { inUse: boolean; installed: boolean }) {
  const { t } = useT("settings");
  if (inUse) {
    return (
      <Badge variant="secondary" className="shrink-0 bg-brand/15 text-brand">
        {t(($) => $.appearance.store.in_use)}
      </Badge>
    );
  }
  if (!installed) return null;
  return (
    <Badge variant="secondary" className="shrink-0">
      {t(($) => $.appearance.theme.installed)}
    </Badge>
  );
}

function UseButton({ id, inUse }: { id: string; inUse: boolean }) {
  const { t } = useT("settings");
  const set = useAppearanceStore((s) => s.set);
  return (
    <Button variant="outline" size="xs" disabled={inUse} onClick={() => set({ theme: id })}>
      {inUse ? t(($) => $.appearance.store.in_use) : t(($) => $.appearance.store.use)}
    </Button>
  );
}

function useRemoveTheme(id: string) {
  const uninstall = useInstalledThemesStore((s) => s.uninstall);
  const set = useAppearanceStore((s) => s.set);
  return () => {
    if (useAppearanceStore.getState().theme === id) set({ theme: "default" });
    uninstall(id);
  };
}

/**
 * An installed theme the store does not list: imported, or in the themes
 * folder. It has no screenshot, so it is drawn
 * from its tokens. Only an imported theme can be removed here; a theme in
 * the themes folder is removed by deleting its folder.
 */
function LocalThemeCard({ theme, name }: { theme: Theme; name: string }) {
  const { t } = useT("settings");
  const mode = useAppliedMode();
  const inFolder = useInstalledThemesStore((s) => theme.id in s.folders);
  const inUse = useAppearanceStore((s) => s.theme === theme.id);
  const remove = useRemoveTheme(theme.id);
  return (
    <ThemeCard
      preview={
        <div className="aspect-[16/10] border-b border-surface-border bg-muted">
          <ThemePreview theme={theme} mode={mode} className="size-full" />
        </div>
      }
      name={name}
      version={theme.version}
      badge={<StateBadge inUse={inUse} installed />}
      subtitle={
        inFolder
          ? t(($) => $.appearance.theme.folder)
          : theme.author
            ? t(($) => $.appearance.store.by, { author: theme.author })
            : t(($) => $.appearance.theme.installed)
      }
    >
      {!inFolder ? (
        <Button
          variant="ghost"
          size="xs"
          onClick={remove}
          aria-label={t(($) => $.appearance.theme.remove, { name })}
        >
          {t(($) => $.appearance.store.remove)}
        </Button>
      ) : null}
      <UseButton id={theme.id} inUse={inUse} />
    </ThemeCard>
  );
}

function StoreThemeCard({ theme }: { theme: StoreTheme }) {
  const { t } = useT("settings");
  const locale = useLocale();
  const appMode = useAppliedMode();
  const supported = theme.screenshots.length > 0 ? theme.screenshots : theme.modes;
  const [mode, setMode] = useState<StoreThemeMode>(supported.includes(appMode) ? appMode : supported[0] ?? appMode);
  const installed = useInstalledThemesStore((s) => s.themes);
  const fromStore = useInstalledThemesStore((s) => s.fromStore);
  const active = useAppearanceStore((s) => s.theme);
  const install = useStoreThemeInstall();
  const remove = useRemoveTheme(theme.id);
  const [busy, setBusy] = useState(false);
  const state = storeThemeState(theme, { themes: installed, fromStore });
  const onDevice = state === "installed" || state === "update";
  const inUse = active === theme.id && onDevice;

  const get = async () => {
    setBusy(true);
    try {
      await install(theme);
    } finally {
      setBusy(false);
    }
  };

  return (
    <ThemeCard
      preview={
        <>
          <Screenshot theme={theme} mode={mode} />
          {supported.length > 1 ? (
            <div className="absolute right-1.5 bottom-1.5 rounded-md bg-surface-raised/90 shadow-sm backdrop-blur">
              <SegmentedToggle<StoreThemeMode>
                value={mode}
                onChange={setMode}
                buttonClassName="px-1.5 py-0 text-caption"
                options={supported.map(
                  (m) => [m, m === "light" ? t(($) => $.appearance.mode.light) : t(($) => $.appearance.mode.dark)] as const,
                )}
              />
            </div>
          ) : null}
        </>
      }
      name={theme.name}
      version={theme.version}
      badge={<StateBadge inUse={inUse} installed={onDevice} />}
      trailing={
        <Button
          variant="ghost"
          size="icon-xs"
          className="-mr-1.5 ml-auto text-muted-foreground"
          onClick={() => openExternal(`https://github.com/${theme.repo}`)}
          title={theme.repo}
          aria-label={t(($) => $.appearance.store.source, { repo: theme.repo })}
        >
          <ExternalLink />
        </Button>
      }
      subtitle={[
        t(($) => $.appearance.store.by, { author: theme.author }),
        t(($) => $.appearance.store.downloads, {
          count: theme.downloads,
          downloads: new Intl.NumberFormat(locale, { notation: "compact" }).format(theme.downloads),
        }),
      ].join(" · ")}
      description={theme.description}
    >
      {onDevice ? (
        <Button
          variant="ghost"
          size="xs"
          disabled={busy}
          onClick={remove}
          aria-label={t(($) => $.appearance.theme.remove, { name: theme.name })}
        >
          {t(($) => $.appearance.store.remove)}
        </Button>
      ) : null}
      {state === "update" ? (
        <Button size="xs" disabled={busy} onClick={() => void get()}>
          {busy ? t(($) => $.appearance.store.installing) : t(($) => $.appearance.store.update, { version: theme.version })}
        </Button>
      ) : null}
      {state === "available" ? (
        <Button size="xs" disabled={busy} onClick={() => void get()}>
          {busy ? t(($) => $.appearance.store.installing) : t(($) => $.appearance.store.install)}
        </Button>
      ) : onDevice ? (
        <UseButton id={theme.id} inUse={inUse} />
      ) : (
        <span className="text-caption text-muted-foreground">{t(($) => $.appearance.store.taken)}</span>
      )}
    </ThemeCard>
  );
}

function Screenshot({ theme, mode }: { theme: StoreTheme; mode: StoreThemeMode }) {
  const { t } = useT("settings");
  const shot = useQuery(storeThemeScreenshotOptions(theme, mode));
  const url = useObjectUrl(shot.data);
  return (
    <div className="aspect-[16/10] border-b border-surface-border bg-muted">
      {url ? (
        <img
          src={url}
          alt={t(($) => $.appearance.store.screenshot_alt, {
            name: theme.name,
            mode: mode === "light" ? t(($) => $.appearance.mode.light) : t(($) => $.appearance.mode.dark),
          })}
          className="size-full object-cover object-top"
          draggable={false}
        />
      ) : shot.isLoading ? (
        <Skeleton className="size-full rounded-none" />
      ) : (
        <div className="flex size-full items-center justify-center text-caption text-muted-foreground">
          {t(($) => $.appearance.store.no_preview)}
        </div>
      )}
    </div>
  );
}

function useObjectUrl(blob: Blob | undefined): string | null {
  const [url, setUrl] = useState<string | null>(null);
  useEffect(() => {
    if (!blob || typeof URL.createObjectURL !== "function") {
      setUrl(null);
      return;
    }
    const next = URL.createObjectURL(blob);
    setUrl(next);
    return () => URL.revokeObjectURL(next);
  }, [blob]);
  return url;
}
