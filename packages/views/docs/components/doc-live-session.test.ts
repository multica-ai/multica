import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as Y from "yjs";

const { apiMock, providers, sockets } = vi.hoisted(() => ({
  apiMock: { createDocCollabTicket: vi.fn() },
  providers: [] as Array<{
    config: {
      name: string;
      token: () => Promise<string>;
      onSynced: (e: { state: boolean }) => void;
      onClose: (e: { event: { reason: string } }) => void;
    };
    destroyed: boolean;
    awareness: { local: unknown };
  }>,
  sockets: [] as Array<{ destroyed: boolean }>,
}));

vi.mock("@multica/core/api", () => ({ api: apiMock }));
vi.mock("@hocuspocus/provider", () => {
  class HocuspocusProviderWebsocket {
    status = "disconnected";
    destroyed = false;
    constructor() {
      sockets.push(this);
    }
    destroy() {
      this.destroyed = true;
    }
  }
  class HocuspocusProvider {
    destroyed = false;
    awareness = {
      local: {} as unknown,
      on() {},
      off() {},
      getStates: () => new Map(),
      getLocalState() {
        return this.local;
      },
      setLocalState(state: unknown) {
        this.local = state;
      },
    };
    constructor(public config: (typeof providers)[number]["config"]) {
      providers.push(this);
    }
    attach() {}
    destroy() {
      this.destroyed = true;
    }
  }
  return {
    HocuspocusProvider,
    HocuspocusProviderWebsocket,
    WebSocketStatus: { Connected: "connected", Connecting: "connecting", Disconnected: "disconnected" },
  };
});

import {
  acquireDocSession,
  findDocSession,
  liveEditingOff,
  peekDocLive,
  prefetchDocLive,
  releaseDocSession,
} from "./doc-live-session";

let n = 0;
const ids = () => {
  n++;
  return { url: `ws://collab-${n}.test/collab`, docId: `doc-${n}` };
};

function savedState(text: string): string {
  const doc = new Y.Doc();
  doc.getText("t").insert(0, text);
  return btoa(String.fromCharCode(...Y.encodeStateAsUpdate(doc)));
}

const ticket = (docId: string, extra: Record<string, unknown> = {}) => ({
  enabled: true,
  token: `token-${docId}`,
  document: `doc:${docId}`,
  expiresAt: new Date(Date.now() + 5 * 60_000).toISOString(),
  ...extra,
});

beforeEach(() => {
  apiMock.createDocCollabTicket.mockReset();
  providers.length = 0;
  sockets.length = 0;
});

afterEach(() => {
  vi.useRealTimers();
});

describe("prefetchDocLive", () => {
  it("asks once for a doc while the answer is fresh, with its saved state", async () => {
    const { url, docId } = ids();
    apiMock.createDocCollabTicket.mockResolvedValue(ticket(docId));
    await Promise.all([prefetchDocLive(url, docId, "u1"), prefetchDocLive(url, docId, "u1")]);
    expect(apiMock.createDocCollabTicket).toHaveBeenCalledTimes(1);
    expect(apiMock.createDocCollabTicket).toHaveBeenCalledWith(docId, { state: true });
    expect(peekDocLive(url, docId, "u1")).toMatchObject({ enabled: true, document: `doc:${docId}` });
  });

  it("remembers a server without live editing and opens no socket to it", async () => {
    const { url, docId } = ids();
    apiMock.createDocCollabTicket.mockResolvedValue({ enabled: false });
    await prefetchDocLive(url, docId, "u1");
    expect(liveEditingOff.has(url)).toBe(true);
    expect(sockets).toHaveLength(0);
  });

  it("opens the socket early only for a server known to edit live", async () => {
    vi.useFakeTimers();
    const { url, docId } = ids();
    apiMock.createDocCollabTicket.mockResolvedValue(ticket(docId));
    const first = prefetchDocLive(url, docId, "u1");
    expect(sockets).toHaveLength(0);
    await first;
    expect(sockets).toHaveLength(1);
    vi.advanceTimersByTime(60_000);
    expect(sockets[0]!.destroyed).toBe(true);

    void prefetchDocLive(url, `${docId}-next`, "u1");
    expect(sockets).toHaveLength(2);
  });

  it("keeps answers per user: a ticket is personal", async () => {
    const { url, docId } = ids();
    apiMock.createDocCollabTicket.mockResolvedValue(ticket(docId));
    await prefetchDocLive(url, docId, "u1");
    expect(peekDocLive(url, docId, "u2")).toBeUndefined();
    await prefetchDocLive(url, docId, "u2");
    expect(apiMock.createDocCollabTicket).toHaveBeenCalledTimes(2);
  });

  it("forgets an answer nobody used once it is stale, and asks again", async () => {
    vi.useFakeTimers();
    const { url, docId } = ids();
    apiMock.createDocCollabTicket.mockResolvedValue(ticket(docId));
    await prefetchDocLive(url, docId, "u1");
    vi.advanceTimersByTime(30_000);
    expect(peekDocLive(url, docId, "u1")).toBeUndefined();
    await prefetchDocLive(url, docId, "u1");
    expect(apiMock.createDocCollabTicket).toHaveBeenCalledTimes(2);
  });

  it("forgets a failed request so the next one retries", async () => {
    const { url, docId } = ids();
    apiMock.createDocCollabTicket.mockRejectedValueOnce(new Error("offline"));
    await expect(prefetchDocLive(url, docId, "u1")).rejects.toThrow("offline");
    apiMock.createDocCollabTicket.mockResolvedValue(ticket(docId));
    await prefetchDocLive(url, docId, "u1");
    expect(apiMock.createDocCollabTicket).toHaveBeenCalledTimes(2);
  });
});

