"use client";

import { useEffect, useState } from "react";
import { RotateCw, Send } from "@multica/ui/icons";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  messageDeliveriesInfiniteOptions,
  messageDeliveryKeys,
  messageDeliveryOptions,
  useRetryMessageDelivery,
} from "@multica/core/message-delivery";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  autopilotDetailOptions,
  autopilotRunOptions,
} from "@multica/core/autopilots/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { TranscriptButton } from "../../common/task-transcript";
import type { AgentTask } from "@multica/core/types/agent";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { ApiError, errorCode } from "@multica/core/api";
import type {
  GetMessageDeliveryResponse,
  MessageDelivery,
  MessageDeliveryReceipt,
} from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Alert, AlertDescription } from "@multica/ui/components/ui/alert";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import { useLocale, useT } from "../../i18n";
import { AppLink } from "../../navigation";
import { deliveryStatusVisual } from "../status-visual";
import {
  canRetryMessageDelivery,
  messageDeliveryErrorCodeKey,
  messageDeliveryErrorKey,
  messageDeliverySourceKindKey,
  messageDeliveryStatusKey,
} from "../copy";

// --- Status visuals -------------------------------------------------------


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

const FILTER_STATUSES = [
  "queued",
  "sending",
  "sent",
  "failed",
  "uncertain",
  "cancelled",
  "suppressed",
] as const;

// --- Section --------------------------------------------------------------

/**
 * Delivery records for this automation's result push: every status from
 * queued to suppressed, with the frozen report content, receipts and a
 * permission-gated retry entry. Same autopilot write gate as the routes
 * section — read-only callers see the restricted state (real 403), servers
 * without OL-25 see the unsupported state (404).
 */
