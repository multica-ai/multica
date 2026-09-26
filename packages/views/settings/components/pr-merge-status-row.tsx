"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { CircleOff } from "lucide-react";
import { api } from "@multica/core/api";
import { derivePRMergeStatus, PR_MERGE_STATUS_NONE } from "@multica/core/github";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import type { IssueStatusCategory, Workspace } from "@multica/core/types";
import { workspaceKeys } from "@multica/core/workspace/queries";
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
import { SettingsRow } from "./settings-layout";

/** The categories a merge may move an issue into, with their built-ins.
 * Blocked is left out: a merge never blocks an issue. */
const TARGET_CATEGORIES: { category: IssueStatusCategory; builtIns: string[] }[] = [
  { category: "started", builtIns: ["in_progress", "in_review"] },
  { category: "done", builtIns: ["done"] },
];

/**
 * The status a merge moves issues to, and the statuses it may move them to.
 * Shared by the editor on Statuses & transitions and the read-only summary on
 * the Code page so both agree on what an archived choice means.
 */
export function usePRMergeStatus() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const statusLabel = useStatusLabel(wsId);

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

  const renderOption = (key: string) =>
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

  return { value, groups, renderOption, statusLabel };
}

/**
 * "After PRs merge, move the issue to" (MUL-7726): the one PR merge rule,
 * shared by GitHub and self-hosted providers. It lives with the other
 * automatic transitions and only affects merges from now on.
 */
export function PRMergeStatusRow({ canManage }: { canManage: boolean }) {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const qc = useQueryClient();
  const { value, groups, renderOption, statusLabel } = usePRMergeStatus();
  const [saving, setSaving] = useState(false);

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
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.auto_save.failed));
    } finally {
      setSaving(false);
    }
  }

  return (
    <SettingsRow
      anchor="pr-merge-status"
      label={t(($) => $.pr_merge_status.label)}
      description={t(($) => $.pr_merge_status.description)}
      size="select"
    >
      <Select
        items={[
          { value: PR_MERGE_STATUS_NONE, label: t(($) => $.pr_merge_status.none) },
          ...groups.flatMap((group) => group.keys.map((key) => ({ value: key, label: statusLabel(key) }))),
        ]}
        value={value}
        onValueChange={(next) => next && void persist(next)}
        disabled={!canManage || saving}
      >
        <SelectTrigger
          size="sm"
          className="w-full"
          aria-label={t(($) => $.pr_merge_status.label)}
          aria-busy={saving || undefined}
        >
          <SelectValue>{() => renderOption(value)}</SelectValue>
        </SelectTrigger>
        <SelectContent align="end">
          <SelectItem value={PR_MERGE_STATUS_NONE}>{renderOption(PR_MERGE_STATUS_NONE)}</SelectItem>
          {groups.map((group) => (
            <SelectGroup key={group.category}>
              <SelectSeparator />
              <SelectLabel>{t(($) => $.issue_statuses.category_labels[group.category])}</SelectLabel>
              {group.keys.map((key) => (
                <SelectItem key={key} value={key}>
                  {renderOption(key)}
                </SelectItem>
              ))}
            </SelectGroup>
          ))}
        </SelectContent>
      </Select>
    </SettingsRow>
  );
}
