"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, Lock, ShieldAlert, ShieldCheck } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  grantsListOptions,
  usePermissionsStore,
  type GrantStatus,
  type PersonaGrant,
} from "../../core";

interface GrantsTableProps {
  wsId: string;
  onRowClick: (grantId: string) => void;
}

export function GrantsTable({ wsId, onRowClick }: GrantsTableProps) {
  const subjectType = usePermissionsStore((s) => s.subjectType);
  const subjectId = usePermissionsStore((s) => s.subjectId);
  const resourceType = usePermissionsStore((s) => s.resourceType);
  const status = usePermissionsStore((s) => s.status);
  const classification = usePermissionsStore((s) => s.classification);
  const search = usePermissionsStore((s) => s.search);
  const limit = usePermissionsStore((s) => s.limit);
  const offset = usePermissionsStore((s) => s.offset);
  const nextPage = usePermissionsStore((s) => s.nextPage);
  const prevPage = usePermissionsStore((s) => s.prevPage);

  const filter = useMemo(
    () => ({
      subjectType,
      subjectId,
      resourceType,
      status,
      classification,
      search,
      limit,
      offset,
    }),
    [
      subjectType,
      subjectId,
      resourceType,
      status,
      classification,
      search,
      limit,
      offset,
    ],
  );

  const list = useQuery(grantsListOptions(wsId, filter));

  // Server-driven search would be nicer, but until JEH-1179 surfaces a
  // `q=` param we do a client-side filter on the current page so the
  // toolbar Søg input still works during the API-shape sketch phase.
  const grants = useMemo(() => {
    const items = list.data?.items ?? [];
    const q = search.trim().toLowerCase();
    if (!q) return items;
    return items.filter((g) => {
      const subj = g.subject.display_name?.toLowerCase() ?? "";
      const cap = g.capability.toLowerCase();
      const res = g.resource.pattern.toLowerCase();
      return subj.includes(q) || cap.includes(q) || res.includes(q);
    });
  }, [list.data, search]);

  const total = list.data?.total ?? grants.length;

  if (list.isError) {
    return (
      <div className="m-4 flex items-start gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
        <AlertCircle className="size-4 mt-0.5 shrink-0" />
        <div className="min-w-0">
          <div className="font-medium">Kunne ikke hente grants</div>
          <div className="text-xs text-destructive/80 truncate">
            {list.error instanceof Error
              ? list.error.message
              : "Ukendt fejl. Genindlæs siden eller prøv igen om lidt."}
          </div>
          <div className="mt-2 text-xs text-muted-foreground">
            Grant-API (JEH-1179) er muligvis endnu ikke deployed i dette
            miljø — slå feature-flaget fra hvis det blokerer.
          </div>
        </div>
      </div>
    );
  }

  if (list.isLoading && grants.length === 0) {
    return (
      <div className="space-y-1 p-4">
        {[0, 1, 2, 3, 4, 5].map((i) => (
          <div key={i} className="h-9 animate-pulse rounded bg-muted/60" />
        ))}
      </div>
    );
  }

  if (grants.length === 0) {
    return (
      <div className="m-4 rounded-md border border-dashed p-12 text-center">
        <ShieldCheck className="mx-auto size-8 text-muted-foreground/60" />
        <p className="mt-3 text-sm font-medium">Ingen grants matcher</p>
        <p className="mt-1 text-xs text-muted-foreground">
          Justér filtrene eller opret et nyt grant for at give adgang.
        </p>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex-1 overflow-auto">
        <table className="w-full text-sm">
          <thead className="sticky top-0 z-10 bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/70">
            <tr className="border-b text-left text-[11px] uppercase tracking-wide text-muted-foreground">
              <Th>Subjekt</Th>
              <Th>Ressource</Th>
              <Th>Kapabilitet</Th>
              <Th>Klassifikation</Th>
              <Th>Status</Th>
              <Th>Tidsvindue</Th>
              <Th>Approval</Th>
            </tr>
          </thead>
          <tbody>
            {grants.map((g) => (
              <GrantRow key={g.id} grant={g} onClick={() => onRowClick(g.id)} />
            ))}
          </tbody>
        </table>
      </div>

      <div className="flex items-center justify-between border-t bg-background/50 px-4 py-2 text-xs text-muted-foreground">
        <span className="font-mono tabular-nums">
          {offset + 1}–{offset + grants.length} af {total}
        </span>
        <div className="flex gap-1">
          <Button
            variant="outline"
            size="sm"
            onClick={prevPage}
            disabled={offset === 0}
          >
            Forrige
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={nextPage}
            disabled={offset + grants.length >= total}
          >
            Næste
          </Button>
        </div>
      </div>
    </div>
  );
}

function Th({ children }: { children: React.ReactNode }) {
  return <th className="px-4 py-2 font-medium">{children}</th>;
}

function GrantRow({
  grant,
  onClick,
}: {
  grant: PersonaGrant;
  onClick: () => void;
}) {
  return (
    <tr
      onClick={onClick}
      className="cursor-pointer border-b transition-colors hover:bg-muted/40 focus-within:bg-muted/40"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onClick();
        }
      }}
    >
      <td className="px-4 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <span className="rounded-sm bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
            {grant.subject.type}
          </span>
          <span className="truncate">
            {grant.subject.display_name ?? grant.subject.id ?? "—"}
          </span>
        </div>
      </td>
      <td className="px-4 py-2 font-mono text-xs">
        <span className="text-muted-foreground">{grant.resource.type}</span>
        <span className="mx-1 text-muted-foreground/50">:</span>
        <span>{grant.resource.pattern}</span>
      </td>
      <td className="px-4 py-2 font-mono text-xs">{grant.capability}</td>
      <td className="px-4 py-2">
        <ClassificationBadge value={grant.classification_ceiling} />
      </td>
      <td className="px-4 py-2">
        <StatusBadge value={grant.status} />
      </td>
      <td className="px-4 py-2 text-xs text-muted-foreground">
        {grant.time_window?.ends_at
          ? `→ ${formatDate(grant.time_window.ends_at)}`
          : "altid"}
      </td>
      <td className="px-4 py-2">
        {grant.approval_required ? (
          <span className="inline-flex items-center gap-1 text-xs text-warning">
            <Lock className="size-3" /> påkrævet
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">nej</span>
        )}
      </td>
    </tr>
  );
}

