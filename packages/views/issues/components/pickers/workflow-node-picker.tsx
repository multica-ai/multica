"use client";

import { useState } from "react";
import type { IssueWorkflowStatusNode } from "@multica/core/types";
import { workflowPhaseCategory } from "@multica/core/issue-workflows";
import { PropertyPicker, PickerItem } from "./property-picker";
import { StatusIcon } from "../status-icon";
import { useT } from "../../../i18n";

/** Selection only: the caller owns creation, transition, or project migration. */
export function WorkflowNodePicker({
  nodes,
  value,
  onChange,
  open: controlledOpen,
  onOpenChange,
  trigger,
  triggerRender,
  disabled = false,
}: {
  nodes: IssueWorkflowStatusNode[];
  value?: string | null;
  onChange: (node: IssueWorkflowStatusNode) => void;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  disabled?: boolean;
}) {
  const { t } = useT("issues");
  const [internalOpen, setInternalOpen] = useState(false);
  const [search, setSearch] = useState("");
  const current = nodes.find((node) => node.id === value);
  const setOpen = (next: boolean) => {
    setInternalOpen(next);
    onOpenChange?.(next);
  };
  return (
    <PropertyPicker
      open={controlledOpen ?? internalOpen}
      onOpenChange={setOpen}
      align="start"
      width="w-64"
      triggerRender={triggerRender}
      searchable={nodes.length > 9}
      searchPlaceholder={t(($) => $.filters.search_status)}
      onSearchChange={setSearch}
      trigger={
        trigger ??
        (current ? (
          <>
            <StatusIcon
              status={current.legacy_status_key ?? "todo"}
              category={workflowPhaseCategory(current.phase)}
              color={current.color}
              className="size-3.5 shrink-0"
            />
            <span className="truncate">{current.name}</span>
          </>
        ) : (
          t(($) => $.workflow_selection.choose)
        ))
      }
    >
      {nodes
        .filter(
          (node) =>
            !node.archived_at &&
            node.name.toLowerCase().includes(search.trim().toLowerCase()),
        )
        .map((node) => (
          <PickerItem
            key={node.id}
            selected={node.id === value}
            disabled={disabled}
            onClick={() => {
              onChange(node);
              setOpen(false);
            }}
          >
            <StatusIcon
              status={node.legacy_status_key ?? "todo"}
              category={workflowPhaseCategory(node.phase)}
              color={node.color}
              className="size-3.5 shrink-0"
            />
            <span className="truncate">{node.name}</span>
          </PickerItem>
        ))}
    </PropertyPicker>
  );
}
