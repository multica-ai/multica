import { useRef, useState, type CSSProperties } from "react";
import { MessageSquare, Search } from "lucide-react";
import type { LocalFolderProject, LocalProviderId } from "@multica/core/local-conversation/protocol";
import { Button } from "@multica/ui/components/ui/button";
import { Command, CommandGroup, CommandInput, CommandItem, CommandList } from "@multica/ui/components/ui/command";
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from "@multica/ui/components/ui/dialog";
import { groupLocalConversations, type LocalSessionEntry } from "../platform/local-conversation-projects";

interface LocalConversationSearchProps {
  sessions: LocalSessionEntry[];
  projects: LocalFolderProject[];
  archived: boolean;
  pending: boolean;
  error: Error | null;
  retry(): void;
  select(id: string, provider: LocalProviderId): void;
  labels: { search: string; noResults: string; loading: string; retry: string };
  unassigned: string;
}

export function LocalConversationSearch({ sessions, projects, archived, pending, error, retry, select, labels, unassigned }: LocalConversationSearchProps) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const groups = groupLocalConversations(sessions, projects, archived, search).filter((group) => group.sessions.length);
  const changeOpen = (value: boolean) => {
    setOpen(value);
    if (!value) setSearch("");
  };

  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogTrigger render={<Button variant="ghost" size="icon-sm" aria-label={labels.search} title={labels.search} style={{ WebkitAppRegion: "no-drag" } as CSSProperties} />}>
        <Search />
      </DialogTrigger>
      <DialogContent className="flex flex-col gap-0 overflow-hidden p-0 sm:max-w-xl" initialFocus={input} showCloseButton={false}>
        <DialogTitle className="sr-only">{labels.search}</DialogTitle>
        <Command className="min-h-0" shouldFilter={false} label={labels.search}>
          <CommandInput ref={input} placeholder={labels.search} value={search} onValueChange={setSearch} />
          <CommandList className="min-h-0 max-h-96" aria-label={labels.search} aria-busy={pending}>
            {pending ? (
              <p role="status" className="p-6 text-center text-body text-muted-foreground">{labels.loading}</p>
            ) : error ? (
              <div role="alert" className="flex flex-col items-center gap-2 p-6 text-body text-muted-foreground">
                <span>{error.message}</span>
                <Button variant="outline" size="sm" onClick={retry}>{labels.retry}</Button>
              </div>
            ) : groups.length ? groups.map((group) => (
              <CommandGroup key={group.id ?? "unassigned"} heading={group.project?.title ?? unassigned}>
                {group.sessions.map((entry) => (
                  <CommandItem className="data-[selected=false]:bg-transparent" key={entry.id} value={entry.id} onSelect={() => { changeOpen(false); select(entry.id, entry.provider); }}>
                    <MessageSquare className="text-muted-foreground" />
                    <span className="min-w-0 flex-1 py-1">
                      <span className="block truncate" title={entry.title}>{entry.title}</span>
                      {entry.preview && <span className="block truncate text-caption text-muted-foreground" title={entry.preview}>{entry.preview}</span>}
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )) : (
              <p role="status" className="p-6 text-center text-body text-muted-foreground">{labels.noResults}</p>
            )}
          </CommandList>
        </Command>
      </DialogContent>
    </Dialog>
  );
}
