"use client";

import React, { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Hash, Lock, Plus } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { channelListOptions, useCreateChannel, useChannelStore } from "@multica/core/channels";
import type { Channel } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { AppLink } from "../navigation";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useT } from "../i18n";

function ChannelListSkeleton() {
  return (
    <div className="flex flex-col gap-1 px-2 py-3">
      {Array.from({ length: 4 }).map((_, i) => (
        <Skeleton key={i} className="h-8 w-full" />
      ))}
    </div>
  );
}

function CreateChannelDialog({
  open,
  onOpenChange,
  wsId,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  wsId: string;
}) {
  const { t } = useT("channels");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [type, setType] = useState<"public" | "private">("public");
  const createChannel = useCreateChannel(wsId);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    await createChannel.mutateAsync({
      name: name.trim(),
      description: description.trim(),
      type,
    });
    setName("");
    setDescription("");
    setType("public");
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.create.title)}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="channel-name">{t(($) => $.create.name_label)}</Label>
            <Input
              id="channel-name"
              placeholder={t(($) => $.create.name_placeholder)}
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoFocus
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="channel-description">{t(($) => $.create.description_label)}</Label>
            <Input
              id="channel-description"
              placeholder={t(($) => $.create.description_placeholder)}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>
          <div className="space-y-1.5">
            <Label>{t(($) => $.create.type_label)}</Label>
            <Select
              value={type}
              onValueChange={(v) => setType(v as "public" | "private")}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="public">
                  <span className="flex items-center gap-2">
                    <Hash className="size-3.5" />
                    {t(($) => $.create.type_public)}
                  </span>
                </SelectItem>
                <SelectItem value="private">
                  <span className="flex items-center gap-2">
                    <Lock className="size-3.5" />
                    {t(($) => $.create.type_private)}
                  </span>
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
            >
              {t(($) => $.create.cancel)}
            </Button>
            <Button
              type="submit"
              disabled={!name.trim() || createChannel.isPending}
            >
              {createChannel.isPending ? t(($) => $.create.creating) : t(($) => $.create.create)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

interface ChannelListProps {
  /** When true the list is rendered inside the channel detail page (inline sidebar). */
  activeChannelId?: string | null;
}

export function ChannelList({ activeChannelId: propActiveId }: ChannelListProps = {}) {
  const { t } = useT("channels");
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const storeActiveId = useChannelStore((s) => s.activeChannelId);
  const activeChannelId = propActiveId !== undefined ? propActiveId : storeActiveId;

  const [createOpen, setCreateOpen] = useState(false);

  const { data: channels = [], isLoading } = useQuery(channelListOptions(wsId));

  const slug = workspace?.slug ?? "";

  if (isLoading) {
    return <ChannelListSkeleton />;
  }

  return (
    <>
      <div className="flex items-center justify-between px-3 pt-3 pb-1">
        <span className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">
          {t(($) => $.list.title)}
        </span>
        <Button
          variant="ghost"
          size="icon"
          className="size-6 text-muted-foreground hover:text-foreground"
          onClick={() => setCreateOpen(true)}
          title={t(($) => $.create.title)}
        >
          <Plus className="size-3.5" />
        </Button>
      </div>

      <div className="flex flex-col gap-0.5 px-2 pb-2">
        {channels.length === 0 ? (
          <div className="px-2 py-4 text-center text-xs text-muted-foreground">
            {t(($) => $.list.empty)}
            <br />
            <button
              type="button"
              className="mt-1 text-primary hover:underline"
              onClick={() => setCreateOpen(true)}
            >
              {t(($) => $.list.create_first)}
            </button>
          </div>
        ) : (
          channels.map((ch: Channel) => {
            const isActive = ch.id === activeChannelId;
            const href = `/${encodeURIComponent(slug)}/channels/${encodeURIComponent(ch.id)}`;

            return (
              <AppLink
                key={ch.id}
                href={href}
                className={cn(
                  "flex w-full items-center gap-2 rounded-md px-3 py-1.5 text-left text-sm transition-colors group",
                  isActive
                    ? "bg-accent text-accent-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                )}
              >
                {ch.type === "private" ? (
                  <Lock className="size-3.5 shrink-0" />
                ) : (
                  <Hash className="size-3.5 shrink-0" />
                )}
                <span className="flex-1 truncate">{ch.name}</span>
                {!ch.is_member && (
                  <span className="text-[10px] text-muted-foreground/60">
                    {t(($) => $.list.join)}
                  </span>
                )}
              </AppLink>
            );
          })
        )}
      </div>

      <CreateChannelDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        wsId={wsId}
      />
    </>
  );
}
