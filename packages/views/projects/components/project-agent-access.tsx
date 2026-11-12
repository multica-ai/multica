"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import type { MemberWithUser } from "@multica/core/types";
import { agentListOptions } from "@multica/core/workspace/queries";
import { Bot } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Popover, PopoverContent, PopoverTitle, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";

export function ProjectAgentAccess({ members, selected, userId }: {
  members: MemberWithUser[];
  selected: string[];
  userId?: string;
}) {
  const { t } = useT("projects");
  const { t: tc } = useT("common");
  const wsId = useWorkspaceId();
  const agents = useQuery(agentListOptions(wsId));
  const owners = new Map(members.map((member) => [member.user_id, member]));
  const memberIds = new Set(selected);
  const viewer = { userId: userId ?? null, role: owners.get(userId ?? "")?.role ?? null };
  const candidates = (agents.data ?? []).filter((agent) =>
    !agent.archived_at && canAssignAgentToIssue(agent, viewer).allowed,
  );
  const available = candidates.flatMap((agent) => {
    const owner = agent.owner_id ? owners.get(agent.owner_id) : undefined;
    return owner && (memberIds.has(owner.user_id) || owner.role === "owner" || owner.role === "admin")
      ? [{ agent, owner }]
      : [];
  });
  const unavailableCount = candidates.length - available.length;

  return (
    <div className="flex items-start gap-2 text-caption text-muted-foreground">
      <Bot className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
      <div className="min-w-0">
        {t(($) => $.access.agent_notice)}{" "}
        <Popover>
          <PopoverTrigger render={
            <Button type="button" variant="link" size="xs" className="h-auto whitespace-normal p-0 text-caption">
              {t(($) => $.access.view_agents)}{agents.isSuccess ? ` · ${available.length}` : null}
            </Button>
          } />
          <PopoverContent side="top" align="start" className="w-80 max-w-[calc(100vw-2rem)]">
            <PopoverTitle className="text-caption text-muted-foreground">{t(($) => $.access.agents_title)}</PopoverTitle>
            {agents.isPending ? (
              <div role="status" className="flex items-center justify-center gap-2 py-3 text-caption text-muted-foreground"><Spinner aria-hidden="true" />{tc(($) => $.loading)}</div>
            ) : agents.isError ? (
              <div role="alert" className="space-y-2 text-caption">
                <p>{t(($) => $.access.agents_load_failed)}</p>
                <Button type="button" variant="outline" size="xs" onClick={() => void agents.refetch()}>{t(($) => $.access.retry)}</Button>
              </div>
            ) : (
              <>
                {available.length > 0 ? (
                  <ul className="max-h-60 space-y-1 overflow-y-auto">
                    {available.map(({ agent, owner }) => (
                      <li key={agent.id} className="flex items-center gap-2 py-1">
                        <ActorAvatar actorType="agent" actorId={agent.id} size="sm" profileLink={false} />
                        <span className="min-w-0 flex-1 truncate text-body" title={agent.name}>{agent.name}</span>
                        <span className="max-w-1/2 truncate text-caption text-muted-foreground" title={owner.name}>
                          {owner.name}{owner.role === "owner" || owner.role === "admin" ? ` · ${owner.role}` : null}
                        </span>
                      </li>
                    ))}
                  </ul>
                ) : <p className="py-2 text-caption text-muted-foreground">{t(($) => $.access.no_agents)}</p>}
                {unavailableCount > 0 ? <p className="border-t pt-2 text-caption text-muted-foreground">{t(($) => $.access.unavailable_agents, { count: unavailableCount })}</p> : null}
              </>
            )}
          </PopoverContent>
        </Popover>
      </div>
    </div>
  );
}
