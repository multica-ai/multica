"use client";

import { useState } from "react";
import {
  CheckCircle2,
  XCircle,
  Loader2,
  Ban,
  AlertTriangle,
  ShieldOff,
  RotateCw,
  Copy,
  Check,
  Webhook,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import {
  autopilotDeliveriesOptions,
  autopilotDeliveryOptions,
  useReplayAutopilotDelivery,
} from "@multica/core/autopilots";
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
import { copyText } from "@multica/ui/lib/clipboard";
import { toast } from "sonner";
import { useT } from "../../i18n";
import type {
  WebhookDelivery,
  WebhookDeliveryStatus,
  WebhookSignatureStatus,
} from "@multica/core/types";

// --- Status visuals -------------------------------------------------------

// Mapping is exhaustive over the current backend enum but every consumer
// site falls back to a generic "unknown" visual when the server adds a new
// value — see the API Response Compatibility rules in CLAUDE.md.
type StatusVisual = {
  color: string;
  icon: typeof CheckCircle2;
  spin?: boolean;
};

const STATUS_VISUAL: Record<WebhookDeliveryStatus, StatusVisual> = {
  queued: { color: "text-blue-500", icon: Loader2, spin: true },
  dispatched: { color: "text-emerald-500", icon: CheckCircle2 },
  // Signature failures and pre-flight bouncebacks land here. Read as a
  // failure visually, the dialog footer explains the reason.
  rejected: { color: "text-destructive", icon: ShieldOff },
  // Ignored covers paused/disabled/archived autopilots — same payload was
  // received but no run was created. Muted so it doesn't look like a bug.
  ignored: { color: "text-muted-foreground", icon: Ban },
  failed: { color: "text-destructive", icon: XCircle },
};

const UNKNOWN_VISUAL: StatusVisual = {
  color: "text-muted-foreground",
  icon: AlertTriangle,
};

function visualForStatus(status: string): StatusVisual {
  return (STATUS_VISUAL as Record<string, StatusVisual>)[status] ?? UNKNOWN_VISUAL;
}

// --- Helpers --------------------------------------------------------------

function formatDate(value: string): string {
  if (!value) return "—";
  return new Date(value).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

// A delivery is replayable when (a) the server allows it (signature is not
// invalid AND the delivery itself wasn't rejected) and (b) we have something
// to replay (raw_body / received). We mirror the server's rule rather than
// rely on the response — keeping the button disabled saves a 400 round-trip.
function canReplay(delivery: WebhookDelivery): boolean {
  if (delivery.signature_status === "invalid") return false;
  if (delivery.status === "rejected") return false;
  // `queued` deliveries are mid-flight on the server; replay would race the
  // synchronous dispatch path. Once they settle, the user can replay.
  if (delivery.status === "queued") return false;
  return true;
}

// --- Section --------------------------------------------------------------

export function WebhookDeliveriesSection({
  autopilotId,
  hasWebhookTrigger,
}: {
  autopilotId: string;
  hasWebhookTrigger: boolean;
}) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();

  const { data: deliveries = [], isLoading } = useQuery(
    autopilotDeliveriesOptions(wsId, autopilotId, {
      enabled: hasWebhookTrigger,
    }),
  );

  // No webhook trigger configured → the entire section is irrelevant. We hide
  // it rather than render an empty card to keep the detail page short for
  // schedule-only autopilots.
  if (!hasWebhookTrigger) return null;

  return (
    <section className="space-y-3">
      <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">
        {t(($) => $.deliveries.section_title)}
      </h2>
      {isLoading ? (
        <div className="space-y-1">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      ) : deliveries.length === 0 ? (
        <div className="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">
          {t(($) => $.deliveries.empty)}
        </div>
      ) : (
        <div className="rounded-md border overflow-hidden">
          {deliveries.map((delivery) => (
            <DeliveryRow
              key={delivery.id}
              delivery={delivery}
              autopilotId={autopilotId}
            />
          ))}
        </div>
      )}
    </section>
  );
}

// --- Row ------------------------------------------------------------------

function DeliveryRow({
  delivery,
  autopilotId,
}: {
  delivery: WebhookDelivery;
  autopilotId: string;
}) {
  const { t } = useT("autopilots");
  const [open, setOpen] = useState(false);

  const visual = visualForStatus(delivery.status);
  const StatusIcon = visual.icon;
  const statusLabel =
    t(($) => $.deliveries.status[delivery.status as WebhookDeliveryStatus]) ??
    delivery.status;
  const providerLabel = delivery.provider || "—";

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm hover:bg-accent/30 transition-colors"
      >
        <StatusIcon
          className={cn(
            "h-4 w-4 shrink-0",
            visual.color,
            visual.spin && "animate-spin",
          )}
        />
        <span className={cn("w-24 shrink-0 text-xs font-medium", visual.color)}>
          {statusLabel}
        </span>
        <span className="w-20 shrink-0 text-xs text-muted-foreground truncate">
          {providerLabel}
        </span>
        <span className="flex-1 min-w-0 text-xs text-muted-foreground truncate font-mono">
          {delivery.event || t(($) => $.webhook_payload.unknown_event)}
        </span>
        {delivery.replayed_from_delivery_id && (
          <Badge variant="secondary" className="shrink-0">
            <RotateCw className="h-3 w-3" />
            {t(($) => $.deliveries.row.replay_badge)}
          </Badge>
        )}
        {delivery.attempt_count > 1 && (
          <Badge variant="outline" className="shrink-0">
            {t(($) => $.deliveries.row.attempts, {
              count: delivery.attempt_count,
            })}
          </Badge>
        )}
        <span className="w-32 shrink-0 text-right text-xs text-muted-foreground tabular-nums">
          {formatDate(delivery.received_at || delivery.created_at)}
        </span>
      </button>
      {open && (
        <DeliveryDetailDialog
          open={open}
          onOpenChange={setOpen}
          autopilotId={autopilotId}
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
  autopilotId,
  delivery,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  autopilotId: string;
  delivery: WebhookDelivery;
}) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const { data: detail, isLoading } = useQuery(
    autopilotDeliveryOptions(wsId, autopilotId, delivery.id, { enabled: open }),
  );
  // Use the detail row when loaded, otherwise the slim row from the list.
  // The slim row is missing raw_body / response_body / selected_headers; the
  // dialog renders skeleton placeholders for those sections while detail is
  // still loading.
  const full = detail ?? delivery;
  const visual = visualForStatus(full.status);
  const StatusIcon = visual.icon;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* max-h + overflow-y-auto: webhook bodies + headers + response can
          easily exceed viewport height. Without a cap the dialog grows past
          the screen edge and the bottom (e.g. Replay button) becomes
          unreachable. 85vh leaves breathing room around the dialog. */}
      <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogTitle className="flex items-center gap-2">
          <Webhook className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.deliveries.detail.title)}
        </DialogTitle>
        <div className="space-y-4 pt-1">
          {/* Header row — status / provider / event */}
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex items-center gap-2">
              <StatusIcon
                className={cn(
                  "h-4 w-4 shrink-0",
                  visual.color,
                  visual.spin && "animate-spin",
                )}
              />
              <span className={cn("text-sm font-medium", visual.color)}>
                {t(($) => $.deliveries.status[full.status as WebhookDeliveryStatus]) ??
                  full.status}
              </span>
            </div>
            <Badge variant="outline">{full.provider || "—"}</Badge>
            <code className="rounded bg-muted px-2 py-0.5 text-xs font-mono">
              {full.event || t(($) => $.webhook_payload.unknown_event)}
            </code>
            <SignatureBadge status={full.signature_status as WebhookSignatureStatus} />
          </div>

          {/* Meta grid */}
          <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
            <MetaRow
              label={t(($) => $.deliveries.detail.received_at)}
              value={formatDate(full.received_at)}
            />
            <MetaRow
              label={t(($) => $.deliveries.detail.last_attempt_at)}
              value={formatDate(full.last_attempt_at)}
            />
            <MetaRow
              label={t(($) => $.deliveries.detail.attempt_count)}
              value={String(full.attempt_count)}
            />
            <MetaRow
              label={t(($) => $.deliveries.detail.dispatch_attempts)}
              value={String(full.dispatch_attempts)}
            />
            {full.status === "queued" && (
              <MetaRow
                label={t(($) => $.deliveries.detail.available_at)}
                value={formatDate(full.available_at)}
              />
            )}
            <MetaRow
              label={t(($) => $.deliveries.detail.response_status)}
              value={full.response_status != null ? String(full.response_status) : "—"}
            />
            <MetaRow
              label={t(($) => $.deliveries.detail.dedupe_key)}
              value={full.dedupe_key ?? "—"}
              mono
            />
            <MetaRow
              label={t(($) => $.deliveries.detail.dedupe_source)}
              value={full.dedupe_source ?? "—"}
            />
            {full.content_type && (
              <MetaRow
                label={t(($) => $.deliveries.detail.content_type)}
                value={full.content_type}
                mono
              />
            )}
            {full.replayed_from_delivery_id && (
              <MetaRow
                label={t(($) => $.deliveries.detail.replayed_from)}
                value={full.replayed_from_delivery_id}
                mono
              />
            )}
          </dl>

          {full.error && (
            <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
              <div className="font-medium">
                {t(($) => $.deliveries.detail.error_label)}
              </div>
              <div className="mt-0.5 font-mono break-all">{full.error}</div>
            </div>
          )}

          {/* Raw body + response body + headers, all loaded lazily */}
          <DetailSections detail={detail} isLoading={isLoading} />

          {/* Replay button */}
          <div className="flex items-center justify-between pt-2">
            <ReplayHint delivery={full} />
            <ReplayButton
              autopilotId={autopilotId}
              delivery={full}
              onSuccess={() => onOpenChange(false)}
            />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function MetaRow({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="flex flex-col">
      <dt className="text-muted-foreground">{label}</dt>
      <dd
        className={cn(
          "truncate text-foreground",
          mono && "font-mono",
        )}
        title={value}
      >
        {value}
      </dd>
    </div>
  );
}

