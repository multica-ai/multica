"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { getCoreRowModel, useReactTable } from "@tanstack/react-table";
import type {
  Agent,
  AgentRuntime,
  AgentTask,
  MemberWithUser,
} from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { latestCliVersionOptions } from "@multica/core/runtimes";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import { DataTable } from "@multica/ui/components/ui/data-table";
import { useNavigation } from "../../navigation";
import { type RuntimeRow, createRuntimeColumns } from "./runtime-columns";
import { useT } from "../../i18n";

interface RuntimeWorkload {
  agentIds: string[];
  runningCount: number;
  queuedCount: number;
}

const EMPTY_WORKLOAD: RuntimeWorkload = {
  agentIds: [],
  runningCount: 0,
  queuedCount: 0,
};

// Per-runtime workload snapshot — agent IDs serving this runtime (drives
// the avatar stack; .length doubles as the agent count) plus task counts
// split by status. Built once per render off the workspace-wide
// agents / agent-task-snapshot caches; filtered locally — no extra requests.
function buildWorkloadIndex(
  agents: Agent[],
  tasks: AgentTask[],
): Map<string, RuntimeWorkload> {
  const result = new Map<string, RuntimeWorkload>();
  const agentToRuntime = new Map<string, string>();

  for (const a of agents) {
    if (!a.runtime_id) continue;
    agentToRuntime.set(a.id, a.runtime_id);
    const entry =
      result.get(a.runtime_id) ?? {
        agentIds: [],
        runningCount: 0,
        queuedCount: 0,
      };
    entry.agentIds.push(a.id);
    result.set(a.runtime_id, entry);
  }
  for (const t of tasks) {
    const rid = agentToRuntime.get(t.agent_id);
    if (!rid) continue;
    const entry = result.get(rid);
    if (!entry) continue;
    if (t.status === "running") entry.runningCount += 1;
    else if (t.status === "queued" || t.status === "dispatched")
      entry.queuedCount += 1;
  }
  return result;
}

export function RuntimeList({
  runtimes,
  updatableIds,
  now,
}: {
  runtimes: AgentRuntime[];
  // Kept on the API surface for callers — the CLI column re-derives
  // update state per row via metadata.cli_version + the GitHub-release
  // query, so this prop is now unused. Left to avoid scope creep on the
  // page-level wrapper that still computes the set.
  updatableIds?: Set<string>;
  now: number;
}) {
  void updatableIds;

  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const slug = useWorkspaceSlug();
  const navigation = useNavigation();
  const user = useAuthStore((s) => s.user);

  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  const { data: latestCliVersion = null } = useQuery(latestCliVersionOptions());

  const currentMember = user
    ? members.find((m) => m.user_id === user.id)
    : null;
  const isAdmin = currentMember
    ? currentMember.role === "owner" || currentMember.role === "admin"
    : false;

  const workloadIndex = useMemo(
    () => buildWorkloadIndex(agents, snapshot),
    [agents, snapshot],
  );

  const memberById = useMemo(() => {
    const map = new Map<string, MemberWithUser>();
    for (const m of members) map.set(m.user_id, m);
    return map;
  }, [members]);

  // Owner column only earns its space when the page actually has multiple
  // distinct owners — otherwise it would just be a column of identical
  // avatars.
  const showOwner = useMemo(() => {
    const owners = new Set<string>();
    for (const r of runtimes) {
      if (r.owner_id) owners.add(r.owner_id);
    }
    return owners.size > 1;
  }, [runtimes]);

  const rows = useMemo<RuntimeRow[]>(() => {
    return runtimes.map((runtime) => ({
      runtime,
      ownerMember: runtime.owner_id
        ? memberById.get(runtime.owner_id) ?? null
        : null,
      workload: workloadIndex.get(runtime.id) ?? EMPTY_WORKLOAD,
      canDelete: isAdmin || (!!user && runtime.owner_id === user.id),
    }));
  }, [runtimes, memberById, workloadIndex, isAdmin, user]);

  const columns = useMemo(
    () =>
      createRuntimeColumns({
        showOwner,
        latestCliVersion,
        wsId,
        now,
        t,
      }),
    [showOwner, latestCliVersion, wsId, now, t],
  );

  const table = useReactTable({
    data: rows,
    columns,
    getCoreRowModel: getCoreRowModel(),
    enableColumnResizing: true,
    // Pin the kebab column right so it stays accessible during horizontal
    // scroll — matches the pattern in Linear / Notion / GitHub.
    initialState: { columnPinning: { right: ["actions"] } },
  });

  return (
    <DataTable
      table={table}
      onRowClick={(row) => {
        if (!slug) return;
        navigation.push(
          paths.workspace(slug).runtimeDetail(row.original.runtime.id),
        );
      }}
    />
  );
}
