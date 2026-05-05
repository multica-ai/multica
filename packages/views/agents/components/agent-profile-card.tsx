"use client";

import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { useAgentPresenceDetail } from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  deriveRuntimeHealth,
  type RuntimeHealth,
} from "@multica/core/runtimes";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { AppLink } from "../../navigation";
import { HealthIcon } from "../../runtimes/components/shared";
import { availabilityConfig } from "../presence";
import { VisibilityBadge } from "./visibility-badge";

interface AgentProfileCardProps {
  agentId: string;
}

export function AgentProfileCard({ agentId }: AgentProfileCardProps) {
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { data: agents = [], isLoading: agentsLoading } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));

  const agent = agents.find((a) => a.id === agentId);

  if (agentsLoading && !agent) {
    return (
      <div className="flex items-center gap-3">
        <Skeleton className="h-10 w-10 rounded-full" />
        <div className="flex-1 space-y-1.5">
          <Skeleton className="h-4 w-28" />
          <Skeleton className="h-3 w-20" />
        </div>
      </div>
    );
  }

  if (!agent) {
    return (
      <div className="text-xs text-muted-foreground">Agent unavailable</div>
    );
  }

  const owner = agent.owner_id
    ? members.find((m) => m.user_id === agent.owner_id) ?? null
    : null;
  const runtime = runtimes.find((r) => r.id === agent.runtime_id) ?? null;
  const isArchived = !!agent.archived_at;
  const initials = agent.name
    .split(" ")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);

  return (
    // `group` enables the hover-only Detail link on the top-right —
    // it fades in only when the user is hovering the card chrome,
    // staying out of the way during a quick glance.
    <div className="group flex flex-col gap-3 text-left">
      {/* Header — avatar + name + availability on the left, "Detail →" link
          on the right (hover-only). Card stays minimal: only the 3-state
          availability dot is surfaced here; last-task state lives in the
          agents list and the agent detail page. */}
      <div className="flex items-start gap-3">
        <ActorAvatarBase
          name={agent.name}
          initials={initials}
          avatarUrl={agent.avatar_url}
          isAgent
          size={40}
          className="rounded-md"
        />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <p className="truncate text-sm font-semibold">{agent.name}</p>
            {!isArchived && <VisibilityBadge value={agent.visibility} compact />}
            {isArchived && (
              <span className="rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                Archived
              </span>
            )}
          </div>
          {!isArchived && (
            <AgentAvailabilityLine wsId={wsId} agentId={agent.id} />
          )}
        </div>
        {!isArchived && (
          <AppLink
            href={p.agentDetail(agent.id)}
            className="mr-1 mt-0.5 shrink-0 text-xs font-normal text-brand opacity-0 transition-opacity group-hover:opacity-100"
          >
            Detail →
          </AppLink>
        )}
      </div>

      {/* Description */}
      {agent.description && (
        <p className="line-clamp-2 text-xs text-muted-foreground">
          {agent.description}
        </p>
      )}

      {/* Meta rows — minimal set: runtime (where it lives), skills (what
          it knows), owner (who manages it). Model is intentionally
          omitted — power-user detail lives on the detail page. */}
      <div className="flex flex-col gap-1.5 text-xs">
        <RuntimeRow agent={agent} runtime={runtime} />
        {agent.skills.length > 0 && (
          <SkillsRow skills={agent.skills.map((s) => s.name)} />
        )}
        {owner && <MetaRow label="Owner" value={owner.name} />}
      </div>
    </div>
  );
}

// Compact availability line under the agent name — single 3-state signal
// (online / unstable / offline). Last-task state is intentionally NOT
// shown here; it belongs in the agents list and the detail page where
// there's room for icon + label + reason without crowding the popover.
function AgentAvailabilityLine({
  wsId,
  agentId,
}: {
  wsId: string | undefined;
  agentId: string;
}) {
  const detail = useAgentPresenceDetail(wsId, agentId);
  if (detail === "loading") {
    return <Skeleton className="mt-0.5 h-3 w-16" />;
  }
  const av = availabilityConfig[detail.availability];
  return (
    <div className="mt-0.5 inline-flex items-center gap-1.5">
      <span className={`h-1.5 w-1.5 rounded-full ${av.dotClass}`} />
      <span className={`text-xs ${av.textClass}`}>{av.label}</span>
    </div>
  );
}

// Compact runtime row — wifi-style health icon + runtime name. The icon
// shape (Wifi / WifiOff) plus colour reflects the live runtime health
// derived from runtime + clock; cloud runtimes always read as online.
// This is duplicate signal with the availability dot above by design —
// the dot is the agent's effective availability (which mostly tracks
// runtime health), and seeing the same wifi icon next to the runtime
// name confirms WHICH runtime is the one currently in the dot's state.
function RuntimeRow({
  agent,
  runtime,
}: {
  agent: Agent;
  runtime: AgentRuntime | null;
}) {
  const isCloud = agent.runtime_mode === "cloud";
  const health: RuntimeHealth = isCloud
    ? "online"
    : runtime
      ? deriveRuntimeHealth(runtime, Date.now())
      : "offline";
  const label = runtime?.name ?? (isCloud ? "Cloud" : "Unknown runtime");
  return (
    <div className="flex items-center gap-1.5">
      <span className="w-12 shrink-0 text-muted-foreground">Runtime</span>
      <HealthIcon health={health} className="h-3 w-3 shrink-0" />
      <span className="min-w-0 truncate" title={label}>
        {label}
      </span>
    </div>
  );
}

function MetaRow({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="flex items-center gap-1.5">
      <span className="w-12 shrink-0 text-muted-foreground">{label}</span>
      <span className={`truncate ${mono ? "font-mono text-[11px]" : ""}`} title={value}>
        {value}
      </span>
    </div>
  );
}

function SkillsRow({ skills }: { skills: string[] }) {
  const visible = skills.slice(0, 3);
  const overflow = skills.length - visible.length;
  return (
    <div className="flex items-center gap-1.5">
      <span className="w-12 shrink-0 text-muted-foreground">Skills</span>
      <div className="flex min-w-0 flex-wrap gap-1">
        {visible.map((s) => (
          <span
            key={s}
            className="rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground"
          >
            {s}
          </span>
        ))}
        {overflow > 0 && (
          <span className="rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
            +{overflow}
          </span>
        )}
      </div>
    </div>
  );
}
