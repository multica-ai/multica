import { randomUUID } from "node:crypto";
import * as Y from "yjs";
import type { Document, Hocuspocus } from "@hocuspocus/server";
import type { DocumentCodec } from "@multica/views/editor/schema/codec";
import type { CollabApi, StoreRequest } from "./api";
import { applyTextEdits, type EditFailure, type TextEdit } from "./edits";
import { mergeDocuments, type MergeConflict } from "./merge";
import type { Actor } from "./ticket";

/** Close code telling a client its local copy belongs to a document the
 *  server has since rebuilt from Markdown: it must start over from the
 *  server's copy, because merging the two histories would duplicate every
 *  block. */
export const RESET_CLOSE_CODE = 4409;
const EPOCH_KEY = "epoch";
const META_MAP = "meta";

export function parseDocumentName(name: string): { type: "doc"; id: string } | null {
  const match = /^doc:([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$/i.exec(name);
  return match ? { type: "doc", id: match[1]!.toLowerCase() } : null;
}

export function documentEpoch(doc: Y.Doc): string | undefined {
  return doc.getMap(META_MAP).get(EPOCH_KEY) as string | undefined;
}

/** Transaction origin for changes the server makes itself. Hocuspocus does
 *  not schedule a store for it; the caller persists explicitly. */
const serverOrigin = (actor: Actor) => ({ source: "local" as const, skipStoreHooks: true, context: { actor } });

interface DocState {
  id: string;
  /** Stored revision the live document was last persisted as. */
  revision: number;
  /** Canonical Markdown of that revision. */
  persisted: string;
  /** The stored Markdown exactly as loaded, until the first change: agents
   *  quote text from it, and it can differ from the canonical form. */
  loadedRaw: string | null;
  /** Editors whose changes are not persisted yet. */
  contributors: Map<string, Actor>;
  /** Serializes persistence and server-side writes per document. */
  queue: Promise<unknown>;
}

export interface WriteRequest {
  actor: Actor;
  /** Stored revision and content the writer based `content` on. */
  base_revision?: number;
  base_content?: string;
  content?: string;
  title?: string;
  message?: string;
  action?: "edit" | "restore";
  action_ref?: number;
  task_id?: string;
}

export interface EditRequest {
  actor: Actor;
  edits: TextEdit[];
  message?: string;
  task_id?: string;
}

export type WriteResult =
  | { status: "ok"; revision: number; content: string; title: string; merged: boolean }
  | { status: "conflict"; revision: number; content: string; conflicts: MergeConflict[] }
  | { status: "edit_failed"; revision: number; content: string; failure: EditFailure }
  | { status: "gone" };

class GoneError extends Error {}

interface Logger {
  info(msg: string, fields?: Record<string, unknown>): void;
  warn(msg: string, fields?: Record<string, unknown>): void;
}

export class DocumentStore {
  private readonly states = new Map<string, DocState>();
  private readonly agentPresence = new Map<string, NodeJS.Timeout>();
  hocuspocus!: Hocuspocus;

  constructor(
    private readonly api: CollabApi,
    private readonly codec: DocumentCodec,
    private readonly log: Logger,
  ) {}

  // ---- Hocuspocus hooks ----------------------------------------------------

  /** Builds the live document from the saved Yjs state, bringing it up to
   *  the stored Markdown when the doc was written without this service (it
   *  was down, or an older API instance wrote during a deploy). Clients that
   *  kept editing offline still share that state's history, so their edits
   *  merge in. Only without a usable saved state is the document rebuilt
   *  from Markdown under a new epoch, which resets every client. */
  async load(name: string): Promise<Uint8Array> {
    const parsed = parseDocumentName(name);
    if (!parsed) throw new Error(`unknown document ${name}`);
    const stored = await this.api.loadDoc(parsed.id);
    if (!stored) throw new GoneError(`doc ${parsed.id} not found`);

    let ydoc: Y.Doc | null = null;
    let from = "markdown";
    if (stored.ystate && stored.ystate_revision !== null && stored.ystate_revision <= stored.revision) {
      const candidate = new Y.Doc();
      Y.applyUpdate(candidate, Buffer.from(stored.ystate, "base64"));
      if (documentEpoch(candidate)) {
        ydoc = candidate;
        from = "state";
        const target = this.codec.serialize(this.codec.parse(stored.content));
        if (this.codec.ydocToMarkdown(candidate) !== target) {
          this.codec.applyJSON(candidate, this.codec.parse(stored.content), serverOrigin({ type: "member", id: "", name: "" }));
          from = "state+catch-up";
        }
      }
    }
    if (!ydoc) {
      ydoc = this.codec.createYDoc(stored.content);
      ydoc.getMap(META_MAP).set(EPOCH_KEY, randomUUID());
    }
    if (from !== "state" || stored.ystate_revision !== stored.revision) {
      // Save the state now: loading again would otherwise redo the catch-up
      // or rebuild with new Yjs items and duplicate text for clients that
      // already hold this one.
      const saved = await this.api.storeDoc(parsed.id, {
        base_revision: stored.revision,
        content: stored.content,
        ystate: Buffer.from(Y.encodeStateAsUpdate(ydoc)).toString("base64"),
        contributors: [],
        state_only: true,
      });
      if (saved.status === "gone") throw new GoneError(`doc ${parsed.id} not found`);
      if (saved.status === "conflict") throw new Error(`doc ${parsed.id} changed while loading`);
    }
    this.states.set(name, {
      id: parsed.id,
      revision: stored.revision,
      persisted: this.codec.ydocToMarkdown(ydoc),
      loadedRaw: stored.content,
      contributors: new Map(),
      queue: Promise.resolve(),
    });
    this.log.info("loaded", { doc: parsed.id, revision: stored.revision, from });
    return Y.encodeStateAsUpdate(ydoc);
  }

  noteChange(name: string, actor: Actor | undefined) {
    const state = this.states.get(name);
    if (!state || !actor) return;
    state.contributors.set(`${actor.type}:${actor.id}`, actor);
    state.loadedRaw = null;
  }

  async autosave(name: string, document: Document): Promise<void> {
    if (!this.states.has(name)) return;
    await this.exclusive(name, () => this.persist(name, document, {}));
  }

  forget(name: string) {
    this.states.delete(name);
    clearTimeout(this.agentPresence.get(name));
    this.agentPresence.delete(name);
  }

  // ---- Server-side writes (agents, CLI, API) -------------------------------

  /** Persists pending editor changes now, so a reader sees the live text. */
  async flush(name: string): Promise<{ loaded: boolean; revision?: number }> {
    const document = this.hocuspocus.documents.get(name);
    if (!document || document.isLoading || !this.states.has(name)) return { loaded: false };
    const revision = await this.exclusive(name, () => this.persist(name, document, {}));
    return { loaded: true, revision };
  }

  /** Whole-document write: three-way merged into the live document. */
  write(name: string, req: WriteRequest): Promise<WriteResult> {
    return this.withDocument(name, req.actor, async (document, state) => {
      let merged = false;
      if (req.content !== undefined) {
        const current = this.codec.ydocToJSON(document);
        const incoming = this.codec.parse(req.content);
        let next = incoming;
        if (req.action !== "restore" && req.base_content !== undefined) {
          const base = this.codec.parse(req.base_content);
          if (this.codec.serialize(base) !== this.codec.serialize(current)) {
            const result = mergeDocuments(this.codec, base, current, incoming);
            if (!result.ok) {
              return {
                status: "conflict",
                revision: state.revision,
                content: this.codec.serialize(current),
                conflicts: result.conflicts,
              };
            }
            next = result.doc;
            merged = true;
          }
        }
        this.codec.applyJSON(document, next, serverOrigin(req.actor));
      }
      return this.commit(name, document, state, req, merged);
    });
  }

  /** Find-and-replace edits applied against the live text. */
  edit(name: string, req: EditRequest): Promise<WriteResult> {
    return this.withDocument(name, req.actor, async (document, state) => {
      const current = this.codec.ydocToMarkdown(document);
      let result = applyTextEdits(current, req.edits);
      // Nothing changed since load, and the agent quoted the stored text
      // verbatim where it differs from the canonical form.
      if (!result.ok && state.loadedRaw !== null && current === state.persisted) {
        const raw = applyTextEdits(state.loadedRaw, req.edits);
        if (raw.ok) result = raw;
      }
      if (!result.ok) {
        const { ok: _ok, ...failure } = result;
        return { status: "edit_failed", revision: state.revision, content: current, failure };
      }
      this.codec.applyJSON(document, this.codec.parse(result.text), serverOrigin(req.actor));
      return this.commit(name, document, state, req, false);
    });
  }

  // ---- internals -----------------------------------------------------------

  private async commit(
    name: string,
    document: Document,
    state: DocState,
    req: { actor: Actor; title?: string; message?: string; action?: "edit" | "restore"; action_ref?: number; task_id?: string },
    merged: boolean,
  ): Promise<WriteResult> {
    this.showAgentPresence(name, document, req.actor);
    const stored = await this.persist(name, document, req);
    return {
      status: "ok",
      revision: stored,
      content: state.persisted,
      title: this.lastTitle.get(name) ?? "",
      merged,
    };
  }

  private readonly lastTitle = new Map<string, string>();

  /** Runs `fn` on the loaded document with its queue held. Pending editor
   *  changes are persisted first, so the write gets a version of its own. */
  private async withDocument(
    name: string,
    actor: Actor,
    fn: (document: Document, state: DocState) => Promise<WriteResult>,
  ): Promise<WriteResult> {
    let connection;
    try {
      connection = await this.hocuspocus.openDirectConnection(name, { actor });
    } catch (error) {
      if (error instanceof GoneError) return { status: "gone" };
      throw error;
    }
    try {
      const document = connection.document!;
      return await this.exclusive(name, async () => {
        const state = this.states.get(name)!;
        await this.persist(name, document, {});
        return fn(document, state);
      });
    } catch (error) {
      if (error instanceof GoneError) return { status: "gone" };
      throw error;
    } finally {
      // Outside the queue: disconnecting runs the store hook, which queues.
      await connection.disconnect();
    }
  }

  private exclusive<T>(name: string, fn: () => Promise<T>): Promise<T> {
    const state = this.states.get(name);
    if (!state) return fn();
    const run = state.queue.then(fn, fn);
    state.queue = run.catch(() => undefined);
    return run;
  }

  /** Writes the live document back to the API when it changed, or when the
   *  request carries a title, message or action of its own. */
  private async persist(
    name: string,
    document: Document,
    req: { actor?: Actor; title?: string; message?: string; action?: "edit" | "restore"; action_ref?: number; task_id?: string },
    attempt = 0,
  ): Promise<number> {
    const state = this.states.get(name);
    if (!state) throw new GoneError(name);
    const content = this.codec.ydocToMarkdown(document);
    const explicit = req.actor !== undefined;
    if (content === state.persisted && req.title === undefined && !req.action && !req.message) {
      return state.revision;
    }
    const contributors = explicit ? [req.actor!] : [...state.contributors.values()];
    const body: StoreRequest = {
      base_revision: state.revision,
      content,
      ystate: Buffer.from(Y.encodeStateAsUpdate(document)).toString("base64"),
      contributors: contributors.map(({ type, id }) => ({ type, id })),
      ...(req.title !== undefined ? { title: req.title } : {}),
      ...(req.message ? { message: req.message } : {}),
      ...(req.action ? { action: req.action } : {}),
      ...(req.action_ref ? { action_ref: req.action_ref } : {}),
      ...(req.task_id ? { task_id: req.task_id } : {}),
    };
    const result = await this.api.storeDoc(state.id, body);
    if (result.status === "gone") {
      this.hocuspocus.closeConnections(name);
      throw new GoneError(name);
    }
    if (result.status === "conflict") {
      // The stored doc moved without this service (an older API instance
      // during a deploy, or a write while the service was down). Fold that
      // change into the live document and try again.
      if (attempt >= 2) throw new Error(`doc ${state.id}: store kept conflicting`);
      this.reconcile(name, document, state, result.content);
      state.revision = result.revision;
      return this.persist(name, document, req, attempt + 1);
    }
    state.revision = result.revision;
    state.persisted = content;
    state.loadedRaw = null;
    this.lastTitle.set(name, result.title);
    if (!explicit) state.contributors.clear();
    this.log.info("stored", {
      doc: state.id,
      revision: result.revision,
      contributors: body.contributors.map((c) => `${c.type}:${c.id}`),
    });
    return result.revision;
  }

  private reconcile(name: string, document: Document, state: DocState, storedContent: string) {
    const base = this.codec.parse(state.persisted);
    const stored = this.codec.parse(storedContent);
    const live = this.codec.ydocToJSON(document);
    const result = mergeDocuments(this.codec, base, live, stored);
    if (result.ok) {
      this.codec.applyJSON(document, result.doc, serverOrigin({ type: "member", id: "", name: "" }));
    } else {
      // Keep what people are typing; the next store overwrites the stray write.
      this.log.warn("stored change conflicts with live edits; keeping live", { doc: state.id, name });
    }
    state.persisted = this.codec.serialize(stored);
  }

  /** Shows the writing agent in everyone's presence list for a moment. */
  private showAgentPresence(name: string, document: Document, actor: Actor) {
    if (actor.type !== "agent") return;
    // Not setLocalStateField: it is a no-op while the local state is null,
    // which is how Hocuspocus leaves the server's own awareness.
    document.awareness.setLocalState({
      user: { name: actor.name, color: "#7c3aed", kind: "agent", id: actor.id },
    });
    clearTimeout(this.agentPresence.get(name));
    this.agentPresence.set(
      name,
      setTimeout(() => {
        this.agentPresence.delete(name);
        if (!document.isDestroyed) document.awareness.setLocalState(null);
      }, 5000),
    );
  }
}
