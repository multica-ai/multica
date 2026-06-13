// TECH-3413 — the Dynamic inbox: one outer box holding tabs + stackable,
// configurable sections over the same data as the classic inbox. Light mode and
// rows match the classic inbox (reused row components). Switching back to the
// classic inbox lives in the ⋯ menu.
"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Plus, MoreHorizontal, LayoutList, Search, X, Inbox, GripVertical, ArrowLeft, Pencil, Save, Trash2 } from "lucide-react";
import {
  DndContext,
  closestCenter,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  verticalListSortingStrategy,
  useSortable,
  arrayMove,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useWorkspaceId } from "@multica/core/hooks";
import { openCreateIssueWithPreference } from "@multica/core/issues/stores/create-mode-store";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuLabel,
  DropdownMenuGroup,
} from "@multica/ui/components/ui/dropdown-menu";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { useQueryClient } from "@tanstack/react-query";
import { useMarkInboxRead, useArchiveInbox } from "@multica/core/inbox/mutations";
import { useArchiveChannel } from "@multica/cerebro-channels";
import {
  GlobalInboxReminderDialog,
  useInboxActionGroupLabels,
  useSetInboxMode,
} from "@multica/cerebro-inbox";
import { api } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import { IssueDetail } from "@multica/views/issues/components";
import { ChannelDetail, NewMessageModal } from "@multica/views/channels";
import { InboxChatPanel } from "@multica/views/inbox/components/inbox-list-item";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { SlackBlock } from "@multica/cerebro-inbox-slack-block";
import type { Channel } from "@multica/core/types";
import { useDynamicInboxData } from "../use-dynamic-inbox-data";
import { useInboxLayout } from "../use-inbox-layout";
import {
  SECTION_CATALOG,
  makeId,
  type InboxLayout,
  type InboxSectionConfig,
  type InboxTabConfig,
  type SectionKind,
} from "../layout";
import type { DynInboxEntry } from "../section-filter";
import { DynamicInboxCreateMenu } from "./dynamic-inbox-create-menu";
import { DynamicInboxSection } from "./dynamic-inbox-section";

function replaceTab(layout: InboxLayout, tabId: string, fn: (t: InboxTabConfig) => InboxTabConfig): InboxLayout {
  return { ...layout, tabs: layout.tabs.map((t) => (t.id === tabId ? fn(t) : t)) };
}

// TECH-3413 #6 — stable identity of an entry so a frozen snapshot can be
// matched back to its live row after a refetch (ids survive read/state changes).
function entryIdentity(entry: DynInboxEntry): string {
  return `${entry.kind}:${entry.id}`;
}

// TECH-3413 #1 — wraps one section in a @dnd-kit sortable. The render prop
// receives the drag handle to place inside the section header so only the
// grip starts a drag (the rest of the box stays clickable).
function SortableSection({
  id,
  highlight,
  children,
}: {
  id: string;
  /** TECH-3502 #5 — flash a ring around a just-added box. */
  highlight?: boolean;
  children: (handle: React.ReactNode) => React.ReactNode;
}) {
  const { setNodeRef, setActivatorNodeRef, attributes, listeners, transform, transition, isDragging } =
    useSortable({ id });
  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    zIndex: isDragging ? 20 : undefined,
    opacity: isDragging ? 0.85 : undefined,
  };
  const handle = (
    <button
      ref={setActivatorNodeRef}
      type="button"
      {...attributes}
      {...listeners}
      className="-ml-1 cursor-grab touch-none rounded p-0.5 text-muted-foreground/50 hover:bg-muted hover:text-muted-foreground active:cursor-grabbing"
      title="Drag to reorder"
      aria-label="Drag to reorder section"
    >
      <GripVertical className="size-3.5" />
    </button>
  );
  return (
    <div
      ref={setNodeRef}
      style={style}
      className={
        highlight
          ? "rounded-xl ring-2 ring-primary ring-offset-2 ring-offset-background transition-shadow"
          : "transition-shadow"
      }
    >
      {children(handle)}
    </div>
  );
}

