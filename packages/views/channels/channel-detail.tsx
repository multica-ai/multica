"use client";

import React, { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Hash, Lock, Users } from "lucide-react";
import {
  channelDetailOptions,
  channelMembersOptions,
  useAddChannelMember,
  useRemoveChannelMember,
  useChannelStore,
} from "@multica/core/channels";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Channel, ChannelMember, Agent, MemberWithUser } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { Separator } from "@multica/ui/components/ui/separator";
import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";
import { ChannelList } from "./channel-list";
import { ChannelMessages } from "./channel-messages";
import { ChannelComposer } from "./channel-composer";

// ─────────────────────────────────────────────────────────────────────────────
// Member Management Dialog
// ─────────────────────────────────────────────────────────────────────────────

function ManageMembersDialog({
  open,
  onOpenChange,
  channelId,
  wsId,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  channelId: string;
  wsId: string;
}) {
  const { data: members = [] } = useQuery(channelMembersOptions(wsId, channelId));
  const { data: workspaceMembers = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const addMember = useAddChannelMember(wsId, channelId);
  const removeMember = useRemoveChannelMember(wsId, channelId);

  const channelMemberIds = new Set(
    (members as ChannelMember[]).map((m) => m.member_id),
  );

  const nonMemberUsers = (workspaceMembers as MemberWithUser[]).filter(
    (m) => !channelMemberIds.has(m.user_id),
  );
  const nonMemberAgents = (agents as Agent[]).filter(
    (a) => !channelMemberIds.has(a.id) && !a.archived_at,
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>管理成员</DialogTitle>
        </DialogHeader>

        {/* Current members */}
        <div className="space-y-1">
          <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
            当前成员 ({(members as ChannelMember[]).length})
          </p>
          {(members as ChannelMember[]).length === 0 ? (
            <p className="text-sm text-muted-foreground py-2">暂无成员</p>
          ) : (
            <div className="max-h-48 overflow-y-auto space-y-1">
              {(members as ChannelMember[]).map((m) => (
                <div
                  key={m.id}
                  className="flex items-center justify-between rounded-md px-2 py-1.5 hover:bg-muted/50"
                >
                  <div className="flex items-center gap-2 min-w-0">
                    <span
                      className={cn(
                        "size-2 rounded-full shrink-0",
                        m.member_type === "agent"
                          ? "bg-purple-500"
                          : "bg-blue-500",
                      )}
                    />
                    <span className="text-sm truncate">
                      {m.name || m.member_id}
                    </span>
                    <Badge variant="outline" className="text-[10px] shrink-0">
                      {m.member_type === "agent" ? "Agent" : "用户"}
                    </Badge>
                  </div>
                  {m.role !== "owner" && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 text-xs text-destructive hover:text-destructive"
                      onClick={() =>
                        removeMember.mutate(m.member_id)
                      }
                      disabled={removeMember.isPending}
                    >
                      移除
                    </Button>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>

        <Separator />

        {/* Add users */}
        {nonMemberUsers.length > 0 && (
          <div className="space-y-1">
            <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
              添加用户
            </p>
            <div className="max-h-36 overflow-y-auto space-y-1">
              {nonMemberUsers.map((m) => (
                <div
                  key={m.user_id}
                  className="flex items-center justify-between rounded-md px-2 py-1 hover:bg-muted/50"
                >
                  <span className="text-sm truncate">
                    {m.name || m.user_id}
                  </span>
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-6 text-xs"
                    onClick={() =>
                      addMember.mutate({
                        member_type: "user",
                        member_id: m.user_id,
                        role: "member",
                      })
                    }
                    disabled={addMember.isPending}
                  >
                    邀请
                  </Button>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Add agents */}
        {nonMemberAgents.length > 0 && (
          <div className="space-y-1">
            <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
              添加 Agent
            </p>
            <div className="max-h-36 overflow-y-auto space-y-1">
              {(nonMemberAgents as Agent[]).map((a) => (
                <div
                  key={a.id}
                  className="flex items-center justify-between rounded-md px-2 py-1 hover:bg-muted/50"
                >
                  <div className="flex items-center gap-2">
                    <span className="size-2 rounded-full bg-purple-500 shrink-0" />
                    <span className="text-sm truncate">{a.name}</span>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-6 text-xs"
                    onClick={() =>
                      addMember.mutate({
                        member_type: "agent",
                        member_id: a.id,
                        role: "member",
                      })
                    }
                    disabled={addMember.isPending}
                  >
                    邀请
                  </Button>
                </div>
              ))}
            </div>
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            完成
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// Channel Header
// ─────────────────────────────────────────────────────────────────────────────

function ChannelHeader({
  channelId,
  wsId,
  onManageMembers,
}: {
  channelId: string;
  wsId: string;
  onManageMembers: () => void;
}) {
  const { data: channel } = useQuery(channelDetailOptions(wsId, channelId));
  const { data: members = [] } = useQuery(channelMembersOptions(wsId, channelId));

  const ch = channel as Channel | undefined;
  const memberCount = (members as ChannelMember[]).length;

  return (
    <div className="flex items-center gap-2 border-b px-4 py-2.5 shrink-0">
      {ch?.type === "private" ? (
        <Lock className="size-4 shrink-0 text-muted-foreground" />
      ) : (
        <Hash className="size-4 shrink-0 text-muted-foreground" />
      )}
      <span className="font-semibold flex-1 truncate">{ch?.name ?? "频道"}</span>
      {ch?.description && (
        <>
          <Separator orientation="vertical" className="h-4" />
          <span className="text-sm text-muted-foreground truncate max-w-xs">
            {ch.description}
          </span>
        </>
      )}
      <div className="ml-auto flex items-center gap-1">
        <Button
          variant="ghost"
          size="sm"
          className="gap-1.5 text-muted-foreground"
          onClick={onManageMembers}
        >
          <Users className="size-3.5" />
          <span className="text-xs">{memberCount}</span>
        </Button>
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// Channel Detail (main layout)
// ─────────────────────────────────────────────────────────────────────────────

export function ChannelDetail({ channelId }: { channelId: string }) {
  const wsId = useWorkspaceId();
  const [manageMembersOpen, setManageMembersOpen] = useState(false);
  const setActiveChannel = useChannelStore((s) => s.setActiveChannel);

  // Sync the store with the URL-derived channelId so ChannelList highlights correctly
  React.useEffect(() => {
    setActiveChannel(channelId);
    return () => {
      // Leave activeChannelId set so navigating back shows last active
    };
  }, [channelId, setActiveChannel]);

  return (
    <div className="flex h-full">
      {/* Left sidebar: channel list */}
      <div className="w-56 shrink-0 border-r flex flex-col overflow-y-auto">
        <ChannelList activeChannelId={channelId} />
      </div>

      {/* Main area */}
      <div className="flex-1 flex flex-col min-w-0">
        <ChannelHeader
          channelId={channelId}
          wsId={wsId}
          onManageMembers={() => setManageMembersOpen(true)}
        />
        <div className="flex-1 overflow-y-auto">
          <ChannelMessages channelId={channelId} />
        </div>
        <ChannelComposer channelId={channelId} />
      </div>

      <ManageMembersDialog
        open={manageMembersOpen}
        onOpenChange={setManageMembersOpen}
        channelId={channelId}
        wsId={wsId}
      />
    </div>
  );
}
