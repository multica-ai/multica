// Cerebro feature: personal-browser agent transport (FIR-2037, Fase 2 + per-action gate).
//
// A runtime agent (Claude Code et al.) is a SEPARATE local process from this
// Electron app, so it cannot reach the pane's `cerebro-browser:agent-*` IPC
// handlers directly — IPC is internal to Electron (main <-> renderer). This
// module is the bridge: a tiny loopback-only HTTP server inside the Electron
// main process that the `multica cerebro-browser` CLI calls. It mirrors the
// proven daemon health-server pattern (127.0.0.1 + a sidecar rendezvous file).
//
// Authorization is PER ACTION and AUTHORITATIVE. Before every action this server
// asks the Multica server "may THIS agent drive the personal browser on THIS
// host, right now?" — POST /api/cerebro/personal-browser/authorize, authenticated
// with the agent's own token (which the CLI forwards). The server resolves the
// full tool-policy chain (workspace → runtime → agent → group → user) with
// Base=Deny and the target host in context, so a host-allowlist condition ("only
// these domains") bites. This makes the gate:
//   - all-layer (the chain), conditional / site-limited (the host), and
//   - a HARD boundary, not advisory: an ungranted agent presenting its own token
//     is denied even if it read the loopback sidecar token, closing the
//     same-user bypass the Fase 3 security review flagged.
//
// Three independent gates remain: (1) per-action server authorization above;
// (2) loopback bind + the bearer token from the 0600 sidecar, so a non-agent
// local process cannot drive the browser; (3) every action audited locally
// (never the typed value) on top of the server-side audit.

import { createServer, type IncomingMessage, type Server } from "node:http";
import { randomBytes } from "node:crypto";
import { mkdir, writeFile, appendFile, rm } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";
import { getCerebroBrowserPane } from "./cerebro-browser-pane";
import { getTargetApiBaseUrl } from "./daemon-manager";

const MULTICA_DIR = join(homedir(), ".multica");
const SIDECAR_PATH = join(MULTICA_DIR, "cerebro-browser-control.json");
const AUDIT_DIR = join(MULTICA_DIR, "logs");
const AUDIT_PATH = join(AUDIT_DIR, "cerebro-browser-audit.log");

// Cap bodies so a buggy/hostile local caller cannot exhaust memory. The agent
// only ever sends a ref, a short value, a URL, plus its token — kilobytes.
const MAX_BODY_BYTES = 256 * 1024;

interface Sidecar {
  port: number;
  token: string;
  pid: number;
}

let server: Server | null = null;
let token: string | null = null;

function isLoopback(req: IncomingMessage): boolean {
  const addr = req.socket.remoteAddress ?? "";
  return addr === "127.0.0.1" || addr === "::1" || addr === "::ffff:127.0.0.1";
}

async function audit(action: string, detail: Record<string, unknown>): Promise<void> {
  try {
    await mkdir(AUDIT_DIR, { recursive: true });
    const line =
      JSON.stringify({ ts: new Date().toISOString(), action, ...detail }) + "\n";
    await appendFile(AUDIT_PATH, line, { mode: 0o600 });
  } catch (err) {
    console.warn("[cerebro-browser-control] audit write failed", err);
  }
}

function readBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    let size = 0;
    const chunks: Buffer[] = [];
    req.on("data", (chunk: Buffer) => {
      size += chunk.length;
      if (size > MAX_BODY_BYTES) {
        reject(new Error("request body too large"));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf-8")));
    req.on("error", reject);
  });
}

function strField(body: Record<string, unknown>, key: string): string | undefined {
  const v = body[key];
  return typeof v === "string" ? v : undefined;
}

/**
 * Ask the Multica server whether the calling agent may drive the personal
 * browser on `host` right now. Authenticated as the agent via the token the CLI
 * forwarded. Fails CLOSED on any error (no server URL, missing token, non-200,
 * network failure) — a security gate must never default open.
 */
