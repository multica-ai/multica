import { useMemo } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { companySquadMembersOptions } from "@multica/core/company";
import {
  squadListOptions,
  squadSkillsOptions,
} from "@multica/core/workspace/queries";

export interface InheritedSquadSkill {
  id: string;
  name: string;
  /** The squads that apply this skill to the agent. */
  squadNames: string[];
}

/**
 * Skills an agent receives through the squads it is a member of. These are
 * delivered on every task (see ListEffectiveAgentSkills on the server), so the
 * agent detail view lists them as read-only. Membership mirrors the server:
 * only squad_member rows count, and archived squads are skipped.
 */
export function useInheritedSquadSkills(
  wsId: string,
  agentId: string,
): InheritedSquadSkill[] {
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const activeSquads = useMemo(
    () => squads.filter((squad) => !squad.archived_at),
    [squads],
  );

  const memberResults = useQueries({
    queries: activeSquads.map((squad) =>
      companySquadMembersOptions(wsId, squad.id),
    ),
  });
  const memberSquads = useMemo(
    () =>
      activeSquads.filter((_, index) =>
        (memberResults[index]?.data ?? []).some(
          (member) =>
            member.member_type === "agent" && member.member_id === agentId,
        ),
      ),
    [activeSquads, memberResults, agentId],
  );

  const skillResults = useQueries({
    queries: memberSquads.map((squad) => squadSkillsOptions(wsId, squad.id)),
  });

  return useMemo(() => {
    const byId = new Map<string, InheritedSquadSkill>();
    memberSquads.forEach((squad, index) => {
      for (const skill of skillResults[index]?.data ?? []) {
        const entry = byId.get(skill.id) ?? {
          id: skill.id,
          name: skill.name,
          squadNames: [],
        };
        entry.squadNames.push(squad.name);
        byId.set(skill.id, entry);
      }
    });
    return [...byId.values()].sort((a, b) => a.name.localeCompare(b.name));
  }, [memberSquads, skillResults]);
}
