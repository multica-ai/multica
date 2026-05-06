"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import { Bot } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { useRequiredWorkspaceSlug, paths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { useCreateOrFetchDM } from "@multica/core/channels";
import { useNavigation } from "../../navigation";
import type { ChannelActorType } from "@multica/core/types";

interface NewDMDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

interface PickerRow {
  type: ChannelActorType;
  id: string;
  label: string;
  sublabel?: string;
  avatarUrl?: string | null;
}

/**
 * NewDMDialog — picker for starting a 1:1 conversation with another member
 * or an agent. Submitting calls POST /api/dms (idempotent on the participant
 * set), then navigates to the resulting channel.
 *
 * Phase 1 keeps this single-recipient (true 1:1 DM). Group DMs are out of
 * scope per the spec; multi-recipient flows should use a private channel.
 */
export function NewDMDialog({ open, onOpenChange }: NewDMDialogProps) {
  const wsId = useWorkspaceId();
  const slug = useRequiredWorkspaceSlug();
  const navigation = useNavigation();
  const currentUser = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const createDM = useCreateOrFetchDM();
  const [filter, setFilter] = useState("");
  const [pendingId, setPendingId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const rows: PickerRow[] = useMemo(() => {
    const out: PickerRow[] = [];
    for (const m of members) {
      // Skip self — DM-to-self is technically supported by the schema but
      // not a useful surface in the picker; surface as "Saved messages" in
      // a follow-up if it ever becomes a real ask.
      if (currentUser?.id && m.user_id === currentUser.id) continue;
      out.push({
        type: "member",
        id: m.user_id,
        label: m.name || m.email,
        sublabel: m.email,
        avatarUrl: m.avatar_url,
      });
    }
    for (const a of agents) {
      // Filter out archived agents — DMing an archived agent would create
      // a dead conversation.
      if (a.archived_at) continue;
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
      (r) =>
        r.label.toLowerCase().includes(needle) ||
        r.sublabel?.toLowerCase().includes(needle),
    );
  }, [members, agents, currentUser, filter]);

  const close = () => {
    if (createDM.isPending) return;
    setFilter("");
    setError(null);
    setPendingId(null);
    onOpenChange(false);
  };

  const start = (row: PickerRow) => {
    if (createDM.isPending) return;
    setPendingId(`${row.type}:${row.id}`);
    setError(null);
    createDM.mutate(
      { participants: [{ type: row.type, id: row.id }] },
      {
        onSuccess: (channel) => {
          close();
          navigation.push(paths.workspace(slug).channelDetail(channel.id));
        },
        onError: (err: unknown) => {
          const msg = err instanceof Error ? err.message : "Failed to open DM";
          setError(msg);
          setPendingId(null);
        },
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={(v) => (v ? onOpenChange(true) : close())}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New direct message</DialogTitle>
          <DialogDescription>
            Pick a teammate or an agent to start a 1:1 conversation. If a DM
            with this person already exists, you'll jump back to it.
          </DialogDescription>
        </DialogHeader>
        <Input
          placeholder="Search people and agents…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          autoFocus
        />
        {rows.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            {filter ? "No matches." : "No teammates or agents yet."}
          </p>
        ) : (
          <ul
            className="max-h-80 overflow-y-auto"
            role="listbox"
            aria-label="DM recipients"
          >
            {rows.map((row) => {
              const key = `${row.type}:${row.id}`;
              const pending = pendingId === key;
              return (
                <li key={key}>
                  <button
                    type="button"
                    role="option"
                    aria-selected={pending}
                    onClick={() => start(row)}
                    disabled={createDM.isPending}
                    className="flex w-full items-center gap-3 rounded-md px-2 py-2 text-left hover:bg-muted/60 disabled:cursor-not-allowed disabled:opacity-60"
                  >
                    <Avatar className="h-8 w-8 shrink-0">
                      {row.type === "member" && row.avatarUrl ? (
                        <AvatarImage src={row.avatarUrl} alt={row.label} />
                      ) : null}
                      <AvatarFallback
                        className={row.type === "agent" ? "bg-purple-100 text-purple-900" : ""}
                      >
                        {row.type === "agent" ? (
                          <Bot className="h-4 w-4" />
                        ) : (
                          row.label.charAt(0).toUpperCase()
                        )}
                      </AvatarFallback>
                    </Avatar>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-medium text-foreground">
                        {row.label}
                      </div>
                      {row.sublabel ? (
                        <div className="truncate text-xs text-muted-foreground">
                          {row.sublabel}
                        </div>
                      ) : null}
                    </div>
                    {pending ? (
                      <span className="text-xs text-muted-foreground">Opening…</span>
                    ) : null}
                  </button>
                </li>
              );
            })}
          </ul>
        )}
        {error ? (
          <p className="text-sm text-destructive" role="alert">
            {error}
          </p>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
