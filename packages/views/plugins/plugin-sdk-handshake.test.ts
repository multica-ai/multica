// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";

// Lives here rather than beside the SDK on purpose: @multica/plugin-sdk ships
// as TypeScript source to third parties and carries no test runner of its own,
// and packages/views is its only consumer in this repo.
//
// The guest half of the handshake. Sibling surfaces are mutually opaque but
// `parent.frames[i]` is an allowed cross-origin access, so another plugin on the
// same page can postMessage into this one. What stops it from becoming this
// surface's "host" is the two checks below — neither is visible from reading a
// plugin's own code, which is exactly why they are pinned here.
//
// Runs on a hand-built window rather than jsdom: the SDK touches exactly
// `addEventListener` and `parent`, and a package that ships to third parties is
// better off with no test-only dependency at all.

class FakeWindow extends EventTarget {
  parent: FakeWindow = this;
  readonly sent: unknown[] = [];
  postMessage(message: unknown) {
    this.sent.push(message);
  }
}

let fakeWindow: FakeWindow;

async function loadSdk() {
  vi.resetModules();
  fakeWindow = new FakeWindow();
  vi.stubGlobal("window", fakeWindow);
  // The SDK writes theme tokens to documentElement when a host sends them; no
  // theme is sent here, so a document is never touched.
  vi.stubGlobal("document", undefined);
  return import("@multica/plugin-sdk");
}

function initFrom(source: unknown, port: MessagePort) {
  const event = new MessageEvent("message", {
    data: { type: "multica:plugin-bridge-init", version: 1 },
  });
  Object.defineProperty(event, "source", { value: source, configurable: true });
  Object.defineProperty(event, "ports", { value: [port], configurable: true });
  fakeWindow.dispatchEvent(event);
}

/**
 * Waits out a turn for a delivery that must never come.
 *
 * Only sound for an assertion that stays true forever: a port delivers on an
 * event-loop drain rather than as a DOM task, so a timer is no barrier for one
 * that IS coming. Wait on the traffic itself in that case — see the third test.
 */
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("surface bridge handshake", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });

  it("accepts a port from the embedder and answers on it", async () => {
    const { multica } = await loadSdk();
    const channel = new MessageChannel();
    const asked: unknown[] = [];
    channel.port2.onmessage = (event) => {
      asked.push(event.data);
      const request = event.data as { id: string };
      channel.port2.postMessage({ id: request.id, ok: true, status: 200, data: { workspace: {}, user: {}, config: {} } });
    };
    channel.port2.start();

    initFrom(fakeWindow.parent, channel.port1);
    await multica.context.get(true);

    expect(asked).toHaveLength(1);
    expect(asked[0]).toMatchObject({ kind: "action", method: "GET", path: "/context" });
  });

  it("ignores an init from a window that is not the embedder", async () => {
    const { multica } = await loadSdk();
    const hostile = new MessageChannel();
    const seen: unknown[] = [];
    hostile.port2.onmessage = (event) => seen.push(event.data);
    hostile.port2.start();

    // A sibling plugin frame shouting an init with its own port.
    initFrom(new FakeWindow(), hostile.port1);
    void multica.context.get(true);
    await settle();

    expect(seen).toHaveLength(0);
  });

  it("keeps the first port when a second init arrives", async () => {
    const { multica } = await loadSdk();
    const real = new MessageChannel();
    const hijack = new MessageChannel();
    const onReal: unknown[] = [];
    const onHijack: unknown[] = [];
    real.port2.onmessage = (event) => onReal.push(event.data);
    hijack.port2.onmessage = (event) => onHijack.push(event.data);
    real.port2.start();
    hijack.port2.start();

    initFrom(fakeWindow.parent, real.port1);
    // Even from the embedder's own window: a hijacker that lost the race must
    // not be able to take the channel over afterwards.
    initFrom(fakeWindow.parent, hijack.port1);
    void multica.context.get(true);
    // The request arriving on the first port is the barrier: until it does,
    // "the hijacker got nothing" is true of a channel nobody has used yet.
    await vi.waitFor(() => expect(onReal.length).toBeGreaterThan(0));

    // The property under test is that the second init cannot capture the
    // channel — traffic keeps going to the port bound first, and the hijacker's
    // port stays silent.
    expect(onHijack).toHaveLength(0);
  });
});
