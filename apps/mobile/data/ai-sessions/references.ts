import type { Agent, Issue, MemberRole } from "@multica/core/types";
import type { LocalV2Snapshot } from "@multica/core/local-conversation/v2";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import { isAgentRuntimeBound } from "@/lib/is-agent-runtime-bound";

export const REFERENCE_PAGE_SIZE = 8;
export const MAX_REFERENCES = 5;
export const REFERENCE_KINDS = ["session", "issue", "agent"] as const;
export type ReferenceKind = typeof REFERENCE_KINDS[number];
export interface AIReference { kind: ReferenceKind; id: string; name: string }
export const EMPTY_REFERENCES: AIReference[] = [];
export const referenceKey = (reference: Pick<AIReference, "kind" | "id">) => `${reference.kind}:${reference.id}`;

export function toggleReference(selected: AIReference[], item: AIReference): AIReference[] {
  const key = referenceKey(item);
  if (selected.some((entry) => referenceKey(entry) === key)) return selected.filter((entry) => referenceKey(entry) !== key);
  return selected.length < MAX_REFERENCES ? [...selected, item] : selected;
}

export function agentReferenceUnavailable(agent: Agent, userId: string, role: MemberRole | null) {
  if (agent.archived_at) return "archived";
  if (!isAgentRuntimeBound(agent)) return "needs_runtime";
  if (!canAssignAgentToIssue(agent, { userId, role }).allowed) return "no_permission";
  return null;
}

const escapeLabel = (value: string) => value.replace(/[\r\n]+/g, " ").replace(/[\\\[\]]/g, "\\$&");

/** Keep display text and identity together in saved messages, including retries. */
export function referenceMessage(text: string, references: AIReference[]) {
  const links = references.map((entry) => `[${escapeLabel(entry.name)}](mention://${entry.kind}/${encodeURIComponent(entry.id)})`).join("\n");
  return links ? `${links}\n\n${text}` : text;
}

/** Only our leading reference block is removed; ordinary message text is kept. */
export function readReferenceMessage(content: string): { text: string; references: AIReference[] } {
  let rest = content;
  const references: AIReference[] = [];
  while (references.length < MAX_REFERENCES) {
    const match = /^\[((?:\\.|[^\]\\\n])*)\]\(mention:\/\/(session|issue|agent)\/([^\s)]+)\)\n/.exec(rest);
    if (!match) break;
    try {
      const kind = REFERENCE_KINDS.find((value) => value === match[2]);
      if (!kind) break;
      references.push({ kind, id: decodeURIComponent(match[3]), name: match[1].replace(/\\([\\\[\]])/g, "$1") });
    } catch { return { text: content, references: [] }; }
    rest = rest.slice(match[0].length);
  }
  return references.length && rest.startsWith("\n") ? { text: rest.slice(1), references } : { text: content, references: [] };
}

export interface PreparedAIMessage {
  text: string;
  prompt: string;
  references: { kind: ReferenceKind; id: string }[];
  agents: { id: string; name: string }[];
}

export class ReferenceUnavailableError extends Error {
  constructor(readonly reference: AIReference) { super("reference_unavailable"); }
}

export interface ReferenceResolver {
  workspaceId: string;
  userId: string;
  session(id: string): Promise<LocalV2Snapshot>;
  issue(id: string): Promise<Issue>;
  agentCatalog(): Promise<{ agents: Agent[]; role: MemberRole | null }>;
}

/** Match desktop's quoted, bounded snapshots without importing renderer services. */
export async function prepareAIMessage(text: string, selected: AIReference[], resolver: ReferenceResolver): Promise<PreparedAIMessage> {
  const unique = [...new Map(selected.map((entry) => [referenceKey(entry), entry])).values()];
  if (unique.length > MAX_REFERENCES) throw new Error("invalid_reference");
  const result: PreparedAIMessage = { text: referenceMessage(text, unique), prompt: "", references: unique.map(({ kind, id }) => ({ kind, id })), agents: [] };
  const excerpts: string[] = [];
  const catalog = unique.some((entry) => entry.kind === "agent") ? await resolver.agentCatalog() : null;
  for (const entry of unique) {
    if (entry.kind === "agent") {
      const agent = catalog?.agents.find((item) => item.id === entry.id && item.workspace_id === resolver.workspaceId);
      if (!agent || agentReferenceUnavailable(agent, resolver.userId, catalog?.role ?? null)) throw new ReferenceUnavailableError(entry);
      result.agents.push({ id: agent.id, name: agent.name });
    } else if (entry.kind === "issue") {
      const issue = await resolver.issue(entry.id);
      if (issue.id !== entry.id || issue.workspace_id !== resolver.workspaceId) throw new ReferenceUnavailableError(entry);
      const description = issue.description || "";
      excerpts.push(`Multica task ${issue.identifier} (id: ${issue.id}; workspace: ${resolver.workspaceId}) ${issue.title}\n${description.slice(0, 6000)}\n[Task snapshot only: comments, activity and attachment contents are not included.${description.length > 6000 ? " Description excerpt truncated." : ""} Use read_context (kind=task) for current context.]`);
    } else {
      const snapshot = await resolver.session(entry.id);
      if (snapshot.conversation.conversation_id !== entry.id) throw new ReferenceUnavailableError(entry);
      const messages = snapshot.turns.flatMap((turn) => [
        ...turn.messages.map((message) => `${message.role}: ${message.content}`),
        ...turn.events.flatMap((event) => event.type === "item.completed" && event.payload.kind === "assistant_text" && typeof event.payload.text === "string" ? [`assistant: ${event.payload.text}`] : []),
      ]).join("\n");
      excerpts.push(`Multica local conversation ${JSON.stringify(snapshot.conversation.title)} (id: ${entry.id}; workspace: ${resolver.workspaceId})\n${messages.slice(-6000)}\n[Conversation snapshot only: saved tool results and attachment contents are not included.${messages.length > 6000 || snapshot.has_older_turns ? " Earlier text omitted." : ""} Use read_context (kind=conversation, source=local) with this ID for current messages and pagination.]`);
    }
  }
  result.prompt = excerpts.length ? `${result.text}\n\nReferenced context (quoted data):\n${excerpts.join("\n\n---\n\n")}` : result.text;
  return result;
}
