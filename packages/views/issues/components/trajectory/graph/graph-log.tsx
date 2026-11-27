import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { graphMessagesOptions, type GraphScope } from "@multica/core/trajectory/graph-web";
import type { TrajectoryNode } from "@multica/core/trajectory/graph";
import type { TaskMessagePayload } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Terminal } from "@multica/ui/icons";
import { useT } from "../../../../i18n";
import { GraphText } from "./graph-record-text";

const messageTypes = new Set(["text", "thinking", "tool_use", "tool_result", "error"]);
const messageFields = new Set(["call_id", "task_id", "issue_id", "chat_session_id", "seq", "type", "tool", "content", "input", "output", "output_truncated", "created_at", "source_message_type"]);

function returnedMessageType(event: TaskMessagePayload): string {
  const original = Object.entries(event).find(([field]) => field === "source_message_type")?.[1];
  return typeof original === "string" ? original : event.type;
}

function valueText(value: unknown): string {
  if (typeof value === "string") return value;
  try { return JSON.stringify(value, null, 2) ?? String(value); }
  catch { return String(value); }
}

function MessageField({ label, value, literal = false }: { label: string; value: unknown; literal?: boolean }) {
  const { t } = useT("issues");
  const text = valueText(value);
  return <section className="trajectory-log-section">
    <h4>{label}</h4>
    {text ? <GraphText content={text} literal={literal || typeof value !== "string"}/> : <p className="text-muted-foreground">{t($ => $.trajectory_detail.no_content)}</p>}
  </section>;
}

function MessageContent({ event }: { event: TaskMessagePayload }) {
  const { t } = useT("issues");
  const hasFields = event.content !== undefined || event.input !== undefined || event.output !== undefined;
  const extraFields = hasFields ? [] : Object.entries(event).filter(([key]) => !messageFields.has(key));
  return <>
    {!messageTypes.has(returnedMessageType(event)) && <p className="text-muted-foreground">{t($ => $.trajectory_detail.unknown_message)}</p>}
    {event.content !== undefined && <MessageField label={event.type === "error" ? t($ => $.trajectory_detail.error) : t($ => $.trajectory_detail.message)} value={event.content}/>}
    {event.input !== undefined && <MessageField label={t($ => $.trajectory_detail.tool_input)} value={event.input} literal/>}
    {event.output !== undefined && <MessageField label={t($ => $.trajectory_detail.tool_output)} value={event.output} literal/>}
    {extraFields.map(([key, value]) => <MessageField key={key} label={key} value={value}/>)}
    {!hasFields && extraFields.length === 0 && <p className="text-muted-foreground">{t($ => $.trajectory_detail.no_content)}</p>}
    {event.output_truncated === true && <p className="text-muted-foreground">{t($ => $.trajectory.storage_truncation)}</p>}
    <details className="trajectory-record-raw"><summary>{t($ => $.trajectory_detail.raw_data)}</summary>
      <pre>{valueText(event)}</pre>
    </details>
  </>;
}

export function GraphLog({ scope, run, messageSeq }: { scope: GraphScope; run: TrajectoryNode; messageSeq?: number }) {
  const { t } = useT("issues");
  const highlighted = useRef<HTMLDetailsElement>(null);
  const located = useRef<string | null>(null);
  const events = useQuery({ ...graphMessagesOptions(scope, run.run_id!), enabled: !!run.run_id });
  // Locate the linked message once it arrives; a later refetch keeps the reading position.
  useEffect(() => {
    const target = `${run.run_id}:${messageSeq}`;
    if (messageSeq !== undefined && highlighted.current && located.current !== target) {
      located.current = target;
      highlighted.current.open = true;
      highlighted.current.scrollIntoView({ block: "nearest" });
    }
  }, [events.data, messageSeq, run.run_id]);
  return <div className="trajectory-log" data-testid="trajectory-log">
    <div className="trajectory-log-heading"><Terminal className="size-4"/><h3>{t($ => $.trajectory_detail.run)}</h3></div>
    <p className="trajectory-log-id">{run.id}</p>
    {events.isPending && <p role="status">{t($ => $.trajectory.loading)}</p>}
    {events.isError && <p role="alert">{t($ => $.trajectory_graph.read_failed)} <Button size="sm" variant="outline" onClick={() => void events.refetch()}>{t($ => $.trajectory.retry)}</Button></p>}
    {events.isSuccess && events.data.length === 0 && <p className="text-muted-foreground">{t($ => $.trajectory_graph.no_messages)}</p>}
    {events.data?.map((event, index) => <details key={`${run.run_id}:${event.seq}:${index}`} className="trajectory-log-message" ref={event.seq === messageSeq ? highlighted : undefined} data-message-seq={event.seq} data-highlighted={event.seq === messageSeq || undefined} open>
      <summary><span>{[event.seq, returnedMessageType(event), ...(event.tool ? [event.tool] : [])].join(" · ")}</span>
        {event.created_at && <time dateTime={event.created_at}>{event.created_at}</time>}
      </summary>
      <MessageContent event={event}/>
    </details>)}
  </div>;
}
