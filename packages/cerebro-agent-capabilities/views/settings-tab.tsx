"use client";

import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Cpu, Save, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useCurrentMember } from "@multica/core/permissions";
import { agentListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import type { Workspace } from "@multica/core/types";
import type { AgentRuntime } from "@multica/core/types/agent";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@multica/ui/components/ui/native-select";
import { csvToList, mergeCapabilitiesSettings, permissionsForAgent, readCapabilitiesConfig } from "./settings";
import type { AgentCapabilitiesConfig } from "./types";

export function AgentCapabilitiesSettingsTab() {
  const workspace = useCurrentWorkspace();
  const { role, isLoading: memberLoading } = useCurrentMember(workspace?.id ?? "");
  const canManage = role === "owner" || role === "admin";
  const qc = useQueryClient();

  const { data: agents = [] } = useQuery({
    ...agentListOptions(workspace?.id ?? ""),
    enabled: !!workspace?.id,
  });
  const { data: runtimes = [] } = useQuery({
    ...runtimeListOptions(workspace?.id ?? ""),
    enabled: !!workspace?.id,
  });

  const initialConfig = useMemo(
    () => readCapabilitiesConfig(workspace?.settings as Record<string, unknown> | undefined),
    [workspace?.settings],
  );
  const [draft, setDraft] = useState<AgentCapabilitiesConfig>(initialConfig);
  const [selectedAgentId, setSelectedAgentId] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setDraft(initialConfig);
  }, [initialConfig]);

  useEffect(() => {
    const firstAgentId = agents[0]?.id;
    if (!selectedAgentId && firstAgentId) {
      setSelectedAgentId(firstAgentId);
    }
  }, [agents, selectedAgentId]);

  if (!workspace || memberLoading) {
    return <div className="py-8 text-sm text-muted-foreground">Workspace context indlæses…</div>;
  }

  const selectedAgent = agents.find((agent) => agent.id === selectedAgentId) ?? agents[0];
  const selectedPermissions = selectedAgent
    ? permissionsForAgent(draft, selectedAgent.id)
    : permissionsForAgent(draft, "");

  const updateDraft = (patch: Partial<AgentCapabilitiesConfig>) => {
    setDraft((current) => ({ ...current, ...patch }));
  };

  const updateRuntimeOverride = (runtimeId: string, profileId: string) => {
    setDraft((current) => {
      const next = { ...current.runtime_profile_overrides };
      if (profileId) next[runtimeId] = profileId;
      else delete next[runtimeId];
      return { ...current, runtime_profile_overrides: next };
    });
  };

  const updateSelectedAgent = (patch: Partial<typeof selectedPermissions>) => {
    if (!selectedAgent) return;
    setDraft((current) => ({
      ...current,
      agent_permissions: {
        ...current.agent_permissions,
        [selectedAgent.id]: {
          ...permissionsForAgent(current, selectedAgent.id),
          ...patch,
        },
      },
    }));
  };

  const applyStrictDefault = () => {
    updateDraft({ default_profile_id: "strict" });
  };

  const save = async () => {
    if (!canManage) return;
    setSaving(true);
    try {
      const settings = mergeCapabilitiesSettings(workspace.settings as Record<string, unknown> | undefined, draft);
      const updated = await api.updateWorkspace(workspace.id, { settings });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success("Agent capabilities gemt");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Kunne ikke gemme agent capabilities");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <ShieldCheck className="h-4 w-4 text-muted-foreground" />
          <div>
            <h2 className="text-sm font-semibold">Agent capabilities</h2>
            <p className="text-xs text-muted-foreground">
              Multica håndhæver sandbox-profiler og blokerede MCP-servere ved agent start.
            </p>
          </div>
        </div>
        <Button size="sm" onClick={save} disabled={!canManage || saving}>
          <Save className="h-4 w-4" />
          {saving ? "Gemmer…" : "Gem"}
        </Button>
      </div>

      {!canManage && (
        <div className="rounded-md border bg-muted/30 p-3 text-xs text-muted-foreground">
          Kun workspace-owners og admins kan ændre agent capabilities.
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Sandbox-profiler</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-3 md:grid-cols-[1fr_auto] md:items-end">
            <Field label="Workspace default">
              <ProfileSelect
                value={draft.default_profile_id}
                profiles={draft.sandbox_profiles}
                disabled={!canManage}
                onChange={(value) => updateDraft({ default_profile_id: value })}
              />
            </Field>
            <Button variant="outline" size="sm" onClick={applyStrictDefault} disabled={!canManage}>
              Brug Strict
            </Button>
          </div>

          <div className="grid gap-3 md:grid-cols-3">
            {draft.sandbox_profiles.map((profile) => (
              <div key={profile.id} className="rounded-md border p-3">
                <div className="mb-2 flex items-center justify-between gap-2">
                  <div className="text-sm font-medium">{profile.name}</div>
                  {profile.id === draft.default_profile_id && <Badge variant="secondary">Default</Badge>}
                </div>
                <p className="text-xs text-muted-foreground">
                  {profile.network_allowlist.length > 0
                    ? profile.network_allowlist.join(", ")
                    : "Ingen ekstra netværksåbninger."}
                </p>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-sm">
            <Cpu className="h-4 w-4 text-muted-foreground" />
            Runtime overrides
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {runtimes.length === 0 ? (
            <p className="text-xs text-muted-foreground">Ingen runtimes fundet.</p>
          ) : (
            runtimes.map((runtime) => (
              <RuntimeRow
                key={runtime.id}
                runtime={runtime}
                value={draft.runtime_profile_overrides[runtime.id] ?? ""}
                profiles={draft.sandbox_profiles}
                disabled={!canManage}
                onChange={(value) => updateRuntimeOverride(runtime.id, value)}
              />
            ))
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-sm">
            <Bot className="h-4 w-4 text-muted-foreground" />
            Agent-permissions
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {agents.length === 0 ? (
            <p className="text-xs text-muted-foreground">Ingen agenter fundet.</p>
          ) : (
            <>
              <Field label="Agent">
                <NativeSelect
                  className="w-full"
                  value={selectedAgent?.id ?? ""}
                  disabled={!canManage}
                  onChange={(event) => setSelectedAgentId(event.target.value)}
                >
                  {agents.map((agent) => (
                    <NativeSelectOption key={agent.id} value={agent.id}>
                      {agent.name}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </Field>
              <div className="max-w-xl">
                <Field label="Blokerede MCP-servere">
                  <Input
                    value={selectedPermissions.mcp_denied_servers.join(", ")}
                    disabled={!canManage}
                    onChange={(event) => updateSelectedAgent({ mcp_denied_servers: csvToList(event.target.value) })}
                    placeholder="github, browser"
                  />
                </Field>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs font-medium text-muted-foreground">{label}</Label>
      {children}
    </div>
  );
}

function ProfileSelect({
  value,
  profiles,
  disabled,
  includeInherit,
  onChange,
}: {
  value: string;
  profiles: AgentCapabilitiesConfig["sandbox_profiles"];
  disabled: boolean;
  includeInherit?: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <NativeSelect className="w-full" value={value} disabled={disabled} onChange={(event) => onChange(event.target.value)}>
      {includeInherit && <NativeSelectOption value="">Inherit workspace default</NativeSelectOption>}
      {profiles.map((profile) => (
        <NativeSelectOption key={profile.id} value={profile.id}>
          {profile.name}
        </NativeSelectOption>
      ))}
    </NativeSelect>
  );
}

function RuntimeRow({
  runtime,
  value,
  profiles,
  disabled,
  onChange,
}: {
  runtime: AgentRuntime;
  value: string;
  profiles: AgentCapabilitiesConfig["sandbox_profiles"];
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <div className="grid gap-2 rounded-md border p-3 md:grid-cols-[1fr_220px] md:items-center">
      <div className="min-w-0">
        <div className="truncate text-sm font-medium">{runtime.name}</div>
        <p className="truncate text-xs text-muted-foreground">{runtime.runtime_mode} · {runtime.status}</p>
      </div>
      <ProfileSelect value={value} profiles={profiles} disabled={disabled} includeInherit onChange={onChange} />
    </div>
  );
}