function SignatureBadge({ status }: { status: WebhookSignatureStatus | string }) {
  const { t } = useT("autopilots");
  let variant: "default" | "secondary" | "destructive" | "outline" = "outline";
  if (status === "valid") variant = "default";
  else if (status === "invalid") variant = "destructive";
  else if (status === "missing") variant = "secondary";
  return (
    <Badge variant={variant}>
      {t(($) => $.deliveries.signature[status as WebhookSignatureStatus]) ?? status}
    </Badge>
  );
}

function DetailSections({
  detail,
  isLoading,
}: {
  detail: WebhookDelivery | undefined;
  isLoading: boolean;
}) {
  const { t } = useT("autopilots");
  if (isLoading && !detail) {
    return (
      <div className="space-y-2">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-16 w-full" />
      </div>
    );
  }
  if (!detail) return null;
  return (
    <div className="space-y-3">
      {detail.raw_body && (
        <CodeBlock
          label={t(($) => $.deliveries.detail.raw_body)}
          value={detail.raw_body}
        />
      )}
      {detail.selected_headers && Object.keys(detail.selected_headers).length > 0 && (
        <CodeBlock
          label={t(($) => $.deliveries.detail.selected_headers)}
          value={JSON.stringify(detail.selected_headers, null, 2)}
        />
      )}
      {detail.response_body && (
        <CodeBlock
          label={t(($) => $.deliveries.detail.response_body)}
          value={detail.response_body}
        />
      )}
    </div>
  );
}

