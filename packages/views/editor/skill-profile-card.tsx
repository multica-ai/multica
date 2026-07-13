"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, selectSkillAssignments } from "@multica/core/workspace/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../navigation";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { BookOpenText } from "lucide-react";
import { useT } from "../i18n";

// Note on the workspace-id lookup: CLAUDE.md prefers hooks that need
// workspace context accept wsId as a parameter rather than calling
// useWorkspaceId() internally. We call it here because SkillProfileCard
// is unconditionally rendered as a child of MentionHoverCard inside the
// workspace provider (via MentionView in the editor and readonly-content
// in chat) — the WorkspaceProvider guarantee holds at every render path.
// If this component is ever rendered outside that context, switch to the
// wsId-as-prop pattern.

interface SkillProfileCardProps {
  skillId: string;
  skillName: string;
  skillDescription?: string;
}

/**
 * SkillProfileCard — hover-card content for a skill mention.
 *
 * Shows the skill's name, description, and which agents are bound to it.
 * Each agent name links to the agent detail page. Follows the
 * `AgentProfileCard` pattern from `mention-hover-card.tsx`.
 */
export function SkillProfileCard({
  skillId,
  skillName,
  skillDescription,
}: SkillProfileCardProps) {
  const { t } = useT("editor");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { data: agents = [], isLoading: agentsLoading } = useQuery(
    agentListOptions(wsId),
  );

  const assignments = selectSkillAssignments(agents);
  const boundAgents = assignments.get(skillId) ?? [];

  const agentsLabel =
    boundAgents.length === 0
      ? t(($) => $.mention.skill_no_agents)
      : boundAgents.length === 1
        ? t(($) => $.mention.skill_bound_to)
        : t(($) => $.mention.skill_bound_to_count, { count: boundAgents.length });

  return (
    <div className="group flex flex-col gap-3 text-left">
      {/* Header — icon + skill name */}
      <div className="flex items-start gap-3">
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-violet-100 dark:bg-violet-900/30">
          <BookOpenText className="h-5 w-5 text-violet-600 dark:text-violet-400" />
        </div>
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-semibold">{skillName}</p>
          {skillDescription && (
            <p className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">
              {skillDescription}
            </p>
          )}
        </div>
      </div>

      {/* Bound agents */}
      <div className="flex flex-col gap-1.5 text-xs">
        <span className="text-muted-foreground">{agentsLabel}</span>
        {agentsLoading && boundAgents.length === 0 ? (
          <div className="flex flex-wrap gap-1">
            <Skeleton className="h-5 w-16 rounded-md" />
            <Skeleton className="h-5 w-20 rounded-md" />
          </div>
        ) : boundAgents.length > 0 ? (
          <div className="flex flex-wrap gap-1">
            {boundAgents.map((agent) => (
              <AppLink
                key={agent.id}
                href={p.agentDetail(agent.id)}
                className="rounded-md bg-muted px-1.5 py-0.5 text-[11px] font-medium text-foreground hover:bg-accent transition-colors"
              >
                {agent.name}
              </AppLink>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}
