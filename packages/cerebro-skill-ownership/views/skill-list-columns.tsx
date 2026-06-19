"use client";

// FIR-1530: cerebro additions to the upstream Skills list — a "Folder" column
// (which folder a skill is filed under) and a "Changes" column (whether a
// change request is awaiting review), plus the data hook the page uses to power
// the "pending changes only" filter. The base columns live upstream in
// packages/views; this module is spliced in via a marked import on the page so
// the upstream file stays a thin patch.

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Folder, GitPullRequestArrow } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { pendingSkillChangeRequestsOptions } from "../core/queries";

const CHEVRON_COLUMN_ID = "_chevron";

// Structural shape of the upstream SkillRow we read from. Kept local (rather
// than importing the type from @multica/views) so this package does not depend
// back on views, which already imports this package — avoiding a cycle.
type SkillListRow = { skill: { id: string } };

/**
 * Set of skill IDs that currently have at least one pending change request,
 * derived from the workspace-wide pending queue. Empty when the ownership
 * feature is off. Shared across every cell + the page filter via one query.
 */
export function usePendingChangeRequestSkillIds(): Set<string> {
  const enabled = useFeatureFlag("cerebro_skill_ownership");
  const { data = [] } = useQuery({
    ...pendingSkillChangeRequestsOptions(),
    enabled,
  });
  return useMemo(() => {
    const ids = new Set<string>();
    for (const cr of data) ids.add(cr.skill_id);
    return ids;
  }, [data]);
}

function FolderCell({ name }: { name: string | null }) {
  if (!name) {
    return <span className="text-xs text-muted-foreground/40">—</span>;
  }
  return (
    <span className="inline-flex min-w-0 items-center gap-1 text-xs text-muted-foreground">
      <Folder className="h-3 w-3 shrink-0" />
      <span className="truncate">{name}</span>
    </span>
  );
}

function PendingCell({ pending }: { pending: boolean }) {
  if (!pending) {
    return <span className="text-xs text-muted-foreground/40">—</span>;
  }
  return (
    <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-warning/15 px-2 py-0.5 text-xs font-medium text-warning">
      <GitPullRequestArrow className="h-3 w-3" />
      Pending
    </span>
  );
}

export interface CerebroSkillColumnDeps {
  /** Resolve a skill's folder name (from useEntityFolderView). */
  folderName: (skillId: string) => string | null;
  /** Skill IDs with a pending change request (from the hook above). */
  pendingIds: Set<string>;
  /** Whether the skill-folders feature is on for this workspace. */
  foldersEnabled: boolean;
  /** Whether the skill-ownership feature is on for this workspace. */
  ownershipEnabled: boolean;
}

/**
 * Splice the cerebro Folder + Changes columns into the base skill columns,
 * just before the trailing chevron. Returns the base array unchanged when both
 * features are off, so the page can call this unconditionally.
 */
export function withCerebroSkillColumns<TRow extends SkillListRow>(
  base: ColumnDef<TRow>[],
  deps: CerebroSkillColumnDeps,
): ColumnDef<TRow>[] {
  const extra: ColumnDef<TRow>[] = [];

  if (deps.foldersEnabled) {
    extra.push({
      id: "cerebro_folder",
      header: "Folder",
      size: 150,
      cell: ({ row }) => (
        <FolderCell name={deps.folderName(row.original.skill.id)} />
      ),
    });
  }

  if (deps.ownershipEnabled) {
    extra.push({
      id: "cerebro_pending",
      header: "Changes",
      size: 120,
      cell: ({ row }) => (
        <PendingCell pending={deps.pendingIds.has(row.original.skill.id)} />
      ),
    });
  }

  if (extra.length === 0) return base;

  const chevronIdx = base.findIndex((c) => c.id === CHEVRON_COLUMN_ID);
  if (chevronIdx < 0) return [...base, ...extra];
  return [...base.slice(0, chevronIdx), ...extra, ...base.slice(chevronIdx)];
}
