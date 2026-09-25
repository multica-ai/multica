"use client";

import { AppLink } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { ProjectChip } from "./project-chip";

interface ProjectMentionCardProps {
  projectId: string;
  /** Fallback text when the project is not resolvable (e.g. its title). */
  fallbackLabel?: string;
}

/**
 * Navigable chip — wraps ProjectChip in an AppLink pointing at the project's
 * detail page. Counterpart of IssueMentionCard, and deliberately shaped like
 * it: `ProjectChip` is presentational, so whatever wraps it decides that a
 * mention is a link, and that decision belongs in one place.
 *
 * It was in none until now — the wrapper lived inline in the readonly
 * renderer — and the cost showed up as gaps that landed on project mentions
 * alone: `.project-mention` never got the CSS rule cancelling generic link
 * chrome, and the link hover card never learned to skip it. A named component
 * is where the next such rule has somewhere obvious to go.
 *
 * Rendered as a real anchor: a mention must be reachable by Tab, activatable by
 * Enter, and expose a URL to copy or open in a new tab. A `<span onClick>`
 * looks identical and works for a mouse, which is exactly why that regression
 * is easy to miss (see project-mention-a11y.test.tsx).
 *
 * AppLink owns plain-click, modifier-click and the desktop new-tab adapter, so
 * none of that is reimplemented here. Unlike an issue there is no "open in new
 * tab" preference to honour — that setting is scoped to issue links.
 */
export function ProjectMentionCard({
  projectId,
  fallbackLabel,
}: ProjectMentionCardProps) {
  const p = useWorkspacePaths();
  return (
    <AppLink
      href={p.projectDetail(projectId)}
      newTabTitle={fallbackLabel}
      className="project-mention inline-flex"
    >
      <ProjectChip
        projectId={projectId}
        fallbackLabel={fallbackLabel}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </AppLink>
  );
}
