"use client";
import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import * as Y from "yjs";
import { useAuthStore } from "@multica/core/auth";
import { configStore } from "@multica/core/config";
import { useWS } from "@multica/core/realtime";
import type { EditorCollaboration } from "../../editor/extensions";
import { COLLAB_FIELD, createDocumentCodec } from "../../editor/schema/codec";
import {
  acquireDocSession,
  colorFor,
  findDocSession,
  liveEditingOff,
  liveOffKey,
  peekDocLive,
  prefetchDocLive,
  releaseDocSession,
  type DocCollaborator,
  type DocSession,
  type DocSessionState,
  type LiveKind,
} from "./doc-live-session";

export type { DocCollaborator };

/** How long a first connection may take before the page edits without the
 *  live session. Saves then go through the API, which merges them in. */
const CONNECT_TIMEOUT_MS = 8000;
/** A first connection quicker than this is not announced. */
const CONNECTING_NOTICE_MS = 3000;

export type DocCollabState =
  /** Asking the server whether live editing is on. */
  | { mode: "checking" }
  /** Live editing is off or unreachable: edit through the API. `carried` is
   *  what was typed into the live session before it gave up, to save. */
  | { mode: "solo"; carried?: string | null }
  | {
      mode: "live";
      /** The shared document holds the doc; the editor can show it. */
      ready: boolean;
      /** The first sync is done. */
      synced: boolean;
      connected: boolean;
      /** Still connecting for the first time after a noticeable while. */
      slow: boolean;
      collaboration: EditorCollaboration;
      collaborators: DocCollaborator[];
      /** Changes the editor's key when the session starts over. */
      generation: number;
      /** The heading a collaborator's caret is under, if any. */
      locate: (key: number) => string | null;
      /** Calls back on every caret move, for views that show where people are. */
      watchCarets: (listener: () => void) => () => void;
    };

const IDLE_STATE: DocSessionState = { ready: false, synced: false, connected: false, collaborators: [], reset: false };
const noSubscribe = () => () => {};
const idleState = () => IDLE_STATE;

/** The doc as typed into the session, when that differs from `base`. */
function editedContent(session: DocSession, base: string): string | null {
  if (!session.edited) return null;
  try {
    const codec = createDocumentCodec({ cdnDomain: configStore.getState().cdnDomain });
    const typed = codec.ydocToMarkdown(session.ydoc);
    return typed === codec.serialize(codec.parse(base)) ? null : typed;
  } catch {
    return null;
  }
}

/** The text of the last heading at or above a collaborator's caret. */
export function locateCaret(session: DocSession, key: number): string | null {
  const cursor = (session.provider.awareness?.getStates().get(key) as { cursor?: { head?: unknown } } | undefined)?.cursor;
  if (!cursor?.head) return null;
  let position: Y.AbsolutePosition | null = null;
  try {
    position = Y.createAbsolutePositionFromRelativePosition(Y.createRelativePositionFromJSON(cursor.head), session.ydoc);
  } catch {
    return null;
  }
  if (!position) return null;
  const root = session.ydoc.getXmlFragment(COLLAB_FIELD);
  const blocks = root.toArray();
  let index = position.index;
  if (position.type !== root) {
    let block: Y.AbstractType<unknown> | null = position.type;
    while (block && block.parent !== root) block = block.parent as Y.AbstractType<unknown> | null;
    if (!block) return null;
    index = blocks.indexOf(block as (typeof blocks)[number]);
  }
  for (let i = Math.min(index, blocks.length - 1); i >= 0; i--) {
    const block = blocks[i];
    if (block instanceof Y.XmlElement && block.nodeName === "heading") {
      const text = plainText(block).trim();
      if (text) return text;
    }
  }
  return null;
}

function plainText(node: Y.XmlElement | Y.XmlText | Y.XmlHook): string {
  if (node instanceof Y.XmlText) {
    return (node.toDelta() as { insert?: unknown }[]).map((d) => (typeof d.insert === "string" ? d.insert : "")).join("");
  }
  if (node instanceof Y.XmlElement) return node.toArray().map(plainText).join("");
  return "";
}

/**
 * The live session of a doc, or of an issue's description (`kind`). A host
 * that stays mounted while `docId` changes gets a fresh state for each. With
 * `enabled` false nothing is asked and the host edits solo, as for a guest.
 */
