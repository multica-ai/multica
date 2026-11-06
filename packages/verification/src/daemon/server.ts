import { randomUUID } from "node:crypto";
import {
  createServer,
  type IncomingMessage,
  type Server,
  type ServerResponse,
} from "node:http";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { isInitializeRequest } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import type { VerificationHost } from "../host/host.js";
import { createVerificationMcpServer } from "../mcp/server.js";
import { consolePage } from "./console-page.js";
import type { HealthMonitor } from "./health.js";

export type DaemonInfo = {
  pid: number;
  startedAt: string;
  commit: string;
  mcpUrl: string;
};

/**
 * One loopback listener: MCP for agents at /mcp, and the management console.
 * Neither needs a credential; only requests addressed to this loopback host
 * are served, and console writes must come from the console page itself.
 */
export async function serveDaemon(options: {
  host: VerificationHost;
  monitor: HealthMonitor;
  port: number;
  info: Omit<DaemonInfo, "mcpUrl">;
}): Promise<{ server: Server; info: DaemonInfo; close(): Promise<void> }> {
  const { host, monitor, port } = options;
  const info: DaemonInfo = {
    ...options.info,
    mcpUrl: `http://127.0.0.1:${port}/mcp`,
  };
  const sessions = new Map<string, StreamableHTTPServerTransport>();
  const allowedHosts = new Set([`127.0.0.1:${port}`, `localhost:${port}`]);
  const json = (response: ServerResponse, status: number, value: unknown) => {
    response.writeHead(status, {
      "Content-Type": "application/json",
      "Cache-Control": "no-store",
    });
    response.end(JSON.stringify(value));
  };

  const server = createServer(async (request, response) => {
    try {
      // Reject DNS-rebinding requests addressed to another host name.
      if (!allowedHosts.has(request.headers.host ?? "")) {
        json(response, 421, { error: "misdirected" });
        return;
      }
      const url = new URL(request.url ?? "/", `http://${request.headers.host}`);
      if (url.pathname === "/mcp") {
        const sessionId = request.headers["mcp-session-id"];
        if (typeof sessionId === "string") {
          const transport = sessions.get(sessionId);
          if (!transport) {
            json(response, 404, { error: "unknown session" });
            return;
          }
          await transport.handleRequest(request, response);
          return;
        }
        const body =
          request.method === "POST" ? await readJson(request) : undefined;
        if (!isInitializeRequest(body)) {
          json(response, 400, { error: "initialize a session first" });
          return;
        }
        const transport = new StreamableHTTPServerTransport({
          sessionIdGenerator: () => randomUUID(),
          onsessioninitialized: (id) => {
            sessions.set(id, transport);
          },
        });
        transport.onclose = () => {
          if (transport.sessionId) sessions.delete(transport.sessionId);
        };
        await createVerificationMcpServer(host).connect(transport);
        await transport.handleRequest(request, response, body);
        return;
      }
      if (url.pathname === "/" && request.method === "GET") {
        response.writeHead(200, {
          "Content-Type": "text/html; charset=utf-8",
          "Cache-Control": "no-store",
          "Content-Security-Policy":
            "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'",
        });
        response.end(consolePage);
        return;
      }
      if (url.pathname.startsWith("/console/api/")) {
        // State-changing console requests must come from the console itself.
        if (
          request.method !== "GET" &&
          request.headers.origin !== `http://${request.headers.host}`
        ) {
          json(response, 403, { error: "forbidden" });
          return;
        }
        const path = url.pathname.slice("/console/api/".length);
        if (path === "overview" && request.method === "GET") {
          json(response, 200, { daemon: info, ...(await monitor.snapshot()) });
          return;
        }
        if (path === "jobs" && request.method === "GET") {
          json(response, 200, await host.jobs.recent(50));
          return;
        }
        if (path === "logs" && request.method === "GET") {
          const query = z
            .object({
              environmentId: z.uuid(),
              name: z.enum(["prepare", "backend", "desktop", "stop"]),
            })
            .parse(Object.fromEntries(url.searchParams));
          json(
            response,
            200,
            await tailLog(host, query.environmentId, query.name),
          );
          return;
        }
        const stop = /^environments\/([^/]+)\/stop$/.exec(path);
        if (stop && request.method === "POST") {
          const environmentId = z.uuid().parse(decodeURIComponent(stop[1]!));
          json(
            response,
            202,
            await host.jobs.submit(
              "env.stop",
              { environmentId },
              `console-stop-${environmentId}-${Date.now()}`,
            ),
          );
          return;
        }
      }
      json(response, 404, { error: "not found" });
    } catch (error) {
      if (!response.headersSent)
        json(response, 500, {
          error: error instanceof Error ? error.message : "internal error",
        });
      else response.end();
    }
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", resolve);
  });
  return {
    server,
    info,
    close: async () => {
      for (const transport of sessions.values())
        await transport.close().catch(() => undefined);
      await new Promise<void>((resolve) => server.close(() => resolve()));
    },
  };
}

async function readJson(request: IncomingMessage): Promise<unknown> {
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of request) {
    size += (chunk as Buffer).length;
    if (size > 50 * 1024 * 1024) throw new Error("Request body too large");
    chunks.push(chunk as Buffer);
  }
  return JSON.parse(Buffer.concat(chunks).toString("utf8") || "null");
}

/** The console shows the end of a log, where failures are. */
async function tailLog(
  host: VerificationHost,
  environmentId: string,
  name: "prepare" | "backend" | "desktop" | "stop",
) {
  let offset = 0,
    text = "",
    truncated = false;
  for (;;) {
    const page = await host.environments.readLog(
      environmentId,
      name,
      offset,
      32000,
    );
    text = (text + page.text).slice(-32000);
    truncated = page.truncated;
    if (!page.hasMore) break;
    offset = page.nextOffset;
  }
  return { text, truncated };
}
