"use client";

import { useState } from "react";
import { Plus, ShieldCheck, Trash2 } from "@multica/ui/icons";
import { useQuery } from "@tanstack/react-query";
import {
  messageEventCatalogOptions,
  messageSourceApprovedTargetsOptions,
  messageSourceRoutesOptions,
  useRevokeMessageSourceTarget,
} from "@multica/core/message-delivery";
import { larkInstallationsOptions } from "@multica/core/lark";
import { projectListOptions } from "@multica/core/projects/queries";
import { useCurrentMember } from "@multica/core/permissions";
import { useWorkspaceId } from "@multica/core/hooks";
import { ApiError, errorCode } from "@multica/core/api";
import type {
  MessageSourceApprovedTarget,
  MessageSourceRoute,
} from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Alert, AlertDescription } from "@multica/ui/components/ui/alert";
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
import { toast } from "sonner";
import { useLocale, useT } from "../../i18n";
import { messageDeliveryErrorKey, messageSourceScopeKey } from "../copy";
import { SourceRouteRow } from "./source-route-row";
import { SourceRouteEditorDialog } from "./source-route-editor-dialog";
import { RouteDeliveriesDialog } from "./route-deliveries-dialog";

function formatDate(value: string | null | undefined, locale: string): string {
  if (!value) return "—";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString(locale, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/**
 * Team event subscriptions (OL-28), mounted in the workspace's Feishu
 * integration settings. These are TEAM source subscriptions (activity /
 * comment → group/topic), not copies of any member's personal inbox.
 * Configuration and target approvals are workspace owner/admin acts
 * (server: 403 message_target_admin_required), so plain members see an
 * explanatory note and the section never fires an admin-gated request for
 * them.
 */
export function TeamSubscriptionsSection() {
  const { t } = useT("message-delivery");
  const wsId = useWorkspaceId();
  const { role, isLoading: roleLoading } = useCurrentMember(wsId);
  const isAdmin = role === "owner" || role === "admin";

  const header = (
    <div className="space-y-1">
      <h2 className="text-body font-semibold">{t(($) => $.team.title)}</h2>
      <p className="text-caption text-muted-foreground">
        {t(($) => $.team.description)}
      </p>
    </div>
  );

  if (roleLoading) {
    return (
      <section className="space-y-3">
        {header}
        <Skeleton className="h-12 w-full" />
      </section>
    );
  }

  if (!isAdmin) {
    return (
      <section className="space-y-3">
        {header}
        <p className="text-caption text-muted-foreground">
          {t(($) => $.team.admin_required)}
        </p>
      </section>
    );
  }

  return (
    <section className="space-y-3">
      {header}
      <TeamSubscriptionsBody />
    </section>
  );
}

function TeamSubscriptionsBody() {
  const { t } = useT("message-delivery");
  const wsId = useWorkspaceId();

  // The unfiltered list returns the acting admin's team rules (their own
  // inbox rules are filtered out below — they belong to the personal
  // settings, not here).
  const routesQuery = useQuery(messageSourceRoutesOptions(wsId));
  const catalogQuery = useQuery(messageEventCatalogOptions(wsId));
  const installationsQuery = useQuery(larkInstallationsOptions(wsId));
  const projectsQuery = useQuery(projectListOptions(wsId));
  const approvalsQuery = useQuery(
    messageSourceApprovedTargetsOptions(wsId, { enabled: routesQuery.isSuccess }),
  );

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<MessageSourceRoute | null>(null);
  const [recordsRouteId, setRecordsRouteId] = useState<string | null>(null);

  const routes = (routesQuery.data ?? []).filter(
    (route) => route.source_kind === "activity" || route.source_kind === "comment",
  );
  const installations = installationsQuery.data?.installations ?? [];
  const projects = (projectsQuery.data ?? []).map((p) => ({ id: p.id, title: p.title }));
  const approvals = approvalsQuery.data ?? [];

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
    const copyKey = status === 404 ? "unsupported" : "load_failed";
    return (
      <Alert>
        <AlertDescription>{t(($) => $.team[copyKey])}</AlertDescription>
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
          <AlertDescription>{t(($) => $.team.load_failed)}</AlertDescription>
        </Alert>
      )}

      {sendingUnavailable && (
        <Alert>
          <AlertDescription>{t(($) => $.section.sending_unavailable)}</AlertDescription>
        </Alert>
      )}

      {routesQuery.isLoading ? (
        <div className="space-y-1">
          {Array.from({ length: 2 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </div>
      ) : routes.length === 0 ? (
        <div className="rounded-md border border-dashed p-4 text-center text-body text-muted-foreground">
          {noBot ? t(($) => $.team.no_bot) : t(($) => $.team.empty)}
        </div>
      ) : (
        <div className="rounded-md border overflow-hidden divide-y">
          {routes.map((route) => (
            <SourceRouteRow
              key={route.id}
              route={route}
              installations={installations}
              projects={projects}
              canManage
              onEdit={() => openEditor(route)}
              onShowRecords={() => setRecordsRouteId(route.id)}
            />
          ))}
        </div>
      )}

      {!noBot && (
        <div>
          <Button
            size="sm"
            variant="outline"
            onClick={() => openEditor(null)}
            disabled={installationsQuery.isSuccess !== true}
          >
            <Plus className="h-3.5 w-3.5 mr-1" />
            {t(($) => $.team.add)}
          </Button>
        </div>
      )}

      {routesQuery.isSuccess && (
        <TeamApprovedTargetsList
          approvals={approvals}
          isLoading={approvalsQuery.isLoading}
          isError={approvalsQuery.isError}
          onReload={() => approvalsQuery.refetch()}
          projects={projects}
        />
      )}

      {editorOpen && (
        <SourceRouteEditorDialog
          open={editorOpen}
          onOpenChange={setEditorOpen}
          mode="team"
          route={editing}
          installations={installations}
          approvals={approvals}
          projects={projects}
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

/**
 * Active team approvals (owner/admin only — the query is never fired for
 * plain members). Each grant is shown with its exact scope (source kind +
 * project range): an approval for one project never implies another, and
 * workspace-wide (null) is its own scope.
 */
function TeamApprovedTargetsList({
  approvals,
  isLoading,
  isError,
  onReload,
  projects,
}: {
  approvals: MessageSourceApprovedTarget[];
  isLoading: boolean;
  isError: boolean;
  onReload: () => void;
  projects: { id: string; title: string }[];
}) {
  const { t } = useT("message-delivery");
  const locale = useLocale();
  const revoke = useRevokeMessageSourceTarget();
  const [revoking, setRevoking] = useState<MessageSourceApprovedTarget | null>(null);

  const handleRevoke = () => {
    if (!revoking) return;
    revoke.mutate(
      { targetId: revoking.id },
      {
        onSuccess: (res) => {
          toast.success(
            t(($) => $.approvals.toast_revoked, { count: res.cancelled_deliveries }),
          );
          setRevoking(null);
        },
        onError: (e) => {
          toast.error(t(($) => $.error[messageDeliveryErrorKey(errorCode(e))]));
        },
      },
    );
  };

  return (
    <div className="space-y-2 pt-2">
      <h3 className="flex items-center gap-1.5 text-caption font-medium text-muted-foreground uppercase tracking-wider">
        <ShieldCheck className="h-3.5 w-3.5" />
        {t(($) => $.team.approvals_title)}
      </h3>
      {isLoading ? (
        <Skeleton className="h-8 w-full" />
      ) : isError ? (
        // A failed approvals read is never a confirmed empty list: keep the
        // error visible with an explicit reload entry.
        <Alert variant="destructive">
          <AlertDescription className="flex items-center justify-between gap-2">
            <span>{t(($) => $.team.approvals_load_failed)}</span>
            <Button size="sm" variant="outline" onClick={onReload}>
              {t(($) => $.editor.retry_reload)}
            </Button>
          </AlertDescription>
        </Alert>
      ) : approvals.length === 0 ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.team.approvals_empty)}
        </p>
      ) : (
        <div className="rounded-md border divide-y">
          {approvals.map((approval) => {
            const typeKey =
              approval.target_type === "group" || approval.target_type === "topic"
                ? approval.target_type
                : null;
            const scopeKey = messageSourceScopeKey(approval.source_kind);
            const projectTitle = approval.project_id
              ? (projects.find((p) => p.id === approval.project_id)?.title ?? approval.project_id)
              : null;
            return (
              <div key={approval.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2">
                <Badge variant="secondary" className="shrink-0">
                  {typeKey ? t(($) => $.target_type[typeKey]) : approval.target_type}
                </Badge>
                <Badge variant="outline" className="shrink-0">
                  {t(($) => $.source.kind[scopeKey])}
                </Badge>
                <Badge
                  variant="outline"
                  className="max-w-40 min-w-0 overflow-hidden"
                  title={projectTitle ?? undefined}
                >
                  <span className="truncate">
                    {projectTitle ?? t(($) => $.source.project_workspace)}
                  </span>
                </Badge>
                <code className="flex-1 min-w-0 truncate text-caption font-mono">
                  {approval.target_key}
                </code>
                <span className="shrink-0 text-caption text-muted-foreground tabular-nums">
                  {t(($) => $.approvals.approved_at, {
                    time: formatDate(approval.approved_at, locale),
                  })}
                </span>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setRevoking(approval)}
                  aria-label={t(($) => $.approvals.revoke)}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            );
          })}
        </div>
      )}
      <AlertDialog
        open={revoking != null}
        onOpenChange={(open) => {
          if (!open && !revoke.isPending) setRevoking(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.approvals.revoke_dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.approvals.revoke_dialog.description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={revoke.isPending}>
              {t(($) => $.approvals.revoke_dialog.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={handleRevoke}
              disabled={revoke.isPending}
              className="bg-destructive text-white hover:bg-destructive/90"
            >
              {revoke.isPending
                ? t(($) => $.approvals.revoke_dialog.revoking)
                : t(($) => $.approvals.revoke_dialog.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
