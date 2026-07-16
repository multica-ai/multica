"use client";

import { useState } from "react";
import type { IssuePriority, UpdateIssueRequest } from "@multica/core/types";
import { PRIORITY_ORDER, PRIORITY_CONFIG } from "@multica/core/issues/config";
import { PriorityIcon } from "../priority-icon";
import { DeferredPopup } from "../../../common/deferred-popup";
import { PropertyPicker, PickerItem, PICKER_TRIGGER_CLASS } from "./property-picker";
import { useT } from "../../../i18n";

interface PriorityPickerProps {
  /**
   * The currently-selected priority, used to check the matching row. `null`
   * means "no single current value" (e.g. a batch selection spanning several
   * priorities) — no row is checked. Single-issue callers always pass a
   * concrete priority.
   */
  priority: IssuePriority | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement<Record<string, unknown>>;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  /** Open the picker on first mount. Used by progressive-disclosure
   *  sidebars so a newly-added field immediately enters edit state. */
  defaultOpen?: boolean;
}

/**
 * Uncontrolled callers that bring their own trigger content (board cards,
 * list rows) get a deferred lookalike trigger; the popover machinery mounts
 * on first interaction. See `DeferredPopup` for why.
 */
export function PriorityPicker(props: PriorityPickerProps) {
  const hasDeferredTriggerContent =
    props.trigger !== undefined || props.triggerRender?.props.children != null;
  const canDefer =
    props.open === undefined &&
    props.onOpenChange === undefined &&
    !props.defaultOpen &&
    hasDeferredTriggerContent;
  if (!canDefer) {
    return <PriorityPickerImpl {...props} />;
  }
  return (
    <DeferredPopup
      trigger={props.trigger}
      triggerRender={props.triggerRender}
      triggerClassName={PICKER_TRIGGER_CLASS}
    >
      {(open, onOpenChange) => (
        <PriorityPickerImpl {...props} open={open} onOpenChange={onOpenChange} />
      )}
    </DeferredPopup>
  );
}

function PriorityPickerImpl({
  priority,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
  defaultOpen = false,
}: PriorityPickerProps) {
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const { t } = useT("issues");

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-44"
      align={align}
      triggerRender={triggerRender}
      trigger={
        customTrigger ??
        (priority != null ? (
          <>
            <PriorityIcon priority={priority} className="shrink-0" />
            <span className="truncate">{t(($) => $.priority[priority])}</span>
          </>
        ) : null)
      }
    >
      {PRIORITY_ORDER.map((p) => {
        const c = PRIORITY_CONFIG[p];
        return (
          <PickerItem
            key={p}
            selected={p === priority}
            onClick={() => {
              onUpdate({ priority: p });
              setOpen(false);
            }}
          >
            <span className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium ${c.badgeBg} ${c.badgeText}`}>
              <PriorityIcon priority={p} className="h-3 w-3" inheritColor />
              {t(($) => $.priority[p])}
            </span>
          </PickerItem>
        );
      })}
    </PropertyPicker>
  );
}
