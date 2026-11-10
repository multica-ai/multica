"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ExternalLink, LayoutGrid, Search } from "@multica/ui/icons";
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
import { useTheme } from "@multica/ui/components/common/theme-provider";
import { useConfigStore } from "@multica/core/config";
import { useAppearanceStore, useInstalledThemesStore } from "@multica/core/appearance";
import {
  readStoreTheme,
  storeThemeScreenshotOptions,
  storeThemeState,
  storeThemesOptions,
  type StoreTheme,
  type StoreThemeMode,
} from "@multica/core/appearance/theme-store";
import { useT } from "../../i18n";
import { SegmentedToggle } from "../../common/segmented-toggle";
import { openExternal } from "../../platform/open-external";

/**
 * Settings → Appearance → Browse: the community theme store (beta). Only
 * rendered when the server serves the store, so a deployment that turned it
 * off shows no entry and makes no store requests.
 */
export function ThemeStoreButton() {
  const { t } = useT("settings");
  const enabled = useConfigStore((s) => s.themeStoreEnabled);
  const [open, setOpen] = useState(false);
  if (!enabled) return null;
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        <LayoutGrid />
        {t(($) => $.appearance.store.button)}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="flex max-h-[min(44rem,calc(100dvh-2rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
          {open ? <ThemeStore /> : null}
        </DialogContent>
      </Dialog>
    </>
  );
}

function ThemeStore() {
  const { t } = useT("settings");
  const [query, setQuery] = useState("");
  const themes = useQuery(storeThemesOptions(true));
  const needle = query.trim().toLowerCase();
  const shown = useMemo(
    () =>
      (themes.data ?? []).filter(
        (theme) => !needle || theme.name.toLowerCase().includes(needle) || theme.author.toLowerCase().includes(needle),
      ),
    [themes.data, needle],
  );

  return (
    <>
      <DialogHeader className="gap-1.5 border-b border-surface-border px-5 pt-5 pb-4">
        <DialogTitle className="flex items-center gap-2">
          {t(($) => $.appearance.store.title)}
          <Badge variant="secondary">{t(($) => $.appearance.import.beta)}</Badge>
        </DialogTitle>
        <DialogDescription className="text-caption">{t(($) => $.appearance.store.description)}</DialogDescription>
        <div className="relative mt-2">
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
      </DialogHeader>
      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
        {themes.isPending ? (
          <div className="grid gap-3 sm:grid-cols-3" aria-label={t(($) => $.appearance.store.loading)}>
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="aspect-[16/14] rounded-lg" />
            ))}
          </div>
        ) : themes.isError ? (
          <div role="alert" className="flex flex-col items-center gap-3 py-12 text-center text-caption text-muted-foreground">
            {t(($) => $.appearance.store.error)}
            <Button variant="outline" size="sm" onClick={() => void themes.refetch()}>
              {t(($) => $.appearance.store.retry)}
            </Button>
          </div>
        ) : shown.length === 0 ? (
          <p className="py-12 text-center text-caption text-muted-foreground">
            {needle
              ? t(($) => $.appearance.store.no_results, { query: query.trim() })
              : t(($) => $.appearance.store.empty)}
          </p>
        ) : (
          <ul className="grid gap-3 sm:grid-cols-3">
            {shown.map((theme) => (
              <li key={theme.id}>
                <StoreThemeCard theme={theme} />
              </li>
            ))}
          </ul>
        )}
      </div>
      <p className="border-t border-surface-border px-5 py-3 text-caption text-muted-foreground">
        {t(($) => $.appearance.store.warning)}
      </p>
    </>
  );
}

