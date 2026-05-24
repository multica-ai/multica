"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, FileClock, X } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import {
  grantAuditOptions,
  type AuditFilter,
  type GrantAuditEntry,
} from "../../core";

interface AuditTabProps {
  wsId: string;
}

const PAGE_SIZE = 50;

export function AuditTab({ wsId }: AuditTabProps) {
  const [subjectId, setSubjectId] = useState("");
  const [capability, setCapability] = useState("");
  const [since, setSince] = useState("");
  const [until, setUntil] = useState("");
  const [offset, setOffset] = useState(0);
  const [selectedEntry, setSelectedEntry] = useState<GrantAuditEntry | null>(null);

  const filter: AuditFilter = {
    subjectId: subjectId.trim() || null,
    grantId: null,
    capability: capability.trim() || null,
    since: since || null,
    until: until || null,
    limit: PAGE_SIZE,
    offset,
  };

  const list = useQuery(grantAuditOptions(wsId, filter));

  const hasFilter =
    !!subjectId.trim() ||
    !!capability.trim() ||
    !!since ||
    !!until;

  function resetFilters() {
    setSubjectId("");
    setCapability("");
    setSince("");
    setUntil("");
    setOffset(0);
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b bg-background/50 px-4 py-3">
        <Input
          value={subjectId}
          onChange={(e) => { setSubjectId(e.target.value); setOffset(0); }}
          placeholder="Subjekt-ID"
          className="h-8 w-52 text-sm"
          aria-label="Filtrér på subjekt"
        />
        <Input
          value={capability}
          onChange={(e) => { setCapability(e.target.value); setOffset(0); }}
          placeholder="Kapabilitet (fx issue.read)"
          className="h-8 w-52 text-sm"
          aria-label="Filtrér på kapabilitet"
        />
        <div className="flex items-center gap-1 text-xs text-muted-foreground">
          <span>Fra</span>
          <input
            type="datetime-local"
            value={since}
            onChange={(e) => { setSince(e.target.value); setOffset(0); }}
            className="h-8 rounded-md border bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
            aria-label="Fra dato"
          />
        </div>
        <div className="flex items-center gap-1 text-xs text-muted-foreground">
          <span>Til</span>
          <input
            type="datetime-local"
            value={until}
            onChange={(e) => { setUntil(e.target.value); setOffset(0); }}
            className="h-8 rounded-md border bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
            aria-label="Til dato"
          />
        </div>
        {hasFilter && (
          <Button
            variant="ghost"
            size="sm"
            onClick={resetFilters}
            className="gap-1 text-muted-foreground"
          >
            <X className="size-3" />
            Nulstil
          </Button>
        )}
      </div>

      <div className="flex flex-1 min-h-0">
        <div className={cn("flex-1 overflow-y-auto", selectedEntry && "md:border-r")}>
          <AuditList
            list={list.data?.items ?? []}
            isLoading={list.isLoading}
            error={list.error}
            selectedId={selectedEntry?.id ?? null}
            onSelect={setSelectedEntry}
          />
        </div>

        {selectedEntry && (
          <AuditDetailPanel
            entry={selectedEntry}
            onClose={() => setSelectedEntry(null)}
          />
        )}
      </div>

      <div className="flex items-center justify-between border-t bg-background/50 px-4 py-2 text-xs text-muted-foreground">
        <span className="font-mono tabular-nums">
          {(list.data?.items.length ?? 0) === 0
            ? "0 hændelser"
            : `${offset + 1}–${offset + (list.data?.items.length ?? 0)} af ${list.data?.total ?? 0}`}
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
              offset + (list.data?.items.length ?? 0) >= (list.data?.total ?? 0)
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
  selectedId: string | null;
  onSelect: (entry: GrantAuditEntry) => void;
}

function AuditList({ list, isLoading, error, selectedId, onSelect }: AuditListProps) {
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
          Ændringer til grants logges her — via UI, API, CLI eller MCP.
        </p>
      </div>
    );
  }

  return (
    <ul className="divide-y">
      {list.map((entry) => (
        <li key={entry.id}>
          <button
            type="button"
            onClick={() => onSelect(entry)}
            className={cn(
              "w-full px-4 py-3 text-left text-sm transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:bg-muted/40",
              selectedId === entry.id && "bg-muted/60",
            )}
            aria-pressed={selectedId === entry.id}
          >
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
                  <div className="text-xs text-muted-foreground">{entry.summary}</div>
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
          </button>
        </li>
      ))}
    </ul>
  );
}

function AuditDetailPanel({
  entry,
  onClose,
}: {
  entry: GrantAuditEntry;
  onClose: () => void;
}) {
  return (
    <div className="w-80 shrink-0 overflow-y-auto border-l bg-background">
      <div className="flex items-center justify-between border-b px-4 py-3">
        <span className="text-sm font-medium">Detaljer</span>
        <Button variant="ghost" size="sm" onClick={onClose} aria-label="Luk detaljer">
          <X className="size-4" />
        </Button>
      </div>
      <div className="space-y-4 p-4 text-sm">
        <DetailRow label="Handling">
          <ActionBadge action={entry.action} />
        </DetailRow>
        <DetailRow label="Aktør">
          <span className="font-medium">{entry.actor_name ?? entry.actor_id ?? "—"}</span>
          {entry.actor_type && (
            <span className="ml-1.5 text-xs text-muted-foreground">({entry.actor_type})</span>
          )}
        </DetailRow>
        <DetailRow label="Surface">
          <code className="rounded bg-muted px-1.5 py-0.5 text-xs">{entry.via ?? "—"}</code>
        </DetailRow>
        {entry.grant_id && (
          <DetailRow label="Grant ID">
            <code className="break-all font-mono text-[11px] text-muted-foreground">
              {entry.grant_id}
            </code>
          </DetailRow>
        )}
        <DetailRow label="Tidspunkt">
          <time dateTime={entry.created_at} className="font-mono text-xs">
            {new Date(entry.created_at).toLocaleString("da-DK")}
          </time>
        </DetailRow>
        {entry.summary && (
          <DetailRow label="Opsummering">
            <span className="text-muted-foreground">{entry.summary}</span>
          </DetailRow>
        )}

        {entry.before !== null && entry.before !== undefined && (
          <DiffSection label="Før" value={entry.before} />
        )}
        {entry.after !== null && entry.after !== undefined && (
          <DiffSection label="Efter" value={entry.after} />
        )}
      </div>
    </div>
  );
}

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-[10px] uppercase tracking-wide text-muted-foreground">{label}</span>
      <div className="flex flex-wrap items-center gap-1">{children}</div>
    </div>
  );
}

function DiffSection({ label, value }: { label: string; value: unknown }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-[10px] uppercase tracking-wide text-muted-foreground">{label}</span>
      <pre className="overflow-auto rounded border bg-muted/40 p-2 text-[11px] font-mono leading-relaxed">
        {JSON.stringify(value, null, 2)}
      </pre>
    </div>
  );
}

function ActionBadge({ action }: { action: string }) {
  const tone =
    action === "created" || action === "create"
      ? "bg-success/10 text-success border-success/30"
      : action === "deleted" || action === "delete" || action === "revoked" || action === "revoke"
        ? "bg-destructive/10 text-destructive border-destructive/40"
        : action === "approved" || action === "approve"
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
    return new Date(iso).toLocaleString("da-DK", { dateStyle: "short", timeStyle: "short" });
  } catch {
    return iso;
  }
}
