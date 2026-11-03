import { AgentMessageMarkdown } from "../../../vendor/tutti/packages/agent/gui/shared/AgentMessageMarkdown";
import { AgentUserImageGrid } from "../../../vendor/tutti/packages/agent/gui/shared/agentConversation/components/AgentMessageImages";
import type { AgentMessageContentVM } from "../../../vendor/tutti/packages/agent/gui/shared/agentConversation/contracts/agentMessageRowVM";

/** Public child messages stay in the background card, outside the root reply. */
export function SubagentTranscript({ message }: { message: Pick<AgentMessageContentVM, "sourceTimelineItems"> }) {
  const transcript = message.sourceTimelineItems?.map((item) => record(item.payload?.subagentTranscript)).find(Boolean);
  if (!transcript) return null;
  const chinese = transcript.locale === "zh-CN";
  const items = Array.isArray(transcript.items) ? transcript.items.map(record).filter((item) => item !== null) : [];
  const prompt = text(transcript.prompt);
  const statuses: Record<string, string> = chinese
    ? { in_progress: "执行中", completed: "已完成", failed: "失败", cancelled: "已停止" }
    : { in_progress: "Running", completed: "Completed", failed: "Failed", cancelled: "Stopped" };
  return <details className="mt-2 min-w-0" data-subagent-transcript>
    <summary className="cursor-pointer select-none text-body font-medium text-foreground">{chinese ? "子对话" : "Subagent conversation"}</summary>
    <ol aria-label={chinese ? "子对话消息" : "Subagent messages"} className="mt-3 space-y-3 border-l border-border pl-3">
      {prompt ? <li className="min-w-0"><div className="text-caption text-muted-foreground">{chinese ? "任务指令" : "Task instructions"}</div><AgentMessageMarkdown content={prompt} /></li> : null}
      {items.length === 0 ? <li className="text-body text-muted-foreground">{chinese ? "尚无执行记录；旧任务未保存的详情无法回看。" : "No execution details recorded. Details from older tasks may be unavailable."}</li> : null}
      {items.map((item) => {
        const kind = text(item.kind);
        const id = text(item.id);
        if (kind === "assistant_text") return <li key={id} className="min-w-0 break-words text-foreground"><AgentMessageMarkdown content={text(item.text)} streaming={item.status === "in_progress"} /></li>;
        if (kind === "image") return <li key={id}><AgentUserImageGrid message={{
          kind: "message-content", id, turnId: "", body: "", presentationKind: "content", occurredAtUnixMs: null,
          images: [{ id, attachmentId: text(item.attachmentId), mimeType: text(item.mimeType),
            workspaceId: message.sourceTimelineItems?.[0]?.workspaceId,
            agentSessionId: message.sourceTimelineItems?.[0]?.agentSessionId ?? "",
          }],
        }} /></li>;
        return <li key={id} className="min-w-0">
          <details className="min-w-0 rounded-md border border-border p-2">
            <summary className="cursor-pointer break-words text-body text-foreground">{text(item.tool) || (chinese ? "错误" : "Error")} <span className="text-caption text-muted-foreground">{statuses[text(item.status)] ?? text(item.status)}</span></summary>
            {item.input !== undefined ? <><div className="mt-2 text-caption text-muted-foreground">{chinese ? "输入" : "Input"}</div><pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words font-mono text-caption text-foreground">{JSON.stringify(item.input, null, 2)}</pre></> : null}
            {text(item.output) ? <><div className="mt-2 text-caption text-muted-foreground">{chinese ? "输出" : "Output"}</div><pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words font-mono text-caption text-foreground">{text(item.output)}</pre></> : null}
          </details>
        </li>;
      })}
    </ol>
  </details>;
}

function record(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function text(value: unknown): string { return typeof value === "string" ? value : ""; }
