import type { LocalBackgroundTask } from "@multica/core/local-conversation/protocol";

/** The fields of a saved task event that identify a background task's call. */
export interface BackgroundTaskEvent {
  type: string;
  call_id?: string;
  input?: Record<string, unknown>;
  output?: string;
}

export function backgroundTaskPresentation(task: LocalBackgroundTask, events: readonly BackgroundTaskEvent[], locale: "en" | "zh-CN") {
  const chinese = locale === "zh-CN";
  // Old saved cards only carry a description. Join only a unique exact match in the same turn.
  const calls = events.filter((event) => event.type === "tool_use" && (task.toolUseId ? event.call_id === task.toolUseId : event.input?.description === task.description));
  const call = calls.length === 1 ? calls[0] : undefined;
  const result = call?.call_id ? events.findLast((event) => event.type === "tool_result" && event.call_id === call.call_id) : undefined;
  const resultSummary = task.summary?.trim();
  const lastOutput = task.status === "running" ? task.output?.trim().split(/[\r\n]/).filter((line) => line.trim()).at(-1)?.trim().slice(0, 240) : undefined;
  const summary = task.status !== "running" && resultSummary && resultSummary !== task.description.trim() ? resultSummary : task.progressSummary?.trim() || (lastOutput ? `${chinese ? "最近输出：" : "Latest output: "}${lastOutput}` : undefined);
  const metrics: string[] = [];
  const measuredDuration = task.createdAt && (task.completedAt || task.updatedAt) ? Date.parse(task.completedAt || task.updatedAt!) - Date.parse(task.createdAt) : NaN;
  const duration = task.usage?.durationMs ?? (Number.isFinite(measuredDuration) && measuredDuration >= 0 ? measuredDuration : undefined);
  if (duration !== undefined) metrics.push(chinese ? `耗时 ${Math.floor(duration / 1000)} 秒` : `${Math.floor(duration / 1000)}s elapsed`);
  if (task.usage) {
    metrics.push(chinese ? `${task.usage.toolUses} 次工具调用` : `${task.usage.toolUses} tool uses`);
    metrics.push(`${task.usage.totalTokens.toLocaleString(locale)} tokens`);
  }
  if (task.lastToolName) metrics.push(chinese ? `最近工具：${task.lastToolName}` : `Last tool: ${task.lastToolName}`);
  const progress = [summary, metrics.join(" · ")].filter(Boolean).join("\n") || (task.status === "running" ? chinese ? "等待 CLI 更新进度" : "Waiting for CLI progress" : "");
  const command = typeof call?.input?.command === "string" ? call.input.command : undefined;
  const output = task.output ?? result?.output;
  const truncated = task.outputTruncated || (output?.length ?? 0) > 32768;
  const detail = [
    command ? `${chinese ? "命令：" : "Command:"}\n${command}` : undefined,
    output?.trim() ? `${chinese ? "输出：" : "Output:"}\n${output.slice(-32768)}${truncated ? `\n[${chinese ? "仅显示末尾输出" : "Only the output tail is shown"}]` : ""}` : undefined,
  ].filter(Boolean).join("\n\n");
  return { progress, detail };
}
