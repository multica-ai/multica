"use client";

import { useEffect, useState } from "react";
import { Link2, Star, Trash2, MessageCircle, Plug, Plus, Settings, FlaskConical } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@multica/ui/components/ui/alert-dialog";
import { toast } from "sonner";
import { useQuery, useQueryClient, useMutation } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { useAuthStore } from "@multica/core/auth";
import {
  workspaceKeys,
  channelBindingListOptions,
  channelConnectionListOptions,
  channelProviderListOptions,
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { githubInstallationsOptions } from "@multica/core/github/queries";
import { api } from "@multica/core/api";
import { useT } from "../../i18n";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import type {
  Agent,
  ChannelBinding,
  ChannelConnection,
  ChannelListenMode,
  ChannelProvider,
  PatchChannelBindingRequest,
  Project,
} from "@multica/core/types";

// lucide-react v1.x dropped brand marks (including Github). Render an inline
// SVG of the official GitHub octocat mark so the card is still recognizable.
function GitHubMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className={className} fill="currentColor">
      <path d="M12 .5C5.6.5.5 5.6.5 12c0 5.1 3.3 9.4 7.9 10.9.6.1.8-.2.8-.6v-2.2c-3.2.7-3.9-1.5-3.9-1.5-.5-1.3-1.3-1.7-1.3-1.7-1.1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1 1.8 2.7 1.3 3.4 1 .1-.8.4-1.3.8-1.6-2.6-.3-5.3-1.3-5.3-5.7 0-1.3.5-2.3 1.2-3.1-.1-.3-.5-1.5.1-3.1 0 0 1-.3 3.3 1.2.9-.3 1.9-.4 2.9-.4s2 .1 2.9.4c2.3-1.5 3.3-1.2 3.3-1.2.6 1.6.2 2.8.1 3.1.7.8 1.2 1.8 1.2 3.1 0 4.4-2.7 5.4-5.3 5.7.4.4.8 1.1.8 2.2v3.3c0 .3.2.7.8.6 4.6-1.5 7.9-5.8 7.9-10.9C23.5 5.6 18.4.5 12 .5z" />
    </svg>
  );
}

