"use client";

import { useState } from "react";
import { Flag, Plus, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useUpdateIssueBlockers } from "@multica/core/issues/blockers";
import type { Issue } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { IssuePickerModal } from "../../modals/issue-picker-modal";
import { IssueSidebarSectionHeader } from "./issue-sidebar-section";

export function IssueBlockedMarker({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const count = issue.blocked_count ?? 0;
  if (!count) return null;
  const label = t(($) => $.waiting.count, { count });
  return <span className="inline-flex shrink-0 items-center gap-1 text-destructive text-caption" title={label} aria-label={label}>
    <Flag className="size-3.5" aria-hidden="true" />{count}
  </span>;
}

export function IssueBlockersSection({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const [open, setOpen] = useState(true);
  const [pickerOpen, setPickerOpen] = useState(false);
  const mutation = useUpdateIssueBlockers(wsId, issue.id);
  if (issue.blocked_by === undefined) return null;
  const blockers = issue.blocked_by;
  const manual = blockers.filter((b) => b.via === "manual");
  const stages = blockers.filter((b) => b.via === "stage");
  const activeStages = stages.filter((b) => !b.closed);
  const stageCount = stages.length - activeStages.length;
  const restrictedCount = Math.max(0, (issue.blocked_count ?? 0) - blockers.filter((b) => !b.closed).length);
  return <div>
    <IssueSidebarSectionHeader title={t(($) => $.waiting.title)} open={open} onOpenChange={setOpen} count={issue.blocked_count}
      actions={<Button variant="ghost" size="icon-xs" aria-label={t(($) => $.waiting.add)} onClick={() => { mutation.reset(); setPickerOpen(true); }}><Plus /></Button>} />
    {open && <div className="px-4 space-y-2 text-caption">
      {manual.map((blocker) => <div key={blocker.id} className="flex min-w-0 items-center gap-1">
        <Flag className={blocker.closed ? "size-3.5 shrink-0 text-muted-foreground" : "size-3.5 shrink-0 text-destructive"} aria-hidden="true" />
        <AppLink href={paths.issueDetail(blocker.id)} className="min-w-0 flex-1 truncate hover:underline" title={blocker.title}>
          <span className="text-muted-foreground">{blocker.identifier}</span>{" "}{blocker.title}
        </AppLink>
        <Button variant="ghost" size="icon-xs" disabled={mutation.isPending} aria-label={t(($) => $.waiting.remove, { identifier: blocker.identifier })}
          onClick={() => mutation.mutate({ remove_blocked_by: [blocker.id] })}><X /></Button>
      </div>)}
      {stages.length > 0 && <div>
        <p className="text-muted-foreground">{t(($) => $.waiting.stage, { finished: stageCount, total: stages.length })}</p>
        {activeStages.map((blocker) => <AppLink key={blocker.id} href={paths.issueDetail(blocker.id)} className="block truncate py-1 hover:underline" title={blocker.title}>
          <span className="text-muted-foreground">{blocker.identifier}</span>{" "}{blocker.title}
        </AppLink>)}
      </div>}
      {restrictedCount > 0 && <p className="text-muted-foreground">{t(($) => $.waiting.restricted, {count: restrictedCount})}</p>}
      {/* Only the fixed backlog key parks work, matching the server's queue admission. */}
      {(issue.blocked_count ?? 0) > 0 && <p className="text-muted-foreground">
        {issue.status === "backlog" ? t(($) => $.waiting.hint_backlog) : t(($) => $.waiting.hint)}
      </p>}
      {blockers.length === 0 && !restrictedCount && <p className="text-muted-foreground">{t(($) => $.waiting.empty)}</p>}
      {mutation.isError && <p role="alert" className="text-destructive">{mutation.error.message}</p>}
    </div>}
    <IssuePickerModal error={mutation.isError ? mutation.error.message : undefined} pending={mutation.isPending} closeOnSelect={false} open={pickerOpen} onOpenChange={setPickerOpen} title={t(($) => $.waiting.add)} description={t(($) => $.waiting.picker_hint)}
      excludeIds={[issue.id, ...(issue.parent_issue_id ? [issue.parent_issue_id] : []), ...manual.map((b) => b.id)]}
      isSelectable={(candidate) => candidate.parent_issue_id !== issue.id}
      onSelect={(candidate) => mutation.mutate({ blocked_by: [candidate.id] }, { onSuccess: () => setPickerOpen(false) })} />
  </div>;
}
