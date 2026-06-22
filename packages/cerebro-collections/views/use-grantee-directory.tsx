// Loads the four named grantee directories (groups, members, agents, runtimes)
// and exposes (a) option lists per grantee type for the "add grant" picker and
// (b) a label resolver that turns a stored (grantee_type, grantee_id) into a
// human name for the grant rows. Lists are only fetched while the editor is
// open (enabled flag), so closed folder rows cost nothing.

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { groupListOptions } from "@multica/cerebro-groups";
import type { GranteeType } from "../types";

export interface GranteeOption {
  id: string;
  name: string;
}

export interface GranteeDirectory {
  options: Record<Exclude<GranteeType, "workspace">, GranteeOption[]>;
  labelFor: (type: GranteeType, id: string | null) => string;
}

export function useGranteeDirectory(enabled: boolean): GranteeDirectory {
  const wsId = useWorkspaceId();
  const on = Boolean(enabled && wsId);

  const { data: groups = [] } = useQuery({
    ...groupListOptions(wsId),
    enabled: on,
  });
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: on,
  });
  const { data: agents = [] } = useQuery({
    ...agentListOptions(wsId),
    enabled: on,
  });
  const { data: runtimes = [] } = useQuery({
    ...runtimeListOptions(wsId),
    enabled: on,
  });

  const options: GranteeDirectory["options"] = {
    group: groups.map((g) => ({ id: g.id, name: g.name })),
    member: members.map((m) => ({
      id: m.user_id,
      name: m.name || m.email || m.user_id,
    })),
    agent: agents.map((a) => ({ id: a.id, name: a.name })),
    runtime: runtimes.map((r) => ({ id: r.id, name: r.name })),
  };

  function labelFor(type: GranteeType, id: string | null): string {
    if (type === "workspace") return "Whole workspace";
    if (!id) return type;
    const list =
      type === "group"
        ? options.group
        : type === "member"
          ? options.member
          : type === "agent"
            ? options.agent
            : options.runtime;
    const hit = list.find((o) => o.id === id);
    // Fall back to a short id when the directory hasn't loaded the grantee
    // (e.g. an archived agent or a member who left) so a row never renders blank.
    return hit?.name ?? `${type} ${id.slice(0, 8)}`;
  }

  return { options, labelFor };
}

export const GRANTEE_TYPE_LABELS: Record<GranteeType, string> = {
  group: "Group",
  member: "Member",
  workspace: "Whole workspace",
  agent: "Agent",
  runtime: "Runtime",
};

export const ROLE_LABELS = {
  viewer: "Viewer",
  editor: "Editor",
  full_access: "Full access",
} as const;
