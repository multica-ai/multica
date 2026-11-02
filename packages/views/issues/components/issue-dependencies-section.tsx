"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronRight, Pencil, TriangleAlert } from "@multica/ui/icons";
import { dependencyErrorDetails } from "@multica/core/api";
import type { IssuePrerequisite } from "@multica/core/api";
import { issueDependenciesOptions } from "@multica/core/issues/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import type { Issue } from "@multica/core/types";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { useT } from "../../i18n";
import { PrerequisiteList } from "./dependency-prerequisites";

/**
 * The issue detail sidebar's dependency section (OL-44): direct and inherited
 * prerequisites, what this issue blocks, and the unsatisfied summary. Direct
 * removal goes through the same compound write as the editor (version-guarded),
 * under the ordinary issue permissions. Relations do not gate execution.
 * Inherited rows are read-only here and in the editor; the constraint changes
 * on the source ancestor only.
 */
export function IssueDependenciesSection({ issue }: { issue: Issue }) {
  const { t } = useT("dependencies");
  const wsId = useWorkspaceId();
  const openModal = useModalStore((s) => s.open);
  const updateIssue = useUpdateIssue();
  const [open, setOpen] = useState(true);
  const [removingId, setRemovingId] = useState<string | null>(null);

  const depsQuery = useQuery(issueDependenciesOptions(wsId, issue.id));
  const view = depsQuery.data ?? null;

  const hasContent =
    !!view &&
    (view.blockedBy.length > 0 ||
      view.inheritedBlockedBy.length > 0 ||
      view.blocking.length > 0 ||
      view.hasRestrictedBlockers);

  const removePrerequisite = async (p: IssuePrerequisite) => {
    if (!view || removingId) return;
    setRemovingId(p.issueId);
    try {
      await updateIssue.mutateAsync({
        id: issue.id,
        blockedBy: view.blockedBy
          .filter((x) => x.issueId !== p.issueId)
          .map((x) => x.issueId)
          .sort(),
        expectedDependencyVersion: view.dependencyVersion,
      });
    } catch (err) {
      const details = dependencyErrorDetails(err);
      // The failed write already invalidated the workspace's dependency
      // projections (mutations.ts), so the fresh picture is on its way; the
      // toast only has to say why THIS removal did not happen.
      toast.error(
        details?.reasonCode === "dependency_version_conflict"
            ? t(($) => $.detail.remove_conflict)
            : t(($) => $.detail.remove_failed),
      );
    } finally {
      setRemovingId(null);
    }
  };

  // Empty-and-loaded hides the section entirely (same contract as the parent
  // issue card); loading and failure stay visible so a slow or broken
  // dependency read never silently looks like "no prerequisites".
  if (!depsQuery.isLoading && !depsQuery.isError && !hasContent && view !== null) {
    return null;
  }

  const unsatisfiedCount = view?.unsatisfied.length ?? 0;

  return (
    <div>
      <div className="mb-2 flex items-center gap-0.5">
        <button
          type="button"
          className={`flex flex-1 items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={() => setOpen(!open)}
          aria-expanded={open}
        >
          {t(($) => $.detail.section_title)}
          <ChevronRight
            className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
          />
        </button>
        <button
          type="button"
          title={t(($) => $.detail.edit_aria)}
          aria-label={t(($) => $.detail.edit_aria)}
          onClick={() => openModal("issue-edit-dependencies", { issueId: issue.id })}
          className="shrink-0 rounded-sm p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          <Pencil className="h-3.5 w-3.5" />
        </button>
      </div>

      {open && (
        <div className="flex flex-col gap-2 pl-2">
          {depsQuery.isLoading && (
            <div className="flex items-center gap-2 px-2 py-1.5 text-caption text-muted-foreground">
              <Spinner className="size-3" />
              {t(($) => $.detail.loading)}
            </div>
          )}

          {(depsQuery.isError || (depsQuery.isSuccess && view === null)) && (
            <div className="flex items-center gap-2 px-2 py-1.5">
              <span className="flex-1 text-caption text-muted-foreground">
                {view === null && !depsQuery.isError
                  ? t(($) => $.detail.unknown_state)
                  : t(($) => $.detail.load_error)}
              </span>
              <button
                type="button"
                onClick={() => void depsQuery.refetch()}
                className="shrink-0 rounded-sm px-1.5 py-0.5 text-micro text-primary hover:bg-accent"
              >
                {t(($) => $.detail.retry)}
              </button>
            </div>
          )}

          {view && (
            <>
              {(unsatisfiedCount > 0 || view.hasRestrictedBlockers) && (
                <div className="flex items-start gap-1.5 rounded-md bg-warning/10 px-2 py-1.5 text-caption text-warning">
                  <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />
                  <span>
                    {unsatisfiedCount > 0 &&
                      t(($) => $.detail.unsatisfied_summary, {
                        count: unsatisfiedCount,
                      })}
                    {view.hasRestrictedBlockers && (
                      <span className="block text-micro">
                        {t(($) => $.detail.restricted_note)}
                      </span>
                    )}
                  </span>
                </div>
              )}

              {view.blockedBy.length > 0 && (
                <PrerequisiteList
                  wsId={wsId}
                  items={view.blockedBy}
                  onRemove={removePrerequisite}
                  removeDisabled={removingId !== null}
                />
              )}

              {view.inheritedBlockedBy.length > 0 && (
                <PrerequisiteList wsId={wsId} items={view.inheritedBlockedBy} mode="inherited" />
              )}

              {view.blocking.length > 0 && (
                <div>
                  <div className="px-2 pb-0.5 pt-1 text-micro font-medium text-muted-foreground">
                    {t(($) => $.detail.blocking_section)}
                  </div>
                  <PrerequisiteList wsId={wsId} items={view.blocking} />
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}
