"use client";

import { useState, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import { Loader2, ChevronRight, ChevronDown, Brain, AlertCircle } from "lucide-react";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { useAutoScroll } from "@multica/ui/hooks/use-auto-scroll";
import { api } from "@multica/core/api";
import { Markdown } from "@multica/views/common/markdown";
import type { ChatMessage, TaskMessagePayload } from "@multica/core/types";
import type { ChatTimelineItem } from "@multica/core/chat";

// ─── Public component ────────────────────────────────────────────────────

interface ChatMessageListProps {
  messages: ChatMessage[];
  timelineItems: ChatTimelineItem[];
  isWaiting: boolean;
}

export function ChatMessageList({
  messages,
  timelineItems,
  isWaiting,
}: ChatMessageListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);
  useAutoScroll(scrollRef);

  const hasTimeline = timelineItems.length > 0;

  return (
    <div
      ref={scrollRef}
      style={fadeStyle}
      className="flex-1 overflow-y-auto px-4 py-3 space-y-4"
    >
      {messages.map((msg) => (
        <MessageBubble key={msg.id} message={msg} />
      ))}
      {/* Live streaming timeline */}
      {hasTimeline && (
        <div className="w-full space-y-1.5">
          <TimelineView items={timelineItems} />
        </div>
      )}
      {isWaiting && !hasTimeline && (
        <Loader2 className="size-4 animate-spin text-muted-foreground" />
      )}
    </div>
  );
}

// ─── Message bubbles ─────────────────────────────────────────────────────

function MessageBubble({ message }: { message: ChatMessage }) {
  if (message.role === "user") {
    return (
      <div className="flex justify-end">
        <div className="rounded-2xl bg-muted px-3.5 py-2 text-sm max-w-[80%] whitespace-pre-wrap break-words">
          {message.content}
        </div>
      </div>
    );
  }

  return <AssistantMessage message={message} />;
}

function AssistantMessage({
  message,
}: {
  message: ChatMessage;
}) {
  const taskId = message.task_id;

  // Always fetch task messages for assistant messages with a task_id
  const { data: taskMessages } = useQuery({
    queryKey: ["task-messages", taskId],
    queryFn: () => api.listTaskMessages(taskId!),
    enabled: !!taskId,
    staleTime: Infinity,
  });

  const timeline: ChatTimelineItem[] = (taskMessages ?? []).map(
    (m: TaskMessagePayload) => ({
      seq: m.seq,
      type: m.type,
      tool: m.tool,
      content: m.content,
      input: m.input,
      output: m.output,
    }),
  );

  return (
    <div className="w-full space-y-1.5">
      {timeline.length > 0 ? (
        <TimelineView items={timeline} />
      ) : (
        <div className="text-sm leading-relaxed prose prose-sm dark:prose-invert max-w-none">
          <Markdown>{message.content}</Markdown>
        </div>
      )}
    </div>
  );
}

// ─── Timeline: flat interleaved text + collapsible tool groups ───────────

interface TimelineSegment {
  kind: "text" | "tools";
  items: ChatTimelineItem[];
}

/** Split items into segments: consecutive non-text → "tools", consecutive text → merged "text". */
function segmentTimeline(items: ChatTimelineItem[]): TimelineSegment[] {
  const segments: TimelineSegment[] = [];
  let toolBuf: ChatTimelineItem[] = [];
  let textBuf: ChatTimelineItem[] = [];

  const flushTools = () => {
    if (toolBuf.length > 0) {
      segments.push({ kind: "tools", items: toolBuf });
      toolBuf = [];
    }
  };

  const flushText = () => {
    if (textBuf.length > 0) {
      segments.push({ kind: "text", items: textBuf });
      textBuf = [];
    }
  };

  for (const item of items) {
    if (item.type === "text") {
      flushTools();
      textBuf.push(item);
    } else {
      flushText();
      toolBuf.push(item);
    }
  }
  flushText();
  flushTools();
  return segments;
}

function TimelineView({ items }: { items: ChatTimelineItem[] }) {
  const segments = segmentTimeline(items);

  return (
    <>
      {segments.map((seg, i) =>
        seg.kind === "text" ? (
          <div key={seg.items[0]!.seq} className="text-sm leading-relaxed prose prose-sm dark:prose-invert max-w-none">
            <Markdown>{seg.items.map((t) => t.content ?? "").join("")}</Markdown>
          </div>
        ) : (
          <ToolGroupCollapsible
            key={seg.items[0]!.seq}
            items={seg.items}
            defaultOpen={i === segments.length - 1}
          />
        ),
      )}
    </>
  );
}