export function useDocCollab(
  docId: string,
  content: string,
  kind: LiveKind = "doc",
  enabled = true,
): DocCollabState {
  const { collabUrl } = useWS();
  const user = useAuthStore((s) => s.user);
  const userId = user?.id ?? "";
  // A kept session or an answer fetched ahead of the page settles the mode
  // without waiting.
  const settle = () => {
    if (liveEditingOff.has(liveOffKey(collabUrl, kind))) return { mode: "solo" as const, document: null };
    const document =
      findDocSession(collabUrl, docId, userId, kind)?.document ??
      (() => {
        const ticket = peekDocLive(collabUrl, docId, userId, kind);
        return ticket?.enabled ? ticket.document : null;
      })();
    return document ? { mode: "live" as const, document } : { mode: "checking" as const, document: null };
  };
  const [initial] = useState(settle);
  const [mode, setMode] = useState<"checking" | "solo" | "live">(initial.mode);
  const [document, setDocument] = useState<string | null>(initial.document);
  const [carried, setCarried] = useState<string | null>(null);
  const [session, setSession] = useState<DocSession | null>(null);
  const target = `${kind}:${docId}`;
  const [settledTarget, setSettledTarget] = useState(target);
  if (settledTarget !== target) {
    const next = settle();
    setSettledTarget(target);
    setMode(next.mode);
    setDocument(next.document);
    setCarried(null);
    // The previous doc's session stays in state until its effect cleans up.
    setSession(null);
  }
  const [generation, setGeneration] = useState(0);
  const [slow, setSlow] = useState(false);
  const discard = useRef(false);
  const contentRef = useRef(content);
  contentRef.current = content;

  useEffect(() => {
    if (!enabled || mode !== "checking") return;
    let cancelled = false;
    prefetchDocLive(collabUrl, docId, userId, kind)
      .then((ticket) => {
        if (cancelled) return;
        setDocument(ticket.enabled ? ticket.document : null);
        setMode(ticket.enabled ? "live" : "solo");
      })
      .catch(() => {
        if (!cancelled) setMode("solo");
      });
    return () => {
      cancelled = true;
    };
  }, [enabled, mode, docId, collabUrl, userId, kind]);

  useEffect(() => {
    if (!enabled || mode !== "live" || !document || !userId) return;
    const next = acquireDocSession(collabUrl, docId, userId, document, kind);
    discard.current = false;
    setSession(next);
    return () => {
      releaseDocSession(next, { discard: discard.current });
      setSession(null);
    };
  }, [enabled, mode, document, docId, collabUrl, userId, generation, kind]);

  const state = useSyncExternalStore(session?.subscribe ?? noSubscribe, session?.getState ?? idleState);

  useEffect(() => {
    if (state.reset) setGeneration((g) => g + 1);
  }, [state.reset]);

  useEffect(() => {
    setSlow(false);
    if (!session || state.synced) return;
    const notice = setTimeout(() => setSlow(true), CONNECTING_NOTICE_MS);
    const giveUp = setTimeout(() => {
      setCarried(editedContent(session, contentRef.current));
      discard.current = true;
      setMode("solo");
    }, CONNECT_TIMEOUT_MS);
    return () => {
      clearTimeout(notice);
      clearTimeout(giveUp);
    };
  }, [session, state.synced]);

  const collaboration = useMemo<EditorCollaboration | null>(() => {
    if (!session || !user) return null;
    return {
      document: session.ydoc,
      provider: session.provider,
      user: { name: user.name || user.email, color: colorFor(user.id), id: user.id, kind: "member" },
    };
  }, [session, user]);

  const locate = useCallback((key: number) => (session ? locateCaret(session, key) : null), [session]);

  if (!enabled) return { mode: "solo" };
  if (mode === "solo") return { mode, carried };
  // A reset session is about to be replaced; nothing may keep editing it.
  if (mode === "checking" || !collaboration || !session || state.reset) return { mode: "checking" };
  return {
    mode: "live",
    ready: state.ready,
    synced: state.synced,
    connected: state.connected,
    slow,
    collaboration,
    collaborators: state.collaborators,
    generation: session.id,
    locate,
    watchCarets: session.watchAwareness,
  };
}
