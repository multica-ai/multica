import type { AvatarActor } from "../components/Avatar";

export interface MentionTarget {
  type: "member" | "agent" | "squad" | "issue";
  id: string;
  /** A person's or squad's name, or an issue's identifier. */
  label: string;
}

export interface MentionCandidate extends MentionTarget {
  actor?: AvatarActor;
  detail?: string;
}

/** What the composer shows for a mention: `@Name`, or an issue's bare identifier. */
export function mentionToken(target: MentionTarget): string {
  return target.type === "issue" ? target.label : `@${target.label}`;
}

/** The link Web and Desktop write for a mention. */
export function mentionMarkdown(target: MentionTarget): string {
  return `[${mentionToken(target)}](mention://${target.type}/${target.id})`;
}

const LONGEST_QUERY = 40;

/** The `@query` being typed at the cursor, if any. */
export function activeMention(text: string, cursor: number): { start: number; query: string } | null {
  for (let i = cursor - 1; i >= 0 && cursor - i <= LONGEST_QUERY + 1; i -= 1) {
    const ch = text[i]!;
    if (ch === "@") return i === 0 || /\s/.test(text[i - 1]!) ? { start: i, query: text.slice(i + 1, cursor) } : null;
    if (/\s/.test(ch)) return null;
  }
  return null;
}

/** Replaces the `@query` from `start` to the cursor with the mention and a space. */
export function insertMention(text: string, start: number, cursor: number, target: MentionTarget): { text: string; cursor: number } {
  const token = `${mentionToken(target)} `;
  return { text: text.slice(0, start) + token + text.slice(cursor), cursor: start + token.length };
}

function escape(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/** Turns the mentions still present in the text back into links, for sending. */
export function withMentionLinks(text: string, targets: readonly MentionTarget[]): string {
  const unique = new Map(targets.map((target) => [mentionToken(target), target]));
  const tokens = [...unique.keys()].sort((a, b) => b.length - a.length);
  if (!tokens.length) return text;
  const pattern = new RegExp(`(^|[^\\w\\[])(${tokens.map(escape).join("|")})(?![\\w-])`, "g");
  return text.replace(pattern, (_match, before: string, token: string) => before + mentionMarkdown(unique.get(token)!));
}

const LINK = /\[([^\]]+)\]\(mention:\/\/(member|agent|squad|issue)\/([^)\s]+)\)/g;

/** A stored body as the composer edits it: mention links become their tokens again. */
export function editableMentions(markdown: string): { text: string; targets: MentionTarget[] } {
  const targets: MentionTarget[] = [];
  const text = markdown.replace(LINK, (_match, label: string, type: MentionTarget["type"], id: string) => {
    const target = { type, id, label: label.replace(/^@/, "") };
    targets.push(target);
    return mentionToken(target);
  });
  return { text, targets };
}

/** Candidates whose label contains the query, those starting with it first. */
export function matchMentions(candidates: readonly MentionCandidate[], query: string, limit = 6): MentionCandidate[] {
  const q = query.toLowerCase();
  const scored = candidates
    .map((candidate) => ({ candidate, at: candidate.label.toLowerCase().indexOf(q) }))
    .filter((entry) => entry.at >= 0)
    .sort((a, b) => Number(a.at !== 0) - Number(b.at !== 0));
  return scored.slice(0, limit).map((entry) => entry.candidate);
}
