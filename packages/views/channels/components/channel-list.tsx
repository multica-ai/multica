"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useRequiredWorkspaceSlug, paths } from "@multica/core/paths";
import { channelsListOptions } from "@multica/core/channels";
import { AppLink } from "../../navigation";
import { Button } from "@multica/ui/components/ui/button";
import { Hash, Lock, MessageCircle, Plus } from "lucide-react";
import type { Channel } from "@multica/core/types";

interface ChannelListProps {
  activeChannelId: string | null;
  onCreateChannel: () => void;
  enabled: boolean;
}

function categorize(channels: Channel[]) {
  const publicChannels: Channel[] = [];
  const privateChannels: Channel[] = [];
  const dms: Channel[] = [];
  for (const c of channels) {
    if (c.kind === "dm") dms.push(c);
    else if (c.visibility === "public") publicChannels.push(c);
    else privateChannels.push(c);
  }
  return { publicChannels, privateChannels, dms };
}

function rowIcon(c: Channel) {
  if (c.kind === "dm") return MessageCircle;
  if (c.visibility === "private") return Lock;
  return Hash;
}

/**
 * ChannelList is the left-pane sidebar inside the Channels page itself —
 * NOT to be confused with the global app sidebar (which has a single
 * "Channels" entry that lands on this page).
 *
 * Three sections: public channels, private channels (the caller is a
 * member of), and DMs. Empty sections are hidden so the list stays tidy
 * for newer workspaces.
 */
export function ChannelList({ activeChannelId, onCreateChannel, enabled }: ChannelListProps) {
  const wsId = useWorkspaceId();
  const slug = useRequiredWorkspaceSlug();
  const { data: channels = [], isLoading } = useQuery(channelsListOptions(wsId, enabled));
  const { publicChannels, privateChannels, dms } = categorize(channels);

  return (
    <aside className="flex w-64 shrink-0 flex-col border-r border-border bg-muted/20">
      <div className="flex items-center justify-between border-b border-border px-3 py-2">
        <span className="text-sm font-semibold text-foreground">Channels</span>
        <Button
          size="sm"
          variant="ghost"
          onClick={onCreateChannel}
          aria-label="Create channel"
        >
          <Plus className="h-4 w-4" />
        </Button>
      </div>
      <div className="flex-1 overflow-y-auto py-2">
        {isLoading ? (
          <div className="px-3 py-2 text-sm text-muted-foreground">Loading…</div>
        ) : channels.length === 0 ? (
          <div className="px-3 py-2 text-sm text-muted-foreground">
            No channels yet. Create one to get started.
          </div>
        ) : (
          <>
            {publicChannels.length > 0 && (
              <Section title="Channels" channels={publicChannels} activeId={activeChannelId} slug={slug} />
            )}
            {privateChannels.length > 0 && (
              <Section title="Private" channels={privateChannels} activeId={activeChannelId} slug={slug} />
            )}
            {dms.length > 0 && (
              <Section title="Direct messages" channels={dms} activeId={activeChannelId} slug={slug} />
            )}
          </>
        )}
      </div>
    </aside>
  );
}

interface SectionProps {
  title: string;
  channels: Channel[];
  activeId: string | null;
  slug: string;
}

function Section({ title, channels, activeId, slug }: SectionProps) {
  return (
    <div className="mb-4">
      <div className="px-3 pb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {title}
      </div>
      <ul className="flex flex-col gap-px">
        {channels.map((c) => {
          const Icon = rowIcon(c);
          const isActive = c.id === activeId;
          const label = c.display_name || c.name;
          return (
            <li key={c.id}>
              <AppLink
                href={paths.workspace(slug).channelDetail(c.id)}
                className={[
                  "flex items-center gap-2 px-3 py-1.5 text-sm",
                  "text-muted-foreground hover:bg-sidebar-accent/70 hover:text-foreground",
                  isActive ? "bg-sidebar-accent text-sidebar-accent-foreground" : "",
                ].join(" ")}
              >
                <Icon className="h-4 w-4 shrink-0" />
                <span className="truncate">{label}</span>
              </AppLink>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
