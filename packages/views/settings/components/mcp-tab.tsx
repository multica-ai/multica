"use client";

import { useMemo, useState } from "react";
import type { TFunction } from "i18next";
import {
  ArrowRight,
  Blocks,
  Loader2,
  Plug,
  Plus,
  Server,
  Unplug,
} from "lucide-react";
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
import { useFeatureEnabled } from "@multica/core/config";
import { PLUGINS_V1_FLAG } from "@multica/core/feature-flags";
import { cn } from "@multica/ui/lib/utils";
import { workspaceMcpServersOptions } from "@multica/core/workspace/queries";
import {
  useCreateWorkspaceMcpServer,
  useDeleteWorkspaceMcpServer,
  useProbeWorkspaceMcpServer,
  useUpdateWorkspaceMcpServer,
} from "@multica/core/workspace/mutations";
import { runtimeDisplayLabel, runtimeListOptions } from "@multica/core/runtimes";
import type {
  WorkspaceMcpProbeRequest,
  WorkspaceMcpServer,
} from "@multica/core/types";
import { McpServerDialog } from "../../agents/components/tabs/mcp-server-dialog";
import type { ManagedMcpServer } from "../../agents/components/tabs/mcp-config-model";
import { McpServerRow } from "../../common/mcp-server-row";
import { useT } from "../../i18n";
import {
  SettingsCard,
  SettingsReadOnlyNotice,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";
import { AppLink, useOptionalNavigation } from "../../navigation";
import { useComposioAvailable } from "./connected-apps-tab";
import { settingsHref } from "./settings-navigation";
import {
  buildRuntimeMachines,
  runtimeRowLabel,
} from "../../runtimes/components/runtime-machines";

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
  const probeServer = useProbeWorkspaceMcpServer(wsId);
  const runtimesQuery = useQuery({
    ...runtimeListOptions(wsId, "me"),
    enabled: canManage && wsId !== "",
  });
  const ownedOnline = useMemo(
    () => (runtimesQuery.data ?? []).filter((runtime) => runtime.status === "online"),
    [runtimesQuery.data],
  );
  const runtimeMachines = useMemo(
    () => buildRuntimeMachines(ownedOnline, { now: Date.now() }),
    [ownedOnline],
  );

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
  const [probeRuntimeId, setProbeRuntimeId] = useState("");
  const [probingId, setProbingId] = useState<string | null>(null);
  const [probeTarget, setProbeTarget] = useState<{
    serverId: string;
    configVersion: string;
  } | null>(null);
  const selectedRuntimeId =
    ownedOnline.length === 1
      ? ownedOnline[0]?.id ?? ""
      : ownedOnline.some((runtime) => runtime.id === probeRuntimeId)
        ? probeRuntimeId
        : "";

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

  const handleProbe = async (server: WorkspaceMcpServer) => {
    if (probingId || selectedRuntimeId === "") return;
    setProbingId(server.id);
    try {
      setProbeTarget({ serverId: server.id, configVersion: server.updated_at });
      await probeServer.mutateAsync({
        serverId: server.id,
        runtimeId: selectedRuntimeId,
      });
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.mcp.test_failed_toast),
      );
    } finally {
      setProbingId(null);
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
      title={t(($) => $.page.tabs.mcp)}
      description={t(($) => $.mcp.description)}
      scope="workspace"
    >
      {!canManage && !currentMember.isLoading ? (
        <SettingsReadOnlyNotice wsId={wsId} />
      ) : null}
      <ToolSourcesOverview />
      <SettingsSection
        title={t(($) => $.mcp.servers_title)}
        description={t(($) => $.mcp.write_only_note)}
        action={
          canManage ? (
            <div className="flex items-center gap-2">
              {ownedOnline.length > 1 ? (
                <select
                  aria-label={t(($) => $.mcp.test_choose_runtime)}
                  className="h-8 max-w-48 rounded-md border border-input bg-background px-2 text-caption"
                  value={probeRuntimeId}
                  disabled={probingId !== null}
                  onChange={(event) => setProbeRuntimeId(event.target.value)}
                >
                  <option value="">{t(($) => $.mcp.test_choose_runtime)}</option>
                  {runtimeMachines.map((machine) => (
                    <optgroup key={machine.id} label={machine.title}>
                      {machine.runtimes.map((runtime) => (
                        <option key={runtime.id} value={runtime.id}>
                          {runtimeRowLabel(runtime, machine.title)}
                        </option>
                      ))}
                    </optgroup>
                  ))}
                </select>
              ) : null}
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
            </div>
          ) : null
        }
      >
        {canManage && runtimesQuery.isFetched && ownedOnline.length === 0 ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.mcp.test_no_runtime)}</p>
        ) : null}
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
            </div>
          ) : (
            <ul className="divide-y divide-surface-border">
              {servers.map((server) => {
                const result =
                  probeTarget?.serverId === server.id &&
                  probeTarget.configVersion === server.updated_at &&
                  probingId === null
                    ? probeServer.data
                    : undefined;
                const runtime = ownedOnline.find(
                  (runtime) => runtime.id === result?.runtime_id,
                );
                return (
                  <McpServerRow
                    key={server.name}
                    name={server.name}
                    transport={server.transport}
                    meta={mcpRowMeta(
                      t,
                      server.agent_count,
                      result,
                      runtime ? runtimeDisplayLabel(runtime) : undefined,
                    )}
                    details={result ? <ProbeDetails result={result} /> : undefined}
                    extraAction={
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        aria-busy={probingId === server.id}
                        disabled={
                          probingId !== null ||
                          renamePending ||
                          ownedOnline.length === 0 ||
                          (ownedOnline.length > 1 && selectedRuntimeId === "")
                        }
                        aria-label={`${t(($) => $.mcp.test_connection)} ${server.name}`}
                        title={
                          runtimesQuery.isFetched && ownedOnline.length === 0
                            ? t(($) => $.mcp.test_no_runtime)
                            : undefined
                        }
                        onClick={() => void handleProbe(server)}
                      >
                        {probingId === server.id ? (
                          <Loader2 className="h-4 w-4 animate-spin motion-reduce:animate-none" />
                        ) : (
                          <Unplug className="h-4 w-4" />
                        )}
                        {t(($) => $.mcp.test_connection)}
                      </Button>
                    }
                    status={
                      server.enabled === false ? (
                        <Badge variant="secondary">
                          {t(($) => $.mcp.disabled_badge)}
                        </Badge>
                      ) : undefined
                    }
                    canManage={canManage}
                    actionsDisabled={renamePending || probingId !== null}
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
                );
              })}
            </ul>
          )}
        </SettingsCard>
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

