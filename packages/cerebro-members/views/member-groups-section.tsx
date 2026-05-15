"use client";

// JEH-1067 (Bundle B / PR-B) — Member detail "Groups" section.
//
// Lists the cerebro groups this user is a member of, with admin-only add /
// remove buttons. Group membership is derived client-side by intersecting
// the workspace's groupList with each group's member roster.

import { useMemo, useState } from "react";
import { Plus, Users, X } from "lucide-react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  groupListOptions,
  groupMembersOptions,
} from "@multica/cerebro-groups";
import {
  useAddCerebroGroupMember,
  useRemoveCerebroGroupMember,
} from "@multica/cerebro-groups/mutations";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";

export interface MemberGroupsSectionProps {
  /** workspace user_id of the member (NOT the member row id). */
  userId: string;
  isAdmin: boolean;
}

export function MemberGroupsSection({
  userId,
  isAdmin,
}: MemberGroupsSectionProps) {
  const wsId = useWorkspaceId();
  const { data: groups = [], isPending } = useQuery(groupListOptions(wsId));

  if (isPending) {
    return (
      <SectionFrame>
        <p className="text-xs text-muted-foreground">Loading groups...</p>
      </SectionFrame>
    );
  }

  return (
    <SectionFrame>
      <MemberGroupsInner
        userId={userId}
        isAdmin={isAdmin}
        groups={groups}
      />
    </SectionFrame>
  );
}

function MemberGroupsInner({
  userId,
  isAdmin,
  groups,
}: {
  userId: string;
  isAdmin: boolean;
  groups: { id: string; name: string; description: string | null }[];
}) {
  const wsId = useWorkspaceId();
  const [pickerOpen, setPickerOpen] = useState(false);
  const [search, setSearch] = useState("");
  const membership = useGroupMembershipMap(wsId, groups, userId);

  const inGroup = useMemo(
    () => groups.filter((g) => membership.get(g.id) === true),
    [groups, membership],
  );
  const notInGroup = useMemo(
    () =>
      groups
        .filter((g) => membership.get(g.id) === false)
        .filter((g) => {
          if (search === "") return true;
          const q = search.toLowerCase();
          return (
            g.name.toLowerCase().includes(q) ||
            (g.description ?? "").toLowerCase().includes(q)
          );
        }),
    [groups, membership, search],
  );

  return (
    <>
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-medium">Groups ({inGroup.length})</h2>
        {isAdmin && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => setPickerOpen((s) => !s)}
          >
            <Plus className="size-3.5 mr-1.5" /> Add to group
          </Button>
        )}
      </div>

      {inGroup.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          {isAdmin
            ? "Brugeren er ikke i nogen gruppe endnu."
            : "Brugeren er ikke i nogen gruppe."}
        </p>
      ) : (
        <ul
          className="divide-y divide-border/50 rounded-md border border-border/50"
          data-testid="member-groups-list"
        >
          {inGroup.map((g) => (
            <li
              key={g.id}
              className="flex items-center gap-3 px-3 py-2 text-sm"
            >
              <Users className="size-3.5 text-muted-foreground shrink-0" />
              <span className="flex-1 min-w-0">
                <span className="block truncate">{g.name}</span>
                {g.description && (
                  <span className="block truncate text-xs text-muted-foreground">
                    {g.description}
                  </span>
                )}
              </span>
              {isAdmin && (
                <RemoveFromGroupButton groupId={g.id} userId={userId} />
              )}
            </li>
          ))}
        </ul>
      )}

      {pickerOpen && isAdmin && (
        <div
          className="rounded-md border border-border bg-muted/30 p-3 space-y-2"
          data-testid="member-groups-picker"
        >
          <Input
            placeholder="Search groups..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            aria-label="Search groups"
          />
          <ul className="max-h-60 overflow-auto divide-y divide-border/50">
            {notInGroup.length === 0 && (
              <li className="text-xs text-muted-foreground py-2 px-1">
                {groups.length === inGroup.length
                  ? "Brugeren er allerede medlem af alle grupper."
                  : "Ingen matches."}
              </li>
            )}
            {notInGroup.map((g) => (
              <li
                key={g.id}
                className="flex items-center gap-3 px-1 py-2 text-sm"
              >
                <span className="flex-1 min-w-0 truncate">{g.name}</span>
                <AddToGroupButton groupId={g.id} userId={userId} />
              </li>
            ))}
          </ul>
        </div>
      )}
    </>
  );
}

function useGroupMembershipMap(
  wsId: string,
  groups: { id: string }[],
  userId: string,
): Map<string, boolean | undefined> {
  const results = useQueries({
    queries: groups.map((g) => groupMembersOptions(wsId, g.id)),
  });
  return useMemo(() => {
    const m = new Map<string, boolean | undefined>();
    groups.forEach((g, idx) => {
      const r = results[idx];
      m.set(
        g.id,
        r?.isPending ? undefined : r?.data?.some((row) => row.user_id === userId),
      );
    });
    return m;
  }, [groups, results, userId]);
}

function AddToGroupButton({
  groupId,
  userId,
}: {
  groupId: string;
  userId: string;
}) {
  const add = useAddCerebroGroupMember(groupId);
  const handleClick = async () => {
    try {
      await add.mutateAsync(userId);
      toast.success("Member added to group");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to add to group");
    }
  };
  return (
    <Button
      size="sm"
      variant="outline"
      onClick={handleClick}
      disabled={add.isPending}
    >
      Add
    </Button>
  );
}

function RemoveFromGroupButton({
  groupId,
  userId,
}: {
  groupId: string;
  userId: string;
}) {
  const remove = useRemoveCerebroGroupMember(groupId);
  const handleClick = async () => {
    try {
      await remove.mutateAsync(userId);
      toast.success("Member removed from group");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to remove from group");
    }
  };
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      onClick={handleClick}
      disabled={remove.isPending}
      aria-label="Remove from group"
    >
      <X className="size-4 text-muted-foreground" />
    </Button>
  );
}

function SectionFrame({ children }: { children: React.ReactNode }) {
  return (
    <section
      className="rounded-md border border-border p-4 space-y-3"
      data-testid="member-groups-section"
    >
      {children}
    </section>
  );
}

export function MemberGroupChips({
  userId,
  maxVisible = 3,
}: {
  userId: string;
  maxVisible?: number;
}) {
  const wsId = useWorkspaceId();
  const { data: groups = [] } = useQuery(groupListOptions(wsId));
  const membership = useGroupMembershipMap(wsId, groups, userId);
  const inGroup = groups.filter((g) => membership.get(g.id) === true);

  if (inGroup.length === 0) {
    return <span className="text-xs text-muted-foreground">-</span>;
  }

  const visible = inGroup.slice(0, maxVisible);
  const overflow = inGroup.length - visible.length;

  return (
    <div
      className="flex flex-wrap items-center gap-1"
      data-testid="member-group-chips"
    >
      {visible.map((g) => (
        <Badge
          key={g.id}
          variant="outline"
          className="text-[10px]"
          data-testid="member-group-chip"
        >
          {g.name}
        </Badge>
      ))}
      {overflow > 0 && (
        <Badge variant="secondary" className="text-[10px]">
          +{overflow}
        </Badge>
      )}
    </div>
  );
}
