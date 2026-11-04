// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { connectAgentGuiHost, createAgentGuiRpcClient } from "./rpc-client";

const cleanup: Array<() => void> = [];
afterEach(() => { for (const dispose of cleanup.splice(0)) dispose(); vi.unstubAllGlobals(); vi.useRealTimers(); });

function connection() {
  const channel = new MessageChannel();
  const handle = createAgentGuiRpcClient(channel.port1);
  channel.port2.start();
  cleanup.push(handle.dispose, () => channel.port2.close());
  return { ...handle, host: channel.port2 };
}

describe("Agent GUI private-port client", () => {
  it("connects to its actual parent when a packaged file page sends an opaque origin", async () => {
    vi.useFakeTimers();
    const parent = { postMessage: vi.fn() };
    const frame = Object.assign(new EventTarget(), { parent, location: { protocol: "file:", origin: "file://" } });
    vi.stubGlobal("window", frame);
    const port = Object.assign(new EventTarget(), { postMessage: vi.fn(), start: vi.fn(), close() {} });
    const result = connectAgentGuiHost().then((connection) => { cleanup.push(connection.dispose); return "connected"; }, (error: unknown) => (error as { code: string }).code);
    frame.dispatchEvent(Object.assign(new Event("message"), {
      source: {}, origin: "null", ports: [port], data: { type: "multica:agent-gui:connect", snapshot: {} },
    }));
    frame.dispatchEvent(Object.assign(new Event("message"), {
      source: parent, origin: "https://untrusted.invalid", ports: [port], data: { type: "multica:agent-gui:connect", snapshot: {} },
    }));
    expect(port.start).not.toHaveBeenCalled();
    frame.dispatchEvent(Object.assign(new Event("message"), {
      source: parent, origin: "null", ports: [port], data: { type: "multica:agent-gui:connect", snapshot: {} },
    }));
    await vi.advanceTimersByTimeAsync(20_000);
    expect(await result).toBe("connected");
    expect(port.start).toHaveBeenCalledOnce();
  });

  it("keeps model discovery pending through the runtime's full claim and execution windows", async () => {
    vi.useFakeTimers();
    const port = Object.assign(new EventTarget(), { postMessage: vi.fn(), start() {}, close() {} });
    const { client, dispose } = createAgentGuiRpcClient(port as unknown as MessagePort);
    cleanup.push(dispose);
    let settled = false;
    const request = client.request("models.list", { agentId: "agent" }).finally(() => { settled = true; });
    await vi.advanceTimersByTimeAsync(100_000);
    expect(settled).toBe(false);
    port.dispatchEvent(new MessageEvent("message", { data: { type: "response", id: "1", result: { models: [], supported: true } } }));
    await expect(request).resolves.toEqual({ models: [], supported: true });
  });

  it("correlates concurrent replies and preserves host error details", async () => {
    const { client, host } = connection();
    const requests: { id: string; method: string }[] = [];
    host.onmessage = ({ data }) => {
      requests.push(data);
      if (requests.length !== 2) return;
      host.postMessage({ type: "response", id: requests[1].id, error: { message: "Access denied", code: "forbidden", status: 403 } });
      host.postMessage({ type: "response", id: requests[0].id, result: "saved draft" });
    };
    const draft = client.request("storage.read", { key: "draft" });
    const session = client.request("session.load", { sessionId: "denied" });
    await expect(session).rejects.toMatchObject({ message: "Access denied", code: "forbidden", status: 403 });
    await expect(draft).resolves.toBe("saved draft");
  });

  it("cancels only the awaiting request and never retries a send", async () => {
    const { client, host } = connection();
    const controller = new AbortController();
    let sends = 0;
    let cancellation: { type: string; id: string } | null = null;
    const observed = new Promise<void>((resolve) => {
      host.onmessage = ({ data }) => {
        if (data.type === "request") {
          sends += 1;
          controller.abort();
          host.postMessage({ type: "response", id: data.id, result: {} });
        } else if (data.type === "cancel") {
          cancellation = data;
          resolve();
        }
      };
    });
    const request = client.request("session.send", { sessionId: "session-1", content: "hello" }, { signal: controller.signal });
    await expect(request).rejects.toMatchObject({ name: "AbortError" });
    await observed;
    expect(sends).toBe(1);
    expect(cancellation).toMatchObject({ type: "cancel" });
  });

  it("unsubscribes events and rejects pending requests when the iframe closes", async () => {
    const { client, host, dispose } = connection();
    const events: unknown[] = [];
    const received = new Promise<void>((resolve) => {
      client.subscribe((event) => { events.push(event); resolve(); });
    });
    host.postMessage({ type: "event", event: { type: "connection", connected: false } });
    await received;
    expect(events).toEqual([{ type: "connection", connected: false }]);
    const pending = client.request("workspace.load", {});
    dispose();
    await expect(pending).rejects.toMatchObject({ name: "AbortError" });
    await expect(client.request("workspace.load", {})).rejects.toMatchObject({ name: "AbortError" });
  });

  it("retains startup composer appends until the source composer acknowledges them", async () => {
    const { client, host } = connection();
    const prompt = { type: "appendPrompt", sequence: 12, prompt: "Recovered draft" };
    const received = new Promise<void>((resolve) => {
      client.subscribe(() => resolve());
    });
    host.postMessage({ type: "event", event: prompt });
    await received;
    const afterMount: unknown[] = [];
    client.subscribe((event) => afterMount.push(event));
    await Promise.resolve();
    expect(afterMount).toEqual([prompt]);
    host.onmessage = ({ data }) => host.postMessage({ type: "response", id: data.id });
    await client.request("host.promptApplied", { sequence: 12 });
    const afterAcknowledgement: unknown[] = [];
    client.subscribe((event) => afterAcknowledgement.push(event));
    await Promise.resolve();
    expect(afterAcknowledgement).toEqual([]);
  });
});
