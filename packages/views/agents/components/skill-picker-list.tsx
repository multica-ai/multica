"use client";

import { useState } from "react";
import { FileText, Search } from "lucide-react";
import type { SkillSummary } from "@multica/core/types";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

interface SkillPickerListProps {
  /** Skills to show. Callers filter (e.g. exclude already-attached
   *  skills in SkillAddDialog) before passing — this component just
   *  renders the rows. */
  skills: readonly SkillSummary[];

  /** Currently-toggled rows. Selected rows get a checked Checkbox and a
   *  subtle background; click toggles. */
  selectedIds: ReadonlySet<string>;

  /** Fires on every row click. Caller updates `selectedIds`. */
  onToggle: (skill: SkillSummary) => void;

  /** Show the search input at the top. Default true. */
  searchable?: boolean;

  /** Loading state for the skills query. */
  loading?: boolean;

  /** Caller-supplied empty / no-match copy. Falls back to generic i18n
   *  strings when omitted — the dialog and the create-form pass their
   *  own flavour-specific copy. */
  emptyMessage?: string;
  noMatchMessage?: string;

  /** Outer-wrapper className. Defaults to `w-full`; callers pass
   *  e.g. `max-w-md` to constrain width. */
  className?: string;
}

/**
 * Headless multi-select list of workspace skills. Used by both
 * SkillAddDialog (filtered to unattached skills) and SkillMultiSelect
 * (create-form selection). One surface owns row layout, the search
 * input, empty/loading states, and the shadcn Checkbox indicator, so
 * tweaks land in one place.
 *
 * Rows truncate the name + description columns inside `flex-1 min-w-0`
 * so long text doesn't push the Checkbox out of view.
 */
export function SkillPickerList({
  skills,
  selectedIds,
  onToggle,
  searchable = true,
  loading = false,
  emptyMessage,
  noMatchMessage,
  className,
}: SkillPickerListProps) {
  const { t } = useT("agents");
  const [query, setQuery] = useState("");

  const trimmedQuery = query.trim().toLowerCase();
  const filtered = trimmedQuery
    ? skills.filter((s) => {
        const name = s.name.toLowerCase();
        const description = s.description?.toLowerCase() ?? "";
        return name.includes(trimmedQuery) || description.includes(trimmedQuery);
      })
    : skills;

  const resolvedEmpty =
    emptyMessage ?? t(($) => $.create_dialog.skills_section.list_empty_default);
  const resolvedNoMatch =
    noMatchMessage ?? t(($) => $.create_dialog.skills_section.list_no_match);

  return (
    <div className={cn("w-full overflow-hidden rounded-lg border bg-card", className)}>
      {searchable && skills.length > 0 && (
        <div className="border-b p-2">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t(($) => $.create_dialog.skills_section.search_placeholder)}
              className="h-8 pl-7 text-xs"
            />
          </div>
        </div>
      )}

      <div className="max-h-64 space-y-0.5 overflow-y-auto p-1.5">
        {loading ? (
          <div className="py-6 text-center text-xs text-muted-foreground">
            {t(($) => $.create_dialog.skills_section.list_loading)}
          </div>
        ) : skills.length === 0 ? (
          <div className="py-6 text-center text-xs text-muted-foreground">{resolvedEmpty}</div>
        ) : filtered.length === 0 ? (
          <div className="py-6 text-center text-xs text-muted-foreground">{resolvedNoMatch}</div>
        ) : (
          filtered.map((skill) => {
            const isSelected = selectedIds.has(skill.id);
            return (
              <button
                key={skill.id}
                type="button"
                onClick={() => onToggle(skill)}
                aria-pressed={isSelected}
                className={cn(
                  "flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left transition-colors",
                  isSelected ? "bg-accent" : "hover:bg-accent/50",
                )}
              >
                {/* Indicator only — the wrapping <button> handles clicks,
                    so the Checkbox is non-interactive on its own. We
                    pass `checked` so the visual matches the row state. */}
                <Checkbox
                  checked={isSelected}
                  tabIndex={-1}
                  className="pointer-events-none"
                />
                <FileText className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{skill.name}</div>
                  {skill.description ? (
                    <div className="truncate text-xs text-muted-foreground">
                      {skill.description}
                    </div>
                  ) : null}
                </div>
              </button>
            );
          })
        )}
      </div>
    </div>
  );
}
