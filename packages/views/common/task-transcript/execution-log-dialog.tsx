"use client";

// Paginated + virtualized Execution Log dialog for TERMINAL (past-run) tasks
// (MUL-5122). A long past run can carry tens of thousands of events; the legacy
// AgentTranscriptDialog fetches and renders the whole array at once, which froze
// the browser. This dialog fetches bounded pages through
// `executionLogPageOptions` and renders one virtualized Virtuoso row per loaded
// message. Scope is strictly the terminal path — the live/chat streaming path
// still uses the shared task-messages cache and AgentTranscriptDialog.
//
// Rows follow the reading hierarchy from `trace-event-presenter`: Agent text and
// errors are the primary reading layer (multi-line, readable body), tool calls /
// results / thinking are the secondary layer (compact one line + expandable
// detail), and #seq / time are tertiary chrome.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { Virtuoso, type Components } from "react-virtuoso";
import {
  AlertCircle,
  ArrowDownNarrowWide,
  ArrowUpNarrowWide,
  Brain,
  Check,
  ChevronRight,
  Copy,
  Loader2,
  X,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  executionLogPageOptions,
  flattenExecutionLogPages,
  type ExecutionLogFilters,
} from "@multica/core/chat/queries";
import {
  useTranscriptViewStore,
  type TranscriptSortDirection,
} from "@multica/core/agents/stores";
import type { AgentTask } from "@multica/core/types/agent";
import type { TaskMessagePayload } from "@multica/core/types/events";
import { redactSecrets } from "./redact";
import {
  TRACE_RESULT_PREVIEW_LINES,
  TRACE_TEXT_PREVIEW_LINES,
  decodeToolResultOutput,
  traceEventHasDetail,
  traceEventKind,
  traceEventLabel,
  traceEventSummary,
  traceToolArgSummary,
} from "./trace-event-presenter";
import { useT } from "../../i18n";

const TOOL_KEY_PREFIX = "tool:";
const OUTPUT_DETAIL_CAP = 4000;
// Errors are primary but usually shorter than an Agent conclusion; six lines
// keep a stack-trace head readable without dominating the list.
const TRACE_ERROR_PREVIEW_LINES = 6;
// Cap the tool-result preview string so a single 50k-char line never enters the
// DOM just to be visually clamped away.
const RESULT_PREVIEW_CHARS = 600;

/** Stable identity for a loaded event, used as the shared expand-set key. Seq is
 *  unique within a Run; the rest disambiguate defensively if seq ever repeats. */
function execEventKey(m: TaskMessagePayload): string {
  return `${m.seq}|${m.created_at ?? ""}|${m.type}|${m.tool ?? ""}`;
}

/** A human label for a type facet key ("text" → "Agent", etc.). */
function typeFacetLabel(key: string): string {
  return traceEventLabel({ type: key });
}

/** First `n` newline-delimited lines of a block, for a bounded inline preview. */
function firstLines(text: string, n: number): string {
  return text.split("\n").slice(0, n).join("\n");
}

/** `-webkit-line-clamp` as an inline style so the line count can vary per kind
 *  without depending on which `line-clamp-N` utilities Tailwind emits. */
function clampStyle(lines: number): React.CSSProperties {
  return {
    display: "-webkit-box",
    WebkitBoxOrient: "vertical",
    WebkitLineClamp: lines,
    overflow: "hidden",
  };
}

// ─── Virtuoso loading-edge slot (loading-older / older-page error) ───────────
//
// Module-scope + context-driven, matching chat-message-list: an inline
// components prop rebuilds the slot type every render and remounts its subtree,
// which is exactly the churn MUL-3960 fixed. Per-render data reaches the slot
// through Virtuoso's `context` prop instead. In chronological order older events
// load at the START (Header slot); in newest-first order they load at the END
// (Footer slot) — the content is identical, only the slot differs.

interface LogListContext {
  isFetchingEarlier: boolean;
  earlierError: boolean;
  onRetryEarlier: () => void;
}

