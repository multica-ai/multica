// Resolves a holder's (layer, subject_id) into a human name for the
// per-permission detail page (FIR-3091 punkt 8). The holders endpoint returns
// raw subject UUIDs; each layer names a different subject kind:
//
//   agent     -> an agent            (agents list, match on id)
//   user      -> a workspace member  (members list, match on user_id)
//   group     -> a cerebro group     (groups list, match on id)
//   runtime   -> a runtime           (runtimes list, match on id)
//   workspace -> the workspace itself (no id lookup)
//   system    -> an autopilot/system actor (no dedicated list; short id)
//
// Lists are only fetched while the page is open (enabled flag), so a closed
// page costs nothing. Groups are read via api.cerebroRequest directly rather
// than through @multica/cerebro-groups, because that package depends on this
// one — importing it back would close a package dependency cycle.

import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes/queries";

const GroupListSchema = z
  .array(
    z
      .object({ id: z.string().default(""), name: z.string().default("") })
      .loose(),
  )
  .default([]);

type NamedSubject = { id: string; name: string };

export interface HolderDirectory {
  labelFor: (layer: string, subjectId: string) => string;
  isLoading: boolean;
}

export function useHolderDirectory(
  wsId: string,
  enabled: boolean,
): HolderDirectory {
  const on = Boolean(enabled && wsId);

  const agents = useQuery({ ...agentListOptions(wsId), enabled: on });
  const members = useQuery({ ...memberListOptions(wsId), enabled: on });
  const runtimes = useQuery({ ...runtimeListOptions(wsId), enabled: on });
  const groups = useQuery({
    queryKey: ["cerebro", "groups", wsId, "list"],
    queryFn: async (): Promise<NamedSubject[]> => {
      const raw = await api.listCerebroGroups<unknown>(wsId);
      return parseWithFallback(raw, GroupListSchema, [], {
        endpoint: "listCerebroGroups",
      });
    },
    enabled: on,
  });

  const agentList: NamedSubject[] = (agents.data ?? []).map((a) => ({
    id: a.id,
    name: a.name,
  }));
  const memberList: NamedSubject[] = (members.data ?? []).map((m) => ({
    id: m.user_id,
    name: m.name || m.email || m.user_id,
  }));
  const runtimeList: NamedSubject[] = (runtimes.data ?? []).map((r) => ({
    id: r.id,
    name: r.name,
  }));
  const groupList: NamedSubject[] = groups.data ?? [];

  function shortId(id: string): string {
    return id ? id.slice(0, 8) : "";
  }

  function labelFor(layer: string, subjectId: string): string {
    switch (layer) {
      case "workspace":
        return "Whole workspace";
      case "system":
        // No directory for autopilots/system actors — keep a stable, readable
        // fallback rather than a blank cell.
        return subjectId ? `System ${shortId(subjectId)}` : "System";
      case "agent":
      case "user":
      case "group":
      case "runtime": {
        if (!subjectId) return layer;
        const list =
          layer === "agent"
            ? agentList
            : layer === "user"
              ? memberList
              : layer === "group"
                ? groupList
                : runtimeList;
        const hit = list.find((o) => o.id === subjectId);
        // Fall back to a short id when the directory hasn't loaded the subject
        // (e.g. an archived agent or a member who left) so a row never blanks.
        return hit?.name ?? `${layer} ${shortId(subjectId)}`;
      }
      default:
        // Enum drift: an unknown future layer downgrades to a readable label
        // instead of crashing.
        return subjectId ? `${layer} ${shortId(subjectId)}` : layer;
    }
  }

  return {
    labelFor,
    isLoading:
      agents.isLoading ||
      members.isLoading ||
      runtimes.isLoading ||
      groups.isLoading,
  };
}
