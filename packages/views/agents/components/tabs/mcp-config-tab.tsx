"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Loader2,
  Lock,
  Pencil,
  Plus,
  RefreshCw,
  Server,
  Trash2,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime, WorkspaceMcpServer } from "@multica/core/types";
import { ApiError } from "@multica/core/api";
import {
  isRuntimeUsableForUser,
  runtimeCapabilitiesOptions,
  runtimeDisplayLabel,
} from "@multica/core/runtimes";
import {
  agentMcpServersOptions,
  workspaceMcpServersOptions,
} from "@multica/core/workspace/queries";
import {
  useAddAgentMcpServer,
  useRemoveAgentMcpServer,
  useSetAgentMcpServerEnabled,
} from "@multica/core/workspace/mutations";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Switch } from "@multica/ui/components/ui/switch";
import { toast } from "sonner";
import { useT } from "../../../i18n";
import {
  listManagedMcpServers,
  removeManagedMcpServer,
  upsertManagedMcpServer,
  type ManagedMcpServer,
} from "./mcp-config-model";
import { McpServerDialog } from "./mcp-server-dialog";

export function McpConfigTab({
  agent,
  runtime,
  currentUserId,
  canEdit = true,
  onSave,
  onDirtyChange,
}: {
  agent: Agent;
  runtime: AgentRuntime | null;
  currentUserId?: string | null;
  /**
   * Whether this viewer may change the agent. A member without it can still
   * read the inventory — it carries no credential material — but every write
   * affordance is hidden rather than left to 403 on click.
   */
  canEdit?: boolean;
  onSave: (updates: { mcp_config: unknown | null }) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");
  const canReadRuntime =
    runtime != null && isRuntimeUsableForUser(runtime, currentUserId ?? null);
  const runtimeId =
    runtime?.runtime_mode === "local" &&
    runtime.status === "online" &&
    canReadRuntime
      ? runtime.id
      : null;
  const runtimeQuery = useQuery(runtimeCapabilitiesOptions(runtimeId));
  // The workspace MCP servers ASSIGNED to this agent, plus the library to pick
  // from (GH #6062). A library entry does nothing until it is added here, and
  // each assignment carries its own toggle. The API returns names and
  // transports only — never the stored credentials.
  const assignedQuery = useQuery(agentMcpServersOptions(agent.id));
  const libraryQuery = useQuery(
    workspaceMcpServersOptions(agent.workspace_id ?? ""),
  );
  const addServer = useAddAgentMcpServer(agent.id);
  const setServerEnabled = useSetAgentMcpServerEnabled(agent.id);
  const removeServer = useRemoveAgentMcpServer(agent.id);
  const redacted = agent.mcp_config_redacted === true;
  const managedServers = useMemo(
    () => listManagedMcpServers(agent.mcp_config),
    [agent.mcp_config],
  );
  const managedNames = useMemo(
    () => new Set(managedServers.map((server) => server.name)),
    [managedServers],
  );
  const assignedServers = assignedQuery.data ?? [];
  const assignedIds = useMemo(
    () => new Set(assignedServers.map((server) => server.id)),
    [assignedServers],
  );
  // What is still available to assign. An entry the agent already has is not
  // offered again.
  const availableServers = (libraryQuery.data ?? []).filter(
    (server) => !assignedIds.has(server.id),
  );
  // Names that shadow a runtime server in the effective set. The daemon merges
  // runtime < (assigned workspace servers + the agent's own), so an assigned
  // server hides a same-named runtime one too — marking only the agent's own
  // names would under-report it. A disabled assignment shadows nothing.
  const effectiveNames = useMemo(() => {
    const names = new Set(managedNames);
    for (const server of assignedServers) {
      if (server.enabled !== false) names.add(server.name);
    }
    return names;
  }, [managedNames, assignedServers]);

  const [editorOpen, setEditorOpen] = useState(false);
  const [editingServer, setEditingServer] = useState<ManagedMcpServer | null>(
    null,
  );
  const [deletingServer, setDeletingServer] =
    useState<ManagedMcpServer | null>(null);
  const [deleting, setDeleting] = useState(false);

  useEffect(() => onDirtyChange?.(false), [onDirtyChange]);

  const openAddDialog = () => {
    setEditingServer(null);
    setEditorOpen(true);
  };

  const openEditDialog = (server: ManagedMcpServer) => {
    setEditingServer(server);
    setEditorOpen(true);
  };

  const handleSaveServer = async (
    name: string,
    config: Record<string, unknown>,
  ) => {
    const next = upsertManagedMcpServer(
      agent.mcp_config,
      editingServer,
      name,
      config,
    );
    try {
      await onSave({ mcp_config: next });
      toast.success(
        editingServer
          ? t(($) => $.tab_body.mcp_config.updated_toast)
          : t(($) => $.tab_body.mcp_config.added_toast),
      );
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.tab_body.mcp_config.save_failed_toast),
      );
      throw error;
    }
  };

  // Assignment writes return the agent's resulting list, so failures surface
  // as a toast and the cache re-syncs from the server rather than a guess.
  const runAssignmentAction = async (action: () => Promise<unknown>) => {
    try {
      await action();
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.tab_body.mcp_config.workspace_action_failed),
      );
    }
  };

  const handleAddWorkspaceServer = (serverId: string) =>
    runAssignmentAction(async () => {
      await addServer.mutateAsync(serverId);
      toast.success(t(($) => $.tab_body.mcp_config.workspace_added_toast));
    });

  const handleToggleWorkspaceServer = (serverId: string, enabled: boolean) =>
    runAssignmentAction(() => setServerEnabled.mutateAsync({ serverId, enabled }));

  const handleRemoveWorkspaceServer = (serverId: string) =>
    runAssignmentAction(async () => {
      await removeServer.mutateAsync(serverId);
      toast.success(t(($) => $.tab_body.mcp_config.workspace_removed_toast));
    });

  const handleDelete = async () => {
    if (!deletingServer) return;
    setDeleting(true);
    try {
      await onSave({
        mcp_config: removeManagedMcpServer(agent.mcp_config, deletingServer),
      });
      toast.success(t(($) => $.tab_body.mcp_config.deleted_toast));
      setDeletingServer(null);
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.tab_body.mcp_config.delete_failed_toast),
      );
    } finally {
      setDeleting(false);
    }
  };

  return (
    // Three sources, one heading each. The prose that used to sit under every
    // heading is gone: precedence is already shown where it applies (the
    // Overridden badges) and nobody reads a paragraph to press a button.
    <div className="space-y-6">
      <section className="space-y-3">
        <div className="flex items-center justify-between gap-4">
          <h3 className="text-body font-medium">
            {t(($) => $.tab_body.mcp_config.managed_title)}
          </h3>
          {!redacted && (
            <Button size="sm" variant="outline" onClick={openAddDialog}>
              <Plus aria-hidden="true" />
              {t(($) => $.tab_body.mcp_config.add_action)}
            </Button>
          )}
        </div>

        {redacted ? (
          <div className="flex items-start gap-2 rounded-lg border px-4 py-3">
            <Lock
              className="mt-0.5 h-4 w-4 text-muted-foreground"
              aria-hidden="true"
            />
            <div>
              <p className="text-body font-medium">
                {t(($) => $.tab_body.mcp_config.redacted_title)}
              </p>
              <p className="mt-1 text-caption text-muted-foreground">
                {t(($) => $.tab_body.mcp_config.redacted_hint)}
              </p>
            </div>
          </div>
        ) : managedServers.length > 0 ? (
          <McpServerList
            servers={managedServers}
            disabledLabel={t(($) => $.tab_body.mcp_config.agent_disabled_badge)}
            onEdit={openEditDialog}
            onDelete={setDeletingServer}
            editLabel={t(($) => $.tab_body.mcp_config.edit_aria)}
            deleteLabel={t(($) => $.tab_body.mcp_config.delete_aria)}
          />
        ) : (
          <McpNotice text={t(($) => $.tab_body.mcp_config.managed_empty)} />
        )}
      </section>

      <section className="space-y-3">
        <div className="flex items-center justify-between gap-4">
          <h3 className="text-body font-medium">
            {t(($) => $.tab_body.mcp_config.workspace_title)}
          </h3>
          {canEdit && availableServers.length > 0 && (
            <McpWorkspaceServerPicker
              servers={availableServers}
              disabled={addServer.isPending}
              onSelect={(serverId) => void handleAddWorkspaceServer(serverId)}
            />
          )}
        </div>
        {assignedQuery.isLoading ? (
          <McpNotice
            loading
            text={t(($) => $.tab_body.mcp_config.workspace_loading)}
          />
        ) : assignedServers.length > 0 ? (
          <ul className="divide-y rounded-lg border bg-surface-raised/40">
            {assignedServers.map((server) => (
              <McpWorkspaceServerRow
                key={server.id}
                server={server}
                overridden={managedNames.has(server.name)}
                canEdit={canEdit}
                busy={setServerEnabled.isPending || removeServer.isPending}
                onToggle={(enabled) => void handleToggleWorkspaceServer(server.id, enabled)}
                onRemove={() => void handleRemoveWorkspaceServer(server.id)}
              />
            ))}
          </ul>
        ) : (
          <McpNotice
            text={
              (libraryQuery.data ?? []).length === 0
                ? t(($) => $.tab_body.mcp_config.workspace_library_empty)
                : t(($) => $.tab_body.mcp_config.workspace_none_assigned)
            }
          />
        )}
      </section>

      <section className="space-y-3">
        <div className="flex items-center justify-between gap-4">
          {/* The machine name moves into the heading — it is the only part of
              the old description that told the reader anything. */}
          <h3 className="min-w-0 truncate text-body font-medium">
            {runtime
              ? t(($) => $.tab_body.mcp_config.runtime_title_named, {
                  runtime: runtimeDisplayLabel(runtime),
                })
              : t(($) => $.tab_body.mcp_config.runtime_title)}
          </h3>
          {runtimeId && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => runtimeQuery.refetch()}
              disabled={runtimeQuery.isFetching}
            >
              <RefreshCw
                className={
                  runtimeQuery.isFetching
                    ? "h-3.5 w-3.5 animate-spin motion-reduce:animate-none"
                    : "h-3.5 w-3.5"
                }
                aria-hidden="true"
              />
              {t(($) => $.tab_body.mcp_config.refresh_action)}
            </Button>
          )}
        </div>
        {!runtime ? (
          <McpNotice text={t(($) => $.tab_body.mcp_config.runtime_missing)} />
        ) : !canReadRuntime ? (
          <McpNotice text={t(($) => $.tab_body.mcp_config.runtime_forbidden)} />
        ) : runtime.status !== "online" ? (
          <McpNotice text={t(($) => $.tab_body.mcp_config.runtime_offline)} />
        ) : runtimeQuery.isLoading ? (
          <McpNotice
            loading
            text={t(($) => $.tab_body.mcp_config.runtime_discovering)}
          />
        ) : runtimeQuery.isError ? (
          <McpNotice
            text={
              runtimeQuery.error instanceof ApiError &&
              runtimeQuery.error.status === 403
                ? t(($) => $.tab_body.mcp_config.runtime_forbidden)
                : t(($) => $.tab_body.mcp_config.runtime_failed)
            }
          />
        ) : runtimeQuery.data?.mcpSupported !== true ? (
          <McpNotice
            text={t(($) => $.tab_body.mcp_config.runtime_unsupported)}
          />
        ) : runtimeQuery.data.mcpServers.length === 0 ? (
          <McpNotice text={t(($) => $.tab_body.mcp_config.runtime_empty)} />
        ) : (
          <McpServerList
            servers={runtimeQuery.data.mcpServers.map((server) => ({
              name: server.name,
              transport: server.transport || "unknown",
              enabled: server.enabled,
              source: server.source,
              overridden: effectiveNames.has(server.name),
            }))}
            disabledLabel={t(($) => $.tab_body.mcp_config.runtime_disabled_badge)}
            overriddenLabel={t(($) => $.tab_body.mcp_config.runtime_overridden_badge)}
          />
        )}
      </section>

      {!redacted && (
        <McpServerDialog
          open={editorOpen}
          server={editingServer}
          existingNames={managedNames}
          onOpenChange={setEditorOpen}
          onSave={handleSaveServer}
        />
      )}

      <AlertDialog
        open={deletingServer !== null}
        onOpenChange={(open) => !open && !deleting && setDeletingServer(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.tab_body.mcp_config.delete_dialog_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.tab_body.mcp_config.delete_dialog_description, {
                name: deletingServer?.name ?? "",
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>
              {t(($) => $.tab_body.mcp_config.dialog_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={handleDelete}
              disabled={deleting}
            >
              {deleting && (
                <Loader2
                  className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none"
                  aria-hidden="true"
                />
              )}
              {t(($) => $.tab_body.mcp_config.delete_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

type McpServerView = {
  name: string;
  transport: string;
  enabled: boolean;
  source?: string;
  overridden?: boolean;
};

/**
 * One workspace server assigned to this agent: the per-agent toggle plus the
 * affordance to take it away. The entry itself is never shown — it is
 * write-only and lives in workspace Settings.
 */
function McpWorkspaceServerRow({
  server,
  overridden,
  canEdit,
  busy,
  onToggle,
  onRemove,
}: {
  server: WorkspaceMcpServer;
  overridden: boolean;
  canEdit: boolean;
  busy: boolean;
  onToggle: (enabled: boolean) => void;
  onRemove: () => void;
}) {
  const { t } = useT("agents");
  const enabled = server.enabled !== false;
  return (
    // Same row shape as the other two lists on this tab — icon chip, name,
    // transport — so the three sources read as one inventory.
    <li className="flex items-center gap-3 p-3">
      <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
        <Server className="h-4 w-4" aria-hidden="true" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-body font-medium">{server.name}</span>
          {overridden && (
            <Badge variant="secondary">
              {t(($) => $.tab_body.mcp_config.workspace_overridden_badge)}
            </Badge>
          )}
        </div>
        <p className="text-caption uppercase text-muted-foreground">
          {server.transport || "unknown"}
        </p>
      </div>
      {canEdit && (
        <>
          <Switch
            checked={enabled}
            disabled={busy}
            onCheckedChange={onToggle}
            aria-label={t(($) => $.tab_body.mcp_config.workspace_toggle_aria, {
              name: server.name,
            })}
          />
          <Button
            variant="ghost"
            size="icon"
            disabled={busy}
            onClick={onRemove}
            aria-label={t(($) => $.tab_body.mcp_config.workspace_remove_aria, {
              name: server.name,
            })}
          >
            <Trash2 className="h-4 w-4" aria-hidden="true" />
          </Button>
        </>
      )}
      {!canEdit && !enabled && (
        <Badge variant="secondary">
          {t(($) => $.tab_body.mcp_config.workspace_disabled_badge)}
        </Badge>
      )}
    </li>
  );
}

/** Picks an unassigned workspace server to give to this agent. */
function McpWorkspaceServerPicker({
  servers,
  disabled,
  onSelect,
}: {
  servers: WorkspaceMcpServer[];
  disabled: boolean;
  onSelect: (serverId: string) => void;
}) {
  const { t } = useT("agents");
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button size="sm" variant="outline" disabled={disabled}>
            <Plus aria-hidden="true" />
            {t(($) => $.tab_body.mcp_config.workspace_add_action)}
          </Button>
        }
      />
      {/* Flush with the trigger's edges (`--anchor-width` is the trigger's own
          width), free to grow for a long name, and capped so a workspace with
          many servers scrolls instead of running off the viewport. */}
      <DropdownMenuContent
        align="end"
        className="max-h-72 min-w-(--anchor-width) max-w-[min(20rem,var(--available-width))]"
      >
        {servers.map((server) => (
          <DropdownMenuItem
            key={server.id}
            className="gap-3"
            onClick={() => onSelect(server.id)}
          >
            <span className="min-w-0 flex-1 truncate">{server.name}</span>
            <span className="shrink-0 text-caption uppercase text-muted-foreground">
              {server.transport || "unknown"}
            </span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function McpServerList({
  servers,
  disabledLabel,
  overriddenLabel,
  onEdit,
  onDelete,
  editLabel,
  deleteLabel,
}: {
  servers: McpServerView[];
  disabledLabel: string;
  overriddenLabel?: string;
  onEdit?: (server: ManagedMcpServer) => void;
  onDelete?: (server: ManagedMcpServer) => void;
  editLabel?: string;
  deleteLabel?: string;
}) {
  return (
    <ul className="divide-y rounded-lg border bg-surface-raised/40">
      {servers.map((server) => (
        <li key={server.name} className="flex items-center gap-3 p-3">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
            <Server className="h-4 w-4" aria-hidden="true" />
          </span>
          <div className="min-w-0 flex-1">
            <p className="truncate text-body font-medium">{server.name}</p>
            <p className="text-caption text-muted-foreground">
              <span className="uppercase">{server.transport}</span>
              {server.source ? ` · ${server.source}` : null}
            </p>
          </div>
          {server.overridden && overriddenLabel ? (
            <Badge variant="outline">{overriddenLabel}</Badge>
          ) : !server.enabled ? (
            <Badge variant="outline">{disabledLabel}</Badge>
          ) : null}
          {onEdit && onDelete && (
            <div className="flex items-center gap-1">
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={`${editLabel} ${server.name}`}
                onClick={() => onEdit(server as ManagedMcpServer)}
              >
                <Pencil aria-hidden="true" />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={`${deleteLabel} ${server.name}`}
                onClick={() => onDelete(server as ManagedMcpServer)}
              >
                <Trash2 aria-hidden="true" />
              </Button>
            </div>
          )}
        </li>
      ))}
    </ul>
  );
}

function McpNotice({ text, loading = false }: { text: string; loading?: boolean }) {
  return (
    <div className="flex items-center gap-2 rounded-lg border border-dashed px-4 py-6 text-caption text-muted-foreground">
      {loading ? (
        <Loader2
          className="h-4 w-4 animate-spin motion-reduce:animate-none"
          aria-hidden="true"
        />
      ) : (
        <Server className="h-4 w-4" aria-hidden="true" />
      )}
      {text}
    </div>
  );
}