function LogListEdge({ context }: { context?: LogListContext }) {
  const { t } = useT("issues");
  if (!context) return null;
  if (context.earlierError) {
    return (
      <div className="flex items-center justify-center gap-2 px-4 py-2 text-xs text-muted-foreground">
        <span>{t(($) => $.execution_log.earlier_error)}</span>
        <button
          type="button"
          onClick={context.onRetryEarlier}
          className="rounded px-1.5 py-0.5 text-foreground underline underline-offset-2 hover:bg-accent"
        >
          {t(($) => $.execution_log.retry)}
        </button>
      </div>
    );
  }
  if (context.isFetchingEarlier) {
    return (
      <div className="flex items-center justify-center gap-2 px-4 py-2 text-xs text-muted-foreground">
        <Loader2 className="h-3 w-3 animate-spin" />
        {t(($) => $.execution_log.loading_earlier)}
      </div>
    );
  }
  return null;
}

const HEADER_COMPONENTS: Components<TaskMessagePayload, LogListContext> = {
  Header: LogListEdge,
};
const FOOTER_COMPONENTS: Components<TaskMessagePayload, LogListContext> = {
  Footer: LogListEdge,
};

// ─── Main dialog ─────────────────────────────────────────────────────────────

interface ExecutionLogDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  task: AgentTask;
  agentName: string;
  /**
   * Optional content rendered between the header and the event list. Mirrors
   * AgentTranscriptDialog's slot so the terminal path keeps surfacing the
   * autopilot webhook payload preview for completed runs.
   */
  headerSlot?: React.ReactNode;
}

