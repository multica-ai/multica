"use client";

// CEREBRO-PATCH(new-message-modal-redesign): tap-to-start unified actor picker.
// Single-tap a row to immediately start a DM (members) or AI chat session
// (agents) — multi-select for groups is opt-in via the "Group" toggle. JEH-718.
// CEREBRO-PATCH(new-message-modal-redesign): JEH-846 — toggle button labels its
// destination ("Switch to channel" / "Switch to message") with matching icon.

import { useMemo, useState } from "react";
import { Lock, MessageCircle, Search, Star, Users, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { useCreateChannel } from "@multica/core/channels";
import { useChannelFavoritesStore, actorKey } from "@multica/cerebro-channels";
import type { Channel } from "@multica/core/types";
import { canAssignAgent } from "../../issues/components";
import { ActorAvatar } from "../../common/actor-avatar";

type ActorType = "member" | "agent";

interface Actor {
  type: ActorType;
  id: string;
  name: string;
  /** True for private agents — rendered with a lock indicator. */
  locked?: boolean;
}

export interface NewMessageModalProps {
  open: boolean;
  onClose: () => void;
  /** Called when a channel/DM has been created (caller navigates to it). */
  onCreated?: (channel: Channel) => void;
  /**
   * Single-agent tap dispatches here so the inbox can route into the chat
   * flow (selectedAgentId + new-chat panel) instead of creating a one-agent
   * channel. If omitted, agent taps fall back to a one-agent DM channel.
   */
  onAgentChatStarted?: (agentId: string) => void;
}

export function NewMessageModal({
  open,
  onClose,
  onCreated,
  onAgentChatStarted,
}: NewMessageModalProps) {
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const [filter, setFilter] = useState("");
  const [groupMode, setGroupMode] = useState(false);
  const [selected, setSelected] = useState<Actor[]>([]);
  const [nameOverride, setNameOverride] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const createChannel = useCreateChannel();
  const favorites = useChannelFavoritesStore((s) => s.favorites);
  const toggleFavorite = useChannelFavoritesStore((s) => s.toggle);

  const currentMember = members.find((m) => m.user_id === user?.id);
  const memberRole = currentMember?.role;

  const allActors = useMemo<Actor[]>(() => {
    const memberActors: Actor[] = members
      .filter((m) => m.user_id !== user?.id)
      .map((m) => ({ type: "member", id: m.user_id, name: m.name }));
    const agentActors: Actor[] = agents
      .filter((a) => !a.archived_at && canAssignAgent(a, user?.id, memberRole))
      .map((a) => ({
        type: "agent",
        id: a.id,
        name: a.name,
        locked: a.visibility === "private",
      }));
    return [...memberActors, ...agentActors].sort((a, b) =>
      a.name.localeCompare(b.name),
    );
  }, [members, agents, user?.id, memberRole]);

  const query = filter.trim().toLowerCase();
  const filtered = useMemo(
    () =>
      query
        ? allActors.filter((a) => a.name.toLowerCase().includes(query))
        : allActors,
    [allActors, query],
  );

  const { favoriteRows, otherRows } = useMemo(() => {
    const isFav = (a: Actor) => favorites.includes(actorKey(a.type, a.id));
    return {
      favoriteRows: filtered.filter(isFav),
      otherRows: filtered.filter((a) => !isFav(a)),
    };
  }, [filtered, favorites]);

  const isPicked = (a: Actor) =>
    selected.some((s) => s.type === a.type && s.id === a.id);

  const togglePick = (a: Actor) =>
    setSelected((prev) => {
      const exists = prev.some((p) => p.type === a.type && p.id === a.id);
      return exists
        ? prev.filter((p) => !(p.type === a.type && p.id === a.id))
        : [...prev, a];
    });

  const removePick = (a: Actor) =>
    setSelected((prev) =>
      prev.filter((p) => !(p.type === a.type && p.id === a.id)),
    );

  const reset = () => {
    setFilter("");
    setSelected([]);
    setGroupMode(false);
    setNameOverride(null);
    setSubmitting(false);
  };

  const close = () => {
    reset();
    onClose();
  };

  const startDirect = async (actor: Actor) => {
    if (submitting) return;
    if (actor.type === "agent" && onAgentChatStarted) {
      onAgentChatStarted(actor.id);
      close();
      return;
    }
    setSubmitting(true);
    try {
      const channel = await createChannel.mutateAsync({
        kind: "dm",
        name: "",
        member_ids: actor.type === "member" ? [actor.id] : [],
        agent_ids: actor.type === "agent" ? [actor.id] : [],
      });
      onCreated?.(channel);
      close();
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to start conversation",
      );
    } finally {
      setSubmitting(false);
    }
  };

  const handleRowTap = (actor: Actor) => {
    if (groupMode) {
      togglePick(actor);
    } else {
      startDirect(actor);
    }
  };

  const toggleGroupMode = () => {
    setGroupMode((v) => {
      if (v) {
        setSelected([]);
        setNameOverride(null);
      }
      return !v;
    });
  };

  const derivedName = selected.map((s) => s.name).join(", ");
  const effectiveName = nameOverride ?? derivedName;
  const canSubmitGroup =
    !submitting && selected.length > 0 && effectiveName.trim().length > 0;

  const handleCreateGroup = async () => {
    if (!canSubmitGroup) return;
    setSubmitting(true);
    try {
      const channel = await createChannel.mutateAsync({
        kind: "channel",
        name: effectiveName.trim(),
        member_ids: selected.filter((s) => s.type === "member").map((s) => s.id),
        agent_ids: selected.filter((s) => s.type === "agent").map((s) => s.id),
      });
      onCreated?.(channel);
      close();
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to create channel",
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !v && close()}>
      <DialogContent
        showCloseButton={false}
        className="!max-w-md gap-0 p-0"
      >
        <DialogTitle className="sr-only">
          {groupMode ? "New group" : "New message"}
        </DialogTitle>

        <div className="flex items-center justify-between border-b px-4 py-2.5">
          <h2 className="text-sm font-semibold">
            {groupMode ? "New group" : "New message"}
          </h2>
          <div className="-mr-1 flex items-center gap-0.5">
            <button
              type="button"
              onClick={toggleGroupMode}
              className={`flex h-7 items-center gap-1.5 rounded-sm border px-2 text-xs font-medium transition-colors ${
                groupMode
                  ? "border-border bg-accent text-foreground"
                  : "border-border text-muted-foreground hover:bg-accent/60 hover:text-foreground"
              }`}
              aria-pressed={groupMode}
            >
              {groupMode ? (
                <MessageCircle className="size-3.5" />
              ) : (
                <Users className="size-3.5" />
              )}
              {groupMode ? "Switch to message" : "Switch to channel"}
            </button>
            <button
              onClick={close}
              className="rounded-sm p-1.5 text-muted-foreground hover:bg-accent/60 hover:text-foreground transition-colors cursor-pointer"
              type="button"
              aria-label="Close"
            >
              <X className="size-4" />
            </button>
          </div>
        </div>

        {groupMode && selected.length > 0 && (
          <div className="flex flex-wrap gap-1.5 border-b px-4 py-2">
            {selected.map((a) => (
              <SelectedChip
                key={`${a.type}:${a.id}`}
                actor={a}
                onRemove={() => removePick(a)}
              />
            ))}
          </div>
        )}

        <div className="border-b px-4 py-2">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground" />
            <Input
              autoFocus
              placeholder="Search…"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="h-9 pl-8 text-sm"
            />
          </div>
        </div>

        <div className="max-h-[min(60vh,24rem)] overflow-y-auto py-1">
          {filtered.length === 0 ? (
            <p className="px-4 py-8 text-center text-sm text-muted-foreground">
              No matches.
            </p>
          ) : (
            <>
              {favoriteRows.length > 0 && (
                <PickerSection label="Favorites">
                  {favoriteRows.map((a) => (
                    <PickerRow
                      key={`${a.type}:${a.id}`}
                      actor={a}
                      groupMode={groupMode}
                      selected={isPicked(a)}
                      favorite
                      onTap={() => handleRowTap(a)}
                      onToggleFavorite={() =>
                        toggleFavorite(actorKey(a.type, a.id))
                      }
                    />
                  ))}
                </PickerSection>
              )}
              {otherRows.length > 0 && (
                <PickerSection
                  label={favoriteRows.length > 0 ? "Everyone" : null}
                >
                  {otherRows.map((a) => (
                    <PickerRow
                      key={`${a.type}:${a.id}`}
                      actor={a}
                      groupMode={groupMode}
                      selected={isPicked(a)}
                      favorite={false}
                      onTap={() => handleRowTap(a)}
                      onToggleFavorite={() =>
                        toggleFavorite(actorKey(a.type, a.id))
                      }
                    />
                  ))}
                </PickerSection>
              )}
            </>
          )}
        </div>

        {groupMode && (
          <div className="space-y-2 border-t px-4 py-2.5">
            {selected.length > 0 && (
              <Input
                value={effectiveName}
                placeholder={derivedName || "Channel name"}
                onChange={(e) => setNameOverride(e.target.value)}
                className="h-9 text-sm"
                aria-label="Channel name"
              />
            )}
            <div className="flex justify-end">
              <Button
                size="sm"
                type="button"
                onClick={handleCreateGroup}
                disabled={!canSubmitGroup}
              >
                {submitting
                  ? "Creating…"
                  : selected.length > 0
                    ? `Create channel (${selected.length})`
                    : "Pick people to add"}
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function PickerSection({
  label,
  children,
}: {
  label: string | null;
  children: React.ReactNode;
}) {
  return (
    <div>
      {label && (
        <div className="px-4 py-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
          {label}
        </div>
      )}
      {children}
    </div>
  );
}

function PickerRow({
  actor,
  groupMode,
  selected,
  favorite,
  onTap,
  onToggleFavorite,
}: {
  actor: Actor;
  groupMode: boolean;
  selected: boolean;
  favorite: boolean;
  onTap: () => void;
  onToggleFavorite: () => void;
}) {
  return (
    <div
      className={`group/row flex items-center transition-colors ${
        selected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      <button
        type="button"
        onClick={onTap}
        className="flex flex-1 items-center gap-2.5 px-4 py-1.5 text-left text-sm"
      >
        {groupMode && (
          <span
            className={`flex size-3.5 shrink-0 items-center justify-center rounded-sm border text-[10px] ${
              selected
                ? "border-primary bg-primary text-primary-foreground"
                : "border-border"
            }`}
          >
            {selected && "✓"}
          </span>
        )}
        <ActorAvatar actorType={actor.type} actorId={actor.id} size={24} />
        <span className="truncate flex-1">{actor.name}</span>
        {actor.locked && (
          <Lock className="size-3 shrink-0 text-muted-foreground" />
        )}
      </button>
      <button
        type="button"
        onClick={onToggleFavorite}
        className={`mr-2 flex size-7 items-center justify-center rounded-sm transition-colors ${
          favorite
            ? "text-amber-500 hover:bg-amber-500/10"
            : "text-muted-foreground/40 hover:bg-accent hover:text-foreground sm:opacity-0 sm:group-hover/row:opacity-100"
        }`}
        aria-label={
          favorite ? `Unfavorite ${actor.name}` : `Favorite ${actor.name}`
        }
        aria-pressed={favorite}
      >
        <Star className="size-4" fill={favorite ? "currentColor" : "none"} />
      </button>
    </div>
  );
}

function SelectedChip({
  actor,
  onRemove,
}: {
  actor: Actor;
  onRemove: () => void;
}) {
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full border bg-accent/40 py-0.5 pl-1 pr-1.5 text-xs">
      <ActorAvatar actorType={actor.type} actorId={actor.id} size={18} />
      <span className="truncate max-w-[160px]">{actor.name}</span>
      <button
        type="button"
        onClick={onRemove}
        className="rounded-full p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground"
        aria-label={`Remove ${actor.name}`}
      >
        <X className="size-3" />
      </button>
    </span>
  );
}
