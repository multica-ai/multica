import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { FolderKanban, CircleUser, Zap } from "lucide-react";
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command";
import { StatusIcon } from "@/features/issues/components";
import { useSearchStore } from "./store";
import { useSearchResults } from "./use-search-results";
import { useModalStore } from "@/features/modals";

/**
 * Application-wide command palette powered by shadcn CommandDialog.
 * Triggered by Cmd+K, Ctrl+K, or the sidebar search button.
 */
export function GlobalSearchDialog() {
  const isOpen = useSearchStore((s) => s.isOpen);
  const close = useSearchStore((s) => s.close);
  const [query, setQuery] = useState("");
  const navigate = useNavigate();
  const { issues, projects, members, isLoading } = useSearchResults(query);

  // Reset query when dialog closes
  const handleOpenChange = (open: boolean) => {
    if (!open) {
      close();
      setQuery("");
    }
  };

  // Navigate to a path and close the dialog
  const go = (to: string) => {
    close();
    setQuery("");
    void navigate({ to });
  };

  const hasResults =
    issues.length > 0 || projects.length > 0 || members.length > 0;
  const showEmpty = query.length > 0 && !isLoading && !hasResults;

  return (
    <CommandDialog
      open={isOpen}
      onOpenChange={handleOpenChange}
      title="Search"
      description="Search issues, projects, and members"
    >
      <Command shouldFilter={false}>
        <CommandInput
          placeholder="Search issues, projects, members..."
          value={query}
          onValueChange={setQuery}
          autoFocus
        />
        <CommandList>
          {showEmpty && (
            <CommandEmpty>No results for &ldquo;{query}&rdquo;</CommandEmpty>
          )}

          {/* Issue results */}
          {issues.length > 0 && (
            <>
              <CommandGroup heading="Issues">
                {issues.map((issue) => (
                  <CommandItem
                    key={issue.id}
                    value={issue.id}
                    onSelect={() => go(`/issues/${issue.id}`)}
                  >
                    <StatusIcon status={issue.status} className="h-4 w-4" />
                    <span className="text-xs text-muted-foreground shrink-0">
                      {issue.identifier}
                    </span>
                    <span className="truncate">{issue.title}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
              <CommandSeparator />
            </>
          )}

          {/* Project results */}
          {projects.length > 0 && (
            <>
              <CommandGroup heading="Projects">
                {projects.map((project) => (
                  <CommandItem
                    key={project.id}
                    value={project.id}
                    onSelect={() => go(`/projects/${project.id}`)}
                  >
                    <FolderKanban className="h-4 w-4 text-muted-foreground shrink-0" />
                    <span className="truncate">{project.title}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
              <CommandSeparator />
            </>
          )}

          {/* Member results */}
          {members.length > 0 && (
            <>
              <CommandGroup heading="Members">
                {members.map((member) => (
                  <CommandItem
                    key={member.id}
                    value={member.id}
                    onSelect={() => go("/settings")}
                  >
                    <CircleUser className="h-4 w-4 text-muted-foreground shrink-0" />
                    <span className="truncate">{member.name}</span>
                    <span className="ml-auto text-xs text-muted-foreground shrink-0">
                      {member.email}
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
              <CommandSeparator />
            </>
          )}

          {/* Static quick actions — always visible */}
          <CommandGroup heading="Actions">
            <CommandItem
              value="action-create-issue"
              onSelect={() => {
                close();
                setQuery("");
                useModalStore.getState().open("create-issue");
              }}
            >
              <Zap className="h-4 w-4 text-muted-foreground shrink-0" />
              <span>Create issue</span>
            </CommandItem>
            <CommandItem
              value="action-go-issues"
              onSelect={() => go("/issues")}
            >
              <Zap className="h-4 w-4 text-muted-foreground shrink-0" />
              <span>Go to Issues</span>
            </CommandItem>
            <CommandItem
              value="action-go-projects"
              onSelect={() => go("/projects")}
            >
              <Zap className="h-4 w-4 text-muted-foreground shrink-0" />
              <span>Go to Projects</span>
            </CommandItem>
            <CommandItem
              value="action-go-inbox"
              onSelect={() => go("/inbox")}
            >
              <Zap className="h-4 w-4 text-muted-foreground shrink-0" />
              <span>Go to Inbox</span>
            </CommandItem>
          </CommandGroup>
        </CommandList>
      </Command>
    </CommandDialog>
  );
}
