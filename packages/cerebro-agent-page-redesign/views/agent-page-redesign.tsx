"use client";

import { useState } from "react";
import {
  ArrowLeft,
  BookOpenText,
  FileText,
  ListTodo,
  MoreHorizontal,
  ShieldCheck,
  Wrench,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime, MemberWithUser } from "@multica/core/types";
import { useWorkspacePresenceMap } from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useAgentPermissions } from "@multica/core/permissions";
import { runtimeListOptions } from "@multica/core/runtimes";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { CerebroCapabilitiesTab } from "@multica/cerebro-agent-capabilities/views";
import { CerebroAgentContextTab } from "@multica/cerebro-agent-context/views";
import { CerebroToolsTab } from "@multica/cerebro-agent-tools/views";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { ActorIssuesPanel } from "@multica/views/common/actor-issues-panel";
import { AppLink } from "@multica/views/navigation";
import { IdentityRail } from "./identity-rail";
import { RedesignSkillsPanel } from "./skills-panel";

type RedesignTab =
  | "tasks"
  | "instructions"
  | "skills"
  | "tools"
  | "capabilities";

const TABS: {
  id: RedesignTab;
  label: string;
  icon: typeof ListTodo;
}[] = [
  { id: "tasks", label: "Tasks", icon: ListTodo },
  { id: "instructions", label: "Instructions", icon: FileText },
  { id: "skills", label: "Skills", icon: BookOpenText },
  { id: "tools", label: "Tools", icon: Wrench },
  { id: "capabilities", label: "Capabilities", icon: ShieldCheck },
];

/**
 * FIR-2670 — redesigned agent detail page, behind the
 * `cerebro_agent_page_redesign` flag. A single framed card with a breadcrumb
 * top bar, the redesigned identity rail, and a flat tab strip. Every tab reuses
 * the existing, shipped surfaces (Tasks = ActorIssuesPanel, Instructions =
 * versioned Agent Office, Tools/Capabilities = the tool-policy + capability
 * engine views) so the redesign is presentation-only — no new data path.
 */
export function CerebroAgentDetailPage({ agentId }: { agentId: string }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const [tab, setTab] = useState<RedesignTab>("tasks");

  const { data: agents = [], isLoading } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);

  const agent: Agent | null = agents.find((a) => a.id === agentId) ?? null;
  const perms = useAgentPermissions(agent, wsId);
  const canEdit = perms.canEdit.allowed;

  if (!agent) {
    return (
      <div className="min-h-screen bg-muted/40 p-6 md:p-10">
        <div className="mx-auto max-w-[1400px]">
          {isLoading ? (
            <Skeleton className="h-[600px] w-full rounded-2xl" />
          ) : (
            <div className="rounded-2xl border bg-background p-10 text-center text-sm text-muted-foreground">
              Agent not found, or you don't have access to it.
            </div>
          )}
        </div>
      </div>
    );
  }

  const runtime: AgentRuntime | null = agent.runtime_id
    ? runtimes.find((r) => r.id === agent.runtime_id) ?? null
    : null;
  const owner: MemberWithUser | undefined = agent.owner_id
    ? members.find((m) => m.user_id === agent.owner_id)
    : undefined;
  const presence = presenceMap.get(agent.id) ?? null;

  return (
    <div className="min-h-screen bg-muted/40 p-4 md:p-10">
      <div className="mx-auto max-w-[1400px] overflow-hidden rounded-2xl border bg-background shadow-sm">
        {/* top bar */}
        <div className="flex items-center justify-between border-b px-5 py-3.5">
          <div className="flex items-center gap-2.5 text-sm">
            <AppLink
              href={paths.agents()}
              className="inline-flex items-center gap-1.5 text-muted-foreground hover:text-foreground"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              Agents
            </AppLink>
            <span className="text-muted-foreground/50">/</span>
            <span className="font-semibold">{agent.name}</span>
          </div>
          <button
            type="button"
            aria-label="More"
            className="rounded-lg border px-2.5 py-1.5 text-muted-foreground hover:bg-accent"
          >
            <MoreHorizontal className="h-4 w-4" />
          </button>
        </div>

        <div className="flex flex-col md:flex-row md:items-stretch">
          <IdentityRail
            agent={agent}
            runtime={runtime}
            owner={owner}
            presence={presence}
          />

          <div className="flex min-w-0 flex-1 flex-col">
            {/* tab strip */}
            <div className="flex items-center gap-6 overflow-x-auto border-b px-6">
              {TABS.map(({ id, label, icon: Icon }) => {
                const active = tab === id;
                return (
                  <button
                    key={id}
                    type="button"
                    onClick={() => setTab(id)}
                    className={`flex items-center gap-1.5 whitespace-nowrap border-b-2 px-0.5 py-3.5 text-sm font-medium ${
                      active
                        ? "border-foreground text-foreground"
                        : "border-transparent text-muted-foreground hover:text-foreground"
                    }`}
                  >
                    <Icon className="h-[15px] w-[15px]" />
                    {label}
                  </button>
                );
              })}
            </div>

            {/* tab content */}
            <div className="min-h-[600px] flex-1 bg-muted/20">
              {tab === "tasks" && (
                <div className="flex min-h-[560px] flex-col p-2 md:p-4">
                  <ActorIssuesPanel actorType="agent" actorId={agent.id} />
                </div>
              )}
              {tab === "instructions" && (
                <CerebroAgentContextTab agent={agent} canEdit={canEdit} />
              )}
              {tab === "skills" && <RedesignSkillsPanel agent={agent} />}
              {tab === "tools" && (
                <div className="p-5 md:p-6">
                  <CerebroToolsTab agent={agent} canEdit={canEdit} />
                </div>
              )}
              {tab === "capabilities" && (
                <div className="p-5 md:p-6">
                  <CerebroCapabilitiesTab agent={agent} />
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
