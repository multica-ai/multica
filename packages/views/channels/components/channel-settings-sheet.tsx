// CEREBRO-PATCH(channel-settings-sheet): TECH-3698 — net-new file in the
// upstream zone (mark on the header so the upstream-zone validator catches
// future edits too). Consolidates the channel header's loose actions — Pin,
// Archive, Agent mentions, Edit name/description — into one gear-opened
// settings sheet, and adds the per-channel permission controls (who may
// rename, who may add/remove others, whether members may leave).
"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Archive, Pencil, Pin, PinOff } from "lucide-react";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  channelPermissionsOptions,
  useUpdateChannelPermissions,
} from "@multica/core/channels";
import { pinListOptions, useCreatePin, useDeletePin } from "@multica/core/pins";
import { useArchiveChannel } from "@multica/cerebro-channels";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import type { Channel, ChannelPermissions } from "@multica/core/types";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@multica/ui/components/ui/sheet";
import { Switch } from "@multica/ui/components/ui/switch";
import { Button } from "@multica/ui/components/ui/button";
import { ListenersList } from "./channel-listeners-panel";

interface ChannelSettingsSheetProps {
  channel: Channel;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** True when the current user may change channel settings (workspace
   *  admin/owner or the channel creator). When false the permission switches
   *  are read-only. */
  canManage: boolean;
  /** Opens the existing edit-name/description dialog (mounted by the parent). */
  onEditChannel: () => void;
  /** Archives the conversation (same gesture the old header button used). */
  onArchive: () => void;
}

export function ChannelSettingsSheet({
  channel,
  open,
  onOpenChange,
  canManage,
  onEditChannel,
  onArchive,
}: ChannelSettingsSheetProps) {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const isChannel = channel.kind === "channel";
  const permissionsEnabled = useFeatureFlag("cerebro_channel_permissions");

  // --- Pin to sidebar -----------------------------------------------------
  const { data: pinnedItems = [] } = useQuery({
    ...pinListOptions(wsId, userId ?? ""),
    enabled: !!userId,
  });
  const isPinned = pinnedItems.some(
    (p) => p.item_type === channel.kind && p.item_id === channel.id,
  );
  const createPin = useCreatePin();
  const deletePin = useDeletePin();
  const togglePin = () => {
    if (isPinned) {
      deletePin.mutate({ itemType: channel.kind, itemId: channel.id });
    } else {
      createPin.mutate({ item_type: channel.kind, item_id: channel.id });
    }
  };

  const archiveChannel = useArchiveChannel();
  void archiveChannel; // archive routed through the parent's onArchive gesture.

  const agentParticipants = useMemo(
    () => channel.participants.filter((p) => p.user_type === "agent"),
    [channel.participants],
  );

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full max-w-sm flex-col gap-0 p-0">
        <SheetHeader className="border-b">
          <SheetTitle>{isChannel ? "Channel settings" : "Conversation settings"}</SheetTitle>
          <SheetDescription>
            {isChannel
              ? "Manage this channel — name, permissions, agents and more."
              : "Pin or archive this direct message."}
          </SheetDescription>
        </SheetHeader>

        <div className="flex-1 min-h-0 overflow-y-auto">
          {isChannel && (
            <section className="border-b px-4 py-3">
              <SectionLabel>Channel</SectionLabel>
              <Button
                variant="outline"
                size="sm"
                className="mt-1 w-full justify-start gap-2"
                onClick={() => {
                  onOpenChange(false);
                  onEditChannel();
                }}
              >
                <Pencil className="size-3.5" />
                Edit name &amp; description
              </Button>
            </section>
          )}

          {isChannel && permissionsEnabled && (
            <section className="border-b px-4 py-3">
              <SectionLabel>Permissions</SectionLabel>
              <PermissionControls channel={channel} canManage={canManage} />
            </section>
          )}

          {agentParticipants.length > 0 && (
            <section className="border-b px-4 py-3">
              <SectionLabel>Agent mentions</SectionLabel>
              <p className="pb-2 text-xs text-muted-foreground">
                Toggle off to require an explicit @-mention before the agent reacts.
              </p>
              <ListenersList
                channelId={channel.id}
                agents={agentParticipants}
                defaultMode={isChannel ? "mention_only" : "always"}
              />
            </section>
          )}

          <section className="px-4 py-3">
            <SectionLabel>Actions</SectionLabel>
            <div className="mt-1 flex flex-col gap-1.5">
              <Button
                variant="outline"
                size="sm"
                className="w-full justify-start gap-2"
                onClick={togglePin}
              >
                {isPinned ? <PinOff className="size-3.5" /> : <Pin className="size-3.5" />}
                {isPinned ? "Unpin from sidebar" : "Pin to sidebar"}
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="w-full justify-start gap-2 text-muted-foreground"
                onClick={() => {
                  onOpenChange(false);
                  onArchive();
                }}
              >
                <Archive className="size-3.5" />
                Archive conversation
              </Button>
            </div>
          </section>
        </div>
      </SheetContent>
    </Sheet>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <h3 className="pb-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
      {children}
    </h3>
  );
}

function PermissionControls({
  channel,
  canManage,
}: {
  channel: Channel;
  canManage: boolean;
}) {
  const wsId = useWorkspaceId();
  const { data: perms } = useQuery(channelPermissionsOptions(wsId, channel.id));
  const update = useUpdateChannelPermissions(channel.id);

  // Default to the safe server defaults while the query resolves.
  const current: ChannelPermissions = perms ?? {
    rename_policy: "admins",
    add_members_policy: "everyone",
    allow_self_leave: true,
  };

  const save = (next: ChannelPermissions) => {
    update.mutate(next, {
      onError: (err) =>
        toast.error(err instanceof Error ? err.message : "Failed to save settings"),
    });
  };

  const disabled = !canManage || update.isPending;

  return (
    <div className="flex flex-col gap-3">
      <PermissionToggle
        id="perm-rename"
        label="Only admins & owners can rename"
        hint="Off: any member can rename the channel."
        checked={current.rename_policy === "admins"}
        disabled={disabled}
        onChange={(on) =>
          save({ ...current, rename_policy: on ? "admins" : "everyone" })
        }
      />
      <PermissionToggle
        id="perm-manage"
        label="Only admins & owners can add/remove people"
        hint="Off: any member can add or remove other participants."
        checked={current.add_members_policy === "admins"}
        disabled={disabled}
        onChange={(on) =>
          save({ ...current, add_members_policy: on ? "admins" : "everyone" })
        }
      />
      <PermissionToggle
        id="perm-leave"
        label="Members can leave on their own"
        hint="Off: only admins/owners can remove a member."
        checked={current.allow_self_leave}
        disabled={disabled}
        onChange={(on) => save({ ...current, allow_self_leave: on })}
      />
      {!canManage && (
        <p className="text-[11px] text-muted-foreground">
          Only admins, owners or the channel creator can change these.
        </p>
      )}
    </div>
  );
}

function PermissionToggle({
  id,
  label,
  hint,
  checked,
  disabled,
  onChange,
}: {
  id: string;
  label: string;
  hint: string;
  checked: boolean;
  disabled: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-3">
      <label htmlFor={id} className="min-w-0 flex-1 cursor-pointer">
        <span className="block text-sm">{label}</span>
        <span className="block text-[11px] text-muted-foreground">{hint}</span>
      </label>
      <Switch id={id} checked={checked} disabled={disabled} onCheckedChange={onChange} />
    </div>
  );
}
