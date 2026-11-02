"use client";

import { useMemo } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Unlink } from "@multica/ui/icons";
import { isIssueStatusCategory } from "@multica/core/issue-statuses";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import type { IssuePrerequisite } from "@multica/core/api";
import type { Issue } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { StatusIcon } from "./status-icon";
import { useStatusLabel } from "../utils/status-label";

/**
 * Display data the dependency projection deliberately does not carry
 * (OL-38: the projection stays compact): each prerequisite's project and the
 * identifiers of the ancestors an inherited edge arrives through. Both come
 * from the workspace-scoped detail cache — usually already warm — with the
 * project list resolving ids to titles. Unknown stays unrendered, never
 * guessed.
 */
export function usePrerequisiteDisplay(wsId: string, items: IssuePrerequisite[]) {
  const ids = useMemo(() => {
    const set = new Set<string>();
    for (const p of items) {
      set.add(p.issueId);
      for (const ancestorId of p.inheritedFrom) set.add(ancestorId);
    }
    return [...set].sort();
  }, [items]);

  const details = useQueries({
    queries: ids.map((id) => issueDetailOptions(wsId, id)),
  });
  const byId = useMemo(() => {
    const map = new Map<string, Issue>();
    details.forEach((result, i) => {
      const id = ids[i];
      if (id && result.data) map.set(id, result.data);
    });
    return map;
  }, [details, ids]);

  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const projectTitleOf = useMemo(() => {
    const titles = new Map(projects.map((p) => [p.id, p.title]));
    return (issueId: string): string | undefined => {
      const projectId = byId.get(issueId)?.project_id;
      return projectId ? titles.get(projectId) : undefined;
    };
  }, [byId, projects]);

  /** Short "PREFIX-123" label for a prerequisite or its source ancestor. */
  const identifierOf = (issueId: string): string | undefined =>
    byId.get(issueId)?.identifier;

  return { projectTitleOf, identifierOf };
}

