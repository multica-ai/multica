"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Globe, Lock, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { ApiError } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { useCreateChannel } from "@multica/core/conversations";
import type { ConversationMember } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { teamHref } from "../selection";

type Candidate = { key: string; type: "agent" | "member"; id: string; name: string; detail: string };

export function TeamCreateChannelDialog({ onClose }: { onClose: () => void }) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { push } = useNavigation();
  const user = useAuthStore((s) => s.user);
  const userId = user?.id ?? "";
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: people = [] } = useQuery(memberListOptions(wsId));
  const create = useCreateChannel();
  const [name, setName] = useState("");
  const [about, setAbout] = useState("");
  const [visibility, setVisibility] = useState<"public" | "private">("public");
  const [picked, setPicked] = useState<string[]>([]);
  const [query, setQuery] = useState("");

  const candidates = useMemo(() => {
    const agentItems: Candidate[] = agents
      .filter((a) => !a.archived_at)
      .map((a) => ({ key: `agent:${a.id}`, type: "agent", id: a.id, name: a.name, detail: a.description ?? "" }));
    const peopleItems: Candidate[] = people
      .filter((p) => p.user_id !== userId)
      .map((p) => ({ key: `member:${p.user_id}`, type: "member", id: p.user_id, name: p.name, detail: p.email }));
    return { agents: agentItems, people: peopleItems, byKey: new Map([...agentItems, ...peopleItems].map((c) => [c.key, c])) };
  }, [agents, people, userId]);
  const q = query.trim().toLowerCase();
  const match = (c: Candidate) => !q || c.name.toLowerCase().includes(q);
  const toggle = (key: string) =>
    setPicked((prev) => (prev.includes(key) ? prev.filter((k) => k !== key) : [...prev, key]));

  const submit = () => {
    if (!name.trim() || create.isPending) return;
    const members: ConversationMember[] = picked.map((key) => {
      const [member_type, member_id] = key.split(":") as [ConversationMember["member_type"], string];
      return { member_type, member_id };
    });
    create.mutate(
      { name, about, visibility, members },
      {
        onSuccess: (channel) => {
          onClose();
          push(teamHref(wsPaths.team(), { kind: "channel", id: channel.id }));
        },
        onError: (error) =>
          toast.error(
            error instanceof ApiError && error.status === 409
              ? t(($) => $.channel.create.name_taken)
              : t(($) => $.channel.create.failed),
          ),
      },
    );
  };

  const shownAgents = candidates.agents.filter(match);
  const shownPeople = candidates.people.filter(match);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="gap-0 p-0 sm:max-w-[560px]">
        <DialogHeader className="gap-1 px-5 pb-2 pt-4">
          <DialogTitle className="text-body font-semibold">{t(($) => $.channel.create.title)}</DialogTitle>
          <DialogDescription className="text-caption">{t(($) => $.channel.create.description)}</DialogDescription>
        </DialogHeader>
        <form
          id="team-create-channel"
          className="flex flex-col gap-4 overflow-y-auto px-5 py-3"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="team-channel-name">{t(($) => $.channel.create.name)}</Label>
            <div className="flex h-8 items-center gap-1.5 rounded-md border bg-background px-2.5 focus-within:ring-1 focus-within:ring-ring">
              <span className="text-muted-foreground">#</span>
              <input
                id="team-channel-name"
                autoFocus
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t(($) => $.channel.create.name_placeholder)}
                maxLength={80}
                className="min-w-0 flex-1 bg-transparent text-body outline-none placeholder:text-muted-foreground"
              />
            </div>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="team-channel-about">
              {t(($) => $.channel.create.about)}
              <span className="font-normal text-muted-foreground">{t(($) => $.channel.create.optional)}</span>
            </Label>
            <Input
              id="team-channel-about"
              value={about}
              onChange={(e) => setAbout(e.target.value)}
              placeholder={t(($) => $.channel.create.about_placeholder)}
              maxLength={500}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="team-channel-members">
              {t(($) => $.channel.create.members)}
              <span className="font-normal text-muted-foreground">
                {t(($) => $.channel.create.members_count, { count: picked.length + 1 })}
              </span>
            </Label>
            <div className="flex min-h-8 flex-wrap items-center gap-1 rounded-md border bg-background px-1.5 py-1 focus-within:ring-1 focus-within:ring-ring">
              <span className="inline-flex h-6 items-center gap-1 rounded-md border px-1.5 text-caption">
                <ActorAvatar actorType="member" actorId={userId} size="xs" />
                {user?.name}
              </span>
              {picked.map((key) => {
                const c = candidates.byKey.get(key);
                if (!c) return null;
                return (
                  <span key={key} className="inline-flex h-6 items-center gap-1 rounded-md border px-1.5 text-caption">
                    <ActorAvatar actorType={c.type} actorId={c.id} size="xs" />
                    {c.name}
                    <button
                      type="button"
                      onClick={() => toggle(key)}
                      aria-label={t(($) => $.channel.remove)}
                      className="-mr-0.5 rounded text-muted-foreground hover:text-foreground"
                    >
                      <X className="size-3" />
                    </button>
                  </span>
                );
              })}
              <input
                id="team-channel-members"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={t(($) => $.channel.add_members)}
                className="h-6 min-w-32 flex-1 bg-transparent px-1 text-body outline-none placeholder:text-muted-foreground"
              />
            </div>
            <div className="max-h-48 overflow-y-auto rounded-md border p-1">
              {shownAgents.length > 0 && (
                <PickGroup label={t(($) => $.channel.create.agents)} items={shownAgents} picked={picked} onToggle={toggle} />
              )}
              {shownPeople.length > 0 && (
                <PickGroup label={t(($) => $.channel.create.people)} items={shownPeople} picked={picked} onToggle={toggle} />
              )}
              {shownAgents.length === 0 && shownPeople.length === 0 && (
                <p className="px-2 py-1.5 text-caption text-muted-foreground">{t(($) => $.channel.create.no_matches)}</p>
              )}
            </div>
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="text-label font-medium">{t(($) => $.channel.create.visibility)}</span>
            <div
              role="radiogroup"
              aria-label={t(($) => $.channel.create.visibility)}
              className="inline-flex w-fit items-center gap-0.5 rounded-lg bg-muted p-0.5"
            >
              {(["public", "private"] as const).map((value) => {
                const Icon = value === "public" ? Globe : Lock;
                return (
                  <button
                    key={value}
                    type="button"
                    role="radio"
                    aria-checked={visibility === value}
                    onClick={() => setVisibility(value)}
                    className={cn(
                      "inline-flex h-7 items-center gap-1.5 rounded-md px-2.5 text-label font-medium transition-colors focus-visible:outline-2 focus-visible:outline-ring",
                      visibility === value
                        ? "bg-background text-foreground shadow-sm"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    <Icon className="size-3.5" />
                    {value === "public" ? t(($) => $.channel.public) : t(($) => $.channel.private)}
                  </button>
                );
              })}
            </div>
            <p className="text-caption text-muted-foreground">
              {visibility === "public" ? t(($) => $.channel.create.public_hint) : t(($) => $.channel.create.private_hint)}
            </p>
          </div>
        </form>
        <div className="flex items-center gap-2 border-t px-5 py-3">
          <span className="flex-1 text-caption text-muted-foreground">{t(($) => $.channel.create.privacy_note)}</span>
          <Button type="button" variant="outline" onClick={onClose}>
            {t(($) => $.channel.create.cancel)}
          </Button>
          <Button type="submit" form="team-create-channel" disabled={!name.trim() || create.isPending}>
            {t(($) => $.channel.create.submit)}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function PickGroup({
  label,
  items,
  picked,
  onToggle,
}: {
  label: string;
  items: Candidate[];
  picked: string[];
  onToggle: (key: string) => void;
}) {
  return (
    <div className="flex flex-col gap-px">
      <span className="px-2 pb-0.5 pt-1.5 text-micro font-medium text-muted-foreground">{label}</span>
      {items.map((item) => (
        <label key={item.key} className="flex h-8 cursor-pointer items-center gap-2 rounded px-2 hover:bg-surface-hover">
          <Checkbox checked={picked.includes(item.key)} onCheckedChange={() => onToggle(item.key)} />
          <ActorAvatar actorType={item.type} actorId={item.id} size="xs" />
          <span className="min-w-0 truncate text-body">{item.name}</span>
          <span className="ml-auto min-w-0 truncate text-caption text-muted-foreground">{item.detail}</span>
        </label>
      ))}
    </div>
  );
}
