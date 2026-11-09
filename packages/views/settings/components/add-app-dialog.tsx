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

/**
 * The add-app dialog: every app the workspace allows, curated ones first.
 * Each row connects a team account (admins) or the caller's own; the
 * recommended one is the primary button.
 */
export function AddAppDialog({
  open,
  onOpenChange,
  canManage,
  overview,
  catalog,
  catalogLoading,
  catalogError,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  canManage: boolean;
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
    for (const c of overview.team) if (c.status === "active") set.add(`team:${c.toolkit_slug}`);
    for (const c of overview.personal) if (c.status === "active") set.add(`personal:${c.toolkit_slug}`);
    return set;
  }, [overview]);

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

  async function connect(app: AppCatalogEntry, scope: AppAccountScope) {
    if (connecting) return;
    setConnecting(`${scope}:${app.slug}`);
    try {
      const { redirect_url } = await api.connectApp(scope, app.slug);
      window.location.href = redirect_url;
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t(($) => $.connected_apps.toast_failed));
      setConnecting(null);
    }
  }

  const connectButton = (app: AppCatalogEntry, scope: AppAccountScope) => {
    const key = `${scope}:${app.slug}`;
    const label =
      scope === "team"
        ? t(($) => $.connected_apps.add.connect_team)
        : t(($) => $.connected_apps.add.connect_personal);
    if (connected.has(key)) {
      return (
        <span key={scope} className="inline-flex items-center gap-1 text-caption text-muted-foreground">
          <Check className="size-3" />
          {label}
        </span>
      );
    }
    const recommended = (app.recommended_scope ?? "personal") === scope;
    return (
      <Button
        key={scope}
        size="xs"
        variant={recommended ? "default" : "outline"}
        disabled={connecting !== null}
        onClick={() => connect(app, scope)}
      >
        {connecting === key && <Loader2 className="animate-spin" />}
        {connecting === key ? t(($) => $.connected_apps.add.connecting) : label}
      </Button>
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.connected_apps.add.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.connected_apps.add.description)}</DialogDescription>
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
                  <div className="flex shrink-0 items-center gap-2">
                    {canManage && connectButton(app, "team")}
                    {connectButton(app, "personal")}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