function ClassificationBadge({ value }: { value: string }) {
  const tone =
    value === "secret" || value === "restricted"
      ? "destructive"
      : value === "confidential"
        ? "warning"
        : "muted";
  return (
    <Badge variant="outline" className={cn("text-[10px]", toneClass(tone))}>
      {value}
    </Badge>
  );
}

function StatusBadge({ value }: { value: string }) {
  const known: GrantStatus[] = [
    "active",
    "pending_approval",
    "expired",
    "revoked",
  ];
  const isKnown = (known as string[]).includes(value);
  const tone =
    value === "active"
      ? "success"
      : value === "pending_approval"
        ? "warning"
        : value === "revoked"
          ? "destructive"
          : "muted";
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-sm px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide",
        toneClass(tone),
      )}
    >
      {value === "pending_approval" && <ShieldAlert className="size-3" />}
      {isKnown ? labelStatus(value as GrantStatus) : value}
    </span>
  );
}

function labelStatus(s: GrantStatus): string {
  switch (s) {
    case "active":
      return "aktiv";
    case "pending_approval":
      return "approval";
    case "expired":
      return "udløbet";
    case "revoked":
      return "tilbagekaldt";
  }
}

function toneClass(
  tone: "success" | "warning" | "destructive" | "muted",
): string {
  switch (tone) {
    case "success":
      return "bg-success/10 text-success border-success/30";
    case "warning":
      return "bg-warning/10 text-warning border-warning/40";
    case "destructive":
      return "bg-destructive/10 text-destructive border-destructive/40";
    case "muted":
    default:
      return "bg-muted text-muted-foreground border-border";
  }
}

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString();
  } catch {
    return iso;
  }
}