function PrerequisiteRow({
  wsId,
  prerequisite,
  projectTitle,
  ancestorLabel,
  onOpen,
  onRemove,
  removeDisabled,
  onEditSource,
}: {
  wsId: string;
  prerequisite: IssuePrerequisite;
  projectTitle?: string;
  /** Set only for inherited rows: the ancestor the constraint comes from. */
  ancestorLabel?: string;
  onOpen?: (issueId: string) => void;
  onRemove?: (prerequisite: IssuePrerequisite) => void;
  removeDisabled?: boolean;
  /** Inherited rows only: switch to editing the source ancestor's edges. */
  onEditSource?: (ancestorId: string) => void;
}) {
  const { t } = useT("dependencies");
  const paths = useWorkspacePaths();
  const statusLabel = useStatusLabel(wsId)(prerequisite.status);
  const satisfied = prerequisite.satisfied === true;
  const sourceId = prerequisite.inheritedFrom[0];

  const body = (
    <>
      <StatusIcon
        status={prerequisite.status}
        category={
          isIssueStatusCategory(prerequisite.statusCategory)
            ? prerequisite.statusCategory
            : undefined
        }
        className="mt-0.5 h-3.5 w-3.5 shrink-0"
      />
      <span className="min-w-0 flex-1">
        <span className="block break-all text-muted-foreground tabular-nums">
          {prerequisite.identifier ?? prerequisite.issueId}
        </span>
        <span className="block whitespace-normal [overflow-wrap:anywhere]">
          {prerequisite.title ?? ""}
        </span>
      </span>
    </>
  );

  return (
    <div className="group/row flex items-start gap-1 rounded-md px-2 -mx-2 py-1 transition-colors hover:bg-accent/50">
      <div className="min-w-0 flex-1">
        {onOpen ? (
          <button
            type="button"
            onClick={() => onOpen(prerequisite.issueId)}
            className="flex w-full min-w-0 cursor-pointer items-start gap-1.5 text-left text-caption"
          >
            {body}
          </button>
        ) : (
          <AppLink
            href={paths.issueDetail(prerequisite.identifier || prerequisite.issueId)}
            className="flex min-w-0 items-start gap-1.5 text-caption"
          >
            {body}
          </AppLink>
        )}
        <div className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5 pl-5 text-micro [overflow-wrap:anywhere]">
          {projectTitle && (
            <span className="max-w-full rounded-full bg-muted/60 px-1.5 py-0.5 text-muted-foreground">
              {projectTitle}
            </span>
          )}
          {!satisfied && <span className="text-warning">{statusLabel}</span>}
          {ancestorLabel && (
            <span className="text-muted-foreground">
              {t(($) => $.detail.inherited_from, { name: ancestorLabel })}
            </span>
          )}
          {onEditSource && sourceId && (
            <button
              type="button"
              onClick={() => onEditSource(sourceId)}
              className="rounded-sm px-1.5 py-0.5 text-primary transition-colors hover:bg-accent"
            >
              {t(($) => $.detail.edit_source)}
            </button>
          )}
        </div>
      </div>
      {onRemove && (
        <button
          type="button"
          disabled={removeDisabled}
          title={t(($) => $.detail.remove_aria, {
            identifier: prerequisite.identifier ?? "",
          })}
          aria-label={t(($) => $.detail.remove_aria, {
            identifier: prerequisite.identifier ?? "",
          })}
          onClick={() => onRemove(prerequisite)}
          className="shrink-0 rounded-sm p-1 text-muted-foreground transition-opacity hover:bg-accent hover:text-foreground focus-visible:opacity-100 group-hover/row:opacity-100 disabled:opacity-50 sm:opacity-0"
        >
          <Unlink className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  );
}

/**
 * A list of prerequisites. `mode` controls inheritance presentation:
 * - "direct" (default): the editable set — removal allowed, no source labels.
 * - "inherited": source labels on, removal never offered.
 * - "mixed" (blocked previews): each row is labeled when it arrives through an
 *   ancestor (inheritedFrom non-empty); removal never offered.
 * Weakening an inherited constraint must happen on the source edge, never
 * silently from the inheriting issue (OL-38).
 */
export function PrerequisiteList({
  wsId,
  items,
  mode = "direct",
  onOpen,
  onRemove,
  removeDisabled,
  onEditSource,
}: {
  wsId: string;
  items: IssuePrerequisite[];
  mode?: "direct" | "inherited" | "mixed";
  onOpen?: (issueId: string) => void;
  onRemove?: (prerequisite: IssuePrerequisite) => void;
  removeDisabled?: boolean;
  onEditSource?: (ancestorId: string) => void;
}) {
  const { projectTitleOf, identifierOf } = usePrerequisiteDisplay(wsId, items);
  return (
    <div className="flex flex-col">
      {items.map((p) => {
        const isInherited = mode === "inherited" || (mode === "mixed" && p.inheritedFrom.length > 0);
        return (
          <PrerequisiteRow
            key={p.issueId}
            wsId={wsId}
            prerequisite={{ ...p, identifier: p.identifier ?? identifierOf(p.issueId) }}
            projectTitle={projectTitleOf(p.issueId)}
            ancestorLabel={
              isInherited && p.inheritedFrom.length > 0
                ? identifierOf(p.inheritedFrom[0]!)
                : undefined
            }
            onOpen={onOpen}
            onRemove={isInherited ? undefined : onRemove}
            removeDisabled={removeDisabled}
            onEditSource={mode === "inherited" ? onEditSource : undefined}
          />
        );
      })}
    </div>
  );
}

/**
 * The unfinished-prerequisite evidence for a dependency-blocked write (OL-41):
 * each blocked target's unsatisfied direct + inherited prerequisites, plus the
 * restricted-blockers note when part of the picture is permission-hidden.
 * Presentational only — confirmation state and submit live in the caller
 * (run-confirm modal / create dialog), and a `null` projection is rendered as
 * "unknown, refresh", never as an empty (read: satisfied) list.
 */