export function MessageDeliveriesSection({
  autopilotId,
  canWrite,
}: {
  autopilotId: string;
  canWrite: boolean;
}) {
  const { t } = useT("message-delivery");
  const wsId = useWorkspaceId();
  const [statusFilter, setStatusFilter] = useState<string>("all");

  // Fixed legal page size + offset pages (R4): the server caps limit at 200,
  // so growing the limit past it would silently bounce back to page one.
  const deliveriesQuery = useInfiniteQuery(
    messageDeliveriesInfiniteOptions(wsId, autopilotId, {
      status: statusFilter === "all" ? undefined : statusFilter,
    }),
  );

  const header = (
    <div className="flex items-center justify-between gap-3">
      <h2 className="text-body font-medium text-muted-foreground uppercase tracking-wider">
        {t(($) => $.deliveries.title)}
      </h2>
      <Select
        items={[
          { value: "all", label: t(($) => $.deliveries.filter_all) },
          ...FILTER_STATUSES.map((s) => ({
            value: s,
            label: t(($) => $.deliveries.status[s]),
          })),
        ]}
        value={statusFilter}
        onValueChange={(v) => setStatusFilter(v as string)}
      >
        <SelectTrigger size="sm" aria-label={t(($) => $.deliveries.title)}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">{t(($) => $.deliveries.filter_all)}</SelectItem>
          {FILTER_STATUSES.map((s) => (
            <SelectItem key={s} value={s}>
              {t(($) => $.deliveries.status[s])}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );

  if (deliveriesQuery.isError) {
    const err = deliveriesQuery.error;
    const status = err instanceof ApiError ? err.status : 0;
    // 403/404 states are already explained by the configuration section
    // above; this section stays quiet there to avoid double banners.
    if (status === 403 || status === 404) return null;
    return (
      <section className="space-y-3">
        {header}
        <Alert>
          <AlertDescription>{t(($) => $.deliveries.load_failed)}</AlertDescription>
        </Alert>
      </section>
    );
  }

  const deliveries = deliveriesQuery.data?.pages.flatMap((page) => page.deliveries) ?? [];
  const mayHaveMore = deliveriesQuery.hasNextPage === true;

  return (
    <section className="space-y-3">
      {header}
      <p className="text-caption text-muted-foreground">
        {t(($) => $.deliveries.description)}
      </p>
      {deliveriesQuery.isLoading ? (
        <div className="space-y-1">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      ) : deliveries.length === 0 ? (
        <div className="rounded-md border border-dashed p-4 text-center text-body text-muted-foreground">
          {t(($) => $.deliveries.empty)}
        </div>
      ) : (
        <>
          <div className="rounded-md border overflow-hidden">
            {deliveries.map((delivery) => (
              <DeliveryRow
                key={delivery.id}
                delivery={delivery}
                autopilotId={autopilotId}
                canWrite={canWrite}
              />
            ))}
          </div>
          {mayHaveMore && (
            <div className="flex justify-center">
              <Button
                size="sm"
                variant="ghost"
                onClick={() => deliveriesQuery.fetchNextPage()}
                disabled={deliveriesQuery.isFetchingNextPage}
              >
                {t(($) => $.deliveries.load_more)}
              </Button>
            </div>
          )}
        </>
      )}
    </section>
  );
}

// --- Row ------------------------------------------------------------------

function DeliveryRow({
  delivery,
  autopilotId,
  canWrite,
}: {
  delivery: MessageDelivery;
  autopilotId: string;
  canWrite: boolean;
}) {
  const { t } = useT("message-delivery");
  const locale = useLocale();
  const [open, setOpen] = useState(false);

  const visual = deliveryStatusVisual(delivery.status);
  const StatusIcon = visual.icon;

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-body hover:bg-accent/30 transition-colors"
      >
        <StatusIcon
          className={cn("h-4 w-4 shrink-0", visual.color, visual.spin && "animate-spin")}
        />
        <span className="w-24 shrink-0 text-caption font-medium text-foreground">
          {t(($) => $.deliveries.status[messageDeliveryStatusKey(delivery.status)])}
        </span>
        <span className="w-28 shrink-0 text-caption text-muted-foreground truncate">
          {t(($) => $.deliveries.source_kind[messageDeliverySourceKindKey(delivery.source_kind)])}
        </span>
        <span className="flex-1 min-w-0 text-caption text-muted-foreground truncate font-mono">
          {delivery.target_key}
        </span>
        {delivery.shard_total > 1 && (
          <Badge variant="outline" className="shrink-0">
            {t(($) => $.deliveries.row.shards, {
              done: delivery.status === "sent" ? delivery.shard_total : 0,
              total: delivery.shard_total,
            })}
          </Badge>
        )}
        {delivery.attempts > 1 && (
          <Badge variant="outline" className="shrink-0">
            {t(($) => $.deliveries.row.attempts, { count: delivery.attempts })}
          </Badge>
        )}
        <span className="w-32 shrink-0 text-right text-caption text-muted-foreground tabular-nums">
          {formatDate(delivery.delivered_at ?? delivery.created_at, locale)}
        </span>
      </button>
      {open && (
        <DeliveryDetailDialog
          open={open}
          onOpenChange={setOpen}
          autopilotId={autopilotId}
          delivery={delivery}
          canWrite={canWrite}
        />
      )}
    </>
  );
}

// --- Detail dialog --------------------------------------------------------

export function DeliveryDetailDialog({
  open,
  onOpenChange,
  autopilotId,
  delivery,
  canWrite,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  autopilotId: string;
  delivery: MessageDelivery;
  canWrite: boolean;
}) {
  const { t } = useT("message-delivery");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const retry = useRetryMessageDelivery();
  const [confirmUncertain, setConfirmUncertain] = useState(false);

  const { data: detail, isLoading, isError, error } = useQuery(
    messageDeliveryOptions(wsId, autopilotId, delivery.id, { enabled: open }),
  );
  // A failed detail read (403/500/…) is NOT an empty report: show the error
  // explicitly and keep retry unavailable until the real state is known.
  const detailError = isError ? error : null;

  // R1: the list row prop refreshes via the section's polling/invalidation.
  // When it observed a newer write than the cached detail, drop the stale
  // detail so the dialog re-reads the truth (status, receipts, retry gate).
  const qc = useQueryClient();
  useEffect(() => {
    if (
      detail &&
      delivery.updated_at &&
      detail.delivery.updated_at !== delivery.updated_at
    ) {
      qc.invalidateQueries({
        queryKey: messageDeliveryKeys.delivery(wsId, autopilotId, delivery.id),
      });
    }
  }, [qc, wsId, autopilotId, delivery.id, delivery.updated_at, detail]);
  // Member targets show the member's name rather than a raw user uuid.
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  // Slim row until the detail lands; snapshots/receipts skeleton meanwhile.
  const full = detail?.delivery ?? delivery;
  const visual = deliveryStatusVisual(full.status);
  const StatusIcon = visual.icon;
  const content = detail?.content_snapshot ?? null;
  const target = detail?.target_snapshot ?? null;
  const sourceRef = detail?.source_ref ?? null;
  const receipts = detail?.receipts ?? [];

  const retryable = canWrite && !detailError && canRetryMessageDelivery(full.status);

  const handleRetry = () => {
    retry.mutate(
      { autopilotId, deliveryId: full.id },
      {
        onSuccess: () => {
          toast.success(t(($) => $.deliveries.retry.toast));
          setConfirmUncertain(false);
        },
        onError: (e) => {
          toast.error(t(($) => $.error[messageDeliveryErrorKey(errorCode(e))]));
        },
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogTitle className="flex items-center gap-2">
          <Send className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.deliveries.detail.title)}
        </DialogTitle>
        <div className="space-y-4 pt-1">
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex items-center gap-2">
              <StatusIcon
                className={cn("h-4 w-4 shrink-0", visual.color, visual.spin && "animate-spin")}
              />
              <span className="text-body font-medium text-foreground">
                {t(($) => $.deliveries.status[messageDeliveryStatusKey(full.status)])}
              </span>
            </div>
            <Badge variant="outline">
              {t(($) => $.deliveries.source_kind[messageDeliverySourceKindKey(full.source_kind)])}
            </Badge>
            {sourceRef?.issue_identifier && (
              <AppLink
                href={wsPaths.issueDetail(sourceRef.issue_identifier)}
                className="text-caption font-medium text-foreground underline underline-offset-2"
              >
                {t(($) => $.deliveries.detail.view_issue)} {sourceRef.issue_identifier}
              </AppLink>
            )}
          </div>

          {full.status === "sent" && (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.deliveries.description)}
            </p>
          )}

          {detailError && (
            <Alert variant="destructive">
              <AlertDescription>
                {t(($) => $.error[messageDeliveryErrorKey(errorCode(detailError))])}
              </AlertDescription>
            </Alert>
          )}

          {/* Report content (frozen at decision time) */}
          {!detailError && (
          <div className="min-w-0 rounded-md border bg-background">
            <div className="border-b px-3 py-1.5 text-micro font-medium text-muted-foreground">
              {t(($) => $.deliveries.detail.report)}
            </div>
            {isLoading && !detail ? (
              <div className="space-y-2 p-3">
                <Skeleton className="h-4 w-2/3" />
                <Skeleton className="h-16 w-full" />
              </div>
            ) : content ? (
              <div className="space-y-2 p-3">
                {content.summary && (
                  <p className="text-body">{content.summary}</p>
                )}
                {content.text ? (
                  <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded-sm bg-muted/40 px-3 py-2 text-caption font-mono leading-relaxed">
                    {content.text}
                  </pre>
                ) : (
                  <p className="text-caption text-muted-foreground">
                    {t(($) => $.deliveries.detail.no_report_body)}
                  </p>
                )}
              </div>
            ) : (
              <p className="p-3 text-caption text-muted-foreground">
                {t(($) => $.deliveries.detail.no_report_body)}
              </p>
            )}
          </div>
          )}

          {/* Target + meta */}
          <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-caption">
            <MetaRow
              label={t(($) => $.deliveries.detail.target)}
              value={targetLabel(target, full.target_key, members)}
              mono={target?.target_type !== "member"}
            />
            <MetaRow
              label={t(($) => $.deliveries.detail.created_at)}
              value={formatDate(full.created_at, locale)}
            />
            <MetaRow
              label={t(($) => $.deliveries.detail.first_attempt_at)}
              value={formatDate(full.first_attempt_at, locale)}
            />
            <MetaRow
              label={t(($) => $.deliveries.detail.delivered_at)}
              value={formatDate(full.delivered_at, locale)}
            />
            <MetaRow
              label={t(($) => $.deliveries.detail.next_attempt_at)}
              value={formatDate(full.next_attempt_at, locale)}
            />
            <MetaRow
              label={t(($) => $.deliveries.detail.attempts)}
              value={String(full.attempts)}
            />
            <MetaRow
              label={t(($) => $.deliveries.detail.route_revision)}
              value={`v${full.route_revision}`}
            />
          </dl>

          {/* R3 reverse linkage: the FULL run id (an 8-char prefix can
              collide across runs) plus the run's own log entry, loaded via
              the existing autopilotRunOptions contract. */}
          {full.run_id && (
            <SourceRunRow
              wsId={wsId}
              autopilotId={autopilotId}
              runId={full.run_id}
            />
          )}

          {full.error_code && (
            <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-caption text-destructive">
              <div className="font-medium">
                {t(($) => $.error_code[messageDeliveryErrorCodeKey(full.error_code)])}
              </div>
              {full.last_error && (
                <div className="mt-0.5 font-mono break-all">{full.last_error}</div>
              )}
            </div>
          )}

          {/* Receipt ledger */}
          {detailError ? null : isLoading && !detail ? (
            <Skeleton className="h-16 w-full" />
          ) : receipts.length > 0 ? (
            <div className="space-y-1.5">
              <div className="text-micro font-medium text-muted-foreground">
                {t(($) => $.deliveries.detail.receipts)}
              </div>
              <div className="rounded-md border divide-y">
                {receipts.map((receipt) => (
                  <ReceiptRow key={receipt.id} receipt={receipt} />
                ))}
              </div>
            </div>
          ) : null}

          {/* Retry — gated on write permission and a retryable status */}
          <div className="flex items-center justify-between pt-2">
            {!retryable ? (
              <span className="text-caption text-muted-foreground">
                {detailError
                  ? t(($) => $.deliveries.detail.load_failed)
                  : !canWrite && canRetryMessageDelivery(full.status)
                    ? t(($) => $.section.read_only)
                    : t(($) => $.deliveries.retry.disabled)}
              </span>
            ) : (
              <span />
            )}
            <Button
              size="sm"
              variant="outline"
              disabled={!retryable || retry.isPending}
              onClick={() => {
                if (full.status === "uncertain") setConfirmUncertain(true);
                else handleRetry();
              }}
            >
              <RotateCw className={cn("h-3.5 w-3.5 mr-1", retry.isPending && "animate-spin")} />
              {retry.isPending
                ? t(($) => $.deliveries.retry.in_progress)
                : t(($) => $.deliveries.retry.action)}
            </Button>
          </div>
        </div>

        {/* Uncertain sends may already have reached Feishu — the platform
            response was lost. The contract requires an explicit verify
            before retry; the retry replays the fixed per-shard UUIDs. */}
        <AlertDialog open={confirmUncertain} onOpenChange={setConfirmUncertain}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t(($) => $.deliveries.retry.uncertain_title)}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t(($) => $.deliveries.retry.uncertain_description)}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={retry.isPending}>
                {t(($) => $.deliveries.retry.cancel)}
              </AlertDialogCancel>
              <AlertDialogAction onClick={handleRetry} disabled={retry.isPending}>
                {retry.isPending
                  ? t(($) => $.deliveries.retry.in_progress)
                  : t(($) => $.deliveries.retry.uncertain_confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </DialogContent>
    </Dialog>
  );
}