function StoreThemeCard({ theme }: { theme: StoreTheme }) {
  const { t } = useT("settings");
  const { resolvedTheme } = useTheme();
  const appMode: StoreThemeMode = resolvedTheme === "dark" ? "dark" : "light";
  const supported = theme.screenshots.length > 0 ? theme.screenshots : theme.modes;
  const [mode, setMode] = useState<StoreThemeMode>(supported.includes(appMode) ? appMode : supported[0] ?? appMode);
  const installed = useInstalledThemesStore((s) => s.themes);
  const fromStore = useInstalledThemesStore((s) => s.fromStore);
  const installFromStore = useInstalledThemesStore((s) => s.installFromStore);
  const install = useInstalledThemesStore((s) => s.install);
  const uninstall = useInstalledThemesStore((s) => s.uninstall);
  const active = useAppearanceStore((s) => s.theme);
  const set = useAppearanceStore((s) => s.set);
  const [busy, setBusy] = useState(false);
  const state = storeThemeState(theme, { themes: installed, fromStore });
  const inUse = active === theme.id && (state === "installed" || state === "update");

  const get = async () => {
    setBusy(true);
    try {
      const result = await readStoreTheme(theme);
      if (!result.ok) {
        toast.error(t(($) => $.appearance.store.install_failed, { name: theme.name }), {
          description: result.errors.join("\n"),
        });
        return;
      }
      const previous = useInstalledThemesStore.getState().themes.find((t) => t.id === theme.id);
      const previousVersion = useInstalledThemesStore.getState().fromStore[theme.id];
      try {
        installFromStore(result.theme, theme.version);
      } catch {
        if (previous && previousVersion !== undefined) installFromStore(previous, previousVersion);
        else if (previous) install(previous);
        else uninstall(theme.id);
        toast.error(t(($) => $.appearance.import.storage_full));
        return;
      }
      if (state === "update") {
        toast.success(t(($) => $.appearance.store.updated, { name: theme.name, version: theme.version }));
      } else {
        set({ theme: theme.id });
        toast.success(t(($) => $.appearance.store.installed_toast, { name: theme.name }));
      }
    } catch (error) {
      toast.error(t(($) => $.appearance.store.install_failed, { name: theme.name }), {
        description: error instanceof Error ? error.message : undefined,
      });
    } finally {
      setBusy(false);
    }
  };

  const remove = () => {
    if (active === theme.id) set({ theme: "default" });
    uninstall(theme.id);
  };

  return (
    <article className="flex h-full flex-col overflow-hidden rounded-lg border border-surface-border bg-surface">
      <div className="relative">
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
      </div>
      <div className="flex flex-1 flex-col gap-0.5 px-3 pt-2 pb-2.5">
        <div className="flex min-w-0 items-center gap-1.5">
          <h3 className="truncate text-label font-medium">{theme.name}</h3>
          <span className="shrink-0 text-caption text-muted-foreground">{theme.version}</span>
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
        </div>
        <p className="truncate text-caption text-muted-foreground">
          {t(($) => $.appearance.store.by, { author: theme.author })}
        </p>
        {theme.description ? <p className="line-clamp-2 text-caption text-muted-foreground">{theme.description}</p> : null}
        <div className="mt-auto flex items-center justify-end gap-1.5 pt-2">
          {state === "installed" || state === "update" ? (
            <Button variant="ghost" size="xs" disabled={busy} onClick={remove}>
              {t(($) => $.appearance.store.remove)}
            </Button>
          ) : null}
          {state === "available" ? (
            <Button size="xs" disabled={busy} onClick={() => void get()}>
              {busy ? t(($) => $.appearance.store.installing) : t(($) => $.appearance.store.install)}
            </Button>
          ) : state === "update" ? (
            <Button size="xs" disabled={busy} onClick={() => void get()}>
              {busy ? t(($) => $.appearance.store.installing) : t(($) => $.appearance.store.update, { version: theme.version })}
            </Button>
          ) : state === "installed" ? (
            <Button variant="outline" size="xs" disabled={inUse} onClick={() => set({ theme: theme.id })}>
              {inUse ? t(($) => $.appearance.store.in_use) : t(($) => $.appearance.store.use)}
            </Button>
          ) : (
            <span className="text-caption text-muted-foreground">{t(($) => $.appearance.store.taken)}</span>
          )}
        </div>
      </div>
    </article>
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
