import { beforeEach, describe, expect, it, vi } from "vitest";

const mockCall = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({ api: { callPluginAction: mockCall } }));

import { createSurfaceBridge } from "./surface-bridge";

// The bridge is the only path from a surface into Multica, so what it refuses
// matters more than what it forwards. None of this is visible from the
// component: it only holds if the guards run before the fetch.

function connectedBridge(options: Parameters<typeof createSurfaceBridge>[0] = { installationId: "installation-1" }) {
  const bridge = createSurfaceBridge(options);
  const posted: unknown[] = [];
  const frame = {
    contentWindow: {
      postMessage: (_message: unknown, _origin: string, transfer: MessagePort[]) => {
        const port = transfer[0];
        if (!port) throw new Error("bridge did not transfer a port");
        port.onmessage = (event: MessageEvent) => posted.push(event.data);
        port.start();
        (frame as unknown as { port: MessagePort }).port = port;
      },
    },
  } as unknown as HTMLIFrameElement & { port: MessagePort };

  bridge.connect(frame, {});
  // The guest announces itself; the host answers that, not the iframe's load
  // event. `source` has to be the frame's own window or the host must ignore it.
  announce(frame.contentWindow as unknown as Window);
  return { bridge, frame, posted };
}

/**
 * Fires the surface's readiness signal as if it came from `source`.
 *
 * `MessageEvent.source` is getter-only, so it is redefined on the instance. The
 * host reads it by identity, which is exactly the check being exercised.
 */
function announce(source: Window | null) {
  const event = new MessageEvent("message", { data: { type: "multica:plugin-surface-ready" } });
  Object.defineProperty(event, "source", { value: source, configurable: true });
  window.dispatchEvent(event);
}

/**
 * Resolves once the bridge has answered everything posted before this call.
 *
 * Nothing here may wait by timer. jsdom has no MessageChannel, so `frame.port`
 * is Node's, and a port delivers on an event-loop drain rather than as a DOM
 * task — `setTimeout(0)` can fire first. That is how this suite failed in CI:
 * the assertions ran before the bridge saw anything, and the handler landed in
 * the *next* test, which then failed on a call it never made.
 *
 * A port is FIFO, so a request posted last and answered proves every earlier
 * one was handled. This probe names a path outside the Action API, which the
 * bridge refuses without awaiting anything. Its answer is not the test's, so it
 * is dropped from `posted` before returning.
 */
async function answered(frame: { port: MessagePort }, posted: unknown[]) {
  const id = "probe:has-the-bridge-caught-up";
  frame.port.postMessage({ id, kind: "action", method: "GET", path: "/probe" });
  const sent = (message: unknown) => (message as { id?: string } | null)?.id === id;
  await vi.waitFor(() => {
    if (!posted.some(sent)) throw new Error("bridge has not answered the probe yet");
  });
  posted.splice(posted.findIndex(sent), 1);
}

/**
 * Waits out one event-loop port drain — the cycle any pending delivery rides.
 *
 * Only for the closed-bridge case, where the assertion is that a message is
 * never delivered and there is therefore no answer to wait for. A drain of our
 * own is the closest available clock: a delivery the closed port wrongly made
 * would have come through on the same one.
 */
