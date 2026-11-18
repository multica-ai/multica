"use client";

import { Sparkles } from "@multica/ui/icons";
import { useQuery } from "@tanstack/react-query";
import type { MemberWithUser } from "@multica/core/types";
import { useAgentPresenceDetail } from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { ActorAvatar } from "../../common/actor-avatar";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { useT } from "../../i18n";
import { availabilityConfig } from "../presence";
import { resolveRuntimeModelDisplayLabel } from "../runtime-model-label";

export interface AgentRuntimeHoverLabels {
  runtime: string;
  owner: string;
  model: string;
  defaultModel: string;
  online: string;
  offline: string;
}

// Shared visual body for the "agent runtime identity" hover card. Both the
// issue timeline (task-derived identity) and the runtime list (agent-config
// derived) render through this so the two surfaces stay pixel-identical:
// 40px agent avatar + name (+ optional badge) + online/offline status line, then a
// runtime / owner / model grid.
export function AgentRuntimeHoverCard({
  agentId,
  agentName,
  runtimeName,
  runtimeProvider,
  runtimeTitle,
  ownerMember,
  ownerName,
  modelLabel,
  labels,
  shareRuntime,
}: {
  agentId: string;
  agentName: string;
  runtimeName: string;
  runtimeProvider: string;
  runtimeTitle?: string;
  ownerMember: MemberWithUser | null;
  ownerName: string;
  modelLabel: string;
  labels: AgentRuntimeHoverLabels;
  shareRuntime?: boolean | null;
}) {
  const displayModelLabel = resolveRuntimeModelDisplayLabel(
    modelLabel,
    labels.defaultModel,
  );
  return (
    <div className="flex flex-col gap-3 text-left">
      <div className="flex items-start gap-3">
        <ActorAvatar
          actorType="agent"
          actorId={agentId}
          size="xl"
          profileLink={false}
        />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-1.5">
            <p className="truncate text-body font-semibold">{agentName}</p>
            <SmartRuntimeAgentBadge shareRuntime={shareRuntime} />
          </div>
          <AgentAvailabilityLine agentId={agentId} />
        </div>
      </div>
      <div className="grid grid-cols-[3.25rem_minmax(0,1fr)] gap-x-2 gap-y-1.5 text-caption">
        <span className="text-muted-foreground">{labels.runtime}</span>
        <span
          className="flex min-w-0 items-center gap-1.5"
          title={runtimeTitle}
        >
          <ProviderLogo
            provider={runtimeProvider}
            className="h-3.5 w-3.5 shrink-0"
          />
          <span className="min-w-0 truncate">{runtimeName}</span>
        </span>
        <span className="text-muted-foreground">{labels.owner}</span>
        <span className="flex min-w-0 items-center gap-1.5">
          {ownerMember && (
            <ActorAvatar
              actorType="member"
              actorId={ownerMember.user_id}
              size="xs"
              profileLink={false}
            />
          )}
          <span className="min-w-0 truncate">{ownerName}</span>
        </span>
        {displayModelLabel && (
          <>
            <span className="text-muted-foreground">{labels.model}</span>
            <span className="min-w-0 truncate font-mono">{displayModelLabel}</span>
          </>
        )}
      </div>
    </div>
  );
}

// Same availability signal as AgentStatusDot on the trigger avatar — derived
// from runtime health via useAgentPresenceDetail, not runtime.status.
function AgentAvailabilityLine({ agentId }: { agentId: string }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const detail = useAgentPresenceDetail(wsId, agentId);
  if (detail === "loading") {
    return <Skeleton className="mt-0.5 h-3 w-16" />;
  }
  const av = availabilityConfig[detail.availability];
  return (
    <div className="mt-0.5 inline-flex items-center gap-1.5">
      <span className={`h-1.5 w-1.5 rounded-full ${av.dotClass}`} />
      <span className={`text-caption ${av.textClass}`}>
        {t(($) => $.availability[detail.availability])}
      </span>
    </div>
  );
}

// The "smart runtime" marker rendered after an agent name when the agent
// runs on the caller's own runtime (share_runtime === false). Sparkles is
// the same glyph the create-agent dialog uses for its smart-runtime option,
// so the icon reads consistently across surfaces.
export function SmartRuntimeBadge({ label }: { label: string }) {
  return (
    <span
      className="inline-flex shrink-0 items-center gap-0.5 rounded bg-info/10 px-1 text-micro font-medium text-info"
      title={label}
    >
      <Sparkles className="h-2.5 w-2.5" />
      {label}
    </span>
  );
}

// Convenience wrapper that resolves the translated label and only renders for
// non-shared (smart) runtimes. Drop it next to any
// agent name so the smart marker reads identically across every surface.
export function SmartRuntimeAgentBadge({
  shareRuntime,
  archived,
}: {
  shareRuntime?: boolean | null;
  archived?: boolean;
}) {
  const { t } = useT("agents");
  if (shareRuntime !== false || archived) return null;
  return <SmartRuntimeBadge label={t(($) => $.runtime_policy.smart)} />;
}

// Same badge for surfaces that only carry an agent id — reads the
// workspace-wide agent cache (already loaded by most pages, so no extra
// request) to look up the share_runtime flag.
export function SmartRuntimeAgentBadgeById({ agentId }: { agentId: string }) {
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery({
    ...agentListOptions(wsId),
    enabled: !!wsId,
  });
  const agent = agents.find((a) => a.id === agentId);
  if (!agent) return null;
  return (
    <SmartRuntimeAgentBadge
      shareRuntime={agent.share_runtime}
      archived={!!agent.archived_at}
    />
  );
}
