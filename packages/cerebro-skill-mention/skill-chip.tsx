"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { skillListOptions } from "@multica/core/workspace/queries";
import { Sparkles } from "lucide-react";

/**
 * SkillMentionChip — visual + interactive chip for `mention://skill/<id>`.
 *
 * Click navigates to the skill detail page in the current tab.
 * Cmd/Ctrl/Shift-click opens it in a new tab (matching IssueMention behaviour).
 *
 * Reuses the same shell classes as IssueChip so the chip sits in a 14px line
 * box and looks consistent next to issue mentions in mixed content.
 */
const BASE_CLASS =
  "skill-mention inline-flex items-center gap-1.5 rounded-md border mx-0.5 px-2 py-0.5 text-xs max-w-72";

export interface SkillMentionChipProps {
  skillId: string;
  /** Shown when the skill can't be resolved (deleted, permissions, …). */
  fallbackLabel?: string;
  className?: string;
}

export function SkillMentionChip({
  skillId,
  fallbackLabel,
  className,
}: SkillMentionChipProps) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { push, openInNewTab } = useNavigation();

  const { data: skills = [] } = useQuery(skillListOptions(wsId));
  const skill = skills.find((s) => s.id === skillId);

  const skillPath = paths.skillDetail(skillId);
  const cls = className ? `${BASE_CLASS} ${className}` : BASE_CLASS;

  const handleClick = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey || e.shiftKey) {
      if (openInNewTab) {
        openInNewTab(skillPath, skill?.name ?? fallbackLabel);
      } else {
        window.open(skillPath, "_blank", "noopener,noreferrer");
      }
      return;
    }
    push(skillPath);
  };

  const label = skill?.name ?? fallbackLabel ?? skillId.slice(0, 8);

  return (
    <a
      href={skillPath}
      onClick={handleClick}
      className={`${cls} cursor-pointer hover:bg-accent transition-colors no-underline`}
    >
      <Sparkles className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      <span className="text-foreground truncate">{label}</span>
    </a>
  );
}
