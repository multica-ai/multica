"use client";
import * as Y from "yjs";
import {
  HocuspocusProvider,
  HocuspocusProviderWebsocket,
  WebSocketStatus,
} from "@hocuspocus/provider";
import { api } from "@multica/core/api";
import type { DocCollabTicket } from "@multica/core/types";

/**
 * Live sessions for docs, kept outside React so opening a doc does as little
 * as possible in front of the reader:
 *
 * - One WebSocket per collaboration server, shared by every open doc, so a
 *   doc joins over a socket that is usually already open.
 * - The ticket and the doc's saved Yjs state come in one request, made as
 *   soon as the doc is opened or hovered. The state fills the shared document
 *   before the session syncs, so the editor shows the doc at once.
 * - A session outlives its page for a while, so going back to a doc finds it
 *   connected and current.
 */

/** Servers that answered that live editing is off. That is server
 *  configuration, so later docs open for solo editing without asking again.
 *  Docs are keyed by the server's URL, other kinds by `liveOffKey`: each kind
 *  is switched on separately. */
export const liveEditingOff = new Set<string>();
/** Servers that answered that live editing is on; their socket can open
 *  before the next answer arrives. */
const liveEditingOn = new Set<string>();

const COLORS = ["#2563eb", "#db2777", "#059669", "#d97706", "#7c3aed", "#0891b2", "#dc2626", "#4f46e5"];

export function colorFor(id: string): string {
  let hash = 0;
  for (const char of id) hash = (hash * 31 + char.charCodeAt(0)) | 0;
  return COLORS[Math.abs(hash) % COLORS.length]!;
}

/** Someone else in the session: a person, or an agent writing through the API. */
export interface DocCollaborator {
  key: number;
  id: string;
  name: string;
  color: string;
  kind: "member" | "agent";
  /** Has a caret in the document, as opposed to only having it open. */
  editing: boolean;
}

// ---- Ticket and saved state --------------------------------------------------

/** How long a fetched ticket and state are used to open the doc. The state
 *  only has to be close: the session brings it up to date. */
const LIVE_INFO_MAX_AGE_MS = 30_000;
/** A ticket this close to expiring is not used for a new connection. */
const TICKET_MARGIN_MS = 30_000;

type LiveTicket = Extract<DocCollabTicket, { enabled: true }>;

interface LiveInfo {
  at: number;
  promise: Promise<DocCollabTicket>;
  value?: DocCollabTicket;
}

/** What a session edits: a doc, or the description of an issue. */
export type LiveKind = "doc" | "issue";

export const liveOffKey = (collabUrl: string, kind: LiveKind) =>
  kind === "doc" ? collabUrl : `${kind}\n${collabUrl}`;

function requestTicket(kind: LiveKind, id: string, options?: { state?: boolean }): Promise<DocCollabTicket> {
  return kind === "issue" ? api.createIssueCollabTicket(id, options) : api.createDocCollabTicket(id, options);
}

/** Tickets are personal, so answers are kept per user like sessions are. */
const liveInfos = new Map<string, LiveInfo>();
const infoKey = (collabUrl: string, docId: string, userId: string, kind: LiveKind) =>
  `${collabUrl}\n${kind === "doc" ? docId : `${kind}:${docId}`}\n${userId}`;

/**
 * Asks for the doc's ticket and saved state, unless a recent answer is on
 * hand or on its way. Also opens the shared socket of a server known to edit
 * live, so it is up by the time the doc joins.
 */
