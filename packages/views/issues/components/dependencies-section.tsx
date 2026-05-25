// CEREBRO-PATCH(issue-dependencies): FIR-823 sidebar UI for blocks/blocked-by/related.
"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus, X } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { issueDependenciesOptions } from "@multica/core/dependencies/queries";
import {
  useAddBlocks,
  useAddBlockedBy,
  useAddRelated,
  useRemoveBlocks,
  useRemoveBlockedBy,
  useRemoveRelated,
} from "@multica/core/dependencies/mutations";
import type { IssueDependencyRef } from "@multica/core/types";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { AppLink } from "../../navigation";
import { IssuePickerModal } from "../../modals/issue-picker-modal";
import { useT } from "../../i18n";
import { StatusIcon } from "./status-icon";

type PickerTarget = "blocked_by" | "blocks" | "related" | null;

export function DependenciesSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const [picker, setPicker] = useState<PickerTarget>(null);

  const { data } = useQuery(issueDependenciesOptions(wsId, issueId));
  const blockedBy = data?.blocked_by ?? [];
  const blocks = data?.blocks ?? [];
  const related = data?.related ?? [];

  const addBlockedBy = useAddBlockedBy(issueId);
  const addBlocks = useAddBlocks(issueId);
  const addRelated = useAddRelated(issueId);
  const removeBlockedBy = useRemoveBlockedBy(issueId);
  const removeBlocks = useRemoveBlocks(issueId);
  const removeRelated = useRemoveRelated(issueId);

  const hasAny = blockedBy.length + blocks.length + related.length > 0;

  const groups: {
    key: Exclude<PickerTarget, null>;
    label: string;
    refs: IssueDependencyRef[];
    onAdd: (otherId: string) => void;
    onRemove: (otherId: string) => void;
  }[] = [
    {
      key: "blocked_by",
      label: t(($) => $.detail.dependencies_blocked_by),
      refs: blockedBy,
      onAdd: (otherId) => addBlockedBy.mutate(otherId),
      onRemove: (otherId) => removeBlockedBy.mutate(otherId),
    },
    {
      key: "blocks",
      label: t(($) => $.detail.dependencies_blocks),
      refs: blocks,
      onAdd: (otherId) => addBlocks.mutate(otherId),
      onRemove: (otherId) => removeBlocks.mutate(otherId),
    },
    {
      key: "related",
      label: t(($) => $.detail.dependencies_related),
      refs: related,
      onAdd: (otherId) => addRelated.mutate(otherId),
      onRemove: (otherId) => removeRelated.mutate(otherId),
    },
  ];

  const activeGroup = groups.find((g) => g.key === picker) ?? null;

  return (
    <div className="space-y-3">
      {!hasAny && (
        <p className="px-2 text-xs text-muted-foreground">
          {t(($) => $.detail.dependencies_none)}
        </p>
      )}

      {groups.map((group) => (
        <div key={group.key}>
          <div className="mb-1 flex items-center justify-between px-2">
            <span className="text-xs font-medium text-muted-foreground">
              {group.label}
            </span>
            <Tooltip>
              <TooltipTrigger
                type="button"
                className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                aria-label={t(($) => $.detail.dependencies_add)}
                onClick={() => setPicker(group.key)}
              >
                <Plus className="h-3 w-3" />
              </TooltipTrigger>
              <TooltipContent>{t(($) => $.detail.dependencies_add)}</TooltipContent>
            </Tooltip>
          </div>
          {group.refs.map((ref) => (
            <div
              key={ref.id}
              className="group -mx-2 flex items-center gap-1 rounded-md px-2 py-1.5 text-xs transition-colors hover:bg-accent/50"
            >
              <AppLink
                href={paths.issueDetail(ref.id)}
                className="flex min-w-0 flex-1 items-center gap-1.5"
              >
                <StatusIcon status={ref.status} className="h-3.5 w-3.5 shrink-0" />
                <span className="shrink-0 text-muted-foreground">{ref.identifier}</span>
                <span className="truncate group-hover:text-foreground">{ref.title}</span>
              </AppLink>
              <Tooltip>
                <TooltipTrigger
                  type="button"
                  className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-colors transition-opacity hover:bg-accent hover:text-foreground focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring group-hover:opacity-100"
                  aria-label={t(($) => $.detail.dependencies_remove)}
                  onClick={() => group.onRemove(ref.id)}
                >
                  <X className="h-3 w-3" />
                </TooltipTrigger>
                <TooltipContent>{t(($) => $.detail.dependencies_remove)}</TooltipContent>
              </Tooltip>
            </div>
          ))}
        </div>
      ))}

      {activeGroup && (
        <IssuePickerModal
          open
          onOpenChange={(open) => {
            if (!open) setPicker(null);
          }}
          title={t(($) => $.detail.dependencies_picker_title)}
          description={t(($) => $.detail.dependencies_picker_description)}
          excludeIds={[issueId, ...activeGroup.refs.map((r) => r.id)]}
          onSelect={(selected) => {
            activeGroup.onAdd(selected.id);
            setPicker(null);
          }}
        />
      )}
    </div>
  );
}
