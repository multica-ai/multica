"use client";

import { useMemo, useState } from "react";
import { Loader2, Plus, Server } from "lucide-react";
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
import { workspaceMcpServersOptions } from "@multica/core/workspace/queries";
import {
  useCreateWorkspaceMcpServer,
  useDeleteWorkspaceMcpServer,
  useUpdateWorkspaceMcpServer,
} from "@multica/core/workspace/mutations";
import type { WorkspaceMcpServer } from "@multica/core/types";
import { McpServerDialog } from "../../agents/components/tabs/mcp-server-dialog";
import type { ManagedMcpServer } from "../../agents/components/tabs/mcp-config-model";
import { McpServerRow } from "../../common/mcp-server-row";
import { useT } from "../../i18n";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";

/**
 * The workspace MCP server library (GH #6062).
 *
 * Two things shape this screen and are worth stating up front:
 *
 *  - A server added here is given to NO agent. It is a library entry, exactly
 *    like a workspace skill: an agent owner assigns it on the agent's own MCP
 *    tab, where it also gets a per-agent on/off toggle. Nothing here reaches an
 *    agent implicitly.
 *  - The stored configuration is WRITE-ONLY. The API returns names and
 *    transports, never urls / commands / headers / env, so there is no
 *    "current value" to prefill and replacing a server means supplying its
 *    complete configuration again. Renaming stays separate and never touches
 *    the write-only entry.
 */
export function McpTab() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const currentMember = useCurrentMember(wsId);
  const canManage =
    currentMember.role === "owner" || currentMember.role === "admin";

  const serversQuery = useQuery(workspaceMcpServersOptions(wsId));
  const createServer = useCreateWorkspaceMcpServer(wsId);
  const updateServer = useUpdateWorkspaceMcpServer(wsId);
  const deleteServer = useDeleteWorkspaceMcpServer(wsId);

  const servers = useMemo(() => serversQuery.data ?? [], [serversQuery.data]);
  const existingNames = useMemo(
    () => new Set(servers.map((server) => server.name)),
    [servers],
  );

  const [editorOpen, setEditorOpen] = useState(false);
  const [editingServer, setEditingServer] = useState<WorkspaceMcpServer | null>(
    null,
  );
  const [renamingServer, setRenamingServer] =
    useState<WorkspaceMcpServer | null>(null);
  const [renameDraft, setRenameDraft] = useState("");
  const [renameError, setRenameError] = useState("");
  const [renamePending, setRenamePending] = useState(false);
  const [deletingServer, setDeletingServer] = useState<WorkspaceMcpServer | null>(
    null,
  );

  // The dialog is shared with the agent MCP tab, which hands it the saved
  // entry to prefill. Here there is nothing to prefill — an edit always
  // starts from an empty form and REPLACES the entry. The transport still
  // comes from the safe summary so the form opens on the right one.
  const dialogServer: ManagedMcpServer | null = useMemo(
    () =>
      editingServer
        ? {
            name: editingServer.name,
            config: {},
            container: "mcpServers",
            transport: editingServer.transport,
            // The library has no per-agent toggle; this field only feeds the
            // dialog's shape.
            enabled: true,
          }
        : null,
    [editingServer],
  );

  const handleSaveServer = async (
    name: string,
    config: Record<string, unknown>,
  ) => {
    try {
      if (editingServer) {
        await updateServer.mutateAsync({ serverId: editingServer.id, config });
      } else {
        await createServer.mutateAsync({ name, config });
      }
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

  const startRename = (server: WorkspaceMcpServer) => {
    if (renamePending) return;
    setRenamingServer(server);
    setRenameDraft(server.name);
    setRenameError("");
  };

  const cancelRename = () => {
    if (renamePending) return;
    setRenamingServer(null);
    setRenameDraft("");
    setRenameError("");
  };

  const handleRename = async () => {
    if (!renamingServer || renamePending) return;
    const name = renameDraft.trim();
    if (name === "") {
      setRenameError(t(($) => $.mcp.rename_required));
      return;
    }
    if (!/^[A-Za-z0-9_-]+$/.test(name)) {
      setRenameError(t(($) => $.mcp.rename_invalid));
      return;
    }
    if (name !== renamingServer.name && existingNames.has(name)) {
      setRenameError(t(($) => $.mcp.rename_duplicate));
      return;
    }
    if (name === renamingServer.name) {
      cancelRename();
      return;
    }

    setRenamePending(true);
    try {
      await updateServer.mutateAsync({ serverId: renamingServer.id, name });
      toast.success(t(($) => $.mcp.renamed_toast));
      setRenamingServer(null);
      setRenameDraft("");
      setRenameError("");
    } catch (error) {
      const message =
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.mcp.rename_failed_toast);
      setRenameError(message);
      toast.error(message);
    } finally {
      setRenamePending(false);
    }
  };

  const handleDelete = async () => {
    if (!deletingServer) return;
    try {
      await deleteServer.mutateAsync(deletingServer.id);
      toast.success(t(($) => $.mcp.removed_toast));
      setDeletingServer(null);
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
              disabled={renamePending}
              onClick={() => {
                cancelRename();
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
          {serversQuery.isLoading ? (
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
                  name={server.name}
                  transport={server.transport}
                  status={
                    server.enabled === false ? (
                      <Badge variant="secondary">
                        {t(($) => $.mcp.disabled_badge)}
                      </Badge>
                    ) : undefined
                  }
                  canManage={canManage}
                  actionsDisabled={renamePending}
                  rename={
                    renamingServer?.id === server.id
                      ? {
                          draft: renameDraft,
                          error: renameError,
                          pending: renamePending,
                          onChange: (value) => {
                            setRenameDraft(value);
                            setRenameError("");
                          },
                          onCancel: cancelRename,
                          onSubmit: () => void handleRename(),
                        }
                      : undefined
                  }
                  labels={{
                    rename: t(($) => $.mcp.rename_action),
                    renameAria: t(($) => $.mcp.rename_server),
                    renameSave: t(($) => $.mcp.rename_save),
                    renameCancel: t(($) => $.mcp.rename_cancel),
                    configure: t(($) => $.mcp.replace_config),
                    configureAria: t(($) => $.mcp.replace_config),
                    remove: t(($) => $.mcp.remove_action),
                    removeAria: t(($) => $.mcp.remove_server),
                  }}
                  configureKind="replace"
                  onRenameStart={() => startRename(server)}
                  onConfigure={() => {
                    cancelRename();
                    setEditingServer(server);
                    setEditorOpen(true);
                  }}
                  onRemove={() => {
                    cancelRename();
                    setDeletingServer(server);
                  }}
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
        replacementMode={editingServer !== null}
        onOpenChange={setEditorOpen}
        onSave={handleSaveServer}
      />

      <AlertDialog
        open={deletingServer !== null}
        onOpenChange={(open) => {
          if (!open) setDeletingServer(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.mcp.delete_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.mcp.delete_description, { name: deletingServer?.name ?? "" })}
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