function targetLabel(
  target: GetMessageDeliveryResponse["target_snapshot"],
  fallbackKey: string,
  members: { user_id: string; name: string; email: string }[],
): string {
  if (!target) return fallbackKey || "—";
  switch (target.target_type) {
    case "member": {
      const member = members.find((m) => m.user_id === target.user_id);
      return member ? member.name || member.email : (target.user_id ?? fallbackKey);
    }
    case "group":
      return target.chat_id ?? fallbackKey;
    case "topic":
      return `${target.chat_id ?? "—"} ↳ ${target.message_id ?? "—"}`;
    default:
      return fallbackKey || "—";
  }
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
      <dd className={cn("truncate text-foreground", mono && "font-mono")} title={value}>
        {value}
      </dd>
    </div>
  );
}

function ReceiptRow({ receipt }: { receipt: MessageDeliveryReceipt }) {
  const { t } = useT("message-delivery");
  const locale = useLocale();
  return (
    <div className="flex items-center gap-3 px-3 py-2 text-caption">
      <span className="shrink-0 text-muted-foreground tabular-nums">
        {t(($) => $.deliveries.detail.receipt_shard, {
          index: receipt.shard_index + 1,
          total: receipt.shard_total,
        })}
      </span>
      <span className="flex-1 min-w-0 truncate font-mono">
        {receipt.external_message_id ?? "—"}
      </span>
      <span
        className="shrink-0 text-muted-foreground font-mono"
        title={`${t(($) => $.deliveries.detail.send_uuid)}: ${receipt.send_uuid}`}
      >
        {receipt.send_uuid.slice(0, 8)}
      </span>
      <span className="shrink-0 text-muted-foreground tabular-nums">
        {formatDate(receipt.created_at, locale)}
      </span>
    </div>
  );
}

