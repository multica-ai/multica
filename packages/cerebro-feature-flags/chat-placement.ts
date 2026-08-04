// FIR-4350 — where each conversation type is shown: the Chat page, the Inbox,
// or both. Per user, stored in the `user.preferences` blob under
// CHAT_PLACEMENT_KEY (the same mechanism cerebro_inbox_favorites uses — no
// table, no endpoint).
//
// Pure module on purpose: every consumer (the Chat page, the sidebar badge,
// both inbox implementations) reads the same predicate, and it is unit-testable
// without React or a query client.

/** The three conversation types the inbox already merges as one family. */
export type ConversationKind = "channel" | "dm" | "agent_chat";

export const CONVERSATION_KINDS: readonly ConversationKind[] = [
  "channel",
  "dm",
  "agent_chat",
];

/** Where one conversation type is shown. At least one is always true. */
export interface PlacementEntry {
  chat: boolean;
  inbox: boolean;
}

export type ChatPlacement = Record<ConversationKind, PlacementEntry>;

export const CHAT_PLACEMENT_KEY = "cerebro_chat_placement";

/**
 * FIR-4350 — agent chats live ONLY in the Inbox (Jesper, 2026-08-04). Their
 * placement is fixed inbox-only, not user-configurable: the settings matrix
 * omits the row and readPlacement ignores any stored legacy value.
 */
const AGENT_PLACEMENT: PlacementEntry = { chat: false, inbox: true };

/**
 * When the feature is turned on, conversations move to Chat and leave the Inbox
 * — that is the whole point of the feature (FIR-4350). Placement only takes
 * effect while cerebro_chat_page is on (see chat-inbox-hiding.ts), so a user who
 * never enables Chat still sees the Inbox unchanged. A malformed blob lands here
 * too. At least one place is always on, so no type ends up nowhere.
 */
export const DEFAULT_PLACEMENT: ChatPlacement = {
  channel: { chat: true, inbox: false },
  dm: { chat: true, inbox: false },
  agent_chat: AGENT_PLACEMENT,
};

function readEntry(value: unknown, fallback: PlacementEntry): PlacementEntry {
  if (!value || typeof value !== "object") return fallback;
  const raw = value as Record<string, unknown>;
  const chat = raw.chat === true;
  const inbox = raw.inbox === true;
  // A stored entry with both places off would make the type invisible while it
  // still collects unread. The UI prevents it; a hand-edited or half-written
  // blob is repaired here rather than trusted.
  if (!chat && !inbox) return fallback;
  return { chat, inbox };
}

/**
 * Read placement from an untrusted preferences blob. Every missing or malformed
 * part falls back to {@link DEFAULT_PLACEMENT}, per the API Response
 * Compatibility rule in CLAUDE.md.
 */
export function readPlacement(value: unknown): ChatPlacement {
  if (!value || typeof value !== "object") return DEFAULT_PLACEMENT;
  const raw = value as Record<string, unknown>;
  return {
    channel: readEntry(raw.channel, DEFAULT_PLACEMENT.channel),
    dm: readEntry(raw.dm, DEFAULT_PLACEMENT.dm),
    // Agents are inbox-only and not configurable — any stored value is ignored.
    agent_chat: AGENT_PLACEMENT,
  };
}

/** Does this conversation type belong on the Chat page? */
export function showsInChat(placement: ChatPlacement, kind: ConversationKind): boolean {
  return placement[kind].chat;
}

/**
 * Does this conversation type belong in the Inbox? A type that is not shown in
 * the Inbox is HIDDEN from the list — never deleted, archived or marked read.
 */
export function showsInInbox(placement: ChatPlacement, kind: ConversationKind): boolean {
  return placement[kind].inbox;
}

/** The conversation types currently hidden from the Inbox list. */
export function hiddenFromInbox(placement: ChatPlacement): ReadonlySet<ConversationKind> {
  return new Set(CONVERSATION_KINDS.filter((k) => !placement[k].inbox));
}

/**
 * Map a channel's `kind` field onto a ConversationKind. Groups render like DMs
 * everywhere else (FIR-2159), so they are placed with DMs here too.
 */
export function conversationKindOfChannel(channelKind: string | undefined): ConversationKind {
  return channelKind === "dm" || channelKind === "group" ? "dm" : "channel";
}

/**
 * May this toggle be turned off? No — when it is the type's last remaining
 * place. The settings UI disables it rather than letting a type end up nowhere.
 */
export function canTurnOff(
  placement: ChatPlacement,
  kind: ConversationKind,
  place: keyof PlacementEntry,
): boolean {
  const entry = placement[kind];
  if (!entry[place]) return true; // already off
  return place === "chat" ? entry.inbox : entry.chat;
}

/** Apply one toggle, refusing a change that would leave the type nowhere. */
export function setPlace(
  placement: ChatPlacement,
  kind: ConversationKind,
  place: keyof PlacementEntry,
  next: boolean,
): ChatPlacement {
  if (!next && !canTurnOff(placement, kind, place)) return placement;
  return { ...placement, [kind]: { ...placement[kind], [place]: next } };
}