export function prefetchDocLive(
  collabUrl: string,
  docId: string,
  userId: string,
  kind: LiveKind = "doc",
): Promise<DocCollabTicket> {
  const key = infoKey(collabUrl, docId, userId, kind);
  const cached = liveInfos.get(key);
  if (cached && Date.now() - cached.at < LIVE_INFO_MAX_AGE_MS) return cached.promise;
  const info: LiveInfo = { at: Date.now(), promise: Promise.resolve({ enabled: false }) };
  info.promise = requestTicket(kind, docId, { state: true }).then(
    (ticket) => {
      info.value = ticket;
      if (ticket.enabled) {
        liveEditingOff.delete(liveOffKey(collabUrl, kind));
        if (!liveEditingOn.has(collabUrl)) warmSocket(collabUrl);
        liveEditingOn.add(collabUrl);
      } else {
        liveEditingOff.add(liveOffKey(collabUrl, kind));
        if (kind === "doc") liveEditingOn.delete(collabUrl);
      }
      return ticket;
    },
    (error: unknown) => {
      if (liveInfos.get(key) === info) liveInfos.delete(key);
      throw error;
    },
  );
  liveInfos.set(key, info);
  // A hovered doc that is never opened must not hold its state for good.
  setTimeout(() => {
    if (liveInfos.get(key) === info) liveInfos.delete(key);
  }, LIVE_INFO_MAX_AGE_MS);
  if (liveEditingOn.has(collabUrl)) warmSocket(collabUrl);
  return info.promise;
}

/** The answer of a recent `prefetchDocLive`, if it has arrived. */
export function peekDocLive(
  collabUrl: string,
  docId: string,
  userId: string,
  kind: LiveKind = "doc",
): DocCollabTicket | undefined {
  const info = liveInfos.get(infoKey(collabUrl, docId, userId, kind));
  return info && Date.now() - info.at < LIVE_INFO_MAX_AGE_MS ? info.value : undefined;
}

/** Takes the answer for a session about to start: its state describes the
 *  doc only until the session moves on, so it is used once. */
function takeDocLive(collabUrl: string, docId: string, userId: string, kind: LiveKind): LiveTicket | undefined {
  const value = peekDocLive(collabUrl, docId, userId, kind);
  liveInfos.delete(infoKey(collabUrl, docId, userId, kind));
  return value?.enabled ? value : undefined;
}

function ticketUsable(ticket: LiveTicket | null): ticket is LiveTicket {
  if (!ticket) return false;
  const expires = Date.parse(ticket.expiresAt);
  return Number.isFinite(expires) && expires - Date.now() > TICKET_MARGIN_MS;
}

// ---- Shared sockets ------------------------------------------------------------

/** How long a socket nobody uses stays open. */
const SOCKET_IDLE_MS = 60_000;

interface SharedSocket {
  socket: HocuspocusProviderWebsocket;
  users: number;
  idle: ReturnType<typeof setTimeout> | null;
}

const sockets = new Map<string, SharedSocket>();

function acquireSocket(url: string): HocuspocusProviderWebsocket {
  let shared = sockets.get(url);
  if (!shared) {
    shared = { socket: new HocuspocusProviderWebsocket({ url }), users: 0, idle: null };
    sockets.set(url, shared);
  }
  if (shared.idle) clearTimeout(shared.idle);
  shared.idle = null;
  shared.users++;
  return shared.socket;
}

function releaseSocket(url: string) {
  const shared = sockets.get(url);
  if (!shared) return;
  shared.users = Math.max(0, shared.users - 1);
  if (shared.users > 0 || shared.idle) return;
  shared.idle = setTimeout(() => {
    if (sockets.get(url) !== shared || shared.users > 0) return;
    sockets.delete(url);
    shared.socket.destroy();
  }, SOCKET_IDLE_MS);
}

/** Opens the server's socket ahead of a doc joining it. */
export function warmSocket(url: string) {
  acquireSocket(url);
  releaseSocket(url);
}

// ---- Sessions ------------------------------------------------------------------

/** How long a session stays connected after its page closes. */
const SESSION_KEEP_MS = 60_000;
/** Sessions kept for pages that are closed; the oldest goes first. */
const MAX_KEPT_SESSIONS = 5;

