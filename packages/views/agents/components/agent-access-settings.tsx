"use client";

import type { Agent, MemberWithUser } from "@multica/core/types";
import { usesInvocationMembers } from "@multica/core/agents";
import { usePaidFeatureAccess } from "@multica/core/billing";
import { useWorkspaceId } from "@multica/core/hooks";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
} from "../../settings/components/settings-layout";
import { useT } from "../../i18n";
import { AccessPicker } from "./inspector/access-picker";

export function AgentAccessSettings({
  agent,
  members,
  currentUserId,
  canEdit,
  onDirtyChange,
  onUpdate,
}: {
  agent: Agent;
  members: MemberWithUser[];
  currentUserId: string | null;
  /** Agent owner or workspace owner/admin. */
  canEdit: boolean;
  onDirtyChange?: (dirty: boolean) => void;
  onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const membersAccess = usePaidFeatureAccess(wsId, "agent_invocation_members", {
    hasExistingConfig: usesInvocationMembers(agent.permission_mode, agent.invocation_targets),
  });
  const restrictToolDetails = agent.tool_details_visibility === "restricted";

  return (
    <div className="space-y-8">
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
      </SettingsSection>
      <SettingsSection title={t(($) => $.access.run_logs_title)}>
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.access.restrict_tool_details_label)}
            description={t(($) => $.access.restrict_tool_details_desc)}
          >
            <Switch
              checked={restrictToolDetails}
              disabled={!canEdit}
              aria-label={t(($) => $.access.restrict_tool_details_label)}
              onCheckedChange={(checked) =>
                onUpdate(agent.id, {
                  tool_details_visibility: checked ? "restricted" : "everyone",
                })
              }
            />
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>
    </div>
  );
}
