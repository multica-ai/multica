"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Check, Inbox, X } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  approveAsk,
  pendingAsksOptions,
  permissionsKeys,
  rejectAsk,
  type PendingAsk,
} from "../../core";

interface PendingAsksTabProps {
  wsId: string;
}

const PAGE_SIZE = 50;

export function PendingAsksTab({ wsId }: PendingAsksTabProps) {
  const [offset, setOffset] = useState(0);
  const list = useQuery(pendingAsksOptions(wsId, { limit: PAGE_SIZE, offset }));
  const items = list.data?.items ?? [];
  const total = list.data?.total ?? 0;

  return (
    <div className="flex h-full flex-col">
      <div className="flex-1 overflow-y-auto">
        <AskList items={items} isLoading={list.isLoading} error={list.error} wsId={wsId} />
      </div>

      <div className="flex items-center justify-between border-t bg-background/50 px-4 py-2 text-xs text-muted-foreground">
        <span className="font-mono tabular-nums">
          {total === 0
            ? "0 afventer"
            : `${offset + 1}–${offset + items.length} af ${total}`}
        </span>
        <div className="flex gap-1">
          <Button
            variant="outline"
            size="sm"
            disabled={offset === 0}
            onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
          >
            Forrige
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={items.length === 0 || offset + items.length >= total}
            onClick={() => setOffset(offset + PAGE_SIZE)}
          >
            Næste
          </Button>
        </div>
      </div>
    </div>
  );
}

function AskList({
  items,
  isLoading,
  error,
  wsId,
}: {
  items: PendingAsk[];
  isLoading: boolean;
  error: unknown;
  wsId: string;
}) {
  if (error) {
    return (
      <div className="m-4 flex items-start gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
        <AlertCircle className="mt-0.5 size-4 shrink-0" />
        <div className="min-w-0">
          <div className="font-medium">Kunne ikke hente pending asks</div>
          <div className="text-xs text-destructive/80 truncate">
            {error instanceof Error ? error.message : "ukendt fejl"}
          </div>
        </div>
      </div>
    );
  }

  if (isLoading && items.length === 0) {
    return (
      <div className="space-y-1 p-4">
        {[0, 1, 2].map((i) => (
          <div key={i} className="h-16 animate-pulse rounded bg-muted/60" />
        ))}
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div className="m-4 rounded-md border border-dashed p-12 text-center">
        <Inbox className="mx-auto size-8 text-muted-foreground/60" />
        <p className="mt-3 text-sm font-medium">Ingen afventende godkendelser</p>
        <p className="mt-1 text-xs text-muted-foreground">
          Approval-asks fra fase 3 dukker op her, når en agent eller et runtime beder om adgang.
        </p>
      </div>
    );
  }

  return (
    <ul className="divide-y">
      {items.map((ask) => (
        <AskRow key={ask.id} ask={ask} wsId={wsId} />
      ))}
    </ul>
  );
}

function AskRow({ ask, wsId }: { ask: PendingAsk; wsId: string }) {
  const qc = useQueryClient();

  const invalidate = () =>
    qc.invalidateQueries({ queryKey: permissionsKeys.all(wsId) });

  const approveMut = useMutation({
    mutationFn: () => approveAsk(wsId, ask.id),
    onSuccess: invalidate,
  });
  const rejectMut = useMutation({
    mutationFn: () => rejectAsk(wsId, ask.id),
    onSuccess: invalidate,
  });

  const isPending = ask.status === "pending";

  return (
    <li className="px-4 py-3 text-sm">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="rounded-sm bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
              {ask.subject.type}
            </span>
            <span className="font-medium">
              {ask.subject.display_name ?? ask.subject.id ?? "Ukendt"}
            </span>
            <span className="text-muted-foreground">vil bruge</span>
            <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">
              {ask.capability}
            </code>
            <span className="text-muted-foreground">på</span>
            <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs text-muted-foreground">
              {ask.resource.pattern}
            </code>
          </div>
          {ask.reason && (
            <p className="mt-1 text-xs text-muted-foreground line-clamp-2">{ask.reason}</p>
          )}
          <div className="mt-1 flex items-center gap-3 text-[11px] text-muted-foreground">
            <time dateTime={ask.requested_at}>{formatDateTime(ask.requested_at)}</time>
            {ask.expires_at && (
              <span>udløber {formatDateTime(ask.expires_at)}</span>
            )}
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-1">
          {isPending ? (
            <>
              <Button
                variant="outline"
                size="sm"
                className={cn("h-7 gap-1 text-xs text-success border-success/40 hover:bg-success/10")}
                onClick={() => approveMut.mutate()}
                disabled={approveMut.isPending || rejectMut.isPending}
                aria-label="Godkend"
              >
                <Check className="size-3" />
                Godkend
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="h-7 gap-1 text-xs text-destructive border-destructive/40 hover:bg-destructive/10"
                onClick={() => rejectMut.mutate()}
                disabled={approveMut.isPending || rejectMut.isPending}
                aria-label="Afvis"
              >
                <X className="size-3" />
                Afvis
              </Button>
            </>
          ) : (
            <AskStatusBadge status={ask.status} />
          )}
        </div>
      </div>
    </li>
  );
}

function AskStatusBadge({ status }: { status: string }) {
  const tone =
    status === "approved"
      ? "bg-success/10 text-success border-success/30"
      : status === "rejected"
        ? "bg-destructive/10 text-destructive border-destructive/40"
        : "bg-muted text-muted-foreground border-border";
  const label =
    status === "approved"
      ? "godkendt"
      : status === "rejected"
        ? "afvist"
        : status === "delegated"
          ? "delegeret"
          : status;
  return (
    <Badge variant="outline" className={cn("text-[10px]", tone)}>
      {label}
    </Badge>
  );
}

function formatDateTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString("da-DK", {
      dateStyle: "short",
      timeStyle: "short",
    });
  } catch {
    return iso;
  }
}
