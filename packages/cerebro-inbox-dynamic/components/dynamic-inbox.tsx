// TECH-3413 — the Dynamic inbox: one outer box holding tabs + stackable,
// configurable sections over the same data as the classic inbox. Light mode and
// rows match the classic inbox (reused row components). Switching back to the
// classic inbox lives in the ⋯ menu.
"use client";

import { useMemo, useState } from "react";
import { Plus, MoreHorizontal, LayoutList, Search, X, Inbox } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
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
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { useQueryClient } from "@tanstack/react-query";
import { useMarkInboxRead, useArchiveInbox } from "@multica/core/inbox/mutations";
import { useArchiveChannel } from "@multica/cerebro-channels";
import { useInboxActionGroupLabels, useSetInboxMode } from "@multica/cerebro-inbox";
import { api } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import { IssueDetail } from "@multica/views/issues/components";
import { ChannelDetail } from "@multica/views/channels";
import { InboxChatPanel } from "@multica/views/inbox/components/inbox-list-item";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { SlackBlock } from "@multica/cerebro-inbox-slack-block";
import type { Channel } from "@multica/core/types";
import { useDynamicInboxData } from "../use-dynamic-inbox-data";
import { useInboxLayout } from "../use-inbox-layout";
import {
  SECTION_CATALOG,
  INBOX_PRESETS,
  makeId,
  type InboxLayout,
  type InboxSectionConfig,
  type InboxTabConfig,
  type SectionKind,
} from "../layout";
import type { DynInboxEntry } from "../section-filter";
import { DynamicInboxSection } from "./dynamic-inbox-section";

function replaceTab(layout: InboxLayout, tabId: string, fn: (t: InboxTabConfig) => InboxTabConfig): InboxLayout {
  return { ...layout, tabs: layout.tabs.map((t) => (t.id === tabId ? fn(t) : t)) };
}

export function DynamicInbox() {
  const wsId = useWorkspaceId();
  const { layout, setLayout } = useInboxLayout();
  const { entries, filterContext, projects, loading } = useDynamicInboxData(wsId);
  const actionLabels = useInboxActionGroupLabels();
  const setMode = useSetInboxMode();
  const slackBlockEnabled = useFeatureFlag("cerebro_inbox_slack_block");

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

  // TECH-3413 #9 — free-text search across all sections in the active tab.
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<DynInboxEntry | null>(null);
  const selectedKey = selected
    ? selected.kind === "notif"
      ? selected.item.issue_id ?? selected.item.id
      : selected.id
    : null;

  const onSelect = (entry: DynInboxEntry) => {
    setSelected(entry);
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
    if (selected && selected.id === entry.id) setSelected(null);
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
    const section: InboxSectionConfig = {
      id: makeId(kind),
      kind,
      groupBy: kind === "act_now" ? "action" : "none",
      sort: "newest",
      ...(kind === "project" ? { projectId: projects[0]?.id } : {}),
    };
    updateActiveTab((t) => ({ ...t, sections: [...t.sections, section] }));
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
  };
  const applyPreset = (build: () => InboxLayout) => {
    const next = build();
    setLayout(next);
    setActiveTabId(next.activeTabId ?? next.tabs[0]?.id ?? "");
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
            onArchive={() => setSelected(null)}
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
            onDelete={() => setSelected(null)}
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
  }, [selected]);

  return (
    <ResizablePanelGroup orientation="horizontal">
      <ResizablePanel id="list" defaultSize={420} minSize={300} className="flex flex-col">
        {/* tabs */}
        <div className="flex items-center gap-1 border-b border-border bg-muted/30 px-2 pt-1">
          {layout.tabs.map((tab) => (
            <button
              key={tab.id}
              type="button"
              onClick={() => setActiveTabId(tab.id)}
              className={`-mb-px border-b-2 px-3 pb-2 pt-2 text-sm font-semibold ${
                tab.id === activeTab.id
                  ? "border-primary text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground"
              }`}
            >
              {tab.title}
            </button>
          ))}
          <button
            type="button"
            onClick={addTab}
            className="px-2 pb-2 pt-2 text-muted-foreground hover:text-foreground"
            title="Add tab"
          >
            <Plus className="size-4" />
          </button>
          {/* ⋯ menu */}
          <div className="ml-auto pb-1">
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<button type="button" className="rounded p-1.5 text-muted-foreground hover:bg-muted" title="Inbox menu" />}
              >
                <MoreHorizontal className="size-4" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
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
                  {INBOX_PRESETS.map((p) => (
                    <DropdownMenuItem key={p.key} onClick={() => applyPreset(p.build)}>
                      {p.label}
                    </DropdownMenuItem>
                  ))}
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
        <div className="flex-1 space-y-3 overflow-y-auto p-3">
          {loading && <p className="px-1 text-sm text-muted-foreground">Loading…</p>}
          {activeTab.sections.length === 0 && !loading && (
            <p className="px-1 text-sm text-muted-foreground">
              Empty tab — add a section from the ⋯ menu.
            </p>
          )}
          {activeTab.sections.map((section, i) =>
            section.kind === "team" ? (
              <SlackBlock
                key={section.id}
                wsId={wsId}
                selectedChannelId={selectedChannelId}
                onOpenChannel={onOpenChannel}
                maxPeople={section.maxPeople}
                onSetMaxPeople={(n) => changeSection({ ...section, maxPeople: n })}
                onRemove={() => removeSection(section.id)}
                onMove={(dir) => moveSection(section.id, dir)}
                isFirst={i === 0}
                isLast={i === activeTab.sections.length - 1}
              />
            ) : (
            <DynamicInboxSection
              key={section.id}
              section={section}
              entries={entries}
              filterContext={filterContext}
              actionLabels={actionLabels}
              projects={projects}
              selectedKey={selectedKey}
              query={query}
              onSelect={onSelect}
              onArchive={onArchive}
              onChange={changeSection}
              onRemove={() => removeSection(section.id)}
              onMove={(dir) => moveSection(section.id, dir)}
              isFirst={i === 0}
              isLast={i === activeTab.sections.length - 1}
            />
          ))}
        </div>
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
  );
}
