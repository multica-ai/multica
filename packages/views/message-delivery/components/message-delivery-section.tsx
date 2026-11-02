"use client";

import { useState } from "react";
import {
  Bot,
  MessagesSquare,
  Pencil,
  Plus,
  Send,
  ShieldCheck,
  Trash2,
  User,
  Users,
} from "@multica/ui/icons";
import { useQuery } from "@tanstack/react-query";
import {
  messageApprovedTargetsOptions,
  messageRoutesOptions,
  useDeleteMessageRoute,
  useRevokeMessageTarget,
  useSetMessageRouteEnabled,
  useTestMessageRoute,
} from "@multica/core/message-delivery";
import { larkInstallationsOptions } from "@multica/core/lark";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useCurrentMember } from "@multica/core/permissions";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { ApiError, errorCode } from "@multica/core/api";
import type {
  MessageApprovedTarget,
  MessageDelivery,
  MessageRoute,
} from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Switch } from "@multica/ui/components/ui/switch";
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
import { AppLink } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { MessageRouteEditorDialog } from "./route-editor-dialog";
import {
  messageDeliveryErrorKey,
  messageDeliveryStatusKey,
} from "../copy";

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

// --- Section --------------------------------------------------------------

/**
 * The "结果推送" configuration surface on the automation detail page. It is a
 * SEPARATE save area from the automation itself: routes are their own
 * revision-guarded resource, so a failed push save never masquerades as an
 * enabled push and a saved automation never implies a saved route.
 *
 * Server gates (OL-25): every read/write here goes through the autopilot
 * write gate. A read-only caller gets a real 403 and sees the restricted
 * state; a server without OL-25 answers 404 and sees the unsupported state.
 */