export function DynamicInbox() {
  const wsId = useWorkspaceId();
  const { layout, setLayout, userPresets, saveUserPreset, deleteUserPreset } = useInboxLayout();
  const { entries, filterContext, projects, loading } = useDynamicInboxData(wsId);
  const actionLabels = useInboxActionGroupLabels();
  const setMode = useSetInboxMode();
  const slackBlockEnabled = useFeatureFlag("cerebro_inbox_slack_block");
  const isMobile = useIsMobile();

  const markRead = useMarkInboxRead();
  const archiveInbox = useArchiveInbox();
  const archiveChannel = useArchiveChannel();
  const qc = useQueryClient();

  const [activeTabId, setActiveTabId] = useState<string>(
    layout.activeTabId ?? layout.tabs[0]?.id ?? "",
  );
  const activeTab =
    layout.tabs.find((t) => t.id === activeTabId) ??
    layout.tabs[0] ?? { id: "", title: "Inbox", sections: [] };

  // TECH-3502 #2 — inline rename of the active tab (also reachable from ⋯).
  const [editingTabId, setEditingTabId] = useState<string | null>(null);
  // TECH-3502 #5 — id of the box just added, so we can flash it; cleared on a
  // timer. The list scrolls to the top (where new boxes land) on insert.
  const [justAddedId, setJustAddedId] = useState<string | null>(null);
  const sectionsScrollRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!justAddedId) return;
    const t = setTimeout(() => setJustAddedId(null), 1800);
    return () => clearTimeout(t);
  }, [justAddedId]);

  // TECH-3413 #9 — free-text search across all sections in the active tab.
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<DynInboxEntry | null>(null);
  const [showNewMessage, setShowNewMessage] = useState(false);
  const [showReminder, setShowReminder] = useState(false);
  const [savePresetOpen, setSavePresetOpen] = useState(false);
  const [presetName, setPresetName] = useState("");
  // TECH-3413 #6 — frozen copy of the selected entry, captured at selection
  // time. While a row is open we render it from this snapshot so marking it
  // read doesn't move it out of its group/section until you pick another row.
  const [selectedSnapshot, setSelectedSnapshot] = useState<DynInboxEntry | null>(null);
  const selectedKey = selected
    ? selected.kind === "notif"
      ? selected.item.issue_id ?? selected.item.id
      : selected.id
    : null;

  const clearSelection = useCallback(() => {
    setSelected(null);
    setSelectedSnapshot(null);
  }, []);

  const onSelect = (entry: DynInboxEntry) => {
    setSelected(entry);
    setSelectedSnapshot(entry);
    // TECH-3413 #7 — selecting never auto-advances to the next row; we only
    // ever set the row the user clicked.
    if (entry.kind === "notif" && !entry.item.read) markRead.mutate(entry.item.id);
  };
  const onArchive = (entry: DynInboxEntry) => {
    if (entry.kind === "notif") archiveInbox.mutate(entry.item.id);
    else if (entry.kind === "channel") archiveChannel.mutate(entry.channel.id);
    else if (entry.kind === "chat") {
      // Mirror the classic inbox: archive the session, then refresh the list.
      void api
        .updateChatSession(entry.session.id, { status: "archived" })
        .then(() => qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) }));
    }
    if (selected && selected.id === entry.id) clearSelection();
  };

  // TECH-3413 #6 — substitute the frozen snapshot for its live row so the
  // section filter/sort sees the entry's selection-time state, not the
  // just-marked-read state. Matched on stable identity; falls through cleanly
  // if the row was archived/removed.
  const displayEntries = useMemo(() => {
    if (!selectedSnapshot) return entries;
    const target = entryIdentity(selectedSnapshot);
    let swapped = false;
    const next = entries.map((e) => {
      if (!swapped && entryIdentity(e) === target) {
        swapped = true;
        return selectedSnapshot;
      }
      return e;
    });
    return swapped ? next : entries;
  }, [entries, selectedSnapshot]);

  // TECH-3413 #1 — drag-to-reorder sections (5px threshold so clicks inside a
  // section still register).
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  const handleSectionDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    updateActiveTab((t) => {
      const oldIndex = t.sections.findIndex((s) => s.id === active.id);
      const newIndex = t.sections.findIndex((s) => s.id === over.id);
      if (oldIndex < 0 || newIndex < 0) return t;
      return { ...t, sections: arrayMove(t.sections, oldIndex, newIndex) };
    });
  };
  // TECH-3422 — the Slack-block opens a channel/DM into the same detail panel.
  const onOpenChannel = (channel: Channel) => {
    setSelected({
      kind: "channel",
      id: channel.id,
      time: Date.parse(channel.updated_at) || 0,
      channel,
    });
  };
  const selectedChannelId = selected?.kind === "channel" ? selected.channel.id : null;

  // ---- layout edits ----
  const updateActiveTab = (fn: (t: InboxTabConfig) => InboxTabConfig) =>
    setLayout(replaceTab(layout, activeTab.id, fn));

  const addSection = (kind: SectionKind) => {
    const id = makeId(kind);
    const section: InboxSectionConfig = {
      id,
      kind,
      groupBy: kind === "act_now" ? "action" : "none",
      sort: "newest",
      ...(kind === "project" ? { projectId: projects[0]?.id } : {}),
    };
    // TECH-3502 #5 — new boxes are inserted at the TOP so they're immediately
    // visible; flash a ring around the new box and scroll the list up so the
    // insert reads clearly.
    updateActiveTab((t) => ({ ...t, sections: [section, ...t.sections] }));
    setJustAddedId(id);
    requestAnimationFrame(() =>
      sectionsScrollRef.current?.scrollTo({ top: 0, behavior: "smooth" }),
    );
  };
  const removeSection = (id: string) =>
    updateActiveTab((t) => ({ ...t, sections: t.sections.filter((s) => s.id !== id) }));
  const changeSection = (next: InboxSectionConfig) =>
    updateActiveTab((t) => ({ ...t, sections: t.sections.map((s) => (s.id === next.id ? next : s)) }));
  const moveSection = (id: string, dir: -1 | 1) =>
    updateActiveTab((t) => {
      const idx = t.sections.findIndex((s) => s.id === id);
      const swap = idx + dir;
      if (idx < 0 || swap < 0 || swap >= t.sections.length) return t;
      const next = [...t.sections];
      const a = next[idx];
      const b = next[swap];
      if (!a || !b) return t;
      next[idx] = b;
      next[swap] = a;
      return { ...t, sections: next };
    });

  const addTab = () => {
    const id = makeId("tab");
    setLayout({
      ...layout,
      tabs: [...layout.tabs, { id, title: "New tab", sections: [{ id: makeId("all"), kind: "all" }] }],
    });
    setActiveTabId(id);
    // TECH-3502 #2 — a fresh tab opens straight into rename so it gets a name.
    setEditingTabId(id);
  };
  // TECH-3502 #2/#4 — rename a tab; empty input keeps the previous title.
  const renameTab = (id: string, title: string) => {
    const trimmed = title.trim();
    setLayout({
      ...layout,
      tabs: layout.tabs.map((t) => (t.id === id ? { ...t, title: trimmed || t.title } : t)),
    });
  };
  const removeTab = (id: string) => {
    if (layout.tabs.length <= 1) return;
    const remaining = layout.tabs.filter((t) => t.id !== id);
    const nextActive = remaining[0]?.id ?? "";
    setLayout({ ...layout, tabs: remaining, activeTabId: nextActive });
    if (activeTabId === id) setActiveTabId(nextActive);
  };
  const applyUserPreset = (presetLayout: InboxLayout) => {
    setLayout(presetLayout);
    setActiveTabId(presetLayout.activeTabId ?? presetLayout.tabs[0]?.id ?? "");
  };
  const onSavePreset = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const cleanName = presetName.trim();
    if (!cleanName) return;
    saveUserPreset(cleanName);
    setPresetName("");
    setSavePresetOpen(false);
  };

  const detail = useMemo(() => {
    if (!selected) return null;
    if (selected.kind === "channel") {
      return (
        <ErrorBoundary resetKeys={[selected.channel.id]}>
          <ChannelDetail
            key={selected.channel.id}
            channelId={selected.channel.id}
            initialChannel={selected.channel}
            onArchive={clearSelection}
          />
        </ErrorBoundary>
      );
    }
    if (selected.kind === "notif" && selected.item.issue_id) {
      return (
        <ErrorBoundary resetKeys={[selected.item.issue_id]}>
          <IssueDetail
            key={selected.item.issue_id}
            issueId={selected.item.issue_id}
            seedFromIssueList={false}
            defaultSidebarOpen={false}
            layoutId="multica_inbox_dynamic_issue_detail_layout"
            linkSelfInBreadcrumb
            onDelete={clearSelection}
          />
        </ErrorBoundary>
      );
    }
    // Agent chat session — open the same conversation panel the classic inbox
    // uses, so chats read identically here.
    if (selected.kind === "chat") {
      return (
        <ErrorBoundary resetKeys={[selected.session.id]}>
          <InboxChatPanel key={selected.session.id} sessionId={selected.session.id} />
        </ErrorBoundary>
      );
    }
    return (
      <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
        <Inbox className="mb-3 h-10 w-10 text-muted-foreground/30" />
        <p className="text-sm">Select a message to read it here.</p>
      </div>
    );
  }, [selected, clearSelection]);

  const createMenuProps = {
    onNewMessage: () => setShowNewMessage(true),
    onNewIssue: () => openCreateIssueWithPreference(),
    onNewReminder: () => setShowReminder(true),
  };

  const listColumn = (
    <div className="relative flex h-full min-h-0 flex-col">
      {/* tabs */}
      <div className="flex items-center gap-1 border-b border-border bg-muted/30 px-2 pt-1">
          {layout.tabs.map((tab) =>
            editingTabId === tab.id ? (
              // TECH-3502 #2/#4 — inline rename of a tab.
              <input
                key={tab.id}
                autoFocus
                defaultValue={tab.title}
                onBlur={(e) => {
                  renameTab(tab.id, e.target.value);
                  setEditingTabId(null);
                }}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    renameTab(tab.id, e.currentTarget.value);
                    setEditingTabId(null);
                  } else if (e.key === "Escape") {
                    setEditingTabId(null);
                  }
                }}
                className="-mb-px w-28 border-b-2 border-primary bg-transparent px-2 pb-2 pt-2 text-sm font-semibold text-foreground outline-none"
                aria-label="Tab name"
              />
            ) : (
              <button
                key={tab.id}
                type="button"
                onClick={() => setActiveTabId(tab.id)}
                onDoubleClick={() => setEditingTabId(tab.id)}
                title="Double-click to rename"
                className={`-mb-px border-b-2 px-3 pb-2 pt-2 text-sm font-semibold ${
                  tab.id === activeTab.id
                    ? "border-primary text-foreground"
                    : "border-transparent text-muted-foreground hover:text-foreground"
                }`}
              >
                {tab.title}
              </button>
            ),
          )}
          {/* create-menu (main) + ⋯ settings menu — tab add/rename/remove now
              lives in ⋯ (TECH-3502 #2) */}
          <div className="ml-auto flex items-center gap-1 pb-1">
            <DynamicInboxCreateMenu {...createMenuProps} />
            <Dialog open={savePresetOpen} onOpenChange={setSavePresetOpen}>
              <DialogContent>
                <form onSubmit={onSavePreset} className="space-y-4">
                  <DialogHeader>
                    <DialogTitle>Save inbox preset</DialogTitle>
                    <DialogDescription>
                      Save the current tabs and sections as a shortcut in this menu.
                    </DialogDescription>
                  </DialogHeader>
                  <Input
                    value={presetName}
                    onChange={(e) => setPresetName(e.target.value)}
                    placeholder="Preset name"
                    autoFocus
                  />
                  <DialogFooter>
                    <Button type="button" variant="outline" onClick={() => setSavePresetOpen(false)}>
                      Cancel
                    </Button>
                    <Button type="submit" disabled={!presetName.trim()}>
                      <Save className="size-4" /> Save preset
                    </Button>
                  </DialogFooter>
                </form>
              </DialogContent>
            </Dialog>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<button type="button" className="rounded p-1.5 text-muted-foreground hover:bg-muted" title="Inbox menu" />}
              >
                <MoreHorizontal className="size-4" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
                <DropdownMenuGroup>
                  <DropdownMenuLabel>Tabs</DropdownMenuLabel>
                  <DropdownMenuItem onClick={addTab}>
                    <Plus className="mr-2 size-4" /> Add tab
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setEditingTabId(activeTab.id)}>
                    <Pencil className="mr-2 size-4" /> Rename current tab
                  </DropdownMenuItem>
                  {layout.tabs.length > 1 && (
                    <DropdownMenuItem onClick={() => removeTab(activeTab.id)}>
                      <Trash2 className="mr-2 size-4" /> Remove current tab
                    </DropdownMenuItem>
                  )}
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  <DropdownMenuLabel>Add section</DropdownMenuLabel>
                  {SECTION_CATALOG.filter(
                    (c) => c.kind !== "team" || slackBlockEnabled,
                  ).map((c) => (
                    <DropdownMenuItem key={c.kind} onClick={() => addSection(c.kind)}>
                      {c.label}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  <DropdownMenuLabel>Start from preset</DropdownMenuLabel>
                  <DropdownMenuItem onClick={() => setSavePresetOpen(true)}>
                    <Save className="mr-2 size-4" /> Save current as preset...
                  </DropdownMenuItem>
                  {userPresets.length > 0 && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuLabel>My presets</DropdownMenuLabel>
                      {userPresets.map((preset) => (
                        <DropdownMenuItem
                          key={preset.id}
                          className="pr-1"
                          onClick={() => applyUserPreset(preset.layout)}
                        >
                          <span className="min-w-0 flex-1 truncate">{preset.label}</span>
                          <button
                            type="button"
                            className="ml-2 rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                            title={`Delete ${preset.label}`}
                            aria-label={`Delete ${preset.label}`}
                            onClick={(e) => {
                              e.preventDefault();
                              e.stopPropagation();
                              deleteUserPreset(preset.id);
                            }}
                          >
                            <Trash2 className="size-3.5" />
                          </button>
                        </DropdownMenuItem>
                      ))}
                    </>
                  )}
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => void setMode("classic")}>
                  <LayoutList className="mr-2 size-4" /> Switch to classic inbox
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>

        {/* search (TECH-3413 #9) */}
        <div className="border-b border-border px-3 py-2">
          <div className="flex items-center gap-2 rounded-lg border border-border bg-muted/40 px-2.5 py-1.5">
            <Search className="size-3.5 flex-none text-muted-foreground" />
            <input
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search inbox…"
              className="w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
            />
            {query && (
              <button
                type="button"
                className="flex-none rounded p-0.5 text-muted-foreground hover:bg-muted"
                onClick={() => setQuery("")}
                title="Clear search"
              >
                <X className="size-3.5" />
              </button>
            )}
          </div>
        </div>

        {/* sections */}
        <div ref={sectionsScrollRef} className="flex-1 space-y-3 overflow-y-auto p-3">
          {loading && <p className="px-1 text-sm text-muted-foreground">Loading…</p>}
          {activeTab.sections.length === 0 && !loading && (
            <p className="px-1 text-sm text-muted-foreground">
              Empty tab — add a section from the ⋯ menu.
            </p>
          )}
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleSectionDragEnd}>
            <SortableContext
              items={activeTab.sections.map((s) => s.id)}
              strategy={verticalListSortingStrategy}
            >
              <div className="space-y-3">
                {activeTab.sections.map((section, i) => (
                  <SortableSection key={section.id} id={section.id} highlight={section.id === justAddedId}>
                    {(handle) =>
                      section.kind === "team" ? (
                        <div className="relative">
                          <div className="absolute left-2 top-2.5 z-10">{handle}</div>
                          <SlackBlock
                            wsId={wsId}
                            selectedChannelId={selectedChannelId}
                            onOpenChannel={onOpenChannel}
                            maxPeople={section.maxPeople}
                            onSetMaxPeople={(n) => changeSection({ ...section, maxPeople: n })}
                            sort={section.teamSort}
                            onSetSort={(s) => changeSection({ ...section, teamSort: s })}
                            onRemove={() => removeSection(section.id)}
                            onMove={(dir) => moveSection(section.id, dir)}
                            isFirst={i === 0}
                            isLast={i === activeTab.sections.length - 1}
                          />
                        </div>
                      ) : (
                        <DynamicInboxSection
                          section={section}
                          entries={displayEntries}
                          filterContext={filterContext}
                          actionLabels={actionLabels}
                          projects={projects}
                          selectedKey={selectedKey}
                          query={query}
                          dragHandle={handle}
                          onSelect={onSelect}
                          onArchive={onArchive}
                          onChange={changeSection}
                          onRemove={() => removeSection(section.id)}
                          onMove={(dir) => moveSection(section.id, dir)}
                          isFirst={i === 0}
                          isLast={i === activeTab.sections.length - 1}
                        />
                      )
                    }
                  </SortableSection>
                ))}
              </div>
            </SortableContext>
          </DndContext>
        </div>
        <div className="pointer-events-none absolute bottom-4 right-4 z-30 md:hidden">
          <div className="pointer-events-auto">
            <DynamicInboxCreateMenu {...createMenuProps} variant="floating" />
          </div>
        </div>
    </div>
  );

  // TECH-3413 (Jesper feedback): mobile/PWA has no room for the side-by-side
  // split. It renders a single column — the section list, or (when a row is
  // open) that message full-screen with a Back button — mirroring how the
  // classic inbox behaves on a phone.
  if (isMobile) {
    if (selected) {
      return (
        <div className="flex h-full flex-col min-h-0">
          <div className="flex h-12 shrink-0 items-center gap-1 border-b border-border px-2">
            <button
              type="button"
              onClick={clearSelection}
              className="flex shrink-0 items-center gap-1.5 rounded px-2 py-1 text-sm text-muted-foreground hover:bg-muted"
            >
              <ArrowLeft className="size-4" /> Back
            </button>
          </div>
          <div className="flex flex-1 flex-col min-h-0">{detail}</div>
        </div>
      );
    }
    return (
      <div className="flex h-full flex-col min-h-0">
        {listColumn}
        <NewMessageModal
          open={showNewMessage}
          onClose={() => setShowNewMessage(false)}
          onCreated={onOpenChannel}
        />
        <GlobalInboxReminderDialog open={showReminder} onOpenChange={setShowReminder} />
      </div>
    );
  }

  return (
    <>
      <ResizablePanelGroup orientation="horizontal">
        <ResizablePanel id="list" defaultSize={420} minSize={300} className="flex flex-col">
          {listColumn}
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel id="detail" minSize="40%" className="flex flex-col">
          {detail ?? (
            // Empty detail placeholder — mirrors the classic inbox (Inbox icon +
            // hint) so the right panel never reads as a blank/broken pane.
            <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
              <Inbox className="mb-3 h-10 w-10 text-muted-foreground/30" />
              <p className="text-sm">Select a message to read it here.</p>
            </div>
          )}
        </ResizablePanel>
      </ResizablePanelGroup>
      <NewMessageModal
        open={showNewMessage}
        onClose={() => setShowNewMessage(false)}
        onCreated={onOpenChannel}
      />
      <GlobalInboxReminderDialog open={showReminder} onOpenChange={setShowReminder} />
    </>
  );
}
