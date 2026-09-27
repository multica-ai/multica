"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { CircleOff, GitMerge } from "lucide-react";
import { api } from "@multica/core/api";
import { derivePRMergeStatus, PR_MERGE_STATUS_NONE } from "@multica/core/github";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import type { IssueStatusCategory, Workspace } from "@multica/core/types";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { StatusIcon } from "../../issues/components/status-icon";
import { useStatusLabel } from "../../issues/utils/status-label";
import { useT } from "../../i18n";

/** The categories a merge may move an issue into, with their built-ins.
 * Blocked is left out: a merge never blocks an issue. */
const TARGET_CATEGORIES: { category: IssueStatusCategory; builtIns: string[] }[] = [
  { category: "started", builtIns: ["in_progress", "in_review"] },
  { category: "done", builtIns: ["done"] },
];

/**
 * "After PRs merge, move the issue to" (MUL-7726): the one PR merge setting,
 * shared by GitHub and self-hosted providers. It saves on change like the
 * other integration switches and only affects merges from now on.
 */
export function PRMergeStatusRow({ canManage, disabled = false }: { canManage: boolean; disabled?: boolean }) {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const catalog = useIssueStatuses(wsId);
  const statusLabel = useStatusLabel(wsId);
  const [saving, setSaving] = useState(false);

  // Built-ins are always offered: the server accepts them before the catalog
  // is seeded, and the catalog may still be loading.
  const groups = TARGET_CATEGORIES.map(({ category, builtIns }) => {
    const listed = catalog.inCategory(category).map((entry) => entry.key).filter((key) => key !== "blocked");
    return { category, keys: [...builtIns.filter((key) => !listed.includes(key)), ...listed] };
  });
  const stored = derivePRMergeStatus(workspace);
  // A choice that no longer names an offered status (archived since) moves
  // nothing on the server, so it reads as "no change" here too.
  const value = !catalog.isLoaded || groups.some((group) => group.keys.includes(stored)) ? stored : PR_MERGE_STATUS_NONE;

  async function persist(next: string) {
    if (!workspace || saving || next === value) return;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        settings: { ...((workspace.settings as Record<string, unknown>) ?? {}), pr_merge_status: next },
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.auto_save.failed));
    } finally {
      setSaving(false);
    }
  }

  const option = (key: string) =>
    key === PR_MERGE_STATUS_NONE ? (
      <>
        <CircleOff className="text-muted-foreground" />
        {t(($) => $.pr_merge_status.none)}
      </>
    ) : (
      <>
        <StatusIcon
          status={key}
          category={catalog.categoryOf(key)}
          color={catalog.colorOf(key)}
          icon={catalog.iconOf(key)}
          className="size-3.5"
        />
        {statusLabel(key)}
      </>
    );

  return (
    <div className="flex items-start justify-between gap-4 px-4 py-3.5">
      <div className="flex items-start gap-3">
        <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground">
          <GitMerge className="h-4 w-4" />
        </div>
        <div className="space-y-1">
          <Label htmlFor="pr-merge-status" className="text-body font-medium">
            {t(($) => $.pr_merge_status.label)}
          </Label>
          <p className="text-body text-muted-foreground">{t(($) => $.pr_merge_status.description)}</p>
        </div>
      </div>
      <Select
        items={[
          { value: PR_MERGE_STATUS_NONE, label: t(($) => $.pr_merge_status.none) },
          ...groups.flatMap((group) => group.keys.map((key) => ({ value: key, label: statusLabel(key) }))),
        ]}
        value={value}
        onValueChange={(next) => next && void persist(next)}
        disabled={!canManage || disabled || saving}
      >
        <SelectTrigger id="pr-merge-status" className="w-48 shrink-0" aria-busy={saving || undefined}>
          <SelectValue>{() => option(value)}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={PR_MERGE_STATUS_NONE}>{option(PR_MERGE_STATUS_NONE)}</SelectItem>
          {groups.map((group) => (
            <SelectGroup key={group.category}>
              <SelectSeparator />
              <SelectLabel>{t(($) => $.issue_statuses.category_labels[group.category])}</SelectLabel>
              {group.keys.map((key) => (
                <SelectItem key={key} value={key}>
                  {option(key)}
                </SelectItem>
              ))}
            </SelectGroup>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
