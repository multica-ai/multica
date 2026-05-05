"use client";

import { useQuery } from "@tanstack/react-query";
import type { Agent, MemberRole } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core";
import { agentRunCounts30dOptions } from "@multica/core/agents";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useT } from "@multica/i18n/react";
import { ActorAvatar } from "../common/actor-avatar";
import { AppLink } from "../navigation";

interface MemberProfileCardProps {
  // The User UUID — matches member.user_id and agent.owner_id. We accept user_id
  // (not member.id) because every existing call site passes user_id (assignee_id,
  // commenter_id, owner_id are all User UUIDs in the polymorphic actor model).
  userId: string;
}

// Mirrors AgentProfileCard's structure so the two hover surfaces feel like
// twins ("agent and human are both first-class team members"). Content is
// asymmetric on purpose: humans get identity + the AI agents they own; they
// don't get a status dot because there's no member-presence backbone today
// and we don't want to fabricate one.
export function MemberProfileCard({ userId }: MemberProfileCardProps) {
  const wsId = useWorkspaceId();
  const { data: members = [], isLoading: membersLoading } = useQuery(
    memberListOptions(wsId),
  );
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: runCounts = [] } = useQuery(agentRunCounts30dOptions(wsId));
  const t = useT("issues");

  const member = members.find((m) => m.user_id === userId);

  if (membersLoading && !member) {
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

  if (!member) {
    return (
      <div className="text-xs text-muted-foreground">{t("member_unavailable")}</div>
    );
  }

  const initials = member.name
    .split(" ")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);

  // Sort owned agents by 30-day run count (most-used first); break ties on
  // name for a stable order. Run counts come from the same workspace-wide
  // query that powers the Agents-list RUNS column — no extra fetch.
  const runCountById = new Map(runCounts.map((r) => [r.agent_id, r.run_count]));
  const ownedAgents = agents
    .filter((a) => a.owner_id === userId && !a.archived_at)
    .sort((a, b) => {
      const ra = runCountById.get(a.id) ?? 0;
      const rb = runCountById.get(b.id) ?? 0;
      if (ra !== rb) return rb - ra;
      return a.name.localeCompare(b.name);
    });

  return (
    <div className="flex flex-col gap-3 text-left">
      {/* Header */}
      <div className="flex items-start gap-3">
        <ActorAvatarBase
          name={member.name}
          initials={initials}
          avatarUrl={member.avatar_url}
          size={40}
          className="rounded-full"
        />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <p className="truncate text-sm font-semibold">{member.name}</p>
            <RoleBadge role={member.role} />
          </div>
          <p className="mt-0.5 truncate text-xs text-muted-foreground">
            {member.email}
          </p>
        </div>
      </div>

      {/* Owned agents */}
      {ownedAgents.length > 0 && <OwnedAgentsSection agents={ownedAgents} />}
    </div>
  );
}

function RoleBadge({ role }: { role: MemberRole }) {
  const t = useT("issues");
  const ROLE_LABEL: Record<MemberRole, string> = {
    owner: t("role_owner"),
    admin: t("role_admin"),
    member: t("role_member"),
  };
  return (
    <span className="rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
      {ROLE_LABEL[role]}
    </span>
  );
}

function OwnedAgentsSection({ agents }: { agents: Agent[] }) {
  // Top-2 by frequency (parent already sorted), each row links to the agent
  // detail page. The presence dot is overlaid on the avatar via ActorAvatar's
  // showStatusDot — `enableHoverCard` deliberately omitted to avoid
  // popover-in-popover nesting; the click-through covers "want to know more".
  // AppLink uses the platform navigation adapter so this works on web (Next
  // router) and desktop (react-router-dom) without per-app branching.
  const p = useWorkspacePaths();
  const t = useT("issues");
  const visible = agents.slice(0, 2);
  const overflow = agents.length - visible.length;

  return (
    <div className="flex flex-col gap-1.5 text-xs">
      <span className="text-muted-foreground">{t("owned_agents", { count: agents.length })}</span>
      <div className="flex flex-col gap-0.5">
        {visible.map((a) => (
          <AppLink
            key={a.id}
            href={p.agentDetail(a.id)}
            className="group -mx-1 flex cursor-pointer items-start gap-2 rounded-md px-1 py-1 transition-colors hover:bg-accent"
          >
            <ActorAvatar
              actorType="agent"
              actorId={a.id}
              size={20}
              showStatusDot
              className="mt-0.5 shrink-0 rounded-md"
            />
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium">{a.name}</div>
              {a.description && (
                <div className="truncate text-muted-foreground">
                  {a.description}
                </div>
              )}
            </div>
            <span
              aria-hidden
              className="mt-0.5 shrink-0 font-normal text-brand opacity-0 transition-opacity group-hover:opacity-100"
            >
              {t("detail_arrow")}
            </span>
          </AppLink>
        ))}
        {overflow > 0 && (
          <span className="text-muted-foreground">
            {overflow === 1 ? t("and_other_agents", { count: overflow }) : t("and_other_agents_plural", { count: overflow })}
          </span>
        )}
      </div>
    </div>
  );
}