export function ExecutionLogDialog({
  open,
  onOpenChange,
  task,
  agentName,
  headerSlot,
}: ExecutionLogDialogProps) {
  const { t } = useT("issues");
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
  const [selectedKeys, setSelectedKeys] = useState<string[]>([]);
  const [copied, setCopied] = useState(false);
  // Shared expand set owned by the dialog so a bulk action can expand/collapse
  // every loaded row at once. Holds `execEventKey` values; a row is open iff its
  // key is present.
  const [expandedKeys, setExpandedKeys] = useState<Set<string>>(() => new Set());

  const sortDirection = useTranscriptViewStore((s) => s.sortDirection);
  const setSortDirection = useTranscriptViewStore((s) => s.setSortDirection);
  const chronological = sortDirection === "chronological";

  // Selected chip keys are in the presenter's traceEventFilterKey format
  // ("error" or "tool:Bash"); split them back into the API's type/tool arrays.
  const filters = useMemo<ExecutionLogFilters | undefined>(() => {
    if (selectedKeys.length === 0) return undefined;
    const types: string[] = [];
    const tools: string[] = [];
    for (const key of selectedKeys) {
      if (key.startsWith(TOOL_KEY_PREFIX)) tools.push(key.slice(TOOL_KEY_PREFIX.length));
      else types.push(key);
    }
    return {
      types: types.length > 0 ? types : undefined,
      tools: tools.length > 0 ? tools : undefined,
    };
  }, [selectedKeys]);

  const {
    data,
    isLoading,
    isError,
    refetch,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = useInfiniteQuery(executionLogPageOptions(task.id, filters, 50));

  const messages = useMemo(() => flattenExecutionLogPages(data?.pages), [data?.pages]);

  // Newest-first is a pure presentation reverse; seq numbers and detail are
  // untouched, so filters/copy keep working against the same data.
  const orderedMessages = useMemo(
    () => (chronological ? messages : [...messages].reverse()),
    [messages, chronological],
  );

  const pages = data?.pages ?? [];
  // Chronological: older pages prepend at the START, so firstItemIndex counts
  // down by their length to keep already-rendered rows anchored across a
  // prepend. Newest-first: older pages append at the END and never shift the
  // existing indices, so a fixed 0 anchor is correct.
  const olderCount = pages.slice(1).reduce((sum, page) => sum + page.messages.length, 0);
  const firstItemIndex = chronological
    ? messages.length > 0
      ? 1_000_000 - olderCount
      : 0
    : 0;

  // Totals and facets are full-Run context, identical on every page.
  const first = pages[0];
  const rawTotal = first?.raw_total ?? 0;
  const matchedTotal = first?.matched_total ?? 0;

  const filterActive = selectedKeys.length > 0;

  const chips = useMemo(
    () => [
      ...(first?.type_facets ?? []).map((f) => ({
        key: f.key,
        label: typeFacetLabel(f.key),
        count: f.count,
      })),
      ...(first?.tool_facets ?? []).map((f) => ({
        key: `${TOOL_KEY_PREFIX}${f.key}`,
        label: f.key,
        count: f.count,
      })),
    ],
    [first],
  );

  const toggleKey = useCallback((key: string) => {
    setSelectedKeys((prev) =>
      prev.includes(key) ? prev.filter((k) => k !== key) : [...prev, key],
    );
  }, []);

  // Bulk expand is bounded to LOADED rows with detail — never the unfetched
  // history (which could be tens of thousands of events).
  const expandableKeys = useMemo(
    () => messages.filter((m) => traceEventHasDetail(m)).map(execEventKey),
    [messages],
  );
  const allExpanded =
    expandableKeys.length > 0 && expandableKeys.every((k) => expandedKeys.has(k));

  const toggleExpanded = useCallback((key: string) => {
    setExpandedKeys((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

  const handleBulkExpand = useCallback(() => {
    setExpandedKeys(allExpanded ? new Set() : new Set(expandableKeys));
  }, [allExpanded, expandableKeys]);

  const handleRetryEarlier = useCallback(() => {
    void fetchNextPage();
  }, [fetchNextPage]);

  const handleEdgeReached = useCallback(() => {
    if (hasNextPage && !isFetchingNextPage) void fetchNextPage();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  // Copies only the currently loaded (and filtered) window — not the whole Run,
  // which may still have unfetched older pages. Uses the on-screen order.
  const handleCopyLoaded = useCallback(() => {
    const text = orderedMessages
      .map((m) => `[${traceEventLabel(m)}] ${traceEventSummary(m)}`)
      .join("\n");
    void copyText(text).then((ok) => {
      if (!ok) return;
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, [orderedMessages]);

  const listContext: LogListContext = {
    isFetchingEarlier: isFetchingNextPage,
    earlierError: isFetchNextPageError,
    onRetryEarlier: handleRetryEarlier,
  };

  let body: React.ReactNode;
  if (isLoading) {
    body = <ExecutionLogSkeleton />;
  } else if (isError) {
    body = (
      <div className="flex h-full flex-col items-center justify-center gap-3 px-4 text-center text-sm text-muted-foreground">
        <span>{t(($) => $.execution_log.load_error)}</span>
        <Button variant="outline" size="sm" onClick={() => void refetch()}>
          {t(($) => $.execution_log.retry)}
        </Button>
      </div>
    );
  } else if (rawTotal === 0) {
    body = (
      <div className="flex h-full items-center justify-center px-4 text-sm text-muted-foreground">
        {t(($) => $.execution_log.empty)}
      </div>
    );
  } else if (messages.length === 0) {
    body = (
      <div className="flex h-full items-center justify-center px-4 text-sm text-muted-foreground">
        {t(($) => $.execution_log.no_match)}
      </div>
    );
  } else if (scrollEl) {
    body = (
      <Virtuoso<TaskMessagePayload, LogListContext>
        // Remount when direction flips so the list lands at that direction's
        // start point (bottom for chronological, top for newest-first).
        key={sortDirection}
        customScrollParent={scrollEl}
        data={orderedMessages}
        firstItemIndex={firstItemIndex}
        initialTopMostItemIndex={chronological ? { index: "LAST", align: "end" } : 0}
        increaseViewportBy={{ top: 400, bottom: 600 }}
        startReached={chronological ? handleEdgeReached : undefined}
        endReached={chronological ? undefined : handleEdgeReached}
        context={listContext}
        components={chronological ? HEADER_COMPONENTS : FOOTER_COMPONENTS}
        itemContent={(_, message) => {
          const key = execEventKey(message);
          return (
            <ExecutionLogRow
              message={message}
              open={expandedKeys.has(key)}
              onToggle={() => toggleExpanded(key)}
            />
          );
        }}
      />
    );
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        data-testid="execution-log-dialog"
        className="!max-w-4xl !w-[calc(100vw-4rem)] !max-h-[calc(100vh-4rem)] !h-[calc(100vh-4rem)] flex flex-col !p-0 !gap-0 overflow-hidden"
        showCloseButton={false}
      >
        <DialogTitle className="sr-only">{t(($) => $.execution_log.dialog_title)}</DialogTitle>

        {/* ── Header ─────────────────────────────────────────────── */}
        <div className="border-b px-4 py-3 shrink-0 space-y-2">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <div className="flex min-w-0 items-center gap-2">
              <span className="font-medium text-sm">{t(($) => $.execution_log.dialog_title)}</span>
              <span className="truncate text-sm text-muted-foreground">{agentName}</span>
            </div>

            <div className="ml-auto flex shrink-0 items-center gap-1">
              {expandableKeys.length > 0 && (
                <button
                  type="button"
                  data-testid={
                    allExpanded ? "execution-log-collapse-all" : "execution-log-expand-all"
                  }
                  onClick={handleBulkExpand}
                  aria-label={
                    allExpanded
                      ? t(($) => $.execution_log.collapse_all)
                      : t(($) => $.execution_log.expand_loaded)
                  }
                  className="flex shrink-0 items-center gap-1 rounded px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                >
                  <ChevronRight
                    className={cn("h-3 w-3 transition-transform", !allExpanded && "rotate-90")}
                  />
                  <span className="hidden sm:inline">
                    {allExpanded
                      ? t(($) => $.execution_log.collapse_all)
                      : t(($) => $.execution_log.expand_loaded)}
                  </span>
                </button>
              )}

              {messages.length > 0 && (
                <SortDirectionToggle
                  value={sortDirection}
                  onChange={setSortDirection}
                  labels={{
                    chronological: t(($) => $.execution_log.sort_chronological),
                    newestFirst: t(($) => $.execution_log.sort_newest_first),
                    ariaLabel: t(($) => $.execution_log.sort_label),
                  }}
                />
              )}

              <button
                type="button"
                onClick={handleCopyLoaded}
                aria-label={t(($) => $.execution_log.copy_loaded)}
                className="flex shrink-0 items-center gap-1 rounded px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              >
                {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
                <span className="hidden sm:inline">
                  {copied ? t(($) => $.execution_log.copied) : t(($) => $.execution_log.copy_loaded)}
                </span>
              </button>
              <button
                type="button"
                onClick={() => onOpenChange(false)}
                aria-label={t(($) => $.execution_log.close)}
                className="flex shrink-0 items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
          </div>

          {/* Counts */}
          <div className="text-xs text-muted-foreground">
            <span data-testid="execution-log-total">{rawTotal}</span>{" "}
            {t(($) => $.execution_log.events_label)}
            {filterActive && (
              <>
                {" · "}
                {t(($) => $.execution_log.matched_count, { n: matchedTotal })}
              </>
            )}
            {" · "}
            {t(($) => $.execution_log.loaded_count, { n: messages.length })}
          </div>

          {/* Filter chips (OR semantics; a tool chip covers tool_use + tool_result) */}
          {chips.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {chips.map((chip) => {
                const active = selectedKeys.includes(chip.key);
                return (
                  <button
                    key={chip.key}
                    type="button"
                    onClick={() => toggleKey(chip.key)}
                    aria-pressed={active}
                    className={cn(
                      "inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] transition-colors",
                      active
                        ? "border-blue-500/40 bg-blue-500/10 text-blue-700 dark:text-blue-300"
                        : "text-muted-foreground hover:bg-accent hover:text-foreground",
                    )}
                  >
                    {chip.label}
                    <span className="tabular-nums text-muted-foreground/70">{chip.count}</span>
                  </button>
                );
              })}
            </div>
          )}
        </div>

        {/* ── Optional header slot (e.g. webhook payload preview) ── */}
        {headerSlot && (
          <div className="border-b px-4 py-3 shrink-0 bg-muted/20">{headerSlot}</div>
        )}

        {/* ── Event list ─────────────────────────────────────────── */}
        <div
          ref={setScrollEl}
          data-testid="execution-log-scroll"
          className="flex-1 overflow-y-auto min-h-0"
        >
          {body}
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ─── Sort direction toggle ───────────────────────────────────────────────────

interface SortDirectionToggleProps {
  value: TranscriptSortDirection;
  onChange: (dir: TranscriptSortDirection) => void;
  labels: { chronological: string; newestFirst: string; ariaLabel: string };
}

function SortDirectionToggle({ value, onChange, labels }: SortDirectionToggleProps) {
  return (
    <div
      role="group"
      data-testid="execution-log-sort"
      aria-label={labels.ariaLabel}
      className="inline-flex shrink-0 items-center rounded border bg-muted/40 p-0.5 text-xs"
    >
      <button
        type="button"
        aria-pressed={value === "chronological"}
        title={labels.chronological}
        onClick={() => onChange("chronological")}
        className={cn(
          "flex items-center gap-1 rounded px-1.5 py-0.5 transition-colors",
          value === "chronological"
            ? "bg-background text-foreground shadow-sm"
            : "text-muted-foreground hover:text-foreground",
        )}
      >
        <ArrowDownNarrowWide className="h-3 w-3" />
        <span className="hidden sm:inline">{labels.chronological}</span>
      </button>
      <button
        type="button"
        aria-pressed={value === "newest_first"}
        title={labels.newestFirst}
        onClick={() => onChange("newest_first")}
        className={cn(
          "flex items-center gap-1 rounded px-1.5 py-0.5 transition-colors",
          value === "newest_first"
            ? "bg-background text-foreground shadow-sm"
            : "text-muted-foreground hover:text-foreground",
        )}
      >
        <ArrowUpNarrowWide className="h-3 w-3" />
        <span className="hidden sm:inline">{labels.newestFirst}</span>
      </button>
    </div>
  );
}

// ─── Skeleton ────────────────────────────────────────────────────────────────

function ExecutionLogSkeleton() {
  return (
    <div className="divide-y">
      {Array.from({ length: 8 }).map((_, i) => (
        <div key={i} className="flex items-start gap-2 px-4 py-2">
          <Skeleton className="h-4 w-16 shrink-0" />
          <Skeleton className="h-4 flex-1" />
        </div>
      ))}
    </div>
  );
}

// ─── Event row ───────────────────────────────────────────────────────────────

// One row per loaded message. Rows are intentionally NOT coalesced: Virtuoso's
// firstItemIndex math requires row count to equal loaded message count. The row
// owns layout + tertiary meta; the per-kind body owns the reading hierarchy.
function ExecutionLogRow({
  message,
  open,
  onToggle,
}: {
  message: TaskMessagePayload;
  open: boolean;
  onToggle: () => void;
}) {
  const time = message.created_at
    ? new Date(message.created_at).toLocaleTimeString(undefined, {
        hour: "2-digit",
        minute: "2-digit",
      })
    : null;

  return (
    <div data-testid="execution-log-row" className="border-b px-4 py-2.5">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <ExecutionLogRowBody message={message} open={open} onToggle={onToggle} />
        </div>
        <div className="flex shrink-0 flex-col items-end gap-0.5 pt-0.5 text-[10px] tabular-nums text-muted-foreground/50">
          <span>#{message.seq}</span>
          {time && <span>{time}</span>}
        </div>
      </div>
    </div>
  );
}

function ExecutionLogRowBody({
  message,
  open,
  onToggle,
}: {
  message: TaskMessagePayload;
  open: boolean;
  onToggle: () => void;
}) {
  const { t } = useT("issues");
  const kind = traceEventKind(message);
  const label = traceEventLabel(message);
  const hasDetail = traceEventHasDetail(message);
  const moreLabel = t(($) => $.execution_log.show_more);
  const lessLabel = t(($) => $.execution_log.show_less);

  switch (kind) {
    case "agent": {
      const content = redactSecrets(message.content ?? "");
      return (
        <div className="space-y-1">
          <RowKindLabel className="text-muted-foreground/70">{label}</RowKindLabel>
          <ClampedText
            text={content}
            lines={TRACE_TEXT_PREVIEW_LINES}
            open={open}
            onToggle={onToggle}
            className="whitespace-pre-wrap text-sm leading-relaxed text-foreground"
            moreLabel={moreLabel}
            lessLabel={lessLabel}
          />
        </div>
      );
    }

    case "error": {
      const content = redactSecrets(message.content ?? "");
      return (
        <div className="space-y-1">
          <RowKindLabel className="text-destructive" icon={<AlertCircle className="h-3 w-3" />}>
            {label}
          </RowKindLabel>
          <ClampedText
            text={content}
            lines={TRACE_ERROR_PREVIEW_LINES}
            open={open}
            onToggle={onToggle}
            className="whitespace-pre-wrap text-sm leading-relaxed text-destructive"
            moreLabel={moreLabel}
            lessLabel={lessLabel}
          />
        </div>
      );
    }

    case "thinking": {
      const preview = redactSecrets(traceEventSummary(message));
      const full = redactSecrets(message.content ?? "");
      return (
        <div className="space-y-1">
          <ChevronLabel
            label={label}
            icon={<Brain className="h-3 w-3" />}
            open={open}
            hasDetail={hasDetail}
            onToggle={onToggle}
            className="text-muted-foreground/70"
          />
          {open && hasDetail ? (
            <p className="whitespace-pre-wrap text-xs italic leading-relaxed text-muted-foreground">
              {full}
            </p>
          ) : (
            <p className="truncate text-xs italic text-muted-foreground">{preview}</p>
          )}
        </div>
      );
    }

    case "tool_use": {
      const argSummary = redactSecrets(traceToolArgSummary(message.input));
      const inputJson = message.input
        ? redactSecrets(JSON.stringify(message.input, null, 2))
        : "";
      return (
        <div className="space-y-1">
          <button
            type="button"
            onClick={onToggle}
            disabled={!hasDetail}
            className={cn(
              "flex w-full min-w-0 items-center gap-1.5 text-left text-xs",
              hasDetail ? "cursor-pointer" : "cursor-default",
            )}
          >
            {hasDetail && (
              <ChevronRight
                className={cn(
                  "h-3 w-3 shrink-0 text-muted-foreground/50 transition-transform",
                  open && "rotate-90",
                )}
              />
            )}
            <span className="shrink-0 font-medium text-foreground">{label}</span>
            {argSummary && (
              <>
                <span className="shrink-0 text-muted-foreground/60">·</span>
                <span className="truncate text-muted-foreground">{argSummary}</span>
              </>
            )}
          </button>
          {open && hasDetail && (
            <pre className="max-h-72 overflow-auto rounded border bg-muted/40 p-2 font-mono text-[11px] leading-relaxed whitespace-pre-wrap break-words text-muted-foreground">
              {inputJson}
            </pre>
          )}
        </div>
      );
    }

    case "tool_result": {
      // Decode defensively once: historical records may be double-JSON-encoded,
      // so render the decoded text (real newlines / pretty JSON), not the blob.
      const { text: decoded } = decodeToolResultOutput(message.output ?? "");
      const fullOutput =
        decoded.length > OUTPUT_DETAIL_CAP
          ? redactSecrets(decoded.slice(0, OUTPUT_DETAIL_CAP)) + "\n... (truncated)"
          : redactSecrets(decoded);
      const preview = redactSecrets(
        firstLines(decoded, TRACE_RESULT_PREVIEW_LINES).slice(0, RESULT_PREVIEW_CHARS),
      );
      return (
        <div className="space-y-1">
          <button
            type="button"
            onClick={onToggle}
            disabled={!hasDetail}
            className={cn(
              "flex w-full min-w-0 items-center gap-1.5 text-left text-xs",
              hasDetail ? "cursor-pointer" : "cursor-default",
            )}
          >
            {hasDetail && (
              <ChevronRight
                className={cn(
                  "h-3 w-3 shrink-0 text-muted-foreground/50 transition-transform",
                  open && "rotate-90",
                )}
              />
            )}
            <span className="shrink-0 font-medium text-foreground">{label}</span>
          </button>
          {open && hasDetail ? (
            <pre className="max-h-72 overflow-auto rounded border bg-muted/40 p-2 font-mono text-[11px] leading-relaxed whitespace-pre-wrap break-words text-muted-foreground">
              {fullOutput}
            </pre>
          ) : (
            preview && (
              <pre
                className="overflow-hidden font-mono text-[11px] leading-relaxed whitespace-pre-wrap break-words text-muted-foreground/80"
                style={clampStyle(TRACE_RESULT_PREVIEW_LINES)}
              >
                {preview}
              </pre>
            )
          )}
        </div>
      );
    }

    default: {
      // Generic/unknown — label is the raw type; surface content, then output,
      // then input JSON (monospace) so nothing is silently dropped.
      const detail = redactSecrets(
        message.content ??
          message.output ??
          (message.input ? JSON.stringify(message.input, null, 2) : ""),
      );
      const mono = !message.content && !message.output && !!message.input;
      return (
        <div className="space-y-1">
          <RowKindLabel className="text-muted-foreground/70">{label}</RowKindLabel>
          <ClampedText
            text={detail}
            lines={TRACE_ERROR_PREVIEW_LINES}
            open={open}
            onToggle={onToggle}
            className={cn(
              "whitespace-pre-wrap text-xs leading-relaxed text-muted-foreground",
              mono && "font-mono",
            )}
            moreLabel={moreLabel}
            lessLabel={lessLabel}
          />
        </div>
      );
    }
  }
}

// ─── Row building blocks ─────────────────────────────────────────────────────

/** Small, subtle kind label for primary bodies (Agent / Error). The body — not
 *  this label — is meant to dominate the row. */
function RowKindLabel({
  children,
  icon,
  className,
}: {
  children: React.ReactNode;
  icon?: React.ReactNode;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 text-[11px] font-medium uppercase tracking-wide",
        className,
      )}
    >
      {icon}
      {children}
    </span>
  );
}

/** Clickable kind label with a rotating chevron — the expand entry for secondary
 *  bodies that toggle via the label (thinking). */
function ChevronLabel({
  label,
  icon,
  open,
  hasDetail,
  onToggle,
  className,
}: {
  label: string;
  icon?: React.ReactNode;
  open: boolean;
  hasDetail: boolean;
  onToggle: () => void;
  className?: string;
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      disabled={!hasDetail}
      className={cn(
        "inline-flex items-center gap-1 text-[11px] font-medium uppercase tracking-wide transition-colors",
        hasDetail ? "cursor-pointer hover:text-foreground" : "cursor-default",
        className,
      )}
    >
      {icon}
      {label}
      {hasDetail && (
        <ChevronRight className={cn("h-3 w-3 transition-transform", open && "rotate-90")} />
      )}
    </button>
  );
}

/** Multi-line text preview clamped to `lines` when collapsed, with a
 *  Show more / Show less toggle that appears only when the text actually
 *  overflows the clamp. Drives (and reflects) the shared `open` state. */
function ClampedText({
  text,
  lines,
  open,
  onToggle,
  className,
  moreLabel,
  lessLabel,
}: {
  text: string;
  lines: number;
  open: boolean;
  onToggle: () => void;
  className?: string;
  moreLabel: string;
  lessLabel: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [overflowing, setOverflowing] = useState(false);

  // Measure only while clamped: scrollHeight (full) vs clientHeight (clamped)
  // reveals whether there's hidden text worth a toggle. When open, keep the last
  // measured value so "Show less" stays available.
  useEffect(() => {
    const el = ref.current;
    if (!el || open) return;
    setOverflowing(el.scrollHeight - el.clientHeight > 1);
  }, [text, lines, open]);

  const showToggle = open || overflowing;

  return (
    <div className="space-y-1">
      <div
        ref={ref}
        className={cn(className, !open && "overflow-hidden")}
        style={open ? undefined : clampStyle(lines)}
      >
        {text || "(empty)"}
      </div>
      {showToggle && (
        <button
          type="button"
          onClick={onToggle}
          className="text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
        >
          {open ? lessLabel : moreLabel}
        </button>
      )}
    </div>
  );
}
