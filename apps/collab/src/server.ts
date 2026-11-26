import { timingSafeEqual } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";
import { type Connection, isTransactionOrigin, Server } from "@hocuspocus/server";
import type { DocumentCodec } from "@multica/views/editor/schema/codec";
import { CollabApi, type DocumentType } from "./api";
import type { CollabConfig } from "./config";
import {
  DocumentStore,
  documentEpoch,
  parseDocumentName,
  RESET_CLOSE_CODE,
  type EditRequest,
  type WriteRequest,
  type WriteResult,
} from "./documents";
import { verifyTicket, type Actor } from "./ticket";

export interface ConnectionContext {
  actor: Actor;
  workspaceId: string;
  /** The document epoch this client's local copy came from, if it has one. */
  epoch?: string;
  /** When the last ticket this connection showed expires, in ms. */
  expiresAt: number;
  /** Set once the connection is asked for a fresh ticket: when it is closed
   *  unless one arrives. */
  dueBy?: number;
}

/** Close reason for a connection that did not show a fresh ticket in time.
 *  The client joins again with a new one, which the API only mints for
 *  someone still allowed in. */
export const REAUTHENTICATE_REASON = "reauthenticate";
const REAUTHENTICATE_CLOSE_CODE = 4401;

/** A connection stays only as long as its ticket: tickets are minted after
 *  the API checks access, so asking for fresh ones is how a connection keeps
 *  proving it. */
export interface AccessTiming {
  /** How long before its ticket expires a connection is asked for a new one. */
  refreshLeadMs: number;
  /** How long a connection asked to revalidate has to show a new ticket. */
  revalidateGraceMs: number;
  /** How often connections are checked. */
  sweepMs: number;
}

export const DEFAULT_ACCESS_TIMING: AccessTiming = {
  refreshLeadMs: 60_000,
  revalidateGraceMs: 15_000,
  sweepMs: 5_000,
};

/** Who must show a fresh ticket after the API revoked some access. */
export interface RevalidateRequest {
  workspace_id: string;
  /** Only this person's or agent's connections. */
  actor?: Pick<Actor, "type" | "id">;
  /** Only connections to this document. */
  document?: string;
  /** Only connections to documents of this type. */
  type?: DocumentType;
}

const reauthenticate = (message: string) =>
  Object.assign(new Error(message), { code: REAUTHENTICATE_CLOSE_CODE, reason: REAUTHENTICATE_REASON });

export interface Logger {
  info(msg: string, fields?: Record<string, unknown>): void;
  warn(msg: string, fields?: Record<string, unknown>): void;
  error(msg: string, fields?: Record<string, unknown>): void;
}

const MAX_BODY_BYTES = 8 << 20;

/** The provider token is `{"ticket": "...", "epoch": "..."}` so a
 *  reconnecting client can say which copy of the document it holds. A bare
 *  ticket is accepted too. */
function parseToken(token: string): { ticket: string; epoch?: string } {
  if (!token.startsWith("{")) return { ticket: token };
  const parsed = JSON.parse(token) as { ticket?: unknown; epoch?: unknown };
  if (typeof parsed.ticket !== "string") throw new Error("malformed token");
  return { ticket: parsed.ticket, epoch: typeof parsed.epoch === "string" ? parsed.epoch : undefined };
}

