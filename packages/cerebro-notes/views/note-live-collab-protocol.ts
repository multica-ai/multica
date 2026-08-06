/**
 * Wire types + pure helpers for live co-editing in notes (FIR-1317).
 *
 * The server is a prosemirror-collab authority: it orders steps and relays
 * carets and presence, but never touches document content. Everything in this
 * file is deliberately free of React and of the editor instance so the rules
 * that decide what the user sees can be tested on their own.
 */

export interface LivePeer {
  id: string;
  user_id: string;
  name: string;
  color: string;
  can_edit: boolean;
}

export interface LiveSnapshot {
  version: number;
  doc: unknown;
}

export interface RemoteCaret {
  peerId: string;
  name: string;
  color: string;
  anchor: number;
  head: number;
}

export type ServerMessage =
  | {
      type: "welcome";
      peer_id: string;
      color: string;
      can_edit: boolean;
      version: number;
      snapshot?: LiveSnapshot | null;
      /** Steps submitted after `snapshot.version`, replayed to catch us up. */
      steps?: { version: number; client_id: string; step: unknown }[];
      peers: LivePeer[];
    }
  | { type: "peers"; peers: LivePeer[] }
  | {
      type: "steps_applied";
      version: number;
      steps: { version: number; client_id: string; step: unknown }[];
    }
  | {
      type: "rejected";
      version: number;
      missing: { version: number; client_id: string; step: unknown }[];
    }
  | {
      type: "caret";
      peer_id: string;
      name: string;
      color: string;
      anchor: number;
      head: number;
    }
  | { type: "snapshot_request" }
  | { type: "reload"; reason?: string }
  | { type: "auth_ack" }
  | { type: "auth_error"; reason?: string }
  | { type: "error"; reason?: string }
  | { type: "pong" };

/**
 * liveCollabUrl turns the REST base URL into the WebSocket URL for a note's
 * live session. It swaps the scheme in place so http→ws and https→wss, which
 * is what keeps this working behind the Cloudflare tunnel as well as on a
 * local dev server.
 */
export function liveCollabUrl(baseUrl: string, noteId: string): string {
  const base = baseUrl.replace(/^http/, "ws").replace(/\/+$/, "");
  return `${base}/api/cerebro/notes/${noteId}/collab`;
}

/**
 * otherPeers drops my own connection from the presence list. Two tabs of the
 * same person are two peers on the server (each has its own caret), so the
 * filter is on the connection id, not the user id.
 */
export function otherPeers(peers: LivePeer[], myPeerId: string): LivePeer[] {
  return peers.filter((p) => p.id !== myPeerId);
}

/**
 * upsertCaret keeps one caret per peer — a moving caret replaces its previous
 * position instead of leaving a trail behind.
 */
export function upsertCaret(
  carets: RemoteCaret[],
  next: RemoteCaret,
): RemoteCaret[] {
  const rest = carets.filter((c) => c.peerId !== next.peerId);
  return [...rest, next];
}

/**
 * pruneCarets removes carets belonging to peers who have left, so a closed tab
 * does not leave a ghost caret sitting in the note.
 */
export function pruneCarets(
  carets: RemoteCaret[],
  peers: LivePeer[],
): RemoteCaret[] {
  const live = new Set(peers.map((p) => p.id));
  return carets.filter((c) => live.has(c.peerId));
}

/**
 * clampCaret keeps a remote position inside the local document. Positions are
 * produced against the sender's document, and ours can be a step behind, so an
 * unclamped position throws when ProseMirror resolves it.
 */
export function clampCaret(pos: number, docSize: number): number {
  if (!Number.isFinite(pos) || pos < 0) return 0;
  const max = Math.max(0, docSize);
  return Math.min(pos, max);
}

/**
 * presenceLabel is the "who is here" line shown above the editor.
 */
export function presenceLabel(peers: LivePeer[]): string {
  const [first, second] = peers;
  if (!first) return "";
  if (!second) return `${first.name} is editing this note`;
  if (peers.length === 2) {
    return `${first.name} and ${second.name} are editing this note`;
  }
  return `${first.name} and ${peers.length - 1} others are editing this note`;
}
