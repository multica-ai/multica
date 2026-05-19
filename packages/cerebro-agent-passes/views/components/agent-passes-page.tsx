"use client";

import { useState } from "react";
import { KeyRound, ShieldOff } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { useCurrentMember } from "@multica/core/permissions";
import { useCurrentWorkspace } from "@multica/core/paths";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { PageHeader } from "@multica/views/layout/page-header";

import { agentPassesListOptions, agentPassesKeys } from "../../core/queries";
import { revokeAgentPass } from "../../core/api";
import {
  formatSpendCeiling,
  type AgentPass,
  type AgentPassStatus,
} from "../../core/types";
import { CreateAgentPassDialog } from "./create-agent-pass-dialog";

// Workspace admin page for Cerebro agent passes (JEH-1731). Owner/admin
// only. Backed by /api/cerebro/agent-passes — lists every pass in the
// workspace and offers row-level revoke + a "new pass" dialog that fails
// closed on scope widening (HTTP 422 → inline error in the dialog).
export function AgentPassesPage() {
  const workspace = useCurrentWorkspace();
  const { role, isLoading: isMemberLoading } = useCurrentMember(workspace?.id ?? "");
  const [showCreate, setShowCreate] = useState(false);

  const wsId = workspace?.id ?? "";
  const list = useQuery(agentPassesListOptions(wsId));
  const queryClient = useQueryClient();

  const revokeMutation = useMutation({
    mutationFn: ({ passId, reason }: { passId: string; reason?: string }) =>
      revokeAgentPass(passId, reason ? { reason } : undefined),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: agentPassesKeys.all(wsId) });
    },
  });

  if (!workspace || isMemberLoading) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Workspace context indlæses…
      </div>
    );
  }

  const isAdmin = role === "owner" || role === "admin";
  if (!isAdmin) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 p-6 text-center">
        <ShieldOff className="size-8 text-muted-foreground" />
        <h2 className="text-base font-medium">Adgang nægtet</h2>
        <p className="max-w-sm text-sm text-muted-foreground">
          Kun workspace-owners og -admins kan administrere agent passes.
        </p>
      </div>
    );
  }

  const passes = list.data?.passes ?? [];

  const handleRevoke = (pass: AgentPass) => {
    if (pass.status !== "active") return;
    const reason = window.prompt(
      "Begrundelse for tilbagekaldelse (valgfri):",
      "",
    );
    if (reason === null) return; // cancelled
    revokeMutation.mutate({ passId: pass.id, reason: reason || undefined });
  };

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3 px-5">
        <div className="flex min-w-0 items-center gap-2">
          <KeyRound className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">Agent passes</h1>
          <p className="ml-2 hidden truncate text-xs text-muted-foreground md:block">
            Maskinlæsbare mandater der afgrænser hvad en agent må på et issue
          </p>
        </div>
        <Button size="sm" onClick={() => setShowCreate(true)}>
          Udsted pas
        </Button>
      </PageHeader>

      <div className="flex-1 min-h-0 overflow-y-auto px-5 py-4">
        {list.isError && (
          <div className="rounded border border-destructive/40 bg-destructive/5 px-3 py-2 text-sm text-destructive">
            Kunne ikke hente agent passes:{" "}
            {(list.error as Error)?.message ?? "ukendt fejl"}
          </div>
        )}
        {list.isLoading && !list.data && (
          <div className="text-sm text-muted-foreground">Indlæser…</div>
        )}
        {list.data && passes.length === 0 && (
          <div className="rounded border border-dashed p-6 text-center text-sm text-muted-foreground">
            Ingen agent passes endnu. Klik på "Udsted pas" for at oprette den
            første.
          </div>
        )}
        {passes.length > 0 && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Status</TableHead>
                <TableHead>Agent</TableHead>
                <TableHead>Issue</TableHead>
                <TableHead>Scope</TableHead>
                <TableHead>Spend ceiling</TableHead>
                <TableHead>Udløber</TableHead>
                <TableHead className="w-24" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {passes.map((pass) => (
                <TableRow key={pass.id}>
                  <TableCell>
                    <StatusBadge status={pass.status} />
                  </TableCell>
                  <TableCell className="font-mono text-[11px]">
                    {pass.agent_id.slice(0, 8)}…
                  </TableCell>
                  <TableCell className="font-mono text-[11px]">
                    {pass.issue_id.slice(0, 8)}…
                  </TableCell>
                  <TableCell className="max-w-[24ch] truncate text-xs text-muted-foreground">
                    {summarizeScope(pass.scope)}
                  </TableCell>
                  <TableCell className="text-xs">
                    {formatSpendCeiling(pass.spend_ceiling_micros)}
                  </TableCell>
                  <TableCell className="text-xs">
                    {pass.expires_at ? formatDateShort(pass.expires_at) : "—"}
                  </TableCell>
                  <TableCell>
                    {pass.status === "active" ? (
                      <Button
                        size="sm"
                        variant="ghost"
                        disabled={revokeMutation.isPending}
                        onClick={() => handleRevoke(pass)}
                      >
                        Tilbagekald
                      </Button>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>

      {showCreate && (
        <CreateAgentPassDialog
          wsId={wsId}
          onClose={() => setShowCreate(false)}
        />
      )}
    </div>
  );
}

function StatusBadge({ status }: { status: AgentPassStatus }) {
  switch (status) {
    case "active":
      return <Badge variant="default">Aktiv</Badge>;
    case "revoked":
      return <Badge variant="destructive">Tilbagekaldt</Badge>;
    case "expired":
      return <Badge variant="secondary">Udløbet</Badge>;
    case "exhausted":
      return <Badge variant="secondary">Budget brugt</Badge>;
    default:
      return <Badge variant="outline">{status}</Badge>;
  }
}

function summarizeScope(scope: Record<string, unknown>): string {
  const keys = Object.keys(scope ?? {});
  if (keys.length === 0) return "{}";
  return keys
    .map((k) => {
      const v = scope[k];
      if (Array.isArray(v)) return `${k}=[${v.length}]`;
      return `${k}=${typeof v}`;
    })
    .join(", ");
}

function formatDateShort(iso: string): string {
  try {
    const d = new Date(iso);
    return d.toLocaleString(undefined, {
      year: "numeric",
      month: "short",
      day: "numeric",
    });
  } catch {
    return iso;
  }
}