function mcpRowMeta(
  t: TFunction<"settings">,
  agentCount: number | undefined,
  result: WorkspaceMcpProbeRequest | undefined,
  runtimeLabel?: string,
) {
  const agentLabel =
    agentCount === undefined
      ? null
      : agentCount > 0
        ? t(($) => $.mcp_usage.agent_count, { count: agentCount })
        : t(($) => $.mcp_usage.none);
  const probeLabel = result ? formatProbeResult(t, result, runtimeLabel) : null;
  if (!agentLabel && !probeLabel) return undefined;
  return (
    <>
      {agentLabel}
      {agentLabel && probeLabel ? " · " : null}
      {probeLabel ? <span role="status">{probeLabel}</span> : null}
    </>
  );
}

function formatProbeResult(
  t: TFunction<"settings">,
  result: WorkspaceMcpProbeRequest,
  runtimeLabel?: string,
) {
  const runtime = runtimeLabel || result.runtime_name || result.runtime_id;
  if (result.status === "completed") {
    return t(($) => $.mcp.test_ok, {
      runtime,
      detail: t(($) => $.mcp.test_tool_count, { count: result.tools?.length ?? 0 }),
    });
  }
  return t(($) => $.mcp.test_fail, {
    runtime,
    detail: result.error_code || result.error || result.status,
  });
}

