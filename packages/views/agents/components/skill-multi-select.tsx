"use client";

import { useState } from "react";
import { ChevronDown, Plus, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { SkillSummary } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { SkillPickerList } from "./skill-picker-list";

interface SkillMultiSelectProps {
  /** Currently-selected skill IDs (controlled). */
  selectedIds: ReadonlySet<string>;
  /** Replaces the selection on every toggle. */
  onChange: (next: Set<string>) => void;
}

/**
 * Multi-select wrapper for the create-agent form. Collapsed by default;
 * expands into a SkillPickerList configured for toggle behaviour
 * (click adds to / removes from the local selection set).
 *
 * Shares its visual surface with SkillAddDialog via SkillPickerList —
 * one component owns search + row rendering + indicators, so a tweak
 * to either appears identically in both flows.
 */
export function SkillMultiSelect({
  selectedIds,
  onChange,
}: SkillMultiSelectProps) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const { data: workspaceSkills = [], isLoading } = useQuery(skillListOptions(wsId));
  const [expanded, setExpanded] = useState(selectedIds.size > 0);

  const label = t(($) => $.create_dialog.skills_section.label);

  const toggle = (skill: SkillSummary) => {
    const next = new Set(selectedIds);
    if (next.has(skill.id)) next.delete(skill.id);
    else next.add(skill.id);
    onChange(next);
  };

  if (!expanded) {
    return (
      <div>
        <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {label}
        </div>
        <button
          type="button"
          onClick={() => setExpanded(true)}
          className="mt-1.5 flex w-full items-center gap-2.5 rounded-lg border bg-card px-3 py-3 text-left transition-colors hover:border-primary/40 hover:bg-accent/40"
        >
          <Plus className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1 truncate text-sm text-muted-foreground">
            {selectedIds.size > 0
              ? t(($) => $.create_dialog.skills_section.selected, {
                  count: selectedIds.size,
                })
              : t(($) => $.create_dialog.skills_section.placeholder)}
          </div>
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground/40" />
        </button>
      </div>
    );
  }

  return (
    <div>
      <div className="flex items-center justify-between">
        <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {label}
          {selectedIds.size > 0 ? (
            <span className="ml-2 text-foreground/60">({selectedIds.size})</span>
          ) : null}
        </div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => setExpanded(false)}
          className="h-6 gap-1 px-2 text-xs"
        >
          <X className="h-3 w-3" />
          {t(($) => $.create_dialog.skills_section.collapse)}
        </Button>
      </div>

      <div className="mt-1.5">
        <SkillPickerList
          skills={workspaceSkills}
          selectedIds={selectedIds}
          onToggle={toggle}
          loading={isLoading}
          emptyMessage={t(($) => $.create_dialog.skills_section.list_empty_multi)}
        />
      </div>
    </div>
  );
}