async function authorize(
  host: string,
  action: string,
  agentToken: string,
): Promise<{ allowed: boolean; reason: string }> {
  const serverUrl = getTargetApiBaseUrl();
  if (!serverUrl) {
    return { allowed: false, reason: "the Multica desktop app is not signed in" };
  }
  if (!agentToken) {
    return { allowed: false, reason: "missing agent token" };
  }
  const url = `${serverUrl.replace(/\/+$/, "")}/api/cerebro/personal-browser/authorize`;
  try {
    const res = await fetch(url, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        authorization: `Bearer ${agentToken}`,
      },
      body: JSON.stringify({ host, action }),
    });
    if (!res.ok) {
      return {
        allowed: false,
        reason: `authorization unavailable (status ${res.status})`,
      };
    }
    const data = (await res.json()) as {
      allowed?: boolean;
      reason?: string;
      decision?: string;
    };
    return {
      allowed: data?.allowed === true,
      reason: data?.reason || data?.decision || "not allowed",
    };
  } catch (err) {
    return {
      allowed: false,
      reason: `authorization request failed: ${err instanceof Error ? err.message : String(err)}`,
    };
  }
}

// A route declares the host its action targets (so we can authorize against the
// right domain) and how to run once allowed.
interface Route {
  action: string;
  // Host to authorize against. For navigate it is the target URL; for actions on
  // the current page it is that page's URL; for pure management it is "".
  hostFor: (body: Record<string, unknown>) => string;
  run: (body: Record<string, unknown>) => Promise<unknown>;
}

function buildRoutes(): Record<string, Route> {
  const requirePane = () => {
    const pane = getCerebroBrowserPane();
    if (!pane) throw new Error("personal browser is not open");
    return pane;
  };
  const sessionOf = (body: Record<string, unknown>) => strField(body, "sessionId");

  return {
    "/agent/snapshot": {
      action: "snapshot",
      hostFor: (b) => requirePane().currentUrl(sessionOf(b)),
      run: async (b) => {
        const snap = await requirePane().agentSnapshot(sessionOf(b));
        await audit("snapshot", { sessionId: sessionOf(b), url: snap.url, nodes: snap.nodes.length });
        return snap;
      },
    },
    "/agent/click": {
      action: "click",
      hostFor: (b) => requirePane().currentUrl(sessionOf(b)),
      run: async (b) => {
        const ref = strField(b, "ref");
        if (!ref) throw new Error("ref is required");
        await requirePane().agentClick(ref, sessionOf(b));
        await audit("click", { sessionId: sessionOf(b), ref });
        return { ok: true };
      },
    },
    "/agent/fill": {
      action: "fill",
      hostFor: (b) => requirePane().currentUrl(sessionOf(b)),
      run: async (b) => {
        const ref = strField(b, "ref");
        const value = strField(b, "value");
        if (!ref || value === undefined) throw new Error("ref and value are required");
        await requirePane().agentFill(ref, value, sessionOf(b));
        // Never audit the typed value — it can be a password into the user's
        // logged-in session. Record only that a fill happened, and where.
        await audit("fill", { sessionId: sessionOf(b), ref, valueLength: value.length });
        return { ok: true };
      },
    },
    "/agent/navigate": {
      action: "navigate",
      // Authorize against the TARGET host, before the page loads.
      hostFor: (b) => strField(b, "url") ?? "",
      run: async (b) => {
        const url = strField(b, "url");
        if (!url) throw new Error("url is required");
        await requirePane().agentNavigate(url, sessionOf(b));
        await audit("navigate", { sessionId: sessionOf(b), url });
        return { ok: true };
      },
    },
    "/agent/open-tab": {
      action: "open-tab",
      // Authorize against the TARGET host when a url is given (it is loaded into
      // the default session before we show the tab); empty host = management.
      hostFor: (b) => strField(b, "url") ?? "",
      run: async (b) => {
        const url = strField(b, "url");
        if (url) await requirePane().agentNavigate(url); // background-load default session
        requirePane().requestOpenTab();
        await audit("open-tab", { url: url ?? "" });
        return { ok: true };
      },
    },
    "/agent/sessions": {
      action: "sessions",
      hostFor: () => "",
      run: async () => {
        const sessions = requirePane().listSessions();
        await audit("sessions", { count: sessions.length });
        return { sessions };
      },
    },
    "/agent/logout": {
      action: "logout",
      hostFor: (b) => requirePane().currentUrl(sessionOf(b)),
      run: async (b) => {
        await requirePane().logout(sessionOf(b));
        await audit("logout", { sessionId: sessionOf(b) });
        return { ok: true };
      },
    },
    "/agent/clear-cookies": {
      action: "clear-cookies",
      hostFor: (b) => requirePane().currentUrl(sessionOf(b)),
      run: async (b) => {
        await requirePane().clearCookies(sessionOf(b));
        await audit("clear-cookies", { sessionId: sessionOf(b) });
        return { ok: true };
      },
    },
  };
}

