"use client";

import { useState } from "react";
import { Cable, Plus, Pencil, Trash2, ShieldOff } from "lucide-react";
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
import { Switch } from "@multica/ui/components/ui/switch";
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
import { useCurrentMember } from "@multica/core/permissions";
import { useWorkspaceId } from "@multica/core/hooks";

import type { Connection } from "../types";
import { useConnections, useDeleteConnection, useToggleConnection } from "../queries";
import { ConnectionFormDialog } from "./connection-form-dialog";

export function ConnectionsSettingsTab() {
  const wsId = useWorkspaceId();
  const { role, isLoading: isMemberLoading } = useCurrentMember(wsId);
  const { data: connections = [], isLoading } = useConnections(wsId);
  const deleteConn = useDeleteConnection(wsId);
  const toggleConn = useToggleConnection(wsId);

  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Connection | undefined>();
  const [deleting, setDeleting] = useState<Connection | undefined>();

  if (!wsId || isMemberLoading) {
    return (
      <div className="flex h-32 items-center justify-center text-sm text-muted-foreground">
        Indlæser…
      </div>
    );
  }

  const isAdmin = role === "owner" || role === "admin";
  if (!isAdmin) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 p-6 text-center">
        <ShieldOff className="size-8 text-muted-foreground" />
        <h2 className="text-base font-medium">Adgang nægtet</h2>
        <p className="max-w-sm text-sm text-muted-foreground">
          Kun workspace-owners og -admins kan administrere forbindelser.
        </p>
      </div>
    );
  }

  function openCreate() {
    setEditing(undefined);
    setFormOpen(true);
  }

  function openEdit(conn: Connection) {
    setEditing(conn);
    setFormOpen(true);
  }

  async function confirmDelete() {
    if (!deleting) return;
    try {
      await deleteConn.mutateAsync(deleting.id);
      toast.success(`"${deleting.display_name}" er slettet.`);
    } catch {
      toast.error("Sletning fejlede. Prøv igen.");
    } finally {
      setDeleting(undefined);
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-base font-semibold">Forbindelser</h2>
          <p className="text-sm text-muted-foreground">
            API- og MCP-endpoints (eksternt eller internt Sliplane) der er
            tilgængelige for alle runtimes i dette workspace.
          </p>
        </div>
        <Button size="sm" onClick={openCreate}>
          <Plus className="mr-1 size-4" />
          Ny forbindelse
        </Button>
      </div>

      {isLoading ? (
        <div className="text-sm text-muted-foreground">Indlæser forbindelser…</div>
      ) : connections.length === 0 ? (
        <div className="flex flex-col items-center gap-3 rounded-md border border-dashed p-8 text-center">
          <Cable className="size-8 text-muted-foreground" />
          <p className="text-sm text-muted-foreground">
            Ingen forbindelser endnu. Klik <strong>Ny forbindelse</strong> for at
            registrere en MCP- eller API-endpoint.
          </p>
        </div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Navn</TableHead>
              <TableHead>Type</TableHead>
              <TableHead>URL</TableHead>
              <TableHead>Aktiv</TableHead>
              <TableHead className="w-20" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {connections.map((conn) => (
              <TableRow key={conn.id}>
                <TableCell>
                  <div className="font-medium">{conn.display_name}</div>
                  <div className="text-xs text-muted-foreground">{conn.name}</div>
                </TableCell>
                <TableCell>
                  <Badge variant="outline">
                    {conn.type === "mcp_http" ? "MCP" : "API"}
                  </Badge>
                  {conn.internal && (
                    <Badge variant="secondary" className="ml-1 text-xs">
                      intern
                    </Badge>
                  )}
                </TableCell>
                <TableCell className="max-w-48 truncate text-sm text-muted-foreground">
                  {conn.url}
                </TableCell>
                <TableCell>
                  <Switch
                    checked={conn.enabled}
                    onCheckedChange={(enabled) =>
                      void toggleConn.mutateAsync({ conn, enabled })
                    }
                    aria-label={conn.enabled ? "Deaktiver" : "Aktiver"}
                  />
                </TableCell>
                <TableCell>
                  <div className="flex gap-1">
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => openEdit(conn)}
                      aria-label="Rediger"
                    >
                      <Pencil className="size-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => setDeleting(conn)}
                      aria-label="Slet"
                    >
                      <Trash2 className="size-4 text-destructive" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <ConnectionFormDialog
        wsId={wsId}
        open={formOpen}
        onOpenChange={setFormOpen}
        existing={editing}
      />

      <AlertDialog open={!!deleting} onOpenChange={(o) => !o && setDeleting(undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Slet forbindelsen?</AlertDialogTitle>
            <AlertDialogDescription>
              Dette fjerner <strong>{deleting?.display_name}</strong> permanent.
              Runtimes mister adgang med det samme. Handlingen kan ikke fortrydes.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Annuller</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => void confirmDelete()}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Slet
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
