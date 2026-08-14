"use client";

import { useMemo, useState } from "react";
import { Loader2, Pencil, Plus, Server, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
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
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useCurrentMember } from "@multica/core/permissions";
import { workspaceMcpConfigOptions } from "@multica/core/workspace/queries";
import {
  useDeleteWorkspaceMcpServer,
  useUpsertWorkspaceMcpServer,
} from "@multica/core/workspace/mutations";
import type { WorkspaceMcpServer } from "@multica/core/types";
import { McpServerDialog } from "../../agents/components/tabs/mcp-server-dialog";
import type { ManagedMcpServer } from "../../agents/components/tabs/mcp-config-model";
import { useT } from "../../i18n";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";

/**
 * Workspace-level MCP servers (GH #6062).
 *
 * Two things shape this screen and are worth stating up front:
 *
 *  - The stored configuration is WRITE-ONLY. The API returns names and
 *    transports, never urls / commands / headers / env, so there is no
 *    "current value" to prefill and editing a server means supplying its
 *    configuration again. The UI says so rather than pretending the empty
 *    form is the saved state.
 *  - Because the client cannot read the document, it cannot do the
 *    read-modify-write the agent tab does. Each row maps to a per-server
 *    endpoint and the server merges under a row lock.
 */
export function McpTab() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const currentMember = useCurrentMember(wsId);
  const canManage =
    currentMember.role === "owner" || currentMember.role === "admin";

  const configQuery = useQuery(workspaceMcpConfigOptions(wsId));
  const upsertServer = useUpsertWorkspaceMcpServer(wsId);
  const deleteServer = useDeleteWorkspaceMcpServer(wsId);

  const servers = configQuery.data?.servers ?? [];
  const existingNames = useMemo(
    () => new Set(servers.map((server) => server.name)),
    [servers],
  );

  const [editorOpen, setEditorOpen] = useState(false);
  const [editingServer, setEditingServer] = useState<WorkspaceMcpServer | null>(
    null,
  );
  const [deletingName, setDeletingName] = useState<string | null>(null);

  // The dialog is shared with the agent MCP tab, which hands it the saved
  // entry to prefill. Here there is nothing to prefill — an edit always
  // starts from an empty form and REPLACES the entry. The transport still
  // comes from the safe summary so the form opens on the right one.
  const dialogServer: ManagedMcpServer | null = editingServer
    ? {
        name: editingServer.name,
        config: {},
        container: "mcpServers",
        transport: editingServer.transport,
        enabled: editingServer.enabled,
      }
    : null;

  const handleSaveServer = async (
    name: string,
    config: Record<string, unknown>,
  ) => {
    // The name is pinned while editing (see lockName below), so an edit can
    // only ever replace the entry it opened — never create a second one and
    // leave the original behind while reporting "updated".
    const targetName = editingServer ? editingServer.name : name;
    try {
      await upsertServer.mutateAsync({ name: targetName, entry: config });
      toast.success(
        editingServer
          ? t(($) => $.mcp.updated_toast)
          : t(($) => $.mcp.added_toast),
      );
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.mcp.save_failed_toast),
      );
      throw error;
    }
  };

  const handleDelete = async () => {
    if (!deletingName) return;
    try {
      await deleteServer.mutateAsync(deletingName);
      toast.success(t(($) => $.mcp.removed_toast));
      setDeletingName(null);
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.mcp.remove_failed_toast),
      );
    }
  };

  return (
    <SettingsTab
      title={t(($) => $.mcp.title)}
      description={t(($) => $.mcp.description)}
    >
      <SettingsSection
        title={t(($) => $.mcp.servers_title)}
        description={t(($) => $.mcp.write_only_note)}
        action={
          canManage ? (
            <Button
              size="sm"
              onClick={() => {
                setEditingServer(null);
                setEditorOpen(true);
              }}
            >
              <Plus className="h-4 w-4" />
              {t(($) => $.mcp.add_server)}
            </Button>
          ) : null
        }
      >
        <SettingsCard>
          {configQuery.isLoading ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
            </div>
          ) : servers.length === 0 ? (
            <div className="px-4 py-8 text-center">
              <Server className="mx-auto h-5 w-5 text-muted-foreground" />
              <p className="mt-3 text-body font-medium">
                {t(($) => $.mcp.empty_title)}
              </p>
              <p className="mx-auto mt-1 max-w-md text-caption leading-5 text-muted-foreground">
                {t(($) => $.mcp.empty_description)}
              </p>
            </div>
          ) : (
            <ul className="divide-y divide-surface-border">
              {servers.map((server) => (
                <McpServerRow
                  key={server.name}
                  server={server}
                  canManage={canManage}
                  onEdit={() => {
                    setEditingServer(server);
                    setEditorOpen(true);
                  }}
                  onDelete={() => setDeletingName(server.name)}
                />
              ))}
            </ul>
          )}
        </SettingsCard>
        {!canManage && !currentMember.isLoading ? (
          <p className="px-0.5 text-caption text-muted-foreground">
            {t(($) => $.mcp.admin_only_note)}
          </p>
        ) : null}
      </SettingsSection>

      <McpServerDialog
        open={editorOpen}
        server={dialogServer}
        existingNames={existingNames}
        // No atomic rename exists on this API: renaming would add a server
        // and orphan the old one, so editing pins the name.
        lockName={editingServer !== null}
        onOpenChange={setEditorOpen}
        onSave={handleSaveServer}
      />

      <AlertDialog
        open={deletingName !== null}
        onOpenChange={(open) => {
          if (!open) setDeletingName(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.mcp.delete_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.mcp.delete_description, { name: deletingName ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteServer.isPending}>
              {t(($) => $.mcp.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                event.preventDefault();
                void handleDelete();
              }}
              disabled={deleteServer.isPending}
            >
              {deleteServer.isPending ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : null}
              {t(($) => $.mcp.delete_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsTab>
  );
}

function McpServerRow({
  server,
  canManage,
  onEdit,
  onDelete,
}: {
  server: WorkspaceMcpServer;
  canManage: boolean;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { t } = useT("settings");
  return (
    <li className="flex items-center gap-3 px-4 py-3">
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-body font-medium">{server.name}</span>
          {server.enabled === false ? (
            <Badge variant="secondary">{t(($) => $.mcp.disabled_badge)}</Badge>
          ) : null}
        </div>
        <p className="mt-0.5 text-caption text-muted-foreground">
          {transportLabel(server.transport)}
        </p>
      </div>
      {canManage ? (
        <div className="flex shrink-0 items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            onClick={onEdit}
            aria-label={t(($) => $.mcp.edit_server)}
          >
            <Pencil className="h-4 w-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={onDelete}
            aria-label={t(($) => $.mcp.remove_server)}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      ) : null}
    </li>
  );
}

/**
 * `transport` is a server-driven string, so an unknown value from a newer
 * backend renders as itself instead of disappearing.
 */
function transportLabel(transport: string): string {
  switch (transport) {
    case "stdio":
      return "stdio";
    case "http":
      return "HTTP";
    case "sse":
      return "SSE";
    default:
      return transport || "unknown";
  }
}
