"use client";

// Paginated + virtualized Execution Log dialog for TERMINAL (past-run) tasks
// (MUL-5122). A long past run can carry tens of thousands of events; the legacy
// AgentTranscriptDialog fetches and renders the whole array at once, which froze
// the browser. This dialog fetches bounded pages through
// `executionLogPageOptions` and renders one virtualized Virtuoso row per loaded
// message. Scope is strictly the terminal path — the live/chat streaming path
// still uses the shared task-messages cache and AgentTranscriptDialog.

import { useCallback, useMemo, useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { Virtuoso, type Components } from "react-virtuoso";
import { AlertCircle, Brain, ChevronRight, Copy, Check, Loader2, X } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  executionLogPageOptions,
  flattenExecutionLogPages,
  type ExecutionLogFilters,
} from "@multica/core/chat/queries";
import type { AgentTask } from "@multica/core/types/agent";
import type { TaskMessagePayload } from "@multica/core/types/events";
import { redactSecrets } from "./redact";
import {
  traceEventHasDetail,
  traceEventKind,
  traceEventLabel,
  traceEventSummary,
  type TraceEventKind,
} from "./trace-event-presenter";
import { useT } from "../../i18n";

const TOOL_KEY_PREFIX = "tool:";
const OUTPUT_DETAIL_CAP = 4000;

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

// Badge color per presenter kind. Thinking is de-emphasized (muted) and error is
// destructive; the rest mirror AgentTranscriptDialog so the two dialogs read the
// same.
const KIND_BADGE_CLASS: Record<TraceEventKind, string> = {
  agent: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
  tool_use: "bg-blue-500/15 text-blue-700 dark:text-blue-300",
  tool_result: "bg-muted text-muted-foreground",
  thinking: "bg-muted text-muted-foreground",
  error: "bg-destructive/15 text-destructive",
  generic: "bg-muted text-muted-foreground",
};

/** A human label for a type facet key ("text" → "Agent", etc.). */
function typeFacetLabel(key: string): string {
  return traceEventLabel({ type: key });
}

// ─── Virtuoso Header (loading-older / older-page error) ──────────────────────
//
// Module-scope + context-driven, matching chat-message-list: an inline
// components prop rebuilds the Header type every render and remounts its
// subtree, which is exactly the churn MUL-3960 fixed. Per-render data reaches
// the Header through Virtuoso's `context` prop instead.

interface LogListContext {
  isFetchingEarlier: boolean;
  earlierError: boolean;
  onRetryEarlier: () => void;
}

