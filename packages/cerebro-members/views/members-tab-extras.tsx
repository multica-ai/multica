"use client";

// JEH-1067 (Bundle B / PR-B) — Cerebro extras for the upstream MembersTab.
//
// Returns the trio `{ toolbar, filterMember, renderRowExtras }` that the
// patched upstream MembersTab accepts as its single `cerebroExtras` prop.
// Implements:
//   - `Groups` column: per-row chip list of the cerebro groups the member
//     belongs to (max 3 visible + `+N` overflow).
//   - `Vis kun members i gruppe X` filter dropdown rendered in the toolbar.
//
// State is owned here — the upstream tab stays stateless about cerebro.

import { useCallback, useMemo, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import type { MemberWithUser } from "@multica/core/types";
import {
  groupListOptions,
  groupMembersOptions,
} from "@multica/cerebro-groups";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import { Button } from "@multica/ui/components/ui/button";
import { MemberGroupChips } from "./member-groups-section";

const ALL_GROUPS_VALUE = "__all__";

export interface MembersTabCerebroExtras {
  toolbar?: React.ReactNode;
  filterMember?: (m: MemberWithUser) => boolean;
  renderRowExtras?: (m: MemberWithUser) => React.ReactNode;
}

/**
 * Hook used by the settings-page CEREBRO-PATCH to build the `cerebroExtras`
 * prop passed to upstream MembersTab. Pulling this into a hook keeps the
 * upstream patch a single line.
 */
export function useMembersTabCerebroExtras(): MembersTabCerebroExtras {
  const wsId = useWorkspaceId();
  const { data: groups = [] } = useQuery(groupListOptions(wsId));
  const memberRosters = useQueries({
    queries: groups.map((g) => groupMembersOptions(wsId, g.id)),
  });
  const [selectedGroupId, setSelectedGroupId] = useState<string | null>(null);

  // Membership index: groupId -> Set<user_id>. Built once per data update.
  const membershipByGroup = useMemo(() => {
    const m = new Map<string, Set<string>>();
    groups.forEach((g, idx) => {
      const rows = memberRosters[idx]?.data ?? [];
      m.set(g.id, new Set(rows.map((r) => r.user_id)));
    });
    return m;
  }, [groups, memberRosters]);

  const filterMember = useCallback(
    (member: MemberWithUser) => {
      if (!selectedGroupId) return true;
      return (
        membershipByGroup.get(selectedGroupId)?.has(member.user_id) === true
      );
    },
    [selectedGroupId, membershipByGroup],
  );

  const renderRowExtras = useCallback(
    (member: MemberWithUser) => (
      <MemberGroupChips userId={member.user_id} />
    ),
    [],
  );

  const toolbar = (
    <div
      className="flex items-center gap-2"
      data-testid="members-tab-groups-filter"
    >
      <span className="text-xs text-muted-foreground">Vis kun members i</span>
      <Select
        value={selectedGroupId ?? ALL_GROUPS_VALUE}
        onValueChange={(v) =>
          setSelectedGroupId(v === ALL_GROUPS_VALUE ? null : v)
        }
      >
        <SelectTrigger size="sm" className="w-48">
          <SelectValue placeholder="Alle medlemmer" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL_GROUPS_VALUE}>Alle medlemmer</SelectItem>
          {groups.map((g) => (
            <SelectItem key={g.id} value={g.id}>
              {g.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {selectedGroupId && (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => setSelectedGroupId(null)}
          data-testid="members-tab-groups-filter-clear"
        >
          Ryd filter
        </Button>
      )}
    </div>
  );

  return { toolbar, filterMember, renderRowExtras };
}