export interface DocSessionState {
  /** The shared document holds the doc, from its saved state or the first
   *  sync, so the editor can show it. */
  ready: boolean;
  /** The first sync with the server is done. */
  synced: boolean;
  connected: boolean;
  collaborators: DocCollaborator[];
  /** The server rebuilt the doc: this session is over and a new one starts
   *  from the server's copy. */
  reset: boolean;
}

let nextSessionId = 1;

export class DocSession {
  readonly id = nextSessionId++;
  readonly ydoc = new Y.Doc();
  readonly provider: HocuspocusProvider;
  /** Changed by this tab since it was filled, not by the server. */
  edited = false;
  private state: DocSessionState;
  private readonly listeners = new Set<() => void>();
  private users = 0;
  private keep: ReturnType<typeof setTimeout> | null = null;
  private ticket: LiveTicket | null;
  private destroyed = false;

  constructor(
    readonly key: string,
    private readonly collabUrl: string,
    private readonly docId: string,
    readonly document: string,
    seed: LiveTicket | undefined,
    private readonly kind: LiveKind = "doc",
  ) {
    this.ticket = seed ?? null;
    let seeded = false;
    if (seed?.state) {
      try {
        Y.applyUpdate(this.ydoc, base64ToBytes(seed.state));
        seeded = true;
      } catch {
        // An unreadable state is ignored; the first sync fills the document.
      }
    }
    const socket = acquireSocket(collabUrl);
    this.state = {
      ready: seeded,
      synced: false,
      connected: socket.status === WebSocketStatus.Connected,
      collaborators: [],
      reset: false,
    };
    this.provider = new HocuspocusProvider({
      websocketProvider: socket,
      name: document,
      document: this.ydoc,
      // Asked on every (re)join. The epoch names the copy this tab holds, so
      // the server can tell it to start over if the doc was rebuilt.
      token: async () => {
        const ticket = ticketUsable(this.ticket) ? this.ticket : await requestTicket(this.kind, this.docId);
        this.ticket = null;
        if (!ticket.enabled) throw new Error("live editing is off");
        const epoch = this.ydoc.getMap("meta").get("epoch");
        return JSON.stringify({ ticket: ticket.token, epoch: typeof epoch === "string" ? epoch : undefined });
      },
      onSynced: ({ state }) => {
        if (state) this.update({ synced: true, ready: true });
      },
      onStatus: ({ status }) => this.update({ connected: status === WebSocketStatus.Connected }),
      onClose: ({ event }) => {
        if (event.reason === "document-reset") this.update({ reset: true });
      },
    });
    this.provider.attach();
    this.ydoc.on("update", (_update: Uint8Array, origin: unknown) => {
      if (origin !== this.provider) this.edited = true;
    });
    this.provider.awareness?.on("change", this.updateCollaborators);
  }

  getState = (): DocSessionState => this.state;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  private update(patch: Partial<DocSessionState>) {
    const next = { ...this.state, ...patch };
    if ((Object.keys(patch) as (keyof DocSessionState)[]).every((k) => next[k] === this.state[k])) return;
    this.state = next;
    if (next.reset) dropSession(this);
    for (const listener of this.listeners) listener();
  }

  // Awareness changes on every caret move of every editor; only re-render
  // when who is here changes.
  private signature = "";
  private updateCollaborators = () => {
    const seen = new Set<string>();
    const next: DocCollaborator[] = [];
    for (const [key, state] of this.provider.awareness?.getStates() ?? []) {
      if (key === this.ydoc.clientID) continue;
      const { user: u, cursor } = state as { user?: Partial<DocCollaborator>; cursor?: unknown };
      if (!u?.name || !u.id) continue;
      const dedupe = `${u.kind}:${u.id}`;
      const editing = !!cursor;
      if (seen.has(dedupe)) {
        // The same person in two tabs is editing if either tab is.
        if (editing) {
          const index = next.findIndex((c) => `${c.kind}:${c.id}` === dedupe);
          if (index >= 0 && !next[index]!.editing) next[index] = { ...next[index]!, key, editing };
        }
        continue;
      }
      seen.add(dedupe);
      next.push({ key, id: u.id, name: u.name, color: u.color ?? colorFor(u.id), kind: u.kind === "agent" ? "agent" : "member", editing });
    }
    const signature = next.map((c) => `${c.key}:${c.kind}:${c.id}:${c.name}:${c.color}:${c.editing}`).join("|");
    if (signature === this.signature) return;
    this.signature = signature;
    this.update({ collaborators: next });
  };