function CodeBlock({ label, value }: { label: string; value: string }) {
  const { t } = useT("autopilots");
  const [copied, setCopied] = useState(false);
  // Truncate in-DOM display for very large bodies; the Copy button still
  // yields the full string. 4 KiB is large enough for typical webhook
  // payloads while keeping the dialog responsive.
  const TRUNCATE_AT = 4096;
  const isTruncated = value.length > TRUNCATE_AT;
  const display = isTruncated ? value.slice(0, TRUNCATE_AT) : value;

  const handleCopy = async () => {
    if (await copyText(value)) {
      setCopied(true);
      toast.success(t(($) => $.webhook_payload.copied));
      setTimeout(() => setCopied(false), 1500);
    } else {
      toast.error(t(($) => $.webhook_payload.copy_failed));
    }
  };

  return (
    // min-w-0 lets this card shrink below the <pre>'s intrinsic min-content
    // width — without it, a minified single-line JSON body would push the
    // surrounding grid/flex cell (and the whole DialogContent) past the
    // viewport edge.
    <div className="min-w-0 rounded-md border bg-background">
      <div className="flex items-center justify-between border-b px-3 py-1.5 text-[11px]">
        <span className="font-medium text-muted-foreground">{label}</span>
        <button
          type="button"
          onClick={handleCopy}
          className="flex items-center gap-1 rounded px-2 py-0.5 hover:bg-accent transition-colors"
        >
          {copied ? (
            <Check className="h-3 w-3 text-emerald-500" />
          ) : (
            <Copy className="h-3 w-3" />
          )}
          {copied
            ? t(($) => $.webhook_payload.copied_short)
            : t(($) => $.webhook_payload.copy)}
        </button>
      </div>
      {/* whitespace-pre-wrap keeps pretty-printed indentation but lets
          long lines wrap; break-all is the only thing that breaks mid-token
          (necessary for minified JSON, which has no whitespace to break at). */}
      <pre className="max-h-48 overflow-auto bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed whitespace-pre-wrap break-all">
        {display}
        {isTruncated && (
          <span className="block pt-2 text-muted-foreground/70">
            {t(($) => $.webhook_payload.truncated_marker)}
          </span>
        )}
      </pre>
    </div>
  );
}

