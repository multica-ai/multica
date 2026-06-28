// CEREBRO-PATCH(channels-participants-panel): add/remove channel participants side-panel (JEH-700)
"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Lock, Search, X } from "lucide-react";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { useToggleChannelParticipant, useConvertGroupToChannel } from "@multica/core/channels"; // CEREBRO-PATCH(channel-group-kind): FIR-2159
import type { Channel, ChannelMember } from "@multica/core/types";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@multica/ui/components/ui/sheet";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button"; // CEREBRO-PATCH(channel-group-kind): FIR-2159
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
import { canAssignAgent } from "../../issues/components";
// CEREBRO-PATCH(agent-surface-visibility): TECH-3670 — hide agents from the channel member picker.
import { isAgentHiddenOnSurface } from "@multica/cerebro-access/views";
import { ActorAvatar } from "../../common/actor-avatar";
import { useActorName } from "@multica/core/workspace/hooks";

interface ParticipantsPanelProps {
  channel: Channel;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function ParticipantsPanel({
  channel,
  open,
  onOpenChange,
}: ParticipantsPanelProps) {
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { getActorName } = useActorName();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const currentMember = members.find((m) => m.user_id === user?.id);
  const memberRole = currentMember?.role;
  const isDm = channel.kind === "dm";
  // CEREBRO-PATCH(channel-group-kind): FIR-2159 — groups have no fixed name; the
  // user can promote one to a named channel from here.
  const isGroup = channel.kind === "group";

  const toggle = useToggleChannelParticipant();
  const convertGroup = useConvertGroupToChannel(); // CEREBRO-PATCH(channel-group-kind): FIR-2159

  const [filter, setFilter] = useState("");
  const [pendingRemove, setPendingRemove] = useState<ChannelMember | null>(null);
  const [convertName, setConvertName] = useState(""); // CEREBRO-PATCH(channel-group-kind): FIR-2159

  // CEREBRO-PATCH(channel-group-kind): FIR-2159 — promote this group to a named channel.
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
          toast.error(
            err instanceof Error ? err.message : "Failed to convert to channel",
          ),
      },
    );
  };

  const isParticipant = (type: ChannelMember["user_type"], id: string) =>
    channel.participants.some((p) => p.user_type === type && p.user_id === id);

  const visibleMembers = useMemo(
    () => members.filter((m) => !isParticipant("member", m.user_id)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [members, channel.participants],
  );
  const visibleAgents = useMemo(
    () =>
      agents.filter(
        (a) =>
          !a.archived_at &&
          canAssignAgent(a, user?.id, memberRole) &&
          // CEREBRO-PATCH(agent-surface-visibility): TECH-3670 — drop agents hidden from channels.
          !isAgentHiddenOnSurface(a, "channels", { userId: user?.id ?? null, role: memberRole ?? null }) &&
          !isParticipant("agent", a.id),
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [agents, user?.id, memberRole, channel.participants],
  );

  const query = filter.trim().toLowerCase();
  const filteredMembers = visibleMembers.filter((m) =>
    m.name.toLowerCase().includes(query),
  );
  const filteredAgents = visibleAgents.filter((a) =>
    a.name.toLowerCase().includes(query),
  );

  const handleAdd = (member: ChannelMember) => {
    toggle.mutate(
      { channelId: channel.id, member, action: "add" },
      {
        onError: (err) =>
          toast.error(
            err instanceof Error ? err.message : "Failed to add participant",
          ),
      },
    );
  };

  const handleRemove = (member: ChannelMember) => {
    toggle.mutate(
      { channelId: channel.id, member, action: "remove" },
      {
        onError: (err) =>
          toast.error(
            err instanceof Error ? err.message : "Failed to remove participant",
          ),
      },
    );
  };

  return (
    <>
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent
          side="right"
          className="flex w-full max-w-sm flex-col gap-0 p-0"
        >
          <SheetHeader className="border-b">
            <SheetTitle>Participants</SheetTitle>
            <SheetDescription>
              {isDm
                ? "Direct messages are 1-on-1 — convert to a channel to add more people."
                : "Add or remove people and agents in this channel."}
            </SheetDescription>
          </SheetHeader>

          <div className="flex-1 min-h-0 overflow-y-auto">
            <section className="px-4 py-3">
              <h3 className="pb-2 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
                In this {isDm ? "conversation" : "channel"} ({channel.participants.length})
              </h3>
              <ul className="flex flex-col gap-1">
                {channel.participants.map((p) => {
                  const isSelf =
                    p.user_type === "member" && p.user_id === user?.id;
                  return (
                    <li
                      key={`${p.user_type}:${p.user_id}`}
                      className="flex items-center gap-2 rounded px-2 py-1.5 hover:bg-accent/40"
                    >
                      <ActorAvatar
                        actorType={p.user_type}
                        actorId={p.user_id}
                        size={22}
                      />
                      <span className="truncate text-sm">
                        {getActorName(p.user_type, p.user_id)}
                        {isSelf && (
                          <span className="ml-1 text-xs text-muted-foreground">
                            (you)
                          </span>
                        )}
                      </span>
                      {!isDm && !isSelf && (
                        <button
                          type="button"
                          onClick={() => setPendingRemove(p)}
                          aria-label={`Remove ${getActorName(p.user_type, p.user_id)}`}
                          className="ml-auto rounded-sm p-1 text-muted-foreground opacity-70 hover:bg-accent hover:opacity-100"
                        >
                          <X className="size-3.5" />
                        </button>
                      )}
                    </li>
                  );
                })}
              </ul>
            </section>

            {!isDm && (
              <section className="border-t px-4 py-3">
                <h3 className="pb-2 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
                  Add participants
                </h3>
                <div className="relative pb-2">
                  <Search className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground" />
                  <Input
                    placeholder="Search people and agents…"
                    value={filter}
                    onChange={(e) => setFilter(e.target.value)}
                    className="pl-8"
                  />
                </div>
                {filteredMembers.length === 0 && filteredAgents.length === 0 && (
                  <p className="py-4 text-center text-xs text-muted-foreground">
                    No more people or agents to add.
                  </p>
                )}
                {filteredMembers.length > 0 && (
                  <PickerSection label="People">
                    {filteredMembers.map((m) => (
                      <PickerRow
                        key={m.user_id}
                        onClick={() =>
                          handleAdd({ user_type: "member", user_id: m.user_id })
                        }
                      >
                        <ActorAvatar actorType="member" actorId={m.user_id} size={20} />
                        <span className="truncate">{m.name}</span>
                      </PickerRow>
                    ))}
                  </PickerSection>
                )}
                {filteredAgents.length > 0 && (
                  <PickerSection label="Agents">
                    {filteredAgents.map((a) => (
                      <PickerRow
                        key={a.id}
                        onClick={() =>
                          handleAdd({ user_type: "agent", user_id: a.id })
                        }
                      >
                        <ActorAvatar actorType="agent" actorId={a.id} size={20} />
                        <span className="truncate">{a.name}</span>
                        {a.visibility === "private" && (
                          <Lock className="ml-auto size-3 text-muted-foreground" />
                        )}
                      </PickerRow>
                    ))}
                  </PickerSection>
                )}
              </section>
            )}

            {/* CEREBRO-PATCH(channel-group-kind): FIR-2159 — promote a group to a
                named channel. On success the server's channel:updated event
                refreshes the list. */}
            {isGroup && (
              <section className="border-t px-4 py-3">
                <h3 className="pb-2 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
                  Convert to channel
                </h3>
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
                  className="mt-2 w-full"
                  disabled={!convertName.trim() || convertGroup.isPending}
                  onClick={handleConvert}
                >
                  Convert to channel
                </Button>
              </section>
            )}
          </div>
        </SheetContent>
      </Sheet>

      <AlertDialog
        open={!!pendingRemove}
        onOpenChange={(v) => !v && setPendingRemove(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Remove participant?</AlertDialogTitle>
            <AlertDialogDescription>
              {pendingRemove
                ? `${getActorName(pendingRemove.user_type, pendingRemove.user_id)} will lose access to this channel and stop receiving messages.`
                : ""}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (pendingRemove) handleRemove(pendingRemove);
                setPendingRemove(null);
              }}
            >
              Remove
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function PickerSection({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="py-1">
      <div className="px-1 pb-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      {children}
    </div>
  );
}

function PickerRow({
  onClick,
  children,
}: {
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent/50"
    >
      {children}
    </button>
  );
}
