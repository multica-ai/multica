"use client";

import { useMemo } from "react";
import { useQuery, useQueries } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { useRequiredWorkspaceSlug, paths } from "@multica/core/paths";
import { channelsListOptions, channelMembersOptions } from "@multica/core/channels";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { AppLink } from "../../navigation";
import { Button } from "@multica/ui/components/ui/button";
import { Hash, Lock, MessageCircle, Plus, Bot } from "lucide-react";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import type {
  Channel,
  ChannelMembership,
  MemberWithUser,
  Agent,
} from "@multica/core/types";

interface ChannelListProps {
  activeChannelId: string | null;
  onCreateChannel: () => void;
  onCreateDM: () => void;
  enabled: boolean;
}

function categorize(channels: Channel[]) {
  const publicChannels: Channel[] = [];
  const privateChannels: Channel[] = [];
  const dms: Channel[] = [];
  for (const c of channels) {
    if (c.kind === "dm") dms.push(c);
    else if (c.visibility === "public") publicChannels.push(c);
    else privateChannels.push(c);
  }
  return { publicChannels, privateChannels, dms };
}

function rowIcon(c: Channel) {
  if (c.kind === "dm") return MessageCircle;
  if (c.visibility === "private") return Lock;
  return Hash;
}

/**
 * dmDisplayName picks the "other" participant from a DM's membership list
 * and resolves them against the cached members + agents lists. Falls back
 * to a placeholder when memberships haven't loaded yet — render flickers
 * for ~50ms on first paint, which is acceptable for a sidebar.
 */
function dmDisplayName(
  selfUserId: string | null,
  members: ChannelMembership[],
  workspaceMembers: Map<string, MemberWithUser>,
  agents: Map<string, Agent>,
): { label: string; type: "member" | "agent" | "self" | "unknown"; avatarUrl?: string | null } {
  if (members.length === 0) {
    return { label: "Direct message", type: "unknown" };
  }
  // The "other" participant is anyone in the membership set who isn't the
  // current user. For self-DMs (DM where the only participant is you) we
  // fall through to the self branch below.
  const others = members.filter(
    (m) => !(m.member_type === "member" && m.member_id === selfUserId),
  );
  if (others.length === 0) {
    return { label: "Notes to self", type: "self" };
  }
  const o = others[0]!;
  if (o.member_type === "member") {
    const wm = workspaceMembers.get(o.member_id);
    return {
      label: wm?.name || wm?.email || "Unknown member",
      type: "member",
      avatarUrl: wm?.avatar_url,
    };
  }
  const a = agents.get(o.member_id);
  return { label: a?.name || "Unknown agent", type: "agent" };
}

/**
 * ChannelList is the left-pane sidebar inside the Channels page. Three
 * sections (Channels / Private / Direct messages); each section header
 * has its own `+` affordance so the create action sits next to the
 * surface it creates into.
 */
