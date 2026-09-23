"use client";

import { issueStatusCategory } from "@multica/core/issues";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useState, useEffect, useCallback, useRef } from "react";
import type { Issue } from "@multica/core/types";
import { api } from "@multica/core/api";
import {
  Command,
  CommandDialog,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
} from "@multica/ui/components/ui/command";
import { StatusIcon } from "../issues/components/status-icon";
import { useT } from "../i18n";

/** Issues offered before the user types, under a heading. */
export interface IssuePickerSuggestionGroup {
  key: string;
  heading: string;
  issues: Issue[];
}

interface IssuePickerModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  excludeIds: string[];
  onSelect: (issue: Issue) => void;
  /** Shown while the query is empty; groups without issues are skipped. */
  suggestions?: IssuePickerSuggestionGroup[];
  /** Drops issues from results and suggestions alike; excludeIds always applies. */
  isSelectable?: (issue: Issue) => boolean;
}

export function IssuePickerModal({
  open,
  onOpenChange,
  title,
  description,
  excludeIds,
  onSelect,
  suggestions,
  isSelectable,
}: IssuePickerModalProps) {
  const { t } = useT("modals");
  const { colorOf, iconOf } = useIssueStatuses(useWorkspaceId());
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<Issue[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const abortRef = useRef<AbortController>(undefined);
  const selectable = useCallback(
    (issue: Issue) => !excludeIds.includes(issue.id) && (isSelectable?.(issue) ?? true),
    [excludeIds, isSelectable],
  );
  const suggestionGroups = (suggestions ?? [])
    .map((group) => ({ ...group, issues: group.issues.filter(selectable) }))
    .filter((group) => group.issues.length > 0);

  useEffect(() => {
    if (!open) {
      setQuery("");
      setResults([]);
      setIsLoading(false);
    }
  }, [open]);

  const search = useCallback(
    (q: string) => {
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
            setResults(res.issues.filter(selectable));
            setIsLoading(false);
          }
        } catch {
          if (!controller.signal.aborted) {
            setIsLoading(false);
          }
        }
      }, 300);
    },
    [selectable],
  );

  const row = (issue: Issue) => (
    <CommandItem
      key={issue.id}
      value={issue.id}
      onSelect={() => {
        onSelect(issue);
        onOpenChange(false);
      }}
    >
      <StatusIcon
        status={issue.status}
        color={colorOf(issue.status)}
        icon={iconOf(issue.status)}
        category={issueStatusCategory(issue) ?? undefined}
        className="h-3.5 w-3.5 shrink-0"
      />
      <span className="text-muted-foreground shrink-0">{issue.identifier}</span>
      <span className="truncate">{issue.title}</span>
    </CommandItem>
  );

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title={title}
      description={description}
    >
      <Command shouldFilter={false}>
        <CommandInput
          placeholder={t(($) => $.issue_picker.search_placeholder)}
          value={query}
          onValueChange={(v) => {
            setQuery(v);
            search(v);
          }}
        />
        <CommandList>
          {isLoading && (
            <div className="py-6 text-center text-body text-muted-foreground">
              {t(($) => $.issue_picker.searching)}
            </div>
          )}
          {!isLoading && query.trim() && results.length === 0 && (
            <CommandEmpty>{t(($) => $.issue_picker.no_results)}</CommandEmpty>
          )}
          {!isLoading && !query.trim() && suggestionGroups.length === 0 && (
            <div className="py-6 text-center text-body text-muted-foreground">
              {t(($) => $.issue_picker.prompt_to_search)}
            </div>
          )}
          {!isLoading && !query.trim() &&
            suggestionGroups.map((group) => (
              <CommandGroup key={group.key} heading={group.heading}>
                {group.issues.map(row)}
              </CommandGroup>
            ))}
          {results.length > 0 && <CommandGroup>{results.map(row)}</CommandGroup>}
        </CommandList>
      </Command>
    </CommandDialog>
  );
}
