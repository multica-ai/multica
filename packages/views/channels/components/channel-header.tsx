"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { channelMembersOptions, useArchiveChannel } from "@multica/core/channels";
import { Hash, Lock, MessageCircle, Users, Bot, MoreHorizontal, Archive, Settings } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { toast } from "sonner";
import { useNavigation } from "../../navigation";
import { useRequiredWorkspaceSlug, paths, useCurrentWorkspace } from "@multica/core/paths";
import type { Channel } from "@multica/core/types";
import { MembersPanel } from "./members-panel";
import { ChannelSettingsDialog } from "./channel-settings-dialog";

interface ChannelHeaderProps {
  channel: Channel;
  enabled: boolean;
}

function channelIcon(channel: Channel) {
  if (channel.kind === "dm") return MessageCircle;
  if (channel.visibility === "private") return Lock;
  return Hash;
}

/**
 * ChannelHeader — channel name + description + member count + admin menu.
 *
 * For DMs the title resolves from membership rather than channel.name (a
 * deterministic hash). The members button opens MembersPanel (a slide-over).
 * The `…` dropdown shows admin actions (currently just Archive).
 *
 * Archive is gated on the caller being an admin of this channel —
 * resolved from the channel_membership row, with a fallback to the
 * channel.created_by_id check (the creator is always a channel admin
 * per ChannelService.Create).
 */
export function ChannelHeader({ channel, enabled }: ChannelHeaderProps) {
  const Icon = channelIcon(channel);
  const wsId = useWorkspaceId();
  const slug = useRequiredWorkspaceSlug();
  const selfUserId = useAuthStore((s) => s.user?.id ?? null);
  const navigation = useNavigation();
  const { data: members = [] } = useQuery(channelMembersOptions(channel.id, enabled));
  const { data: workspaceMembers = [] } = useQuery(memberListOptions(wsId));
  const { data: workspaceAgents = [] } = useQuery(agentListOptions(wsId));
  const archiveMut = useArchiveChannel();
  const workspace = useCurrentWorkspace();
  const [membersOpen, setMembersOpen] = useState(false);
  const [archiveConfirmOpen, setArchiveConfirmOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);

  const dmInfo = useMemo(() => {
    if (channel.kind !== "dm") return null;
    const others = members.filter(
      (m) => !(m.member_type === "member" && m.member_id === selfUserId),
    );
    if (others.length === 0) {
      return { label: "Notes to self", type: "self" as const, avatarUrl: null as string | null };
    }
    const o = others[0]!;
    if (o.member_type === "member") {
      const wm = workspaceMembers.find((m) => m.user_id === o.member_id);
      return {
        label: wm?.name || wm?.email || "Unknown member",
        type: "member" as const,
        avatarUrl: wm?.avatar_url ?? null,
      };
    }
    const a = workspaceAgents.find((x) => x.id === o.member_id);
    return {
      label: a?.name || "Unknown agent",
      type: "agent" as const,
      avatarUrl: null,
    };
  }, [channel.kind, members, selfUserId, workspaceMembers, workspaceAgents]);

  const displayName = dmInfo?.label || channel.display_name || channel.name;

  // Caller is an admin if their membership row has role='admin'. The
  // creator is always given admin role on Create (see service.go), so this
  // matches the spec's permission table without an extra "is creator"
  // branch.
  const selfMembership = members.find(
    (m) => m.member_type === "member" && m.member_id === selfUserId,
  );
  const isAdmin = selfMembership?.role === "admin";

  const handleArchive = () => {
    setArchiveConfirmOpen(false);
    archiveMut.mutate(channel.id, {
      onSuccess: () => {
        toast.success("Channel archived");
        navigation.push(paths.workspace(slug).channels());
      },
      onError: (err: unknown) => {
        toast.error(err instanceof Error ? err.message : "Failed to archive channel");
      },
    });
  };

  return (
    <header className="flex items-center justify-between border-b border-border px-4 py-3">
      <div className="flex min-w-0 items-center gap-2">
        {dmInfo ? (
          <Avatar className="h-6 w-6 shrink-0">
            {dmInfo.avatarUrl ? (
              <AvatarImage src={dmInfo.avatarUrl} alt={dmInfo.label} />
            ) : null}
            <AvatarFallback className={dmInfo.type === "agent" ? "bg-purple-100 text-purple-900" : ""}>
              {dmInfo.type === "agent" ? (
                <Bot className="h-3 w-3" />
              ) : (
                dmInfo.label.charAt(0).toUpperCase() || "?"
              )}
            </AvatarFallback>
          </Avatar>
        ) : (
          <Icon className="h-5 w-5 shrink-0 text-muted-foreground" />
        )}
        <div className="min-w-0">
          <div className="truncate text-base font-semibold text-foreground">{displayName}</div>
          {channel.description && channel.kind !== "dm" ? (
            <div className="truncate text-sm text-muted-foreground">{channel.description}</div>
          ) : null}
        </div>
      </div>
      <div className="flex items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          onClick={() => setMembersOpen(true)}
          aria-label="Show members"
        >
          <Users className="mr-2 h-4 w-4" />
          {members.length}
        </Button>
        {isAdmin && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="outline" size="sm" aria-label="Channel actions">
                  <MoreHorizontal className="h-4 w-4" />
                </Button>
              }
            />
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => setSettingsOpen(true)}>
                <Settings className="mr-2 h-4 w-4" />
                Channel settings
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => setArchiveConfirmOpen(true)}
                className="text-destructive focus:text-destructive"
              >
                <Archive className="mr-2 h-4 w-4" />
                Archive channel
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>

      <MembersPanel
        channel={channel}
        open={membersOpen}
        onOpenChange={setMembersOpen}
        enabled={enabled}
      />

      <ChannelSettingsDialog
        channel={channel}
        workspaceRetentionDays={workspace?.channel_retention_days ?? null}
        open={settingsOpen}
        onOpenChange={setSettingsOpen}
      />

      <AlertDialog open={archiveConfirmOpen} onOpenChange={setArchiveConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Archive this channel?</AlertDialogTitle>
            <AlertDialogDescription>
              The channel will disappear from the list and from search.
              Historical messages and memberships are preserved — nothing
              is deleted. An admin can reverse this from the database
              (no restore UI yet).
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleArchive}>Archive</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </header>
  );
}
