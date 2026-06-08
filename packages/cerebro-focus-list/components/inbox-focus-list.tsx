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
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
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
  GripVertical,
} from "lucide-react";
import { toast } from "sonner";
import { focusListOptions } from "../queries";
import {
  useCreateFocusListItem,
  useUpdateFocusListItem,
  useMarkFocusListItemDone,
  useSnoozeFocusListItem,
  useDeleteFocusListItem,
  useReorderFocusListItems,
} from "../mutations";
import { nextLocalNineAm } from "../snooze-time";

const MAX_VISIBLE = 3;

// Local-day ISO key (YYYY-MM-DD). Done items linger until this key flips at
// local midnight, so a task ticked at 23:59 disappears at 00:00.
function localDayKey(date: Date): string {
  const y = date.getFullYear();
  const m = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

// Visible = not currently snoozed AND not done-on-a-previous-day. A done item
// from today stays visible (with strike-through) until the user deletes it or
// the local day rolls over.
function isVisibleToday(item: FocusListItem, today: string): boolean {
  if (item.snoozed_until && new Date(item.snoozed_until) > new Date()) return false;
  if (item.done_at && localDayKey(new Date(item.done_at)) !== today) return false;
  return true;
}

interface IssueLinkPickerProps {
  onSelect: (issueId: string, issueTitle: string) => void;
  onClear: () => void;
  currentIssueId: string | null;
  currentIssueTitle?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

function IssueLinkPicker({ onSelect, onClear, currentIssueId, currentIssueTitle, open, onOpenChange }: IssueLinkPickerProps) {
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
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger
        title={currentIssueId ? "Linked to issue — click to change" : "Link to issue"}
        className={`flex size-5 items-center justify-center rounded text-muted-foreground/60 hover:text-muted-foreground ${currentIssueId ? "text-brand!" : ""}`}
      >
        <Link2 className="size-3.5" />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 p-0" side="bottom">
        <Command>
          <CommandInput
            placeholder="Search issue…"
            value={query}
            onValueChange={setQuery}
            autoFocus
          />
          <CommandList>
            {currentIssueId && (
              <CommandGroup heading="Current">
                <CommandItem
                  onSelect={() => { onClear(); onOpenChange(false); setQuery(""); }}
                  className="gap-2 text-muted-foreground"
                >
                  <X className="size-3.5 shrink-0" />
                  <span className="truncate">{currentIssueTitle ?? currentIssueId}</span>
                  <span className="ml-auto text-xs text-muted-foreground/60">remove</span>
                </CommandItem>
              </CommandGroup>
            )}
            <CommandEmpty className="py-3 text-center text-sm text-muted-foreground">
              {query.trim() ? "No results" : "Type to search…"}
            </CommandEmpty>
            {results.length > 0 && (
              <CommandGroup heading="Issues">
                {results.map((issue) => (
                  <CommandItem
                    key={issue.id}
                    value={issue.id}
                    onSelect={() => {
                      onSelect(issue.id, issue.title);
                      onOpenChange(false);
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
  priorityNumber: number | null;
  isDragging: boolean;
  isDragOver: boolean;
  onDragStart: (id: string) => void;
  onDragOver: (id: string) => void;
  onDrop: () => void;
  onDragEnd: () => void;
}

function FocusListRow({
  item,
  issueTitle,
  priorityNumber,
  isDragging,
  isDragOver,
  onDragStart,
  onDragOver,
  onDrop,
  onDragEnd,
}: FocusListRowProps) {
  const [editing, setEditing] = useState(false);
  const [editText, setEditText] = useState(item.text);
  const [contextOpen, setContextOpen] = useState(false);
  const [linkPickerOpen, setLinkPickerOpen] = useState(false);
  const markDone = useMarkFocusListItemDone();
  const snooze = useSnoozeFocusListItem();
  const deleteItem = useDeleteFocusListItem();
  const updateItem = useUpdateFocusListItem();
  const isDone = !!item.done_at;

  const commitEdit = () => {
    const trimmed = editText.trim();
    if (trimmed && trimmed !== item.text) {
      updateItem.mutate({ id: item.id, text: trimmed }, {
        onError: () => toast.error("Could not save change"),
      });
    } else {
      setEditText(item.text);
    }
    setEditing(false);
  };

  const handleSnooze = (until: Date) => {
    snooze.mutate({ id: item.id, until }, {
      onError: () => toast.error("Could not snooze"),
    });
  };

  return (
    <div
      draggable={!editing}
      onDragStart={(e) => {
        e.dataTransfer.effectAllowed = "move";
        onDragStart(item.id);
      }}
      onDragOver={(e) => {
        e.preventDefault();
        e.dataTransfer.dropEffect = "move";
        onDragOver(item.id);
      }}
      onDrop={(e) => {
        e.preventDefault();
        onDrop();
      }}
      onDragEnd={onDragEnd}
      className={`group/row flex min-h-8 items-start gap-1.5 px-3 py-1 ${
        isDragging ? "opacity-40" : ""
      } ${isDragOver ? "border-t-2 border-brand" : ""}`}
    >
      {/* Drag handle */}
      <span
        className="flex size-4 shrink-0 cursor-grab items-center justify-center text-muted-foreground/40 hover:text-muted-foreground active:cursor-grabbing"
        title="Drag to reorder"
      >
        <GripVertical className="size-3.5" />
      </span>

      {/* Priority number — only shown for top 5 */}
      {priorityNumber !== null && (
        <span
          className="flex size-4 shrink-0 items-center justify-center rounded bg-accent text-[10px] font-semibold text-muted-foreground"
          title={`Priority ${priorityNumber}`}
        >
          {priorityNumber}
        </span>
      )}

      {/* Done toggle — empty circle when active, filled with check when done */}
      <button
        type="button"
        onClick={() => {
          if (isDone) return;
          markDone.mutate(item.id, { onError: () => toast.error("Could not mark as done") });
        }}
        disabled={isDone}
        className={`flex size-4 shrink-0 items-center justify-center rounded-full border transition-colors ${
          isDone
            ? "border-brand bg-brand text-white"
            : "border-muted-foreground/40 text-transparent hover:border-brand hover:text-brand/40"
        }`}
        title={isDone ? "Marked as done today" : "Mark as done"}
      >
        {isDone && <Check className="size-2.5" strokeWidth={3} />}
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
          className={`flex-1 text-left text-sm ${
            isDone ? "text-muted-foreground/60 line-through" : ""
          }`}
          onClick={() => setEditing(true)}
          onContextMenu={(e) => { e.preventDefault(); setContextOpen(true); }}
          title="Click to edit"
        >
          {item.issue_id ? (
            <span className="flex items-center gap-1 min-w-0 flex-wrap">
              <span>{item.text}</span>
              {issueTitle && (
                <span className="shrink-0 rounded bg-accent px-1 py-0.5 text-xs text-muted-foreground max-w-28 truncate">
                  {issueTitle}
                </span>
              )}
            </span>
          ) : item.text}
        </button>
      )}

      {/* Action buttons — visible on hover (desktop) */}
      <div className="flex shrink-0 items-center gap-0.5 opacity-0 group-hover/row:opacity-100 transition-opacity">
        <IssueLinkPicker
          currentIssueId={item.issue_id}
          currentIssueTitle={issueTitle}
          open={linkPickerOpen}
          onOpenChange={setLinkPickerOpen}
          onSelect={(issueId) =>
            updateItem.mutate({ id: item.id, issueId }, {
              onError: () => toast.error("Could not link issue"),
            })
          }
          onClear={() =>
            updateItem.mutate({ id: item.id, issueId: null }, {
              onError: () => toast.error("Could not unlink issue"),
            })
          }
        />

        <DropdownMenu>
          <DropdownMenuTrigger
            title="Snooze"
            className="flex size-5 items-center justify-center rounded text-muted-foreground/60 hover:text-muted-foreground"
          >
            <Clock className="size-3.5" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-40">
            <DropdownMenuItem onClick={() => handleSnooze(nextLocalNineAm())}>
              Tomorrow at 9
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => handleSnooze(nextLocalNineAm(3))}>
              In 3 days
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => handleSnooze(nextLocalNineAm(7))}>
              In a week
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>

        <button
          type="button"
          onClick={() => deleteItem.mutate(item.id, { onError: () => toast.error("Could not delete") })}
          title="Delete"
          className="flex size-5 items-center justify-center rounded text-muted-foreground/60 hover:text-destructive"
        >
          <Trash2 className="size-3.5" />
        </button>
      </div>

      {/* Long-press context menu — opens on contextmenu event (long press on mobile, right-click on desktop) */}
      <DropdownMenu open={contextOpen} onOpenChange={setContextOpen}>
        <DropdownMenuTrigger className="size-0 shrink-0 opacity-0 pointer-events-none" tabIndex={-1} aria-hidden="true" />
        <DropdownMenuContent align="end" side="bottom" className="w-44">
          <DropdownMenuItem onClick={() => { setContextOpen(false); setLinkPickerOpen(true); }}>
            <Link2 className="size-3.5" />
            Link issue
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuLabel>Snooze</DropdownMenuLabel>
            <DropdownMenuItem onClick={() => handleSnooze(nextLocalNineAm())}>
              <Clock className="size-3.5" />
              Tomorrow at 9
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => handleSnooze(nextLocalNineAm(3))}>
              In 3 days
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => handleSnooze(nextLocalNineAm(7))}>
              In a week
            </DropdownMenuItem>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            variant="destructive"
            onClick={() => deleteItem.mutate(item.id, { onError: () => toast.error("Could not delete") })}
          >
            <Trash2 className="size-3.5" />
            Delete
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
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
  const reorder = useReorderFocusListItems();
  const [addText, setAddText] = useState("");
  const [collapsed, setCollapsed] = useState(false);
  const [showAll, setShowAll] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  // Re-evaluate isVisibleToday at day rollover. The active list also depends
  // on snoozed_until, which is checked against Date.now(); a re-render every
  // minute is enough to surface re-emerging snoozed items without a refetch.
  const [today, setToday] = useState(() => localDayKey(new Date()));
  useEffect(() => {
    const tick = () => setToday(localDayKey(new Date()));
    const id = window.setInterval(tick, 60_000);
    return () => window.clearInterval(id);
  }, []);

  const [draggingId, setDraggingId] = useState<string | null>(null);
  const [dragOverId, setDragOverId] = useState<string | null>(null);

  // Issue titles for linked items — fetched lazily via search
  const [issueTitles, setIssueTitles] = useState<Record<string, string>>({});

  const visibleItems = items.filter((i) => isVisibleToday(i, today));
  // Snoozed counter — items that will resurface later. Done items stay
  // in-list (visible), so they no longer contribute to the "+N" pill.
  const snoozedCount = items.filter(
    (i) => !i.done_at && i.snoozed_until && new Date(i.snoozed_until) > new Date(),
  ).length;

  const overflowCount = Math.max(0, visibleItems.length - MAX_VISIBLE);
  const displayItems = showAll ? visibleItems : visibleItems.slice(0, MAX_VISIBLE);

  // Fetch issue titles for all linked items
  useEffect(() => {
    const issueIds = [...new Set(visibleItems.filter((i) => i.issue_id).map((i) => i.issue_id!))];
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
  }, [visibleItems.map((i) => i.issue_id).join(",")]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleAdd = () => {
    const text = addText.trim();
    if (!text) return;
    createItem.mutate({ text }, {
      onSuccess: () => setAddText(""),
      onError: () => toast.error("Could not add"),
    });
  };

  const handleDrop = () => {
    if (!draggingId || !dragOverId || draggingId === dragOverId) {
      setDraggingId(null);
      setDragOverId(null);
      return;
    }
    const order = visibleItems.map((i) => i.id);
    const fromIdx = order.indexOf(draggingId);
    const toIdx = order.indexOf(dragOverId);
    if (fromIdx === -1 || toIdx === -1) {
      setDraggingId(null);
      setDragOverId(null);
      return;
    }
    const next = [...order];
    const [moved] = next.splice(fromIdx, 1);
    if (moved) next.splice(toIdx, 0, moved);
    reorder.mutate(next, {
      onError: () => toast.error("Could not reorder"),
    });
    setDraggingId(null);
    setDragOverId(null);
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
          Priorities
        </span>
        {visibleItems.length > 0 && (
          <span className="ml-1 rounded-full bg-brand/10 px-1.5 py-0.5 text-xs font-medium text-brand">
            {visibleItems.length}
          </span>
        )}
        {snoozedCount > 0 && (
          <span className="ml-0.5 text-xs text-muted-foreground/60" title={`${snoozedCount} snoozed`}>
            +{snoozedCount}
          </span>
        )}
      </button>

      {!collapsed && (
        <div className="pb-1.5">
          {/* Items */}
          {displayItems.map((item, idx) => (
            <FocusListRow
              key={item.id}
              item={item}
              issueTitle={item.issue_id ? issueTitles[item.issue_id] : undefined}
              priorityNumber={idx < 5 ? idx + 1 : null}
              isDragging={draggingId === item.id}
              isDragOver={dragOverId === item.id && draggingId !== item.id}
              onDragStart={setDraggingId}
              onDragOver={setDragOverId}
              onDrop={handleDrop}
              onDragEnd={() => { setDraggingId(null); setDragOverId(null); }}
            />
          ))}

          {/* Show more / less toggle */}
          {overflowCount > 0 && (
            <button
              type="button"
              onClick={() => setShowAll((v) => !v)}
              className="flex w-full items-center gap-1 px-3 py-1 text-left text-xs text-muted-foreground hover:bg-accent/40 hover:text-foreground"
            >
              {showAll ? (
                <>
                  <ChevronDown className="size-3 shrink-0 rotate-180" />
                  Show less
                </>
              ) : (
                <>
                  <ChevronDown className="size-3 shrink-0" />
                  Show {overflowCount} more
                </>
              )}
            </button>
          )}

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
              placeholder="Add a priority…"
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
                Add
              </Button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
