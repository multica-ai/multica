// FIR-4000 — evidence-first Production prompt explorer.
//
// "This run" is byte-exact recorded evidence. Pending and Difference views
// apply only the versioned proposal fields we can prove from the stored
// snapshot, and say so explicitly; a new byte-exact prompt exists only after a
// run is captured.

"use client";

import { useMemo, useState, type ComponentType, type ReactNode } from "react";
import { Check, Copy, FileCode2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type {
  Agent,
  AgentContextChangeRequest,
  AgentContextSnapshot,
  AgentRuntime,
} from "@multica/core/types";
import { api } from "@multica/core/api";
import { estimateTokensFromLength } from "@multica/cerebro-profile/core";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  getAgentPromptSnapshot,
  listAgentPromptSnapshots,
  type PromptSnapshot,
  type PromptSnapshotRef,
} from "../api";

export interface AgentPromptSnapshotTabExtension {
  id: string;
  labelKey: "production_prompt";
  icon: ComponentType<{ className?: string }>;
  render: (context: {
    agent: Agent;
    runtimes: AgentRuntime[];
    canEdit: boolean;
  }) => ReactNode;
}

export function createAgentPromptSnapshotTabs(): AgentPromptSnapshotTabExtension[] {
  return [
    {
      id: "production_prompt",
      labelKey: "production_prompt",
      icon: FileCode2,
      render: ({ agent }) => <CerebroAgentPromptSnapshotTab agent={agent} />,
    },
  ];
}

type ViewMode = "current" | "after_proposal" | "diff";

export interface PromptPart {
  id: "role" | "tools" | "skills" | "shared" | "case" | "controls";
  label: string;
  content: string;
  delivery: string;
  byteSize: number;
  state?: string;
}

const listKey = (agentId: string) =>
  ["cerebro", "agent-prompt-snapshots", agentId] as const;
const detailKey = (agentId: string, taskId: string) =>
  ["cerebro", "agent-prompt-snapshot", agentId, taskId] as const;
const proposalsKey = (agentId: string) =>
  ["cerebro", "agent-context", agentId, "change-requests"] as const;

const shortSha = (sha: string) => (sha ? sha.slice(0, 10) : "—");

