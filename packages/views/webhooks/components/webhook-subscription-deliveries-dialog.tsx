"use client";

import { useState } from "react";
import {
  CheckCircle2,
  XCircle,
  AlertTriangle,
  RotateCw,
  Webhook,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import {
  webhookDeliveriesOptions,
  webhookDeliveryOptions,
  useRedeliverWebhookSubscriptionDelivery,
} from "@multica/core/webhooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import { useT } from "../../i18n";
import {
  CodeBlock,
  MetaRow,
  formatDeliveryDate,
} from "../../common/delivery-primitives";
import type {
  OutboundWebhookDelivery,
  OutboundWebhookDeliveryStatus,
} from "@multica/core/types";

// --- Status visuals -------------------------------------------------------
// Exhaustive over the current backend enum, but every consumer site falls back
// to a generic "unknown" visual when the server adds a value (API Response
// Compatibility — see CLAUDE.md).
type StatusVisual = { color: string; icon: typeof CheckCircle2 };

const STATUS_VISUAL: Record<OutboundWebhookDeliveryStatus, StatusVisual> = {
  delivered: { color: "text-emerald-500", icon: CheckCircle2 },
  failed: { color: "text-destructive", icon: XCircle },
};

const UNKNOWN_VISUAL: StatusVisual = {
  color: "text-muted-foreground",
  icon: AlertTriangle,
};

function visualForStatus(status: string): StatusVisual {
  return (STATUS_VISUAL as Record<string, StatusVisual>)[status] ?? UNKNOWN_VISUAL;
}

// --- Dialog ---------------------------------------------------------------

export function WebhookSubscriptionDeliveriesDialog({
  subscriptionId,
  open,
  onOpenChange,
}: {
  subscriptionId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data, isLoading } = useQuery(
    webhookDeliveriesOptions(wsId, subscriptionId, { enabled: open }),
  );
  const deliveries = data?.deliveries ?? [];

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogTitle className="flex items-center gap-2">
          <Webhook className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.webhooks.deliveries.title)}
        </DialogTitle>
        {isLoading ? (
          <div className="space-y-1 pt-1">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : deliveries.length === 0 ? (
          <div className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">
            {t(($) => $.webhooks.deliveries.empty)}
          </div>
        ) : (
          <div className="rounded-md border overflow-hidden">
            {deliveries.map((delivery) => (
              <DeliveryRow
                key={delivery.id}
                delivery={delivery}
                subscriptionId={subscriptionId}
              />
            ))}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

// --- Row ------------------------------------------------------------------

function DeliveryRow({
  delivery,
  subscriptionId,
}: {
  delivery: OutboundWebhookDelivery;
  subscriptionId: string;
}) {
  const { t } = useT("settings");
  const [open, setOpen] = useState(false);
  const visual = visualForStatus(delivery.status);
  const StatusIcon = visual.icon;
  const statusLabel =
    t(($) => $.webhooks.deliveries.status[delivery.status as OutboundWebhookDeliveryStatus]) ??
    delivery.status;

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm hover:bg-accent/30 transition-colors"
      >
        <StatusIcon className={cn("h-4 w-4 shrink-0", visual.color)} />
        <span className={cn("w-20 shrink-0 text-xs font-medium", visual.color)}>
          {statusLabel}
        </span>
        <span className="flex-1 min-w-0 truncate font-mono text-xs text-muted-foreground">
          {delivery.event}
        </span>
        {delivery.response_status != null && (
          <Badge variant="outline" className="shrink-0 tabular-nums">
            {delivery.response_status}
          </Badge>
        )}
        {delivery.redelivered_from_id && (
          <Badge variant="secondary" className="shrink-0">
            <RotateCw className="h-3 w-3" />
            {t(($) => $.webhooks.deliveries.redelivered_badge)}
          </Badge>
        )}
        {delivery.attempt_count > 1 && (
          <Badge variant="outline" className="shrink-0">
            {t(($) => $.webhooks.deliveries.attempts, { count: delivery.attempt_count })}
          </Badge>
        )}
        <span className="w-32 shrink-0 text-right text-xs text-muted-foreground tabular-nums">
          {formatDeliveryDate(delivery.created_at)}
        </span>
      </button>
      {open && (
        <DeliveryDetailDialog
          open={open}
          onOpenChange={setOpen}
          subscriptionId={subscriptionId}
          delivery={delivery}
        />
      )}
    </>
  );
}

// --- Detail dialog --------------------------------------------------------

function DeliveryDetailDialog({
  open,
  onOpenChange,
  subscriptionId,
  delivery,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  subscriptionId: string;
  delivery: OutboundWebhookDelivery;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: detail, isLoading } = useQuery(
    webhookDeliveryOptions(wsId, subscriptionId, delivery.id, { enabled: open }),
  );
  const full = detail ?? delivery;
  const visual = visualForStatus(full.status);
  const StatusIcon = visual.icon;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogTitle className="flex items-center gap-2">
          <Webhook className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.webhooks.deliveries.detail_title)}
        </DialogTitle>
        <div className="space-y-4 pt-1">
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex items-center gap-2">
              <StatusIcon className={cn("h-4 w-4 shrink-0", visual.color)} />
              <span className={cn("text-sm font-medium", visual.color)}>
                {t(($) => $.webhooks.deliveries.status[full.status as OutboundWebhookDeliveryStatus]) ??
                  full.status}
              </span>
            </div>
            <code className="rounded bg-muted px-2 py-0.5 text-xs font-mono">
              {full.event}
            </code>
          </div>

          <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
            <MetaRow
              label={t(($) => $.webhooks.deliveries.created_at)}
              value={formatDeliveryDate(full.created_at)}
            />
            <MetaRow
              label={t(($) => $.webhooks.deliveries.attempt_count)}
              value={String(full.attempt_count)}
            />
            <MetaRow
              label={t(($) => $.webhooks.deliveries.response_status)}
              value={full.response_status != null ? String(full.response_status) : "—"}
            />
            {full.redelivered_from_id && (
              <MetaRow
                label={t(($) => $.webhooks.deliveries.redelivered_from)}
                value={full.redelivered_from_id}
                mono
              />
            )}
          </dl>

          {full.error && (
            <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
              <div className="font-medium">
                {t(($) => $.webhooks.deliveries.error_label)}
              </div>
              <div className="mt-0.5 font-mono break-all">{full.error}</div>
            </div>
          )}

          <DetailBodies detail={detail} isLoading={isLoading} />

          <div className="flex items-center justify-end pt-2">
            <RedeliverButton
              subscriptionId={subscriptionId}
              deliveryId={full.id}
              onSuccess={() => onOpenChange(false)}
            />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function DetailBodies({
  detail,
  isLoading,
}: {
  detail: OutboundWebhookDelivery | undefined;
  isLoading: boolean;
}) {
  const { t } = useT("settings");
  if (isLoading && !detail) {
    return (
      <div className="space-y-2">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-16 w-full" />
      </div>
    );
  }
  if (!detail) return null;
  const cbLabels = {
    copy: t(($) => $.webhooks.deliveries.copy),
    copied: t(($) => $.webhooks.deliveries.copied),
    copiedToast: t(($) => $.webhooks.deliveries.copied),
    copyFailedToast: t(($) => $.webhooks.deliveries.copy_failed),
    truncated: t(($) => $.webhooks.deliveries.truncated_marker),
  };
  return (
    <div className="space-y-3">
      {detail.request_body && (
        <CodeBlock
          label={t(($) => $.webhooks.deliveries.request_body)}
          value={detail.request_body}
          labels={cbLabels}
        />
      )}
      {detail.response_body && (
        <CodeBlock
          label={t(($) => $.webhooks.deliveries.response_body)}
          value={detail.response_body}
          labels={cbLabels}
        />
      )}
    </div>
  );
}

function RedeliverButton({
  subscriptionId,
  deliveryId,
  onSuccess,
}: {
  subscriptionId: string;
  deliveryId: string;
  onSuccess: () => void;
}) {
  const { t } = useT("settings");
  const redeliver = useRedeliverWebhookSubscriptionDelivery(subscriptionId);

  const handleClick = async () => {
    try {
      await redeliver.mutateAsync(deliveryId);
      toast.success(t(($) => $.webhooks.deliveries.redeliver_toast_success));
      onSuccess();
    } catch (e: unknown) {
      const message =
        e instanceof Error
          ? e.message
          : t(($) => $.webhooks.deliveries.redeliver_toast_failed);
      toast.error(message);
    }
  };

  return (
    <Button size="sm" variant="outline" onClick={handleClick} disabled={redeliver.isPending}>
      <RotateCw className={cn("h-3.5 w-3.5 mr-1", redeliver.isPending && "animate-spin")} />
      {redeliver.isPending
        ? t(($) => $.webhooks.deliveries.redeliver_in_progress)
        : t(($) => $.webhooks.deliveries.redeliver_action)}
    </Button>
  );
}
