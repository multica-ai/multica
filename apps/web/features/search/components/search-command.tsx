"use client";

import { useEffect, useCallback } from "react";
import { useRouter } from "next/navigation";
import { Loader2 } from "lucide-react";
import {
  Command,
  CommandDialog,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
} from "@/components/ui/command";
import { StatusIcon, PriorityIcon } from "@/features/issues";
import type { IssueStatus, IssuePriority } from "@/shared/types";
import { useSearch } from "../hooks/use-search";
import { useSearchCommandStore } from "../stores/search-command-store";

export function SearchCommand() {
  const open = useSearchCommandStore((s) => s.open);
  const setOpen = useSearchCommandStore((s) => s.setOpen);
  const toggle = useSearchCommandStore((s) => s.toggle);
  const router = useRouter();
  const { results, isLoading, search, clear } = useSearch();

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        toggle();
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [toggle]);

  const handleOpenChange = useCallback(
    (value: boolean) => {
      setOpen(value);
      if (!value) clear();
    },
    [setOpen, clear],
  );

  const handleSelect = useCallback(
    (issueId: string) => {
      setOpen(false);
      clear();
      router.push(`/issues/${issueId}`);
    },
    [router, clear],
  );

  return (
    <CommandDialog
      open={open}
      onOpenChange={handleOpenChange}
      title="Search issues"
      description="Search for issues by title, description, or number"
      showCloseButton={false}
    >
      <Command shouldFilter={false}>
        <CommandInput
          placeholder="Search issues..."
          onValueChange={search}
        />
        <CommandList>
          {isLoading && results.length === 0 ? (
            <div className="flex items-center justify-center py-6">
              <Loader2 className="size-4 animate-spin text-muted-foreground" />
            </div>
          ) : (
            <>
              <CommandEmpty>No results found.</CommandEmpty>
              {results.length > 0 && (
                <CommandGroup heading="Issues">
                  {results.map((issue) => (
                    <CommandItem
                      key={issue.id}
                      value={issue.id}
                      onSelect={() => handleSelect(issue.id)}
                      className="flex items-center gap-2"
                    >
                      <StatusIcon
                        status={issue.status as IssueStatus}
                        className="size-4 shrink-0"
                      />
                      <span className="shrink-0 text-xs text-muted-foreground">
                        {issue.identifier}
                      </span>
                      <span className="truncate">{issue.title}</span>
                      <PriorityIcon
                        priority={issue.priority as IssuePriority}
                        className="ml-auto shrink-0"
                      />
                    </CommandItem>
                  ))}
                </CommandGroup>
              )}
            </>
          )}
        </CommandList>
      </Command>
    </CommandDialog>
  );
}