function ProbeDetails({ result }: { result: WorkspaceMcpProbeRequest }) {
  const { t } = useT("settings");
  const timestamp = new Date(result.updated_at);
  return (
    <div className="mt-1 space-y-1 text-caption text-muted-foreground">
      <p>
        {Number.isNaN(timestamp.getTime()) ? null : (
          <>
            <time dateTime={result.updated_at}>{timestamp.toLocaleString()}</time>
            {" · "}
          </>
        )}
        {t(($) => $.mcp.test_elapsed, { count: result.elapsed_ms ?? 0 })}
      </p>
      {result.tools?.length ? (
        <p className="max-h-28 overflow-y-auto break-all">
          <code>{result.tools.join(", ")}</code>
        </p>
      ) : null}
    </div>
  );
}

/**
 * Three things give agents outside tools, and they differ in who owns the
 * credential and who can use it. Name all three here — the page people look
 * for when they think "MCP" — and link to the other two.
 */
function ToolSourcesOverview() {
  const { t } = useT("settings");
  const navigation = useOptionalNavigation();
  const appsAvailable = useComposioAvailable();
  const tabHref = (tab: string) =>
    navigation
      ? settingsHref(navigation.pathname, navigation.searchParams, tab)
      : `?tab=${tab}`;
  const pluginsEnabled = useFeatureEnabled(PLUGINS_V1_FLAG, false);
  const sources = [
    {
      key: "mcp",
      icon: Server,
      title: t(($) => $.page.tabs.mcp),
      description: t(($) => $.mcp_sources.mcp),
      href: null,
    },
    ...(appsAvailable
      ? [
          {
            key: "apps",
            icon: Plug,
            title: t(($) => $.page.tabs.apps),
            description: t(($) => $.mcp_sources.apps),
            href: tabHref("apps"),
          },
        ]
      : []),
    ...(pluginsEnabled
      ? [
          {
            key: "plugins",
            icon: Blocks,
            title: t(($) => $.page.tabs.plugins),
            description: t(($) => $.mcp_sources.plugins),
            href: tabHref("plugins"),
          },
        ]
      : []),
  ];
  // With nothing to compare against, the explainer would only restate the
  // page description.
  if (sources.length < 2) return null;
  return (
    <SettingsSection title={t(($) => $.mcp_sources.title)}>
      <div
        className={cn(
          "grid overflow-hidden rounded-xl border border-surface-border bg-surface",
          sources.length === 3 ? "sm:grid-cols-3" : "sm:grid-cols-2",
        )}
      >
        {sources.map((source, index) => {
          const Icon = source.icon;
          const body = (
            <>
              <span className="flex items-center gap-2 text-body font-medium">
                <Icon className="size-4 text-muted-foreground" aria-hidden="true" />
                {source.title}
                {source.href ? (
                  <ArrowRight
                    className="ml-auto size-3.5 text-muted-foreground"
                    aria-hidden="true"
                  />
                ) : (
                  <span className="ml-auto rounded-full bg-muted px-2 py-0.5 text-micro font-medium text-muted-foreground">
                    {t(($) => $.mcp_sources.current)}
                  </span>
                )}
              </span>
              <span className="mt-1.5 block text-caption leading-5 text-muted-foreground">
                {source.description}
              </span>
            </>
          );
          const className = cn(
            "block px-4 py-3.5",
            index > 0 && "border-t border-surface-border sm:border-l sm:border-t-0",
          );
          return source.href ? (
            <AppLink
              key={source.key}
              href={source.href}
              className={cn(
                className,
                "transition-colors hover:bg-surface-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring",
              )}
            >
              {body}
            </AppLink>
          ) : (
            <div key={source.key} className={cn(className, "bg-muted/30")}>
              {body}
            </div>
          );
        })}
      </div>
    </SettingsSection>
  );
}
