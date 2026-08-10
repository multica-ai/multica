"use client";

import { useQuery } from "@tanstack/react-query";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { ActorAvatar } from "../../common/actor-avatar";
import { ListDetailLayout, ListDetailRail } from "../../layout/list-detail";
import { AgentDetailPage } from "./agent-detail-page";

/**
 * Route-level wrapper for `/{ws}/agents/{id}`.
 *
 * Adds the two-column list-rail layout around the existing detail page: every
 * workspace agent on the left, the current one highlighted, clicking a row
 * navigates in place (`replace`, so Back doesn't bounce between list entries).
 * The rail/detail stay mounted across switches — no `key={agentId}` — so the
 * list keeps its scroll position.
 */
export function AgentDetailRoute({ agentId }: { agentId: string }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const rail = (
    <ListDetailRail
      count={agents.length}
      expandAriaLabel={t(($) => $.detail.list_rail.toggle_expand_aria)}
      collapseAriaLabel={t(($) => $.detail.list_rail.toggle_collapse_aria)}
      countBadge={t(($) => $.detail.list_rail.count_badge, {
        count: agents.length,
      })}
      testIdPrefix="agent-detail-rail"
    >
      {agents.map((agent) => {
        const active = agent.id === agentId;
        return (
          <button
            key={agent.id}
            type="button"
            data-active={active || undefined}
            data-testid={`agent-detail-rail-row-${agent.id}`}
            onClick={() => navigation.replace(paths.agentDetail(agent.id))}
            className={cn(
              "flex w-full items-center gap-2 px-3 py-1.5 text-left text-body",
              active
                ? "bg-surface-selected hover:bg-surface-selected"
                : "hover:bg-surface-hover",
            )}
          >
            <ActorAvatar
              actorType="agent"
              actorId={agent.id}
              size="sm"
              className="h-5 w-5 shrink-0"
            />
            <span className="min-w-0 flex-1 truncate">{agent.name}</span>
          </button>
        );
      })}
    </ListDetailRail>
  );

  return (
    <ListDetailLayout
      rail={rail}
      detail={<AgentDetailPage agentId={agentId} />}
    />
  );
}
