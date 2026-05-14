"use client";

// JEH-1067 (Bundle B) — Groups list view rendered inside Settings → Groups.
// The legacy inline card layout (members/permissions/runtimes/agents expanded
// per-card) has moved to the dedicated Group detail page (`group-detail.tsx`)
// — this tab is now a compact table that links into it.
//
// Columns mirror the issue spec:
//   Navn · Members · Agents · Runtimes · Oprettet af · Capabilities
//
// Counts are fetched per-row via the existing groupMembers/groupAgents/
// groupRuntimes/groupCapabilities query options. This is N+3 queries per
// group, which is fine for the small number of groups a workspace typically
// has and avoids a new batch endpoint.

import { useMemo, useState } from "react";
import { Plus, Users } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useCreateCerebroGroup } from "../mutations";
import {
  groupListOptions,
  groupMembersOptions,
  groupCapabilitiesOptions,
  groupRuntimesOptions,
  groupAgentsOptions,
} from "../queries";
import type { CerebroGroup } from "../types";

export interface GroupsTabProps {
  /**
   * Called when a group row is clicked. The app layer is responsible for
   * navigating to the Group detail page — keeps cerebro-groups decoupled
   * from `@multica/views/navigation` (would otherwise be a cycle, since
   * @multica/views already depends on @multica/cerebro-groups).
   * Defaults to a no-op so existing callers that haven't wired navigation
   * yet still render without crashing.
   */
  onSelectGroup?: (groupId: string) => void;
}

export function GroupsTab({ onSelectGroup }: GroupsTabProps = {}) {
  const enabled = useFeatureFlag("cerebro_groups_enabled");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: groups = [], isPending } = useQuery(groupListOptions(wsId));

  const currentMember = members.find((m) => m.user_id === user?.id);
  const isAdmin =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const memberById = useMemo(() => {
    const m = new Map<string, string>();
    for (const ws of members) m.set(ws.user_id, ws.name);
    return m;
  }, [members]);

  if (!enabled) return null;

  return (
    <div className="space-y-6 max-w-3xl" data-testid="groups-tab">
      <div>
        <h3 className="text-sm font-semibold mb-1">Groups</h3>
        <p className="text-xs text-muted-foreground max-w-prose">
          Named collections of workspace members. Use groups to control which
          runtimes, agents, and projects a team can access. Workspace owners
          and admins always have access to everything regardless of group
          membership.
        </p>
      </div>

      {isAdmin && <CreateGroupForm />}

      {isPending ? (
        <div className="text-xs text-muted-foreground">Loading groups…</div>
      ) : groups.length === 0 ? (
        <div className="rounded-md border border-dashed border-border p-6 text-center text-xs text-muted-foreground">
          No groups yet.{" "}
          {isAdmin
            ? "Create one above to start organising workspace access."
            : "An admin can create groups in this workspace."}
        </div>
      ) : (
        <div
          className="overflow-x-auto rounded-md border border-border"
          data-testid="groups-table-wrapper"
        >
          <table
            className="w-full text-left text-sm"
            data-testid="groups-table"
          >
            <thead className="bg-muted/40 text-xs uppercase tracking-wide text-muted-foreground">
              <tr>
                <th className="px-3 py-2 font-medium">Navn</th>
                <th className="px-3 py-2 font-medium text-right">Members</th>
                <th className="px-3 py-2 font-medium text-right">Agents</th>
                <th className="px-3 py-2 font-medium text-right">Runtimes</th>
                <th className="px-3 py-2 font-medium">Oprettet af</th>
                <th className="px-3 py-2 font-medium">Capabilities</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/60">
              {groups.map((g) => (
                <GroupRow
                  key={g.id}
                  group={g}
                  createdByName={
                    g.created_by ? memberById.get(g.created_by) ?? null : null
                  }
                  onSelect={onSelectGroup}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function CreateGroupForm() {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const create = useCreateCerebroGroup();

  const trimmed = name.trim();
  const handleSubmit = async () => {
    if (!trimmed) return;
    try {
      await create.mutateAsync({
        name: trimmed,
        description: description.trim(),
      });
      setName("");
      setDescription("");
      toast.success(`Group "${trimmed}" created`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to create group");
    }
  };

  return (
    <div
      className="rounded-md border border-border bg-muted/30 p-4 space-y-3"
      data-testid="create-group-form"
    >
      <div className="text-sm font-medium">Opret gruppe</div>
      <Input
        placeholder="Gruppenavn (fx Engineering)"
        value={name}
        onChange={(e) => setName(e.target.value)}
        aria-label="Group name"
      />
      <Textarea
        placeholder="Valgfri beskrivelse"
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        aria-label="Group description"
        rows={2}
      />
      <div className="flex justify-end">
        <Button
          size="sm"
          onClick={handleSubmit}
          disabled={!trimmed || create.isPending}
        >
          <Plus className="size-4 mr-1.5" />
          {create.isPending ? "Opretter…" : "Opret gruppe"}
        </Button>
      </div>
    </div>
  );
}

function GroupRow({
  group,
  createdByName,
  onSelect,
}: {
  group: CerebroGroup;
  createdByName: string | null;
  onSelect?: (groupId: string) => void;
}) {
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(groupMembersOptions(wsId, group.id));
  const { data: agents = [] } = useQuery(groupAgentsOptions(wsId, group.id));
  const { data: runtimes = [] } = useQuery(
    groupRuntimesOptions(wsId, group.id),
  );
  const { data: capabilities = [] } = useQuery(
    groupCapabilitiesOptions(wsId, group.id),
  );

  const handleClick = () => {
    onSelect?.(group.id);
  };

  return (
    <tr
      onClick={handleClick}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          handleClick();
        }
      }}
      tabIndex={onSelect ? 0 : -1}
      role={onSelect ? "button" : undefined}
      data-testid="group-row"
      className={
        onSelect
          ? "cursor-pointer hover:bg-muted/40 focus:bg-muted/40 focus:outline-none"
          : undefined
      }
    >
      <td className="px-3 py-2">
        <div className="flex items-center gap-2 min-w-0">
          <Users className="size-3.5 text-muted-foreground shrink-0" />
          <span className="min-w-0">
            <span className="block truncate font-medium">{group.name}</span>
            {group.description && (
              <span className="block truncate text-xs text-muted-foreground">
                {group.description}
              </span>
            )}
          </span>
        </div>
      </td>
      <td className="px-3 py-2 text-right tabular-nums">{members.length}</td>
      <td className="px-3 py-2 text-right tabular-nums">{agents.length}</td>
      <td className="px-3 py-2 text-right tabular-nums">{runtimes.length}</td>
      <td className="px-3 py-2 truncate text-xs text-muted-foreground">
        {createdByName ?? "—"}
      </td>
      <td className="px-3 py-2">
        <div className="flex flex-wrap gap-1">
          {capabilities.some((c) => c.capability === "create_runtime") && (
            <Badge
              variant="outline"
              className="text-[10px]"
              data-testid="group-cap-badge-create-runtime"
            >
              create_runtime
            </Badge>
          )}
          {capabilities.some((c) => c.capability === "create_agent") && (
            <Badge
              variant="outline"
              className="text-[10px]"
              data-testid="group-cap-badge-create-agent"
            >
              create_agent
            </Badge>
          )}
          {capabilities.length === 0 && (
            <span className="text-xs text-muted-foreground">—</span>
          )}
        </div>
      </td>
    </tr>
  );
}