function ToolGroupCollapsible({
  items,
  defaultOpen,
}: {
  items: ChatTimelineItem[];
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen ?? false);
  const toolCount = items.filter((i) => i.type === "tool_use").length;
  const label = `${toolCount} ${toolCount === 1 ? "tool" : "tools"}`;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground transition-colors">
        {open ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
        <span>{label}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="mt-1 rounded-lg border bg-muted/20 p-2 space-y-0.5">
          {items.map((item) => (
            <ItemRow key={item.seq} item={item} />
          ))}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

// ─── Individual item rows ────────────────────────────────────────────────

function ItemRow({ item }: { item: ChatTimelineItem }) {
  switch (item.type) {
    case "tool_use":
      return <ToolCallRow item={item} />;
    case "tool_result":
      return <ToolResultRow item={item} />;
    case "thinking":
      return <ThinkingRow item={item} />;
    case "error":
      return <ErrorRow item={item} />;
    default:
      return null;
  }
}

function shortenPath(p: string): string {
  const parts = p.split("/");
  if (parts.length <= 3) return p;
  return ".../" + parts.slice(-2).join("/");
}

function getToolSummary(item: ChatTimelineItem): string {
  if (!item.input) return "";
  const inp = item.input as Record<string, string>;
  if (inp.query) return inp.query;
  if (inp.file_path) return shortenPath(inp.file_path);
  if (inp.path) return shortenPath(inp.path);
  if (inp.pattern) return inp.pattern;
  if (inp.description) return String(inp.description);
  if (inp.command) {
    const cmd = String(inp.command);
    return cmd.length > 100 ? cmd.slice(0, 100) + "..." : cmd;
  }
  if (inp.prompt) {
    const p = String(inp.prompt);
    return p.length > 100 ? p.slice(0, 100) + "..." : p;
  }
  if (inp.skill) return String(inp.skill);
  for (const v of Object.values(inp)) {
    if (typeof v === "string" && v.length > 0 && v.length < 120) return v;
  }
  return "";
}

function ToolCallRow({ item }: { item: ChatTimelineItem }) {
  const [open, setOpen] = useState(false);
  const summary = getToolSummary(item);
  const hasInput = item.input && Object.keys(item.input).length > 0;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-center gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <ChevronRight
          className={cn(
            "h-3 w-3 shrink-0 text-muted-foreground transition-transform",
            open && "rotate-90",
            !hasInput && "invisible",
          )}
        />
        <span className="font-medium text-foreground shrink-0">{item.tool}</span>
        {summary && <span className="truncate text-muted-foreground">{summary}</span>}
      </CollapsibleTrigger>
      {hasInput && (
        <CollapsibleContent>
          <pre className="ml-[18px] mt-0.5 max-h-32 overflow-auto rounded bg-muted/50 p-2 text-[11px] text-muted-foreground whitespace-pre-wrap break-all">
            {JSON.stringify(item.input, null, 2)}
          </pre>
        </CollapsibleContent>
      )}
    </Collapsible>
  );
}

function ToolResultRow({ item }: { item: ChatTimelineItem }) {
  const [open, setOpen] = useState(false);
  const output = item.output ?? "";
  if (!output) return null;

  const preview = output.length > 120 ? output.slice(0, 120) + "..." : output;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-start gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <ChevronRight
          className={cn("h-3 w-3 shrink-0 text-muted-foreground transition-transform mt-0.5", open && "rotate-90")}
        />
        <span className="text-muted-foreground/70 truncate">
          {item.tool ? `${item.tool} result: ` : "result: "}{preview}
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="ml-[18px] mt-0.5 max-h-40 overflow-auto rounded bg-muted/50 p-2 text-[11px] text-muted-foreground whitespace-pre-wrap break-all">
          {output.length > 4000 ? output.slice(0, 4000) + "\n... (truncated)" : output}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  );
}

function ThinkingRow({ item }: { item: ChatTimelineItem }) {
  const [open, setOpen] = useState(false);
  const text = item.content ?? "";
  if (!text) return null;

  const preview = text.length > 150 ? text.slice(0, 150) + "..." : text;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-start gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <Brain className="h-3 w-3 shrink-0 text-muted-foreground/60 mt-0.5" />
        <span className="text-muted-foreground italic truncate">{preview}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="ml-[18px] mt-0.5 max-h-40 overflow-auto rounded bg-muted/30 p-2 text-[11px] text-muted-foreground whitespace-pre-wrap break-words">
          {text}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  );
}

function ErrorRow({ item }: { item: ChatTimelineItem }) {
  return (
    <div className="flex items-start gap-1.5 px-1 -mx-1 py-0.5 text-xs">
      <AlertCircle className="h-3 w-3 shrink-0 text-destructive mt-0.5" />
      <span className="text-destructive">{item.content}</span>
    </div>
  );
}

// ─── Shared ──────────────────────────────────────────────────────────────

