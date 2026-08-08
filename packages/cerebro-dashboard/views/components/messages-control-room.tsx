"use client";

import { useEffect, useMemo, useState, type Dispatch, type SetStateAction } from "react";
import { useQuery } from "@tanstack/react-query";
import type { AnalyticsDimension, AnalyticsOperator } from "@multica/cerebro-usage";
import { useAuthStore } from "@multica/core/auth";
import type { ActorMessage, DashboardOverview, TopActor } from "../../core/api";
import {
  removeAnalyticsFilterValue,
  addAnalyticsFilterValue,
  type AnalyticsFilter,
} from "../../core/analytics";
import { allMessagesOptions } from "../../core/queries";
import { useDashboardStore } from "../../core/store";
import { AnalyticsDashboard } from "./analytics-dashboard";
import {
  ControlRoomEmpty,
  ControlRoomKpi,
  ControlRoomLoading,
  ControlRoomPanel,
  MetricBar,
  formatCompact,
  formatDollars,
} from "./control-room-primitives";
import { MessageDetailSheet } from "./message-detail-sheet";
import { RunsToolbar } from "./runs-toolbar";

export function MessagesControlRoom({
  workspaceId,
  workspaceSlug,
  data,
  isLoading,
  filters,
  onFiltersChange,
  onNewVisual,
  onSelectActor,
  builderOpen,
  onBuilderOpenChange,
}: {
  workspaceId: string;
  workspaceSlug: string;
  data: DashboardOverview | undefined;
  isLoading: boolean;
  filters: AnalyticsFilter[];
  onFiltersChange: Dispatch<SetStateAction<AnalyticsFilter[]>>;
  onNewVisual: () => void;
  onSelectActor: (actorId: string, actorName: string, actorType: "member" | "agent") => void;
  builderOpen?: boolean;
  onBuilderOpenChange?: (open: boolean) => void;
}) {
  const range = useDashboardStore((state) => state.range);
  const setRange = useDashboardStore((state) => state.setRange);
  const clearFilters = useDashboardStore((state) => state.clearFilters);
  const scope = useDashboardStore((state) => state.scope);
  const actorId = useDashboardStore((state) => state.actorId);
  const exactTimeRange = useDashboardStore((state) => state.exactTimeRange);
  const currentUserId = useAuthStore((state) => state.user?.id ?? "");
  const messagesQuery = useQuery(allMessagesOptions(workspaceId, range, scope, actorId, exactTimeRange));
  const [compactLayout, setCompactLayout] = useState(false);
  const [search, setSearch] = useState("");
  const [messagePage, setMessagePage] = useState(0);
  const [selectedMessage, setSelectedMessage] = useState<ActorMessage | null>(null);
  const timeline = data?.timeline ?? [];
  const messageFlow = data?.message_flow ?? [];
  const topMessageSenders = data?.top_message_senders ?? [];
  const topMessageRecipients = data?.top_message_recipients ?? [];

  const messages = useMemo(() => {
    const normalized = search.trim().toLowerCase();
    const source = messagesQuery.data?.messages ?? [];
    if (!normalized) return source;
    return source.filter((message) =>
      [message.content, message.sender_name, message.agent_name, message.issue_title]
        .filter(Boolean)
        .some((value) => value!.toLowerCase().includes(normalized)),
    );
  }, [messagesQuery.data?.messages, search]);
  const messagePageSize = 10;
  const messagePageCount = Math.max(1, Math.ceil(messages.length / messagePageSize));
  const visibleMessages = messages.slice(messagePage * messagePageSize, (messagePage + 1) * messagePageSize);

  useEffect(() => setMessagePage(0), [search, actorId, range, scope]);
  useEffect(() => {
    if (messagePage >= messagePageCount) setMessagePage(messagePageCount - 1);
  }, [messagePage, messagePageCount]);

  const addFilter = (dimension: AnalyticsDimension, value: string, operator: "in" | "not_in" | "contains" | "not_contains") => {
    onFiltersChange((current) => addAnalyticsFilterValue(current, dimension, value, operator));
  };
  const removeFilter = (dimension: AnalyticsDimension, value: string, operator: AnalyticsOperator) => {
    onFiltersChange((current) => removeAnalyticsFilterValue(current, dimension, value, operator));
  };
  const selectActor = (id: string, name: string, actorType: "member" | "agent" = "member") => {
    onSelectActor(id, name, actorType);
  };
  const filterDay = (day: string) => {
    const start = new Date(`${day}T00:00:00.000Z`);
    const end = new Date(start);
    end.setUTCDate(end.getUTCDate() + 1);
    onFiltersChange((current) => [
      ...current.filter((filter) => filter.dimension !== "time"),
      { dimension: "time", operator: "gte", values: [start.toISOString()] },
      { dimension: "time", operator: "lte", values: [end.toISOString()] },
    ]);
  };

  const maxMessages = Math.max(1, ...timeline.map((bucket) => bucket.messages_sent ?? 0));

  return (
    <div className={`min-w-0 p-6 ${compactLayout ? "space-y-2" : "space-y-3"}`}>
      <RunsToolbar
        workspaceId={workspaceId}
        range={range}
        onRangeChange={setRange}
        filters={filters}
        onAddFilter={addFilter}
        onRemoveFilter={removeFilter}
        onClear={() => {
          clearFilters();
          setMessagePage(0);
        }}
        onCustomize={() => setCompactLayout((current) => !current)}
        onNewVisual={onNewVisual}
      />

      <section aria-label="Message operations" className="grid grid-cols-2 divide-x divide-y overflow-hidden rounded-lg border bg-card sm:grid-cols-4 sm:divide-y-0">
        <ControlRoomKpi label="Direct messages" value={formatCompact(data?.chat_messages.value ?? 0)} note="member and agent conversations" />
        <ControlRoomKpi label="Channel messages" value={formatCompact(data?.channel_messages.value ?? 0)} note="workspace collaboration" />
        <ControlRoomKpi label="Message cost" value={formatDollars(data?.spend_cents.value ?? 0)} note="exact charges plus token estimates" />
        <ControlRoomKpi label="Visible messages" value={formatCompact(messages.length)} note="matching current selection" />
      </section>

      <div className="grid gap-3 xl:grid-cols-[minmax(0,2fr)_minmax(300px,1fr)]">
        <ControlRoomPanel title="Message activity" meta="Messages by day · click a day to filter every panel">
          {isLoading ? <ControlRoomLoading /> : timeline.length === 0 ? <ControlRoomEmpty>No message activity in the selected range.</ControlRoomEmpty> : (
            <div className="grid min-h-48 grid-flow-col auto-cols-fr items-end gap-1 p-4" role="list" aria-label="Daily message activity">
              {timeline.map((bucket) => {
                const count = bucket.messages_sent ?? 0;
                const height = Math.max(6, Math.round((count / maxMessages) * 100));
                return (
                  <button key={bucket.day} type="button" onClick={() => filterDay(bucket.day)} className="group flex h-full min-w-0 flex-col justify-end gap-2 rounded px-1 hover:bg-muted/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={`Filter Dashboard by ${bucket.day}: ${count} messages`}>
                    <span className="w-full rounded-t bg-primary/75 transition-colors group-hover:bg-primary" style={{ height: `${height}%` }} />
                    <span className="truncate text-[9px] text-muted-foreground">{bucket.day.slice(5)}</span>
                  </button>
                );
              })}
            </div>
          )}
        </ControlRoomPanel>

        <ControlRoomPanel title="Conversation flow" meta="Sender to recipient · click a row to filter the complete Dashboard">
          {isLoading ? <ControlRoomLoading /> : messageFlow.length === 0 ? <ControlRoomEmpty>No message flows in the selected range.</ControlRoomEmpty> : (
            <div className="space-y-1 p-3">
              {messageFlow.map((flow) => (
                <button key={`${flow.sender_id}:${flow.recipient_id}`} type="button" onClick={() => selectActor(flow.sender_id, flow.sender_name)} className="grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-3 rounded-md px-2 py-2 text-left hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={`Filter Dashboard by conversation from ${flow.sender_name} to ${flow.recipient_name}`}>
                  <span className="truncate text-xs"><strong>{flow.sender_name}</strong><span className="text-muted-foreground"> → {flow.recipient_name}</span></span>
                  <span className="font-mono text-xs tabular-nums">{formatCompact(flow.count)}</span>
                </button>
              ))}
            </div>
          )}
        </ControlRoomPanel>
      </div>

      <div className="grid gap-3 lg:grid-cols-2">
        <ControlRoomPanel title="Top senders" meta="Click a sender to filter every Messages panel">
          <ActorRows prefix="sender" rows={topMessageSenders} loading={isLoading} actorType="member" onSelect={selectActor} />
        </ControlRoomPanel>
        <ControlRoomPanel title="Top recipients" meta="Click a recipient to filter every Messages panel">
          <ActorRows prefix="recipient" rows={topMessageRecipients} loading={isLoading} actorType="agent" onSelect={selectActor} />
        </ControlRoomPanel>
      </div>

      <ControlRoomPanel title="All messages" meta="Search, inspect a conversation, or open the full thread">
        <div className="border-b p-3">
          <input value={search} onChange={(event) => setSearch(event.target.value)} aria-label="Search all messages" placeholder="Search message, sender, agent, or issue" className="h-9 w-full rounded-md border bg-background px-3 text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" />
        </div>
        {messagesQuery.isLoading ? <ControlRoomLoading rows={5} /> : messages.length === 0 ? <ControlRoomEmpty>No messages match the current selection.</ControlRoomEmpty> : (
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead><tr className="border-b text-left text-[10px] uppercase tracking-wide text-muted-foreground"><th className="px-3 py-2 font-medium">Time</th><th className="px-3 py-2 font-medium">From</th><th className="px-3 py-2 font-medium">To</th><th className="px-3 py-2 font-medium">Issue</th><th className="px-3 py-2 font-medium">Message</th></tr></thead>
              <tbody>
                {visibleMessages.map((message) => (
                  <tr key={message.id} className="border-b transition-colors last:border-0 hover:bg-muted/60">
                    <td className="whitespace-nowrap px-3 py-2 font-mono text-[10px] text-muted-foreground">{formatTime(message.created_at)}</td>
                    <td className="px-3 py-2"><button type="button" onClick={(event) => { event.stopPropagation(); if (message.sender_id && message.sender_name) selectActor(message.sender_id, message.sender_name); }} className="font-medium hover:underline">{message.sender_name ?? "Unknown"}</button></td>
                    <td className="whitespace-nowrap px-3 py-2 text-muted-foreground">{message.agent_name}</td>
                    <td className="max-w-44 truncate px-3 py-2 text-muted-foreground">
                      {message.issue_title ? (
                        <button type="button" aria-label={`Filter messages by issue ${message.issue_title}`} onClick={(event) => { event.stopPropagation(); setSearch(message.issue_title!); setMessagePage(0); }} className="max-w-full truncate hover:underline">
                          {`#${message.issue_number} ${message.issue_title}`}
                        </button>
                      ) : (
                        "—"
                      )}
                    </td>
                    <td className="max-w-md px-3 py-2">
                      <button type="button" aria-label={`Open conversation with ${message.agent_name}`} onClick={() => setSelectedMessage(message)} className="w-full rounded-sm text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                        <span className="line-clamp-2">{message.content}</span>
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {messages.length > 0 && (
          <footer className="flex items-center justify-between border-t px-3 py-2">
            <span className="text-[11px] text-muted-foreground">Page {messagePage + 1} of {messagePageCount}</span>
            <div className="flex gap-1">
              <button type="button" aria-label="Previous messages page" disabled={messagePage === 0} onClick={() => setMessagePage((page) => Math.max(0, page - 1))} className="rounded border px-2 py-1 text-xs disabled:opacity-40">Previous</button>
              <button type="button" aria-label="Next messages page" disabled={messagePage + 1 >= messagePageCount} onClick={() => setMessagePage((page) => Math.min(messagePageCount - 1, page + 1))} className="rounded border px-2 py-1 text-xs disabled:opacity-40">Next</button>
            </div>
          </footer>
        )}
      </ControlRoomPanel>

      <AnalyticsDashboard workspaceId={workspaceId} initialVisuals={[]} filters={filters} onFiltersChange={onFiltersChange} showToolbar={false} builderOpen={builderOpen} onBuilderOpenChange={onBuilderOpenChange} />
      <MessageDetailSheet message={selectedMessage} wsId={workspaceId} workspaceSlug={workspaceSlug} currentUserId={currentUserId} onClose={() => setSelectedMessage(null)} />
    </div>
  );
}

function ActorRows({ prefix, rows, loading, actorType, onSelect }: { prefix: "sender" | "recipient"; rows: TopActor[]; loading: boolean; actorType: "member" | "agent"; onSelect: (id: string, name: string, actorType: "member" | "agent") => void }) {
  if (loading) return <ControlRoomLoading />;
  if (rows.length === 0) return <ControlRoomEmpty>No people activity in the selected range.</ControlRoomEmpty>;
  const maximum = Math.max(...rows.map((row) => row.count), 1);
  return (
    <div className="space-y-1 p-3">
      {rows.map((row) => (
        <button key={row.id} type="button" onClick={() => onSelect(row.id, row.name, actorType)} className="grid w-full grid-cols-[28px_minmax(0,1fr)_auto] items-center gap-3 rounded-md px-2 py-2 text-left hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={`Filter Dashboard by ${prefix} ${row.name}`}>
          <span className="flex size-7 items-center justify-center rounded-full bg-primary/10 text-[10px] font-semibold text-primary">{row.name.slice(0, 1).toUpperCase()}</span>
          <span className="min-w-0"><span className="mb-1 block truncate text-xs font-medium">{row.name}</span><MetricBar value={row.count} maximum={maximum} /></span>
          <span className="font-mono text-xs tabular-nums">{formatCompact(row.count)}</span>
        </button>
      ))}
    </div>
  );
}

function formatTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString("en-GB", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });
}
