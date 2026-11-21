"use client";
import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { useAuthStore } from "@multica/core/auth";
import { configStore } from "@multica/core/config";
import { useWS } from "@multica/core/realtime";
import type { EditorCollaboration } from "../../editor/extensions";
import { createDocumentCodec } from "../../editor/schema/codec";
import {
  acquireDocSession,
  colorFor,
  findDocSession,
  liveEditingOff,
  peekDocLive,
  prefetchDocLive,
  releaseDocSession,
  type DocCollaborator,
  type DocSession,
  type DocSessionState,
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

export function useDocCollab(docId: string, content: string): DocCollabState {
  const { collabUrl } = useWS();
  const user = useAuthStore((s) => s.user);
  const userId = user?.id ?? "";
  // A kept session or an answer fetched ahead of the page settles the mode
  // without waiting.
  const [initial] = useState(() => {
    if (liveEditingOff.has(collabUrl)) return { mode: "solo" as const, document: null };
    const document =
      findDocSession(collabUrl, docId, userId)?.document ??
      (() => {
        const ticket = peekDocLive(collabUrl, docId, userId);
        return ticket?.enabled ? ticket.document : null;
      })();
    return document ? { mode: "live" as const, document } : { mode: "checking" as const, document: null };
  });
  const [mode, setMode] = useState<"checking" | "solo" | "live">(initial.mode);
  const [document, setDocument] = useState<string | null>(initial.document);
  const [carried, setCarried] = useState<string | null>(null);
  const [generation, setGeneration] = useState(0);
  const [session, setSession] = useState<DocSession | null>(null);
  const [slow, setSlow] = useState(false);
  const discard = useRef(false);
  const contentRef = useRef(content);
  contentRef.current = content;

  useEffect(() => {
    if (mode !== "checking") return;
    let cancelled = false;
    prefetchDocLive(collabUrl, docId, userId)
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
  }, [mode, docId, collabUrl, userId]);

  useEffect(() => {
    if (mode !== "live" || !document || !userId) return;
    const next = acquireDocSession(collabUrl, docId, userId, document);
    discard.current = false;
    setSession(next);
    return () => {
      releaseDocSession(next, { discard: discard.current });
      setSession(null);
    };
  }, [mode, document, docId, collabUrl, userId, generation]);

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
  };
}
