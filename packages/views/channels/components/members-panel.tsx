"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { Button } from "@multica/ui/components/ui/button";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import { Bot, UserPlus, X } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import {
  channelMembersOptions,
  useAddChannelMember,
  useRemoveChannelMember,
} from "@multica/core/channels";
import type {
  Agent,
  Channel,
  ChannelActorType,
  ChannelMembership,
  MemberWithUser,
} from "@multica/core/types";

interface MembersPanelProps {
  channel: Channel;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  enabled: boolean;
}

interface AddCandidate {
  type: ChannelActorType;
  id: string;
  label: string;
  sublabel?: string;
  avatarUrl?: string | null;
}

/**
 * MembersPanel — slide-over for viewing and managing a channel's members.
 *
 * Shows current members with avatars and roles. Channel admins (and
 * workspace owners/admins, by transitivity) can remove existing members
 * and add new ones via the picker at the bottom. Phase 1: anyone in the
 * channel can see the list; only admins can mutate.
 *
 * Add semantics: clicking a candidate calls AddChannelMember and the row
 * disappears from the picker (it's no longer a candidate). The optimistic
 * affordance is "candidate row vanishes immediately"; on error it
 * reappears via the query invalidation.
 */
export function MembersPanel({ channel, open, onOpenChange, enabled }: MembersPanelProps) {
  const wsId = useWorkspaceId();
  const selfUserId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members = [] } = useQuery(channelMembersOptions(channel.id, enabled));
  const { data: workspaceMembers = [] } = useQuery(memberListOptions(wsId));
  const { data: workspaceAgents = [] } = useQuery(agentListOptions(wsId));
  const addMember = useAddChannelMember(channel.id);
  const removeMember = useRemoveChannelMember(channel.id);

  const [filter, setFilter] = useState("");

  // Resolve memberships to display rows by joining against the cached
  // workspace member + agent lists. We fall back to a "Unknown …" row when
  // the join target is missing — happens when an agent has been archived
  // since being added, or a workspace member has left.
  const memberRows = useMemo(() => {
    const memberMap = new Map(workspaceMembers.map((m) => [m.user_id, m]));
    const agentMap = new Map(workspaceAgents.map((a) => [a.id, a]));
    return members.map((mem) => resolveMemberRow(mem, memberMap, agentMap));
  }, [members, workspaceMembers, workspaceAgents]);

  // Candidates for "Add member": every workspace member + active agent
  // that isn't already a member of this channel.
  const candidates = useMemo<AddCandidate[]>(() => {
    const existing = new Set(members.map((m) => `${m.member_type}:${m.member_id}`));
    const out: AddCandidate[] = [];
    for (const m of workspaceMembers) {
      if (existing.has(`member:${m.user_id}`)) continue;
      out.push({
        type: "member",
        id: m.user_id,
        label: m.name || m.email,
        sublabel: m.email,
        avatarUrl: m.avatar_url,
      });
    }
    for (const a of workspaceAgents) {
      if (a.archived_at) continue;
      if (existing.has(`agent:${a.id}`)) continue;
      out.push({
        type: "agent",
        id: a.id,
        label: a.name,
        sublabel: "agent",
      });
    }
    if (!filter) return out;
    const needle = filter.toLowerCase();
    return out.filter(
      (c) =>
        c.label.toLowerCase().includes(needle) ||
        (c.sublabel && c.sublabel.toLowerCase().includes(needle)),
    );
  }, [members, workspaceMembers, workspaceAgents, filter]);

  const handleAdd = (c: AddCandidate) => {
    addMember.mutate({
      member_type: c.type,
      member_id: c.id,
      role: "member",
    });
  };

  const handleRemove = (mem: ChannelMembership) => {
    removeMember.mutate({ memberType: mem.member_type, memberId: mem.member_id });
  };

  // For DMs we hide the entire member-management UI: DM membership is
  // determined by the participant set passed at creation, and adding a
  // third member would silently turn the DM into a private channel.
  const isDM = channel.kind === "dm";

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent>
        <SheetHeader>
          <SheetTitle>Members</SheetTitle>
          <SheetDescription>
            {isDM
              ? "Direct message participants. To talk to someone else, start a new DM."
              : "Workspace members and agents in this channel."}
          </SheetDescription>
        </SheetHeader>

        <div className="px-4 pb-4">
          <h3 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
            In this channel ({memberRows.length})
          </h3>
          <ul className="flex flex-col gap-1">
            {memberRows.map((row) => {
              const isSelf = row.type === "member" && row.id === selfUserId;
              return (
                <li
                  key={`${row.type}:${row.id}`}
                  className="flex items-center gap-3 rounded-md px-2 py-1.5 hover:bg-muted/40"
                >
                  <RowAvatar row={row} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-baseline gap-2">
                      <span className="truncate text-sm font-medium text-foreground">
                        {row.label}
                      </span>
                      {isSelf ? (
                        <span className="text-xs text-muted-foreground">you</span>
                      ) : null}
                    </div>
                    <div className="truncate text-xs text-muted-foreground">
                      {row.role === "admin" ? "admin" : row.sublabel ?? ""}
                    </div>
                  </div>
                  {!isDM && !isSelf && row.role !== "admin" ? (
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => handleRemove(row.raw)}
                      disabled={removeMember.isPending}
                      aria-label={`Remove ${row.label}`}
                      className="h-7 w-7 p-0"
                    >
                      <X className="h-4 w-4" />
                    </Button>
                  ) : null}
                </li>
              );
            })}
          </ul>
        </div>

        {!isDM && (
          <div className="border-t border-border px-4 py-4">
            <div className="mb-2 flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              <UserPlus className="h-3 w-3" /> Add to channel
            </div>
            <input
              type="text"
              placeholder="Search workspace members and agents…"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="mb-2 w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
            {candidates.length === 0 ? (
              <p className="py-4 text-center text-xs text-muted-foreground">
                {filter ? "No matches." : "Everyone is already in this channel."}
              </p>
            ) : (
              <ul className="flex flex-col gap-1">
                {candidates.map((c) => (
                  <li key={`${c.type}:${c.id}`}>
                    <button
                      type="button"
                      onClick={() => handleAdd(c)}
                      disabled={addMember.isPending}
                      className="flex w-full items-center gap-3 rounded-md px-2 py-1.5 text-left hover:bg-muted/60 disabled:cursor-not-allowed disabled:opacity-60"
                    >
                      <CandidateAvatar c={c} />
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm font-medium text-foreground">
                          {c.label}
                        </div>
                        {c.sublabel ? (
                          <div className="truncate text-xs text-muted-foreground">
                            {c.sublabel}
                          </div>
                        ) : null}
                      </div>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}

interface ResolvedMemberRow {
  type: ChannelActorType;
  id: string;
  label: string;
  sublabel?: string;
  avatarUrl?: string | null;
  role: string;
  raw: ChannelMembership;
}

function resolveMemberRow(
  mem: ChannelMembership,
  memberMap: Map<string, MemberWithUser>,
  agentMap: Map<string, Agent>,
): ResolvedMemberRow {
  if (mem.member_type === "member") {
    const m = memberMap.get(mem.member_id);
    return {
      type: "member",
      id: mem.member_id,
      label: m?.name || m?.email || "Unknown member",
      sublabel: m?.email,
      avatarUrl: m?.avatar_url,
      role: mem.role,
      raw: mem,
    };
  }
  const a = agentMap.get(mem.member_id);
  return {
    type: "agent",
    id: mem.member_id,
    label: a?.name || "Unknown agent",
    sublabel: "agent",
    role: mem.role,
    raw: mem,
  };
}

function RowAvatar({ row }: { row: ResolvedMemberRow }) {
  return (
    <Avatar className="h-8 w-8 shrink-0">
      {row.type === "member" && row.avatarUrl ? (
        <AvatarImage src={row.avatarUrl} alt={row.label} />
      ) : null}
      <AvatarFallback className={row.type === "agent" ? "bg-purple-100 text-purple-900" : ""}>
        {row.type === "agent" ? (
          <Bot className="h-4 w-4" />
        ) : (
          row.label.charAt(0).toUpperCase() || "?"
        )}
      </AvatarFallback>
    </Avatar>
  );
}

function CandidateAvatar({ c }: { c: AddCandidate }) {
  return (
    <Avatar className="h-7 w-7 shrink-0">
      {c.type === "member" && c.avatarUrl ? <AvatarImage src={c.avatarUrl} alt={c.label} /> : null}
      <AvatarFallback className={c.type === "agent" ? "bg-purple-100 text-purple-900" : ""}>
        {c.type === "agent" ? (
          <Bot className="h-3 w-3" />
        ) : (
          c.label.charAt(0).toUpperCase() || "?"
        )}
      </AvatarFallback>
    </Avatar>
  );
}
