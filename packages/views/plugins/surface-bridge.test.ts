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
