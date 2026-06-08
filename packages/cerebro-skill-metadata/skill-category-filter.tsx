"use client";

import { useMemo } from "react";
import type { SkillSummary } from "@multica/core/types";
import "@multica/cerebro-types/augment";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";

interface SkillCategoryFilterProps {
  skills: SkillSummary[];
  category: string | null;
  onCategoryChange: (category: string | null) => void;
}

export const UNCATEGORIZED_SKILL_CATEGORY = "__uncategorized__";
export const UNCATEGORIZED_SKILL_CATEGORY_LABEL = "Ukategoriseret";

export function skillCategoryKey(skill: SkillSummary): string {
  const category = skill.metadata?.category?.trim();
  return category || UNCATEGORIZED_SKILL_CATEGORY;
}

function skillCategoryLabel(category: string): string {
  return category === UNCATEGORIZED_SKILL_CATEGORY
    ? UNCATEGORIZED_SKILL_CATEGORY_LABEL
    : category;
}

export function SkillCategoryFilter({
  skills,
  category,
  onCategoryChange,
}: SkillCategoryFilterProps) {
  const categories = useMemo(() => {
    const seen = new Set<string>();
    for (const s of skills) {
      seen.add(skillCategoryKey(s));
    }
    return Array.from(seen).sort((a, b) =>
      skillCategoryLabel(a).localeCompare(skillCategoryLabel(b)),
    );
  }, [skills]);

  if (categories.length === 0) return null;

  return (
    <div className="-mx-1 flex flex-wrap gap-1.5 overflow-x-auto px-1 pb-1 sm:mx-0 sm:overflow-visible sm:px-0 sm:pb-0">
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant="outline"
              size="sm"
              className={
                category === null
                  ? "shrink-0 bg-accent text-accent-foreground hover:bg-accent/80"
                  : "shrink-0 text-muted-foreground"
              }
              onClick={() => onCategoryChange(null)}
            >
              Alle kategorier
            </Button>
          }
        />
        <TooltipContent side="bottom">Vis skills fra alle kategorier</TooltipContent>
      </Tooltip>
      {categories.map((cat) => {
        const label = skillCategoryLabel(cat);
        const count = skills.filter((s) => skillCategoryKey(s) === cat).length;
        return (
          <Tooltip key={cat}>
            <TooltipTrigger
              render={
                <Button
                  variant="outline"
                  size="sm"
                  className={
                    category === cat
                      ? "shrink-0 bg-accent text-accent-foreground hover:bg-accent/80"
                      : "shrink-0 text-muted-foreground"
                  }
                  onClick={() => onCategoryChange(category === cat ? null : cat)}
                >
                  {label}
                  <span className="ml-1 font-mono text-xs opacity-60">{count}</span>
                </Button>
              }
            />
            <TooltipContent side="bottom">Vis {count} skills i kategorien {label}</TooltipContent>
          </Tooltip>
        );
      })}
    </div>
  );
}
