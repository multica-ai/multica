"use client";

import { useState } from "react";
import {
  Brain,
  AlertCircle,
  ChevronRight,
  Loader2,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@multica/ui/components/ui/collapsible";
import type { TimelineItem } from "./build-timeline";
import { redactSecrets } from "./redact";

// ─── Color mapping for timeline segments ────────────────────────────────────

type EventColor = "agent" | "thinking" | "tool" | "result" | "error";

function getEventColor(item: TimelineItem): EventColor {
  switch (item.type) {
    case "text":
      return "agent";
    case "thinking":
      return "thinking";
    case "tool_use":
      return "tool";
    case "tool_result":
      return "result";
    case "error":
      return "error";
    default:
      return "result";
  }
}

const colorClasses: Record<EventColor, { bg: string; bgActive: string; label: string }> = {
  agent: { bg: "bg-emerald-400/60", bgActive: "bg-emerald-500", label: "bg-emerald-500" },
  thinking: { bg: "bg-violet-400/60", bgActive: "bg-violet-500", label: "bg-violet-500/20 text-violet-700 dark:text-violet-300" },
  tool: { bg: "bg-blue-400/60", bgActive: "bg-blue-500", label: "bg-blue-500/20 text-blue-700 dark:text-blue-300" },
  result: { bg: "bg-slate-300/60 dark:bg-slate-600/60", bgActive: "bg-slate-400 dark:bg-slate-500", label: "bg-muted text-muted-foreground" },
  error: { bg: "bg-red-400/60", bgActive: "bg-red-500", label: "bg-red-500/20 text-red-700 dark:text-red-300" },
};

// ─── Helpers ────────────────────────────────────────────────────────────────

function getEventLabel(item: TimelineItem): string {
  switch (item.type) {
    case "text":
      return "Agent";
    case "thinking":
      return "Thinking";
    case "tool_use":
      return item.tool ?? "Tool";
    case "tool_result":
      return item.tool ? `${item.tool}` : "Result";
    case "error":
      return "Error";
    default:
      return "Event";
  }
}

export function getEventSummary(item: TimelineItem): string {
  switch (item.type) {
    case "text":
      return item.content?.split("\n").find((l) => l.trim().length > 0) ?? "";
    case "thinking":
      return item.content?.slice(0, 200) ?? "";
    case "tool_use": {
      if (!item.input) return "";
      const inp = item.input as Record<string, string>;
      if (inp.query) return inp.query;
      if (inp.file_path) return shortenPath(inp.file_path);
      if (inp.path) return shortenPath(inp.path);
      if (inp.pattern) return inp.pattern;
      if (inp.description) return String(inp.description);
      if (inp.command) {
        const cmd = String(inp.command);
        return cmd.length > 120 ? cmd.slice(0, 120) + "..." : cmd;
      }
      if (inp.prompt) {
        const p = String(inp.prompt);
        return p.length > 120 ? p.slice(0, 120) + "..." : p;
      }
      if (inp.skill) return String(inp.skill);
      for (const v of Object.values(inp)) {
        if (typeof v === "string" && v.length > 0 && v.length < 120) return v;
      }
      return "";
    }
    case "tool_result":
      return item.output?.slice(0, 200) ?? "";
    case "error":
      return item.content ?? "";
    default:
      return "";
  }
}

function shortenPath(p: string): string {
  const parts = p.split("/");
  if (parts.length <= 3) return p;
  return ".../" + parts.slice(-2).join("/");
}

// ─── Component interface ──────────────────────────────────────────────────────

export interface TaskTranscriptTimelineProps {
  items: TimelineItem[];
  isLive?: boolean;
  className?: string;
  emptyLabel?: string;
  liveEmptyLabel?: string;
}

// ─── Main component ─────────────────────────────────────────────────────────

export function TaskTranscriptTimeline({
  items,
  isLive = false,
  className,
  emptyLabel = "No events yet.",
  liveEmptyLabel = "Waiting for events...",
}: TaskTranscriptTimelineProps) {
  if (items.length === 0) {
    return (
      <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
        {isLive ? (
          <div className="flex items-center gap-2">
            <Loader2 className="h-4 w-4 animate-spin" />
            {liveEmptyLabel}
          </div>
        ) : (
          emptyLabel
        )}
      </div>
    );
  }

  return (
    <div className={cn("divide-y", className)}>
      {items.map((item) => (
        <TranscriptEventRow key={item.seq} item={item} />
      ))}
    </div>
  );
}

// ─── Transcript event row ───────────────────────────────────────────────────

interface TranscriptEventRowProps {
  item: TimelineItem;
}

function TranscriptEventRow({ item }: TranscriptEventRowProps) {
  const [expanded, setExpanded] = useState(false);
  const color = getEventColor(item);
  const label = getEventLabel(item);
  const summary = getEventSummary(item);

  const hasDetail =
    (item.type === "tool_use" && item.input && Object.keys(item.input).length > 0) ||
    (item.type === "tool_result" && item.output && item.output.length > 0) ||
    (item.type === "thinking" && item.content && item.content.length > 0) ||
    (item.type === "text" && item.content && item.content.length > 0) ||
    (item.type === "error" && item.content && item.content.length > 0);

  return (
    <div className="group transition-colors">
      <Collapsible open={expanded} onOpenChange={setExpanded}>
        <div className="flex items-start gap-2 px-4 py-2">
          {/* Type label badge */}
          <span
            className={cn(
              "inline-flex items-center shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium mt-0.5 min-w-[60px] justify-center",
              colorClasses[color].label,
            )}
          >
            {item.type === "thinking" && <Brain className="h-3 w-3 mr-1 shrink-0" />}
            {item.type === "error" && <AlertCircle className="h-3 w-3 mr-1 shrink-0" />}
            {label}
          </span>

          {/* Summary */}
          <CollapsibleTrigger
            className={cn(
              "flex-1 text-left text-xs min-w-0 py-0.5 transition-colors",
              hasDetail ? "cursor-pointer hover:text-foreground" : "cursor-default",
              item.type === "error" ? "text-destructive" : "text-muted-foreground",
            )}
            disabled={!hasDetail}
          >
            <div className="flex items-start gap-1.5">
              {hasDetail && (
                <ChevronRight
                  className={cn(
                    "h-3 w-3 shrink-0 mt-0.5 text-muted-foreground/50 transition-transform",
                    expanded && "rotate-90",
                  )}
                />
              )}
              <span className="truncate">{summary || "(empty)"}</span>
            </div>
          </CollapsibleTrigger>

          {/* Seq number / index */}
          <span className="shrink-0 text-[10px] text-muted-foreground/50 tabular-nums mt-1">
            #{item.seq}
          </span>
        </div>

        {/* Expanded detail */}
        {hasDetail && (
          <CollapsibleContent>
            <div className="px-4 pb-3">
              <div className="ml-[72px] rounded bg-muted/40 border">
                <EventDetailContent item={item} />
              </div>
            </div>
          </CollapsibleContent>
        )}
      </Collapsible>
    </div>
  );
}

// ─── Event detail content ───────────────────────────────────────────────────

function EventDetailContent({ item }: { item: TimelineItem }) {
  switch (item.type) {
    case "tool_use":
      return (
        <pre className="max-h-60 overflow-auto p-3 text-[11px] text-muted-foreground whitespace-pre-wrap break-all">
          {item.input ? redactSecrets(JSON.stringify(item.input, null, 2)) : ""}
        </pre>
      );
    case "tool_result":
      return (
        <pre className="max-h-60 overflow-auto p-3 text-[11px] text-muted-foreground whitespace-pre-wrap break-all">
          {item.output
            ? item.output.length > 4000
              ? redactSecrets(item.output.slice(0, 4000)) + "\n... (truncated)"
              : redactSecrets(item.output)
            : ""}
        </pre>
      );
    case "thinking":
      return (
        <pre className="max-h-60 overflow-auto p-3 text-[11px] text-muted-foreground whitespace-pre-wrap break-words">
          {item.content ?? ""}
        </pre>
      );
    case "text":
      return (
        <pre className="max-h-60 overflow-auto p-3 text-[11px] text-muted-foreground whitespace-pre-wrap break-words">
          {item.content ?? ""}
        </pre>
      );
    case "error":
      return (
        <pre className="max-h-60 overflow-auto p-3 text-[11px] text-destructive whitespace-pre-wrap break-words">
          {item.content ?? ""}
        </pre>
      );
    default:
      return null;
  }
}
