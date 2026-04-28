"use client";

import { Cloud, Monitor, Wifi, WifiOff } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { statusConfig } from "../config";
import { useAgentsT, type AgentsDict } from "../i18n";
import { formatLastSeen } from "../../runtimes/utils";

interface AgentProfileCardProps {
  agentId: string;
}

export function AgentProfileCard({ agentId }: AgentProfileCardProps) {
  const t = useAgentsT();
  const wsId = useWorkspaceId();
  const { data: agents = [], isLoading: agentsLoading } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));

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
      <div className="text-xs text-muted-foreground">{t.profileCard.agentUnavailable}</div>
    );
  }

  const runtime = runtimes.find((r) => r.id === agent.runtime_id) ?? null;
  const owner = agent.owner_id
    ? members.find((m) => m.user_id === agent.owner_id) ?? null
    : null;
  const isArchived = !!agent.archived_at;
  const initials = agent.name
    .split(" ")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);

  return (
    <div className="flex flex-col gap-3 text-left">
      {/* Header */}
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
            {isArchived && (
              <span className="rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                {t.profileCard.archived}
              </span>
            )}
          </div>
          <AgentStatusLine agent={agent} t={t} />
        </div>
      </div>

      {/* Description */}
      {agent.description && (
        <p className="line-clamp-2 text-xs text-muted-foreground">
          {agent.description}
        </p>
      )}

      {/* Meta rows */}
      <div className="flex flex-col gap-1.5 text-xs">
        <RuntimeRow agent={agent} runtime={runtime} t={t} />
        {agent.model && <MetaRow label={t.profileCard.model} value={agent.model} mono />}
        {agent.skills.length > 0 && (
          <SkillsRow skills={agent.skills.map((s) => s.name)} label={t.profileCard.skills} />
        )}
        {owner && <MetaRow label={t.profileCard.owner} value={owner.name} />}
      </div>
    </div>
  );
}

function AgentStatusLine({ agent, t }: { agent: Agent; t: AgentsDict }) {
  const st = statusConfig[agent.status];
  return (
    <div className="mt-0.5 flex items-center gap-1.5">
      <span className={`h-1.5 w-1.5 rounded-full ${st.dot}`} />
      <span className={`text-xs ${st.color}`}>{t.status[agent.status]}</span>
    </div>
  );
}

function RuntimeRow({
  agent,
  runtime,
  t,
}: {
  agent: Agent;
  runtime: AgentRuntime | null;
  t: AgentsDict;
}) {
  const isCloud = agent.runtime_mode === "cloud";
  const Icon = isCloud ? Cloud : Monitor;
  const isOnline = runtime?.status === "online";
  // Cloud runtimes are always reachable from the user's perspective.
  const showOnline = isCloud || isOnline;

  let detail: string;
  if (isCloud) {
    detail = runtime?.name ?? t.profileCard.cloud;
  } else if (runtime) {
    detail = isOnline
      ? runtime.name
      : `${runtime.name} · ${t.profileCard.lastSeen(formatLastSeen(runtime.last_seen_at))}`;
  } else {
    detail = t.profileCard.unknownRuntime;
  }

  return (
    <div className="flex items-center gap-1.5">
      <span className="w-12 shrink-0 text-muted-foreground">{t.profileCard.runtime}</span>
      <Icon className="h-3 w-3 shrink-0 text-muted-foreground" />
      <span className="truncate" title={detail}>
        {detail}
      </span>
      {showOnline ? (
        <Wifi className="ml-auto h-3 w-3 shrink-0 text-success" />
      ) : (
        <WifiOff className="ml-auto h-3 w-3 shrink-0 text-muted-foreground" />
      )}
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

function SkillsRow({ skills, label }: { skills: string[]; label: string }) {
  const visible = skills.slice(0, 3);
  const overflow = skills.length - visible.length;
  return (
    <div className="flex items-center gap-1.5">
      <span className="w-12 shrink-0 text-muted-foreground">{label}</span>
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
