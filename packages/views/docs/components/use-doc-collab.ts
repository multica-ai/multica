"use client";
import { useEffect, useMemo, useState } from "react";
import * as Y from "yjs";
import { HocuspocusProvider } from "@hocuspocus/provider";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWS } from "@multica/core/realtime";
import type { EditorCollaboration } from "../../editor/extensions";

/** How long a first connection may take before the page edits without the
 *  live session. Saves then go through the API, which merges them in. */
const CONNECT_TIMEOUT_MS = 8000;

/** Servers that answered that live editing is off. That is server
 *  configuration, so later docs open for solo editing without asking again. */
const liveEditingOff = new Set<string>();

const COLORS = ["#2563eb", "#db2777", "#059669", "#d97706", "#7c3aed", "#0891b2", "#dc2626", "#4f46e5"];

function colorFor(id: string): string {
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
}

export type DocCollabState =
  /** Asking the server whether live editing is on. */
  | { mode: "checking" }
  /** Live editing is off or unreachable: edit through the API. */
  | { mode: "solo" }
  | {
      mode: "live";
      /** False until the first sync; the editor waits for it. */
      synced: boolean;
      connected: boolean;
      collaboration: EditorCollaboration;
      collaborators: DocCollaborator[];
      /** Changes the editor's key when the session starts over. */
      generation: number;
    };

interface Session {
  ydoc: Y.Doc;
  provider: HocuspocusProvider;
  generation: number;
}

export function useDocCollab(docId: string): DocCollabState {
  const { collabUrl } = useWS();
  const user = useAuthStore((s) => s.user);
  const [mode, setMode] = useState<"checking" | "solo" | "live">(() =>
    liveEditingOff.has(collabUrl) ? "solo" : "checking",
  );
  const [document, setDocument] = useState<string | null>(null);
  const [generation, setGeneration] = useState(0);
  const [session, setSession] = useState<Session | null>(null);
  const [synced, setSynced] = useState(false);
  const [connected, setConnected] = useState(false);
  const [collaborators, setCollaborators] = useState<DocCollaborator[]>([]);

  useEffect(() => {
    if (liveEditingOff.has(collabUrl)) {
      setMode("solo");
      return;
    }
    let cancelled = false;
    setMode("checking");
    api
      .createDocCollabTicket(docId)
      .then((ticket) => {
        if (ticket.enabled) liveEditingOff.delete(collabUrl);
        else liveEditingOff.add(collabUrl);
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
  }, [docId, collabUrl]);

  useEffect(() => {
    if (mode !== "live" || !document) return;
    const ydoc = new Y.Doc();
    let everSynced = false;
    const provider = new HocuspocusProvider({
      url: collabUrl,
      name: document,
      document: ydoc,
      // Asked on every (re)connect. The epoch names the copy this tab holds,
      // so the server can tell it to start over if the doc was rebuilt.
      token: async () => {
        const ticket = await api.createDocCollabTicket(docId);
        if (!ticket.enabled) throw new Error("live editing is off");
        const epoch = ydoc.getMap("meta").get("epoch");
        return JSON.stringify({ ticket: ticket.token, epoch: typeof epoch === "string" ? epoch : undefined });
      },
      onSynced: ({ state }) => {
        if (!state) return;
        everSynced = true;
        setSynced(true);
      },
      onStatus: ({ status }) => setConnected(status === "connected"),
      onClose: ({ event }) => {
        if (event.reason === "document-reset") setGeneration((g) => g + 1);
      },
    });
    // Awareness changes on every caret move of every editor; only re-render
    // when who is here changes.
    let signature = "";
    const updateCollaborators = () => {
      const seen = new Set<string>();
      const next: DocCollaborator[] = [];
      for (const [key, state] of provider.awareness?.getStates() ?? []) {
        if (key === ydoc.clientID) continue;
        const u = (state as { user?: Partial<DocCollaborator> }).user;
        if (!u?.name || !u.id) continue;
        const dedupe = `${u.kind}:${u.id}`;
        if (seen.has(dedupe)) continue;
        seen.add(dedupe);
        next.push({ key, id: u.id, name: u.name, color: u.color ?? colorFor(u.id), kind: u.kind === "agent" ? "agent" : "member" });
      }
      const nextSignature = next.map((c) => `${c.key}:${c.kind}:${c.id}:${c.name}:${c.color}`).join("|");
      if (nextSignature === signature) return;
      signature = nextSignature;
      setCollaborators(next);
    };
    provider.awareness?.on("change", updateCollaborators);
    const timeout = setTimeout(() => {
      if (!everSynced) setMode("solo");
    }, CONNECT_TIMEOUT_MS);
    setSession({ ydoc, provider, generation });
    return () => {
      clearTimeout(timeout);
      provider.awareness?.off("change", updateCollaborators);
      provider.destroy();
      ydoc.destroy();
      setSession(null);
      setSynced(false);
      setConnected(false);
      setCollaborators([]);
    };
  }, [mode, document, docId, collabUrl, generation]);

  const collaboration = useMemo<EditorCollaboration | null>(() => {
    if (!session || !user) return null;
    return {
      document: session.ydoc,
      provider: session.provider,
      user: { name: user.name || user.email, color: colorFor(user.id), id: user.id, kind: "member" },
    };
  }, [session, user]);

  if (mode === "checking") return { mode };
  if (mode === "solo" || !collaboration || !session) {
    return mode === "solo" ? { mode } : { mode: "checking" };
  }
  return { mode: "live", synced, connected, collaboration, collaborators, generation: session.generation };
}