function LogListHeader({ context }: { context?: LogListContext }) {
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

const LOG_COMPONENTS: Components<TaskMessagePayload, LogListContext> = {
  Header: LogListHeader,
};

// ─── Main dialog ─────────────────────────────────────────────────────────────

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

  const pages = data?.pages ?? [];
  // firstItemIndex anchors scroll position when older pages prepend: every page
  // after page 0 is older history, so the running big-constant minus their count
  // keeps already-rendered rows in place across a prepend.
  const olderCount = pages.slice(1).reduce((sum, page) => sum + page.messages.length, 0);
  const firstItemIndex = messages.length > 0 ? 1_000_000 - olderCount : 0;

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

  const handleRetryEarlier = useCallback(() => {
    void fetchNextPage();
  }, [fetchNextPage]);

  // Copies only the currently loaded (and filtered) window — not the whole Run,
  // which may still have unfetched older pages.
  const handleCopyLoaded = useCallback(() => {
    const text = messages
      .map((m) => `[${traceEventLabel(m)}] ${traceEventSummary(m)}`)
      .join("\n");
    void copyText(text).then((ok) => {
      if (!ok) return;
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, [messages]);

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
        customScrollParent={scrollEl}
        data={messages}
        firstItemIndex={firstItemIndex}
        initialTopMostItemIndex={{ index: "LAST", align: "end" }}
        increaseViewportBy={{ top: 400, bottom: 600 }}
        startReached={() => {
          if (hasNextPage && !isFetchingNextPage) void fetchNextPage();
        }}
        context={listContext}
        components={LOG_COMPONENTS}
        itemContent={(_, message) => <ExecutionLogRow message={message} />}
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
// firstItemIndex math requires row count to equal loaded message count. Each row
// owns its own expansion state so opening one never opens the rest.
function ExecutionLogRow({ message }: { message: TaskMessagePayload }) {
  const [open, setOpen] = useState(false);
  const kind = traceEventKind(message);
  const label = traceEventLabel(message);
  const summary = traceEventSummary(message);
  const hasDetail = traceEventHasDetail(message);
  const time = message.created_at
    ? new Date(message.created_at).toLocaleTimeString(undefined, {
        hour: "2-digit",
        minute: "2-digit",
      })
    : null;

  return (
    <div data-testid="execution-log-row" className="border-b">
      <Collapsible open={open} onOpenChange={setOpen}>
        <div className="flex items-start gap-2 px-4 py-2">
          <span
            className={cn(
              "mt-0.5 inline-flex min-w-[60px] shrink-0 items-center justify-center rounded px-1.5 py-0.5 text-[11px] font-medium",
              KIND_BADGE_CLASS[kind],
            )}
          >
            {kind === "thinking" && <Brain className="mr-1 h-3 w-3 shrink-0" />}
            {kind === "error" && <AlertCircle className="mr-1 h-3 w-3 shrink-0" />}
            {label}
          </span>

          <CollapsibleTrigger
            disabled={!hasDetail}
            className={cn(
              "min-w-0 flex-1 py-0.5 text-left text-xs transition-colors",
              hasDetail ? "cursor-pointer hover:text-foreground" : "cursor-default",
              kind === "error" ? "text-destructive" : "text-muted-foreground",
            )}
          >
            <div className="flex items-start gap-1.5">
              {hasDetail && (
                <ChevronRight
                  className={cn(
                    "mt-0.5 h-3 w-3 shrink-0 text-muted-foreground/50 transition-transform",
                    open && "rotate-90",
                  )}
                />
              )}
              <span className="truncate">{summary || "(empty)"}</span>
            </div>
          </CollapsibleTrigger>

          <span className="mt-1 shrink-0 text-[10px] tabular-nums text-muted-foreground/50">
            #{message.seq}
          </span>
          {time && (
            <span className="mt-1 shrink-0 text-[10px] tabular-nums text-muted-foreground/50">
              {time}
            </span>
          )}
        </div>

        {hasDetail && (
          <CollapsibleContent>
            <div className="px-4 pb-3">
              <div className="ml-[72px] rounded border bg-muted/40">
                <ExecutionLogRowDetail message={message} />
              </div>
            </div>
          </CollapsibleContent>
        )}
      </Collapsible>
    </div>
  );
}

function ExecutionLogRowDetail({ message }: { message: TaskMessagePayload }) {
  if (message.type === "tool_use") {
    return (
      <pre className="max-h-60 overflow-auto whitespace-pre-wrap break-all p-3 text-[11px] text-muted-foreground">
        {message.input ? redactSecrets(JSON.stringify(message.input, null, 2)) : ""}
      </pre>
    );
  }
  if (message.type === "tool_result") {
    const output = message.output ?? "";
    return (
      <pre className="max-h-60 overflow-auto whitespace-pre-wrap break-all p-3 text-[11px] text-muted-foreground">
        {output.length > OUTPUT_DETAIL_CAP
          ? redactSecrets(output.slice(0, OUTPUT_DETAIL_CAP)) + "\n... (truncated)"
          : redactSecrets(output)}
      </pre>
    );
  }
  // text / thinking / error / generic — surface content, then output, then input.
  const detail =
    message.content ??
    message.output ??
    (message.input ? JSON.stringify(message.input, null, 2) : "");
  return (
    <pre
      className={cn(
        "max-h-60 overflow-auto whitespace-pre-wrap break-words p-3 text-[11px]",
        message.type === "error" ? "text-destructive" : "text-muted-foreground",
      )}
    >
      {redactSecrets(detail)}
    </pre>
  );
}
