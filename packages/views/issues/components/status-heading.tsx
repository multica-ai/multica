import type { IssueStatus } from "@multica/core/types";
import { StatusIcon } from "./status-icon";
// CEREBRO-PATCH(status-heading-model): FIR-1550 status-model label/color override
import { useStatusPresentation } from "@multica/cerebro-status-models/views";
import { useT } from "../../i18n";

export function StatusHeading({
  status,
  count,
}: {
  status: IssueStatus;
  count: number;
}) {
  const { t } = useT("issues");
  // CEREBRO-PATCH(status-heading-model): swap upstream label/color for the project's model
  const pres = useStatusPresentation();
  const modelColor = pres.getColor(status);
  return (
    <div className="flex items-center gap-2">
      <span className="inline-flex items-center gap-1.5 text-xs font-semibold">
        <span style={modelColor ? { color: modelColor } : undefined}>
          <StatusIcon status={status} className="h-3 w-3" inheritColor={!!modelColor} />
        </span>
        {pres.getLabel(status) ?? t(($) => $.status[status])}
      </span>
      <span className="text-xs text-muted-foreground">{count}</span>
    </div>
  );
}
