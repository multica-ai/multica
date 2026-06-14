"use client";

// CEREBRO-PATCH(cerebro-notes-link-picker): TECH-3421 — the "@ link" picker that
// lets a note tag a person or point at the work it is about: an issue, a
// channel, a DM, or an agent chat. Selecting an item hands a NoteLink back to
// the editor, which inserts it into the note body as a markdown chip. Tagging a
// person (member) becomes a routed notification on save; the work-links are pure
// navigation (see core/links.ts + server/internal/cerebro/note).
import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { AtSign, Hash, ListTodo, MessageSquare, User } from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { channelListOptions } from "@multica/core/channels/queries";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { Channel } from "@multica/core/types";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@multica/ui/components/ui/command";
import type { NoteLink } from "../core";

function truncate(s: string, max = 60): string {
  return s.length > max ? `${s.slice(0, max - 1)}…` : s;
}

// Small debounce so typing in the picker does not fire a search per keystroke.
function useDebounced(value: string, ms = 200): string {
  const [debounced, setDebounced] = React.useState(value);
  React.useEffect(() => {
    const t = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return debounced;
}

interface NoteLinkPickerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onPick: (link: NoteLink) => void;
  /** The button that anchors the popover (also the visible trigger). */
  trigger: React.ReactElement;
}

export function NoteLinkPicker({
  open,
  onOpenChange,
  onPick,
  trigger,
}: NoteLinkPickerProps) {
  const wsId = useWorkspaceId();
  const [q, setQ] = React.useState("");
  const query = useDebounced(q.trim());

  // Reset the query each time the picker opens so it never reopens mid-search.
  React.useEffect(() => {
    if (open) setQ("");
  }, [open]);

  const { data: issueRes } = useQuery({
    queryKey: ["note-link", "issues", query],
    queryFn: () =>
      api.searchIssues({ q: query, limit: 8, include_closed: true }),
    enabled: open && query.length > 0,
  });

  const { data: chatRes } = useQuery({
    queryKey: ["note-link", "chats", query],
    queryFn: () =>
      api
        .searchChatSessions({ q: query, limit: 6 })
        .catch(() => ({ chat_sessions: [], total: 0 })),
    enabled: open && query.length > 0,
  });

  const { data: channels = [] } = useQuery({
    ...channelListOptions(wsId),
    enabled: open,
  });

  // Issues group: real issues only — channels/DMs are issues too but get their
  // own group below, sourced from the channel list (carries titles + kind).
  const issues = (issueRes?.issues ?? []).filter((i) => i.kind === "issue");

  const channelMatches = (channels as Channel[])
    .filter((c) =>
      query ? c.title.toLowerCase().includes(query.toLowerCase()) : true,
    )
    .slice(0, query ? 8 : 6);
  const channelGroup = channelMatches.filter((c) => c.kind === "channel");
  const dmGroup = channelMatches.filter((c) => c.kind === "dm");

  const chats = chatRes?.chat_sessions ?? [];

  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: open,
  });
  const memberMatches = members
    .filter((m) =>
      query ? m.name.toLowerCase().includes(query.toLowerCase()) : true,
    )
    .slice(0, query ? 8 : 6);

  const pick = (link: NoteLink) => {
    onPick(link);
    onOpenChange(false);
  };

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger render={trigger} />
      <PopoverContent align="start" className="w-80 p-0">
        <Command shouldFilter={false}>
          <CommandInput
            value={q}
            onValueChange={setQ}
            placeholder="Tag a person, or link an issue, channel, DM or chat…"
            autoFocus
          />
          <CommandList className="max-h-72">
            {query.length === 0 &&
              memberMatches.length === 0 &&
              channelGroup.length === 0 &&
              dmGroup.length === 0 && (
                <CommandEmpty>Type to search people and your work…</CommandEmpty>
              )}
            {query.length > 0 &&
              memberMatches.length === 0 &&
              issues.length === 0 &&
              channelGroup.length === 0 &&
              dmGroup.length === 0 &&
              chats.length === 0 && <CommandEmpty>No matches.</CommandEmpty>}

            {memberMatches.length > 0 && (
              <CommandGroup heading="People">
                {memberMatches.map((m) => (
                  <CommandItem
                    key={`member:${m.user_id}`}
                    value={`member:${m.user_id}`}
                    onSelect={() =>
                      pick({ type: "member", id: m.user_id, label: m.name })
                    }
                  >
                    <AtSign className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">{m.name}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}

            {issues.length > 0 && (
              <CommandGroup heading="Issues">
                {issues.map((i) => (
                  <CommandItem
                    key={`issue:${i.id}`}
                    value={`issue:${i.id}`}
                    onSelect={() =>
                      pick({
                        type: "issue",
                        id: i.id,
                        label: truncate(`${i.identifier} ${i.title}`),
                      })
                    }
                  >
                    <ListTodo className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">
                      <span className="text-muted-foreground">
                        {i.identifier}
                      </span>{" "}
                      {i.title}
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}

            {channelGroup.length > 0 && (
              <CommandGroup heading="Channels">
                {channelGroup.map((c) => (
                  <CommandItem
                    key={`channel:${c.id}`}
                    value={`channel:${c.id}`}
                    onSelect={() =>
                      pick({
                        type: "channel",
                        id: c.id,
                        label: `#${c.title}`,
                      })
                    }
                  >
                    <Hash className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">{c.title}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}

            {dmGroup.length > 0 && (
              <CommandGroup heading="Direct messages">
                {dmGroup.map((c) => (
                  <CommandItem
                    key={`dm:${c.id}`}
                    value={`dm:${c.id}`}
                    onSelect={() =>
                      pick({ type: "dm", id: c.id, label: c.title })
                    }
                  >
                    <User className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">{c.title}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}

            {chats.length > 0 && (
              <CommandGroup heading="Chats">
                {chats.map((s) => (
                  <CommandItem
                    key={`chat:${s.chat_session_id}`}
                    value={`chat:${s.chat_session_id}`}
                    onSelect={() =>
                      pick({
                        type: "chat",
                        id: s.chat_session_id,
                        label: truncate(s.title || "Chat"),
                      })
                    }
                  >
                    <MessageSquare className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">{s.title || "Chat"}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
