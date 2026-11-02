"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronDown,
  ChevronRight,
  Folder,
  FolderOpen,
  Inbox,
  Package,
  Search,
} from "@multica/ui/icons";
import { SkillIcon } from "../lib/skill-icon";
import type { SkillSummary } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillFolderTreeOptions } from "@multica/core/skills/package-queries";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import {
  buildPickerModel,
  defaultCollapsedFolderIds,
  PICKER_UNCATEGORIZED_ID,
  searchPickerModel,
  type PickerRow,
} from "../lib/picker-tree-model";

/**
 * Fork-owned tree mode of the skill picker (OL-104), split out of the
 * upstream `agents/components/skill-picker-list.tsx` so upstream changes to
 * the flat list merge cleanly. Props mirror `SkillPickerListProps` minus
 * `tree`; keep them in step when the upstream interface grows.
 */
export interface SkillPickerTreeProps {
  /** Skills to show. Callers filter (e.g. exclude already-attached
   *  skills in SkillAddDialog) before passing — this component just
   *  renders the rows. Folders prune to this set; package candidates that
   *  were never imported show as gray, unbindable rows. */
  skills: readonly SkillSummary[];

  /** Currently-toggled rows. Selected rows get a checked Checkbox and a
   *  subtle background; click toggles. */
  selectedIds: ReadonlySet<string>;

  /** Fires on every row click. Caller updates `selectedIds`. */
  onToggle: (skill: SkillSummary) => void;

  /** Fires from a folder's tri-state checkbox with every visible selectable
   *  skill under it. */
  onToggleMany?: (skills: SkillSummary[], selected: boolean) => void;

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

function SkillRowButton({
  skill,
  selected,
  onToggle,
  depth = 0,
  pathLabel,
}: {
  skill: SkillSummary;
  selected: boolean;
  onToggle: (skill: SkillSummary) => void;
  depth?: number;
  /** Folder path shown under the name in flattened search results. */
  pathLabel?: string;
}) {
  return (
    <button
      type="button"
      onClick={() => onToggle(skill)}
      aria-pressed={selected}
      style={depth > 0 ? { paddingLeft: `${depth * 14 + 10}px` } : undefined}
      className={cn(
        "flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left transition-colors",
        selected ? "bg-accent" : "hover:bg-accent/50",
      )}
    >
      {/* Indicator only — the wrapping <button> handles clicks, so the
          Checkbox is non-interactive on its own. */}
      <Checkbox
        checked={selected}
        tabIndex={-1}
        className="pointer-events-none"
      />
      <SkillIcon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-body font-medium">{skill.name}</div>
        {pathLabel ? (
          <div className="truncate text-caption text-faint-foreground">{pathLabel}</div>
        ) : skill.description ? (
          <div className="truncate text-caption text-muted-foreground">
            {skill.description}
          </div>
        ) : null}
      </div>
    </button>
  );
}

function GrayRow({
  name,
  depth = 0,
  pathLabel,
}: {
  name: string;
  depth?: number;
  pathLabel?: string;
}) {
  const { t } = useT("skill-packages");
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <div
            aria-disabled="true"
            style={depth > 0 ? { paddingLeft: `${depth * 14 + 24}px` } : undefined}
            className="flex w-full cursor-default items-center gap-2.5 rounded-md px-2.5 py-2 text-left text-faint-foreground"
          >
            <span className="size-4 shrink-0" aria-hidden="true" />
            <Package className="h-3.5 w-3.5 shrink-0" />
            <div className="min-w-0 flex-1">
              <div className="truncate text-body">{name}</div>
              {pathLabel ? (
                <div className="truncate text-caption">{pathLabel}</div>
              ) : null}
            </div>
            <span className="shrink-0 rounded-xs bg-muted px-1 py-0.5 text-caption">
              {t(($) => $.picker.not_imported)}
            </span>
          </div>
        }
      />
      <TooltipContent side="right">{t(($) => $.picker.not_imported_hint)}</TooltipContent>
    </Tooltip>
  );
}