const portDrain = () =>
  new Promise<void>((resolve) => {
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

  it("forwards an allowed path with the installation that owns the channel", async () => {
    const { frame } = connectedBridge({ installationId: "installation-1", issueId: "issue-1" });
    frame.port.postMessage({ id: "r1", kind: "action", method: "GET", path: "/context" });

    await vi.waitFor(() => {
      expect(mockCall).toHaveBeenCalledWith("installation-1", expect.objectContaining({
        method: "GET",
        path: "/context",
        issueId: "issue-1",
      }));
    });
  });

  it("refuses a path outside the Action API before it reaches the network", async () => {
    // A surface asking for /me or ../admin must never turn into a real request:
    // the Action API's scope checks only exist on the paths it defines.
    const { frame, posted } = connectedBridge();
    for (const path of ["/me", "/workspaces/w1/plugins", "/../admin", "/issues/i1/comments/extra"]) {
      frame.port.postMessage({ id: `bad-${path}`, kind: "action", method: "GET", path });
    }
    await vi.waitFor(() => expect(posted).toHaveLength(4));

    expect(mockCall).not.toHaveBeenCalled();
    for (const response of posted as Array<{ ok: boolean; status: number }>) {
      expect(response.ok).toBe(false);
      expect(response.status).toBe(400);
    }
  });

  it("passes the refusal status through so a surface can tell 403 from 404", async () => {
    // 403 means "the admin did not grant this scope" — a different fix for a
    // plugin author than a missing resource, so it must not be flattened.
    mockCall.mockRejectedValue(Object.assign(new Error("not granted comments:write"), { status: 403 }));
    const { frame, posted } = connectedBridge();
    frame.port.postMessage({ id: "r1", kind: "action", method: "POST", path: "/issues/i1/comments", body: {} });
    await vi.waitFor(() => expect(posted).toHaveLength(1));

    expect(posted[0]).toMatchObject({ id: "r1", ok: false, status: 403 });
  });

  it("refuses a method outside the allowlist before it reaches fetch", async () => {
    const { frame, posted } = connectedBridge();
    frame.port.postMessage({ id: "r1", kind: "action", method: "TRACE", path: "/context" });
    frame.port.postMessage({ id: "r2", kind: "action", method: "get", path: "/context" });
    await answered(frame, posted);

    expect(mockCall).not.toHaveBeenCalled();
    expect(posted).toHaveLength(0);
  });

  it("ignores messages that are not bridge requests", async () => {
    const { frame, posted } = connectedBridge();
    frame.port.postMessage({ hello: "world" });
    frame.port.postMessage({ id: 7, kind: "action", method: "GET", path: "/context" });
    await answered(frame, posted);

    expect(mockCall).not.toHaveBeenCalled();
    expect(posted).toHaveLength(0);
  });

  it("clamps a resize so a surface cannot push the page out of view", async () => {
    const heights: number[] = [];
    const { frame, posted } = connectedBridge({ installationId: "installation-1", onResize: (h) => heights.push(h) });
    frame.port.postMessage({ id: "r1", kind: "ui.resize", height: 10_000_000 });
    frame.port.postMessage({ id: "r2", kind: "ui.resize", height: -5 });
    frame.port.postMessage({ id: "r3", kind: "ui.resize", height: 320 });
    await answered(frame, posted);

    expect(heights).toEqual([4000, 0, 320]);
  });

  it("ignores a readiness signal from a window it does not own", () => {
    // Every sandboxed frame reports the opaque origin "null", so the window
    // reference is the only thing that distinguishes this surface from any
    // other frame on the page — including a hostile one shouting the signal.
    // Nothing to wait for: the host answers the signal on the dispatch itself.
    const bridge = createSurfaceBridge({ installationId: "installation-1" });
    let transferred = false;
    const frame = {
      contentWindow: { postMessage: () => { transferred = true; } },
    } as unknown as HTMLIFrameElement;
    bridge.connect(frame, {});
    announce({} as Window);

    expect(transferred).toBe(false);
    bridge.close();
  });

  it("stops answering once closed", async () => {
    // Answer one request first. Without that, a bridge that answered nothing at
    // all — closed or not — would pass this test.
    const { bridge, frame } = connectedBridge();
    frame.port.postMessage({ id: "r1", kind: "action", method: "GET", path: "/context" });
    await vi.waitFor(() => expect(mockCall).toHaveBeenCalledTimes(1));

    bridge.close();
    frame.port.postMessage({ id: "r2", kind: "action", method: "GET", path: "/context" });
    await portDrain();

    expect(mockCall).toHaveBeenCalledTimes(1);
  });
});