function ReplayHint({ delivery }: { delivery: WebhookDelivery }) {
  const { t } = useT("autopilots");
  if (delivery.signature_status === "invalid") {
    return (
      <span className="text-xs text-muted-foreground">
        {t(($) => $.deliveries.replay.disabled_invalid_signature)}
      </span>
    );
  }
  if (delivery.status === "rejected") {
    return (
      <span className="text-xs text-muted-foreground">
        {t(($) => $.deliveries.replay.disabled_rejected)}
      </span>
    );
  }
  if (delivery.status === "queued") {
    return (
      <span className="text-xs text-muted-foreground">
        {t(($) => $.deliveries.replay.disabled_queued)}
      </span>
    );
  }
  return null;
}

function ReplayButton({
  autopilotId,
  delivery,
  onSuccess,
}: {
  autopilotId: string;
  delivery: WebhookDelivery;
  onSuccess: () => void;
}) {
  const { t } = useT("autopilots");
  const replay = useReplayAutopilotDelivery();
  const enabled = canReplay(delivery) && !replay.isPending;

  const handleClick = async () => {
    try {
      await replay.mutateAsync({ autopilotId, deliveryId: delivery.id });
      toast.success(t(($) => $.deliveries.replay.toast_success));
      onSuccess();
    } catch (e: unknown) {
      const message =
        e instanceof Error
          ? e.message
          : t(($) => $.deliveries.replay.toast_failed);
      toast.error(message);
    }
  };

  return (
    <Button
      size="sm"
      variant="outline"
      onClick={handleClick}
      disabled={!enabled}
    >
      <RotateCw
        className={cn(
          "h-3.5 w-3.5 mr-1",
          replay.isPending && "animate-spin",
        )}
      />
      {replay.isPending
        ? t(($) => $.deliveries.replay.in_progress)
        : t(($) => $.deliveries.replay.action)}
    </Button>
  );
}
