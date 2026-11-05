import { localSubmissionEventSchema, mergeSubmission } from "@multica/core/local-conversation/submission";
import { localV2SnapshotSchema, localV2SummarySchema, type LocalV2Event, type LocalV2Snapshot, type LocalV2Summary } from "@multica/core/local-conversation/v2";

const text = (value: unknown) => typeof value === "string" ? value : "";
const record = (value: unknown): Record<string, unknown> => value !== null && typeof value === "object" && !Array.isArray(value) ? Object.fromEntries(Object.entries(value)) : {};

/** Pure mobile projection; a sequence gap forces a snapshot before more deltas. */
export function applyEvents(snapshot: LocalV2Snapshot, events: LocalV2Event[]): LocalV2Snapshot | null {
  const next = structuredSnapshot(snapshot);
  for (const event of events) {
    if (event.seq <= next.snapshot_seq) continue;
    if (event.seq !== next.snapshot_seq + 1) return null;
    next.snapshot_seq = event.seq;
    const p = event.payload;
    if(event.type==="submission.updated"){
      const result=localSubmissionEventSchema.safeParse(p);
      if(result.success){const values=new Map(next.submissions.map((value)=>[value.request_id,value]));const value=result.data.submission;values.set(value.request_id,mergeSubmission(values.get(value.request_id),value));next.submissions=[...values.values()];}
    }
    if (event.type === "queue.updated") {
      const queue = localV2SnapshotSchema.shape.queue.safeParse({ ...next.queue, ...p, error: typeof p.error === "string" ? p.error : undefined });
      if (queue.success && queue.data.revision >= next.queue.revision) next.queue = queue.data;
      if (typeof p.count === "number") next.conversation = { ...next.conversation, queue_count: p.count };
    }
    if (event.type === "conversation.updated") {
      const summary = localV2SummarySchema.safeParse({ ...next.conversation, ...p });
      if (summary.success) next.conversation = summary.data;
      const settings = localV2SnapshotSchema.shape.settings.safeParse({ ...next.settings, ...p });
      if (settings.success) next.settings = settings.data;
    }
    let turn = next.turns.find((entry) => entry.id === event.turn_id);
    if (!turn && event.turn_id) {
      turn = { id: event.turn_id, turn_index: Math.max(-1, ...next.turns.map((entry) => entry.turn_index)) + 1, request_id: text(p.request_id), status: "running", started_at: event.created_at, messages: [], events: [] };
      next.turns.push(turn);
    }
    if (turn) turn.events.push(event);
    if (turn) next.conversation = { ...next.conversation, turn_count: Math.max(next.conversation.turn_count, turn.turn_index + 1) };
    if (event.type === "user.message" && turn && !turn.messages.some((m) => m.id === p.message_id)) {
      turn.messages.push({ id: text(p.message_id), role: "user", content: text(p.content), attachment_ids: Array.isArray(p.attachment_ids) ? p.attachment_ids.filter((v): v is string => typeof v === "string") : [], steered: p.steered === true, failed: false, created_at: event.created_at });
    }
    if (event.type === "turn.started" && turn) turn.status = "running";
    if (event.type === "turn.completed" && turn) {
      turn.status = text(p.status) || "completed";
      turn.completed_at = event.created_at;
      const error = record(p.error);
      if (typeof error.kind === "string" && typeof error.message === "string") turn.error = { kind: error.kind, message: error.message };
    }
    if (event.type === "interaction.requested" && event.turn_id && !next.pending_interactions.some((i) => i.interaction_id === p.interaction_id)) {
      next.pending_interactions.push({ interaction_id: text(p.interaction_id), turn_id: event.turn_id, kind: text(p.kind), request: record(p.request), created_at: event.created_at });
    }
    if (event.type === "interaction.resolved") next.pending_interactions = next.pending_interactions.filter((i) => i.interaction_id !== p.interaction_id);
  }
  return next;
}

/** Keep already-read settled turns when a newer first-screen window overlaps. */
export function retainSnapshotHistory(previous: LocalV2Snapshot | undefined, next: LocalV2Snapshot): LocalV2Snapshot {
  if (!previous || previous.store_generation !== next.store_generation) return next;
  if (previous.snapshot_seq > next.snapshot_seq) return previous;
  const first = next.turns[0]?.turn_index;
  if (first == null || !previous.turns.some((turn) => turn.turn_index >= first)) return next;
  const retained = previous.turns.filter((turn) => turn.turn_index < first && turn.status !== "running");
  return retained.length ? { ...next, turns: [...retained, ...next.turns], has_older_turns: previous.has_older_turns } : next;
}

