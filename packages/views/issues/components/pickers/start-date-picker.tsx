"use client";

import { CalendarClock } from "@multica/ui/icons";
import type { UpdateIssueRequest } from "@multica/core/types";
import { DateOnlyPicker } from "../../../common/date-only-picker";
import { useT } from "../../../i18n";

export function StartDatePicker({
  startDate,
  onUpdate,
  trigger,
  triggerRender,
  tooltip,
  open,
  onOpenChange,
  align = "start",
  defaultOpen = false,
}: {
  startDate: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement<Record<string, unknown>>;
  /** Hover tooltip naming the property; hidden while the picker is open. */
  tooltip?: React.ReactNode;
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
      value={startDate}
      onChange={(v) => onUpdate({ start_date: v })}
      icon={<CalendarClock className="h-3.5 w-3.5 text-muted-foreground" />}
      placeholder={t(($) => $.pickers.start_date.trigger_label)}
      emptyLabel={t(($) => $.pickers.start_date.none)}
      trigger={trigger}
      triggerRender={triggerRender}
      tooltip={tooltip}
      open={open}
      onOpenChange={onOpenChange}
      align={align}
      defaultOpen={defaultOpen}
    />
  );
}
