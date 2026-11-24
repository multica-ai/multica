"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { agentWorkspaceGrantsOptions, workspaceKeys, workspaceListOptions } from "@multica/core/workspace/queries";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { toast } from "sonner";
import type { Agent, MemberWithUser } from "@multica/core/types";
import { usesInvocationMembers } from "@multica/core/agents";
import { usePaidFeatureAccess } from "@multica/core/billing";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  SettingsCard,
  SettingsSection,
} from "../../settings/components/settings-layout";
import { useT } from "../../i18n";
import { AccessPicker } from "./inspector/access-picker";

export function AgentAccessSettings({
  agent,
  members,
  currentUserId,
  onDirtyChange,
  onUpdate,
}: {
  agent: Agent;
  members: MemberWithUser[];
  currentUserId: string | null;
  onDirtyChange?: (dirty: boolean) => void;
  onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const membersAccess = usePaidFeatureAccess(wsId, "agent_invocation_members", {
    hasExistingConfig: usesInvocationMembers(agent.permission_mode, agent.invocation_targets),
  });
  const owner = currentUserId !== null && agent.owner_id === currentUserId;
  const qc = useQueryClient();
  const workspaces = useQuery({ ...workspaceListOptions(), enabled: owner });
  const grants = useQuery({ ...agentWorkspaceGrantsOptions(wsId, agent.id), enabled: owner });
  const changeGrant = useMutation({
    mutationFn: async ({ workspaceId, grant }: { workspaceId: string; grant: boolean }) => {
      if (grant) await api.addAgentWorkspaceGrant(agent.id, workspaceId);
      else await api.removeAgentWorkspaceGrant(agent.id, workspaceId);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: workspaceKeys.agentWorkspaceGrants(wsId, agent.id) }),
    onError: (error) => toast.error(error instanceof Error ? error.message : t(($) => $.access.workspace_grants_error)),
  });
  const changeCreation = useMutation({
    mutationFn: async ({ workspaceId, allow }: { workspaceId: string; allow: boolean }) =>
      api.setAgentWorkspaceGrantAgentCreation(agent.id, workspaceId, allow),
    onSuccess: () => qc.invalidateQueries({ queryKey: workspaceKeys.agentWorkspaceGrants(wsId, agent.id) }),
    onError: (error) => toast.error(error instanceof Error ? error.message : t(($) => $.access.workspace_grants_error)),
  });
  const targets = (workspaces.data ?? []).filter((workspace) => workspace.id !== wsId);

  return (
    <SettingsSection
      title={t(($) => $.access.section_title)}
    >
      <SettingsCard>
        <AccessPicker
          permissionMode={agent.permission_mode}
          invocationTargets={agent.invocation_targets}
          visibility={agent.visibility}
          members={members}
          ownerId={agent.owner_id}
          canEdit={
            currentUserId !== null && agent.owner_id === currentUserId
          }
          hasComposioAllowlist={
            (agent.composio_toolkit_allowlist ?? []).length > 0
          }
          onDirtyChange={onDirtyChange}
          membersAccess={membersAccess}
          onChange={(next) => onUpdate(agent.id, next)}
        />
      </SettingsCard>
      {owner && (
        <SettingsCard>
          <div className="p-4">
            <div className="space-y-1">
              <h3 className="text-label font-medium">{t(($) => $.access.workspace_grants_title)}</h3>
              <p className="text-caption text-muted-foreground">{t(($) => $.access.workspace_grants_description)}</p>
            </div>
            {targets.length > 0 ? (
              <div className="mt-4 space-y-3">
                {targets.map((workspace) => {
                  const grant = (grants.data ?? []).find((item) => item.workspace_id === workspace.id);
                  return <div key={workspace.id} className="rounded-md border border-border p-3 space-y-3">
                    <div className="flex items-center gap-3">
                    <Checkbox
                      id={`agent-workspace-grant-${workspace.id}`}
                      checked={!!grant}
                      disabled={grants.isPending || changeGrant.isPending || changeCreation.isPending}
                      onCheckedChange={(checked) => changeGrant.mutate({ workspaceId: workspace.id, grant: checked === true })}
                    />
                    <label htmlFor={`agent-workspace-grant-${workspace.id}`} className="min-w-0 flex-1 cursor-pointer text-body">
                      {workspace.name}
                    </label>
                    </div>
                    <div className="flex items-center gap-3 pl-7">
                      <Checkbox
                        id={`agent-creation-grant-${workspace.id}`}
                        checked={grant?.can_create_agents ?? false}
                        disabled={!grant || changeGrant.isPending || changeCreation.isPending}
                        onCheckedChange={(checked) => changeCreation.mutate({ workspaceId: workspace.id, allow: checked === true })}
                      />
                      <label htmlFor={`agent-creation-grant-${workspace.id}`} className="text-caption text-muted-foreground cursor-pointer">
                        {t(($) => $.access.workspace_grants_agent_creation)}
                      </label>
                    </div>
                  </div>;
                })}
              </div>
            ) : (
              <p className="mt-3 text-caption text-muted-foreground">{t(($) => $.access.workspace_grants_empty)}</p>
            )}
          </div>
        </SettingsCard>
      )}
    </SettingsSection>
  );
}
