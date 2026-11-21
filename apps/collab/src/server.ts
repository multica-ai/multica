import { timingSafeEqual } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";
import { isTransactionOrigin, Server } from "@hocuspocus/server";
import type { DocumentCodec } from "@multica/views/editor/schema/codec";
import { CollabApi } from "./api";
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
}

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

export function createCollabServer(config: CollabConfig, codec: DocumentCodec, log: Logger) {
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
      return { actor: verified.actor, workspaceId: verified.workspaceId, epoch };
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

    async onRequest({ request, response }) {
      if (await handleRequest(request, response)) {
        // Rejecting with a falsy value stops Hocuspocus's default response.
        throw null;
      }
    },
  });
  store.hocuspocus = server.hocuspocus;

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

  return { server, store };
}

function sendResult(response: ServerResponse, result: WriteResult): boolean {
  switch (result.status) {
    case "ok":
      return send(response, 200, result);
    case "gone":
      return send(response, 404, { error: "document not found" });
    case "conflict":
      return send(response, 409, { error: "merge_conflict", ...result });
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