/**
 * Start the loopback control server (idempotent). Writes the sidecar rendezvous
 * file the CLI reads. Safe to call on every pane open — returns early if running.
 */
export async function ensureCerebroBrowserControlServer(): Promise<void> {
  if (server) return;

  token = randomBytes(32).toString("hex");
  const routes = buildRoutes();

  const srv = createServer((req, res) => {
    void (async () => {
      const reply = (status: number, payload: unknown): void => {
        const json = JSON.stringify(payload);
        res.writeHead(status, {
          "content-type": "application/json",
          "content-length": Buffer.byteLength(json),
        });
        res.end(json);
      };

      try {
        // Gate 2a: loopback only.
        if (!isLoopback(req)) {
          reply(403, { error: "forbidden" });
          return;
        }
        if (req.method !== "POST") {
          reply(405, { error: "method not allowed" });
          return;
        }
        // Gate 2b: bearer token from the 0600 sidecar.
        const auth = req.headers["authorization"] ?? "";
        if (auth !== `Bearer ${token}`) {
          reply(401, { error: "unauthorized" });
          return;
        }
        const route = routes[(req.url ?? "").split("?")[0]];
        if (!route) {
          reply(404, { error: "unknown action" });
          return;
        }
        const raw = await readBody(req);
        let body: Record<string, unknown> = {};
        if (raw.trim()) {
          const parsed: unknown = JSON.parse(raw);
          if (parsed && typeof parsed === "object") {
            body = parsed as Record<string, unknown>;
          }
        }

        // Gate 1: per-action, per-host, authoritative authorization against the
        // server, as the calling agent. Compute the host first (may create the
        // session) so we authorize against exactly what the action will touch.
        const host = route.hostFor(body);
        const agentToken = strField(body, "agentToken") ?? "";
        const verdict = await authorize(host, route.action, agentToken);
        if (!verdict.allowed) {
          await audit("denied", { action: route.action, host, reason: verdict.reason });
          reply(403, { error: verdict.reason });
          return;
        }

        const result = await route.run(body);
        reply(200, result);
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        reply(400, { error: message });
      }
    })();
  });

  await new Promise<void>((resolve, reject) => {
    srv.once("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      srv.removeListener("error", reject);
      resolve();
    });
  });

  const address = srv.address();
  const port = typeof address === "object" && address ? address.port : 0;
  if (!port) {
    srv.close();
    throw new Error("[cerebro-browser-control] failed to acquire a port");
  }

  server = srv;

  const sidecar: Sidecar = { port, token, pid: process.pid };
  await mkdir(MULTICA_DIR, { recursive: true });
  // 0600: only the OS user can read the loopback token. It is the non-agent-
  // process gate; the per-action server authorization is the per-agent gate.
  await writeFile(SIDECAR_PATH, JSON.stringify(sidecar), { mode: 0o600 });
  console.log(`[cerebro-browser-control] listening on 127.0.0.1:${port}`);
}

/** Tear down the server and remove the sidecar. Called on app quit. */
export async function stopCerebroBrowserControlServer(): Promise<void> {
  if (server) {
    await new Promise<void>((resolve) => server?.close(() => resolve()));
    server = null;
  }
  token = null;
  await rm(SIDECAR_PATH, { force: true }).catch(() => undefined);
}
