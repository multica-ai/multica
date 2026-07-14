"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, BookOpenText } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentListOptions,
  selectSkillAssignments,
  skillDetailOptions,
} from "@multica/core/workspace/queries";
import { parseFrontmatter, type SkillFrontmatter } from "@multica/core/skills/frontmatter";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../navigation";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
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

// Frontmatter keys promoted to the always-visible summary. These are the
// fields a reader of a hover preview is most likely to want without
// expanding the disclosure — the canonical surface area for "what does
// this skill DO". Anything not in this set falls into the collapsible
// "details" body so the card stays compact at 288px width.
const PROMOTED_FRONTMATTER_KEYS = new Set([
  "version",
  "repository",
  "homepage",
  "github",
  "author",
  "tags",
  "trigger",
  "when_to_use",
  "scope",
  "domain",
  "language",
]);

// Tiny helper: classify frontmatter into promoted vs additional.
function splitFrontmatter(fm: SkillFrontmatter): {
  promoted: Array<[string, string]>;
  additional: Array<[string, string]>;
} {
  const promoted: Array<[string, string]> = [];
  const additional: Array<[string, string]> = [];
  for (const [k, v] of Object.entries(fm)) {
    // Skip `description` here — we render it from the Skill's top-level
    // `description` field above (which supersedes the frontmatter copy),
    // avoiding duplicate rows.
    if (k === "description") continue;
    if (PROMOTED_FRONTMATTER_KEYS.has(k)) {
      promoted.push([k, v]);
    } else {
      additional.push([k, v]);
    }
  }
  return { promoted, additional };
}