export function MessageDeliverySection({
  autopilotId,
  canWrite,
  executionMode,
}: {
  autopilotId: string;
  canWrite: boolean;
  executionMode: string;
}) {
  const { t } = useT("message-delivery");
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const isAdmin = role === "owner" || role === "admin";

  const routesQuery = useQuery(messageRoutesOptions(wsId, autopilotId));
  const installationsQuery = useQuery(larkInstallationsOptions(wsId));
  const membersQuery = useQuery(memberListOptions(wsId));
  // Approvals are owner/admin-only server-side; collaborators never fetch
  // them (a 403 there is expected, not an error to show).
  const approvalsQuery = useQuery(
    messageApprovedTargetsOptions(wsId, autopilotId, {
      enabled: isAdmin && routesQuery.isSuccess,
    }),
  );

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<MessageRoute | null>(null);

  const routes = routesQuery.data ?? [];
  const installations = installationsQuery.data?.installations ?? [];
  const members = membersQuery.data ?? [];
  const approvals = approvalsQuery.data ?? [];

  const openEditor = (route: MessageRoute | null) => {
    setEditing(route);
    setEditorOpen(true);
  };

  const addButton = canWrite && (
    <Button
      size="sm"
      variant="outline"
      onClick={() => openEditor(null)}
      disabled={routesQuery.isError === true}
    >
      <Plus className="h-3.5 w-3.5 mr-1" />
      {t(($) => $.section.add_target)}
    </Button>
  );

  const header = (
    <div className="flex items-center justify-between">
      <h2 className="text-body font-medium text-muted-foreground uppercase tracking-wider">
        {t(($) => $.section.title)}
      </h2>
      {addButton}
    </div>
  );

  if (routesQuery.isError) {
    const err = routesQuery.error;
    const status = err instanceof ApiError ? err.status : 0;
    const copyKey =
      status === 403
        ? "read_only"
        : status === 404
          ? "unsupported"
          : "load_failed";
    return (
      <section className="space-y-3">
        {header}
        <Alert>
          <AlertDescription>{t(($) => $.section[copyKey])}</AlertDescription>
        </Alert>
      </section>
    );
  }

  const sendingUnavailable = installationsQuery.data?.configured === false;
  const noBot =
    installationsQuery.isSuccess &&
    installations.every((inst) => inst.status !== "active");

  return (
    <section className="space-y-3">
      {header}
      <p className="text-caption text-muted-foreground">
        {t(($) => $.section.description)}
      </p>

      {sendingUnavailable && (
        <Alert>
          <AlertDescription>
            {t(($) => $.section.sending_unavailable)}
          </AlertDescription>
        </Alert>
      )}

      {routesQuery.isLoading ? (
        <div className="space-y-1">
          {Array.from({ length: 2 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </div>
      ) : routes.length === 0 ? (
        noBot ? (
          <NoBotHint />
        ) : (
          <div className="rounded-md border border-dashed p-4 text-center text-body text-muted-foreground">
            {t(($) => $.section.empty)}
          </div>
        )
      ) : (
        <div className="rounded-md border overflow-hidden divide-y">
          {routes.map((route) => (
            <RouteRow
              key={route.id}
              route={route}
              autopilotId={autopilotId}
              canWrite={canWrite}
              installations={installations}
              members={members}
              onEdit={() => openEditor(route)}
            />
          ))}
        </div>
      )}

      {isAdmin && routesQuery.isSuccess && (
        <ApprovedTargetsList
          autopilotId={autopilotId}
          approvals={approvals}
          isLoading={approvalsQuery.isLoading}
        />
      )}

      {editorOpen && (
        <MessageRouteEditorDialog
          open={editorOpen}
          onOpenChange={setEditorOpen}
          autopilotId={autopilotId}
          route={editing}
          executionMode={executionMode}
          installations={installations}
          members={members}
          approvals={approvals}
          isAdmin={isAdmin}
        />
      )}
    </section>
  );
}

function NoBotHint() {
  const { t } = useT("message-delivery");
  const wsPaths = useWorkspacePaths();
  return (
    <div className="rounded-md border border-dashed p-4 text-center text-body text-muted-foreground space-y-2">
      <p>{t(($) => $.section.no_bot)}</p>
      <AppLink
        href={wsPaths.settings()}
        className="inline-block text-caption font-medium text-foreground underline underline-offset-2"
      >
        {t(($) => $.section.install_bot)}
      </AppLink>
    </div>
  );
}

// --- Route row ------------------------------------------------------------

function RouteRow({
  route,
  autopilotId,
  canWrite,
  installations,
  members,
  onEdit,
}: {
  route: MessageRoute;
  autopilotId: string;
  canWrite: boolean;
  installations: { id: string; agent_id: string; status: string }[];
  members: { user_id: string; name: string; email: string }[];
  onEdit: () => void;
}) {
  const { t } = useT("message-delivery");
  const { getActorName } = useActorName();
  const setEnabled = useSetMessageRouteEnabled();
  const deleteRoute = useDeleteMessageRoute();
  const testSend = useTestMessageRoute();
  const [deleteOpen, setDeleteOpen] = useState(false);

  const installation = installations.find((inst) => inst.id === route.installation_id);
  const botLabel = installation
    ? getActorName("agent", installation.agent_id)
    : t(($) => $.routes.bot_unknown);
  const botDead = installation == null || installation.status !== "active";

  const member =
    route.target_type === "member"
      ? members.find((m) => m.user_id === route.target_user_id)
      : undefined;
  const conditionKey =
    route.conditions === "success" ||
    route.conditions === "failure" ||
    route.conditions === "all"
      ? route.conditions
      : null;
  const contentModeKey =
    route.content_mode === "summary" || route.content_mode === "with_output"
      ? route.content_mode
      : null;
  const TargetIcon =
    route.target_type === "member"
      ? User
      : route.target_type === "group"
        ? Users
        : MessagesSquare;
  const targetLabel =
    route.target_type === "member"
      ? (member ? member.name || member.email : t(($) => $.routes.member_unknown))
      : route.target_type === "group"
        ? (route.target_chat_id ?? route.target_key)
        : `${route.target_chat_id ?? "—"} ↳ ${route.target_message_id ?? "—"}`;

  const handleToggle = (checked: boolean) => {
    setEnabled.mutate(
      {
        autopilotId,
        routeId: route.id,
        enabled: checked,
        expectedRevision: route.revision,
      },
      {
        onSuccess: () => {
          toast.success(
            checked ? t(($) => $.routes.toast_enabled) : t(($) => $.routes.toast_disabled),
          );
        },
        onError: (e) => {
          // A 409 means somebody else edited the rule; the settled
          // invalidation already refetched it, so just tell the user.
          toast.error(t(($) => $.error[messageDeliveryErrorKey(errorCode(e))]));
        },
      },
    );
  };

  const handleTestSend = () => {
    testSend.mutate(
      { autopilotId, routeId: route.id },
      {
        onSuccess: (delivery: MessageDelivery) => {
          const label = t(($) => $.deliveries.status[messageDeliveryStatusKey(delivery.status)]);
          const message = t(($) => $.routes.test_toast, { status: label });
          if (delivery.status === "sent") toast.success(message);
          else if (delivery.status === "failed") toast.error(message);
          else toast.warning(message);
        },
        onError: (e) => {
          toast.error(t(($) => $.error[messageDeliveryErrorKey(errorCode(e))]));
        },
      },
    );
  };

  const handleDelete = () => {
    deleteRoute.mutate(
      { autopilotId, routeId: route.id },
      {
        onSuccess: () => {
          toast.success(t(($) => $.routes.toast_deleted));
          setDeleteOpen(false);
        },
        onError: (e) => {
          toast.error(t(($) => $.error[messageDeliveryErrorKey(errorCode(e))]));
        },
      },
    );
  };

  return (
    <div className="flex items-center gap-3 px-4 py-2.5">
      <Switch
        size="sm"
        checked={route.enabled}
        disabled={!canWrite || setEnabled.isPending}
        onCheckedChange={handleToggle}
        aria-label={t(($) => $.section.title)}
      />
      <TargetIcon className="h-4 w-4 shrink-0 text-muted-foreground" />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 min-w-0">
          <span className="truncate text-body">{targetLabel}</span>
          {botDead && (
            <Badge variant="destructive" className="shrink-0">
              {t(($) => $.routes.bot_revoked)}
            </Badge>
          )}
        </div>
        <div className="mt-0.5 flex items-center gap-1.5 text-caption text-muted-foreground min-w-0">
          <Bot className="h-3 w-3 shrink-0" />
          <span className="truncate">{botLabel}</span>
          <span aria-hidden>·</span>
          <span>
            {conditionKey ? t(($) => $.condition[conditionKey]) : route.conditions}
          </span>
          <span aria-hidden>·</span>
          <span>
            {contentModeKey ? t(($) => $.content_mode[contentModeKey]) : route.content_mode}
          </span>
          <span aria-hidden>·</span>
          <span className="shrink-0 tabular-nums">
            {t(($) => $.routes.revision, { revision: route.revision })}
          </span>
        </div>
      </div>
      {canWrite && (
        <div className="flex shrink-0 items-center gap-1">
          <Button
            size="sm"
            variant="ghost"
            disabled={!route.enabled || testSend.isPending}
            title={route.enabled ? undefined : t(($) => $.error.route_disabled)}
            onClick={handleTestSend}
          >
            <Send className="h-3.5 w-3.5 sm:mr-1" />
            <span className="hidden sm:inline">
              {testSend.isPending
                ? t(($) => $.routes.test_sending)
                : t(($) => $.routes.test_send)}
            </span>
          </Button>
          <Button size="sm" variant="ghost" onClick={onEdit} aria-label={t(($) => $.routes.edit)}>
            <Pencil className="h-3.5 w-3.5" />
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setDeleteOpen(true)}
            aria-label={t(($) => $.routes.delete)}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      )}
      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.routes.delete_dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.routes.delete_dialog.description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteRoute.isPending}>
              {t(($) => $.routes.delete_dialog.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              disabled={deleteRoute.isPending}
              className="bg-destructive text-white hover:bg-destructive/90"
            >
              {deleteRoute.isPending
                ? t(($) => $.routes.delete_dialog.deleting)
                : t(($) => $.routes.delete_dialog.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// --- Approved targets (owner/admin only) -----------------------------------

function ApprovedTargetsList({
  autopilotId,
  approvals,
  isLoading,
}: {
  autopilotId: string;
  approvals: MessageApprovedTarget[];
  isLoading: boolean;
}) {
  const { t } = useT("message-delivery");
  const locale = useLocale();
  const revoke = useRevokeMessageTarget();
  const [revoking, setRevoking] = useState<MessageApprovedTarget | null>(null);

  const handleRevoke = () => {
    if (!revoking) return;
    revoke.mutate(
      { autopilotId, targetId: revoking.id },
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
        {t(($) => $.approvals.title)}
      </h3>
      {isLoading ? (
        <Skeleton className="h-8 w-full" />
      ) : approvals.length === 0 ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.approvals.empty)}
        </p>
      ) : (
        <div className="rounded-md border divide-y">
          {approvals.map((approval) => {
            const typeKey =
              approval.target_type === "group" || approval.target_type === "topic"
                ? approval.target_type
                : null;
            return (
              <div key={approval.id} className="flex items-center gap-3 px-3 py-2">
                <Badge variant="secondary" className="shrink-0">
                  {typeKey ? t(($) => $.target_type[typeKey]) : approval.target_type}
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