function providerLabel(value: string) {
  if (!value) return "Channel";
  return value
    .split(/[-_]/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function connectionLabel(binding: ChannelBinding, connections: Map<string, ChannelConnection>) {
  const connection = connections.get(binding.connection_id);
  return connection?.display_name || providerLabel(binding.provider);
}

type ConnectionDraft = {
  id?: string;
  provider: string;
  display_name: string;
  enabled: boolean;
  is_default: boolean;
  config: Record<string, string>;
  secret_config: Record<string, string>;
};

function draftFromConnection(connection: ChannelConnection): ConnectionDraft {
  return {
    id: connection.id,
    provider: connection.provider,
    display_name: connection.display_name,
    enabled: connection.enabled,
    is_default: connection.is_default,
    config: { ...(connection.config ?? {}) },
    secret_config: {},
  };
}

function BindingCard({
  binding,
  canManage,
  busy,
  onSetPrimary,
  onUnbind,
  connectionName,
  listenSummary,
  agentSummary,
  onEditSettings,
  labels,
}: {
  binding: ChannelBinding;
  canManage: boolean;
  busy: boolean;
  onSetPrimary: () => void;
  onUnbind: () => void;
  connectionName: string;
  listenSummary: string;
  agentSummary: string;
  onEditSettings: () => void;
  labels: {
    agent: string;
    assistantMode: string;
    edit: string;
    editTitle: string;
    primary: string;
    setPrimary: string;
    unbindTitle: string;
  };
}) {
  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <div className="flex h-8 w-8 items-center justify-center rounded-full bg-muted">
        <MessageCircle className="h-4 w-4 text-muted-foreground" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium truncate">
          {binding.external_chat_name ?? binding.external_chat_id}
        </div>
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
          <span>{connectionName}</span>
          <span>·</span>
          <span className="capitalize">{binding.chat_type}</span>
          <span>·</span>
          <span>{listenSummary}</span>
          <span>·</span>
          <span className="truncate">{labels.agent}: {agentSummary}</span>
        </div>
        <div className="mt-1 text-xs text-muted-foreground">{labels.assistantMode}</div>
      </div>
      <div className="flex flex-wrap items-center justify-end gap-2">
        {canManage && (
          <Button variant="outline" size="sm" disabled={busy} onClick={onEditSettings} title={labels.editTitle}>
            <Settings className="h-3.5 w-3.5 mr-1" />
            {labels.edit}
          </Button>
        )}
        {binding.is_primary ? (
          <Badge variant="default">
            <Star className="h-3 w-3 mr-1" />
            {labels.primary}
          </Badge>
        ) : (
          canManage && (
            <Button variant="outline" size="sm" disabled={busy} onClick={onSetPrimary}>
              {labels.setPrimary}
            </Button>
          )
        )}
        {canManage && (
          <Button variant="ghost" size="icon-sm" disabled={busy} onClick={onUnbind} title={labels.unbindTitle}>
            <Trash2 className="h-4 w-4 text-muted-foreground" />
          </Button>
        )}
      </div>
    </div>
  );
}

export function IntegrationsTab() {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const [githubConnecting, setGithubConnecting] = useState(false);

  const currentMemberForGitHub = members.find((m) => m.user_id === user?.id) ?? null;
  const canManageGitHub =
    currentMemberForGitHub?.role === "owner" || currentMemberForGitHub?.role === "admin";

  const { data: githubInstallData } = useQuery({
    ...githubInstallationsOptions(wsId),
    enabled: !!wsId && canManageGitHub,
  });
  const githubConfigured = githubInstallData?.configured ?? false;

  async function handleGitHubConnect() {
    setGithubConnecting(true);
    try {
      const resp = await api.getGitHubConnectURL(wsId);
      if (!resp.configured || !resp.url) {
        toast.error(t(($) => $.integrations.toast_not_configured));
        return;
      }
      window.open(resp.url, "_blank", "noopener");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.toast_open_failed));
    } finally {
      setGithubConnecting(false);
    }
  }
  const { data: providersData } = useQuery(channelProviderListOptions());
  const { data: connectionsData } = useQuery(channelConnectionListOptions());
  const {
    data: bindingsData,
    isLoading,
    isError: bindingsIsError,
    error: bindingsError,
  } = useQuery(channelBindingListOptions(wsId));
  const { data: bindProjectsData } = useQuery({
    queryKey: ["settings", "integrations", wsId, "projects"],
    queryFn: () => api.listProjects({ workspace_id: wsId }),
    enabled: !!wsId,
  });
  const { data: bindAgents = [] } = useQuery({
    ...agentListOptions(wsId),
    enabled: !!wsId,
  });
  const connections = connectionsData?.connections ?? [];
  const canManageConnections = connectionsData?.can_manage ?? false;
  const connectionByID = new Map(connections.map((connection) => [connection.id, connection]));
  const bindings = bindingsData?.bindings ?? [];
  const bindProjects = bindProjectsData?.projects ?? [];

  const [actionBindingId, setActionBindingId] = useState<string | null>(null);
  const [editBinding, setEditBinding] = useState<ChannelBinding | null>(null);
  const [draft, setDraft] = useState<ConnectionDraft | null>(null);
  const providers = providersData?.providers ?? [];
  const providerByID = new Map(providers.map((provider) => [provider.provider, provider]));
  const [confirmAction, setConfirmAction] = useState<{
    title: string;
    description: string;
    variant?: "destructive";
    onConfirm: () => Promise<void>;
  } | null>(null);

  const { userId, role } = useCurrentMember(wsId);
  const canManageBinding = (binding: ChannelBinding) =>
    role === "owner" || role === "admin" || binding.bound_by_user_id === userId;

  const setPrimaryMutation = useMutation({
    mutationFn: ({ bindingId }: { bindingId: string }) =>
      api.setPrimaryChannelBinding(wsId, bindingId, { is_primary: true }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.channelBindings(wsId) });
      toast.success(t(($) => $.integrations.channel.toast_primary_updated));
    },
    onError: (e: Error) => {
      toast.error(e.message || t(($) => $.integrations.channel.toast_primary_failed));
    },
  });

  const updateBindingMutation = useMutation({
    mutationFn: ({ bindingId, patch }: { bindingId: string; patch: PatchChannelBindingRequest }) =>
      api.updateChannelBinding(wsId, bindingId, patch),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.channelBindings(wsId) });
      toast.success(t(($) => $.integrations.channel.toast_binding_saved));
      setEditBinding(null);
    },
    onError: (e: Error) => {
      toast.error(e.message || t(($) => $.integrations.channel.toast_binding_failed));
    },
  });

  const saveConnectionMutation = useMutation({
    mutationFn: async (input: ConnectionDraft) => {
      const provider = providerByID.get(input.provider);
      const config: Record<string, string | null> = {};
      const secret_config: Record<string, string | null> = {};
      for (const field of provider?.config_schema ?? []) {
        if (field.secret) {
          const value = input.secret_config[field.key];
          if (value !== undefined && value !== "") secret_config[field.key] = value;
        } else {
          config[field.key] = input.config[field.key] ?? "";
        }
      }
      const payload = {
        provider: input.provider,
        display_name: input.display_name,
        enabled: input.enabled,
        is_default: input.is_default,
        config,
        secret_config,
      };
      if (input.id) return api.updateChannelConnection(input.id, payload);
      return api.createChannelConnection(payload);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.channelConnections() });
      setDraft(null);
      toast.success(t(($) => $.integrations.channel.toast_connection_saved));
    },
    onError: (e: Error) => toast.error(e.message || t(($) => $.integrations.channel.toast_connection_save_failed)),
  });

  const testConnectionMutation = useMutation({
    mutationFn: (connectionId: string) => api.testChannelConnection(connectionId),
    onSuccess: () => toast.success(t(($) => $.integrations.channel.toast_test_succeeded)),
    onError: (e: Error) => toast.error(e.message || t(($) => $.integrations.channel.toast_test_failed)),
  });

  const toggleConnection = async (connection: ChannelConnection, enabled: boolean) => {
    try {
      await api.updateChannelConnection(connection.id, {
        display_name: connection.display_name,
        enabled,
        is_default: connection.is_default,
        config: connection.config ?? {},
      });
      qc.invalidateQueries({ queryKey: workspaceKeys.channelConnections() });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.channel.toast_connection_update_failed));
    }
  };

  const handleSetPrimary = (binding: ChannelBinding) => {
    setActionBindingId(binding.id);
    setPrimaryMutation.mutate(
      { bindingId: binding.id },
      { onSettled: () => setActionBindingId(null) }
    );
  };

  const handleUnbind = (binding: ChannelBinding) => {
    setConfirmAction({
      title: t(($) => $.integrations.channel.unbind_title, {
        name: binding.external_chat_name ?? binding.external_chat_id,
      }),
      description: t(($) => $.integrations.channel.unbind_description, {
        connection: connectionLabel(binding, connectionByID),
      }),
      variant: "destructive",
      onConfirm: async () => {
        setActionBindingId(binding.id);
        try {
          await api.deleteChannelBinding(wsId, binding.id);
          qc.invalidateQueries({ queryKey: workspaceKeys.channelBindings(wsId) });
          toast.success(t(($) => $.integrations.channel.toast_binding_removed));
        } catch (e) {
          toast.error(e instanceof Error ? e.message : t(($) => $.integrations.channel.toast_binding_remove_failed));
        } finally {
          setActionBindingId(null);
          setConfirmAction(null);
        }
      },
    });
  };

  if (!wsId) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.integrations.section_title)}</h2>

        <Card>
          <CardContent className="space-y-4">
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <GitHubMark className="h-6 w-6 mt-0.5 shrink-0" />
                <div className="space-y-1">
                  <p className="text-sm font-medium">{t(($) => $.integrations.github_title)}</p>
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.integrations.github_description_prefix)}{" "}
                    <code className="rounded bg-muted px-1 py-0.5 text-[10px]">
                      {t(($) => $.integrations.github_identifier_example)}
                    </code>{" "}
                    {t(($) => $.integrations.github_description_suffix)}{" "}
                    <strong>{t(($) => $.integrations.github_description_done)}</strong>.
                  </p>
                </div>
              </div>
              {canManageGitHub && (
                <Button
                  size="sm"
                  onClick={handleGitHubConnect}
                  disabled={githubConnecting || !githubConfigured}
                  title={!githubConfigured ? t(($) => $.integrations.connect_disabled_tooltip) : undefined}
                >
                  {githubConnecting
                    ? t(($) => $.integrations.connect_opening)
                    : t(($) => $.integrations.connect_github)}
                </Button>
              )}
            </div>

            {canManageGitHub && !githubConfigured && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.integrations.not_configured)}{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[10px]">GITHUB_APP_SLUG</code>{" "}
                {t(($) => $.integrations.not_configured_and)}{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[10px]">GITHUB_WEBHOOK_SECRET</code>.
              </p>
            )}

            {!canManageGitHub && (
              <p className="text-xs text-muted-foreground">{t(($) => $.integrations.manage_hint)}</p>
            )}
          </CardContent>
        </Card>
      </section>

      <section className="space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <Plug className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-sm font-semibold">
              {t(($) => $.integrations.channel.connections_title, { count: connections.length })}
            </h2>
          </div>
          {canManageConnections ? (
            <Button
              size="sm"
              disabled={providers.length === 0}
              onClick={() => {
                const provider = providers[0];
                if (!provider) return;
                setDraft({
                  provider: provider.provider,
                  display_name: provider.display_name,
                  enabled: false,
                  is_default: false,
                  config: {},
                  secret_config: {},
                });
              }}
            >
              <Plus className="h-4 w-4" />
              {t(($) => $.integrations.channel.add)}
            </Button>
          ) : null}
        </div>
        {!canManageConnections ? (
          <p className="text-sm text-muted-foreground">{t(($) => $.integrations.channel.owner_manage_hint)}</p>
        ) : null}

        {connections.length > 0 ? (
          <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
            {connections.map((connection, i) => (
              <ConnectionRow
                key={connection.id}
                connection={connection}
                separated={i > 0}
                canManage={canManageConnections}
                onEdit={() => setDraft(draftFromConnection(connection))}
                onToggle={(enabled) => toggleConnection(connection, enabled)}
                onTest={() => testConnectionMutation.mutate(connection.id)}
                onDelete={() => {
                  setConfirmAction({
                    title: t(($) => $.integrations.channel.delete_connection_title, {
                      name: connection.display_name,
                    }),
                    description: t(($) => $.integrations.channel.delete_connection_description),
                    variant: "destructive",
                    onConfirm: async () => {
                      try {
                        await api.deleteChannelConnection(connection.id);
                        qc.invalidateQueries({ queryKey: workspaceKeys.channelConnections() });
                        qc.invalidateQueries({ queryKey: workspaceKeys.channelBindings(wsId) });
                        toast.success(t(($) => $.integrations.channel.toast_connection_deleted));
                      } catch (e) {
                        toast.error(e instanceof Error ? e.message : t(($) => $.integrations.channel.toast_connection_delete_failed));
                      } finally {
                        setConfirmAction(null);
                      }
                    },
                  });
                }}
              />
            ))}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">{t(($) => $.integrations.channel.no_connections)}</p>
        )}
      </section>

      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Link2 className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">
            {bindingsIsError
              ? t(($) => $.integrations.channel.bindings_title_plain)
              : t(($) => $.integrations.channel.bindings_title, { count: bindings.length })}
          </h2>
        </div>

        {isLoading ? (
          <p className="text-sm text-muted-foreground">{t(($) => $.integrations.channel.loading)}</p>
        ) : bindingsIsError ? (
          <p className="text-sm text-destructive">
            {bindingsError instanceof Error ? bindingsError.message : t(($) => $.integrations.channel.load_bindings_failed)}
          </p>
        ) : bindings.length > 0 ? (
          <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
            {bindings.map((b, i) => {
              const agentSummary = b.agent_id
                ? bindAgents.find((a) => a.id === b.agent_id)?.name ?? b.agent_id
                : t(($) => $.integrations.channel.agent_auto_select);
              return (
                <div key={b.id} className={i > 0 ? "border-t border-border/50" : ""}>
                  <BindingCard
                    binding={b}
                    canManage={canManageBinding(b)}
                    busy={actionBindingId === b.id || updateBindingMutation.isPending}
                    onSetPrimary={() => handleSetPrimary(b)}
                    onUnbind={() => handleUnbind(b)}
                    connectionName={connectionLabel(b, connectionByID)}
                    listenSummary={t(($) => $.integrations.channel.listen_modes[(b.listen_mode ?? "mentions") as ChannelListenMode])}
                    agentSummary={agentSummary}
                    onEditSettings={() => setEditBinding(b)}
                    labels={{
                      agent: t(($) => $.integrations.channel.agent_label),
                      assistantMode: t(($) => $.integrations.channel.assistant_mode),
                      edit: t(($) => $.integrations.channel.edit),
                      editTitle: t(($) => $.integrations.channel.edit_title),
                      primary: t(($) => $.integrations.channel.primary),
                      setPrimary: t(($) => $.integrations.channel.set_primary),
                      unbindTitle: t(($) => $.integrations.channel.unbind),
                    }}
                  />
                </div>
              );
            })}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">{t(($) => $.integrations.channel.no_bindings)}</p>
        )}
      </section>

      <BindingSettingsDialog
        binding={editBinding}
        open={!!editBinding}
        onOpenChange={(open) => {
          if (!open) setEditBinding(null);
        }}
        projects={bindProjects}
        agents={bindAgents.filter((a) => !a.archived_at)}
        busy={updateBindingMutation.isPending}
        onSave={(patch) => {
          if (!editBinding) return;
          updateBindingMutation.mutate({ bindingId: editBinding.id, patch });
        }}
      />

      {canManageConnections ? (
        <ConnectionDialog
          draft={draft}
          providers={providers}
          busy={saveConnectionMutation.isPending}
          onChange={setDraft}
          onClose={() => setDraft(null)}
          onSave={() => {
            if (draft) saveConnectionMutation.mutate(draft);
          }}
        />
      ) : null}

      <AlertDialog open={!!confirmAction} onOpenChange={(v) => { if (!v) setConfirmAction(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirmAction?.title}</AlertDialogTitle>
            <AlertDialogDescription>{confirmAction?.description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.integrations.channel.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant={confirmAction?.variant === "destructive" ? "destructive" : "default"}
              onClick={async () => {
                await confirmAction?.onConfirm();
              }}
            >
              {t(($) => $.integrations.channel.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function BindingSettingsDialog({
  binding,
  open,
  onOpenChange,
  projects,
  agents,
  busy,
  onSave,
}: {
  binding: ChannelBinding | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projects: Project[];
  agents: Agent[];
  busy: boolean;
  onSave: (patch: PatchChannelBindingRequest) => void;
}) {
  const { t } = useT("settings");
  const [defaultProjectId, setDefaultProjectId] = useState("");
  const [listenMode, setListenMode] = useState<ChannelListenMode>("mentions");
  const [agentId, setAgentId] = useState("");

  useEffect(() => {
    if (!binding) return;
    setDefaultProjectId(binding.default_project_id ?? "");
    setListenMode((binding.listen_mode as ChannelListenMode) || "mentions");
    setAgentId(binding.agent_id ?? "");
  }, [binding]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.integrations.channel.binding_settings_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.integrations.channel.binding_settings_description)}
          </DialogDescription>
        </DialogHeader>
        {binding ? (
          <div className="space-y-4">
            <div className="rounded-lg border border-border/70 bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
              {t(($) => $.integrations.channel.binding_settings_help)}
            </div>
            <div className="grid gap-2">
              <Label htmlFor="edit-binding-project">{t(($) => $.integrations.channel.default_project)}</Label>
              <NativeSelect
                id="edit-binding-project"
                value={defaultProjectId}
                disabled={busy}
                onChange={(e) => setDefaultProjectId(e.target.value)}
              >
                <option value="">{t(($) => $.integrations.channel.none)}</option>
                {projects.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.title}
                  </option>
                ))}
              </NativeSelect>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="edit-binding-listen">{t(($) => $.integrations.channel.listen_scope)}</Label>
              <NativeSelect
                id="edit-binding-listen"
                value={listenMode}
                disabled={busy}
                onChange={(e) => setListenMode(e.target.value as ChannelListenMode)}
              >
                <option value="mentions">{t(($) => $.integrations.channel.listen_modes.mentions)}</option>
                <option value="all">{t(($) => $.integrations.channel.listen_modes.all)}</option>
              </NativeSelect>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="edit-binding-agent">{t(($) => $.integrations.channel.agent_optional)}</Label>
              <NativeSelect
                id="edit-binding-agent"
                value={agentId}
                disabled={busy}
                onChange={(e) => setAgentId(e.target.value)}
              >
                <option value="">{t(($) => $.integrations.channel.agent_auto_select)}</option>
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </NativeSelect>
            </div>
            <DialogFooter>
              <Button variant="secondary" type="button" disabled={busy} onClick={() => onOpenChange(false)}>
                {t(($) => $.integrations.channel.cancel)}
              </Button>
              <Button
                type="button"
                disabled={busy}
                onClick={() =>
                  onSave({
                    default_project_id: defaultProjectId === "" ? null : defaultProjectId,
                    listen_mode: listenMode,
                    agent_id: agentId === "" ? "" : agentId,
                  })
                }
              >
                {t(($) => $.integrations.channel.save)}
              </Button>
            </DialogFooter>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function ConnectionDialog({
  draft,
  providers,
  busy,
  onChange,
  onClose,
  onSave,
}: {
  draft: ConnectionDraft | null;
  providers: ChannelProvider[];
  busy: boolean;
  onChange: (draft: ConnectionDraft | null) => void;
  onClose: () => void;
  onSave: () => void;
}) {
  const { t } = useT("settings");
  const provider = providers.find((item) => item.provider === draft?.provider);
  return (
    <Dialog open={!!draft} onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {draft?.id
              ? t(($) => $.integrations.channel.edit_connection_title)
              : t(($) => $.integrations.channel.add_connection_title)}
          </DialogTitle>
          <DialogDescription>{t(($) => $.integrations.channel.connection_description)}</DialogDescription>
        </DialogHeader>
        {draft ? (
          <div className="space-y-4">
            <div className="grid gap-2">
              <Label>{t(($) => $.integrations.channel.provider)}</Label>
              <NativeSelect
                value={draft.provider}
                disabled={!!draft.id}
                onChange={(event) => {
                  const nextProvider = providers.find((item) => item.provider === event.target.value);
                  if (!nextProvider) return;
                  onChange({
                    provider: nextProvider.provider,
                    display_name: nextProvider.display_name,
                    enabled: draft.enabled,
                    is_default: draft.is_default,
                    config: {},
                    secret_config: {},
                  });
                }}
              >
                {providers.map((item) => (
                  <option key={item.provider} value={item.provider}>{item.display_name}</option>
                ))}
              </NativeSelect>
            </div>
            <div className="grid gap-2">
              <Label>{t(($) => $.integrations.channel.display_name)}</Label>
              <Input
                value={draft.display_name}
                onChange={(event) => onChange({ ...draft, display_name: event.target.value })}
              />
            </div>
            <div className="flex items-center justify-between gap-3 rounded-lg border border-border px-3 py-2">
              <Label>{t(($) => $.integrations.channel.enabled)}</Label>
              <Switch checked={draft.enabled} onCheckedChange={(enabled) => onChange({ ...draft, enabled })} />
            </div>
            {(provider?.config_schema ?? []).map((field) => (
              <div className="grid gap-2" key={field.key}>
                <Label>{field.label || field.key}{field.required ? " *" : ""}</Label>
                <Input
                  type={field.secret ? "password" : "text"}
                  placeholder={field.secret && field.configured ? t(($) => $.integrations.channel.secret_configured) : undefined}
                  value={field.secret ? draft.secret_config[field.key] ?? "" : draft.config[field.key] ?? ""}
                  onChange={(event) => {
                    const value = event.target.value;
                    if (field.secret) {
                      onChange({ ...draft, secret_config: { ...draft.secret_config, [field.key]: value } });
                    } else {
                      onChange({ ...draft, config: { ...draft.config, [field.key]: value } });
                    }
                  }}
                />
              </div>
            ))}
          </div>
        ) : null}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>{t(($) => $.integrations.channel.cancel)}</Button>
          <Button disabled={!draft || busy} onClick={onSave}>{t(($) => $.integrations.channel.save)}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ConnectionRow({
  connection,
  separated,
  canManage,
  onEdit,
  onToggle,
  onTest,
  onDelete,
}: {
  connection: ChannelConnection;
  separated: boolean;
  canManage: boolean;
  onEdit: () => void;
  onToggle: (enabled: boolean) => void;
  onTest: () => void;
  onDelete: () => void;
}) {
  const { t } = useT("settings");
  const requiredFields = (connection.config_schema ?? [])
    .filter((field) => field.required)
    .map((field) => field.label || field.key);

  return (
    <div className={`flex items-center gap-3 px-4 py-3 ${separated ? "border-t border-border/50" : ""}`}>
      <div className="flex h-8 w-8 items-center justify-center rounded-full bg-muted">
        <MessageCircle className="h-4 w-4 text-muted-foreground" />
      </div>
      <div className="min-w-0 flex-1 space-y-0.5">
        <div className="text-sm font-medium truncate">{connection.display_name}</div>
        <div className="text-xs text-muted-foreground">{providerLabel(connection.provider)}</div>
        {requiredFields.length > 0 ? (
          <div className="text-xs text-muted-foreground">
            {t(($) => $.integrations.channel.required_config, { fields: requiredFields.join(", ") })}
          </div>
        ) : null}
      </div>
      <div className="flex items-center gap-2">
        <Badge variant={connection.enabled ? "default" : "secondary"}>
          {connection.status || (connection.enabled ? t(($) => $.integrations.channel.enabled) : t(($) => $.integrations.channel.disabled))}
        </Badge>
        {canManage ? (
          <>
            <Switch checked={connection.enabled} onCheckedChange={onToggle} />
            <Button variant="ghost" size="icon-sm" title={t(($) => $.integrations.channel.test_connection)} onClick={onTest}>
              <FlaskConical className="h-4 w-4 text-muted-foreground" />
            </Button>
            <Button variant="ghost" size="icon-sm" title={t(($) => $.integrations.channel.edit_connection)} onClick={onEdit}>
              <Settings className="h-4 w-4 text-muted-foreground" />
            </Button>
            <Button variant="ghost" size="icon-sm" title={t(($) => $.integrations.channel.delete_connection)} onClick={onDelete}>
              <Trash2 className="h-4 w-4 text-muted-foreground" />
            </Button>
          </>
        ) : null}
      </div>
    </div>
  );
}
