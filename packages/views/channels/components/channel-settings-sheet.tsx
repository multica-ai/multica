// CEREBRO-PATCH(channel-settings-sheet): TECH-3698 — net-new file in the
// upstream zone (mark on the header so the upstream-zone validator catches
// future edits too). Consolidates the channel header's loose actions — Pin,
// Archive, Agent mentions, Edit name/description — into one gear-opened
// settings sheet, and adds the per-channel permission controls (who may
// rename, who may add/remove others, whether members may leave).
"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Archive, ArrowUpRight, LogOut, Pencil, Pin, PinOff, Trash2 } from "lucide-react"; // CEREBRO-PATCH(channel-group-kind): FIR-2159 — convert icon.
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  channelPermissionsOptions,
  useUpdateChannelPermissions,
  useConvertGroupToChannel, // CEREBRO-PATCH(channel-group-kind): FIR-2159 — promote a group to a named channel.
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
import { Switch } from "@multica/ui/components/ui/switch";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input"; // CEREBRO-PATCH(channel-group-kind): FIR-2159 — name field for convert.
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
  /** TECH-3758 — leaves the channel (removes the caller's own subscription).
   *  Distinct from archive: the channel is gone from the chat roster, not just
   *  hidden from the inbox feed. */
  onLeave: () => void;
  /** TECH-3758 — deletes the channel for everyone. Only offered to privileged
   *  callers (canManage). */
  onDelete: () => void;
}

export function ChannelSettingsSheet({
  channel,
  open,
  onOpenChange,
  canManage,
  onEditChannel,
  onArchive,
  onLeave,
  onDelete,
}: ChannelSettingsSheetProps) {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const isChannel = channel.kind === "channel";
  // CEREBRO-PATCH(channel-group-kind): FIR-2159 — groups are multi-party, so
  // they get Leave/Delete affordances too; channel-only things (rename, name
  // display) stay on the strict `isChannel` check.
  const isChannelOrGroup = channel.kind === "channel" || channel.kind === "group";
  // CEREBRO-PATCH(channel-group-kind): FIR-2159 — a group can be promoted to a
  // named channel from the gear (the discoverable settings home).
  const isGroup = channel.kind === "group";
  const permissionsEnabled = useFeatureFlag("cerebro_channel_permissions");
  // CEREBRO-PATCH(channel-leave-delete-actions): TECH-3758 — Leave/Delete
  // actions + confirm gate for the destructive "delete for everyone" action.
  const [confirmDelete, setConfirmDelete] = useState(false);
  // CEREBRO-PATCH(channel-group-kind): FIR-2159 — convert-to-channel name draft.
  const [convertName, setConvertName] = useState("");
  const convertGroup = useConvertGroupToChannel();
  const handleConvert = () => {
    const name = convertName.trim();
    if (!name) return;
    convertGroup.mutate(
      { channelId: channel.id, name },
      {
        onSuccess: () => {
          setConvertName("");
          onOpenChange(false);
        },
        onError: (err) =>
          toast.error(err instanceof Error ? err.message : "Failed to convert to channel"),
      },
    );
  };

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

          {/* CEREBRO-PATCH(channel-group-kind): FIR-2159 — promote a group to a
              named channel. On success the server's channel:updated event moves
              the row from Groups to Channels. */}
          {isGroup && (
            <section className="border-b px-4 py-3">
              <SectionLabel>Convert to channel</SectionLabel>
              <p className="pb-2 text-xs text-muted-foreground">
                Give this group a permanent name to turn it into a channel.
              </p>
              <Input
                placeholder="Channel name…"
                value={convertName}
                onChange={(e) => setConvertName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    handleConvert();
                  }
                }}
              />
              <Button
                size="sm"
                className="mt-2 w-full gap-2"
                disabled={!convertName.trim() || convertGroup.isPending}
                onClick={handleConvert}
              >
                <ArrowUpRight className="size-3.5" />
                Convert to channel
              </Button>
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
              {/* TECH-3758 — leave removes the caller from the channel (gone
                  from the chat roster), distinct from inbox archive. Channels
                  and groups only: a DM has no "leave". */}
              {/* CEREBRO-PATCH(channel-group-kind): FIR-2159 — groups allow Leave. */}
              {isChannelOrGroup && (
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full justify-start gap-2 text-muted-foreground"
                  onClick={() => {
                    onOpenChange(false);
                    onLeave();
                  }}
                >
                  <LogOut className="size-3.5" />
                  Leave channel
                </Button>
              )}
              {/* TECH-3758 — delete removes the channel for everyone; offered
                  only to the creator / workspace admins & owners. */}
              {/* CEREBRO-PATCH(channel-group-kind): FIR-2159 — groups allow Delete. */}
              {isChannelOrGroup && canManage && (
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full justify-start gap-2 text-destructive hover:text-destructive"
                  onClick={() => setConfirmDelete(true)}
                >
                  <Trash2 className="size-3.5" />
                  Delete channel
                </Button>
              )}
            </div>
          </section>
        </div>
      </SheetContent>

      {/* TECH-3758 — destructive delete confirmation. */}
      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete this channel?</AlertDialogTitle>
            <AlertDialogDescription>
              This permanently deletes &ldquo;{channel.title}&rdquo; and all its
              messages for everyone. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={() => {
                setConfirmDelete(false);
                onOpenChange(false);
                onDelete();
              }}
            >
              Delete channel
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
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
