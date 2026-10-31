"use client";

import { useCallback, type ReactNode } from "react";
import { Globe, Lock, Users } from "@multica/ui/icons";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
} from "@multica/core/types";
import {
  type AgentPresenceDetail,
  effectiveAccessScope,
  qoderAgentId,
  summarizeActivityWindow,
  useWorkspaceActivityMap,
} from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { providerDisplayName } from "@multica/core/runtimes";
import { ActorAvatar } from "../../common/actor-avatar";
import { ObjectPanelSection, ObjectProperty } from "../../layout/object-page";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { useLocale, useT } from "../../i18n";
import { AgentPresenceIndicator } from "./agent-presence-indicator";
import { CHIP_CLASS } from "./inspector/chip";
import { ModelPicker } from "./inspector/model-picker";
import { RuntimePicker } from "./inspector/runtime-picker";
import { useExecutionControls } from "./inspector/use-execution-controls";
import { AgentPerformanceSummary, SuccessRate } from "./tabs/activity-tab";

const ACCESS_ICONS = {
  workspace: Globe,
  "specific-people": Users,
  "owner-only": Lock,
} as const;

/**
 * The agent page's right panel: what it is and where it runs, with the
 * runtime and model editable in place.
 */
export function AgentPropertiesPanel({
  agent,
  runtime,
  runtimes,
  presence,
  owner,
  members,
  currentUserId,
  canEdit,
  onUpdate,
  onEditAccess,
}: {
  agent: Agent;
  runtime: AgentRuntime | null;
  runtimes: AgentRuntime[];
  presence: AgentPresenceDetail | null;
  owner: MemberWithUser | null;
  members: MemberWithUser[];
  currentUserId: string | null;
  canEdit: boolean;
  onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
  /** Opens the full access form; access is a draft-and-save choice. */
  onEditAccess: () => void;
}) {
  const { t } = useT("agents");
  const { t: tl } = useT("layout");
  const { t: tr } = useT("runtimes");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const update = useCallback(
    (data: Record<string, unknown>) => onUpdate(agent.id, data),
    [agent.id, onUpdate],
  );
  const {
    qoderRuntime,
    canDiscoverRuntimeModels,
    runtimeOptions,
    changeModel,
    changeRuntime,
  } = useExecutionControls({ agent, runtime, runtimes, currentUserId, update });

  const { byAgent: activityMap } = useWorkspaceActivityMap(wsId);
  const week = summarizeActivityWindow(activityMap.get(agent.id), 7);

  const accessScope = effectiveAccessScope(
    agent.permission_mode,
    agent.invocation_targets,
  );
  const AccessIcon = ACCESS_ICONS[accessScope];
  const accessLabel = t(($) =>
    accessScope === "workspace"
      ? $.access.scope_labels.workspace
      : accessScope === "specific-people"
        ? $.access.scope_labels.specific_people
        : $.access.scope_labels.owner_only,
  );

  return (
    <div className="space-y-4">
      <ObjectPanelSection title={tl(($) => $.object_page.properties)}>
        <ObjectProperty label={t(($) => $.panel.status)}>
          <Value>
            <AgentPresenceIndicator detail={presence} hideCounts />
          </Value>
        </ObjectProperty>

        <ObjectProperty label={t(($) => $.inspector.prop_owner)}>
          <Value>
            {owner ? (
              <>
                <ActorAvatar actorType="member" actorId={owner.user_id} size="xs" />
                <span className="truncate">{owner.name}</span>
              </>
            ) : (
              <span className="text-muted-foreground">—</span>
            )}
          </Value>
        </ObjectProperty>

        <ObjectProperty label={t(($) => $.inspector.prop_runtime)}>
          <RuntimePicker
            value={agent.runtime_id}
            runtimes={runtimeOptions}
            members={members}
            currentUserId={currentUserId}
            canEdit={canEdit && !qoderRuntime}
            onChange={changeRuntime}
          />
        </ObjectProperty>

        <ObjectProperty label={tr(($) => $.page.columns.harness)}>
          <Value>
            {runtime ? (
              <>
                <ProviderLogo provider={runtime.provider} className="size-3.5 shrink-0" />
                <span className="truncate">{providerDisplayName(runtime.provider)}</span>
              </>
            ) : (
              <span className="text-muted-foreground">—</span>
            )}
          </Value>
        </ObjectProperty>

        {/* A Qoder agent's model lives on its agent in Qoder Cloud Agents; the
            bound agent id is what identifies it here. */}
        {qoderRuntime ? (
          <ObjectProperty label={tr(($) => $.qoder.agent)}>
            <Value>
              <span className="truncate font-mono">
                {qoderAgentId(agent.runtime_config) || "—"}
              </span>
            </Value>
          </ObjectProperty>
        ) : (
          <ObjectProperty label={t(($) => $.inspector.prop_model)}>
            <ModelPicker
              runtimeId={agent.runtime_id}
              runtimeOnline={canDiscoverRuntimeModels}
              value={agent.model ?? ""}
              canEdit={canEdit}
              onChange={changeModel}
            />
          </ObjectProperty>
        )}

        <ObjectProperty label={t(($) => $.columns.access)}>
          <button
            type="button"
            className={CHIP_CLASS}
            onClick={onEditAccess}
            aria-label={t(($) => $.panel.edit_access_aria, { value: accessLabel })}
          >
            <AccessIcon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
            <span className="truncate">{accessLabel}</span>
          </button>
        </ObjectProperty>

        <ObjectProperty label={t(($) => $.panel.runs_7d)}>
          <Value>
            <span className="tabular-nums">{week.totalRuns}</span>
            {week.totalRuns > 0 ? (
              <>
                <span className="text-faint-foreground" aria-hidden="true">·</span>
                <SuccessRate rate={week.successRate} labelled />
              </>
            ) : null}
          </Value>
        </ObjectProperty>

        <ObjectProperty label={t(($) => $.inspector.prop_created)}>
          <Value>
            <span className="truncate tabular-nums">
              {new Date(agent.created_at).toLocaleDateString(locale, {
                year: "numeric",
                month: "short",
                day: "numeric",
              })}
            </span>
          </Value>
        </ObjectProperty>
      </ObjectPanelSection>

      <ObjectPanelSection
        title={t(($) => $.tab_body.activity.section_last_30d)}
        defaultOpen={false}
      >
        <div className="py-1">
          <AgentPerformanceSummary agent={agent} />
        </div>
      </ObjectPanelSection>
    </div>
  );
}

/** A read-only value, inset to line up with the pickers' chip text. */
function Value({ children }: { children: ReactNode }) {
  return (
    <span className="flex min-w-0 items-center gap-1.5 px-1.5">{children}</span>
  );
}
