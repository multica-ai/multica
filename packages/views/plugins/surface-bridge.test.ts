import { beforeEach, describe, expect, it, vi } from "vitest";

const mockCall = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({ api: { callPluginAction: mockCall } }));

import { createSurfaceBridge } from "./surface-bridge";

const TOKEN = "single-use-launch-proof";

function connectMessage(source: Window | null, port: MessagePort, challenge = TOKEN, version = 2) {
  const event = new MessageEvent("message", {
    data: { type: "multica:plugin-bridge-connect", version, challenge },
  });
  Object.defineProperty(event, "source", { value: source, configurable: true });
  Object.defineProperty(event, "ports", { value: [port], configurable: true });
  window.dispatchEvent(event);
}

function connectedBridge(
  options: Parameters<typeof createSurfaceBridge>[0] = { installationId: "installation-1", bridgeToken: TOKEN },
) {
  const bridge = createSurfaceBridge(options);
  const posted: unknown[] = [];
  const frame = { contentWindow: {} } as unknown as HTMLIFrameElement;
  const channel = new MessageChannel();
  channel.port2.onmessage = (event) => {
    if ((event.data as { kind?: string } | null)?.kind !== "theme") posted.push(event.data);
  };
  channel.port2.start();
  bridge.connect(frame, {});
  connectMessage(frame.contentWindow, channel.port1, options.bridgeToken);
  return { bridge, frame, port: channel.port2, posted };
}

async function answered(port: MessagePort, posted: unknown[]) {
  const id = "probe:bridge-drained";
  port.postMessage({ id, kind: "action", method: "GET", path: "/probe" });
  const sent = (message: unknown) => (message as { id?: string } | null)?.id === id;
  await vi.waitFor(() => {
    if (!posted.some(sent)) throw new Error("bridge has not answered the probe yet");
  });
  posted.splice(posted.findIndex(sent), 1);
}

const portDrain = () => new Promise<void>((resolve) => {
  const channel = new MessageChannel();
  channel.port1.onmessage = () => {
    channel.port1.close();
    channel.port2.close();
    resolve();
  };
  channel.port1.start();
  channel.port2.postMessage(0);
});

