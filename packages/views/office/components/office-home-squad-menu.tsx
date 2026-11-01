"use client";

import type { ReactElement } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { officeSquadMembersOptions, useSetAgentHomeSquad } from "@multica/core/office";
import { squadListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Agent } from "@multica/core/types";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../../i18n";

/**
 * Where an agent is drawn: its home squad. The menu lists the squads the agent
 * belongs to (the server only accepts a squad it is a member of) and, when one
 * is set, a way to clear it. The trigger is the caller's, so the agent panel
 * and the unseated tray share the same menu.
 */
export function OfficeHomeSquadMenu({
  agent,
  trigger,
  align = "end",
}: {
  agent: Agent;
  trigger: ReactElement;
  align?: "start" | "end";
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={trigger} />
      <DropdownMenuContent align={align} className="w-56">
        <HomeSquadItems agent={agent} />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function HomeSquadItems({ agent }: { agent: Agent }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const live = squads.filter((squad) => !squad.archived_at);
  const members = useQueries({
    queries: live.map((squad) => officeSquadMembersOptions(wsId, squad.id)),
  });
  const setHome = useSetAgentHomeSquad();

  const choose = (squadId: string | null, name: string) => {
    setHome.mutate(
      { agentId: agent.id, squadId },
      {
        onSuccess: () =>
          toast.success(
            squadId
              ? t(($) => $.office.home_squad_set, { name: agent.name, squad: name })
              : t(($) => $.office.home_squad_cleared, { name: agent.name }),
          ),
        onError: () => toast.error(t(($) => $.office.home_squad_failed)),
      },
    );
  };

  const candidates = live.filter((_, index) =>
    members[index]?.data?.some((member) => member.member_type === "agent" && member.member_id === agent.id),
  );

  return (
    <>
      {candidates.length === 0 ? (
        <DropdownMenuItem disabled>{t(($) => $.office.home_squad_empty)}</DropdownMenuItem>
      ) : (
        candidates.map((squad) => (
          <DropdownMenuItem
            key={squad.id}
            disabled={squad.id === agent.home_squad_id}
            onClick={() => choose(squad.id, squad.name)}
          >
            <span className="min-w-0 flex-1 truncate">{squad.name}</span>
            {squad.id === agent.home_squad_id ? (
              <span className="text-caption text-muted-foreground">{t(($) => $.office.home_squad_current)}</span>
            ) : null}
          </DropdownMenuItem>
        ))
      )}
      {agent.home_squad_id ? (
        <>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => choose(null, "")}>{t(($) => $.office.home_squad_clear)}</DropdownMenuItem>
        </>
      ) : null}
    </>
  );
}
