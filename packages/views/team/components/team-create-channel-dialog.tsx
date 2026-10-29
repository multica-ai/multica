"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
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

export function TeamCreateChannelDialog({ onClose }: { onClose: () => void }) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { push } = useNavigation();
  const userId = useAuthStore((s) => s.user?.id ?? "");
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: people = [] } = useQuery(memberListOptions(wsId));
  const create = useCreateChannel();
  const [name, setName] = useState("");
  const [about, setAbout] = useState("");
  const [isPrivate, setIsPrivate] = useState(false);
  const [picked, setPicked] = useState<Set<string>>(() => new Set());

  const liveAgents = useMemo(() => agents.filter((a) => !a.archived_at), [agents]);
  const others = useMemo(() => people.filter((p) => p.user_id !== userId), [people, userId]);
  const toggle = (key: string) =>
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  const submit = () => {
    if (!name.trim() || create.isPending) return;
    const members: ConversationMember[] = [...picked].map((key) => {
      const [member_type, member_id] = key.split(":") as [ConversationMember["member_type"], string];
      return { member_type, member_id };
    });
    create.mutate(
      { name, about, visibility: isPrivate ? "private" : "public", members },
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

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-md gap-0 p-0">
        <DialogHeader className="border-b px-5 py-3">
          <DialogTitle className="text-body font-semibold">{t(($) => $.channel.create.title)}</DialogTitle>
        </DialogHeader>
        <form
          className="flex max-h-[70vh] flex-col gap-4 overflow-y-auto px-5 py-4"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="team-channel-name">{t(($) => $.channel.create.name)}</Label>
            <Input
              id="team-channel-name"
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t(($) => $.channel.create.name_placeholder)}
              maxLength={80}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="team-channel-about">{t(($) => $.channel.create.about)}</Label>
            <Input
              id="team-channel-about"
              value={about}
              onChange={(e) => setAbout(e.target.value)}
              placeholder={t(($) => $.channel.create.about_placeholder)}
              maxLength={500}
            />
          </div>
          <label className="flex items-start gap-3">
            <Switch checked={isPrivate} onCheckedChange={setIsPrivate} className="mt-0.5" />
            <span className="flex flex-col gap-0.5">
              <span className="text-body font-medium">{t(($) => $.channel.create.private)}</span>
              <span className="text-caption text-muted-foreground">{t(($) => $.channel.create.private_hint)}</span>
            </span>
          </label>
          {liveAgents.length > 0 && (
            <PickList
              label={t(($) => $.channel.create.agents)}
              hint={t(($) => $.channel.agents_hint)}
              items={liveAgents.map((a) => ({ key: `agent:${a.id}`, type: "agent" as const, id: a.id, name: a.name }))}
              picked={picked}
              onToggle={toggle}
            />
          )}
          {others.length > 0 && (
            <PickList
              label={t(($) => $.channel.create.people)}
              items={others.map((p) => ({ key: `member:${p.user_id}`, type: "member" as const, id: p.user_id, name: p.name }))}
              picked={picked}
              onToggle={toggle}
            />
          )}
          <div className="flex justify-end">
            <Button type="submit" disabled={!name.trim() || create.isPending}>
              {t(($) => $.channel.create.submit)}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function PickList({
  label,
  hint,
  items,
  picked,
  onToggle,
}: {
  label: string;
  hint?: string;
  items: { key: string; type: "agent" | "member"; id: string; name: string }[];
  picked: Set<string>;
  onToggle: (key: string) => void;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-body font-medium">{label}</span>
      {hint && <span className="text-caption text-muted-foreground">{hint}</span>}
      <div className="flex max-h-40 flex-col gap-px overflow-y-auto rounded-md border p-1">
        {items.map((item) => (
          <label key={item.key} className="flex h-8 cursor-pointer items-center gap-2 rounded px-2 hover:bg-surface-hover">
            <Checkbox checked={picked.has(item.key)} onCheckedChange={() => onToggle(item.key)} />
            <ActorAvatar actorType={item.type} actorId={item.id} size="xs" />
            <span className="truncate text-body">{item.name}</span>
          </label>
        ))}
      </div>
    </div>
  );
}
