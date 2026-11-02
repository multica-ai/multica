"use client";

import { useState } from "react";
import { Plus } from "@multica/ui/icons";
import { useQuery } from "@tanstack/react-query";
import {
  messageEventCatalogOptions,
  messageSourceRoutesOptions,
} from "@multica/core/message-delivery";
import { larkInstallationsOptions } from "@multica/core/lark";
import { useWorkspaceId } from "@multica/core/hooks";
import { ApiError } from "@multica/core/api";
import type { MessageSourceRoute } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { Alert, AlertDescription } from "@multica/ui/components/ui/alert";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { SourceRouteRow } from "./source-route-row";
import { SourceRouteEditorDialog } from "./source-route-editor-dialog";
import { RouteDeliveriesDialog } from "./route-deliveries-dialog";

/**
 * "推送到我的飞书" — the personal notification settings entry (OL-28). It
 * manages ONLY the acting member's own inbox route: the recipient is the
 * server-resolved member, there is no member picker, and a 403
 * (route_not_self / message_forbidden) renders as an explicit restricted
 * state rather than a phantom empty one. The section coexists with the
 * category mutes above it (a muted category is not forwarded — the server
 * re-checks at send time) and the browser/OS switch below it (an unrelated
 * preference, never a Feishu master switch).
 */
export function PersonalFeishuPushSection() {
  const { t } = useT("message-delivery");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();

  const routesQuery = useQuery(messageSourceRoutesOptions(wsId, "inbox"));
  const catalogQuery = useQuery(messageEventCatalogOptions(wsId));
  const installationsQuery = useQuery(larkInstallationsOptions(wsId));

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<MessageSourceRoute | null>(null);
  const [recordsRouteId, setRecordsRouteId] = useState<string | null>(null);

  const routes = routesQuery.data ?? [];
  const installations = installationsQuery.data?.installations ?? [];

  const openEditor = (route: MessageSourceRoute | null) => {
    setEditing(route);
    setEditorOpen(true);
  };

  // Only an INITIAL load failure replaces the section. A failed background
  // refresh (data retained) shows a non-blocking note and must NOT unmount
  // anything — an open editor's unsaved draft survives the failed refresh.
  if (routesQuery.isError && routesQuery.data == null) {
    const err = routesQuery.error;
    const status = err instanceof ApiError ? err.status : 0;
    const copyKey =
      status === 403 ? "forbidden" : status === 404 ? "unsupported" : "load_failed";
    return (
      <Alert>
        <AlertDescription>{t(($) => $.personal[copyKey])}</AlertDescription>
      </Alert>
    );
  }

  const sendingUnavailable = installationsQuery.data?.configured === false;
  const noBot =
    installationsQuery.isSuccess &&
    installations.every((inst) => inst.status !== "active");

  return (
    <div className="space-y-3">
      {routesQuery.isError && (
        <Alert>
          <AlertDescription>{t(($) => $.personal.load_failed)}</AlertDescription>
        </Alert>
      )}

      {sendingUnavailable && (
        <Alert>
          <AlertDescription>{t(($) => $.section.sending_unavailable)}</AlertDescription>
        </Alert>
      )}

      {routesQuery.isLoading ? (
        <div className="space-y-1">
          <Skeleton className="h-12 w-full" />
        </div>
      ) : routes.length === 0 ? (
        noBot ? (
          <div className="rounded-md border border-dashed p-4 text-center text-body text-muted-foreground space-y-2">
            <p>{t(($) => $.personal.no_bot)}</p>
            <AppLink
              href={`${wsPaths.settings()}?tab=integrations&integration=lark`}
              className="inline-block text-caption font-medium text-foreground underline underline-offset-2"
            >
              {t(($) => $.personal.install_bot)}
            </AppLink>
          </div>
        ) : (
          <div className="rounded-md border border-dashed p-4 text-center text-body text-muted-foreground">
            {t(($) => $.personal.empty)}
          </div>
        )
      ) : (
        <div className="rounded-md border overflow-hidden divide-y">
          {routes.map((route) => (
            <SourceRouteRow
              key={route.id}
              route={route}
              installations={installations}
              canManage
              onEdit={() => openEditor(route)}
              onShowRecords={() => setRecordsRouteId(route.id)}
            />
          ))}
        </div>
      )}

      <p className="text-caption text-muted-foreground">
        {t(($) => $.personal.mute_hint)}
      </p>
      <p className="text-caption text-muted-foreground">
        {t(($) => $.personal.system_hint)}
      </p>

      {!noBot && (
        <div>
          <Button
            size="sm"
            variant="outline"
            onClick={() => openEditor(null)}
            disabled={installationsQuery.isSuccess !== true}
          >
            <Plus className="h-3.5 w-3.5 mr-1" />
            {t(($) => $.personal.add)}
          </Button>
        </div>
      )}

      {editorOpen && (
        <SourceRouteEditorDialog
          open={editorOpen}
          onOpenChange={setEditorOpen}
          mode="personal"
          route={editing}
          installations={installations}
          catalog={catalogQuery.data}
          catalogError={catalogQuery.isError}
          // Editor mounts only outside the initial-load error branch, so an
          // isError here is always a failed background refresh with retained data.
          routesRefreshFailed={routesQuery.isError}
          onCatalogRetry={() => catalogQuery.refetch()}
        />
      )}

      {recordsRouteId && (
        <RouteDeliveriesDialog
          open={recordsRouteId != null}
          onOpenChange={(o) => {
            if (!o) setRecordsRouteId(null);
          }}
          routeId={recordsRouteId}
          canManage
        />
      )}
    </div>
  );
}
