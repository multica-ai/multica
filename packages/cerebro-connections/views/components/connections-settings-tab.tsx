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
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";

import type { Connection } from "../types";
import { useConnections, useDeleteConnection, useToggleConnection } from "../queries";

export function ConnectionsSettingsTab() {
  const wsId = useWorkspaceId();
  const router = useNavigation();
  const wsPaths = useWorkspacePaths();
  const { role, isLoading: isMemberLoading } = useCurrentMember(wsId);
  const { data: connections = [], isLoading } = useConnections(wsId);
  const deleteConn = useDeleteConnection(wsId);
  const toggleConn = useToggleConnection(wsId);

  const [deleting, setDeleting] = useState<Connection | undefined>();

  if (!wsId || isMemberLoading) {
    return (
      <div className="flex h-32 items-center justify-center text-sm text-muted-foreground">
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
          Only workspace owners and admins can manage connections.
        </p>
      </div>
    );
  }

  async function confirmDelete() {
    if (!deleting) return;
    try {
      await deleteConn.mutateAsync(deleting.id);
      toast.success(`"${deleting.display_name}" deleted.`);
    } catch {
      toast.error("Delete failed. Please try again.");
    } finally {
      setDeleting(undefined);
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-base font-semibold">Connections</h2>
          <p className="text-sm text-muted-foreground">
            API and MCP endpoints (external or internal Sliplane paths) available
            to all runtimes in this workspace.
          </p>
        </div>
        <Button size="sm" onClick={() => router.push(wsPaths.connectionNew())}>
          <Plus className="mr-1 size-4" />
          New connection
        </Button>
      </div>

      {isLoading ? (
        <div className="text-sm text-muted-foreground">Loading connections…</div>
      ) : connections.length === 0 ? (
        <div className="flex flex-col items-center gap-3 rounded-md border border-dashed p-8 text-center">
          <Cable className="size-8 text-muted-foreground" />
          <p className="text-sm text-muted-foreground">
            No connections yet. Click <strong>New connection</strong> to register
            an MCP or API endpoint.
          </p>
        </div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Type</TableHead>
              <TableHead>URL</TableHead>
              <TableHead>Active</TableHead>
              <TableHead className="w-20" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {connections.map((conn) => (
              <TableRow key={conn.id}>
                <TableCell className="max-w-48">
                  <div className="truncate font-medium">{conn.display_name}</div>
                  <div className="truncate text-xs text-muted-foreground">
                    {conn.name}
                  </div>
                </TableCell>
                <TableCell>
                  <Badge variant="outline">
                    {conn.type === "mcp_http" ? "MCP" : "API"}
                  </Badge>
                  {conn.internal && (
                    <Badge variant="secondary" className="ml-1 text-xs">
                      internal
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
                    aria-label={conn.enabled ? "Disable" : "Enable"}
                  />
                </TableCell>
                <TableCell>
                  <div className="flex gap-1">
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => router.push(wsPaths.connectionEdit(conn.id))}
                      aria-label="Edit"
                    >
                      <Pencil className="size-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => setDeleting(conn)}
                      aria-label="Delete"
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

      <AlertDialog open={!!deleting} onOpenChange={(o) => !o && setDeleting(undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete connection?</AlertDialogTitle>
            <AlertDialogDescription>
              This permanently removes <strong>{deleting?.display_name}</strong>.
              Runtimes lose access immediately. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => void confirmDelete()}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
