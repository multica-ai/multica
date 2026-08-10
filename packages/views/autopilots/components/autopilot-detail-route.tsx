"use client";

import { useQuery } from "@tanstack/react-query";
import { Zap } from "lucide-react";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { ListDetailLayout, ListDetailRail } from "../../layout/list-detail";
import { AutopilotDetailPage } from "./autopilot-detail-page";

/**
 * Route-level wrapper for `/{ws}/autopilots/{id}`.
 *
 * Adds the two-column list-rail layout around the existing detail page: every
 * workspace autopilot on the left, the current one highlighted, clicking a row
 * navigates in place (`replace`, so Back doesn't bounce between list entries).
 * The rail/detail stay mounted across switches — no `key={autopilotId}` — so
 * the list keeps its scroll position.
 */
export function AutopilotDetailRoute({ autopilotId }: { autopilotId: string }) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const { data: autopilots = [] } = useQuery(autopilotListOptions(wsId));

  const rail = (
    <ListDetailRail
      count={autopilots.length}
      expandAriaLabel={t(($) => $.detail.list_rail.toggle_expand_aria)}
      collapseAriaLabel={t(($) => $.detail.list_rail.toggle_collapse_aria)}
      countBadge={t(($) => $.detail.list_rail.count_badge, {
        count: autopilots.length,
      })}
      testIdPrefix="autopilot-detail-rail"
    >
      {autopilots.map((autopilot) => {
        const active = autopilot.id === autopilotId;
        return (
          <button
            key={autopilot.id}
            type="button"
            data-active={active || undefined}
            data-testid={`autopilot-detail-rail-row-${autopilot.id}`}
            onClick={() =>
              navigation.replace(paths.autopilotDetail(autopilot.id))
            }
            className={cn(
              "flex w-full items-center gap-2 px-3 py-1.5 text-left text-body",
              active
                ? "bg-surface-selected hover:bg-surface-selected"
                : "hover:bg-surface-hover",
            )}
          >
            <Zap
              className={cn(
                "h-3.5 w-3.5 shrink-0",
                autopilot.status === "paused"
                  ? "text-amber-500"
                  : "text-muted-foreground",
              )}
            />
            <span className="min-w-0 flex-1 truncate">{autopilot.title}</span>
          </button>
        );
      })}
    </ListDetailRail>
  );

  return (
    <ListDetailLayout
      rail={rail}
      detail={<AutopilotDetailPage autopilotId={autopilotId} />}
    />
  );
}