/**
 * SkillProfileCard — hover-card content for a skill mention.
 *
 * Layout (compact, ~288px wide hover preview):
 *   [icon] name
 *          description (line-clamp-2)
 *   ─────────────
 *   <promoted metadata> (max ~3 short keys from frontmatter)
 *   <▾ show X more>     (collapsed by default when there's more)
 *   ─────────────
 *   Bound to: <agent pills>
 *   View full skill →
 *
 * The card is sized for a hover preview, not a fully-detailed view of
 * the skill's body — the dedicated skill detail page handles that. Anything
 * not picked up by the always-visible summary lives in the collapsible
 * disclosure so the card stays useful even when a skill's frontmatter has
 * 20+ keys.
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
  // The API name supersedes the prop hint because the prop comes from the
  // markdown label, which is the rendered display name but not always the
  // canonical skill name from the registry.
  const { data: skillDetail } = useQuery(skillDetailOptions(wsId, skillId));
  const resolvedName = skillDetail?.name ?? skillName;
  const resolvedDescription = skillDetail?.description ?? skillDescription;
  const frontmatter = skillDetail?.content
    ? parseFrontmatter(skillDetail.content).frontmatter
    : null;
  const { promoted: promotedFm, additional: additionalFm } = frontmatter
    ? splitFrontmatter(frontmatter)
    : { promoted: [], additional: [] };

  // Bound agents — the agents cache is already prefetched by
  // createMentionSuggestion, so this should resolve without a network call.
  const { data: agents = [], isLoading: agentsLoading } = useQuery(
    agentListOptions(wsId),
  );
  const boundAgents = selectSkillAssignments(agents).get(skillId) ?? [];

  const [expanded, setExpanded] = useState(false);
  const additionalCount = additionalFm.length;
  const showExpandControl = additionalCount > 0;

  const agentsLabel =
    boundAgents.length === 0
      ? t(($) => $.mention.skill_no_agents)
      : boundAgents.length === 1
        ? t(($) => $.mention.skill_bound_to)
        : t(($) => $.mention.skill_bound_to_count, { count: boundAgents.length });

  return (
    <div className="group flex w-full flex-col gap-2.5 overflow-hidden text-left">
      {/* Header — icon + skill name + description */}
      <div className="flex items-start gap-3 overflow-hidden">
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-violet-100 dark:bg-violet-900/30">
          <BookOpenText className="h-4 w-4 text-violet-600 dark:text-violet-400" />
        </div>
        <div className="min-w-0 flex-1 overflow-hidden">
          {resolvedName ? (
            <p className="truncate text-sm font-semibold">{resolvedName}</p>
          ) : (
            // Edge case: prop is empty AND detail query hasn't returned yet.
            // Shows a skeleton for the name rather than falling back to the
            // UUID (which is what was happening before the SkillDetail fix).
            <Skeleton className="mb-1 h-4 w-3/4" />
          )}
          {resolvedDescription && (
            <p className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">
              {resolvedDescription}
            </p>
          )}
        </div>
      </div>

      {/* Promoted frontmatter — small subset of high-signal fields. Each
          row is a single line (line-clamp-1) so the card stays compact. */}
      {promotedFm.length > 0 && (
        <dl className="flex flex-col gap-0.5 overflow-hidden border-t pt-2 text-[11px]">
          {promotedFm.slice(0, 3).map(([key, value]) => (
            <div
              key={key}
              className="flex min-w-0 items-baseline gap-1.5 overflow-hidden"
            >
              <dt className="w-14 shrink-0 truncate text-muted-foreground">{key}</dt>
              <dd className="min-w-0 flex-1 truncate text-foreground" title={value}>
                {value}
              </dd>
            </div>
          ))}
          {showExpandControl && (
            <button
              type="button"
              onClick={() => setExpanded((v) => !v)}
              className="mt-1 inline-flex items-center gap-1 self-start rounded text-[11px] font-medium text-violet-600 hover:text-violet-700 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 dark:text-violet-400 dark:hover:text-violet-300"
              aria-expanded={expanded}
            >
              {expanded ? (
                <>
                  <ChevronDown className="h-3 w-3" />
                  {t(($) => $.mention.skill_hide_details)}
                </>
              ) : (
                <>
                  <ChevronRight className="h-3 w-3" />
                  {t(($) => $.mention.skill_show_details, {
                    count: additionalCount,
                  })}
                </>
              )}
            </button>
          )}
        </dl>
      )}

      {/* Collapsed details — the rest of the frontmatter. Renders only when
          the disclosure button is expanded. We don't scroll inside a hover
          card; the skill detail page is the place for long bodies. */}
      {expanded && additionalFm.length > 0 && (
        <dl className="flex flex-col gap-0.5 overflow-hidden border-t pt-2 text-[11px]">
          {additionalFm.map(([key, value]) => (
            <div
              key={key}
              className="flex min-w-0 items-baseline gap-1.5 overflow-hidden"
            >
              <dt className="w-14 shrink-0 truncate text-muted-foreground">{key}</dt>
              <dd className="min-w-0 flex-1 truncate text-foreground" title={value}>
                {value}
              </dd>
            </div>
          ))}
        </dl>
      )}

      {/* Bound agents */}
      <div className="flex min-w-0 flex-col gap-1 overflow-hidden border-t pt-2 text-xs">
        <span className="truncate text-muted-foreground">{agentsLabel}</span>
        {agentsLoading && boundAgents.length === 0 ? (
          <div className="flex flex-wrap gap-1 overflow-hidden">
            <Skeleton className="h-5 w-16 rounded-md" />
            <Skeleton className="h-5 w-20 rounded-md" />
          </div>
        ) : boundAgents.length > 0 ? (
          <div className="flex flex-wrap gap-1 overflow-hidden">
            {boundAgents.map((agent) => (
              <AppLink
                key={agent.id}
                href={p.agentDetail(agent.id)}
                className="max-w-28 truncate rounded-md bg-muted px-1.5 py-0.5 text-[11px] font-medium text-foreground hover:bg-accent transition-colors"
              >
                {agent.name}
              </AppLink>
            ))}
          </div>
        ) : null}
      </div>

      {/* Deep link to the full skill page — hover cards are summaries by
          design, the skill detail page is the canonical source. */}
      <div className="flex justify-end overflow-hidden">
        <AppLink
          href={p.skillDetail(skillId)}
          className="shrink-0 text-[11px] font-medium text-violet-600 hover:text-violet-700 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 dark:text-violet-400 dark:hover:text-violet-300"
        >
          {t(($) => $.mention.skill_view_full)}
        </AppLink>
      </div>
    </div>
  );
}
