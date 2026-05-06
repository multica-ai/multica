"use client";

import { useMemo, useState } from "react";
import { Lock, Search, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@multica/ui/components/ui/tabs";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { useCreateChannel } from "@multica/core/channels";
import type { Channel } from "@multica/core/types";
import { canAssignAgent } from "../../issues/components";
import { ActorAvatar } from "../../common/actor-avatar";

type Tab = "dm" | "channel";

interface Selected {
  type: "member" | "agent";
  id: string;
  name: string;
}

export interface NewMessageModalProps {
  open: boolean;
  onClose: () => void;
  /** Called when a channel/DM has been created. The caller can navigate to it. */
  onCreated?: (channel: Channel) => void;
  /** Initial tab — defaults to `dm`. */
  defaultTab?: Tab;
}

export function NewMessageModal({
  open,
  onClose,
  onCreated,
  defaultTab = "dm",
}: NewMessageModalProps) {
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const [tab, setTab] = useState<Tab>(defaultTab);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState<Selected[]>([]);
  const [submitting, setSubmitting] = useState(false);

  const createChannel = useCreateChannel();

  const currentMember = members.find((m) => m.user_id === user?.id);
  const memberRole = currentMember?.role;

  // Members that aren't the current user (you can't DM/channel yourself).
  const visibleMembers = members.filter((m) => m.user_id !== user?.id);

  // Hide private agents the current user can't access — never expose them in
  // the picker.
  const visibleAgents = useMemo(
    () =>
      agents.filter(
        (a) => !a.archived_at && canAssignAgent(a, user?.id, memberRole),
      ),
    [agents, user?.id, memberRole],
  );

  const query = filter.trim().toLowerCase();
  const filteredMembers = visibleMembers.filter((m) =>
    m.name.toLowerCase().includes(query),
  );
  const filteredAgents = visibleAgents.filter((a) =>
    a.name.toLowerCase().includes(query),
  );

  const isPicked = (type: "member" | "agent", id: string) =>
    selected.some((s) => s.type === type && s.id === id);

  const togglePick = (s: Selected) => {
    setSelected((prev) => {
      const exists = prev.some((p) => p.type === s.type && p.id === s.id);
      if (exists) return prev.filter((p) => !(p.type === s.type && p.id === s.id));
      // DM tab: replace any existing pick (single-recipient).
      if (tab === "dm") return [s];
      return [...prev, s];
    });
  };

  const removePick = (s: Selected) =>
    setSelected((prev) =>
      prev.filter((p) => !(p.type === s.type && p.id === s.id)),
    );

  const reset = () => {
    setTab(defaultTab);
    setName("");
    setDescription("");
    setFilter("");
    setSelected([]);
    setSubmitting(false);
  };

  const close = () => {
    reset();
    onClose();
  };

  const switchTab = (next: Tab) => {
    setTab(next);
    setFilter("");
    if (next === "dm") {
      // DM only allows one member — drop everything else.
      setSelected((prev) => {
        const firstMember = prev.find((p) => p.type === "member");
        return firstMember ? [firstMember] : [];
      });
      setName("");
    }
  };

  // Validation rules per tab — gates the submit button so users see what's
  // blocking them rather than getting a server-side rejection.
  const memberPicks = selected.filter((s) => s.type === "member");
  const agentPicks = selected.filter((s) => s.type === "agent");
  const canSubmit =
    !submitting &&
    (tab === "dm"
      ? memberPicks.length === 1 && agentPicks.length === 0
      : name.trim().length > 0 && selected.length > 0);

  const handleSubmit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      const memberIds = selected
        .filter((s) => s.type === "member")
        .map((s) => s.id);
      const agentIds = selected
        .filter((s) => s.type === "agent")
        .map((s) => s.id);
      const channel = await createChannel.mutateAsync({
        kind: tab,
        // For DMs the server ignores name and derives it from participants.
        name: tab === "dm" ? "" : name.trim(),
        description: description.trim() || undefined,
        member_ids: memberIds,
        agent_ids: agentIds,
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
        className="!max-w-md !w-[calc(100vw-2rem)] gap-0 p-0"
      >
        <DialogTitle className="sr-only">
          {tab === "dm" ? "New direct message" : "New channel"}
        </DialogTitle>

        <Tabs
          value={tab}
          onValueChange={(v) => switchTab(v as Tab)}
          className="!gap-0"
        >
          <div className="flex items-center justify-between px-4 pt-3 pb-2">
            <TabsList>
              <TabsTrigger value="dm">Direct message</TabsTrigger>
              <TabsTrigger value="channel">Channel</TabsTrigger>
            </TabsList>
            <button
              onClick={close}
              className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
              type="button"
              aria-label="Close"
            >
              <X className="size-4" />
            </button>
          </div>

          <TabsContent value="channel" className="px-4 pt-1 pb-2 space-y-2">
            <Input
              autoFocus
              placeholder="Channel name"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <Input
              placeholder="Description (optional)"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </TabsContent>

          <TabsContent value="dm" className="px-4 pt-1 pb-0" />

          <div className="px-4 pb-2">
            {selected.length > 0 && (
              <div className="flex flex-wrap gap-1 pb-2">
                {selected.map((s) => (
                  <SelectedChip
                    key={`${s.type}:${s.id}`}
                    pick={s}
                    onRemove={() => removePick(s)}
                  />
                ))}
              </div>
            )}
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground" />
              <Input
                placeholder={
                  tab === "dm" ? "Add a person…" : "Add people and agents…"
                }
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                className="pl-8"
              />
            </div>
          </div>

          <div className="max-h-64 overflow-y-auto border-t">
            {filteredMembers.length === 0 && filteredAgents.length === 0 && (
              <p className="px-4 py-6 text-center text-xs text-muted-foreground">
                No matches.
              </p>
            )}
            {filteredMembers.length > 0 && (
              <PickerSection label="People">
                {filteredMembers.map((m) => (
                  <PickerRow
                    key={m.user_id}
                    selected={isPicked("member", m.user_id)}
                    onClick={() =>
                      togglePick({ type: "member", id: m.user_id, name: m.name })
                    }
                  >
                    <ActorAvatar actorType="member" actorId={m.user_id} size={20} />
                    <span className="truncate">{m.name}</span>
                  </PickerRow>
                ))}
              </PickerSection>
            )}
            {tab === "channel" && filteredAgents.length > 0 && (
              <PickerSection label="Agents">
                {filteredAgents.map((a) => (
                  <PickerRow
                    key={a.id}
                    selected={isPicked("agent", a.id)}
                    onClick={() =>
                      togglePick({ type: "agent", id: a.id, name: a.name })
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
          </div>

          <div className="flex items-center justify-end gap-2 border-t px-4 py-3">
            <Button variant="ghost" size="sm" onClick={close} type="button">
              Cancel
            </Button>
            <Button
              size="sm"
              type="button"
              onClick={handleSubmit}
              disabled={!canSubmit}
            >
              {submitting
                ? "Starting…"
                : tab === "dm"
                  ? "Start chat"
                  : "Create channel"}
            </Button>
          </div>
        </Tabs>
      </DialogContent>
    </Dialog>
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
      <div className="px-4 py-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
      {children}
    </div>
  );
}

function PickerRow({
  selected,
  onClick,
  children,
}: {
  selected: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex w-full items-center gap-2 px-4 py-1.5 text-left text-sm transition-colors ${
        selected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      <span
        className={`flex size-3.5 shrink-0 items-center justify-center rounded-sm border text-[10px] ${
          selected
            ? "border-primary bg-primary text-primary-foreground"
            : "border-border"
        }`}
      >
        {selected && "✓"}
      </span>
      {children}
    </button>
  );
}

function SelectedChip({
  pick,
  onRemove,
}: {
  pick: Selected;
  onRemove: () => void;
}) {
  return (
    <span className="inline-flex items-center gap-1 rounded-full border bg-accent/40 py-0.5 pl-1 pr-2 text-xs">
      <ActorAvatar actorType={pick.type} actorId={pick.id} size={16} />
      <span className="truncate max-w-[140px]">{pick.name}</span>
      <button
        type="button"
        onClick={onRemove}
        className="ml-0.5 rounded-full p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground"
        aria-label={`Remove ${pick.name}`}
      >
        <X className="size-3" />
      </button>
    </span>
  );
}