/**
 * Reverse linkage for a delivery: locate its exact source run and offer the
 * run's own entries (transcript log via the shared TranscriptButton, issue
 * link when the run created one). No new pages, no new endpoints.
 */
function SourceRunRow({
  wsId,
  autopilotId,
  runId,
}: {
  wsId: string;
  autopilotId: string;
  runId: string;
}) {
  const { t } = useT("message-delivery");
  const wsPaths = useWorkspacePaths();
  const { getActorName } = useActorName();
  const { data: run, isLoading } = useQuery(
    autopilotRunOptions(wsId, autopilotId, runId, { enabled: true }),
  );
  const { data: autopilotData } = useQuery(autopilotDetailOptions(wsId, autopilotId));
  const autopilot = autopilotData?.autopilot;

  const agentName = autopilot
    ? getActorName(autopilot.assignee_type, autopilot.assignee_id)
    : "";
  // Same synthetic AgentTask mapping the run history rows use.
  const syntheticTask: AgentTask | null =
    run && run.task_id && autopilot
      ? {
          id: run.task_id,
          agent_id: autopilot.assignee_id,
          runtime_id: "",
          issue_id: "",
          status:
            run.status === "running"
              ? "running"
              : run.status === "completed"
                ? "completed"
                : run.status === "failed"
                  ? "failed"
                  : "queued",
          priority: 0,
          dispatched_at: null,
          started_at: run.triggered_at || null,
          completed_at: run.completed_at || null,
          result: null,
          error: run.failure_reason || null,
          created_at: run.created_at,
        }
      : null;

  return (
    <div className="flex flex-wrap items-center gap-2 text-caption">
      <span className="text-muted-foreground">
        {t(($) => $.deliveries.detail.run)}
      </span>
      <code className="rounded-sm bg-muted px-1.5 py-0.5 font-mono text-micro break-all">
        {runId}
      </code>
      {isLoading && <Skeleton className="h-4 w-16" />}
      {syntheticTask && (
        <TranscriptButton
          task={syntheticTask}
          agentName={agentName}
          isLive={run?.status === "running"}
          title={t(($) => $.deliveries.detail.view_log)}
        />
      )}
      {run?.issue_id && (
        <AppLink
          href={wsPaths.issueDetail(run.issue_id)}
          className="font-medium text-foreground underline underline-offset-2"
        >
          {t(($) => $.deliveries.detail.view_issue)}
        </AppLink>
      )}
    </div>
  );
}
