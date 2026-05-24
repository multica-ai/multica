"use client";

import { useQuery } from "@tanstack/react-query";
import { Loader2, Inbox } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";
import { approvalsListOptions } from "../../core/queries";
import type { Approval } from "../../core/types";
import { StatusBadge } from "./status-badge";

interface ApprovalListProps {
  wsId: string;
  status: string | null;
  selectedId: string | null;
  onSelect: (id: string) => void;
}

// One flat list of asks (pending first, newest first). Clicking a row opens the
// detail sheet. Matches the Tools-UI table design language.
export function ApprovalList({
  wsId,
  status,
  selectedId,
  onSelect,
}: ApprovalListProps) {
  const query = useQuery(
    approvalsListOptions(wsId, { status, limit: 50, offset: 0 }),
  );

  if (query.isLoading) {
    return (
      <div className="flex h-40 items-center justify-center text-muted-foreground">
        <Loader2 className="size-4 animate-spin" />
      </div>
    );
  }

  const approvals = query.data?.approvals ?? [];
  if (approvals.length === 0) {
    return (
      <div className="flex h-40 flex-col items-center justify-center gap-2 text-center text-muted-foreground">
        <Inbox className="size-6" />
        <p className="text-sm">Nothing in the inbox</p>
      </div>
    );
  }

  return (
    <table className="w-full text-sm">
      <thead className="bg-muted/30 text-xs uppercase tracking-wide text-muted-foreground">
        <tr>
          <th className="px-4 py-2.5 text-left font-medium">Capability</th>
          <th className="px-4 py-2.5 text-left font-medium">Resource</th>
          <th className="px-4 py-2.5 text-left font-medium">Requester</th>
          <th className="px-4 py-2.5 text-left font-medium">Status</th>
          <th className="px-4 py-2.5 text-left font-medium">Created</th>
        </tr>
      </thead>
      <tbody>
        {approvals.map((a: Approval) => (
          <tr
            key={a.id}
            onClick={() => onSelect(a.id)}
            className={cn(
              "cursor-pointer border-t hover:bg-muted/40",
              selectedId === a.id && "bg-muted/60",
            )}
          >
            <td className="px-4 py-2.5 font-medium">{a.capability || "—"}</td>
            <td className="px-4 py-2.5 text-muted-foreground">
              {a.resource || "*"}
            </td>
            <td className="px-4 py-2.5">
              <Badge variant="outline">{a.requester_type}</Badge>
            </td>
            <td className="px-4 py-2.5">
              <StatusBadge status={a.status} />
            </td>
            <td className="px-4 py-2.5 text-muted-foreground">
              {formatWhen(a.created_at)}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function formatWhen(iso: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString();
}
