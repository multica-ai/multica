"use client";

import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Brain, Check, ChevronDown, ChevronRight, ClipboardList, Code2, FileText, Loader2, MessageSquare, Radio, RefreshCw, ShieldAlert, Terminal, Wrench, X } from "lucide-react";
import { api } from "@multica/core/api";
import { useWSEvent } from "@multica/core/realtime";
import type { AgentTask, TaskInteraction, TaskTraceLine } from "@multica/core/types";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@multica/ui/components/ui/collapsible";
import { cn } from "@multica/ui/lib/utils";
import { Markdown } from "@multica/ui/markdown";
import { toast } from "sonner";
import { redactSecrets } from "../../common/task-transcript/redact";

interface TaskTraceOutputProps {
  task: AgentTask;
  defaultOpen?: boolean;
  compact?: boolean;
  fill?: boolean;
}

function metadataHealthPort(metadata: Record<string, unknown> | undefined): number | null {
  const value = metadata?.health_port;
  if (typeof value === "number" && Number.isFinite(value) && value > 0) return value;
  if (typeof value === "string") {
    const parsed = Number.parseInt(value, 10);
    if (Number.isFinite(parsed) && parsed > 0) return parsed;
  }
  return null;
}

function lineText(line: TaskTraceLine): string {
  const text = line.content || line.raw_payload || "";
  return redactSecrets(text);
}

function lineLabel(channel: string): string {
  switch (channel) {
    case "display_event":
      return "event";
    case "provider_event":
      return "raw";
    case "command_stdout":
      return "stdout";
    case "command_stderr":
      return "stderr";
    case "approval_request":
      return "approval";
    case "approval_response":
      return "response";
    case "raw_stdout":
      return "process";
    case "raw_stderr":
      return "process";
    default:
      return channel.replace(/_/g, " ");
  }
}

function channelClass(channel: string): string {
  switch (channel) {
    case "display_event":
      return "text-foreground";
    case "provider_event":
      return "text-muted-foreground";
    case "command_stdout":
    case "raw_stdout":
      return "text-foreground";
    case "command_stderr":
    case "raw_stderr":
      return "text-destructive";
    case "approval_request":
      return "text-warning";
    case "approval_response":
      return "text-info";
    default:
      return "text-foreground";
  }
}

function isApprovalTraceChannel(channel: string): boolean {
  return channel === "approval_request" || channel === "approval_response";
}

interface DisplayEvent {
  type?: string;
  title?: string;
  content?: string;
  metadata?: Record<string, unknown>;
}

interface TraceWorkItem {
  line: TaskTraceLine;
  event: DisplayEvent | null;
  pairedEvent?: DisplayEvent;
  group?: TraceWorkItem[];
  groupKind?: "context";
  approvalRequestLine?: TaskTraceLine;
  approvalResponseLine?: TaskTraceLine;
}

function parseDisplayEvent(line: TaskTraceLine): DisplayEvent | null {
  const value = line.content || line.raw_payload || "";
  if (!value) return null;
  try {
    const parsed = JSON.parse(value) as DisplayEvent;
    if (!parsed || typeof parsed !== "object") return null;
    return parsed;
  } catch {
    return null;
  }
}

function metadataText(metadata: Record<string, unknown> | undefined, key: string): string {
  const value = metadata?.[key];
  return typeof value === "string" ? value : "";
}

function eventCallId(event: DisplayEvent | null): string {
  return metadataText(event?.metadata, "call_id");
}

