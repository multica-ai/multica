// @vitest-environment node
import { createServer, type IncomingMessage, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WebSocketServer, type WebSocket } from "ws";
import type { LocalV2Bridge } from "../../../shared/local-v2-bridge";
import { DirectLocalV2Transport, type DirectLocalV2Options } from "./local-v2-transport";

const scope = { userId: "user", workspaceId: "ws" };

interface Daemon { helloVersion?: number; capabilities?: string[]; server: Server; base: string; token: string; tickets: Set<string>; sockets: WebSocket[]; requests: IncomingMessage[]; received: unknown[]; close(): Promise<void> }

async function fakeDaemon(): Promise<Daemon> {
  const daemon = { token: "r1", tickets: new Set(), sockets: [], requests: [], received: [] } as unknown as Daemon;
  const wss = new WebSocketServer({ noServer: true });
  const json = (res: import("node:http").ServerResponse, status: number, body: unknown): void => { res.writeHead(status, { "Content-Type": "application/json" }).end(JSON.stringify(body)); };
  daemon.server = createServer((req, res) => {
    daemon.requests.push(req);
    if (req.headers.authorization !== `Bearer ${daemon.token}`) { res.writeHead(403).end("forbidden"); return; }
    if (req.url === "/local/v2/hello") { json(res, 200, { version: daemon.helloVersion ?? 2, store_generation: "g", capabilities: daemon.capabilities ?? [] }); return; }
    if (req.url?.endsWith("/local/v2/ws-ticket")) { const ticket = `ticket-${daemon.requests.length}`; daemon.tickets.add(ticket); json(res, 200, { ticket }); return; }
    if (req.url === "/local/v2/sessions/other/snapshot") { json(res, 403, { error: { kind: "account_mismatch", message: "another account" } }); return; }
    if (req.url === "/local/v2/sessions/missing/snapshot") { json(res, 404, { error: { kind: "not_found", message: "conversation missing" } }); return; }
    let body = "";
    req.on("data", (chunk) => { body += chunk; });
    req.on("end", () => json(res, 200, { path: req.url, method: req.method, body: body ? JSON.parse(body) : null }));
  });
  daemon.server.on("upgrade", (req, socket, head) => {
    const ticket = new URL(req.url ?? "", "http://x").searchParams.get("ticket") ?? "";
    if (!daemon.tickets.delete(ticket)) { socket.write("HTTP/1.1 403 Forbidden\r\n\r\n"); socket.destroy(); return; }
    wss.handleUpgrade(req, socket, head, (ws) => {
      daemon.sockets.push(ws);
      ws.on("message", (data) => daemon.received.push(JSON.parse(data.toString())));
    });
  });
  await new Promise<void>((resolve) => daemon.server.listen(0, "127.0.0.1", resolve));
  daemon.base = `127.0.0.1:${(daemon.server.address() as AddressInfo).port}`;
  daemon.close = async () => { for (const ws of daemon.sockets) ws.terminate(); wss.close(); await new Promise<void>((resolve) => daemon.server.close(() => resolve())); };
  return daemon;
}

const cleanups: (() => Promise<void> | void)[] = [];
afterEach(async () => { for (const cleanup of cleanups.splice(0)) await cleanup(); });

async function setup(hostRequest: LocalV2Bridge["hostRequest"] = vi.fn(async () => ({ ok: true })), device?: { deviceId: string; chooseRemote?: DirectLocalV2Options["chooseRemote"]; timing?: DirectLocalV2Options["timing"] }) {
  const daemon = await fakeDaemon();
  let token = daemon.token, base = daemon.base;
  const bridge = { endpoint: vi.fn(async () => ({ base, apiUrl: "https://api.multica.ai", token, fileKey: "fk" })), choose: vi.fn(), chooseRemote: vi.fn(), hostRequest };
  const transport = new DirectLocalV2Transport({ bridge, ...device });
  cleanups.push(() => transport.release(scope), () => daemon.close());
  return { daemon, transport, bridge, rotate: (next: string) => { token = next; }, move: (next: string) => { base = next; } };
}

