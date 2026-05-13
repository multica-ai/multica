"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, FileClock } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { grantAuditOptions, type GrantAuditEntry } from "../../core";

interface AuditTabProps {
  wsId: string;
}

const PAGE_SIZE = 50;

export function AuditTab({ wsId }: AuditTabProps) {
  const [subjectId, setSubjectId] = useState("");
  const [grantId, setGrantId] = useState("");
  const [offset, setOffset] = useState(0);

  const filter = {
    subjectId: subjectId.trim() || null,
    grantId: grantId.trim() || null,
    since: null,
    limit: PAGE_SIZE,
    offset,
  };

  const list = useQuery(grantAuditOptions(wsId, filter));

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b bg-background/50 px-4 py-3">
        <Input
          value={subjectId}
          onChange={(e) => {
            setSubjectId(e.target.value);
            setOffset(0);
          }}
          placeholder="Subjekt-ID"
          className="h-8 w-56 text-sm"
          aria-label="Filtrér på subjekt"
        />
        <Input
          value={grantId}
          onChange={(e) => {
            setGrantId(e.target.value);
            setOffset(0);
          }}
          placeholder="Grant-ID"
          className="h-8 w-56 text-sm"
          aria-label="Filtrér på grant"
        />
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            setSubjectId("");
            setGrantId("");
            setOffset(0);
          }}
          className="text-muted-foreground"
        >
          Nulstil
        </Button>
      </div>

      <div className="flex-1 overflow-y-auto">
        <AuditList list={list.data?.items ?? []} isLoading={list.isLoading} error={list.error} />
      </div>

      <div className="flex items-center justify-between border-t bg-background/50 px-4 py-2 text-xs text-muted-foreground">
        <span className="font-mono tabular-nums">
          {(list.data?.items.length ?? 0) === 0
            ? "0 hændelser"
            : `${offset + 1}–${offset + (list.data?.items.length ?? 0)} af ${
                list.data?.total ?? 0
              }`}
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
            disabled={
              (list.data?.items.length ?? 0) === 0 ||
              offset + (list.data?.items.length ?? 0) >=
                (list.data?.total ?? 0)
            }
            onClick={() => setOffset(offset + PAGE_SIZE)}
          >
            Næste
          </Button>
        </div>
      </div>
    </div>
  );
}

interface AuditListProps {
  list: GrantAuditEntry[];
  isLoading: boolean;
  error: unknown;
}

function AuditList({ list, isLoading, error }: AuditListProps) {
  if (error) {
    return (
      <div className="m-4 flex items-start gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
        <AlertCircle className="size-4 mt-0.5 shrink-0" />
        <div className="min-w-0">
          <div className="font-medium">Kunne ikke hente audit-log</div>
          <div className="text-xs text-destructive/80 truncate">
            {error instanceof Error ? error.message : "ukendt fejl"}
          </div>
        </div>
      </div>
    );
  }

  if (isLoading && list.length === 0) {
    return (
      <div className="space-y-1 p-4">
        {[0, 1, 2, 3, 4].map((i) => (
          <div key={i} className="h-10 animate-pulse rounded bg-muted/60" />
        ))}
      </div>
    );
  }

  if (list.length === 0) {
    return (
      <div className="m-4 rounded-md border border-dashed p-12 text-center">
        <FileClock className="mx-auto size-8 text-muted-foreground/60" />
        <p className="mt-3 text-sm font-medium">Ingen audit-hændelser</p>
        <p className="mt-1 text-xs text-muted-foreground">
          Hver gang nogen opretter, ændrer eller sletter et grant — uanset
          om de bruger UI, API, CLI eller MCP — lander det her.
        </p>
      </div>
    );
  }

  return (
    <ul className="divide-y">
      {list.map((entry) => (
        <li key={entry.id} className="px-4 py-3 text-sm">
          <div className="flex items-start gap-3">
            <ActionBadge action={entry.action} />
            <div className="min-w-0 flex-1">
              <div className="font-medium">
                {entry.actor_name ?? entry.actor_id ?? "Ukendt aktør"}
                <span className="ml-1.5 font-normal text-muted-foreground">
                  via {entry.via ?? "?"}
                </span>
              </div>
              {entry.summary && (
                <div className="text-xs text-muted-foreground">
                  {entry.summary}
                </div>
              )}
              {entry.grant_id && (
                <div className="mt-0.5 font-mono text-[10px] text-muted-foreground/70">
                  grant: {entry.grant_id}
                </div>
              )}
            </div>
            <time
              className="shrink-0 font-mono text-[10px] tabular-nums text-muted-foreground"
              dateTime={entry.created_at}
            >
              {formatDateTime(entry.created_at)}
            </time>
          </div>
        </li>
      ))}
    </ul>
  );
}

function ActionBadge({ action }: { action: string }) {
  const tone =
    action === "create"
      ? "bg-success/10 text-success border-success/30"
      : action === "delete" || action === "revoke"
        ? "bg-destructive/10 text-destructive border-destructive/40"
        : action === "approve"
          ? "bg-brand/10 text-brand border-brand/40"
          : "bg-muted text-muted-foreground border-border";
  return (
    <Badge variant="outline" className={cn("text-[10px] shrink-0", tone)}>
      {action}
    </Badge>
  );
}

function formatDateTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString();
  } catch {
    return iso;
  }
}
