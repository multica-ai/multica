"use client";

import { useEffect, useState } from "react";
import type { Agent, MemberWithUser } from "@multica/core/types";
import {
  SettingsCard,
  SettingsSection,
} from "../../settings/components/settings-layout";
import { useT } from "../../i18n";
import { AccessPicker } from "./inspector/access-picker";
import { A2aInvocationPicker } from "./inspector/a2a-invocation-picker";

export function AgentAccessSettings({
  agent,
  agents,
  members,
  currentUserId,
  onDirtyChange,
  onUpdate,
}: {
  agent: Agent;
  /** Workspace agents — candidates for the A2A `specific_agents` whitelist. */
  agents: Agent[];
  members: MemberWithUser[];
  currentUserId: string | null;
  onDirtyChange?: (dirty: boolean) => void;
  onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
}) {
  const { t } = useT("agents");
  const canEditA2A = currentUserId !== null && agent.owner_id === currentUserId;
  const canEditAccess =
    currentUserId !== null && agent.owner_id === currentUserId;

  // Two pickers each report their own unsaved-draft flag through the same
  // parent callback; OR them together so one picker settling its draft can't
  // mask the other's unsaved changes.
  const [accessDirty, setAccessDirty] = useState(false);
  const [a2aDirty, setA2aDirty] = useState(false);
  useEffect(() => {
    onDirtyChange?.(accessDirty || a2aDirty);
    return () => onDirtyChange?.(false);
  }, [accessDirty, a2aDirty, onDirtyChange]);

  return (
    <>
      <SettingsSection
        title={t(($) => $.access.section_title)}
        description={t(($) => $.inspector.section_access_hint)}
      >
        <SettingsCard>
          <AccessPicker
            permissionMode={agent.permission_mode}
            invocationTargets={agent.invocation_targets}
            visibility={agent.visibility}
            members={members}
            ownerId={agent.owner_id}
            canEdit={canEditAccess}
            hasComposioAllowlist={
              (agent.composio_toolkit_allowlist ?? []).length > 0
            }
            onDirtyChange={setAccessDirty}
            onChange={(next) => onUpdate(agent.id, next)}
          />
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.a2a.section_title)}
        description={t(($) => $.a2a.section_hint)}
      >
        <SettingsCard>
          <A2aInvocationPicker
            mode={agent.a2a_invocation_mode}
            grants={agent.a2a_invocation_grants}
            agentId={agent.id}
            agents={agents}
            canEdit={canEditA2A}
            onDirtyChange={setA2aDirty}
            onChange={(next) => onUpdate(agent.id, next)}
          />
        </SettingsCard>
      </SettingsSection>
    </>
  );
}
