"use client";

import { useMemo, useState } from "react";
import { toast } from "sonner";
import { Check, Loader2 } from "@multica/ui/icons";
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
import { api } from "@multica/core/api";
import type { AppAccountScope, AppCatalogEntry, AppsOverview } from "@multica/core/types";
import { ComposioToolkitLogo } from "../../common/composio-toolkit-logo";
import { useT } from "../../i18n";
import { isDesktopShell, openExternal } from "../../platform";

/**
 * The add-app dialog: every app the workspace allows, curated ones first.
 * The section that opened it determines the account scope.
 */
export function AddAppDialog({
  open,
  onOpenChange,
  canManage,
  scope,
  overview,
  catalog,
  catalogLoading,
  catalogError,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  canManage: boolean;
  scope: AppAccountScope;
  overview: AppsOverview;
  catalog: AppCatalogEntry[];
  catalogLoading: boolean;
  catalogError: boolean;
}) {
  const { t } = useT("settings");
  const [query, setQuery] = useState("");
  const [connecting, setConnecting] = useState<string | null>(null);

  const connected = useMemo(() => {
    const set = new Set<string>();
    for (const c of overview[scope]) if (c.status === "active") set.add(c.toolkit_slug);
    return set;
  }, [overview, scope]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return catalog;
    return catalog.filter(
      (app) =>
        app.name.toLowerCase().includes(q) ||
        app.slug.includes(q) ||
        (app.category ?? "").toLowerCase().includes(q),
    );
  }, [catalog, query]);

  async function connect(app: AppCatalogEntry) {
    if (connecting || (scope === "team" && !canManage)) return;
    setConnecting(app.slug);
    try {
      const { redirect_url } = await api.connectApp(scope, app.slug);
      openExternal(redirect_url, { webTarget: "same-tab" });
      // The web tab leaves for the hosted flow. Desktop opens it in the system
      // browser and stays here, so the row must become clickable again.
      if (isDesktopShell()) setConnecting(null);
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t(($) => $.connected_apps.toast_failed));
      setConnecting(null);
    }
  }

  const connectButton = (app: AppCatalogEntry) => {
    if (connected.has(app.slug)) {
      return (
        <span className="inline-flex items-center gap-1 text-caption text-muted-foreground">
          <Check className="size-3" />
          {t(($) => $.connected_apps.add.connected)}
        </span>
      );
    }
    return (
      <Button
        size="xs"
        variant="outline"
        disabled={connecting !== null || (scope === "team" && !canManage)}
        onClick={() => connect(app)}
      >
        {connecting === app.slug && <Loader2 className="animate-spin" />}
        {connecting === app.slug ? t(($) => $.connected_apps.add.connecting) : t(($) => $.connected_apps.add.connect)}
      </Button>
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{scope === "team" ? t(($) => $.connected_apps.add.connect_team) : t(($) => $.connected_apps.add.connect_personal)}</DialogTitle>
          <DialogDescription>{scope === "team" ? t(($) => $.connected_apps.add.team_description) : t(($) => $.connected_apps.add.personal_description)}</DialogDescription>
        </DialogHeader>
        <Input
          autoFocus
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t(($) => $.connected_apps.add.search)}
        />
        <div className="max-h-[60vh] min-h-48 overflow-y-auto rounded-lg border">
          {catalogLoading ? (
            <p className="p-4 text-caption text-muted-foreground">{t(($) => $.connected_apps.loading)}</p>
          ) : catalogError ? (
            <p className="p-4 text-caption text-destructive">{t(($) => $.connected_apps.load_failed)}</p>
          ) : filtered.length === 0 ? (
            <p className="p-4 text-caption text-muted-foreground">{t(($) => $.connected_apps.add.empty)}</p>
          ) : (
            <ul className="divide-y">
              {filtered.map((app) => (
                <li key={app.slug} className="flex items-center gap-3 px-3 py-2.5">
                  <ComposioToolkitLogo slug={app.slug} name={app.name} fallbackLogo={app.logo} />
                  <div className="min-w-0 flex-1">
                    <p className="flex items-center gap-2 truncate text-body font-medium">
                      {app.name || app.slug}
                      {app.curated && (
                        <Badge variant="secondary">{t(($) => $.connected_apps.add.curated)}</Badge>
                      )}
                    </p>
                    <p className="truncate text-caption text-muted-foreground">
                      {app.recommended_scope === "team"
                        ? t(($) => $.connected_apps.add.recommended_team)
                        : app.recommended_scope === "personal"
                          ? t(($) => $.connected_apps.add.recommended_personal)
                          : app.category}
                    </p>
                  </div>
                  <div className="shrink-0">{connectButton(app)}</div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
