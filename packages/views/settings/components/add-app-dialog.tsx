"use client";

import { useMemo, useState } from "react";
import { toast } from "sonner";
import { Check, Loader2 } from "@multica/ui/icons";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { api } from "@multica/core/api";
import type { PaidFeatureAccess } from "@multica/core/billing";
import { useWorkspacePaths } from "@multica/core/paths";
import type { AppAccountScope, AppCatalogEntry, AppsOverview } from "@multica/core/types";
import { ComposioToolkitLogo } from "../../common/composio-toolkit-logo";
import { PaidFeatureNotice, usePaidFeatureErrorMessage } from "../../billing";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";
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
  teamAccess,
  onConnectError,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  canManage: boolean;
  scope: AppAccountScope;
  overview: AppsOverview;
  catalog: AppCatalogEntry[];
  catalogLoading: boolean;
  catalogError: boolean;
  /** The plan's `team_app_accounts` access. Omitted means available. */
  teamAccess?: Pick<PaidFeatureAccess, "canAdd" | "state">;
  onConnectError?: (error: unknown) => void;
}) {
  const { t } = useT("settings");
  const paidFeatureError = usePaidFeatureErrorMessage();
  const [query, setQuery] = useState("");
  const [connecting, setConnecting] = useState<string | null>(null);

  const connected = useMemo(() => {
    const set = new Set<string>();
    for (const c of overview[scope]) if (c.status === "active") set.add(c.toolkit_slug);
    return set;
  }, [overview, scope]);
  // A team account that already exists (active or expired) can always be
  // reauthorized; only a team account for a new app needs the paid feature.
  const existingTeam = useMemo(
    () => new Set(overview.team.filter((c) => c.status !== "revoked").map((c) => c.toolkit_slug)),
    [overview.team],
  );
  const canAddTeam = teamAccess?.canAdd ?? true;
  const teamState = scope === "team" ? teamAccess?.state : null;

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
    if (connecting || (scope === "team" && (!canManage || (!canAddTeam && !existingTeam.has(app.slug))))) return;
    setConnecting(app.slug);
    try {
      const { redirect_url } = await api.connectApp(scope, app.slug);
      openExternal(redirect_url, { webTarget: "same-tab" });
      // The web tab leaves for the hosted flow. Desktop opens it in the system
      // browser and stays here, so the row must become clickable again.
      if (isDesktopShell()) setConnecting(null);
    } catch (e) {
      onConnectError?.(e);
      toast.error(paidFeatureError(e, e instanceof Error && e.message ? e.message : t(($) => $.connected_apps.toast_failed)));
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
    const reconnect = scope === "team" && existingTeam.has(app.slug);
    if (scope === "team" && canManage && !canAddTeam && !reconnect) {
      return <UpgradeToAddLink />;
    }
    return (
      <Button
        size="xs"
        variant="outline"
        disabled={connecting !== null || (scope === "team" && !canManage)}
        onClick={() => connect(app)}
      >
        {connecting === app.slug && <Loader2 className="animate-spin" />}
        {connecting === app.slug
          ? t(($) => $.connected_apps.add.connecting)
          : reconnect
            ? t(($) => $.connected_apps.reconnect)
            : t(($) => $.connected_apps.add.connect)}
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
        {canManage && (teamState === "read_only" || teamState === "unavailable") ? (
          <PaidFeatureNotice state={teamState} />
        ) : null}
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

/** Stands in for Connect where the plan cannot add a team account. */
function UpgradeToAddLink() {
  const { t } = useT("billing");
  const paths = useWorkspacePaths();
  return (
    <AppLink href={`${paths.settings()}?tab=billing`} className={buttonVariants({ size: "xs", variant: "outline" })}>
      {t(($) => $.paid_feature.upgrade_to_add)}
    </AppLink>
  );
}
