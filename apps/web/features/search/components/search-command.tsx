"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "@/i18n/navigation";
import { useTranslations } from "next-intl";
import { Loader2, MessageSquare, SearchIcon } from "lucide-react";
import { Command as CommandPrimitive } from "cmdk";
import { useQuery } from "@tanstack/react-query";
import type { SearchIssueResult } from "@multica/core/types";
import { api } from "@/platform/api";
import { StatusIcon } from "@multica/views/issues/components";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@multica/ui/components/ui/dialog";

export function SearchCommand() {
  const router = useRouter();
  const t = useTranslations("search");
  const tIssues = useTranslations("issues");
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Global Cmd+K / Ctrl+K shortcut
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setOpen((prev) => !prev);
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, []);

  // Cleanup debounce on unmount
  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, []);

  // Reset state when dialog closes
  useEffect(() => {
    if (!open) {
      setQuery("");
      setDebouncedQuery("");
    }
  }, [open]);

  const { data, isFetching } = useQuery({
    queryKey: ["issueSearch", debouncedQuery],
    queryFn: () =>
      api.searchIssues({ q: debouncedQuery.trim(), limit: 20, include_closed: true }),
    enabled: !!debouncedQuery.trim(),
    staleTime: 30_000,
  });

  const results: SearchIssueResult[] = data?.issues ?? [];

  const handleValueChange = useCallback((value: string) => {
    setQuery(value);
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      setDebouncedQuery(value);
    }, 300);
  }, []);

  const handleSelect = useCallback(
    (issueId: string) => {
      setOpen(false);
      router.push(`/issues/${issueId}`);
    },
    [router],
  );

  const isLoading = isFetching && !!debouncedQuery.trim();

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent
        className="top-[20%] translate-y-0 overflow-hidden rounded-xl! p-0 sm:max-w-xl!"
        showCloseButton={false}
      >
        <DialogHeader className="sr-only">
          <DialogTitle>{t("title")}</DialogTitle>
          <DialogDescription>
            {t("description")}
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
              placeholder={t("placeholder")}
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

            {!isLoading && debouncedQuery.trim() && results.length === 0 && (
              <CommandPrimitive.Empty className="py-10 text-center text-sm text-muted-foreground">
                {t("noIssuesFound")}
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
                      <span className="truncate">{issue.title}</span>
                      <span
                        className={`ml-auto text-xs shrink-0 ${STATUS_CONFIG[issue.status].iconColor}`}
                      >
                        {tIssues(`statusLabels.${issue.status}` as Parameters<typeof tIssues>[0])}
                      </span>
                    </div>
                    {issue.match_source === "comment" &&
                      issue.matched_snippet && (
                        <div className="flex items-start gap-2 pl-[26px]">
                          <MessageSquare className="size-3 shrink-0 text-muted-foreground mt-0.5" />
                          <span className="text-xs text-muted-foreground truncate">
                            {issue.matched_snippet}
                          </span>
                        </div>
                      )}
                  </CommandPrimitive.Item>
                ))}
              </CommandPrimitive.Group>
            )}

            {!isLoading && !query.trim() && (
              <div className="py-10 text-center text-sm text-muted-foreground">
                {t("typeToSearch")}
              </div>
            )}
          </CommandPrimitive.List>
        </CommandPrimitive>
      </DialogContent>
    </Dialog>
  );
}
