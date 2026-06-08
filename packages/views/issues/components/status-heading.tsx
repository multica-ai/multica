import type { IssueStatus } from "@multica/core/types";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import { StatusIcon } from "./status-icon";

export function StatusHeading({
  status,
  count,
  label: explicitLabel,
  color,
}: {
  status: IssueStatus;
  count: number;
  /** Override label for custom statuses that don't have an i18n key. */
  label?: string;
  /** Override color passed to StatusIcon for custom statuses. */
  color?: string;
}) {
  const cfg = STATUS_CONFIG[status];
  const displayLabel = explicitLabel ?? cfg?.label ?? status;
  return (
    <div className="flex items-center gap-2">
      <span className="inline-flex items-center gap-1.5 text-xs font-semibold">
        <StatusIcon status={status} className="h-3 w-3" color={color} />
        {displayLabel}
      </span>
      <span className="text-xs text-muted-foreground">{count}</span>
    </div>
  );
}
