import type { V2Conversation } from "@multica/core/local-conversation/conversation";

export type SideActivityState = "idle" | "running" | "waiting" | "completed" | "failed" | "cancelled";

const text = (value: unknown) => typeof value === "string" ? value : "";

/** Derive display information from this conversation only, without exposing reasoning. */
export function localSideActivity(conversation?: V2Conversation) {
  const turns = conversation ? [...conversation.turns.values()].sort((a, b) => a.startedAt.localeCompare(b.startedAt)) : [];
  const turn = turns.filter((turn) => turn.status === "running").at(-1) ?? turns.at(-1);
  const all = conversation ? [...conversation.items.values()] : [];
  const background = all.filter((item) => item.kind === "background_task" && item.status === "in_progress");
  const dispatching = !!conversation?.queue?.dispatching_request_id;
  const running = turn?.status === "running" || background.length > 0 || dispatching;
  const waiting = running && !!conversation && [...conversation.interactions.values()].some((interaction) => interaction.status === "pending");
  const items = !turn || (dispatching && turn.status !== "running") ? [] : conversation!.turnItems(turn.id);
  const tools = items.filter((item) => item.kind === "tool_call" || item.kind === "command" || item.kind === "file_change");
  const completedTools = tools.filter((item) => item.status !== "in_progress").length;
  const last = items.filter((item) => item.kind !== "background_task" && item.kind !== "image").at(-1);
  const toolName = (item: typeof last) => item?.kind === "command" ? "Bash" : item?.kind === "file_change" ? "Edit" : text(item?.state.tool) || undefined;
  const tool = last && tools.includes(last) ? toolName(last) : undefined;
  const output = items.filter((item) => item.kind === "assistant_text").map((item) => text(item.state.text)).join("").trim();
  const state: SideActivityState = waiting ? "waiting" : running ? "running" : turn?.status ?? (output ? "completed" : "idle");
  return {
    state, spinning: running && !waiting,
    tool, toolFinished: !!last && tools.includes(last) && last.status !== "in_progress", completedTools, totalTools: tools.length,
    thinking: last?.kind === "reasoning",
    background: background.map((item) => text(item.state.progress_summary) || text(item.state.description)).filter(Boolean).join(" · "),
    output: dispatching ? "" : output,
    error: state === "failed" ? turn?.error?.message : undefined,
    turnId: turn?.status === "running" ? turn.id : undefined,
  };
}
