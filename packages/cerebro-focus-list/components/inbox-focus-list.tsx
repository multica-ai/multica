"use client";

import { useState, useRef, useCallback, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import type { FocusListItem } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
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
import {
  Plus,
  Check,
  Trash2,
  Clock,
  Link2,
  ChevronDown,
  ChevronRight,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { focusListOptions } from "../queries";
import {
  useCreateFocusListItem,
  useUpdateFocusListItem,
  useMarkFocusListItemDone,
  useSnoozeFocusListItem,
  useDeleteFocusListItem,
} from "../mutations";
import { nextLocalNineAm } from "../snooze-time";

// Only show items that are active (not done, not currently snoozed).
function isActive(item: FocusListItem): boolean {
  if (item.done_at) return false;
  if (item.snoozed_until && new Date(item.snoozed_until) > new Date()) return false;
  return true;
}

interface IssueLinkPickerProps {
  onSelect: (issueId: string, issueTitle: string) => void;
  onClear: () => void;
  currentIssueId: string | null;
  currentIssueTitle?: string;
}

function IssueLinkPicker({ onSelect, onClear, currentIssueId, currentIssueTitle }: IssueLinkPickerProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<Array<{ id: string; title: string; identifier: string }>>([]);
  const abortRef = useRef<AbortController | undefined>(undefined);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const search = useCallback((q: string) => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    if (abortRef.current) abortRef.current.abort();
    if (!q.trim()) { setResults([]); return; }
    debounceRef.current = setTimeout(async () => {
      const controller = new AbortController();
      abortRef.current = controller;
      try {
        const res = await api.searchIssues({ q: q.trim(), limit: 15, signal: controller.signal });
        if (!controller.signal.aborted) {
          setResults(res.issues.map((i) => ({ id: i.id, title: i.title, identifier: i.identifier ?? "" })));
        }
      } catch { /* aborted */ }
    }, 220);
  }, []);

  useEffect(() => { search(query); }, [query, search]);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        title={currentIssueId ? "Koblet til issue — klik for at ændre" : "Kobl til issue"}
        className={`flex size-5 items-center justify-center rounded text-muted-foreground/60 hover:text-muted-foreground ${currentIssueId ? "text-brand!" : ""}`}
      >
        <Link2 className="size-3.5" />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 p-0" side="bottom">
        <Command>
          <CommandInput
            placeholder="Søg issue…"
            value={query}
            onValueChange={setQuery}
            autoFocus
          />
          <CommandList>
            {currentIssueId && (
              <CommandGroup heading="Nuværende">
                <CommandItem
                  onSelect={() => { onClear(); setOpen(false); setQuery(""); }}
                  className="gap-2 text-muted-foreground"
                >
                  <X className="size-3.5 shrink-0" />
                  <span className="truncate">{currentIssueTitle ?? currentIssueId}</span>
                  <span className="ml-auto text-xs text-muted-foreground/60">fjern</span>
                </CommandItem>
              </CommandGroup>
            )}
            <CommandEmpty className="py-3 text-center text-sm text-muted-foreground">
              {query.trim() ? "Ingen resultater" : "Skriv for at søge…"}
            </CommandEmpty>
            {results.length > 0 && (
              <CommandGroup heading="Issues">
                {results.map((issue) => (
                  <CommandItem
                    key={issue.id}
                    value={issue.id}
                    onSelect={() => {
                      onSelect(issue.id, issue.title);
                      setOpen(false);
                      setQuery("");
                      setResults([]);
                    }}
                    className="gap-2"
                  >
                    <span className="shrink-0 text-xs text-muted-foreground">{issue.identifier}</span>
                    <span className="truncate">{issue.title}</span>
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

interface FocusListRowProps {
  item: FocusListItem;
  issueTitle?: string;
}

function FocusListRow({ item, issueTitle }: FocusListRowProps) {
  const [editing, setEditing] = useState(false);
  const [editText, setEditText] = useState(item.text);
  const markDone = useMarkFocusListItemDone();
  const snooze = useSnoozeFocusListItem();
  const deleteItem = useDeleteFocusListItem();
  const updateItem = useUpdateFocusListItem();

  const commitEdit = () => {
    const trimmed = editText.trim();
    if (trimmed && trimmed !== item.text) {
      updateItem.mutate({ id: item.id, text: trimmed }, {
        onError: () => toast.error("Kunne ikke gemme ændringen"),
      });
    } else {
      setEditText(item.text);
    }
    setEditing(false);
  };

  const handleSnooze = (until: Date) => {
    snooze.mutate({ id: item.id, until }, {
      onError: () => toast.error("Kunne ikke udsætte"),
    });
  };

  return (
    <div className="group/row flex min-h-8 items-center gap-1.5 px-3 py-1">
      {/* Done checkbox */}
      <button
        type="button"
        onClick={() => markDone.mutate(item.id, { onError: () => toast.error("Kunne ikke markere som færdig") })}
        className="flex size-4 shrink-0 items-center justify-center rounded border border-border text-muted-foreground hover:border-brand hover:text-brand"
        title="Markér som færdig"
      >
        <Check className="size-3" />
      </button>

      {/* Text / edit */}
      {editing ? (
        <input
          autoFocus
          className="flex-1 bg-transparent text-sm outline-none"
          value={editText}
          onChange={(e) => setEditText(e.target.value)}
          onBlur={commitEdit}
          onKeyDown={(e) => {
            if (e.key === "Enter") { e.preventDefault(); commitEdit(); }
            if (e.key === "Escape") { setEditText(item.text); setEditing(false); }
          }}
        />
      ) : (
        <button
          type="button"
          className="flex-1 truncate text-left text-sm"
          onClick={() => setEditing(true)}
          title="Klik for at redigere"
        >
          {item.issue_id ? (
            <span className="flex items-center gap-1 min-w-0">
              <span className="truncate">{item.text}</span>
              {issueTitle && (
                <span className="shrink-0 rounded bg-accent px-1 py-0.5 text-xs text-muted-foreground max-w-28 truncate">
                  {issueTitle}
                </span>
              )}
            </span>
          ) : item.text}
        </button>
      )}

      {/* Action buttons — visible on hover */}
      <div className="flex shrink-0 items-center gap-0.5 opacity-0 group-hover/row:opacity-100 transition-opacity">
        {/* Issue link */}
        <IssueLinkPicker
          currentIssueId={item.issue_id}
          currentIssueTitle={issueTitle}
          onSelect={(issueId) =>
            updateItem.mutate({ id: item.id, issueId }, {
              onError: () => toast.error("Kunne ikke koble issue"),
            })
          }
          onClear={() =>
            updateItem.mutate({ id: item.id, issueId: null }, {
              onError: () => toast.error("Kunne ikke fjerne issue"),
            })
          }
        />

        {/* Snooze */}
        <DropdownMenu>
          <DropdownMenuTrigger
            title="Udsæt til i morgen"
            className="flex size-5 items-center justify-center rounded text-muted-foreground/60 hover:text-muted-foreground"
          >
            <Clock className="size-3.5" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-40">
            <DropdownMenuItem onClick={() => handleSnooze(nextLocalNineAm())}>
              I morgen kl. 9
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => handleSnooze(nextLocalNineAm(3))}>
              Om 3 dage
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => handleSnooze(nextLocalNineAm(7))}>
              Om en uge
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>

        {/* Delete */}
        <button
          type="button"
          onClick={() => deleteItem.mutate(item.id, { onError: () => toast.error("Kunne ikke slette") })}
          title="Slet"
          className="flex size-5 items-center justify-center rounded text-muted-foreground/60 hover:text-destructive"
        >
          <Trash2 className="size-3.5" />
        </button>
      </div>
    </div>
  );
}

interface InboxFocusListProps {
  onIssueOpen?: (issueId: string) => void;
}

export function InboxFocusList({ onIssueOpen: _onIssueOpen }: InboxFocusListProps) {
  const wsId = useWorkspaceId();
  const { data: items = [] } = useQuery(focusListOptions(wsId));
  const createItem = useCreateFocusListItem();
  const [addText, setAddText] = useState("");
  const [collapsed, setCollapsed] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  // Issue titles for linked items — fetched lazily via search
  const [issueTitles, setIssueTitles] = useState<Record<string, string>>({});

  const activeItems = items.filter(isActive);
  const snoozedCount = items.filter(
    (i) => !i.done_at && i.snoozed_until && new Date(i.snoozed_until) > new Date(),
  ).length;

  // Fetch issue titles for all linked items
  useEffect(() => {
    const issueIds = [...new Set(activeItems.filter((i) => i.issue_id).map((i) => i.issue_id!))];
    const missing = issueIds.filter((id) => !issueTitles[id]);
    if (!missing.length) return;
    for (const id of missing) {
      api.searchIssues({ q: id, limit: 1 }).then((res) => {
        const found = res.issues.find((i) => i.id === id);
        if (found) {
          setIssueTitles((prev) => ({ ...prev, [id]: found.title }));
        }
      }).catch(() => undefined);
    }
  }, [activeItems.map((i) => i.issue_id).join(",")]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleAdd = () => {
    const text = addText.trim();
    if (!text) return;
    createItem.mutate({ text }, {
      onSuccess: () => setAddText(""),
      onError: () => toast.error("Kunne ikke tilføje"),
    });
  };

  return (
    <div className="border-b">
      {/* Header */}
      <button
        type="button"
        onClick={() => setCollapsed((c) => !c)}
        className="flex w-full items-center gap-1.5 px-3 py-2 text-left hover:bg-accent/40 transition-colors"
      >
        {collapsed ? (
          <ChevronRight className="size-3 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
        )}
        <span className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
          Fokus
        </span>
        {activeItems.length > 0 && (
          <span className="ml-1 rounded-full bg-brand/10 px-1.5 py-0.5 text-xs font-medium text-brand">
            {activeItems.length}
          </span>
        )}
        {snoozedCount > 0 && (
          <span className="ml-0.5 text-xs text-muted-foreground/60" title={`${snoozedCount} udsat`}>
            +{snoozedCount}
          </span>
        )}
      </button>

      {!collapsed && (
        <div className="pb-1.5">
          {/* Items */}
          {activeItems.map((item) => (
            <FocusListRow
              key={item.id}
              item={item}
              issueTitle={item.issue_id ? issueTitles[item.issue_id] : undefined}
            />
          ))}

          {/* Quick-add */}
          <div className="flex items-center gap-1 px-3 pt-0.5">
            <Plus className="size-3.5 shrink-0 text-muted-foreground/50" />
            <Input
              ref={inputRef}
              value={addText}
              onChange={(e) => setAddText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") { e.preventDefault(); handleAdd(); }
              }}
              placeholder="Tilføj opgave…"
              className="h-7 border-none bg-transparent px-0 text-sm shadow-none placeholder:text-muted-foreground/50 focus-visible:ring-0"
            />
            {addText.trim() && (
              <Button
                size="sm"
                variant="ghost"
                className="h-6 px-2 text-xs"
                onClick={handleAdd}
                disabled={createItem.isPending}
              >
                Tilføj
              </Button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