export function SkillPickerTree({
  skills,
  selectedIds,
  onToggle,
  onToggleMany,
  searchable = true,
  loading = false,
  emptyMessage,
  noMatchMessage,
  className,
}: SkillPickerTreeProps) {
  const { t } = useT("agents");
  const { t: tPkg } = useT("skill-packages");
  const wsId = useWorkspaceId();
  const { data: folderTree, isLoading: treeLoading } = useQuery(
    skillFolderTreeOptions(wsId),
  );
  const [query, setQuery] = useState("");
  const [overrides, setOverrides] = useState<ReadonlyMap<string, boolean>>(new Map());

  const trimmedQuery = query.trim().toLowerCase();
  const searching = trimmedQuery.length > 0;

  const defaults = useMemo(
    () => (folderTree ? defaultCollapsedFolderIds(folderTree, skills) : new Set<string>()),
    [folderTree, skills],
  );
  const collapsed = useMemo(() => {
    const set = new Set<string>();
    for (const id of defaults) if (overrides.get(id) !== false) set.add(id);
    for (const [id, value] of overrides) if (value) set.add(id);
    return set;
  }, [defaults, overrides]);

  const model = useMemo(
    () => (folderTree ? buildPickerModel(folderTree, skills, collapsed) : null),
    [folderTree, skills, collapsed],
  );
  const searchResult = useMemo(
    () =>
      folderTree && searching
        ? searchPickerModel(folderTree, skills, trimmedQuery, tPkg(($) => $.picker.uncategorized))
        : null,
    [folderTree, searching, skills, trimmedQuery, tPkg],
  );

  const toggleFolder = (folderId: string) => {
    setOverrides((prev) => {
      const next = new Map(prev);
      next.set(folderId, !collapsed.has(folderId));
      return next;
    });
  };

  const resolvedEmpty =
    emptyMessage ?? t(($) => $.create_dialog.skills_section.list_empty_default);
  const resolvedNoMatch =
    noMatchMessage ?? t(($) => $.create_dialog.skills_section.list_no_match);

  const isLoading = loading || treeLoading;
  // An unreadable tree (malformed response, request failure) must not block
  // binding: fall back to the flat list with a visible notice.
  const treeUnavailable = !isLoading && !folderTree;

  const renderRow = (row: PickerRow) => {
    if (row.kind === "skill") {
      return (
        <SkillRowButton
          key={row.skill.id}
          skill={row.skill}
          selected={selectedIds.has(row.skill.id)}
          onToggle={onToggle}
          depth={row.depth}
        />
      );
    }
    if (row.kind === "gray") {
      return (
        <GrayRow
          key={`${row.candidate.packageId} ${row.candidate.path}`}
          name={row.candidate.name}
          depth={row.depth}
        />
      );
    }
    const isCollapsed = collapsed.has(row.folderId);
    const name = row.folderId === PICKER_UNCATEGORIZED_ID
      ? tPkg(($) => $.picker.uncategorized)
      : row.name;
    const subtree = model?.subtreeSkills.get(row.folderId) ?? [];
    const selectedCount = subtree.filter((s) => selectedIds.has(s.id)).length;
    const allSelected = subtree.length > 0 && selectedCount === subtree.length;
    const someSelected = selectedCount > 0 && !allSelected;
    const ChevronIcon = isCollapsed ? ChevronRight : ChevronDown;
    const FolderIcon = row.folderId === PICKER_UNCATEGORIZED_ID
      ? Inbox
      : isCollapsed ? Folder : FolderOpen;

    return (
      <div
        key={row.folderId}
        className="flex items-center rounded-md hover:bg-accent/50"
        style={{ paddingLeft: `${row.depth * 14 + 2}px` }}
      >
        {row.expandable ? (
          <button
            type="button"
            aria-label={tPkg(($) =>
              isCollapsed ? $.tree.expand_folder : $.tree.collapse_folder, { name })}
            aria-expanded={!isCollapsed}
            onClick={() => toggleFolder(row.folderId)}
            className="shrink-0 rounded-xs p-1 text-faint-foreground hover:text-foreground"
          >
            <ChevronIcon className="h-3 w-3" />
          </button>
        ) : (
          <span className="w-5 shrink-0" aria-hidden="true" />
        )}
        {onToggleMany && subtree.length > 0 && (
          <button
            type="button"
            role="checkbox"
            aria-checked={allSelected ? true : someSelected ? "mixed" : false}
            aria-label={tPkg(($) => $.picker.toggle_folder, { name })}
            onClick={() => onToggleMany(subtree, !allSelected)}
            className="shrink-0 rounded-xs p-0.5"
          >
            <Checkbox
              checked={allSelected}
              indeterminate={someSelected}
              tabIndex={-1}
              className="pointer-events-none"
            />
          </button>
        )}
        <button
          type="button"
          onClick={() => row.expandable && toggleFolder(row.folderId)}
          className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md px-1 py-2 text-left text-caption text-muted-foreground"
        >
          <FolderIcon className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate">{name}</span>
        </button>
      </div>
    );
  };

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
              className="h-8 pl-7 text-caption"
            />
          </div>
        </div>
      )}

      <div className="max-h-64 space-y-0.5 overflow-y-auto p-1.5">
        {isLoading ? (
          <div className="py-6 text-center text-caption text-muted-foreground">
            {t(($) => $.create_dialog.skills_section.list_loading)}
          </div>
        ) : skills.length === 0 && !searching ? (
          <div className="py-6 text-center text-caption text-muted-foreground">{resolvedEmpty}</div>
        ) : treeUnavailable ? (
          <>
            <div role="status" className="px-2.5 py-1.5 text-caption text-muted-foreground">
              {tPkg(($) => $.tree.indeterminate)}
            </div>
            {skills.map((skill) => (
              <SkillRowButton
                key={skill.id}
                skill={skill}
                selected={selectedIds.has(skill.id)}
                onToggle={onToggle}
              />
            ))}
          </>
        ) : searching ? (
          searchResult && searchResult.skills.length + searchResult.gray.length > 0 ? (
            <>
              {searchResult.skills.map(({ skill, path }) => (
                <SkillRowButton
                  key={skill.id}
                  skill={skill}
                  selected={selectedIds.has(skill.id)}
                  onToggle={onToggle}
                  pathLabel={path}
                />
              ))}
              {searchResult.gray.map(({ candidate, path }) => (
                <GrayRow
                  key={`${candidate.packageId} ${candidate.path}`}
                  name={candidate.name}
                  pathLabel={path}
                />
              ))}
            </>
          ) : (
            <div className="py-6 text-center text-caption text-muted-foreground">{resolvedNoMatch}</div>
          )
        ) : (
          model?.rows.map(renderRow)
        )}
      </div>
    </div>
  );
}