  /** Calls `listener` on every awareness change, caret moves included. */
  watchAwareness = (listener: () => void): (() => void) => {
    const awareness = this.provider.awareness;
    awareness?.on("change", listener);
    return () => awareness?.off("change", listener);
  };

  /** Out of the cache: no page joins it again, and it ends with its last user. */
  dropped = false;

  /** @internal */
  retain() {
    if (this.keep) clearTimeout(this.keep);
    this.keep = null;
    this.users++;
    // Fields such as the editor's user can only be set on a present state.
    const awareness = this.provider.awareness;
    if (awareness && awareness.getLocalState() === null) awareness.setLocalState({});
  }

  /** @internal Reports whether the session is now kept for later. */
  release(): boolean {
    this.users = Math.max(0, this.users - 1);
    if (this.users > 0) return false;
    if (this.dropped) {
      this.destroy();
      return false;
    }
    // Nobody is on the page any more, so others stop seeing this tab in it.
    this.provider.awareness?.setLocalState(null);
    this.keep = setTimeout(() => dropSession(this), SESSION_KEEP_MS);
    return true;
  }

  get idle() {
    return this.users === 0;
  }

  /** @internal */
  destroy() {
    if (this.destroyed) return;
    this.destroyed = true;
    if (this.keep) clearTimeout(this.keep);
    this.provider.awareness?.off("change", this.updateCollaborators);
    this.provider.destroy();
    this.ydoc.destroy();
    releaseSocket(this.collabUrl);
    this.listeners.clear();
  }
}

/** Live sessions by server, doc and user, oldest first. */
const sessions = new Map<string, DocSession>();

const sessionKey = infoKey;

function dropSession(session: DocSession) {
  if (sessions.get(session.key) === session) sessions.delete(session.key);
  session.dropped = true;
  if (session.idle) session.destroy();
}

/**
 * The doc's live session, joined or started. A new session is filled from
 * the saved state a recent `prefetchDocLive` brought, if any. Pair with
 * `releaseDocSession`.
 */
export function acquireDocSession(
  collabUrl: string,
  docId: string,
  userId: string,
  document: string,
  kind: LiveKind = "doc",
): DocSession {
  const key = sessionKey(collabUrl, docId, userId, kind);
  let session = sessions.get(key);
  if (session) {
    sessions.delete(key);
  } else {
    session = new DocSession(key, collabUrl, docId, document, takeDocLive(collabUrl, docId, userId, kind), kind);
  }
  sessions.set(key, session);
  session.retain();
  return session;
}

/** Hands the session back. It stays connected for a while unless `discard`
 *  ends it now, as when the page stops editing live. */
export function releaseDocSession(session: DocSession, opts: { discard?: boolean } = {}) {
  if (opts.discard && sessions.get(session.key) === session) sessions.delete(session.key);
  if (opts.discard) session.dropped = true;
  if (!session.release()) return;
  const kept = [...sessions.values()].filter((s) => s.idle);
  for (const old of kept.slice(0, Math.max(0, kept.length - MAX_KEPT_SESSIONS))) dropSession(old);
}

/** The doc's live session, if one is open or kept. */
export function findDocSession(
  collabUrl: string,
  docId: string,
  userId: string,
  kind: LiveKind = "doc",
): DocSession | undefined {
  return sessions.get(sessionKey(collabUrl, docId, userId, kind));
}

function base64ToBytes(base64: string): Uint8Array {
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}
