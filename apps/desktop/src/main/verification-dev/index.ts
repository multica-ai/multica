import { app, ipcMain, systemPreferences } from "electron";
import { createServer } from "node:http";
import { timingSafeEqual, randomUUID } from "node:crypto";
import { lstatSync, readFileSync, mkdirSync } from "node:fs";
import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { z } from "zod";
import { redactDiagnosticText } from "../developer-runtime-logs";
import { desktopRuntimeRegistry } from "../desktop-runtime-instance";

const sessionSchema = z.object({
  schemaVersion: z.literal(1),
  environmentId: z.string().uuid(),
  token: z.string().regex(/^[a-f0-9]{64}$/),
  directory: z.string(),
  profileDir: z.string(),
  commit: z.string().regex(/^[a-f0-9]{40}$/),
  sourceDigest: z.string(),
  apiOrigin: z.string().url(),
});
function sessionFromHost() {
  const path = process.env.MULTICA_VERIFICATION_SESSION;
  if (!path) return null;
  const file = lstatSync(path);
  if (!file.isFile() || file.isSymbolicLink() || (file.mode & 0o077) !== 0)
    throw new Error("Verification session must be private");
  const session = sessionSchema.parse(JSON.parse(readFileSync(path, "utf8")));
  if (
    process.env.MULTICA_VERIFICATION_MODE !== "1" ||
    process.env.MULTICA_VERIFICATION_TOKEN !== session.token
  )
    throw new Error(
      "Verification launch capability does not match its private session",
    );
  return session;
}
const session = sessionFromHost();
type Options = Parameters<
  NonNullable<typeof import("#verification-dev/main").verificationDev>["start"]
>[0];

