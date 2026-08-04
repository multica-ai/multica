"use client";

import { useQuery } from "@tanstack/react-query";
import { skillListOptions } from "@multica/core/workspace/queries";

import { useSkillChanges } from "../use-skill-changes";

interface SkillChangesNavBadgeProps {
  workspaceId: string;
}

export function SkillChangesNavBadge({
  workspaceId,
}: SkillChangesNavBadgeProps) {
  const { data: skills = [] } = useQuery(skillListOptions(workspaceId));
  const { mine } = useSkillChanges(skills);
  const count = mine.length;

  if (count === 0) return null;

  return (
    <span
      aria-label={`${count} skill ${count === 1 ? "change" : "changes"} to review`}
      data-testid="skills-sidebar-change-count"
      className="ml-auto inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-warning px-1.5 text-[10px] font-semibold leading-none tabular-nums text-warning-foreground"
    >
      {count > 99 ? "99+" : count}
    </span>
  );
}
