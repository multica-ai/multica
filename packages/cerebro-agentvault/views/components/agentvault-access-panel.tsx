"use client";

import { useState } from "react";
import { KeyRound, Plus, Trash2, ShieldOff } from "lucide-react";
import { toast } from "sonner";

import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from "@multica/ui/components/ui/table";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useCurrentMember } from "@multica/core/permissions";
import { useWorkspaceId } from "@multica/core/hooks";

import type { AgentVaultRole } from "../types";
import {
  useAgentVaultAccess,
  useSetAgentVaultAccess,
  useDeleteAgentVaultAccess,
} from "../queries";

const ROLES: AgentVaultRole[] = ["read-only", "member", "admin"];

/**
 * Per-agent Agent Vault access table (TECH-3196). Admin-only: grant or revoke
 * which Agent Vault boxes (vaults) an agent may use, and at which role. The
 * backend mints a vault-scoped token at task claim from exactly these rows.
 */
export function AgentVaultAccessPanel({
  agentId,
}: {
  agentId: string;
  agentName?: string;
}) {
  const wsId = useWorkspaceId();
  const { role, isLoading: memberLoading } = useCurrentMember(wsId);
  const { data: access = [], isLoading } = useAgentVaultAccess(wsId, agentId);
  const setAccess = useSetAgentVaultAccess(wsId, agentId);
  const delAccess = useDeleteAgentVaultAccess(wsId, agentId);

  const [vault, setVault] = useState("");
  const [newRole, setNewRole] = useState<AgentVaultRole>("read-only");

  if (!wsId || memberLoading) {
    return (
      <div className="flex h-24 items-center justify-center text-sm text-muted-foreground">
        Loading…
      </div>
    );
  }

  const isAdmin = role === "owner" || role === "admin";
  if (!isAdmin) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 p-6 text-center">
        <ShieldOff className="size-8 text-muted-foreground" />
        <h2 className="text-base font-medium">Access denied</h2>
        <p className="max-w-sm text-sm text-muted-foreground">
          Only workspace owners and admins can manage Agent Vault access.
        </p>
      </div>
    );
  }

  async function add() {
    const v = vault.trim();
    if (!v) return;
    try {
      await setAccess.mutateAsync({ vault: v, role: newRole });
      setVault("");
      toast.success(`Granted ${v}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to grant access");
    }
  }

  async function remove(v: string) {
    try {
      await delAccess.mutateAsync(v);
      toast.success(`Removed ${v}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to remove access");
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        <KeyRound className="size-4 text-muted-foreground" />
        <p className="text-sm text-muted-foreground">
          Boxes this agent may use via Agent Vault. The agent uses a secret without
          ever holding it.
        </p>
      </div>

      <div className="flex items-end gap-2">
        <div className="flex flex-1 flex-col gap-1">
          <label className="text-xs text-muted-foreground">Box (vault)</label>
          <Input
            value={vault}
            onChange={(e) => setVault(e.target.value)}
            placeholder="e.g. cloudflare"
          />
        </div>
        <div className="flex flex-col gap-1">
          <label className="text-xs text-muted-foreground">Role</label>
          <Select value={newRole} onValueChange={(v) => setNewRole(v as AgentVaultRole)}>
            <SelectTrigger className="w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ROLES.map((r) => (
                <SelectItem key={r} value={r}>
                  {r}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button onClick={add} disabled={!vault.trim() || setAccess.isPending}>
          <Plus className="size-4" />
          Grant
        </Button>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Box</TableHead>
            <TableHead>Role</TableHead>
            <TableHead className="w-12" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {isLoading ? (
            <TableRow>
              <TableCell colSpan={3} className="text-center text-sm text-muted-foreground">
                Loading…
              </TableCell>
            </TableRow>
          ) : access.length === 0 ? (
            <TableRow>
              <TableCell colSpan={3} className="text-center text-sm text-muted-foreground">
                No boxes granted yet.
              </TableCell>
            </TableRow>
          ) : (
            access.map((a) => (
              <TableRow key={a.vault}>
                <TableCell className="font-medium">{a.vault}</TableCell>
                <TableCell>
                  <Badge variant="secondary">{a.role}</Badge>
                </TableCell>
                <TableCell>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => remove(a.vault)}
                    disabled={delAccess.isPending}
                    aria-label={`Remove ${a.vault}`}
                  >
                    <Trash2 className="size-4 text-destructive" />
                  </Button>
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  );
}