export function ChannelList({
  activeChannelId,
  onCreateChannel,
  onCreateDM,
  enabled,
}: ChannelListProps) {
  const wsId = useWorkspaceId();
  const slug = useRequiredWorkspaceSlug();
  const selfUserId = useAuthStore((s) => s.user?.id ?? null);
  const { data: channels = [], isLoading } = useQuery(channelsListOptions(wsId, enabled));
  const { data: workspaceMembers = [] } = useQuery(memberListOptions(wsId));
  const { data: workspaceAgents = [] } = useQuery(agentListOptions(wsId));
  const { publicChannels, privateChannels, dms } = categorize(channels);

  // Fetch members for every DM in parallel so the sidebar can resolve the
  // "other participant" label. useQueries is fine for the small N we
  // expect here; if a workspace ever has hundreds of DMs we should
  // denormalize the participant info into the channel response instead.
  const dmMembersResults = useQueries({
    queries: dms.map((c) => channelMembersOptions(c.id, enabled)),
  });

  const memberById = useMemo(() => {
    const m = new Map<string, MemberWithUser>();
    for (const x of workspaceMembers) m.set(x.user_id, x);
    return m;
  }, [workspaceMembers]);
  const agentById = useMemo(() => {
    const m = new Map<string, Agent>();
    for (const x of workspaceAgents) m.set(x.id, x);
    return m;
  }, [workspaceAgents]);

  return (
    <aside className="flex w-64 shrink-0 flex-col border-r border-border bg-muted/20">
      <div className="flex items-center justify-between border-b border-border px-3 py-2">
        <span className="text-sm font-semibold text-foreground">Channels</span>
      </div>
      <div className="flex-1 overflow-y-auto py-2">
        {isLoading ? (
          <div className="px-3 py-2 text-sm text-muted-foreground">Loading…</div>
        ) : (
          <>
            <SectionHeader title="Channels" onAdd={onCreateChannel} addLabel="Create channel" />
            {publicChannels.length > 0 ? (
              <ChannelRows channels={publicChannels} activeId={activeChannelId} slug={slug} />
            ) : (
              <EmptyHint text="No public channels yet" />
            )}

            {privateChannels.length > 0 && (
              <>
                <SectionHeader title="Private" />
                <ChannelRows channels={privateChannels} activeId={activeChannelId} slug={slug} />
              </>
            )}

            <SectionHeader title="Direct messages" onAdd={onCreateDM} addLabel="New direct message" />
            {dms.length > 0 ? (
              <ul className="flex flex-col gap-px">
                {dms.map((c, i) => {
                  const memQuery = dmMembersResults[i];
                  const mems = memQuery?.data ?? [];
                  const info = dmDisplayName(selfUserId, mems, memberById, agentById);
                  const isActive = c.id === activeChannelId;
                  return (
                    <li key={c.id}>
                      <AppLink
                        href={paths.workspace(slug).channelDetail(c.id)}
                        className={[
                          "flex items-center gap-2 px-3 py-1.5 text-sm",
                          "text-muted-foreground hover:bg-sidebar-accent/70 hover:text-foreground",
                          isActive ? "bg-sidebar-accent text-sidebar-accent-foreground" : "",
                        ].join(" ")}
                      >
                        <DMAvatar info={info} />
                        <span className="truncate">{info.label}</span>
                      </AppLink>
                    </li>
                  );
                })}
              </ul>
            ) : (
              <EmptyHint text="No direct messages yet" />
            )}
          </>
        )}
      </div>
    </aside>
  );
}

interface SectionHeaderProps {
  title: string;
  onAdd?: () => void;
  addLabel?: string;
}

function SectionHeader({ title, onAdd, addLabel }: SectionHeaderProps) {
  return (
    <div className="flex items-center justify-between px-3 pb-1 pt-3">
      <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {title}
      </span>
      {onAdd ? (
        <Button
          size="sm"
          variant="ghost"
          className="h-5 w-5 p-0"
          onClick={onAdd}
          aria-label={addLabel ?? "Add"}
        >
          <Plus className="h-3.5 w-3.5" />
        </Button>
      ) : null}
    </div>
  );
}

function EmptyHint({ text }: { text: string }) {
  return <div className="px-3 py-1 text-xs text-muted-foreground/70">{text}</div>;
}

interface ChannelRowsProps {
  channels: Channel[];
  activeId: string | null;
  slug: string;
}

function ChannelRows({ channels, activeId, slug }: ChannelRowsProps) {
  return (
    <ul className="flex flex-col gap-px">
      {channels.map((c) => {
        const Icon = rowIcon(c);
        const isActive = c.id === activeId;
        const label = c.display_name || c.name;
        return (
          <li key={c.id}>
            <AppLink
              href={paths.workspace(slug).channelDetail(c.id)}
              className={[
                "flex items-center gap-2 px-3 py-1.5 text-sm",
                "text-muted-foreground hover:bg-sidebar-accent/70 hover:text-foreground",
                isActive ? "bg-sidebar-accent text-sidebar-accent-foreground" : "",
              ].join(" ")}
            >
              <Icon className="h-4 w-4 shrink-0" />
              <span className="truncate">{label}</span>
            </AppLink>
          </li>
        );
      })}
    </ul>
  );
}

interface DMAvatarProps {
  info: ReturnType<typeof dmDisplayName>;
}

function DMAvatar({ info }: DMAvatarProps) {
  if (info.type === "agent") {
    return (
      <Avatar className="h-5 w-5 shrink-0">
        <AvatarFallback className="bg-purple-100 text-purple-900">
          <Bot className="h-3 w-3" />
        </AvatarFallback>
      </Avatar>
    );
  }
  if (info.type === "member") {
    return (
      <Avatar className="h-5 w-5 shrink-0">
        {info.avatarUrl ? <AvatarImage src={info.avatarUrl} alt={info.label} /> : null}
        <AvatarFallback className="text-[10px]">
          {info.label.charAt(0).toUpperCase() || "?"}
        </AvatarFallback>
      </Avatar>
    );
  }
  return <MessageCircle className="h-4 w-4 shrink-0" />;
}