export function createCollabServer(
  config: CollabConfig,
  codec: DocumentCodec,
  log: Logger,
  timing: AccessTiming = DEFAULT_ACCESS_TIMING,
) {
  const api = new CollabApi(config.apiUrl, config.secret);
  const store = new DocumentStore(api, codec, log);

  const server = new Server<ConnectionContext>({
    port: config.port,
    quiet: true,
    debounce: config.storeDebounceMs,
    maxDebounce: config.storeMaxDebounceMs,
    // Keep the doc warm for the debounce window after the last editor leaves,
    // so a quick reconnect does not reload it.
    unloadImmediately: false,

    async onAuthenticate({ token, documentName, connectionConfig }) {
      const { ticket, epoch } = parseToken(token);
      const verified = verifyTicket(ticket, config.secret);
      if (verified.document !== documentName) throw new Error("ticket is for another document");
      if (!parseDocumentName(documentName)) throw new Error("unknown document");
      if (verified.readOnly) connectionConfig.readOnly = true;
      return { actor: verified.actor, workspaceId: verified.workspaceId, epoch, expiresAt: verified.expiresAt * 1000 };
    },

    // A fresh ticket the client sent when asked (see checkAccess). Thrown
    // errors close the connection with their code and reason.
    async onTokenSync({ token, documentName, connection }) {
      const context = connection.context as ConnectionContext;
      let verified;
      try {
        verified = verifyTicket(parseToken(token).ticket, config.secret);
      } catch (error) {
        throw reauthenticate(String(error));
      }
      if (
        verified.document !== documentName ||
        verified.workspaceId !== context.workspaceId ||
        verified.actor.type !== context.actor.type ||
        verified.actor.id !== context.actor.id
      ) {
        throw reauthenticate("ticket is for another connection");
      }
      if (verified.readOnly) connection.readOnly = true;
      context.expiresAt = verified.expiresAt * 1000;
      context.dueBy = undefined;
    },

    async onLoadDocument({ documentName }) {
      return store.load(documentName);
    },

    async beforeHandleMessage({ context, document }) {
      if (context.epoch && context.epoch !== documentEpoch(document)) {
        throw Object.assign(new Error("document was rebuilt"), {
          code: RESET_CLOSE_CODE,
          reason: "document-reset",
        });
      }
    },

    async onChange({ documentName, transactionOrigin, context }) {
      if (isTransactionOrigin(transactionOrigin) && transactionOrigin.source === "connection") {
        store.noteChange(documentName, context?.actor);
      }
    },

    async onStoreDocument({ documentName, document }) {
      await store.autosave(documentName, document);
    },

    async afterUnloadDocument({ documentName }) {
      store.forget(documentName);
    },

    async onDestroy() {
      clearInterval(sweep);
    },

    async onRequest({ request, response }) {
      if (await handleRequest(request, response)) {
        // Rejecting with a falsy value stops Hocuspocus's default response.
        throw null;
      }
    },
  });
  store.hocuspocus = server.hocuspocus;

  function* connections(): Generator<[string, Connection<ConnectionContext>]> {
    for (const [name, document] of server.hocuspocus.documents) {
      for (const connection of document.getConnections()) yield [name, connection as Connection<ConnectionContext>];
    }
  }

  /** Asks a connection for a fresh ticket, closing it at `dueBy` without one. */
  function requestTicket(connection: Connection<ConnectionContext>, dueBy: number) {
    const context = connection.context;
    context.dueBy = Math.min(context.dueBy ?? Infinity, dueBy);
    connection.requestToken();
  }

  /** Closes connections past their ticket or revalidation deadline, and asks
   *  those whose ticket is about to expire for a new one. */
  function checkAccess(now = Date.now()) {
    for (const [name, connection] of connections()) {
      const context = connection.context;
      if (!context?.expiresAt) continue;
      if (now >= Math.min(context.expiresAt, context.dueBy ?? Infinity)) {
        log.info("connection access expired", { name, actor: `${context.actor.type}:${context.actor.id}` });
        connection.close({ code: REAUTHENTICATE_CLOSE_CODE, reason: REAUTHENTICATE_REASON });
      } else if (context.dueBy === undefined && context.expiresAt - now <= timing.refreshLeadMs) {
        requestTicket(connection, context.expiresAt);
      }
    }
  }
  const sweep = setInterval(() => checkAccess(), timing.sweepMs);
  sweep.unref();

  /** The API revoked some access: everyone it may affect must show a fresh
   *  ticket within the grace period or be disconnected. Those still allowed
   *  in get one without noticing. */
  function revalidate(req: RevalidateRequest): number {
    const dueBy = Date.now() + timing.revalidateGraceMs;
    let asked = 0;
    for (const [name, connection] of connections()) {
      const context = connection.context;
      if (!context?.expiresAt || context.workspaceId !== req.workspace_id) continue;
      if (req.actor && (context.actor.type !== req.actor.type || context.actor.id !== req.actor.id)) continue;
      if (req.document && name !== req.document) continue;
      if (req.type && parseDocumentName(name)?.type !== req.type) continue;
      requestTicket(connection, dueBy);
      asked++;
    }
    return asked;
  }

  const authorized = (request: IncomingMessage) => {
    const given = Buffer.from(String(request.headers["x-collab-secret"] ?? ""));
    const expected = Buffer.from(config.secret);
    return given.length === expected.length && timingSafeEqual(given, expected);
  };

  async function handleRequest(request: IncomingMessage, response: ServerResponse): Promise<boolean> {
    const url = new URL(request.url ?? "/", "http://collab");
    if (url.pathname === "/health") {
      send(response, 200, { ok: true, documents: server.hocuspocus.getDocumentsCount() });
      return true;
    }
    if (url.pathname === "/internal/revalidate") {
      if (request.method !== "POST") return send(response, 405, { error: "method not allowed" });
      if (!authorized(request)) return send(response, 401, { error: "unauthorized" });
      let body: RevalidateRequest;
      try {
        body = (await readJson(request)) as RevalidateRequest;
      } catch {
        return send(response, 400, { error: "invalid body" });
      }
      if (typeof body?.workspace_id !== "string" || !body.workspace_id) {
        return send(response, 400, { error: "workspace_id is required" });
      }
      const asked = revalidate(body);
      log.info("revalidating connections", { ...body, asked });
      return send(response, 200, { asked });
    }
    const match = /^\/internal\/documents\/([^/]+)\/(write|edit|flush)$/.exec(url.pathname);
    if (!match) return false;
    if (request.method !== "POST") return send(response, 405, { error: "method not allowed" });
    if (!authorized(request)) return send(response, 401, { error: "unauthorized" });
    const name = decodeURIComponent(match[1]!);
    if (!parseDocumentName(name)) return send(response, 400, { error: "unknown document" });
    try {
      if (match[2] === "flush") return send(response, 200, await store.flush(name));
      const body = await readJson(request);
      const result =
        match[2] === "write"
          ? await store.write(name, body as WriteRequest)
          : await store.edit(name, body as EditRequest);
      return sendResult(response, result);
    } catch (error) {
      log.error("internal request failed", { path: url.pathname, error: String(error) });
      return send(response, 500, { error: "internal error" });
    }
  }

  return { server, store, checkAccess, revalidate };
}

function sendResult(response: ServerResponse, result: WriteResult): boolean {
  switch (result.status) {
    case "ok":
      return send(response, 200, result);
    case "gone":
      return send(response, 404, { error: "document not found" });
    case "conflict":
      return send(response, 409, { error: "merge_conflict", ...result });
    case "stale":
      return send(response, 409, { error: "revision_conflict", ...result });
    case "edit_failed":
      return send(response, 409, { error: "edit_failed", ...result });
    case "too_large":
      return send(response, 413, { error: "content_too_large", ...result });
  }
}

function send(response: ServerResponse, status: number, body: unknown): true {
  response.writeHead(status, { "Content-Type": "application/json" });
  response.end(JSON.stringify(body));
  return true;
}

async function readJson(request: IncomingMessage): Promise<unknown> {
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of request) {
    size += (chunk as Buffer).length;
    if (size > MAX_BODY_BYTES) throw new Error("request body too large");
    chunks.push(chunk as Buffer);
  }
  return JSON.parse(Buffer.concat(chunks).toString("utf8") || "{}");
}