describe("surface bridge", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCall.mockResolvedValue({ ok: true });
  });

  it("forwards an allowed request with the installation that owns the channel", async () => {
    const { port } = connectedBridge({ installationId: "installation-1", bridgeToken: TOKEN, issueId: "issue-1" });
    port.postMessage({ id: "r1", kind: "action", method: "GET", path: "/context" });

    await vi.waitFor(() => expect(mockCall).toHaveBeenCalledWith("installation-1", expect.objectContaining({
      method: "GET",
      path: "/context",
      issueId: "issue-1",
    })));
  });

  it("refuses paths and methods outside the Action API before fetch", async () => {
    const { port, posted } = connectedBridge();
    port.postMessage({ id: "bad-path", kind: "action", method: "GET", path: "/me" });
    port.postMessage({ id: "bad-method", kind: "action", method: "TRACE", path: "/context" });
    await answered(port, posted);

    expect(mockCall).not.toHaveBeenCalled();
    expect(posted).toContainEqual(expect.objectContaining({ id: "bad-path", ok: false, status: 400 }));
  });

  it("passes a refusal status through to the plugin", async () => {
    mockCall.mockRejectedValue(Object.assign(new Error("not granted"), { status: 403 }));
    const { port, posted } = connectedBridge();
    port.postMessage({ id: "r1", kind: "action", method: "POST", path: "/issues/i1/comments", body: {} });
    await vi.waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0]).toMatchObject({ id: "r1", ok: false, status: 403 });
  });

  it("clamps resize requests", async () => {
    const heights: number[] = [];
    const { port, posted } = connectedBridge({
      installationId: "installation-1",
      bridgeToken: TOKEN,
      onResize: (height) => heights.push(height),
    });
    port.postMessage({ id: "large", kind: "ui.resize", height: 10_000_000 });
    port.postMessage({ id: "small", kind: "ui.resize", height: -5 });
    await answered(port, posted);
    expect(heights).toEqual([4000, 0]);
  });

  it("lets only a composer-opened surface insert once into its captured draft", async () => {
    const insert = vi.fn(() => true);
    const { port, posted } = connectedBridge({
      installationId: "installation-1",
      bridgeToken: TOKEN,
      onComposerInsert: insert,
    });
    port.postMessage({ id: "first", kind: "composer.insert", format: "markdown", text: "**draft**" });
    await vi.waitFor(() => expect(posted).toContainEqual({ id: "first", ok: true, status: 200, data: null }));
    port.postMessage({ id: "second", kind: "composer.insert", format: "markdown", text: "again" });
    await vi.waitFor(() => expect(posted).toContainEqual(expect.objectContaining({ id: "second", ok: false, status: 403 })));
    expect(insert).toHaveBeenCalledExactlyOnceWith("**draft**");
  });

  it("keeps the one-shot grant consumed while an asynchronous host check is pending", async () => {
    let resolveInsert!: (allowed: boolean) => void;
    const insert = vi.fn(() => new Promise<boolean>((resolve) => { resolveInsert = resolve; }));
    const { port, posted } = connectedBridge({
      installationId: "installation-1",
      bridgeToken: TOKEN,
      onComposerInsert: insert,
    });
    port.postMessage({ id: "first", kind: "composer.insert", format: "markdown", text: "first" });
    await vi.waitFor(() => expect(insert).toHaveBeenCalledTimes(1));
    port.postMessage({ id: "second", kind: "composer.insert", format: "markdown", text: "second" });
    await vi.waitFor(() => expect(posted).toContainEqual(expect.objectContaining({ id: "second", ok: false, status: 403 })));
    resolveInsert(false);
    await vi.waitFor(() => expect(posted).toContainEqual(expect.objectContaining({ id: "first", ok: false, status: 409 })));
    expect(insert).toHaveBeenCalledTimes(1);
  });

  it("refuses composer insertion without a live host invocation or with invalid input", async () => {
    const ordinary = connectedBridge();
    ordinary.port.postMessage({ id: "panel", kind: "composer.insert", format: "markdown", text: "unsafe" });
    await vi.waitFor(() => expect(ordinary.posted).toContainEqual(expect.objectContaining({ id: "panel", ok: false, status: 403 })));

    const insert = vi.fn(() => false);
    const stale = connectedBridge({ installationId: "installation-2", bridgeToken: TOKEN, onComposerInsert: insert });
    stale.port.postMessage({ id: "stale", kind: "composer.insert", format: "markdown", text: "safe" });
    await vi.waitFor(() => expect(stale.posted).toContainEqual(expect.objectContaining({ id: "stale", ok: false, status: 409 })));
    expect(insert).toHaveBeenCalledExactlyOnceWith("safe");

    const oversized = vi.fn(() => true);
    const bounded = connectedBridge({ installationId: "installation-3", bridgeToken: TOKEN, onComposerInsert: oversized });
    bounded.port.postMessage({ id: "large", kind: "composer.insert", format: "markdown", text: "x".repeat(64 * 1024 + 1) });
    await vi.waitFor(() => expect(bounded.posted).toContainEqual(expect.objectContaining({ id: "large", ok: false, status: 413 })));
    expect(oversized).not.toHaveBeenCalled();

    const multibyte = connectedBridge({ installationId: "installation-4", bridgeToken: TOKEN, onComposerInsert: oversized });
    multibyte.port.postMessage({ id: "multibyte", kind: "composer.insert", format: "markdown", text: "界".repeat(22_000) });
    await vi.waitFor(() => expect(multibyte.posted).toContainEqual(expect.objectContaining({ id: "multibyte", ok: false, status: 413 })));
    expect(oversized).not.toHaveBeenCalled();
  });

  it("refuses the wrong frame, protocol, challenge, and a replay", async () => {
    const bridge = createSurfaceBridge({ installationId: "installation-1", bridgeToken: TOKEN });
    const frame = { contentWindow: {} } as unknown as HTMLIFrameElement;
    bridge.connect(frame, {});

    const wrongSource = new MessageChannel();
    const wrongVersion = new MessageChannel();
    const wrongChallenge = new MessageChannel();
    connectMessage({} as Window, wrongSource.port1);
    connectMessage(frame.contentWindow, wrongVersion.port1, TOKEN, 1);
    connectMessage(frame.contentWindow, wrongChallenge.port1, "wrong");

    const accepted = new MessageChannel();
    const replay = new MessageChannel();
    const acceptedMessages: unknown[] = [];
    const replayMessages: unknown[] = [];
    accepted.port2.onmessage = (event) => acceptedMessages.push(event.data);
    replay.port2.onmessage = (event) => replayMessages.push(event.data);
    accepted.port2.start();
    replay.port2.start();
    connectMessage(frame.contentWindow, accepted.port1);
    connectMessage(frame.contentWindow, replay.port1);
    accepted.port2.postMessage({ id: "real", kind: "action", method: "GET", path: "/context" });
    replay.port2.postMessage({ id: "replay", kind: "action", method: "GET", path: "/context" });

    await vi.waitFor(() => expect(mockCall).toHaveBeenCalledTimes(1));
    expect(mockCall).toHaveBeenCalledWith("installation-1", expect.objectContaining({ path: "/context" }));
    expect(replayMessages).toHaveLength(0);
    expect(acceptedMessages).toContainEqual(expect.objectContaining({ kind: "theme" }));
    bridge.close();
  });

  it("stops answering once closed", async () => {
    const { bridge, port } = connectedBridge();
    port.postMessage({ id: "r1", kind: "action", method: "GET", path: "/context" });
    await vi.waitFor(() => expect(mockCall).toHaveBeenCalledTimes(1));
    bridge.close();
    port.postMessage({ id: "r2", kind: "action", method: "GET", path: "/context" });
    await portDrain();
    expect(mockCall).toHaveBeenCalledTimes(1);
  });
});
