"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Loader2, MessageSquare, SearchIcon } from "lucide-react";
import { Command as CommandPrimitive } from "cmdk";
import type { SearchIssueResult } from "@multica/core/types";
import { api } from "@multica/core/api";
import { StatusIcon } from "../issues/components";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@multica/ui/components/ui/dialog";
import { useNavigation } from "../navigation";
import { useSearchStore } from "./search-store";

function HighlightText({ text, query }: { text: string; query: string }) {
  const parts = useMemo(() => {
    if (!query.trim()) return [{ text, highlight: false }];
    const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const regex = new RegExp(`(${escaped})`, "gi");
    const result: { text: string; highlight: boolean }[] = [];
    let lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = regex.exec(text)) !== null) {
      if (match.index > lastIndex) {
        result.push({ text: text.slice(lastIndex, match.index), highlight: false });
      }
      result.push({ text: match[0], highlight: true });
      lastIndex = regex.lastIndex;
    }
    if (lastIndex < text.length) {
      result.push({ text: text.slice(lastIndex), highlight: false });
    }
    return result.length > 0 ? result : [{ text, highlight: false }];
  }, [text, query]);

  return (
    <>
      {parts.map((part, i) =>
        part.highlight ? (
          <mark key={i} className="bg-yellow-200 dark:bg-yellow-900/60 text-inherit rounded-sm">
            {part.text}
          </mark>
        ) : (
          part.text
        ),
      )}
    </>
  );
}

export function SearchCommand() {
  const { push } = useNavigation();
  const open = useSearchStore((s) => s.open);
  const setOpen = useSearchStore((s) => s.setOpen);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchIssueResult[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  // Global Cmd+K / Ctrl+K shortcut
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        useSearchStore.getState().toggle();
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, []);

  // Cleanup debounce/abort on unmount
  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      if (abortRef.current) abortRef.current.abort();
    };
  }, []);

  // Reset state when dialog closes
  useEffect(() => {
    if (!open) {
      setQuery("");
      setResults([]);
      setIsLoading(false);
    }
  }, [open]);

  const search = useCallback((q: string) => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    if (abortRef.current) abortRef.current.abort();

    if (!q.trim()) {
      setResults([]);
      setIsLoading(false);
      return;
    }

    setIsLoading(true);
    debounceRef.current = setTimeout(async () => {
      const controller = new AbortController();
      abortRef.current = controller;
      try {
        const res = await api.searchIssues({
          q: q.trim(),
          limit: 20,
          include_closed: true,
          signal: controller.signal,
        });
        if (!controller.signal.aborted) {
          setResults(res.issues);
          setIsLoading(false);
        }
      } catch {
        if (!controller.signal.aborted) {
          setIsLoading(false);
        }
      }
    }, 300);
  }, []);

  const handleValueChange = useCallback(
    (value: string) => {
      setQuery(value);
      search(value);
    },
    [search],
  );

  const handleSelect = useCallback(
    (issueId: string) => {
      setOpen(false);
      push(`/issues/${issueId}`);
    },
    [push],
  );

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent
        className="top-[20%] translate-y-0 overflow-hidden rounded-xl! p-0 sm:max-w-xl!"
        showCloseButton={false}
      >
        <DialogHeader className="sr-only">
          <DialogTitle>Search Issues</DialogTitle>
          <DialogDescription>
            Search issues by title, description, or comments
          </DialogDescription>
        </DialogHeader>
        <CommandPrimitive
          shouldFilter={false}
          className="flex size-full flex-col overflow-hidden rounded-xl bg-popover text-popover-foreground"
        >
          {/* Search input */}
          <div className="flex items-center gap-3 border-b px-4 py-3">
            <SearchIcon className="size-5 shrink-0 text-muted-foreground" />
            <CommandPrimitive.Input
              placeholder="Type a command or search..."
              value={query}
              onValueChange={handleValueChange}
              className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
            />
            <kbd className="hidden shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground sm:inline">
              ESC
            </kbd>
          </div>

          {/* Results list */}
          <CommandPrimitive.List className="max-h-[min(400px,50vh)] overflow-y-auto overflow-x-hidden">
            {isLoading && (
              <div className="flex items-center justify-center py-10">
                <Loader2 className="size-5 animate-spin text-muted-foreground" />
              </div>
            )}

            {!isLoading && query.trim() && results.length === 0 && (
              <CommandPrimitive.Empty className="py-10 text-center text-sm text-muted-foreground">
                No issues found.
              </CommandPrimitive.Empty>
            )}

            {!isLoading && results.length > 0 && (
              <CommandPrimitive.Group className="p-2">
                {results.map((issue) => (
                  <CommandPrimitive.Item
                    key={issue.id}
                    value={issue.id}
                    onSelect={handleSelect}
                    className="flex cursor-default select-none flex-col gap-1 rounded-lg px-3 py-2.5 text-sm outline-none data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50 data-selected:bg-accent"
                  >
                    <div className="flex items-center gap-2.5">
                      <StatusIcon
                        status={issue.status}
                        className="size-4 shrink-0"
                      />
                      <span className="text-xs text-muted-foreground shrink-0">
                        {issue.identifier}
                      </span>
                      <span className="truncate">
                        <HighlightText text={issue.title} query={query} />
                      </span>
                      <span
                        className={`ml-auto text-xs shrink-0 ${STATUS_CONFIG[issue.status].iconColor}`}
                      >
                        {STATUS_CONFIG[issue.status].label}
                      </span>
                    </div>
                    {issue.match_source === "comment" &&
                      issue.matched_snippet && (
                        <div className="flex items-start gap-2 pl-[26px]">
                          <MessageSquare className="size-3 shrink-0 text-muted-foreground mt-0.5" />
                          <span className="text-xs text-muted-foreground truncate">
                            <HighlightText
                              text={issue.matched_snippet}
                              query={query}
                            />
                          </span>
                        </div>
                      )}
                  </CommandPrimitive.Item>
                ))}
              </CommandPrimitive.Group>
            )}

            {!isLoading && !query.trim() && (
              <div className="flex flex-col items-center gap-2 py-10 text-sm text-muted-foreground">
                <span>Type to search issues...</span>
                <span className="text-xs">Press <kbd className="rounded bg-muted px-1.5 py-0.5 font-medium">⌘K</kbd> to open this anytime</span>
              </div>
            )}
          </CommandPrimitive.List>
        </CommandPrimitive>
      </DialogContent>
    </Dialog>
  );
}