const until = async (check: () => boolean) => { for (let i = 0; i < 200 && !check(); i++) await new Promise((r) => setTimeout(r, 10)); expect(check()).toBe(true); };

describe("DirectLocalV2Transport", () => {
  it("reads what the daemon supports once per connection", async () => {
    const { daemon, transport } = await setup();
    daemon.capabilities = ["device-link"];
    await expect(transport.capabilities()).resolves.toEqual(["device-link"]);
    daemon.capabilities = ["device-link", "conversation-focus-v1"];
    await expect(transport.capabilities()).resolves.toEqual(["device-link"]);
    await transport.send(scope, { version: 2, type: "subscribe_scope" });
    await expect(transport.capabilities()).resolves.toEqual(["device-link", "conversation-focus-v1"]);
  });

  it("reports availability from the hello probe and from each request", async () => {
    const { daemon, transport } = await setup();
    const availability = vi.fn();
    transport.onAvailability(availability);
    await transport.probe();
    expect(availability).toHaveBeenLastCalledWith({ state: "connected" });
    await expect(transport.request(scope, "GET", "/sessions/other/snapshot")).rejects.toThrow(/^account_mismatch: /);
    expect(availability).toHaveBeenLastCalledWith({ state: "unavailable", reason: "account_mismatch" });
    daemon.helloVersion = 3;
    await transport.probe();
    expect(availability).toHaveBeenLastCalledWith({ state: "unavailable", reason: "incompatible" });
    await transport.request(scope, "GET", "/sessions");
    expect(availability).toHaveBeenLastCalledWith({ state: "connected" });
    expect(transport.availability()).toEqual({ state: "connected" });
  });

  it("reports an unreachable daemon as offline", async () => {
    const { transport, move } = await setup();
    move("127.0.0.1:1");
    await transport.probe();
    expect(transport.availability()).toEqual({ state: "unavailable", reason: "daemon_offline" });
  });

  it("sends scoped requests with the renderer token and keeps the daemon's error kinds", async () => {
    const { daemon, transport } = await setup();
    expect(await transport.request(scope, "POST", "/sessions/c1/commands", { type: "send" })).toEqual({ path: "/local/v2/sessions/c1/commands", method: "POST", body: { type: "send" } });
    const headers = daemon.requests[0]!.headers;
    expect(headers.authorization).toBe("Bearer r1");
    expect(headers["x-multica-user-id"]).toBe("user");
    expect(headers["x-workspace-id"]).toBe("ws");
    expect(headers["x-multica-api-origin"]).toBe("https://api.multica.ai");
    await expect(transport.request(scope, "GET", "/sessions/missing/snapshot")).rejects.toThrow(/^not_found: conversation missing$/);
    await expect(transport.request(scope, "GET", "/../v1/hello")).rejects.toThrow(/^invalid_request: /);
  });

  it("re-reads rotated credentials once", async () => {
    const { daemon, transport, bridge, rotate } = await setup();
    await transport.request(scope, "GET", "/sessions");
    daemon.token = "r2"; rotate("r2");
    await transport.request(scope, "GET", "/sessions");
    expect(bridge.endpoint).toHaveBeenCalledTimes(2);
  });

  it("builds file URLs with the file key and the scope", async () => {
    const { daemon, transport } = await setup();
    expect(transport.fileUrl(scope, "/attachments/a1")).toBe("");
    await transport.request(scope, "GET", "/sessions");
    expect(transport.fileUrl(scope, "/attachments/a1")).toBe(`http://${daemon.base}/local/v2/attachments/a1?file_key=fk&api_origin=https%3A%2F%2Fapi.multica.ai&user_id=user&workspace_id=ws`);
    expect(transport.fileUrl(scope, "/preview/content?root=r&path=p")).toMatch(/\/preview\/content\?root=r&path=p&file_key=fk&/);
  });

  it("opens the WebSocket with a ticket, relays messages and answers host requests through main", async () => {
    const hostRequest = vi.fn(async (_device: string, _scope: unknown, kind: string) => { if (kind === "fail") throw new Error("Error invoking remote method 'local-v2:host-request': Error: declined: The user declined"); return { approved: true }; });
    const { daemon, transport } = await setup(hostRequest);
    const messages = vi.fn(), connection = vi.fn();
    transport.subscribe(messages); transport.onConnection(connection);
    await transport.send(scope, { version: 2, type: "subscribe_scope", after_scope_seq: 0 });
    await until(() => daemon.received.length === 1 && daemon.sockets.length === 1);
    expect(connection).toHaveBeenCalledWith(scope, true);
    const ws = daemon.sockets[0]!;
    ws.send(JSON.stringify({ version: 2, type: "scope.summaries", scope_seq: 1, summaries: [] }));
    ws.send(JSON.stringify({ version: 2, type: "host.request", request_id: "h1", kind: "settings.confirm", payload: { operation: "x" } }));
    ws.send(JSON.stringify({ version: 2, type: "host.request", request_id: "h2", kind: "fail", payload: {} }));
    await until(() => daemon.received.length === 3);
    expect(messages).toHaveBeenCalledWith(scope, expect.objectContaining({ type: "scope.summaries" }));
    expect(messages).toHaveBeenCalledTimes(1);
    expect(hostRequest).toHaveBeenCalledWith("local", scope, "settings.confirm", { operation: "x" });
    expect(daemon.received.slice(1)).toEqual(expect.arrayContaining([
      { version: 2, type: "host.reply", request_id: "h1", result: { approved: true } },
      { version: 2, type: "host.reply", request_id: "h2", error: { kind: "declined", message: "The user declined" } },
    ]));
  });

  it("reconnects a dropped socket with a new ticket and reports the drop", async () => {
    const { daemon, transport } = await setup();
    const connection = vi.fn();
    transport.onConnection(connection);
    await transport.send(scope, { version: 2, type: "subscribe_scope", after_scope_seq: 0 });
    await until(() => daemon.sockets.length === 1);
    daemon.sockets[0]!.terminate();
    await until(() => daemon.sockets.length === 2);
    await until(() => connection.mock.calls.filter(([, up]) => up).length === 2);
    expect(connection).toHaveBeenCalledWith(scope, false);
  });

  it("retries a failed initial handshake when the peer becomes reachable", async () => {
    const { daemon, transport } = await setup(undefined, { deviceId: "peer-1" });
    const upgrade = daemon.server.listeners("upgrade")[0]!;
    daemon.server.removeAllListeners("upgrade");
    let unavailable = true;
    daemon.server.on("upgrade", (req, socket, head) => {
      if (unavailable) { socket.write("HTTP/1.1 502 Bad Gateway\r\n\r\n"); socket.destroy(); return; }
      upgrade(req, socket, head);
    });
    const connection = vi.fn(); transport.onConnection(connection);
    await expect(transport.send(scope, { version: 2, type: "subscribe_scope", after_scope_seq: 0 })).rejects.toThrow();
    unavailable = false;
    // The daemon sees the socket before this client hears it opened.
    await until(() => daemon.sockets.length === 1 && connection.mock.lastCall?.[1] === true);
    expect(connection).toHaveBeenLastCalledWith(scope, true);
    expect(daemon.requests.filter((request) => request.url?.endsWith("/ws-ticket")).length).toBeGreaterThan(1);
    expect(daemon.received).toEqual([]);
  });

  it("preserves account errors instead of reporting them as a network outage", async () => {
    const { daemon, transport } = await setup(undefined, { deviceId: "peer-1" });
    daemon.server.removeAllListeners("request");
    daemon.server.on("request", (_req, res) => { res.writeHead(403, { "Content-Type": "application/json" }).end(JSON.stringify({ error: { kind: "account_mismatch", message: "another account" } })); });
    await expect(transport.send(scope, { version: 2, type: "subscribe_scope", after_scope_seq: 0 })).rejects.toThrow("account_mismatch");
    expect(transport.availability()).toEqual({ state: "unavailable", reason: "account_mismatch" });
  });

  it("cancels a pending handshake on release without resurrecting the scope", async () => {
    const { daemon, transport } = await setup();
    daemon.server.removeAllListeners("upgrade");
    let waiting = false;
    let closeWaiting = () => {};
    daemon.server.on("upgrade", (_req, socket) => { waiting = true; closeWaiting = () => { socket.destroy(); }; });
    try {
      const pending = expect(transport.send(scope, { version: 2, type: "subscribe_scope", after_scope_seq: 0 })).rejects.toThrow();
      await until(() => waiting);
      transport.release(scope);
      await pending;
      const tickets = () => daemon.requests.filter((request) => request.url?.endsWith("/ws-ticket")).length;
      const before = tickets();
      await new Promise((resolve) => setTimeout(resolve, 650));
      expect(tickets()).toBe(before);
      expect(daemon.sockets).toHaveLength(0);
    } finally { closeWaiting(); }
  });

  it("times out a stalled handshake and obtains a fresh ticket for its retry", async () => {
    vi.useFakeTimers();
    const sockets: HandshakeSocket[] = [];
    class HandshakeSocket extends EventTarget {
      static OPEN = 1; static CONNECTING = 0;
      readyState = 0;
      constructor(_url: string) { super(); sockets.push(this); }
      close() { this.readyState = 3; this.dispatchEvent(new Event("close")); }
      send = vi.fn();
    }
    const request = vi.fn(async () => new Response(JSON.stringify({ ticket: "fresh-ticket" }), { headers: { "Content-Type": "application/json" } }));
    const transport = new DirectLocalV2Transport({
      bridge: { endpoint: vi.fn(async () => ({ base: "127.0.0.1:1", apiUrl: "https://api.multica.ai", token: "r1", fileKey: "fk" })), choose: vi.fn(), chooseRemote: vi.fn(), hostRequest: vi.fn() },
      WebSocket: HandshakeSocket as unknown as NonNullable<DirectLocalV2Options["WebSocket"]>, fetch: request,
    });
    try {
      const pending = expect(transport.send(scope, { version: 2, type: "subscribe_scope", after_scope_seq: 0 })).rejects.toThrow("handshake timed out");
      await vi.advanceTimersByTimeAsync(0);
      expect(sockets).toHaveLength(1);
      await vi.advanceTimersByTimeAsync(20_000);
      await pending;
      expect(sockets[0]!.readyState).toBe(3);
      await vi.advanceTimersByTimeAsync(500);
      expect(sockets).toHaveLength(2);
      expect(request).toHaveBeenCalledTimes(2);
      expect(sockets.every((socket) => socket.send.mock.calls.length === 0)).toBe(true);
    } finally { transport.release(scope); await vi.advanceTimersByTimeAsync(0); vi.useRealTimers(); }
  });

  it("wakes an exhausted WebSocket retry delay as soon as the local daemon reports a ready path", async () => {
    vi.useFakeTimers();
    let online = false;
    const sockets: LinkSocket[] = [];
    class LinkSocket extends EventTarget {
      static OPEN = 1; static CONNECTING = 0;
      readyState = 0;
      constructor(_url: string) {
        super(); sockets.push(this);
        queueMicrotask(() => { this.readyState = online ? 1 : 3; this.dispatchEvent(new Event(online ? "open" : "error")); });
      }
      close() { this.readyState = 3; this.dispatchEvent(new Event("close")); }
      send = vi.fn();
    }
    let events!: ReadableStreamDefaultController<Uint8Array>;
    const request = vi.fn(async (url: string | URL | Request) => {
      if (String(url).endsWith("/link-events")) return new Response(new ReadableStream({ start(controller) { events = controller; } }), { headers: { "Content-Type": "text/event-stream" } });
      return new Response('{"ticket":"fresh"}', { headers: { "Content-Type": "application/json" } });
    });
    const transport = new DirectLocalV2Transport({
      bridge: { endpoint: vi.fn(async () => ({ base: "127.0.0.1:1", apiUrl: "https://api.multica.ai", token: "r1", fileKey: "fk" })), choose: vi.fn(), chooseRemote: vi.fn(), hostRequest: vi.fn() },
      deviceId: "peer-1", WebSocket: LinkSocket as unknown as NonNullable<DirectLocalV2Options["WebSocket"]>, fetch: request,
    });
    const connection = vi.fn(); transport.onConnection(connection);
    try {
      await expect(transport.send(scope, { version: 2, type: "subscribe_scope", after_scope_seq: 0 })).rejects.toThrow();
      for (const delay of [500, 1000, 2000, 4000, 8000]) await vi.advanceTimersByTimeAsync(delay);
      expect(sockets).toHaveLength(6);
      online = true;
      events.enqueue(new TextEncoder().encode("event: unknown\ndata: {}\n\nevent: link.con"));
      await vi.advanceTimersByTimeAsync(0);
      expect(sockets).toHaveLength(6);
      events.enqueue(new TextEncoder().encode("nected\r\ndata: {}\r\n\r\n"));
      await vi.advanceTimersByTimeAsync(0);
      expect(sockets).toHaveLength(7);
      expect(connection).toHaveBeenLastCalledWith(scope, true);
      expect(sockets.every((socket) => socket.send.mock.calls.length === 0)).toBe(true);
    } finally { events.close(); transport.release(scope); await vi.advanceTimersByTimeAsync(0); vi.useRealTimers(); }
  });

  it("reaches another device under this daemon's /local/devices route with a stable client ID", async () => {
    const chooseRemote = vi.fn<NonNullable<DirectLocalV2Options["chooseRemote"]>>(async () => ({ id: "f1" }));
    const { daemon, transport, bridge } = await setup(undefined, { deviceId: "peer 1", chooseRemote });
    await transport.request(scope, "GET", "/sessions");
    await transport.request(scope, "GET", "/sessions");
    expect(daemon.requests.map((request) => request.url)).toEqual(["/local/devices/peer%201/local/v2/sessions", "/local/devices/peer%201/local/v2/sessions"]);
    const [first, second] = daemon.requests.map((request) => request.headers["x-multica-client-id"]);
    expect(first).toMatch(/^[0-9a-f-]{36}$/);
    expect(second).toBe(first);
    expect(transport.fileUrl(scope, "/attachments/a1")).toContain(`http://${daemon.base.replace("127.0.0.1", "localhost")}/local/devices/peer%201/local/v2/attachments/a1?`);
    // Another device has no OS picker here.
    await expect(transport.choose(scope, { purpose: "folder_project" })).resolves.toEqual({ id: "f1" });
    expect(bridge.choose).not.toHaveBeenCalled();
    expect(chooseRemote).toHaveBeenCalledWith(scope, { purpose: "folder_project" });
  });

  it("keeps another device's requests off the host this device's requests use", async () => {
    const local = await setup();
    await local.transport.request(scope, "GET", "/sessions");
    expect(local.daemon.requests[0]!.headers.host).toBe(local.daemon.base);
    const remote = await setup(undefined, { deviceId: "peer" });
    await remote.transport.request(scope, "GET", "/sessions");
    expect(remote.daemon.requests[0]!.headers.host).toBe(remote.daemon.base.replace("127.0.0.1", "localhost"));
  });

  it("needs Device link support from another device's daemon", async () => {
    const { daemon, transport } = await setup(undefined, { deviceId: "peer-1" });
    daemon.server.removeAllListeners("request");
    daemon.server.on("request", (_req, res) => { res.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({ version: 2, capabilities: ["claude"] })); });
    await transport.probe();
    expect(transport.availability()).toEqual({ state: "unavailable", reason: "incompatible" });
  });

  it("reports another device turning Device link off", async () => {
    const { daemon, transport } = await setup(undefined, { deviceId: "peer-1" });
    daemon.server.removeAllListeners("request");
    daemon.server.on("request", (_req, res) => { res.writeHead(503, { "Content-Type": "application/json" }).end(JSON.stringify({ error: { kind: "device_link_off", message: "off" } })); });
    await expect(transport.request(scope, "GET", "/sessions")).rejects.toThrow(/^device_link_off: /);
    expect(transport.availability()).toEqual({ state: "unavailable", reason: "device_link_off" });
  });

  it("replaces a stalled remote read after reconnect without replaying writes or other scopes", async () => {
    const { daemon, transport } = await setup(undefined, { deviceId: "peer-1" });
    await transport.send(scope, { version: 2, type: "subscribe_scope", after_scope_seq: 0 });
    await until(() => daemon.sockets.length === 1);
    const handler = daemon.server.listeners("request")[0]!;
    let reads = 0, writes = 0, otherReads = 0;
    const held: import("node:http").ServerResponse[] = [];
    daemon.server.removeAllListeners("request");
    daemon.server.on("request", (req, res) => {
      if (req.url?.endsWith("/snapshot")) {
        if (req.headers["x-workspace-id"] === "other") { otherReads++; held.push(res); return; }
        reads++;
        if (reads === 1) { res.writeHead(200, { "Content-Type": "application/json" }); res.write('{"stalled":'); held.push(res); return; }
        res.writeHead(200, { "Content-Type": "application/json" }).end('{"restored":true}'); return;
      }
      if (req.url?.endsWith("/commands")) { writes++; held.push(res); return; }
      handler(req, res);
    });
    cleanups.unshift(() => { for (const res of held) res.destroy(); });
    const other = transport.request({ ...scope, workspaceId: "other" }, "GET", "/sessions/c1/snapshot").catch(() => undefined);
    const read = transport.request(scope, "GET", "/sessions/c1/snapshot");
    const write = transport.request(scope, "POST", "/sessions/c1/commands", { type: "send" });
    const written = write.catch(() => undefined);
    await until(() => reads === 1 && writes === 1 && otherReads === 1);
    daemon.sockets[0]!.terminate();
    await until(() => daemon.sockets.length === 2);
    const result = await Promise.race([read, new Promise((resolve) => setTimeout(() => resolve("still stalled"), 300))]);
    expect(result).toEqual({ restored: true });
    expect(reads).toBe(2);
    expect(writes).toBe(1);
    expect(otherReads).toBe(1);
    for (const res of held) res.destroy();
    await Promise.all([written, other]);
  });

  it("cancels a remote read on scope release without retrying it", async () => {
    const { daemon, transport } = await setup(undefined, { deviceId: "peer-1" });
    let held: import("node:http").ServerResponse | undefined;
    daemon.server.removeAllListeners("request");
    daemon.server.on("request", (_req, res) => { held = res; });
    cleanups.unshift(() => { held?.destroy(); });
    const read = expect(transport.request(scope, "GET", "/sessions/c1/snapshot")).rejects.toThrow("scope_released");
    await until(() => !!held);
    transport.release(scope);
    await read;
  });

  it("passes host requests to main with the device that asked", async () => {
    const hostRequest = vi.fn(async () => ({ ok: true }));
    const { daemon, transport } = await setup(hostRequest, { deviceId: "peer-1" });
    await transport.send(scope, { version: 2, type: "subscribe_scope", after_scope_seq: 0 });
    await until(() => daemon.sockets.length === 1);
    daemon.sockets[0]!.send(JSON.stringify({ version: 2, type: "host.request", request_id: "h1", kind: "settings.confirm", payload: {} }));
    await until(() => hostRequest.mock.calls.length === 1);
    expect(hostRequest).toHaveBeenCalledWith("peer-1", scope, "settings.confirm", {});
  });

  it("keeps traffic for a bounded number of turns that never report an end", async () => {
    const records: { stage: string; turnId?: string; count?: number }[] = [];
    const remote = await setup(undefined, { deviceId: "peer-a", timing: (record) => records.push(record) });
    await remote.transport.send(scope, { version: 2, type: "subscribe", conversation_id: "c1", after_seq: 0, store_generation: "g" });
    await vi.waitFor(() => expect(remote.daemon.sockets).toHaveLength(1));
    const event = (turn: string, type: string) => remote.daemon.sockets[0]!.send(JSON.stringify({ version: 2, type: "conversation.events", conversation_id: "c1", events: [{ turn_id: turn, type }] }));
    event("oldest", "item.delta");
    for (let turn = 0; turn < 64; turn++) event(`t${turn}`, "item.delta");
    event("oldest", "turn.completed");
    await vi.waitFor(() => expect(records.map((record) => record.stage)).toContain("link.event_received"));
    // Its first batch fell out of the cap, so only the last one is counted.
    expect(records.find((record) => record.stage === "link.event_received")).toMatchObject({ turnId: "oldest", count: 1 });
  });

  it("records another device's link and leaves this device's silent", async () => {
    const records: { stage: string; peer?: string; reason?: string; bytes?: number; conversationId?: string; turnId?: string; count?: number }[] = [];
    const remote = await setup(undefined, { deviceId: "peer-a", timing: (record) => records.push(record) });
    await remote.transport.send(scope, { version: 2, type: "subscribe", conversation_id: "c1", after_seq: 0, store_generation: "g" });
    await vi.waitFor(() => expect(remote.daemon.sockets).toHaveLength(1));
    remote.daemon.sockets[0]!.send(JSON.stringify({ version: 2, type: "subscription.status", conversation_id: "c1", status: "subscribed", high_water: 0 }));
    remote.daemon.sockets[0]!.send(JSON.stringify({ version: 2, type: "subscription.status", conversation_id: "c1", status: "live", high_water: 0 }));
    await vi.waitFor(() => expect(records.map((record) => record.stage)).toContain("ws.catch_up"));
    const batch = (turnType: string) => JSON.stringify({ version: 2, type: "conversation.events", conversation_id: "c1", events: [{ turn_id: "t1", type: turnType }] });
    remote.daemon.sockets[0]!.send(batch("item.delta"));
    remote.daemon.sockets[0]!.send(batch("turn.completed"));
    await vi.waitFor(() => expect(records.map((record) => record.stage)).toContain("link.event_received"));
    expect(records.filter((record) => record.stage === "link.event_received")).toHaveLength(1);
    expect(records.find((record) => record.stage === "link.event_received")).toMatchObject({ turnId: "t1", count: 2, bytes: batch("item.delta").length + batch("turn.completed").length });
    remote.daemon.sockets[0]!.close(1001);
    await vi.waitFor(() => expect(records.map((record) => record.stage)).toContain("ws.closed"));
    expect(records.find((record) => record.stage === "ws.closed")?.reason).toBe("ws_going_away");
    expect(records.map((record) => record.stage)).toEqual(expect.arrayContaining(["ws.ticket", "ws.open", "ws.subscribe", "ws.subscribed", "ws.retry"]));
    expect(records.every((record) => record.peer === "peer-a")).toBe(true);

    await remote.transport.request(scope, "POST", "/sessions/c1/commands", { type: "send", request_id: "req-1" });
    const command = remote.daemon.requests.find((req) => req.url?.endsWith("/commands"));
    expect(command?.headers["x-multica-request-id"]).toBe("req-1");

    await remote.transport.request(scope, "GET", "/sessions/c1/snapshot");
    expect(records.filter((record) => record.stage.startsWith("snapshot.")).map((record) => record.stage)).toEqual(["snapshot.request", "snapshot.first_byte", "snapshot.received", "snapshot.parsed"]);
    expect(records.find((record) => record.stage === "snapshot.received")?.bytes).toBeGreaterThan(0);

    const local: unknown[] = [];
    const own = await setup(undefined, { deviceId: "local", timing: (record) => local.push(record) });
    await own.transport.send(scope, { version: 2, type: "subscribe", conversation_id: "c1", after_seq: 0, store_generation: "g" });
    await own.transport.request(scope, "POST", "/sessions/c1/commands", { type: "send", request_id: "req-1" });
    expect(local).toEqual([]);
    expect(own.daemon.requests.find((req) => req.url?.endsWith("/commands"))?.headers["x-multica-request-id"]).toBeUndefined();
  });
});

