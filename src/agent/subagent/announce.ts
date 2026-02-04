/**
 * Subagent announcement flow.
 *
 * Handles result propagation from child → parent agent:
 * - Builds system prompts for child agents
 * - Reads child session output
 * - Formats and delivers announcement messages
 */

import { readEntries } from "../session/storage.js";
import { getHub } from "../../hub/hub-singleton.js";
import { buildSystemPrompt } from "../system-prompt/index.js";
import type {
  SubagentAnnounceParams,
  SubagentRunOutcome,
  SubagentSystemPromptParams,
} from "./types.js";

/**
 * Build the system prompt injected into a subagent session.
 * Uses the structured prompt builder with "minimal" mode.
 */
export function buildSubagentSystemPrompt(params: SubagentSystemPromptParams): string {
  return buildSystemPrompt({
    mode: "minimal",
    subagent: {
      requesterSessionId: params.requesterSessionId,
      childSessionId: params.childSessionId,
      label: params.label,
      task: params.task,
    },
  });
}

/**
 * Read the latest assistant reply from a session's JSONL file.
 */
export function readLatestAssistantReply(sessionId: string): string | undefined {
  const entries = readEntries(sessionId);

  // Walk backwards to find last assistant message
  for (let i = entries.length - 1; i >= 0; i--) {
    const entry = entries[i]!;
    if (entry.type !== "message") continue;

    const message = entry.message;
    if (message.role !== "assistant") continue;

    return extractAssistantText(message);
  }

  return undefined;
}

/**
 * Extract text content from an assistant message.
 * AgentMessage.content for assistant is (TextContent | ThinkingContent | ToolCall)[].
 */
function extractAssistantText(message: { role: string; content: unknown }): string {
  const content = message.content;
  if (typeof content === "string") {
    return sanitizeText(content);
  }

  if (!Array.isArray(content)) return "";

  const textParts: string[] = [];
  for (const block of content) {
    if (block && typeof block === "object" && "type" in block && block.type === "text" && "text" in block) {
      textParts.push(String(block.text));
    }
  }

  return sanitizeText(textParts.join("\n"));
}

/**
 * Strip thinking tags and tool markers from text.
 */
function sanitizeText(text: string): string {
  return text
    .replace(/<thinking>[\s\S]*?<\/thinking>/g, "")
    .replace(/<tool_call>[\s\S]*?<\/tool_call>/g, "")
    .trim();
}

/**
 * Format the duration between two timestamps as a human-readable string.
 */
function formatDuration(startMs: number, endMs: number): string {
  const totalSeconds = Math.round((endMs - startMs) / 1000);
  if (totalSeconds < 60) return `${totalSeconds}s`;

  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) return seconds > 0 ? `${minutes}m${seconds}s` : `${minutes}m`;

  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  return remainingMinutes > 0 ? `${hours}h${remainingMinutes}m` : `${hours}h`;
}

/**
 * Format a status label from an outcome.
 */
function formatStatusLabel(outcome: SubagentRunOutcome | undefined): string {
  if (!outcome) return "completed with unknown status";
  switch (outcome.status) {
    case "ok":
      return "completed successfully";
    case "error":
      return outcome.error ? `failed: ${outcome.error}` : "failed";
    case "timeout":
      return "timed out";
    default:
      return "completed with unknown status";
  }
}

/** Parameters for formatAnnouncementMessage */
export interface FormatAnnouncementParams {
  runId: string;
  childSessionId: string;
  requesterSessionId: string;
  task: string;
  label?: string | undefined;
  cleanup: "delete" | "keep";
  outcome?: SubagentRunOutcome | undefined;
  startedAt?: number | undefined;
  endedAt?: number | undefined;
  findings?: string | undefined;
}

/**
 * Format the announcement message sent to the parent agent.
 */
export function formatAnnouncementMessage(params: FormatAnnouncementParams): string {
  const { task, label, outcome, findings, startedAt, endedAt, childSessionId } = params;
  const displayName = label || task.slice(0, 60);
  const statusLabel = formatStatusLabel(outcome);

  const parts: string[] = [
    `A background task "${displayName}" just ${statusLabel}.`,
    "",
    "Findings:",
    findings || "(no output)",
  ];

  // Stats line
  const stats: string[] = [];
  if (startedAt && endedAt) {
    stats.push(`runtime ${formatDuration(startedAt, endedAt)}`);
  }
  stats.push(`session ${childSessionId}`);

  parts.push("", `Stats: ${stats.join(" • ")}`);

  parts.push(
    "",
    "Summarize this naturally for the user. Keep it brief (1-2 sentences).",
    "Flow it into the conversation naturally.",
    "Do not mention technical details like session IDs or that this was a background task.",
    "You can respond with NO_REPLY if no announcement is needed (e.g., internal task with no user-facing result).",
  );

  return parts.join("\n");
}

/**
 * Run the full subagent announcement flow:
 * 1. Read child's last assistant reply
 * 2. Format announcement message
 * 3. Send to parent agent via Hub
 */
export function runSubagentAnnounceFlow(params: SubagentAnnounceParams): boolean {
  const { requesterSessionId, childSessionId } = params;

  // Read child's final output
  const findings = readLatestAssistantReply(childSessionId);

  // Format the announcement
  const message = formatAnnouncementMessage({
    runId: params.runId,
    childSessionId: params.childSessionId,
    requesterSessionId: params.requesterSessionId,
    task: params.task,
    label: params.label,
    cleanup: params.cleanup,
    outcome: params.outcome,
    startedAt: params.startedAt,
    endedAt: params.endedAt,
    findings,
  });

  // Deliver to parent agent via Hub
  try {
    const hub = getHub();
    const parentAgent = hub.getAgent(requesterSessionId);
    if (!parentAgent || parentAgent.closed) {
      console.warn(
        `[SubagentAnnounce] Parent agent not found or closed: ${requesterSessionId}`,
      );
      return false;
    }

    parentAgent.write(message);
    return true;
  } catch (err) {
    console.error(`[SubagentAnnounce] Failed to announce to parent:`, err);
    return false;
  }
}
