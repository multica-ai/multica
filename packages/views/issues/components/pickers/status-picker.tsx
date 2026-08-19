"use client";

import { useMemo, useState } from "react";
import type { IssueStatus, UpdateIssueRequest } from "@multica/core/types";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { StatusIcon } from "../status-icon";
import { PropertyPicker, PickerItem } from "./property-picker";
import { useT } from "../../../i18n";
import { useStatusLabel } from "../../utils/status-label";
import { useStatusOptions } from "../../utils/status-options";

/** Above this many options the flat list stops being scannable. */
const SEARCH_THRESHOLD = 9;

export function StatusPicker({
  status,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
}: {
  /**
   * The currently-selected status, used to check the matching row. `null`
   * means "no single current value" (e.g. a batch selection spanning several
   * statuses) — no row is checked. Single-issue callers always pass a concrete
   * status.
   */
  status: IssueStatus | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
}) {
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const [query, setQuery] = useState("");
  const { t } = useT("issues");
  // Every StatusPicker call site lives inside the workspace shell (issue
  // detail, table, board batch toolbar, create-issue modal), so the provider
  // is guaranteed here.
  const wsId = useWorkspaceId();
  const { categoryOf, entryOf } = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);

  /**
   * Offerable statuses as one flat list, in canonical category order.
   *
   * Archived statuses are excluded: archiving retires a status from future
   * assignment while leaving the issues already on it untouched. Falls back to
   * the 7 built-ins until the catalog lands, so a cold render offers exactly
   * what it always did instead of an empty popover. (MUL-6243)
   */
  const allOptions = useStatusOptions(wsId);

  const options = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return allOptions;
    return allOptions.filter((o) => o.label.toLowerCase().includes(q));
  }, [allOptions, query]);

  const searchable = allOptions.length > SEARCH_THRESHOLD;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v) => {
        if (!v) setQuery("");
        setOpen(v);
      }}
      width="w-52"
      align={align}
      triggerRender={triggerRender}
      searchable={searchable}
      searchPlaceholder={t(($) => $.filters.search_status)}
      onSearchChange={setQuery}
      trigger={
        customTrigger ??
        (status != null ? (
          <>
            <StatusIcon
              status={status}
              category={categoryOf(status)}
              color={entryOf(status)?.color}
              className="h-3.5 w-3.5 shrink-0"
            />
            <span className="truncate">{labelOf(status)}</span>
          </>
        ) : null)
      }
    >
      {options.map((option) => (
        <PickerItem
          key={option.key}
          selected={option.key === status}
          hoverClassName={STATUS_CONFIG[option.category].hoverBg}
          onClick={() => {
            onUpdate({ status: option.key });
            setOpen(false);
            setQuery("");
          }}
        >
          <StatusIcon
            status={option.key}
            category={option.category}
            color={option.color}
            className="h-3.5 w-3.5"
          />
          <span className="truncate">{option.label}</span>
        </PickerItem>
      ))}
    </PropertyPicker>
  );
}
