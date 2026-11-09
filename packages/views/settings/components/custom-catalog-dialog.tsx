"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Loader2 } from "@multica/ui/icons";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { useWorkspaceId } from "@multica/core/hooks";
import { fullAppCatalogOptions, useUpdateAppSettings } from "@multica/core/connected-apps";
import { ComposioToolkitLogo } from "../../common/composio-toolkit-logo";
import { useT } from "../../i18n";

/**
 * Picks the apps of a custom catalog. Saving switches the workspace to it:
 * agents see only these apps, and nobody can add an account for another.
 */
export function CustomCatalogDialog({
  initial,
  onClose,
}: {
  initial: string[];
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const catalog = useQuery(fullAppCatalogOptions(wsId));
  const update = useUpdateAppSettings();
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState(() => new Set(initial));

  const apps = useMemo(() => {
    const q = query.trim().toLowerCase();
    const all = catalog.data?.apps ?? [];
    const matches = q
      ? all.filter((app) => app.name.toLowerCase().includes(q) || app.slug.includes(q))
      : all;
    // Chosen apps first so the current list reads at a glance.
    return [...matches].sort((a, b) => Number(selected.has(b.slug)) - Number(selected.has(a.slug)));
    // Re-sorting on every toggle would move the row under the cursor.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [catalog.data, query]);

  const toggle = (slug: string, checked: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) next.add(slug);
      else next.delete(slug);
      return next;
    });

  const save = () =>
    update.mutate(
      { catalog_mode: "custom", allowed_toolkits: [...selected] },
      {
        onSuccess: () => {
          toast.success(t(($) => $.connected_apps.custom.saved));
          onClose();
        },
        onError: () => toast.error(t(($) => $.connected_apps.toast_failed)),
      },
    );

  return (
    <Dialog open onOpenChange={(open) => !open && !update.isPending && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.connected_apps.custom.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.connected_apps.custom.description)}</DialogDescription>
        </DialogHeader>
        <Input
          autoFocus
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t(($) => $.connected_apps.add.search)}
        />
        <div className="max-h-[50vh] min-h-48 overflow-y-auto rounded-lg border">
          {catalog.isLoading ? (
            <p className="p-4 text-caption text-muted-foreground">{t(($) => $.connected_apps.loading)}</p>
          ) : catalog.isError ? (
            <p className="p-4 text-caption text-destructive">{t(($) => $.connected_apps.load_failed)}</p>
          ) : apps.length === 0 ? (
            <p className="p-4 text-caption text-muted-foreground">{t(($) => $.connected_apps.add.empty)}</p>
          ) : (
            <ul className="divide-y">
              {apps.map((app) => (
                <li key={app.slug}>
                  <label className="flex cursor-pointer items-center gap-3 px-3 py-2">
                    <Checkbox
                      checked={selected.has(app.slug)}
                      onCheckedChange={(checked) => toggle(app.slug, checked === true)}
                    />
                    <ComposioToolkitLogo slug={app.slug} name={app.name} fallbackLogo={app.logo} className="size-6" />
                    <span className="min-w-0 flex-1 truncate text-body">{app.name || app.slug}</span>
                    {app.curated && <Badge variant="secondary">{t(($) => $.connected_apps.add.curated)}</Badge>}
                  </label>
                </li>
              ))}
            </ul>
          )}
        </div>
        <DialogFooter className="items-center sm:justify-between">
          <span className="text-caption text-muted-foreground">
            {t(($) => $.connected_apps.custom.selected, { count: selected.size })}
          </span>
          <div className="flex gap-2">
            <Button variant="outline" disabled={update.isPending} onClick={onClose}>
              {t(($) => $.connected_apps.custom.cancel)}
            </Button>
            <Button disabled={selected.size === 0 || update.isPending} onClick={save}>
              {update.isPending && <Loader2 className="animate-spin" />}
              {t(($) => $.connected_apps.custom.save)}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
