"use client";

import { CalendarDays } from "lucide-react";
import type { UpdateIssueRequest } from "@multica/core/types";
import { DateOnlyPicker } from "../../../common/date-only-picker";
import { useT } from "../../../i18n";

export function DueDatePicker({
  dueDate,
  onUpdate,
  trigger,
  triggerRender,
  open,
  onOpenChange,
  align = "start",
  defaultOpen = false,
}: {
  dueDate: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  /** Open the popover on first mount. Used by progressive-disclosure
   *  sidebars so a newly-added field immediately enters edit state. */
  defaultOpen?: boolean;
}) {
  const { t } = useT("issues");
  return (
    <DateOnlyPicker
      value={dueDate}
      onChange={(v) => onUpdate({ due_date: v })}
      icon={<CalendarDays className="h-3.5 w-3.5 text-muted-foreground" />}
      placeholder={t(($) => $.pickers.due_date.trigger_label)}
      clearLabel={t(($) => $.pickers.due_date.clear_action)}
      highlightOverdue
      trigger={trigger}
      triggerRender={triggerRender}
      open={open}
      onOpenChange={onOpenChange}
      align={align}
      defaultOpen={defaultOpen}
    />
  );
}