function structuredSnapshot(snapshot: LocalV2Snapshot): LocalV2Snapshot {
  return { ...snapshot, turns: snapshot.turns.map((turn) => ({ ...turn, messages: [...turn.messages], events: [...turn.events] })), pending_interactions: [...snapshot.pending_interactions] };
}

export interface TranscriptRow { id: string; turnId?: string; role: "user" | "assistant" | "tool" | "reasoning"; content: string; running?: boolean; attachmentIds?: string[]; error?: boolean }
export type ConversationRow = (Omit<TranscriptRow, "role"> & { role: "user" | "assistant" | "error" }) | { id: string; turnId?: string; role: "activity"; items: TranscriptRow[] };

/** Keep user/assistant boundaries and failures visible while compacting diagnostics. */
export function groupTranscriptActivities(rows: TranscriptRow[]): ConversationRow[] {
  const out: ConversationRow[] = [];
  for (const row of rows) {
    if (row.role === "user" || row.role === "assistant" || row.error) {
      out.push({ ...row, role: row.error ? "error" : row.role === "user" ? "user" : "assistant" });
      continue;
    }
    const previous = out.at(-1);
    if (previous?.role === "activity" && previous.turnId === row.turnId) previous.items.push(row);
    else out.push({ id: `${row.id}:activity`, turnId: row.turnId, role: "activity", items: [row] });
  }
  return out;
}

export function transcript(snapshot: LocalV2Snapshot): TranscriptRow[] {
  return snapshot.turns.flatMap((turn) => {
    const rows: TranscriptRow[] = turn.messages.filter((m) => m.role === "user").map((m) => ({ id: m.id, role: "user", content: m.content, attachmentIds: m.attachment_ids }));
    const items = new Map<string, { kind: string; content: string; completed: boolean }>();
    for (const event of turn.events) {
      if (!event.item_id || !event.type.startsWith("item.")) continue;
      const previous = items.get(event.item_id) ?? { kind: text(event.payload.kind), content: "", completed: false };
      const p = event.payload;
      const delta = event.type === "item.delta" && (p.field === "text" || p.field === "output");
      items.set(event.item_id, {
        kind: text(p.kind) || previous.kind,
        content: delta ? previous.content + text(p.text) : text(p.text) || text(p.output) || text(p.command) || text(p.title) || previous.content,
        completed: previous.completed || event.type === "item.completed",
      });
    }
    let hasAssistant = false;
    for (const [id, item] of items) {
      if (item.kind === "reasoning") {
        // Summaries can be off or arrive late, so a running item shows without text.
        const running = !item.completed && turn.status === "running";
        if (item.content || running) rows.push({ id, role: "reasoning", content: item.content, running });
        continue;
      }
      if (!item.content) continue;
      const role = item.kind === "assistant_text" ? "assistant" : "tool";
      if (role === "assistant") hasAssistant = true;
      rows.push({ id, role, content: item.content });
    }
    if (!hasAssistant) rows.push(...turn.messages.filter((m) => m.role !== "user").map((m) => ({ id: m.id, role: "assistant" as const, content: m.content })));
    if (turn.error) rows.push({ id: `${turn.id}:error`, role: "tool", content: turn.error.message, error: true });
    return rows.map((row) => ({ ...row, turnId: turn.id }));
  });
}

export function visibleSessions(sessions: LocalV2Summary[], search: string, archived: boolean) {
  const needle = search.trim().toLocaleLowerCase();
  return sessions.filter((s) => s.archived === archived && (!needle || `${s.title}\n${s.last_message_preview}`.toLocaleLowerCase().includes(needle)))
    .sort((a, b) => Number(b.pinned) - Number(a.pinned) || b.updated_at.localeCompare(a.updated_at));
}

/** Folder grouping follows execution directories, independently of cloud projects. */
export function groupSessions(sessions: LocalV2Summary[], projects: { id: string; title: string; path: string }[], search: string, archived: boolean, unassigned: string) {
  const directory = (path: string) => path.replace(/[/\\]+$/, "") || path;
  const groups = projects.map((project) => ({ ...project, data: [] as LocalV2Summary[] }));
  const other = { id: null, title: unassigned, path: "", data: [] as LocalV2Summary[] };
  const needle = search.trim().toLocaleLowerCase();
  for (const session of visibleSessions(sessions, "", archived)) {
    const group = groups.find((g) => directory(g.path) === directory(session.cwd)) ?? other;
    if (!needle || [session.title, session.last_message_preview, group.title, group.path].some((value) => value.toLocaleLowerCase().includes(needle))) group.data.push(session);
  }
  const visible = groups.filter((g) => g.data.length || (!archived && !needle));
  return other.data.length || (!needle && visible.length) ? [...visible, other] : visible;
}