describe("uploads", () => {
  class FakeXHR {
    static last: FakeXHR;
    upload = new EventTarget();
    private events = new EventTarget();
    status = 0;
    responseText = "";
    headers: Record<string, string> = {};
    aborted = false;
    body?: unknown;
    constructor() { FakeXHR.last = this; }
    open() {}
    setRequestHeader(name: string, value: string) { this.headers[name] = value; }
    getResponseHeader() { return "application/json"; }
    addEventListener(type: string, listener: () => void) { this.events.addEventListener(type, listener); }
    send(body: unknown) { this.body = body; }
    abort() { this.aborted = true; this.events.dispatchEvent(new Event("abort")); }
    respond(status: number, body: unknown) { this.status = status; this.responseText = JSON.stringify(body); this.events.dispatchEvent(new Event("load")); }
    fail() { this.events.dispatchEvent(new Event("error")); }
    progress() { this.upload.dispatchEvent(new Event("progress")); }
  }
  const bridge = { endpoint: async () => ({ base: "127.0.0.1:1", apiUrl: "https://api", token: "t", fileKey: "k" }) } as unknown as LocalV2Bridge;
  const file = new File(["x"], "a b.txt", { type: "text/plain" });
  const started = <T,>(value: Promise<T>) => { void value.catch(() => undefined); return value; };

  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  it("sends the id the client chose and keeps going while bytes move", async () => {
    const transport = new DirectLocalV2Transport({ bridge, XMLHttpRequest: FakeXHR as never });
    const upload = started(transport.upload(scope, file, { id: "abc" }));
    await vi.advanceTimersByTimeAsync(0);
    expect(FakeXHR.last.headers).toMatchObject({ "X-Attachment-ID": "abc", "X-File-Name": "a%20b.txt", "Content-Type": "text/plain" });
    // Far past any total deadline, but the file is still moving.
    for (let minute = 0; minute < 10; minute++) { await vi.advanceTimersByTimeAsync(59_000); FakeXHR.last.progress(); }
    FakeXHR.last.respond(201, { id: "abc", name: "a b.txt", mime_type: "text/plain", size: 1 });
    await expect(upload).resolves.toMatchObject({ id: "abc" });
  });

  it("gives up when the file stops moving, without reporting the device down", async () => {
    const transport = new DirectLocalV2Transport({ bridge, deviceId: "peer", XMLHttpRequest: FakeXHR as never });
    const upload = started(transport.upload(scope, file));
    await vi.advanceTimersByTimeAsync(60_000);
    await expect(upload).rejects.toThrow(/^upload_stalled: /);
    expect(FakeXHR.last.aborted).toBe(true);
    expect(transport.availability()).toBeNull();
  });

  it("does not report the device down when one upload fails", async () => {
    const transport = new DirectLocalV2Transport({ bridge, deviceId: "peer", XMLHttpRequest: FakeXHR as never });
    const upload = started(transport.upload(scope, file));
    await vi.advanceTimersByTimeAsync(0);
    FakeXHR.last.fail();
    await expect(upload).rejects.toThrow(/^upload_failed: /);
    expect(transport.availability()).toBeNull();
  });

  it("stops when the caller cancels", async () => {
    const transport = new DirectLocalV2Transport({ bridge, XMLHttpRequest: FakeXHR as never });
    const controller = new AbortController();
    const upload = started(transport.upload(scope, file, { signal: controller.signal }));
    await vi.advanceTimersByTimeAsync(0);
    controller.abort();
    await expect(upload).rejects.toThrow();
    expect(FakeXHR.last.aborted).toBe(true);
  });

  it("carries the daemon's error kind", async () => {
    const transport = new DirectLocalV2Transport({ bridge, XMLHttpRequest: FakeXHR as never });
    const upload = started(transport.upload(scope, file));
    await vi.advanceTimersByTimeAsync(0);
    FakeXHR.last.respond(413, { error: { kind: "file_too_large", message: "The file exceeds 25 MB" } });
    await expect(upload).rejects.toThrow("file_too_large: The file exceeds 25 MB");
  });
});