describe("doc sessions", () => {
  it("shows the saved state before syncing and joins with the prefetched ticket once", async () => {
    const { url, docId } = ids();
    apiMock.createDocCollabTicket.mockResolvedValueOnce(ticket(docId, { state: savedState("hello") }));
    await prefetchDocLive(url, docId, "u1");

    const session = acquireDocSession(url, docId, "u1", `doc:${docId}`);
    expect(session.getState()).toMatchObject({ ready: true, synced: false });
    expect(session.ydoc.getText("t").toString()).toBe("hello");
    expect(session.edited).toBe(false);

    const provider = providers.at(-1)!;
    expect(JSON.parse(await provider.config.token())).toEqual({ ticket: `token-${docId}` });
    expect(apiMock.createDocCollabTicket).toHaveBeenCalledTimes(1);

    // A rejoin asks for a fresh ticket.
    apiMock.createDocCollabTicket.mockResolvedValueOnce(ticket(docId, { token: "fresh" }));
    expect(JSON.parse(await provider.config.token()).ticket).toBe("fresh");

    provider.config.onSynced({ state: true });
    expect(session.getState()).toMatchObject({ ready: true, synced: true });
    releaseDocSession(session, { discard: true });
  });

  it("waits for the first sync when there is no saved state", async () => {
    const { url, docId } = ids();
    apiMock.createDocCollabTicket.mockResolvedValueOnce(ticket(docId));
    await prefetchDocLive(url, docId, "u1");
    const session = acquireDocSession(url, docId, "u1", `doc:${docId}`);
    expect(session.getState().ready).toBe(false);
    session.ydoc.getText("t").insert(0, "typed");
    expect(session.edited).toBe(true);
    releaseDocSession(session, { discard: true });
  });

  it("keeps a released session for the next visit, then ends it", () => {
    vi.useFakeTimers();
    const { url, docId } = ids();
    const session = acquireDocSession(url, docId, "u1", `doc:${docId}`);
    releaseDocSession(session);
    expect(acquireDocSession(url, docId, "u1", `doc:${docId}`)).toBe(session);
    expect(providers).toHaveLength(1);

    releaseDocSession(session);
    vi.advanceTimersByTime(60_000);
    expect(findDocSession(url, docId, "u1")).toBeUndefined();
    expect(providers[0]!.destroyed).toBe(true);
    // The socket closes once nothing uses it.
    vi.advanceTimersByTime(60_000);
    expect(sockets.at(-1)!.destroyed).toBe(true);
  });

  it("leaves the doc's presence while kept, and rejoins it on the next visit", () => {
    const { url, docId } = ids();
    const session = acquireDocSession(url, docId, "u1", `doc:${docId}`);
    const awareness = providers.at(-1)!.awareness;
    awareness.local = { user: { id: "u1" } };
    releaseDocSession(session);
    expect(awareness.local).toBeNull();
    acquireDocSession(url, docId, "u1", `doc:${docId}`);
    expect(awareness.local).toEqual({});
    releaseDocSession(session, { discard: true });
  });

  it("shares one socket between docs on the same server", () => {
    const { url, docId } = ids();
    const a = acquireDocSession(url, docId, "u1", `doc:${docId}`);
    const b = acquireDocSession(url, `${docId}-b`, "u1", `doc:${docId}-b`);
    expect(sockets).toHaveLength(1);
    releaseDocSession(a, { discard: true });
    releaseDocSession(b, { discard: true });
  });

  it("ends a reset session once its page lets go, and starts the next one fresh", () => {
    const { url, docId } = ids();
    const session = acquireDocSession(url, docId, "u1", `doc:${docId}`);
    providers.at(-1)!.config.onClose({ event: { reason: "document-reset" } });
    expect(session.getState().reset).toBe(true);
    expect(findDocSession(url, docId, "u1")).toBeUndefined();
    expect(providers.at(-1)!.destroyed).toBe(false);

    const next = acquireDocSession(url, docId, "u1", `doc:${docId}`);
    expect(next).not.toBe(session);
    releaseDocSession(session);
    expect(providers[0]!.destroyed).toBe(true);
    releaseDocSession(next, { discard: true });
  });
});