export const verificationDev = {
  enabled: session !== null,
  runtimeHome: session ? join(session.directory, "runtime-home") : undefined,
  configureBeforeReady() {
    if (!session) return;
    mkdirSync(session.profileDir, { recursive: true, mode: 0o700 });
    app.setPath("userData", session.profileDir);
  },
  async start(options: Options) {
    if (!session) return;
    if (app.isPackaged || options.apiOrigin() !== session.apiOrigin)
      throw new Error(
        "Verification requires a matching development API target",
      );
    const launch = await desktopRuntimeRegistry().assertOwner(
      session.apiOrigin,
    );
    if (!session.commit.startsWith(launch.cliCommit))
      throw new Error(
        "Verification CLI commit does not match the source checkout",
      );
    await mkdir(session.directory, { recursive: true, mode: 0o700 });
    let cursor = 0;
    const events: {
      cursor: number;
      type: string;
      occurredAt: string;
      payload: unknown;
    }[] = [];
    const pending = new Map<
      string,
      { sender: number; resolve(value: unknown): void }
    >();
    function record(type: string, payload: unknown) {
      const event = {
        cursor: ++cursor,
        type,
        occurredAt: new Date().toISOString(),
        payload,
      };
      events.push(event);
      if (events.length > 2000) events.shift();
    }
    const attach = (window: Electron.BrowserWindow) => {
      const id = window.id;
      record("window.created", { windowId: id });
      window.webContents.on("render-process-gone", (_event, details) =>
        record("renderer.crashed", {
          windowId: id,
          reason: details.reason,
          exitCode: details.exitCode,
        }),
      );
      window.webContents.on("did-fail-load", (_event, code, description) => {
        if (code !== -3)
          record("page.loadFailed", {
            windowId: id,
            code,
            description: redactDiagnosticText(description),
          });
      });
      window.webContents.on("console-message", (details) => {
        if (details.level === "error" || details.level === "warning")
          record(`console.${details.level}`, {
            windowId: id,
            message: redactDiagnosticText(details.message).slice(0, 8192),
          });
      });
      window.once("closed", () => record("window.closed", { windowId: id }));
    };
    options.windows().forEach(attach);
    app.on("browser-window-created", (_event, window) => attach(window));
    ipcMain.on("verification-dev:response", (event, value: unknown) => {
      const response = z
        .object({
          id: z.string(),
          value: z.unknown().optional(),
          unavailable: z.boolean().optional(),
        })
        .safeParse(value);
      if (!response.success || event.senderFrame !== event.sender.mainFrame)
        return;
      const request = pending.get(response.data.id);
      if (!request || request.sender !== event.sender.id) return;
      pending.delete(response.data.id);
      if (Buffer.byteLength(JSON.stringify(response.data)) > 1024 * 1024) {
        request.resolve({ unavailable: true });
        return;
      }
      request.resolve(
        response.data.unavailable ? { unavailable: true } : response.data.value,
      );
    });
    const renderer = async (subjectRef?: string) => {
      const window = options.windows().find((item) => !item.isDestroyed());
      if (!window || pending.size >= 16) return { unavailable: true };
      const id = randomUUID();
      return new Promise<unknown>((resolve) => {
        const timer = setTimeout(() => {
          pending.delete(id);
          resolve({ unavailable: true });
        }, 2000);
        pending.set(id, {
          sender: window.webContents.id,
          resolve: (value) => {
            clearTimeout(timer);
            resolve(value);
          },
        });
        window.webContents.send("verification-dev:request", {
          id,
          subjectRef,
          userId: options.currentUser(),
        });
      });
    };
    const server = createServer(async (request, response) => {
      response.setHeader("Content-Type", "application/json");
      response.setHeader("Cache-Control", "no-store");
      if (!authorized(request.headers)) {
        response.writeHead(403).end(JSON.stringify({ error: "forbidden" }));
        return;
      }
      try {
        const url = new URL(request.url ?? "/", "http://127.0.0.1");
        if (request.method === "POST" && url.pathname === "/shutdown") {
          response.end(JSON.stringify({ accepted: true }));
          setTimeout(() => app.quit(), 50);
          return;
        }
        if (request.method !== "GET") {
          response.writeHead(405).end();
          return;
        }
        if (url.pathname === "/capabilities") {
          response.end(
            JSON.stringify({
              schemaVersion: 1,
              environmentId: session.environmentId,
              pid: process.pid,
              userId: options.currentUser(),
              commit: session.commit,
              cursor,
              permissions: {
                accessibility:
                  process.platform === "darwin"
                    ? systemPreferences.isTrustedAccessibilityClient(false)
                    : null,
                screenRecording:
                  process.platform === "darwin"
                    ? systemPreferences.getMediaAccessStatus("screen")
                    : "unknown",
              },
              earliestCursor: events[0]?.cursor ?? cursor + 1,
              capabilities: [
                "main.snapshot",
                "renderer.snapshot",
                "events",
                "daemon.connection",
              ],
            }),
          );
          return;
        }
        if (url.pathname === "/snapshot") {
          const target = url.searchParams.get("source") ?? "main";
          response.end(
            JSON.stringify({
              schemaVersion: 1,
              environmentId: session.environmentId,
              generation: `${session.environmentId}:${launch.ownerId}`,
              cursor,
              payload:
                target === "renderer"
                  ? await renderer(
                      url.searchParams.get("subjectRef") ?? undefined,
                    )
                  : {
                      pid: process.pid,
                      userId: options.currentUser(),
                      version: app.getVersion(),
                      windows: options
                        .windows()
                        .filter((window) => !window.isDestroyed())
                        .map((window) => ({
                          id: window.id,
                          title: window.getTitle(),
                          url: window.webContents.getURL(),
                          loading: window.webContents.isLoading(),
                        })),
                    },
            }),
          );
          return;
        }
        if (url.pathname === "/events") {
          const after = z.coerce
            .number()
            .int()
            .nonnegative()
            .parse(url.searchParams.get("after"));
          response.end(
            JSON.stringify({
              schemaVersion: 1,
              generation: `${session.environmentId}:${launch.ownerId}`,
              cursor,
              earliestCursor: events[0]?.cursor ?? cursor + 1,
              events: events.filter((event) => event.cursor > after),
              hasMore: false,
            }),
          );
          return;
        }
        if (url.pathname === "/connection") {
          const userId = options.currentUser();
          if (!userId) throw new Error("Login is required");
          const state = z
            .object({ workspaceId: z.string().min(1) })
            .parse(await renderer());
          const backendToken = await options.backendToken();
          if (!backendToken) throw new Error("Runtime login is required");
          const membership = await fetch(
            `${session.apiOrigin}/api/workspaces/${encodeURIComponent(state.workspaceId)}`,
            {
              headers: {
                Authorization: `Bearer ${backendToken}`,
                "X-Workspace-ID": state.workspaceId,
              },
              signal: AbortSignal.timeout(5000),
              redirect: "error",
            },
          );
          if (!membership.ok) throw new Error("Workspace is unavailable");
          const kind = url.searchParams.get("kind");
          if (kind !== "daemon" && kind !== "backend")
            throw new Error("Invalid connection kind");
          const identity = await options.identity();
          if (identity.ownerId !== launch.ownerId)
            throw new Error("Runtime ownership changed");
          const endpoint =
            kind === "daemon"
              ? await options.endpoint()
              : { base: new URL(session.apiOrigin).host, token: backendToken };
          const tokenFile = join(session.directory, `${kind}.token`);
          await writeFile(tokenFile, endpoint.token, { mode: 0o600 });
          response.end(
            JSON.stringify({
              baseUrl:
                kind === "daemon"
                  ? `http://${endpoint.base}`
                  : session.apiOrigin,
              tokenFile,
              scope: {
                apiOrigin: session.apiOrigin,
                userId,
                workspaceId: state.workspaceId,
              },
              identity: {
                ownerId: identity.ownerId,
                runId: identity.runId,
                commit: identity.cliCommit,
                pid: identity.pid,
              },
            }),
          );
          return;
        }
        response.writeHead(404).end();
      } catch {
        response
          .writeHead(503)
          .end(JSON.stringify({ error: "source_unavailable" }));
      }
    });
    function authorized(
      headers: import("node:http").IncomingHttpHeaders,
    ): boolean {
      if (
        !session ||
        headers.origin ||
        !/^Bearer [a-f0-9]{64}$/.test(headers.authorization ?? "") ||
        !/^(127\.0\.0\.1|localhost):\d+$/.test(headers.host ?? "")
      )
        return false;
      const presented = Buffer.from(
          headers.authorization?.replace(/^Bearer /, "") ?? "",
        ),
        expected = Buffer.from(session.token);
      return (
        presented.length === expected.length &&
        timingSafeEqual(presented, expected)
      );
    }
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(0, "127.0.0.1", resolve);
    });
    const address = server.address();
    if (!address || typeof address === "string")
      throw new Error("Missing diagnostic listener");
    for (let attempt = 0; attempt < 50; attempt++) {
      if (
        await readFile(
          join(app.getPath("userData"), "DevToolsActivePort"),
          "utf8",
        ).catch(() => "")
      )
        break;
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    const debugText = await readFile(
      join(app.getPath("userData"), "DevToolsActivePort"),
      "utf8",
    ).catch(() => "0");
    const temporary = join(session.directory, `desktop-${randomUUID()}.tmp`);
    await writeFile(
      temporary,
      JSON.stringify({
        schemaVersion: 1,
        environmentId: session.environmentId,
        pid: process.pid,
        port: address.port,
        ownerId: launch.ownerId,
        commit: session.commit,
        sourceDigest: session.sourceDigest,
        apiOrigin: session.apiOrigin,
        debugPort: Number(debugText.split("\n")[0]) || 0,
      }),
      { mode: 0o600 },
    );
    await rename(temporary, join(session.directory, "desktop.json"));
    app.once("will-quit", () => server.close());
  },
};