function formatRunDate(iso: string | null | undefined): string {
  if (!iso) return "—";
  const value = new Date(iso);
  if (Number.isNaN(value.getTime())) return "—";
  return value.toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function deliveryLabel(delivery: string): string {
  switch (delivery) {
    case "system_prompt":
      return "Native system prompt";
    case "workdir_file":
      return "Instruction file";
    case "user_prompt":
      return "User message";
    case "daemon":
      return "Applied by Multica";
    default:
      return delivery || "Not delivered";
  }
}

function runtimeConfig(
  snapshot: AgentContextSnapshot | Agent,
): Record<string, unknown> {
  const value = snapshot.runtime_config;
  return value != null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function settingText(value: unknown, fallback: string): string {
  if (typeof value === "string" && value !== "") return value;
  if (typeof value === "number" && value > 0) return String(value);
  return fallback;
}

function controlText(snapshot: AgentContextSnapshot | Agent): string {
  const config = runtimeConfig(snapshot);
  return [
    `System prompt: ${settingText(config.system_prompt_mode, "Engine default")}`,
    `Model: ${settingText(snapshot.model, "Engine default")}`,
    `Thinking time: ${settingText(snapshot.thinking_level, "Engine default")}`,
    `Speed: ${settingText(config.speed_mode, "Standard")}`,
    `Stop after: ${settingText(config.max_turns, "Run mode default")}`,
    `Give up after: ${settingText(config.timeout_minutes, "Run mode default")}`,
  ].join("\n");
}

interface TextRange {
  start: number;
  end: number;
  content: string;
}

function sectionRange(
  content: string,
  heading: RegExp,
  nextHeading: RegExp,
): TextRange | null {
  const startMatch = heading.exec(content);
  if (!startMatch || startMatch.index == null) return null;
  const start = startMatch.index;
  const tail = content.slice(start + startMatch[0].length);
  const next = nextHeading.exec(tail);
  const end =
    next?.index == null
      ? content.length
      : start + startMatch[0].length + next.index;
  return { start, end, content: content.slice(start, end).trim() };
}

function withoutRanges(content: string, ranges: Array<TextRange | null>): string {
  const sorted = ranges
    .filter((value): value is TextRange => value != null)
    .sort((a, b) => b.start - a.start);
  let result = content;
  for (const range of sorted) {
    result = `${result.slice(0, range.start)}${result.slice(range.end)}`;
  }
  return result.trim();
}

export function splitPromptParts(
  snapshot: PromptSnapshot,
  agent: Agent,
): PromptPart[] {
  const runtime =
    snapshot.layers.find((layer) => layer.name === "runtime_brief") ??
    snapshot.layers.find((layer) => layer.name === "agent_identity");
  const task = snapshot.layers.find((layer) => layer.name === "task_prompt");
  const content = runtime?.content_redacted ?? "";
  const role = sectionRange(
    content,
    /^## Agent Identity\s*$/m,
    /^##\s+/m,
  );
  const skills = sectionRange(
    content,
    /^## Agent Skills\s*$/m,
    /^##\s+/m,
  );
  const tools = sectionRange(
    content,
    /^### Connections & MCP tools.*$/m,
    /^(?:##|###)\s+/m,
  );
  const shared = withoutRanges(content, [role, skills, tools]);
  const delivery = runtime?.delivery ?? "";

  return [
    {
      id: "role",
      label: "Role & instructions",
      content: role?.content ?? "",
      delivery,
      byteSize: role?.content.length ?? 0,
      state: role ? undefined : "Not present in this run",
    },
    {
      id: "tools",
      label: "Tools & connections",
      content: tools?.content ?? "",
      delivery,
      byteSize: tools?.content.length ?? 0,
      state: tools ? undefined : "Not present in this run",
    },
    {
      id: "skills",
      label: "Skills",
      content: skills?.content ?? "",
      delivery,
      byteSize: skills?.content.length ?? 0,
      state: skills ? undefined : "No skills included",
    },
    {
      id: "shared",
      label: "Shared brief",
      content: shared,
      delivery,
      byteSize: shared.length,
      state: shared ? undefined : "Skipped for this run",
    },
    {
      id: "case",
      label: "Case itself",
      content: task?.content_redacted ?? "",
      delivery: task?.delivery ?? "user_prompt",
      byteSize: task?.byte_size ?? 0,
      state: task ? undefined : "No case text recorded",
    },
    {
      id: "controls",
      label: "Run controls",
      content: controlText(agent),
      delivery: "daemon",
      byteSize: 0,
      state: "Runtime envelope — not prompt text",
    },
  ];
}

function applyPendingProposal(
  current: PromptPart[],
  agent: Agent,
  proposal: AgentContextChangeRequest,
): PromptPart[] {
  const proposed = proposal.proposed_snapshot;
  const config = runtimeConfig(proposed);
  return current.map((part) => {
    if (part.id === "role" && proposed.instructions !== agent.instructions) {
      const replaced = part.content.includes(agent.instructions)
        ? part.content.replace(agent.instructions, proposed.instructions)
        : proposed.instructions;
      return {
        ...part,
        content: replaced,
        byteSize: replaced.length,
        state: "Pending instructions applied",
      };
    }
    if (part.id === "shared" && config.workspace_brief_mode === "off") {
      return {
        ...part,
        content: "",
        byteSize: 0,
        state: "Skipped by pending change",
      };
    }
    if (part.id === "tools" && config.tools_brief_mode === "summary") {
      return {
        ...part,
        state: "Pending change will use the connection summary",
      };
    }
    if (part.id === "role") {
      const mode = config.system_prompt_mode;
      return {
        ...part,
        delivery:
          mode === "prepend"
            ? "user_prompt"
            : mode === "append" || mode === "replace"
              ? "system_prompt"
              : part.delivery,
      };
    }
    if (part.id === "controls") {
      const content = controlText(proposed);
      return {
        ...part,
        content,
        state: `Pending version ${proposal.proposed_version}`,
      };
    }
    return part;
  });
}

function diffContent(before: string, after: string): string {
  if (before === after) return "No change";
  const beforeLines = before.split("\n");
  const afterLines = after.split("\n");
  const lines: string[] = [];
  const max = Math.max(beforeLines.length, afterLines.length);
  for (let index = 0; index < max; index += 1) {
    if (beforeLines[index] === afterLines[index]) {
      if (beforeLines[index] != null) lines.push(`  ${beforeLines[index]}`);
      continue;
    }
    if (beforeLines[index] != null) lines.push(`- ${beforeLines[index]}`);
    if (afterLines[index] != null) lines.push(`+ ${afterLines[index]}`);
  }
  return lines.join("\n");
}

function diffParts(current: PromptPart[], after: PromptPart[]): PromptPart[] {
  return current.map((part, index) => {
    const next = after[index] ?? part;
    const content = diffContent(part.content, next.content);
    return {
      ...next,
      content,
      byteSize: content === "No change" ? 0 : content.length,
      state: content === "No change" ? "Unchanged" : "Changed by pending proposal",
    };
  });
}

export function CerebroAgentPromptSnapshotTab({ agent }: { agent: Agent }) {
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const [viewMode, setViewMode] = useState<ViewMode>("current");
  const [activePart, setActivePart] = useState<PromptPart["id"]>("role");

  const listQuery = useQuery({
    queryKey: listKey(agent.id),
    queryFn: () => listAgentPromptSnapshots(agent.id),
    enabled: !!agent.id,
    retry: false,
  });
  const proposalQuery = useQuery({
    queryKey: proposalsKey(agent.id),
    queryFn: () => api.listAgentContextChangeRequests(agent.id),
    enabled: !!agent.id,
    retry: false,
  });
  const snapshots = listQuery.data ?? [];
  const effectiveTaskId = selectedTaskId ?? snapshots[0]?.task_id ?? null;
  const detailQuery = useQuery({
    queryKey: detailKey(agent.id, effectiveTaskId ?? ""),
    queryFn: () => getAgentPromptSnapshot(agent.id, effectiveTaskId ?? ""),
    enabled: !!agent.id && !!effectiveTaskId,
    retry: false,
  });

  if (listQuery.isLoading) return <Skeleton className="h-72 w-full" />;
  if (listQuery.isError) {
    return (
      <p className="text-sm text-muted-foreground">
        Prompt snapshots could not be loaded. Check that the snapshot store is
        available and try again.
      </p>
    );
  }
  if (snapshots.length === 0) {
    return (
      <div className="space-y-2">
        <h3 className="text-base font-semibold">What {agent.name} actually read</h3>
        <p className="text-sm text-muted-foreground">
          No recorded run exists yet. The first run captures the exact prompt,
          delivery path and redacted evidence here.
        </p>
      </div>
    );
  }

  const snapshot = detailQuery.data ?? null;
  const pending =
    proposalQuery.data?.find((request) => request.status === "pending") ?? null;

  return (
    <div className="space-y-4 p-4 md:p-6">
      <div className="flex flex-col gap-3 xl:flex-row xl:items-end xl:justify-between">
        <div>
          <h3 className="text-lg font-semibold">
            What {agent.name} actually read
          </h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Recorded prompt evidence, split into understandable parts. Secrets
            stay redacted.
          </p>
        </div>
        <RunSelector
          snapshots={snapshots}
          selectedTaskId={effectiveTaskId}
          onSelect={(taskId) => {
            setSelectedTaskId(taskId);
            setActivePart("role");
          }}
        />
      </div>

      <div
        className="inline-flex rounded-lg border bg-muted/30 p-1"
        role="group"
        aria-label="Prompt comparison"
      >
        {(
          [
            ["current", "This run"],
            ["after_proposal", "If the pending change is approved"],
            ["diff", "Difference"],
          ] as const
        ).map(([value, label]) => (
          <Button
            key={value}
            size="sm"
            variant={viewMode === value ? "secondary" : "ghost"}
            onClick={() => setViewMode(value)}
          >
            {label}
          </Button>
        ))}
      </div>

      {viewMode !== "current" && !pending ? (
        <div className="rounded-xl border border-dashed p-5 text-sm text-muted-foreground">
          No pending configuration change exists, so there is nothing to
          compare with this run.
        </div>
      ) : detailQuery.isLoading ? (
        <Skeleton className="h-96 w-full" />
      ) : snapshot ? (
        <PromptExplorer
          snapshot={snapshot}
          agent={agent}
          pending={pending}
          viewMode={viewMode}
          activePart={activePart}
          onSelectPart={setActivePart}
        />
      ) : (
        <p className="text-sm text-muted-foreground">
          This run&apos;s prompt snapshot could not be loaded.
        </p>
      )}
    </div>
  );
}

function RunSelector({
  snapshots,
  selectedTaskId,
  onSelect,
}: {
  snapshots: PromptSnapshotRef[];
  selectedTaskId: string | null;
  onSelect: (taskId: string) => void;
}) {
  return (
    <div className="space-y-1">
      <label
        htmlFor="cerebro-prompt-snapshot-run"
        className="text-xs font-medium"
      >
        Run
      </label>
      <select
        id="cerebro-prompt-snapshot-run"
        className="h-9 min-w-72 rounded-md border bg-background px-3 text-xs"
        value={selectedTaskId ?? ""}
        onChange={(event) => onSelect(event.target.value)}
      >
        {snapshots.map((snapshot) => (
          <option key={snapshot.task_id} value={snapshot.task_id}>
            {formatRunDate(snapshot.created_at)} · {snapshot.provider} ·{" "}
            {snapshot.agent_context_version || "no version"}
          </option>
        ))}
      </select>
    </div>
  );
}

function PromptExplorer({
  snapshot,
  agent,
  pending,
  viewMode,
  activePart,
  onSelectPart,
}: {
  snapshot: PromptSnapshot;
  agent: Agent;
  pending: AgentContextChangeRequest | null;
  viewMode: ViewMode;
  activePart: PromptPart["id"];
  onSelectPart: (part: PromptPart["id"]) => void;
}) {
  const current = useMemo(
    () => splitPromptParts(snapshot, agent),
    [snapshot, agent],
  );
  const after = useMemo(
    () => (pending ? applyPendingProposal(current, agent, pending) : current),
    [current, agent, pending],
  );
  const parts =
    viewMode === "current"
      ? current
      : viewMode === "after_proposal"
        ? after
        : diffParts(current, after);
  const selected = (parts.find((part) => part.id === activePart) ?? parts[0])!;
  const text = parts.map((part) => part.content).join("\n\n");
  const { tokens } = estimateTokensFromLength(snapshot.total_bytes, "en");
  const [copied, setCopied] = useState(false);

  return (
    <section
      role="region"
      aria-label="Prompt evidence"
      className="space-y-4"
    >
      <div className="grid gap-px overflow-hidden rounded-xl border bg-border sm:grid-cols-3">
        <Metric
          label="Characters"
          value={snapshot.total_bytes.toLocaleString("en-US")}
        />
        <Metric
          label="Estimated tokens"
          value={`~${tokens.toLocaleString("en-US")} tokens`}
        />
        <Metric label="Parts" value={String(parts.length)} />
      </div>

      {viewMode !== "current" && pending && (
        <p className="rounded-lg border border-blue-500/30 bg-blue-500/5 px-3 py-2 text-xs text-muted-foreground">
          This comparison applies the pending settings to recorded evidence.
          The next run creates the exact after snapshot.
        </p>
      )}

      <div className="grid min-h-[34rem] overflow-hidden rounded-xl border lg:grid-cols-[19rem_minmax(0,1fr)]">
        <nav
          className="border-b bg-muted/20 p-3 lg:border-b-0 lg:border-r"
          aria-label="Prompt parts"
        >
          <p className="mb-2 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            The parts it is made of
          </p>
          <div className="space-y-2">
            {parts.map((part) => (
              <PromptPartChoice
                key={part.id}
                part={part}
                active={activePart === part.id}
                onSelect={() => onSelectPart(part.id)}
              />
            ))}
          </div>
        </nav>

        <div className="min-w-0">
          <header className="flex flex-wrap items-center gap-2 border-b bg-muted/10 px-4 py-3">
            <span className="text-sm font-semibold">{selected.label}</span>
            <Badge variant="outline">
              {deliveryLabel(selected.delivery)}
            </Badge>
            {snapshot.redacted && <Badge variant="secondary">Redacted</Badge>}
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="ml-auto"
              onClick={() => {
                void navigator.clipboard.writeText(text).then(() => {
                  setCopied(true);
                  window.setTimeout(() => setCopied(false), 1500);
                });
              }}
            >
              {copied ? (
                <Check className="size-3.5" />
              ) : (
                <Copy className="size-3.5" />
              )}
              {copied ? "Copied" : "Copy whole prompt"}
            </Button>
          </header>
          <LineNumberedText
            content={selected.content}
            diff={viewMode === "diff"}
          />
        </div>
      </div>

      <details className="rounded-xl border bg-card">
        <summary className="cursor-pointer px-4 py-3 text-sm font-medium">
          Technical evidence
          <span className="ml-2 text-xs font-normal text-muted-foreground">
            run identity, delivery and hashes
          </span>
        </summary>
        <div className="space-y-4 border-t p-4">
          <div className="grid gap-px overflow-hidden rounded-xl border bg-border sm:grid-cols-3">
            <Metric
              label="Delivery"
              value={snapshot.system_prompt_mode || "Engine default"}
            />
            <Metric
              label="Redaction"
              value={snapshot.redacted ? "Applied" : "Not needed"}
            />
            <Metric
              label="Config"
              value={snapshot.agent_context_version || "Unversioned"}
            />
          </div>
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <Badge variant="secondary">
              Captured from the run — not reconstructed
            </Badge>
            <span className="font-mono">run {snapshot.task_id}</span>
            <span className="font-mono">
              {snapshot.provider} {snapshot.runtime_version}
            </span>
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1 rounded-lg bg-muted/40 px-3 py-2 font-mono text-xs text-muted-foreground">
            <span title={snapshot.sha256_original}>
              original {shortSha(snapshot.sha256_original)}
            </span>
            <span title={snapshot.sha256_redacted}>
              view {shortSha(snapshot.sha256_redacted)}
            </span>
          </div>
        </div>
      </details>
    </section>
  );
}

function PromptPartChoice({
  part,
  active,
  onSelect,
}: {
  part: PromptPart;
  active: boolean;
  onSelect: () => void;
}) {
  const partTokens = estimateTokensFromLength(part.byteSize, "en").tokens;
  const action = promptPartAction(part.id);
  return (
    <div
      className={`rounded-lg border p-3 ${
        active
          ? "border-primary/40 bg-background shadow-sm"
          : "border-transparent hover:bg-background/70"
      }`}
    >
      <button type="button" className="w-full text-left" onClick={onSelect}>
        <span className="flex items-center justify-between gap-2 text-xs font-medium">
          <span>{part.label}</span>
          <span className="font-mono text-muted-foreground">
            {part.byteSize === 0 ? "0" : `~${partTokens}`}
          </span>
        </span>
        <span className="mt-1 block text-xs leading-relaxed text-muted-foreground">
          {promptPartDescription(part)}
        </span>
      </button>
      {action ? (
        <a
          href={action.href}
          className="mt-2 inline-block text-xs font-medium underline underline-offset-2"
        >
          {action.label}
        </a>
      ) : (
        <span className="mt-2 block text-xs font-medium text-muted-foreground">
          Not configuration.
        </span>
      )}
    </div>
  );
}

function promptPartDescription(part: PromptPart): string {
  if (part.state) return part.state;
  switch (part.id) {
    case "role":
      return "The job, boundaries and engine instructions.";
    case "tools":
      return "The wording for tools and connections.";
    case "skills":
      return "The skills available for this run.";
    case "shared":
      return "Workspace rules and shared context.";
    case "case":
      return "The task that started this run.";
    case "controls":
      return runLimitSummary(part.content);
  }
}

function promptPartAction(
  id: PromptPart["id"],
): { label: string; href: string } | null {
  switch (id) {
    case "role":
      return {
        label: "Edit instructions",
        href: "?tab=context#agent-instructions",
      };
    case "tools":
      return {
        label: "Change tool wording",
        href: "?tab=context#ac-tools-brief",
      };
    case "skills":
      return { label: "Change skills", href: "?tab=skills" };
    case "shared":
      return {
        label: "Change shared brief",
        href: "?tab=context#ac-workspace-brief",
      };
    case "controls":
      return {
        label: "Change run controls",
        href: "?tab=context#how-agent-runs",
      };
    case "case":
      return null;
  }
}

function runLimitSummary(content: string): string {
  const steps = /^Stop after:\s*(.+)$/m.exec(content)?.[1] ?? "Run mode default";
  const minutes =
    /^Give up after:\s*(.+)$/m.exec(content)?.[1] ?? "Run mode default";
  return `Stops after ${steps} steps and gives up after ${minutes} minutes.`;
}

function LineNumberedText({
  content,
  diff,
}: {
  content: string;
  diff: boolean;
}) {
  const lines = (content || "No content in this part.").split("\n");
  return (
    <pre className="min-h-[28rem] overflow-x-auto bg-background p-0 text-xs leading-6">
      <code>
        {lines.map((line, index) => (
          <span
            key={`${index}-${line}`}
            className={`flex ${
              diff && line.startsWith("+ ")
                ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
                : diff && line.startsWith("- ")
                  ? "bg-red-500/10 text-red-700 dark:text-red-300"
                  : ""
            }`}
          >
            <span className="w-14 shrink-0 select-none border-r pr-3 text-right text-muted-foreground/60">
              {index + 1}
            </span>
            <span className="whitespace-pre-wrap px-4">{line}</span>
          </span>
        ))}
      </code>
    </pre>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-card px-3 py-2.5">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-0.5 truncate text-sm font-semibold">{value}</p>
    </div>
  );
}
