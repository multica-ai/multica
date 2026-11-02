"use client";

import { useState } from "react";
import {
  Bot,
  History,
  MessagesSquare,
  Pencil,
  Send,
  Trash2,
  User,
  Users,
} from "@multica/ui/icons";
import {
  useDeleteMessageSourceRoute,
  useSetMessageSourceRouteEnabled,
  useTestMessageSourceRoute,
} from "@multica/core/message-delivery";
import { useActorName } from "@multica/core/workspace/hooks";
import { errorCode } from "@multica/core/api";
import type { MessageSourceDelivery, MessageSourceRoute } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
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
import {
  messageDeliveryErrorKey,
  messageDeliveryStatusKey,
  messageSourceScopeKey,
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

/**
 * One personal/team route row. Enable/disable always carries the row's
 * displayed revision as expected_revision; a 409 needs no special UI because
 * the mutation's settled invalidation refetches the truth. Test-send runs the
 * REAL send path and reports the delivery's actual status — never a simulated
 * success.
 */
export function SourceRouteRow({
  route,
  installations,
  projects,
  canManage,
  onEdit,
  onShowRecords,
}: {
  route: MessageSourceRoute;
  installations: { id: string; agent_id: string; status: string }[];
  /** Team routes only: resolve project_id to a title for the scope badge. */
  projects?: { id: string; title: string }[];
  canManage: boolean;
  onEdit: () => void;
  onShowRecords: () => void;
}) {
  const { t } = useT("message-delivery");
  const locale = useLocale();
  const { getActorName } = useActorName();
  const setEnabled = useSetMessageSourceRouteEnabled();
  const deleteRoute = useDeleteMessageSourceRoute();
  const testSend = useTestMessageSourceRoute();
  const [deleteOpen, setDeleteOpen] = useState(false);

  const isPersonal = route.source_kind === "inbox";

  const installation = installations.find((inst) => inst.id === route.installation_id);
  const botLabel = installation
    ? getActorName("agent", installation.agent_id)
    : t(($) => $.routes.bot_unknown);
  const botDead = installation == null || installation.status !== "active";

  const TargetIcon = isPersonal
    ? User
    : route.target_type === "topic"
      ? MessagesSquare
      : Users;
  const targetLabel = isPersonal
    ? t(($) => $.personal.target_self)
    : route.target_type === "topic"
      ? `${route.target_chat_id ?? "—"} ↳ ${route.target_message_id ?? "—"}`
      : (route.target_chat_id ?? route.target_key);

  const scopeKey = messageSourceScopeKey(route.source_kind);
  const projectTitle = route.project_id
    ? (projects?.find((p) => p.id === route.project_id)?.title ?? route.project_id)
    : null;

  const handleToggle = (checked: boolean) => {
    setEnabled.mutate(
      { routeId: route.id, enabled: checked, expectedRevision: route.revision },
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
      { routeId: route.id },
      {
        onSuccess: (delivery: MessageSourceDelivery) => {
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
      { routeId: route.id },
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
        disabled={!canManage || setEnabled.isPending}
        onCheckedChange={handleToggle}
        aria-label={t(($) => $.source.kind[scopeKey])}
      />
      <TargetIcon className="h-4 w-4 shrink-0 text-muted-foreground" />
      <div className="flex-1 min-w-0">
        <div className="flex flex-wrap items-center gap-2 min-w-0">
          <span className="truncate text-body">{targetLabel}</span>
          {!isPersonal && (
            <Badge variant="secondary" className="shrink-0">
              {t(($) => $.source.kind[scopeKey])}
            </Badge>
          )}
          {!isPersonal && (
            <Badge variant="outline" className="shrink-0">
              {projectTitle ?? t(($) => $.source.project_workspace)}
            </Badge>
          )}
          {botDead && (
            <Badge variant="destructive" className="shrink-0">
              {t(($) => $.routes.bot_revoked)}
            </Badge>
          )}
        </div>
        <div className="mt-0.5 flex flex-wrap items-center gap-1.5 text-caption text-muted-foreground min-w-0">
          <Bot className="h-3 w-3 shrink-0" />
          <span className="truncate">{botLabel}</span>
          <span aria-hidden>·</span>
          <span>
            {route.event_types.length === 0
              ? t(($) => $.source.events_all)
              : t(($) => $.source.events_count, { count: route.event_types.length })}
          </span>
          <span aria-hidden>·</span>
          <span className="tabular-nums">
            {t(($) => $.source.effective_from, { time: formatDate(route.effective_from, locale) })}
          </span>
          <span aria-hidden>·</span>
          <span className="shrink-0 tabular-nums">
            {t(($) => $.routes.revision, { revision: route.revision })}
          </span>
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <Button
          size="sm"
          variant="ghost"
          onClick={onShowRecords}
          aria-label={t(($) => $.source.records)}
        >
          <History className="h-3.5 w-3.5 sm:mr-1" />
          <span className="hidden sm:inline">{t(($) => $.source.records)}</span>
        </Button>
        {canManage && (
          <>
            <Button
              size="sm"
              variant="ghost"
              disabled={!route.enabled || testSend.isPending}
              title={route.enabled ? undefined : t(($) => $.error.route_disabled)}
              onClick={handleTestSend}
              // The visible text hides below sm; the accessible name stays.
              aria-label={
                testSend.isPending
                  ? t(($) => $.routes.test_sending)
                  : t(($) => $.routes.test_send)
              }
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
          </>
        )}
      </div>
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