function compactJson(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function unwrapJson(value: string): unknown {
  let current: unknown = value;
  for (let i = 0; i < 3; i += 1) {
    if (typeof current !== "string") return current;
    const trimmed = current.trim();
    if (!trimmed) return "";
    if (!/^[{["]/.test(trimmed)) return current;
    try {
      current = JSON.parse(trimmed);
    } catch {
      return current;
    }
  }
  return current;
}

function summarizeJson(value: unknown): string {
  if (Array.isArray(value)) return `${value.length} items`;
  if (!value || typeof value !== "object") return "";
  const record = value as Record<string, unknown>;
  const identifier = typeof record.identifier === "string" ? record.identifier : "";
  const title = typeof record.title === "string" ? record.title : "";
  const status = typeof record.status === "string" ? record.status : "";
  if (identifier || title) return [identifier, title, status && `(${status})`].filter(Boolean).join(" ");
  const keys = Object.keys(record);
  if (keys.length === 0) return "Empty object";
  return `${keys.length} fields: ${keys.slice(0, 6).join(", ")}${keys.length > 6 ? "..." : ""}`;
}

function normalizedInputRecord(input: unknown): Record<string, unknown> | null {
  if (input && typeof input === "object" && !Array.isArray(input)) {
    return input as Record<string, unknown>;
  }
  if (typeof input !== "string") return null;
  const parsed = unwrapJson(input);
  if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
    return parsed as Record<string, unknown>;
  }
  return null;
}

function nestedInputCandidates(input: unknown): Array<Record<string, unknown>> {
  const record = normalizedInputRecord(input);
  if (!record) return [];

  const candidates = [record];
  for (const key of ["args", "arguments", "input", "payload", "tool_input", "params"]) {
    const nested = normalizedInputRecord(record[key]);
    if (nested) candidates.push(nested);
  }
  return candidates;
}

function summarizeResultForCommand(command: string, parsed: unknown): string {
  if (Array.isArray(parsed)) {
    if (/\bmultica\s+issue\s+comment\s+list\b/.test(command)) return `Loaded ${parsed.length} comments`;
    if (/\bmultica\s+issue\s+list\b/.test(command)) return `Loaded ${parsed.length} issues`;
    return `${parsed.length} items returned`;
  }
  if (!parsed || typeof parsed !== "object") return "";
  const record = parsed as Record<string, unknown>;
  if (/\bmultica\s+issue\s+get\b/.test(command)) {
    const identifier = typeof record.identifier === "string" ? record.identifier : "issue";
    const title = typeof record.title === "string" ? record.title : "";
    return [identifier, title].filter(Boolean).join(": ");
  }
  if (/\bmultica\s+issue\s+comment\s+add\b/.test(command)) return "Comment posted";
  return summarizeJson(parsed);
}

function commandFromInput(input: unknown): string {
  const candidates = nestedInputCandidates(input);
  for (const record of candidates) {
    for (const key of ["command", "cmd", "script"]) {
      const value = record[key];
      if (typeof value === "string" && value.trim()) return value.trim();
    }
  }
  if (typeof input === "string") return input.trim();
  return "";
}

function descriptionFromInput(input: unknown): string {
  const candidates = nestedInputCandidates(input);
  for (const record of candidates) {
    for (const key of ["description", "summary", "purpose"]) {
      const value = record[key];
      if (typeof value === "string" && value.trim()) return value.trim();
    }
  }
  return "";
}

function inputText(input: unknown, keys: string[]): string {
  const candidates = nestedInputCandidates(input);
  for (const record of candidates) {
    for (const key of keys) {
      const value = record[key];
      if (typeof value === "string" && value.trim()) return value.trim();
    }
  }
  return "";
}

function exitPlanText(input: unknown): string {
  if (typeof input === "string") return input.trim();
  if (!input || typeof input !== "object") return "";
  const record = input as Record<string, unknown>;
  for (const key of ["plan", "content", "summary", "message"]) {
    const value = record[key];
    if (typeof value === "string" && value.trim()) return value.trim();
  }
  return "";
}

function classifyCommand(command: string): string {
  if (!command) return "Run command";
  if (/\bmultica\s+issue\s+get\b/.test(command)) return "Read issue";
  if (/\bmultica\s+issue\s+comment\s+list\b/.test(command)) return "Read comments";
  if (/\bmultica\s+issue\s+comment\s+add\b/.test(command)) return "Post comment";
  if (/\b(go|pnpm|npm|yarn|bun)\s+(test|build|vet|tsc)\b/.test(command)) return "Run verification";
  if (/\b(rg|grep|find)\b/.test(command)) return "Search code";
  if (/\b(git\s+status|git\s+diff|git\s+show)\b/.test(command)) return "Inspect changes";
  return "Run command";
}

function toolKind(title: string): "read" | "search" | "write" | "edit" | "other" {
  const tool = title.toLowerCase();
  if (tool === "read" || tool === "ls") return "read";
  if (tool === "grep" || tool === "glob") return "search";
  if (tool === "write" || tool === "notebookedit") return "write";
  if (tool === "edit" || tool === "multiedit") return "edit";
  return "other";
}

function summarizeToolResult(kind: ReturnType<typeof toolKind>, result: string | undefined): string {
  if (result === undefined) return "";
  const text = redactSecrets(String(unwrapJson(result)));
  if (!text.trim()) return "Completed";
  const lines = text.split(/\r?\n/).filter((line: string) => line.trim().length > 0);
  if (kind === "read") return `Read ${lines.length || 1} line${lines.length === 1 ? "" : "s"}`;
  if (kind === "search") return `Found ${lines.length} matching line${lines.length === 1 ? "" : "s"}`;
  if (kind === "write" || kind === "edit") {
    if (/success|updated|written|modified|created/i.test(text)) return "File updated";
    return "Change applied";
  }
  return lines[0]?.slice(0, 160) ?? "Completed";
}

function normalizedCommand(command: string): string {
  return command.replace(/\s+/g, " ").trim();
}

function actionKey(event: DisplayEvent): string {
  const callID = eventCallId(event);
  if (callID) return `call:${callID}`;
  if (event.type === "command_start") return `command:${normalizedCommand(event.content ?? "")}`;
  if (event.type === "tool_call") {
    const command = commandFromInput(event.metadata?.input);
    if (command) return `command:${normalizedCommand(command)}`;
    return `tool:${event.title ?? ""}:${compactJson(event.metadata?.input).slice(0, 300)}`;
  }
  if (event.type === "command_output" || event.type === "tool_result") {
    return `${event.type}:${(event.content ?? "").slice(0, 300)}`;
  }
  return `${event.type ?? "event"}:${event.title ?? ""}:${(event.content ?? "").slice(0, 300)}`;
}

function pairResultIndex(events: Array<DisplayEvent | null>, consumed: Set<number>, start: number, expectedType: string, callID: string): number | null {
  for (let j = start + 1; j < events.length; j += 1) {
    if (consumed.has(j)) continue;
    const candidate = events[j];
    if (!candidate) continue;
    if (candidate.type === expectedType) {
      const candidateCallID = eventCallId(candidate);
      if (callID) {
        if (candidateCallID === callID) return j;
        continue;
      }
      return j;
    }
    if (!callID && (candidate.type === "tool_call" || candidate.type === "command_start")) {
      return null;
    }
  }
  return null;
}

function workItemAcceptsApproval(item: TraceWorkItem | undefined): boolean {
  if (!item?.event) return false;
  return item.event.type === "tool_call" || item.event.type === "command_start";
}

function appendApprovalLine(items: TraceWorkItem[], line: TaskTraceLine): boolean {
  const previous = items[items.length - 1];
  if (!workItemAcceptsApproval(previous)) return false;
  if (!previous) return false;
  if (line.channel === "approval_request") {
    previous.approvalRequestLine = line;
    return true;
  }
  if (line.channel === "approval_response") {
    previous.approvalResponseLine = line;
    return true;
  }
  return false;
}

function buildWorkItems(lines: TaskTraceLine[], showRaw: boolean): TraceWorkItem[] {
  if (showRaw) return lines.map((line) => ({ line, event: parseDisplayEvent(line) }));

  const items: TraceWorkItem[] = [];
  const consumed = new Set<number>();
  const parsed = lines.map((line) => parseDisplayEvent(line));
  const recentNarration = new Map<string, number>();
  const recentActions = new Map<string, number>();
  const recentResults = new Map<string, number>();

  for (let i = 0; i < lines.length; i += 1) {
    if (consumed.has(i)) continue;
    const line = lines[i]!;
    const event = parsed[i];
    if (isApprovalTraceChannel(line.channel) && appendApprovalLine(items, line)) {
      continue;
    }
    if (line.channel !== "display_event" || !event) {
      items.push({ line, event: event ?? null });
      continue;
    }

    if ((event.type === "assistant_text" || event.type === "thinking") && event.content) {
      const key = `${event.type}:${event.content.trim()}`;
      const previousIndex = recentNarration.get(key);
      if (previousIndex !== undefined && i - previousIndex < 20) {
        continue;
      }
      recentNarration.set(key, i);
    }

    if (event.type === "status" && (event.content === "running" || event.content === "completed" || event.content === "task_progress")) {
      continue;
    }

    if (event.type === "tool_call" || event.type === "command_start") {
      const expectedType = event.type === "tool_call" ? "tool_result" : "command_output";
      const callID = eventCallId(event);
      const key = actionKey(event);
      const previousIndex = recentActions.get(key);
      const pairIndex = pairResultIndex(parsed, consumed, i, expectedType, callID);
      if (previousIndex !== undefined && i - previousIndex < 30) {
        if (pairIndex !== null) consumed.add(pairIndex);
        continue;
      }
      recentActions.set(key, i);
      if (pairIndex !== null) {
        consumed.add(pairIndex);
        items.push({ line, event, pairedEvent: parsed[pairIndex] ?? undefined });
        continue;
      }
      if (!items.some((item) => item.line === line)) items.push({ line, event });
      continue;
    }

    if (event.type === "tool_result" || event.type === "command_output") {
      const key = actionKey(event);
      const previousIndex = recentResults.get(key);
      if (previousIndex !== undefined && i - previousIndex < 30) continue;
      recentResults.set(key, i);
    }

    items.push({ line, event });
  }

  return groupReadSearchItems(items);
}

function commandLabel(command: string, fallbackInput?: unknown): string {
  if (command) return `$ ${command}`;
  const fallback = compactJson(normalizedInputRecord(fallbackInput) ?? fallbackInput);
  if (fallback) return fallback;
  return "Command";
}

function workItemToolKind(item: TraceWorkItem): "read" | "search" | "todo" | null {
  if (item.event?.type !== "tool_call") return null;
  const title = (item.event.title ?? "").toLowerCase();
  if (title === "todowrite" || title === "todo" || title.includes("todo")) return "todo";
  const kind = toolKind(item.event.title ?? "");
  return kind === "read" || kind === "search" ? kind : null;
}

function groupReadSearchItems(items: TraceWorkItem[]): TraceWorkItem[] {
  const grouped: TraceWorkItem[] = [];
  let pending: TraceWorkItem[] = [];

  const flush = () => {
    if (pending.length === 0) return;
    grouped.push({
      line: pending[0]!.line,
      event: pending[0]!.event,
      group: pending,
      groupKind: "context",
    });
    pending = [];
  };

  for (const item of items) {
    const kind = workItemToolKind(item);
    if (!kind) {
      flush();
      grouped.push(item);
      continue;
    }
    pending.push(item);
  }
  flush();
  return grouped;
}

function approvalTraceAppearance(channel: string, text: string): {
  icon: ReactNode;
  title: string;
  containerClass: string;
  titleClass: string;
  textClass: string;
} {
  if (channel === "approval_request") {
    return {
      icon: <ShieldAlert className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />,
      title: "Approval",
      containerClass: "border-warning/30 bg-warning/10",
      titleClass: "text-warning",
      textClass: "text-foreground",
    };
  }

  const normalized = text.toLowerCase();
  if (/allow|approved=true|accept/.test(normalized)) {
    return {
      icon: <Check className="mt-0.5 h-3.5 w-3.5 shrink-0 text-success" />,
      title: "Response",
      containerClass: "border-success/30 bg-success/10",
      titleClass: "text-success",
      textClass: "text-foreground",
    };
  }
  if (/deny|reject|approved=false|cancel/.test(normalized)) {
    return {
      icon: <X className="mt-0.5 h-3.5 w-3.5 shrink-0 text-destructive" />,
      title: "Response",
      containerClass: "border-destructive/30 bg-destructive/10",
      titleClass: "text-destructive",
      textClass: "text-foreground",
    };
  }
  return {
    icon: <Radio className="mt-0.5 h-3.5 w-3.5 shrink-0 text-info" />,
    title: "Response",
    containerClass: "border-info/30 bg-info/10",
    titleClass: "text-info",
    textClass: "text-foreground",
  };
}

function approvalTone(channel: string, text: string): "warning" | "success" | "error" | "info" {
  if (channel === "approval_request") return "warning";
  const normalized = text.toLowerCase();
  if (/allow|approved=true|accept/.test(normalized)) return "success";
  if (/deny|reject|approved=false|cancel/.test(normalized)) return "error";
  return "info";
}

function ApprovalTraceLine({ line }: { line: TaskTraceLine }) {
  const text = lineText(line);
  const appearance = approvalTraceAppearance(line.channel, text);

  return (
    <div className={cn("rounded-md border px-3 py-2 text-[13px] leading-5", appearance.containerClass)}>
      <div className="flex min-w-0 items-start gap-2">
        {appearance.icon}
        <div className="min-w-0 flex-1">
          <div className={cn("text-xs font-semibold", appearance.titleClass)}>{appearance.title}</div>
          <div className={cn("mt-1 whitespace-pre-wrap break-words", appearance.textClass)}>{text}</div>
        </div>
      </div>
    </div>
  );
}

function InlineApprovalStatus({
  requestLine,
  responseLine,
}: {
  requestLine?: TaskTraceLine;
  responseLine?: TaskTraceLine;
}) {
  if (!requestLine && !responseLine) return null;

  return (
    <div className="mt-2 space-y-1">
      {requestLine && (
        <div className="flex items-start gap-2 px-0.5 py-0.5">
          <ShieldAlert className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning/90" />
          <div className="min-w-0 flex-1">
            <div className="text-[11px] font-semibold uppercase tracking-[0.08em] text-warning/90">Approval</div>
            <div className="mt-0.5 whitespace-pre-wrap break-words text-[12px] text-foreground/85">{lineText(requestLine)}</div>
          </div>
        </div>
      )}
      {responseLine && (
        <div className={cn(
          "flex items-start gap-2 px-0.5 py-0.5",
        )}>
          {approvalTone(responseLine.channel, lineText(responseLine)) === "success" ? (
            <Check className="mt-0.5 h-3.5 w-3.5 shrink-0 text-success/90" />
          ) : approvalTone(responseLine.channel, lineText(responseLine)) === "error" ? (
            <X className="mt-0.5 h-3.5 w-3.5 shrink-0 text-destructive/90" />
          ) : (
            <Radio className="mt-0.5 h-3.5 w-3.5 shrink-0 text-info/90" />
          )}
          <div className="min-w-0 flex-1">
            <div className={cn(
              "text-[11px] font-semibold uppercase tracking-[0.08em]",
              approvalTone(responseLine.channel, lineText(responseLine)) === "success" && "text-success/90",
              approvalTone(responseLine.channel, lineText(responseLine)) === "error" && "text-destructive/90",
              approvalTone(responseLine.channel, lineText(responseLine)) === "info" && "text-info/90",
            )}>Response</div>
            <div className="mt-0.5 whitespace-pre-wrap break-words text-[12px] text-foreground/85">{lineText(responseLine)}</div>
          </div>
        </div>
      )}
    </div>
  );
}

function DisplayTraceLine({
  line,
  event: parsedEvent,
  pairedEvent,
  group,
  groupKind,
  approvalRequestLine,
  approvalResponseLine,
}: TraceWorkItem) {
  if (group && groupKind) {
    return <ContextActivityBlock items={group} />;
  }
  const event = parsedEvent ?? parseDisplayEvent(line);
  if (!event) {
    return (
      <WorkLogBlock title="Event" muted>
        <pre className="max-h-40 overflow-y-auto whitespace-pre-wrap break-words font-mono text-muted-foreground">{lineText(line)}</pre>
      </WorkLogBlock>
    );
  }

  const type = event.type ?? "event";
  const title = event.title ?? type.replace(/_/g, " ");
  const content = redactSecrets(event.content ?? "");

  switch (type) {
    case "assistant_text":
      return (
        <WorkLogBlock tone="assistant" title={title || "Assistant"} icon={<MessageSquare className="h-3.5 w-3.5" />}>
          <Markdown mode="minimal" className="text-[13px] leading-5">
            {content}
          </Markdown>
        </WorkLogBlock>
      );
    case "thinking":
      return (
        <WorkLogBlock title="Progress note" tone="thinking" icon={<Brain className="h-3.5 w-3.5" />}>
          <div className="whitespace-pre-wrap break-words text-violet-950 dark:text-violet-100">{content}</div>
        </WorkLogBlock>
      );
    case "plan_stage":
      return (
        <WorkLogBlock title={title || "Plan mode"} tone="plan" icon={<ClipboardList className="h-3.5 w-3.5" />}>
          <div className="whitespace-pre-wrap break-words text-indigo-950 dark:text-indigo-100">{content}</div>
        </WorkLogBlock>
      );
    case "command_start":
      return (
        <CommandBlock
          command={content}
          result={pairedEvent?.content}
          approvalRequestLine={approvalRequestLine}
          approvalResponseLine={approvalResponseLine}
        />
      );
    case "command_output":
      return (
        <WorkLogBlock title="Command output" tone="result" icon={<Terminal className="h-3.5 w-3.5" />}>
          <pre className="max-h-56 overflow-y-auto whitespace-pre-wrap break-all font-mono text-[12px] text-foreground">{content || "(no output)"}</pre>
        </WorkLogBlock>
      );
    case "tool_call": {
      const input = event.metadata?.input;
      const command = commandFromInput(input);
      const description = descriptionFromInput(input);
      if (title === "ExitPlanMode") {
        const plan = exitPlanText(input);
        return (
          <WorkLogBlock title="Plan ready" tone="plan" icon={<ClipboardList className="h-3.5 w-3.5" />}>
            {plan ? (
              <Markdown mode="minimal" className="text-[13px] leading-5">
                {redactSecrets(plan)}
              </Markdown>
            ) : (
              <ExpandableRaw label="Tool input" value={redactSecrets(compactJson(input))} defaultOpen />
            )}
          </WorkLogBlock>
        );
      }
      if (title === "Bash" || command) {
        return (
          <CommandBlock
            command={command}
            description={description}
            result={pairedEvent?.content}
            fallbackInput={input}
            approvalRequestLine={approvalRequestLine}
            approvalResponseLine={approvalResponseLine}
          />
        );
      }
      const kind = toolKind(title);
      if (kind === "read" || kind === "search") {
        return <ContextActivityBlock items={[{ line, event, pairedEvent }]} />;
      }
      if (kind === "write" || kind === "edit") {
        return <FileChangeBlock kind={kind} title={title} input={input} result={pairedEvent?.content} />;
      }
      const inputText = compactJson(input);
      return (
        <WorkLogBlock title={title} icon={<Wrench className="h-3.5 w-3.5" />}>
          {inputText && <ExpandableRaw label="Tool input" value={redactSecrets(inputText)} />}
        </WorkLogBlock>
      );
    }
    case "tool_result": {
      const parsed = unwrapJson(content);
      const summary = summarizeJson(parsed);
      return (
        <WorkLogBlock title={summary ? "Tool result" : "Tool output"} tone="result">
          {summary && <div className="text-xs text-foreground">{redactSecrets(summary)}</div>}
          {typeof parsed === "string" ? (
          <pre className="mt-1 max-h-56 overflow-y-auto overflow-x-hidden whitespace-pre-wrap break-all font-mono text-[12px] text-muted-foreground">{redactSecrets(parsed || "(empty result)")}</pre>
          ) : (
            <ExpandableRaw label="Raw result" value={redactSecrets(compactJson(parsed))} defaultOpen={!summary} />
          )}
        </WorkLogBlock>
      );
    }
    case "file_change": {
      const changes = extractFileChanges(event.metadata);
      if (changes.length === 0) {
        return (
          <WorkLogBlock title="File change" tone="file" icon={<Code2 className="h-3.5 w-3.5" />}>
            <div className="break-all font-mono text-[12px] text-amber-800 dark:text-amber-300">{content || title}</div>
          </WorkLogBlock>
        );
      }
      // Only render diff on "started" (which carries the data); skip "completed" to avoid duplicate.
      if (content === "completed") return null;
      return (
        <>
          {changes.map((ch, i) => {
            const rows = parseUnifiedDiffRows(ch.diff);
            return (
              <WorkLogBlock key={i} title="Edit file" tone="file" icon={<Code2 className="h-3.5 w-3.5" />}>
                <div className="break-all font-mono text-[12.5px] font-semibold text-amber-950 dark:text-amber-100">{ch.path}</div>
                {rows.length > 0 && <UnifiedDiffPreview rows={rows} />}
              </WorkLogBlock>
            );
          })}
        </>
      );
    }
    case "approval_prompt":
      if (event.metadata?.interaction_type === "plan_approval") {
        return (
          <WorkLogBlock title="Plan decision" tone="plan" icon={<ClipboardList className="h-3.5 w-3.5" />}>
            <div className="font-medium text-indigo-700 dark:text-indigo-300">{content || title}</div>
          </WorkLogBlock>
        );
      }
      return (
        <WorkLogBlock title="Approval required" tone="warning">
          <div className="font-medium text-warning">{title}</div>
        </WorkLogBlock>
      );
    case "error":
      return (
        <WorkLogBlock title="Error" tone="error">
          <pre className="whitespace-pre-wrap break-all font-mono text-[12px] text-destructive">{content || title}</pre>
        </WorkLogBlock>
      );
    case "status": {
      const error = metadataText(event.metadata, "error");
      return (
        <WorkLogBlock title="Status" muted>
          <div className="text-muted-foreground">
            {content || title}
            {error && <span className="text-destructive">: {redactSecrets(error)}</span>}
          </div>
        </WorkLogBlock>
      );
    }
    default:
      return (
        <WorkLogBlock title={title} muted>
          <div className="font-medium text-foreground">{title}</div>
          {content && <pre className="mt-1 max-h-40 overflow-y-auto whitespace-pre-wrap break-all font-mono text-[12px] text-muted-foreground">{content}</pre>}
        </WorkLogBlock>
      );
  }
}

function WorkLogBlock({
  title,
  children,
  icon,
  tone,
  muted,
}: {
  title: string;
  children: ReactNode;
  icon?: ReactNode;
  tone?: "assistant" | "thinking" | "plan" | "command" | "result" | "read" | "file" | "warning" | "error";
  muted?: boolean;
}) {
  return (
    <div className={cn(
      "min-w-0 max-w-full overflow-hidden rounded-md border px-3 py-2 text-[13px] leading-5",
      tone === "assistant" && "border-sky-200 bg-sky-50 text-sky-950 dark:border-sky-900/60 dark:bg-sky-950/30 dark:text-sky-100",
      tone === "thinking" && "border-violet-200 bg-violet-50 text-violet-950 dark:border-violet-900/60 dark:bg-violet-950/30 dark:text-violet-100",
      tone === "plan" && "border-indigo-200 bg-indigo-50 text-indigo-950 dark:border-indigo-900/60 dark:bg-indigo-950/30 dark:text-indigo-100",
      tone === "command" && "border-blue-200 bg-blue-50 dark:border-blue-900/60 dark:bg-blue-950/30",
      tone === "result" && "border-emerald-200 bg-emerald-50 dark:border-emerald-900/60 dark:bg-emerald-950/25",
      tone === "read" && "border-cyan-200 bg-cyan-50 dark:border-cyan-900/60 dark:bg-cyan-950/25",
      tone === "file" && "border-amber-200 bg-amber-50 dark:border-amber-900/60 dark:bg-amber-950/25",
      tone === "warning" && "border-warning/40 bg-warning/15",
      tone === "error" && "border-destructive/40 bg-destructive/10",
      !tone && muted && "border-border/50 bg-muted/20",
      !tone && !muted && "border-border/70 bg-card/50",
    )}>
      {title && (
        <div className={cn(
          "mb-1.5 flex min-w-0 items-center gap-1.5 text-xs font-semibold",
          tone === "assistant" && "text-sky-700 dark:text-sky-300",
          tone === "thinking" && "text-violet-700 dark:text-violet-300",
          tone === "plan" && "text-indigo-700 dark:text-indigo-300",
          tone === "command" && "text-blue-700 dark:text-blue-300",
          tone === "result" && "text-emerald-700 dark:text-emerald-300",
          tone === "read" && "text-cyan-700 dark:text-cyan-300",
          tone === "file" && "text-amber-700 dark:text-amber-300",
          tone === "warning" && "text-warning",
          tone === "error" && "text-destructive",
          (!tone || muted) && "text-muted-foreground",
        )}>
          {icon}
          <span className="min-w-0 truncate">{title}</span>
        </div>
      )}
      <div className="min-w-0 max-w-full overflow-hidden">{children}</div>
    </div>
  );
}

function CommandBlock({
  command,
  description,
  result,
  fallbackInput,
  approvalRequestLine,
  approvalResponseLine,
}: {
  command: string;
  description?: string;
  result?: string;
  fallbackInput?: unknown;
  approvalRequestLine?: TaskTraceLine;
  approvalResponseLine?: TaskTraceLine;
}) {
  return (
    <WorkLogBlock title={classifyCommand(command)} tone="command" icon={<Terminal className="h-3.5 w-3.5" />}>
      {description && <div className="mb-1 break-words text-muted-foreground">{description}</div>}
      <pre className="max-w-full whitespace-pre-wrap break-all rounded bg-blue-100/70 px-2 py-1.5 font-mono text-[12px] text-blue-950 dark:bg-blue-950/50 dark:text-blue-100">
        {commandLabel(command, fallbackInput)}
      </pre>
      <InlineApprovalStatus requestLine={approvalRequestLine} responseLine={approvalResponseLine} />
      {result !== undefined && <CommandResult command={command} result={result} />}
    </WorkLogBlock>
  );
}

export const __taskTraceOutputTestUtils = {
  buildWorkItems,
  commandFromInput,
  commandLabel,
  isApprovalTraceChannel,
};

function CommandResult({ command, result }: { command: string; result: string }) {
  const parsed = unwrapJson(redactSecrets(result));
  const summary = summarizeResultForCommand(command, parsed);
  const raw = typeof parsed === "string" ? parsed : compactJson(parsed);
  return (
    <div className="mt-2 min-w-0 max-w-full overflow-hidden rounded border border-emerald-200 bg-emerald-50/70 px-2 py-1.5 dark:border-emerald-900/60 dark:bg-emerald-950/20">
      {summary && <div className="break-words text-xs font-semibold text-emerald-800 dark:text-emerald-300">Completed: {redactSecrets(summary)}</div>}
      {typeof parsed === "string" && !summary && (
        <pre className="mt-1 max-h-52 overflow-y-auto overflow-x-hidden whitespace-pre-wrap break-all font-mono text-[12px] text-muted-foreground">
          {parsed || "(no output)"}
        </pre>
      )}
      {raw && <ExpandableRaw label={summary ? "Raw output" : "Raw result"} value={redactSecrets(raw)} defaultOpen={!summary && typeof parsed !== "string"} />}
    </div>
  );
}

function todoCount(input: unknown): number {
  if (!input || typeof input !== "object") return 0;
  const todos = (input as Record<string, unknown>).todos;
  return Array.isArray(todos) ? todos.length : 0;
}

function contextActivityInfo(item: TraceWorkItem): { kind: "read" | "search" | "todo"; label: string; subject: string; summary: string } {
  const input = item.event?.metadata?.input;
  const title = item.event?.title ?? "";
  const kind = workItemToolKind(item) ?? "read";
  const result = item.pairedEvent?.content;
  if (kind === "todo") {
    const count = todoCount(input);
    return {
      kind,
      label: "Todo",
      subject: "Updated todo list",
      summary: count > 0 ? `${count} item${count === 1 ? "" : "s"}` : "Task state updated",
    };
  }

  const subject = inputText(input, ["file_path", "path", "pattern"]) || inputText(input, ["query", "pattern", "glob"]) || title;
  return {
    kind,
    label: kind === "read" ? "Read" : "Search",
    subject,
    summary: summarizeToolResult(kind, result),
  };
}

function contextActivityTitle(items: TraceWorkItem[]): string {
  const counts = items.reduce((acc, item) => {
    const kind = workItemToolKind(item);
    if (kind) acc[kind] += 1;
    return acc;
  }, { read: 0, search: 0, todo: 0 });
  const parts = [
    counts.read > 0 && `${counts.read} read`,
    counts.search > 0 && `${counts.search} search`,
    counts.todo > 0 && `${counts.todo} todo`,
  ].filter(Boolean);
  return `Context activity${parts.length > 0 ? `: ${parts.join(", ")}` : ""}`;
}

function ContextActivityBlock({ items }: { items: TraceWorkItem[] }) {
  const preview = items.slice(0, 4).map((item) => contextActivityInfo(item).subject).filter(Boolean);
  const hidden = Math.max(0, items.length - preview.length);
  return (
    <details className="rounded-md border border-cyan-200/70 bg-cyan-50/60 text-[12px] dark:border-cyan-900/50 dark:bg-cyan-950/20">
      <summary className="flex cursor-pointer list-none items-center gap-2 px-2.5 py-1.5 text-cyan-900 hover:bg-cyan-100/60 dark:text-cyan-200 dark:hover:bg-cyan-950/40">
        <FileText className="h-3.5 w-3.5 shrink-0" />
        <span className="shrink-0 font-medium">{contextActivityTitle(items)}</span>
        {preview.length > 0 && (
          <span className="min-w-0 truncate font-mono text-[11px] text-cyan-800/80 dark:text-cyan-300/80">
            {preview.join(" · ")}{hidden > 0 ? ` · +${hidden}` : ""}
          </span>
        )}
      </summary>
      <div className="border-t border-cyan-200/70 px-2.5 py-1.5 dark:border-cyan-900/50">
        <div className="space-y-1">
          {items.map((item) => {
            const info = contextActivityInfo(item);
            return (
              <div key={`${item.line.run_id}:${item.line.seq}`} className="grid min-w-0 grid-cols-[3.25rem_minmax(0,1fr)] gap-2">
                <span className="text-muted-foreground">{info.label}</span>
                <span className="min-w-0 truncate">
                  <span className="font-mono text-cyan-950 dark:text-cyan-100">{info.subject}</span>
                  {info.summary && <span className="ml-2 text-muted-foreground">{info.summary}</span>}
                </span>
              </div>
            );
          })}
        </div>
      </div>
    </details>
  );
}

interface DiffHunk {
  label: string;
  oldText: string;
  newText: string;
}

interface DiffRow {
  kind: "context" | "remove" | "add";
  text: string;
  oldLine?: number;
  newLine?: number;
}

function diffHunksFromInput(input: unknown): DiffHunk[] {
  if (!input || typeof input !== "object") return [];
  const record = input as Record<string, unknown>;
  const edits = record.edits;
  if (Array.isArray(edits)) {
    return edits.map((edit, index) => {
      const item = edit && typeof edit === "object" ? edit as Record<string, unknown> : {};
      return {
        label: `Edit ${index + 1}`,
        oldText: typeof item.old_string === "string" ? item.old_string : "",
        newText: typeof item.new_string === "string" ? item.new_string : "",
      };
    }).filter((hunk) => hunk.oldText || hunk.newText);
  }
  const oldText = inputText(input, ["old_string"]);
  const newText = inputText(input, ["new_string", "content"]);
  if (!oldText && !newText) return [];
  return [{ label: oldText ? "Change" : "Added content", oldText, newText }];
}

function buildDiffRows(oldLines: string[], newLines: string[]): DiffRow[] {
  if (oldLines.length === 0) return newLines.map((text, index) => ({ kind: "add", text, newLine: index + 1 }));
  if (newLines.length === 0) return oldLines.map((text, index) => ({ kind: "remove", text, oldLine: index + 1 }));

  if (oldLines.length * newLines.length > 25000) {
    return [
      ...oldLines.map((text, index) => ({ kind: "remove" as const, text, oldLine: index + 1 })),
      ...newLines.map((text, index) => ({ kind: "add" as const, text, newLine: index + 1 })),
    ];
  }

  const dp: number[][] = Array.from({ length: oldLines.length + 1 }, () => Array(newLines.length + 1).fill(0));
  for (let i = oldLines.length - 1; i >= 0; i -= 1) {
    for (let j = newLines.length - 1; j >= 0; j -= 1) {
      dp[i]![j] = oldLines[i] === newLines[j] ? dp[i + 1]![j + 1]! + 1 : Math.max(dp[i + 1]![j]!, dp[i]![j + 1]!);
    }
  }

  const rows: DiffRow[] = [];
  let i = 0;
  let j = 0;
  while (i < oldLines.length && j < newLines.length) {
    if (oldLines[i] === newLines[j]) {
      rows.push({ kind: "context", text: oldLines[i]!, oldLine: i + 1, newLine: j + 1 });
      i += 1;
      j += 1;
      continue;
    }
    if (dp[i + 1]![j]! >= dp[i]![j + 1]!) {
      rows.push({ kind: "remove", text: oldLines[i]!, oldLine: i + 1 });
      i += 1;
    } else {
      rows.push({ kind: "add", text: newLines[j]!, newLine: j + 1 });
      j += 1;
    }
  }
  while (i < oldLines.length) {
    rows.push({ kind: "remove", text: oldLines[i]!, oldLine: i + 1 });
    i += 1;
  }
  while (j < newLines.length) {
    rows.push({ kind: "add", text: newLines[j]!, newLine: j + 1 });
    j += 1;
  }
  return rows;
}

function trimDiffRows(rows: DiffRow[], max = 120): { rows: DiffRow[]; truncated: boolean } {
  if (rows.length <= max) return { rows, truncated: false };
  return { rows: rows.slice(0, max), truncated: true };
}

function diffStats(rows: DiffRow[]): { added: number; removed: number } {
  return rows.reduce((stats, row) => {
    if (row.kind === "add") stats.added += 1;
    if (row.kind === "remove") stats.removed += 1;
    return stats;
  }, { added: 0, removed: 0 });
}

// --- Codex unified diff helpers ---

interface FileChangeEntry {
  path: string;
  diff: string;
}

function extractFileChanges(metadata: Record<string, unknown> | undefined): FileChangeEntry[] {
  if (!metadata) return [];
  const changes = metadata.changes;
  if (!Array.isArray(changes)) return [];
  return changes
    .filter((c): c is Record<string, unknown> => c != null && typeof c === "object")
    .map((c) => ({
      path: typeof c.path === "string" ? c.path : "",
      diff: typeof c.diff === "string" ? c.diff : "",
    }))
    .filter((c) => c.path || c.diff);
}

/** Parse a unified diff string into DiffRow[] for rendering. */
function parseUnifiedDiffRows(diff: string): DiffRow[] {
  if (!diff) return [];
  const rows: DiffRow[] = [];
  let oldLine = 1;
  let newLine = 1;
  for (const line of diff.split("\n")) {
    if (line.startsWith("@@")) {
      // Parse hunk header: @@ -a,b +c,d @@
      const match = /@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(line);
      if (match) {
        oldLine = parseInt(match[1]!, 10);
        newLine = parseInt(match[2]!, 10);
      }
      continue;
    }
    if (line.startsWith("+")) {
      rows.push({ kind: "add", text: line.slice(1), newLine: newLine++ });
    } else if (line.startsWith("-")) {
      rows.push({ kind: "remove", text: line.slice(1), oldLine: oldLine++ });
    } else {
      // context line (starts with space or is plain text)
      const text = line.startsWith(" ") ? line.slice(1) : line;
      if (text || rows.length > 0) {
        rows.push({ kind: "context", text, oldLine: oldLine++, newLine: newLine++ });
      }
    }
  }
  return rows;
}

function UnifiedDiffPreview({ rows: allRows }: { rows: DiffRow[] }) {
  const { rows, truncated } = trimDiffRows(allRows);
  const stats = diffStats(allRows);
  return (
    <div className="mt-2 max-w-full overflow-hidden rounded-lg border border-zinc-300 bg-zinc-50 shadow-sm dark:border-zinc-700 dark:bg-zinc-950/70">
      <div className="flex items-center gap-2 border-b border-zinc-300 bg-zinc-100 px-2.5 py-1.5 text-xs text-zinc-950 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100">
        <span className="shrink-0 rounded bg-zinc-200 px-1.5 py-0.5 font-mono text-[11px] font-semibold text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300">diff</span>
        <span className="ml-auto shrink-0 font-mono text-[11px] text-emerald-700 dark:text-emerald-300">+{stats.added}</span>
        <span className="shrink-0 font-mono text-[11px] text-red-700 dark:text-red-300">-{stats.removed}</span>
      </div>
      <div className="max-h-96 overflow-y-auto overflow-x-hidden font-mono text-[12.5px] leading-[1.65]">
        {renderDiffRows(rows)}
        {truncated && (
          <div className="border-t border-dashed border-border px-3 py-1.5 text-center text-[11px] text-muted-foreground">
            … {allRows.length - rows.length} more lines
          </div>
        )}
      </div>
    </div>
  );
}

function DiffPreview({ hunks }: { hunks: DiffHunk[] }) {
  if (hunks.length === 0) return null;
  return (
    <div className="mt-2 max-w-full overflow-hidden rounded-lg border border-zinc-300 bg-zinc-50 shadow-sm dark:border-zinc-700 dark:bg-zinc-950/70">
      {hunks.map((hunk, index) => {
        const rows = buildDiffRows(
          hunk.oldText ? redactSecrets(hunk.oldText).split(/\r?\n/) : [],
          hunk.newText ? redactSecrets(hunk.newText).split(/\r?\n/) : [],
        );
        const stats = diffStats(rows);
        return (
          <div key={`${hunk.label}-${index}`} className={cn(index > 0 && "border-t border-border")}>
            <div className="flex items-center gap-2 border-b border-zinc-300 bg-zinc-100 px-2.5 py-1.5 text-xs text-zinc-950 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100">
              <span className="shrink-0 rounded bg-zinc-200 px-1.5 py-0.5 font-mono text-[11px] font-semibold text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300">diff</span>
              <span className="min-w-0 truncate font-mono font-semibold">{hunk.label}</span>
              <span className="ml-auto shrink-0 font-mono text-[11px] text-emerald-700 dark:text-emerald-300">+{stats.added}</span>
              <span className="shrink-0 font-mono text-[11px] text-red-700 dark:text-red-300">-{stats.removed}</span>
            </div>
            <div className="max-h-96 overflow-y-auto overflow-x-hidden font-mono text-[12.5px] leading-[1.65]">
              {renderDiffRows(rows)}
            </div>
          </div>
        );
      })}
    </div>
  );
}

function renderDiffRows(rows: DiffRow[]): ReactNode {
  const trimmed = trimDiffRows(rows);
  return (
    <>
      {trimmed.rows.map((row, index) => {
        return (
          <div
            key={`${index}-${row.kind}-${row.text}`}
            className={cn(
              "grid min-w-0 grid-cols-[2.25rem_2.25rem_1.25rem_minmax(0,1fr)] gap-1 border-b border-border/35 px-2 py-0.5 last:border-b-0",
              row.kind === "add" && "bg-emerald-50/90 text-emerald-950 dark:bg-emerald-950/30 dark:text-emerald-100",
              row.kind === "remove" && "bg-red-50/90 text-red-950 dark:bg-red-950/30 dark:text-red-100",
              row.kind === "context" && "bg-transparent text-zinc-700 dark:text-zinc-300",
            )}
          >
            <span className="select-none text-right text-[11px] text-muted-foreground/70">{row.oldLine ?? ""}</span>
            <span className="select-none text-right text-[11px] text-muted-foreground/70">{row.newLine ?? ""}</span>
            <span className={cn(
              "select-none text-center text-sm font-semibold",
              row.kind === "add" && "text-emerald-700 dark:text-emerald-300",
              row.kind === "remove" && "text-red-700 dark:text-red-300",
              row.kind === "context" && "text-muted-foreground/60",
            )}>
              {row.kind === "add" ? "+" : row.kind === "remove" ? "-" : " "}
            </span>
            <span className={cn(
              "min-w-0 whitespace-pre-wrap break-all",
              row.kind === "context" ? "font-normal" : "font-medium",
            )}>
              {row.text || " "}
            </span>
          </div>
        );
      })}
      {trimmed.truncated && (
        <div className="px-2 py-1 text-xs text-muted-foreground">
          Diff preview truncated. Open raw detail for the full content.
        </div>
      )}
    </>
  );
}

function FileChangeBlock({
  kind,
  title,
  input,
  result,
}: {
  kind: "write" | "edit";
  title: string;
  input: unknown;
  result?: string;
}) {
  const path = inputText(input, ["file_path", "path", "notebook_path"]);
  const diffHunks = diffHunksFromInput(input);
  const summary = summarizeToolResult(kind, result);
  return (
    <WorkLogBlock title={kind === "write" ? "Write file" : "Edit file"} tone="file" icon={<Code2 className="h-3.5 w-3.5" />}>
      <div className="space-y-1">
        <div className="break-all font-mono text-[12.5px] font-semibold text-amber-950 dark:text-amber-100">{path || title}</div>
        {summary && <div className="text-xs font-medium text-amber-800 dark:text-amber-300">{summary}</div>}
      </div>
      <DiffPreview hunks={diffHunks} />
      {result && <ExpandableRaw label="Tool result" value={redactSecrets(String(unwrapJson(result)))} />}
    </WorkLogBlock>
  );
}

function ExpandableRaw({ label, value, defaultOpen = false }: { label: string; value: string; defaultOpen?: boolean }) {
  if (!value) return null;
  return (
    <details open={defaultOpen} className="mt-2">
      <summary className="cursor-pointer text-xs text-muted-foreground hover:text-foreground">{label}</summary>
      <pre className="mt-1 max-h-56 overflow-y-auto overflow-x-hidden whitespace-pre-wrap break-all rounded bg-muted/40 px-2 py-1.5 font-mono text-[12px] text-muted-foreground">
        {value}
      </pre>
    </details>
  );
}

export function TaskTraceOutput({ task, defaultOpen = false, compact = false, fill = false }: TaskTraceOutputProps) {
  const [open, setOpen] = useState(defaultOpen);
  const [showRaw, setShowRaw] = useState(false);
  const [healthPort, setHealthPort] = useState<number | null>(null);
  const [runId, setRunId] = useState("");
  const [lines, setLines] = useState<TaskTraceLine[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [connected, setConnected] = useState(false);
  const [streamNonce, setStreamNonce] = useState(0);
  const [interactions, setInteractions] = useState<TaskInteraction[]>([]);
  const scrollRef = useRef<HTMLDivElement>(null);

  const fetchPendingInteractions = useCallback(() => {
    api.listTaskInteractions(task.id, "pending").then(setInteractions).catch(console.error);
  }, [task.id]);

  useEffect(() => {
    fetchPendingInteractions();
    const interval = setInterval(() => {
      if (document.visibilityState === "visible") fetchPendingInteractions();
    }, 30_000);
    return () => clearInterval(interval);
  }, [fetchPendingInteractions]);

  useWSEvent("interaction:created", (payload: unknown) => {
    const p = payload as { task_id?: string };
    if (p.task_id === task.id) fetchPendingInteractions();
  });

  useWSEvent("interaction:resolved", (payload: unknown) => {
    const p = payload as { task_id?: string };
    if (p.task_id === task.id) fetchPendingInteractions();
  });

  useEffect(() => {
    let cancelled = false;
    setHealthPort(null);
    setError(null);

    if (!task.runtime_id) {
      setError("No runtime is attached to this task.");
      setLoading(false);
      return;
    }

    api.listRuntimes().then((runtimes) => {
      if (cancelled) return;
      const rt = runtimes.find((item) => item.id === task.runtime_id);
      const port = metadataHealthPort(rt?.metadata);
      if (!port) {
        setError("Local daemon trace port is not available. Restart the daemon so runtime metadata is refreshed.");
        setLoading(false);
        return;
      }
      setHealthPort(port);
    }).catch((e) => {
      if (!cancelled) {
        setError(e instanceof Error ? e.message : "Failed to resolve local runtime.");
        setLoading(false);
      }
    });

    return () => {
      cancelled = true;
    };
  }, [task.runtime_id]);

  useEffect(() => {
    if (!healthPort) return;
    let cancelled = false;
    setLines([]);
    setRunId("");
    setConnected(false);
    setLoading(true);

    let source: EventSource | null = null;

    const connect = (initialRunId?: string, afterSeq?: number) => {
      if (cancelled) return;
      source = new EventSource(api.getLocalTaskTraceStreamUrl(healthPort, task.id, {
        run_id: initialRunId || undefined,
        after_seq: afterSeq,
      }));
      source.addEventListener("open", () => {
        setConnected(true);
        setError(null);
      });
      source.addEventListener("ready", (event) => {
        try {
          const payload = JSON.parse((event as MessageEvent).data) as { run_id?: string };
          const nextRunId = payload.run_id ?? "";
          if (nextRunId) setRunId(nextRunId);
        } catch {
          // Ignore malformed ready events.
        }
        setLoading(false);
      });
      source.addEventListener("trace", (event) => {
        try {
          const line = JSON.parse((event as MessageEvent).data) as TaskTraceLine;
          setRunId(line.run_id);
          setLines((prev) => {
            if (prev.some((item) => item.run_id === line.run_id && item.seq === line.seq)) {
              return prev;
            }
            return [...prev, line].sort((a, b) => a.seq - b.seq);
          });
        } catch {
          // Ignore malformed trace events.
        }
        setLoading(false);
      });
      source.addEventListener("error", (event) => {
        setConnected(false);
        setLoading(false);
        const data = (event as MessageEvent).data;
        if (typeof data === "string" && data) {
          try {
            const payload = JSON.parse(data) as { error?: string };
            setError(payload.error ?? "Local trace stream disconnected.");
            return;
          } catch {
            // Fall through to generic message.
          }
        }
        setError("Local trace stream disconnected.");
      });
    };

    api.getLocalTaskTrace(healthPort, task.id)
      .then((trace) => {
        if (cancelled) return;
        const history = [...trace.lines].sort((a, b) => a.seq - b.seq);
        setRunId(trace.run_id ?? history.at(-1)?.run_id ?? "");
        setLines(history);
        connect(trace.run_id || undefined, history.at(-1)?.seq);
      })
      .catch((e) => {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : "Failed to load local task trace.");
        connect();
      });

    return () => {
      cancelled = true;
      source?.close();
    };
  }, [healthPort, streamNonce, task.id]);

  const visibleLines = useMemo(() => {
    if (showRaw) return lines;
    return lines.filter((line) => (
      line.channel === "display_event" ||
      line.channel === "approval_request" ||
      line.channel === "approval_response"
    ));
  }, [lines, showRaw]);

  const workItems = useMemo(() => buildWorkItems(visibleLines, showRaw), [visibleLines, showRaw]);
  const latestVisibleLineSeq = visibleLines[visibleLines.length - 1]?.seq ?? -1;
  const latestInteractionStamp = interactions.length > 0
    ? `${interactions[interactions.length - 1]?.id ?? ""}:${interactions[interactions.length - 1]?.status ?? ""}:${interactions[interactions.length - 1]?.responded_at ?? ""}:${interactions[interactions.length - 1]?.created_at ?? ""}`
    : "";

  useEffect(() => {
    const scrollToBottom = () => {
      const el = scrollRef.current;
      if (el) {
        el.scrollTop = el.scrollHeight;
      }
    };
    // Scroll immediately for the common case
    scrollToBottom();
    // Also schedule after a frame to handle cases where layout hasn't
    // settled yet (e.g. CollapsibleContent animation, initial mount)
    const raf = requestAnimationFrame(scrollToBottom);
    return () => cancelAnimationFrame(raf);
  }, [latestVisibleLineSeq, latestInteractionStamp, showRaw, open]);

  const planPhase = useMemo(() => latestPlanPhase(lines), [lines]);
  const planBadge = planPhase
    ? planPhase === "executing" || planPhase === "rejected" || planPhase === "expired" || planPhase === "cancelled"
      ? planPhase
      : "plan"
    : "";

  return (
    <Collapsible open={open} onOpenChange={setOpen} className={cn("max-w-full overflow-hidden", fill && "flex h-full min-h-0 flex-col")}>
      <div className={cn("shrink-0 border-info/10 px-3 py-2", compact ? "" : "border-t")}>
        <div className="flex min-w-0 items-center gap-2">
          <CollapsibleTrigger className="flex min-w-0 flex-1 items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors">
            {open ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
            <Radio className={cn("h-3 w-3", connected ? "text-success" : "text-muted-foreground")} />
            <span className="shrink-0 font-medium text-foreground">Agent stream</span>
            {planBadge && <span className="rounded bg-info/10 px-1.5 py-0.5 text-info">{planBadge}</span>}
            <span className="shrink-0 text-muted-foreground">{workItems.length}/{lines.length}</span>
            {interactions.length > 0 && (
              <span className={cn(
                "rounded px-1.5 py-0.5",
                interactions.some((interaction) => interaction.type === "plan_approval")
                  ? "bg-indigo-500/10 text-indigo-500"
                  : "bg-warning/10 text-warning",
              )}>
                {interactions.some((interaction) => interaction.type === "plan_approval") ? "plan decision" : `${interactions.length} approval`}
              </span>
            )}
            {runId && <span className="min-w-0 truncate text-muted-foreground/70">{runId}</span>}
          </CollapsibleTrigger>
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setShowRaw((value) => !value);
            }}
            className={cn(
              "rounded px-1.5 py-0.5 text-[11px] transition-colors",
              showRaw ? "bg-accent text-foreground" : "text-muted-foreground hover:text-foreground hover:bg-accent/50",
            )}
          >
            Raw
          </button>
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setStreamNonce((value) => value + 1);
            }}
            className="rounded p-1 text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors"
            title="Refresh stream"
          >
            {loading ? <Loader2 className="h-3 w-3 animate-spin" /> : <RefreshCw className="h-3 w-3" />}
          </button>
        </div>
        {error && <div className="mt-1 text-[11px] text-muted-foreground">{error}</div>}
      </div>
      <CollapsibleContent className={cn(fill && "min-h-0 flex-1 flex flex-col overflow-hidden")}>
        <div
          ref={scrollRef}
          className={cn(
            "max-w-full overflow-y-auto overflow-x-hidden border-t border-info/10 bg-background/60 px-3 py-2",
            fill ? "flex-1 min-h-0" : compact ? "max-h-[55vh]" : "max-h-72",
          )}
        >
          {loading && lines.length === 0 ? (
            <div className="flex items-center gap-2 py-2 text-xs text-muted-foreground">
              <Loader2 className="h-3 w-3 animate-spin" />
              Loading stream...
            </div>
          ) : (
            <div className="space-y-2">
              {workItems.length === 0 ? (
                <div className="py-2 text-xs text-muted-foreground">
                  No local trace lines yet.
                </div>
              ) : (
                workItems.map((item) => (
                  item.line.channel === "display_event" && !showRaw ? (
                    <DisplayTraceLine key={`${item.line.run_id}:${item.line.seq}`} {...item} />
                  ) : isApprovalTraceChannel(item.line.channel) && !showRaw ? (
                    <ApprovalTraceLine key={`${item.line.run_id}:${item.line.seq}`} line={item.line} />
                  ) : (
                    <div key={`${item.line.run_id}:${item.line.seq}`} className="grid min-w-0 grid-cols-[4.5rem_minmax(0,1fr)] gap-2 text-xs leading-relaxed">
                      <span className="truncate text-muted-foreground">{lineLabel(item.line.channel)}</span>
                      <pre className={cn("min-w-0 whitespace-pre-wrap break-all font-mono", channelClass(item.line.channel))}>
                        {lineText(item.line)}
                      </pre>
                    </div>
                  )
                ))
              )}
              {interactions.map((interaction) => (
                <TraceInteractionCard
                  key={interaction.id}
                  interaction={interaction}
                  taskId={task.id}
                  onResolved={fetchPendingInteractions}
                />
              ))}
            </div>
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

function latestPlanPhase(lines: TaskTraceLine[]): string {
  for (let i = lines.length - 1; i >= 0; i -= 1) {
    const line = lines[i]!;
    if (line.channel !== "display_event") continue;
    const event = parseDisplayEvent(line);
    if (event?.type !== "plan_stage") continue;
    const stage = event.metadata?.stage;
    if (typeof stage === "string" && stage.trim()) return stage;
  }
  return "";
}

function TraceInteractionCard({
  interaction,
  taskId,
  onResolved,
}: {
  interaction: TaskInteraction;
  taskId: string;
  onResolved: () => void;
}) {
  const [responding, setResponding] = useState<string | null>(null);
  const [revisionMessage, setRevisionMessage] = useState("");
  const isPlanApproval = interaction.type === "plan_approval";
  const isUserInputRequest = interaction.type === "user_input_request";
  const options =
    interaction.options.length > 0
      ? interaction.options
      : [
          { id: "allow", label: "Allow" },
          { id: "deny", label: "Deny" },
        ];

  const handleRespond = async (option: string) => {
    if (responding) return;
    setResponding(option);
    try {
      const responseMessage = isPlanApproval && /revise|keep_planning/i.test(option) ? revisionMessage : undefined;
      await api.respondInteraction(taskId, interaction.id, option, responseMessage);
      onResolved();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to respond");
      setResponding(null);
    }
  };

  return (
    <div className={cn(
      "mb-2 rounded border p-2 text-xs",
      isPlanApproval
        ? "border-indigo-300/50 bg-indigo-500/10 dark:border-indigo-800/60 dark:bg-indigo-950/30"
        : isUserInputRequest
          ? "border-info/30 bg-info/10"
          : "border-warning/30 bg-warning/10",
    )}>
      <div className="flex items-start gap-2">
        {isPlanApproval ? (
          <ClipboardList className="mt-0.5 h-4 w-4 shrink-0 text-indigo-500" />
        ) : isUserInputRequest ? (
          <Radio className="mt-0.5 h-4 w-4 shrink-0 text-info" />
        ) : (
          <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
        )}
        <div className="min-w-0 flex-1">
          <div className="font-medium text-foreground">{interaction.title}</div>
          {interaction.detail && (
            isPlanApproval ? (
              <div className="mt-2 max-h-80 overflow-auto rounded border border-info/20 bg-background/70 px-2 py-1.5">
                <Markdown mode="minimal" className="text-[13px] leading-5">
                  {redactSecrets(interaction.detail)}
                </Markdown>
              </div>
            ) : isUserInputRequest ? (
              <div className="mt-2 max-h-80 overflow-auto rounded border border-info/20 bg-background/70 px-2 py-1.5">
                <Markdown mode="minimal" className="text-[13px] leading-5">
                  {redactSecrets(interaction.detail)}
                </Markdown>
              </div>
            ) : (
              <pre className="mt-1 max-h-28 overflow-auto whitespace-pre-wrap break-words font-mono text-[11px] text-muted-foreground">
                {redactSecrets(interaction.detail)}
              </pre>
            )
          )}
          {isPlanApproval && (
            <textarea
              value={revisionMessage}
              onChange={(event) => setRevisionMessage(event.target.value)}
              placeholder="Optional revision notes for Claude"
              className="mt-2 min-h-16 w-full resize-y rounded border border-border bg-background px-2 py-1.5 text-xs text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-info"
              disabled={responding !== null}
            />
          )}
          <div className="mt-2 flex flex-wrap items-center gap-1.5">
            {options.map((option) => {
              const destructive = /deny|reject|cancel|stop/i.test(`${option.id} ${option.label}`);
              const revise = /revise|keep planning/i.test(`${option.id} ${option.label}`);
              return (
                <button
                  key={option.id}
                  type="button"
                  onClick={() => handleRespond(option.id)}
                  disabled={responding !== null}
                  className={
                    isUserInputRequest
                      ? "flex items-center gap-1 rounded bg-info/10 px-2 py-0.5 font-medium text-info transition-colors hover:bg-info/20 disabled:opacity-50"
                      : destructive
                      ? "flex items-center gap-1 rounded bg-destructive/10 px-2 py-0.5 font-medium text-destructive transition-colors hover:bg-destructive/20 disabled:opacity-50"
                      : revise
                        ? "flex items-center gap-1 rounded bg-warning/10 px-2 py-0.5 font-medium text-warning transition-colors hover:bg-warning/20 disabled:opacity-50"
                      : "flex items-center gap-1 rounded bg-success/10 px-2 py-0.5 font-medium text-success transition-colors hover:bg-success/20 disabled:opacity-50"
                  }
                >
                  {responding === option.id ? (
                    <Loader2 className="h-3 w-3 animate-spin" />
                  ) : destructive ? (
                    <X className="h-3 w-3" />
                  ) : (
                    <Check className="h-3 w-3" />
                  )}
                  {option.label}
                </button>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
