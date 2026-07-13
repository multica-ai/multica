"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentListOptions,
  selectSkillAssignments,
  skillDetailOptions,
} from "@multica/core/workspace/queries";
import { parseFrontmatter } from "@multica/core/skills/frontmatter";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../navigation";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { BookOpenText } from "lucide-react";
import { FrontmatterCard } from "../skills/components/frontmatter-card";
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
  /** Best-effort name from the markdown label; superseded by the API name
   *  once `skillDetailOptions` resolves. Accepting both lets the hover card
   *  open before the detail query finishes without flashing a UUID. */
  skillName?: string;
  /** Optional description shorthand (no frontmatter). Subsumed by the
   *  API-supplied description once the detail query resolves. */
  skillDescription?: string;
}

/**
 * SkillProfileCard — hover-card content for a skill mention.
 *
 * Shows the skill's name, description, frontmatter (if any), and bound
 * agents. Fetches the full skill via skillDetailOptions to resolve the
 * name from the API instead of trusting the (often-UUID) hint passed in
 * props; falls back to the prop while the detail query is in flight so
 * the card opens immediately on hover.
 */
export function SkillProfileCard({
  skillId,
  skillName,
  skillDescription,
}: SkillProfileCardProps) {
  const { t } = useT("editor");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();

  // Resolve the skill detail (name + description + content/frontmatter).
  // The name returned by the API supersedes the prop hint because the prop
  // comes from the markdown label, which is the rendered display name but
  // is not always the canonical skill name from the registry.
  const { data: skillDetail } = useQuery(skillDetailOptions(wsId, skillId));
  const resolvedName = skillDetail?.name ?? skillName;
  const resolvedDescription = skillDetail?.description ?? skillDescription;
  const frontmatter = skillDetail?.content
    ? parseFrontmatter(skillDetail.content).frontmatter
    : null;

  // Bound agents — the agents cache is already prefetched by
  // createMentionSuggestion, so this should resolve without a network call.
  const { data: agents = [], isLoading: agentsLoading } = useQuery(
    agentListOptions(wsId),
  );
  const boundAgents = selectSkillAssignments(agents).get(skillId) ?? [];

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
          {resolvedName ? (
            <p className="truncate text-sm font-semibold">{resolvedName}</p>
          ) : (
            // Edge case: prop is empty AND detail query hasn't returned yet.
            // Shows a skeleton for the name rather than falling back to the
            // UUID (which is what was happening before this fix).
            <Skeleton className="mb-1 h-4 w-3/4" />
          )}
          {resolvedDescription && (
            <p className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">
              {resolvedDescription}
            </p>
          )}
        </div>
      </div>

      {/* Frontmatter — when the skill body opens with YAML, show key/value panel */}
      {frontmatter && <FrontmatterCard data={frontmatter} />}

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
