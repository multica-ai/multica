"use client";

import { ArchiveRestore } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { useCerebroInboxStrings } from "../strings";

/**
 * Toolbar icon-button that mirrors the upstream archive button in the
 * IssueDetail header but fires an unarchive action instead. Used when the
 * IssueDetail is embedded in the archived inbox view (JEH-1166 / JEH-1321) so
 * the user can restore an archived inbox row from the detail panel — not only
 * from the row-hover affordance.
 *
 * Visually matches the other toolbar buttons (ghost variant, icon-sm size,
 * tooltip on bottom). Lives in cerebro-inbox so the upstream IssueDetail only
 * needs a one-line import + render to wire it in.
 */
export function CerebroUnarchiveToolbarButton({
  onUnarchive,
}: {
  onUnarchive: () => void;
}) {
  const strings = useCerebroInboxStrings();
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            className="text-muted-foreground"
            aria-label={strings.unarchive_label}
            onClick={onUnarchive}
          >
            <ArchiveRestore />
          </Button>
        }
      />
      <TooltipContent side="bottom">{strings.unarchive_tooltip}</TooltipContent>
    </Tooltip>
  );
}
