"use client";

import { useMemo, useState } from "react";
import { Tag } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { labelListOptions } from "@multica/core/labels/queries";
import { useAttachIssueLabel, useDetachIssueLabel } from "@multica/core/labels/mutations";
import type { IssueLabel } from "@multica/core/types";
import { MAX_LABELS_PER_ISSUE } from "@multica/core/types";
import {
  PropertyPicker,
  PickerItem,
  PickerEmpty,
} from "./property-picker";
import { LabelColorDot } from "../label-chip";

/**
 * Multi-select picker for attaching/detaching labels on an issue. Stays open
 * across selections so the user can toggle several labels in one go.
 *
 * Workspace-scoped — pulls labelListOptions(wsId) through React Query.
 *
 * Disables un-checked rows once the issue already has MAX_LABELS_PER_ISSUE
 * attached, but always allows un-checking already-attached labels.
 */
export function LabelPicker({
  issueId,
  attached,
  trigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align = "start",
}: {
  issueId: string;
  attached: IssueLabel[];
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
}) {
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const [filter, setFilter] = useState("");

  const wsId = useWorkspaceId();
  const { data: allLabels = [] } = useQuery(labelListOptions(wsId));
  const attach = useAttachIssueLabel();
  const detach = useDetachIssueLabel();

  const attachedIds = useMemo(() => new Set(attached.map((l) => l.id)), [attached]);
  const atLimit = attached.length >= MAX_LABELS_PER_ISSUE;

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return allLabels;
    return allLabels.filter((l) => l.name.toLowerCase().includes(q));
  }, [allLabels, filter]);

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        if (!v) setFilter("");
      }}
      width="w-60"
      align={align}
      searchable
      searchPlaceholder="Filter labels..."
      onSearchChange={setFilter}
      triggerRender={triggerRender}
      trigger={
        trigger ?? (
          <>
            <Tag className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="text-muted-foreground">
              {attached.length === 0 ? "Add labels" : `${attached.length} label${attached.length === 1 ? "" : "s"}`}
            </span>
          </>
        )
      }
    >
      {filtered.length === 0 && filter && <PickerEmpty />}
      {filtered.length === 0 && !filter && (
        <div className="px-2 py-3 text-center text-xs text-muted-foreground">
          No labels yet — create one in workspace settings.
        </div>
      )}
      {filtered.map((label) => {
        const isAttached = attachedIds.has(label.id);
        const disabled = !isAttached && atLimit;
        return (
          <PickerItem
            key={label.id}
            selected={isAttached}
            disabled={disabled}
            onClick={() => {
              if (isAttached) {
                detach.mutate({ issueId, labelId: label.id });
              } else {
                if (disabled) return;
                attach.mutate({ issueId, labelId: label.id });
              }
              // Stay open — multi-select.
            }}
          >
            <LabelColorDot color={label.color} />
            <span className="truncate">{label.name}</span>
          </PickerItem>
        );
      })}
      {atLimit && (
        <div className="px-2 pt-1 pb-2 text-[11px] text-muted-foreground">
          Max {MAX_LABELS_PER_ISSUE} labels per issue.
        </div>
      )}
    </PropertyPicker>
  );
}
