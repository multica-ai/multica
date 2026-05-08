"use client";

// CEREBRO-PATCH(new-message-modal-redesign): unified actor picker (no agent/member split),
// multiselect, search, per-workspace favorites, mobile-friendly sizing — JEH-718.

import { useMemo, useState } from "react";
import { Lock, Search, Star, X } from "lucide-react";
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
  /** Called when a channel/DM has been created. The caller can navigate to it. */
  onCreated?: (channel: Channel) => void;
}

export function NewMessageModal({ open, onClose, onCreated }: NewMessageModalProps) {
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState<Actor[]>([]);
  // Manual override of the auto-derived channel name. `null` means "use the
  // auto-derived name"; once the user types, we lock to their text.
  const [nameOverride, setNameOverride] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const createChannel = useCreateChannel();
  const favorites = useChannelFavoritesStore((s) => s.favorites);
  const toggleFavorite = useChannelFavoritesStore((s) => s.toggle);

  const currentMember = members.find((m) => m.user_id === user?.id);
  const memberRole = currentMember?.role;

  // Build the unified list once: drop self, drop archived/inaccessible agents.
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

  // Pin favorites to the top, then everyone else. Order within each group is
  // alphabetical (preserved from `allActors`).
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

  // Default channel name = participants comma-separated. The user can edit
  // this after the channel is created (or override it inline before creating).
  const derivedName = selected.map((s) => s.name).join(", ");
  const effectiveName = nameOverride ?? derivedName;

  // A single-member, no-agent pick is a DM — the server derives the name from
  // participants and ignores the name field. Anything else (incl. one agent)
  // is a channel that needs a name.
  const memberPicks = selected.filter((s) => s.type === "member");
  const agentPicks = selected.filter((s) => s.type === "agent");
  const isDm = memberPicks.length === 1 && agentPicks.length === 0;

  const reset = () => {
    setFilter("");
    setSelected([]);
    setNameOverride(null);
    setSubmitting(false);
  };

  const close = () => {
    reset();
    onClose();
  };

  const canSubmit =
    !submitting &&
    selected.length > 0 &&
    (isDm || effectiveName.trim().length > 0);

  const handleSubmit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      const channel = await createChannel.mutateAsync({
        kind: isDm ? "dm" : "channel",
        name: isDm ? "" : effectiveName.trim(),
        member_ids: memberPicks.map((s) => s.id),
        agent_ids: agentPicks.map((s) => s.id),
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

  return (
    <Dialog open={open} onOpenChange={(v) => !v && close()}>
      <DialogContent
        showCloseButton={false}
        // Roomier on every breakpoint than the prior `md`/256-tall list. On
        // mobile we explicitly fill the viewport (minus a small inset) so the
        // tap targets and the actor list both have space to breathe.
        className="!max-w-lg !w-[calc(100vw-1.5rem)] gap-0 p-0 sm:!w-full"
      >
        <DialogTitle className="sr-only">New message</DialogTitle>

        <div className="flex items-center justify-between border-b px-4 py-3 sm:py-2.5">
          <h2 className="text-base font-semibold sm:text-sm">New message</h2>
          <button
            onClick={close}
            className="-mr-1.5 rounded-sm p-2 opacity-70 hover:bg-accent/60 hover:opacity-100 transition-all cursor-pointer sm:p-1.5"
            type="button"
            aria-label="Close"
          >
            <X className="size-4" />
          </button>
        </div>

        <div className="space-y-2 px-4 pt-3 pb-2">
          {selected.length > 0 && (
            <div className="flex flex-wrap gap-1.5 pb-1">
              {selected.map((a) => (
                <SelectedChip
                  key={`${a.type}:${a.id}`}
                  actor={a}
                  onRemove={() => removePick(a)}
                />
              ))}
            </div>
          )}
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
            <Input
              autoFocus
              placeholder="Search people and agents…"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              // Bigger tap target on mobile; collapses to default on sm+.
              className="h-11 pl-9 text-base sm:h-9 sm:text-sm"
            />
          </div>
        </div>

        {/*
         * Adaptive list — caps at 60% of viewport on mobile, ~22rem on desktop —
         * so the list always shows several rows without pushing the action bar
         * off-screen.
         */}
        <div className="max-h-[min(60vh,22rem)] overflow-y-auto border-t">
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
                      selected={isPicked(a)}
                      favorite
                      onToggle={() => togglePick(a)}
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
                      selected={isPicked(a)}
                      favorite={false}
                      onToggle={() => togglePick(a)}
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

        {/*
         * Channel-name editor only matters once the selection is no longer a
         * 1:1 DM. Reveal it inline so the user sees the auto-derived name and
         * can rename before creating, but stays hidden in the simple case.
         */}
        {!isDm && selected.length > 0 && (
          <div className="border-t px-4 pt-3 pb-1">
            <label className="mb-1 block text-xs font-medium text-muted-foreground">
              Channel name
            </label>
            <Input
              value={effectiveName}
              placeholder={derivedName || "Channel name"}
              onChange={(e) => setNameOverride(e.target.value)}
              className="h-11 text-base sm:h-9 sm:text-sm"
            />
          </div>
        )}

        <div className="flex items-center justify-end gap-2 border-t px-4 py-3 sm:py-2.5">
          <Button variant="ghost" size="sm" onClick={close} type="button">
            Cancel
          </Button>
          <Button
            size="sm"
            type="button"
            onClick={handleSubmit}
            disabled={!canSubmit}
          >
            {submitting ? "Starting…" : isDm ? "Start chat" : "Create channel"}
          </Button>
        </div>
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
    <div className="py-1">
      {label && (
        <div className="px-4 py-1.5 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
          {label}
        </div>
      )}
      {children}
    </div>
  );
}

function PickerRow({
  actor,
  selected,
  favorite,
  onToggle,
  onToggleFavorite,
}: {
  actor: Actor;
  selected: boolean;
  favorite: boolean;
  onToggle: () => void;
  onToggleFavorite: () => void;
}) {
  return (
    <div
      className={`flex items-center gap-2 transition-colors ${
        selected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      <button
        type="button"
        onClick={onToggle}
        // Generous tap target — at least 44px tall on mobile, the iOS HIG
        // minimum. Collapses on sm+ to match the previous compact density.
        className="flex flex-1 items-center gap-3 px-4 py-2.5 text-left text-sm sm:gap-2 sm:py-1.5"
      >
        <span
          className={`flex size-4 shrink-0 items-center justify-center rounded-sm border text-[10px] sm:size-3.5 ${
            selected
              ? "border-primary bg-primary text-primary-foreground"
              : "border-border"
          }`}
        >
          {selected && "✓"}
        </span>
        <ActorAvatar actorType={actor.type} actorId={actor.id} size={24} />
        <span className="truncate flex-1">{actor.name}</span>
        {actor.locked && (
          <Lock className="size-3 shrink-0 text-muted-foreground" />
        )}
      </button>
      <button
        type="button"
        onClick={onToggleFavorite}
        // Star is a separate hit target so tapping it doesn't add the actor
        // to the channel. Keep it 44px tall on mobile to stay tappable.
        className={`mr-2 flex size-11 items-center justify-center rounded-sm transition-colors sm:size-7 ${
          favorite
            ? "text-amber-500 hover:bg-amber-500/10"
            : "text-muted-foreground/50 hover:bg-accent hover:text-foreground"
        }`}
        aria-label={favorite ? `Unfavorite ${actor.name}` : `Favorite ${actor.name}`}
        aria-pressed={favorite}
      >
        <Star
          className="size-4"
          fill={favorite ? "currentColor" : "none"}
        />
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
    <span className="inline-flex items-center gap-1.5 rounded-full border bg-accent/40 py-1 pl-1 pr-1 text-sm sm:py-0.5 sm:pr-2 sm:text-xs">
      <ActorAvatar actorType={actor.type} actorId={actor.id} size={20} />
      <span className="truncate max-w-[160px]">{actor.name}</span>
      <button
        type="button"
        onClick={onRemove}
        className="rounded-full p-1 text-muted-foreground hover:bg-accent hover:text-foreground sm:p-0.5"
        aria-label={`Remove ${actor.name}`}
      >
        <X className="size-3.5 sm:size-3" />
      </button>
    </span>
  );
}

