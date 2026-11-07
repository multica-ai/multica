// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { WebSocketServer, type WebSocket } from "ws";
import { ComputerChannel, type ComputerClientState } from "./channel";
import { COMPUTER_CAPABILITY_VERSION, COMPUTER_HOST_PROTOCOL_VERSION } from "./version";

type Message = { jsonrpc: string; id?: string | number; method?: string; params?: any; result?: any; error?: any };

const state = (user: string | null): ComputerClientState => ({
  user: { id: user },
  capabilities: { computer: { version: COMPUTER_CAPABILITY_VERSION, prompts: { conversation: "c", task: "t", taskBrief: "b" }, runtime: { executable: "/x", entry: "/y" } } },
});

const cleanups: (() => void)[] = [];
afterEach(() => { while (cleanups.length) cleanups.pop()!(); });

/** A daemon stand-in that records every frame Desktop sends. */
async function daemon() {
  const server = new WebSocketServer({ port: 0, path: "/local/v2/computer-host" });
  await new Promise<void>((resolve) => server.once("listening", () => resolve()));
  const received: Message[] = [];
  let socket: WebSocket | undefined;
  const waiters: (() => void)[] = [];
  server.on("connection", (ws) => {
    socket = ws;
    ws.on("message", (data) => { received.push(JSON.parse(data.toString())); waiters.splice(0).forEach((w) => w()); });
  });
  const next = async (predicate: (m: Message) => boolean) => {
    for (;;) {
      const found = received.find(predicate);
      if (found) return found;
      await new Promise<void>((resolve) => waiters.push(resolve));
    }
  };
  const send = (message: Message) => socket!.send(JSON.stringify(message));
  cleanups.push(() => server.close());
  return { base: `127.0.0.1:${(server.address() as { port: number }).port}`, received, next, send };
}

function channel(base: string, overrides: Partial<ConstructorParameters<typeof ComputerChannel>[0]> = {}) {
  let user: string | null = "u";
  const handle = vi.fn(async (_envelope: unknown, _signal: AbortSignal): Promise<unknown> => ({ ok: true }));
  const log = vi.fn();
  const c = new ComputerChannel({
    endpoint: async () => ({ base, token: "t" }), clientInfo: { name: "multica-desktop", version: "9.9.9" },
    describe: () => state(user), handle, stop: vi.fn(), log,
    timing: { handshakeMs: 100, retryMs: 20, incompatibleRetryMs: 60_000 }, ...overrides,
  });
  void c.run();
  cleanups.push(() => c.close());
  return { c, handle, log, setUser: (value: string | null) => { user = value; } };
}

async function ready(d: Awaited<ReturnType<typeof daemon>>) {
  const init = await d.next((m) => m.method === "initialize");
  d.send({ jsonrpc: "2.0", id: init.id, result: { protocolVersion: COMPUTER_HOST_PROTOCOL_VERSION, capabilities: { computer: { status: "enabled" } } } });
  await d.next((m) => m.method === "notifications/initialized");
  return init;
}

describe("Computer Host channel", () => {
  it("reports its capability in initialize before serving anything", async () => {
    const d = await daemon();
    const { handle } = channel(d.base);
    const init = await d.next((m) => m.method === "initialize");
    expect(init.params).toMatchObject({ protocolVersion: COMPUTER_HOST_PROTOCOL_VERSION, clientInfo: { version: "9.9.9" }, user: { id: "u" }, capabilities: { computer: { version: COMPUTER_CAPABILITY_VERSION } } });
    d.send({ jsonrpc: "2.0", id: "early", method: "computer/control", params: { capabilityVersion: COMPUTER_CAPABILITY_VERSION, envelope: {} } });
    d.send({ jsonrpc: "2.0", id: init.id, result: { capabilities: { computer: { status: "enabled" } } } });
    await d.next((m) => m.method === "notifications/initialized");
    expect(handle).not.toHaveBeenCalled();
    expect(d.received.some((m) => m.id === "early")).toBe(false);
  });

  it("serves patch-compatible control requests and refuses incompatible runtimes", async () => {
    const d = await daemon();
    const { handle } = channel(d.base);
    await ready(d);
    const [major, minor] = COMPUTER_CAPABILITY_VERSION.split(".");
    d.send({ jsonrpc: "2.0", id: "1", method: "computer/control", params: { capabilityVersion: `${major}.${minor}.99`, envelope: { op: "status" } } });
    expect((await d.next((m) => m.id === "1")).result).toEqual({ ok: true });
    expect(handle).toHaveBeenCalledWith({ op: "status" }, expect.any(AbortSignal));
    d.send({ jsonrpc: "2.0", id: "2", method: "computer/control", params: { capabilityVersion: `${major}.${Number(minor) + 1}.0`, envelope: { op: "status" } } });
    expect((await d.next((m) => m.id === "2")).error).toMatchObject({ code: -32000, data: { code: "runtime_outdated" } });
    expect(handle).toHaveBeenCalledTimes(1);
  });

  it("returns domain errors with their code and aborts cancelled requests", async () => {
    const d = await daemon();
    let seen: AbortSignal | undefined;
    const { handle } = channel(d.base);
    handle.mockImplementation(async (_e, signal) => { seen = signal; await new Promise((_, reject) => signal.addEventListener("abort", () => reject(new Error("execution_ended: cancelled")))); });
    await ready(d);
    d.send({ jsonrpc: "2.0", id: "slow", method: "computer/control", params: { capabilityVersion: COMPUTER_CAPABILITY_VERSION, envelope: {} } });
    await vi.waitFor(() => expect(seen).toBeDefined());
    d.send({ jsonrpc: "2.0", method: "notifications/cancelled", params: { requestId: "slow" } });
    expect((await d.next((m) => m.id === "slow")).error).toMatchObject({ message: "execution_ended: cancelled", data: { code: "execution_ended" } });
  });

  it("reports account changes once, without reconnecting", async () => {
    const d = await daemon();
    const { c, setUser } = channel(d.base);
    await ready(d);
    c.update();
    setUser("other");
    c.update();
    c.update();
    const updates = () => d.received.filter((m) => m.method === "notifications/client/updated");
    await vi.waitFor(() => expect(updates()).toHaveLength(1));
    expect(updates()[0].params).toMatchObject({ user: { id: "other" } });
    expect(d.received.filter((m) => m.method === "initialize")).toHaveLength(1);
  });

  it("backs off when the daemon does not answer initialize", async () => {
    const d = await daemon();
    const { log } = channel(d.base);
    await d.next((m) => m.method === "initialize");
    await vi.waitFor(() => expect(log).toHaveBeenCalledWith(expect.stringContaining("no initialize response")));
    await new Promise((resolve) => setTimeout(resolve, 200));
    expect(d.received.filter((m) => m.method === "initialize")).toHaveLength(1);
  });
});
