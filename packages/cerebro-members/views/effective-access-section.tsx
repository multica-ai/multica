"use client";

// JEH-1067 (Bundle B / PR-B) — Member detail "Effective access" section.
//
// Aggregates the agents + runtimes the member can reach through their group
// allowlists, with a `via gruppe: <name>` chip for each row. For workspace
// owners and admins the section short-circuits to a single "Adgang: Alt
// (admin)" line — they bypass group gates server-side and listing every
// agent/runtime would be noise.

import { useMemo } from "react";
import { Bot, Cpu, Crown, Shield } from "lucide-react";
import { useQuery, useQueries } from "@tanstack/react-query";
import type { MemberRole } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentListOptions,
} from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import {
  groupListOptions,
  groupMembersOptions,
  groupAgentsOptions,
  groupRuntimesOptions,
} from "@multica/cerebro-groups";
import { Badge } from "@multica/ui/components/ui/badge";

export interface EffectiveAccessSectionProps {
  userId: string;
  role: MemberRole;
}

export function EffectiveAccessSection({
  userId,
  role,
}: EffectiveAccessSectionProps) {
  if (role === "owner" || role === "admin") {
    return (
      <SectionFrame>
        <h2 className="text-sm font-medium">Effective access</h2>
        <div
          className="flex items-center gap-2 text-sm text-muted-foreground"
          data-testid="effective-access-admin"
        >
          {role === "owner" ? (
            <Crown className="size-4" />
          ) : (
            <Shield className="size-4" />
          )}
          Adgang: Alt ({role})
        </div>
      </SectionFrame>
    );
  }
  return <ScopedEffectiveAccess userId={userId} />;
}

function ScopedEffectiveAccess({ userId }: { userId: string }) {
  const wsId = useWorkspaceId();
  const { data: groups = [] } = useQuery(groupListOptions(wsId));
  const { data: allAgents = [] } = useQuery(agentListOptions(wsId));
  const { data: allRuntimes = [] } = useQuery(runtimeListOptions(wsId));

  const memberRosters = useQueries({
    queries: groups.map((g) => groupMembersOptions(wsId, g.id)),
  });
  const agentRosters = useQueries({
    queries: groups.map((g) => groupAgentsOptions(wsId, g.id)),
  });
  const runtimeRosters = useQueries({
    queries: groups.map((g) => groupRuntimesOptions(wsId, g.id)),
  });

  // Walk groups, keep only those the user is in, build a source-tagged view
  // of agents and runtimes. Each agent / runtime ID maps to a list of group
  // names so we can render `via gruppe: ai-team, design` when the same
  // resource is reachable through multiple groups.
  const { agentSources, runtimeSources } = useMemo(() => {
    const agentMap = new Map<string, string[]>();
    const runtimeMap = new Map<string, string[]>();
    groups.forEach((g, idx) => {
      const members = memberRosters[idx]?.data ?? [];
      const isMember = members.some((m) => m.user_id === userId);
      if (!isMember) return;
      const agents = agentRosters[idx]?.data ?? [];
      const runtimes = runtimeRosters[idx]?.data ?? [];
      for (const a of agents) {
        const prev = agentMap.get(a.agent_id) ?? [];
        prev.push(g.name);
        agentMap.set(a.agent_id, prev);
      }
      for (const r of runtimes) {
        const prev = runtimeMap.get(r.runtime_id) ?? [];
        prev.push(g.name);
        runtimeMap.set(r.runtime_id, prev);
      }
    });
    return { agentSources: agentMap, runtimeSources: runtimeMap };
  }, [groups, memberRosters, agentRosters, runtimeRosters, userId]);

  const accessibleAgents = useMemo(
    () => allAgents.filter((a) => agentSources.has(a.id)),
    [allAgents, agentSources],
  );
  const accessibleRuntimes = useMemo(
    () => allRuntimes.filter((r) => runtimeSources.has(r.id)),
    [allRuntimes, runtimeSources],
  );

  return (
    <SectionFrame>
      <h2 className="text-sm font-medium">Effective access</h2>

      <SubSection
        title="Agents"
        icon={<Bot className="size-3.5 text-muted-foreground" />}
        empty="Ingen agenter via gruppe-medlemskab."
        testId="effective-access-agents"
      >
        {accessibleAgents.map((a) => (
          <AccessRow
            key={a.id}
            primary={a.name}
            secondary={a.description ?? null}
            sources={agentSources.get(a.id) ?? []}
            testId="effective-access-agent-row"
          />
        ))}
      </SubSection>

      <SubSection
        title="Runtimes"
        icon={<Cpu className="size-3.5 text-muted-foreground" />}
        empty="Ingen runtimes via gruppe-medlemskab."
        testId="effective-access-runtimes"
      >
        {accessibleRuntimes.map((r) => (
          <AccessRow
            key={r.id}
            primary={r.name || r.provider || "Runtime"}
            secondary={r.provider}
            sources={runtimeSources.get(r.id) ?? []}
            testId="effective-access-runtime-row"
          />
        ))}
      </SubSection>
    </SectionFrame>
  );
}

function SubSection({
  title,
  icon,
  empty,
  testId,
  children,
}: {
  title: string;
  icon: React.ReactNode;
  empty: string;
  testId: string;
  children: React.ReactNode;
}) {
  const hasChildren = Array.isArray(children)
    ? children.filter(Boolean).length > 0
    : !!children;
  return (
    <div className="space-y-2" data-testid={testId}>
      <div className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {icon}
        {title}
      </div>
      {hasChildren ? (
        <ul className="divide-y divide-border/50 rounded-md border border-border/50">
          {children}
        </ul>
      ) : (
        <p className="text-xs text-muted-foreground">{empty}</p>
      )}
    </div>
  );
}

function AccessRow({
  primary,
  secondary,
  sources,
  testId,
}: {
  primary: string;
  secondary: string | null;
  sources: string[];
  testId: string;
}) {
  return (
    <li
      className="flex items-center gap-3 px-3 py-2 text-sm"
      data-testid={testId}
    >
      <span className="flex-1 min-w-0">
        <span className="block truncate">{primary}</span>
        {secondary && (
          <span className="block truncate text-xs text-muted-foreground">
            {secondary}
          </span>
        )}
      </span>
      <span className="flex flex-wrap items-center gap-1 shrink-0">
        {sources.map((name) => (
          <Badge
            key={name}
            variant="outline"
            className="text-[10px]"
            data-testid="effective-access-source-chip"
          >
            via gruppe: {name}
          </Badge>
        ))}
      </span>
    </li>
  );
}

function SectionFrame({ children }: { children: React.ReactNode }) {
  return (
    <section
      className="rounded-md border border-border p-4 space-y-3"
      data-testid="effective-access-section"
    >
      {children}
    </section>
  );
}
