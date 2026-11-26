import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as Y from "yjs";

const { apiMock, providers, sockets } = vi.hoisted(() => ({
  apiMock: { createDocCollabTicket: vi.fn(), createIssueCollabTicket: vi.fn() },
  providers: [] as Array<{
    config: {
      name: string;
      token: () => Promise<string>;
      onSynced: (e: { state: boolean }) => void;
      onClose: (e: { event: { reason: string } }) => void;
    };
    destroyed: boolean;
    tokensSent: number;
    syncs: number;
    awareness: { local: unknown; states: Map<number, unknown>; emit: () => void };
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
      states: new Map<number, unknown>(),
      listeners: new Set<() => void>(),
      on(_event: string, listener: () => void) {
        this.listeners.add(listener);
      },
      off(_event: string, listener: () => void) {
        this.listeners.delete(listener);
      },
      emit() {
        for (const listener of this.listeners) listener();
      },
      getStates() {
        return this.states;
      },
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
    tokensSent = 0;
    syncs = 0;
    attach() {}
    async sendToken() {
      this.tokensSent++;
    }
    startSync() {
      this.syncs++;
    }
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
  liveOffKey,
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
  apiMock.createIssueCollabTicket.mockReset();
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

  it("asks for an issue description's ticket apart from a doc with the same id", async () => {
    const { url, docId: id } = ids();
    apiMock.createDocCollabTicket.mockResolvedValue(ticket(id));
    apiMock.createIssueCollabTicket.mockResolvedValue({ ...ticket(id), document: `issue:${id}` });
    await prefetchDocLive(url, id, "u1", "issue");
    expect(apiMock.createIssueCollabTicket).toHaveBeenCalledWith(id, { state: true });
    expect(apiMock.createDocCollabTicket).not.toHaveBeenCalled();
    expect(peekDocLive(url, id, "u1")).toBeUndefined();
    expect(peekDocLive(url, id, "u1", "issue")).toMatchObject({ document: `issue:${id}` });

    const session = acquireDocSession(url, id, "u1", `issue:${id}`, "issue");
    expect(findDocSession(url, id, "u1")).toBeUndefined();
    expect(findDocSession(url, id, "u1", "issue")).toBe(session);
    // The seeded ticket is used once; the next join asks the issue endpoint.
    await providers.at(-1)!.config.token();
    await providers.at(-1)!.config.token();
    expect(apiMock.createIssueCollabTicket).toHaveBeenLastCalledWith(id, undefined);
    releaseDocSession(session, { discard: true });
  });

  it("remembers live issue descriptions being off without turning docs solo", async () => {
    const { url, docId: id } = ids();
    apiMock.createIssueCollabTicket.mockResolvedValue({ enabled: false });
    await prefetchDocLive(url, id, "u1", "issue");
    expect(liveEditingOff.has(liveOffKey(url, "issue"))).toBe(true);
    expect(liveEditingOff.has(url)).toBe(false);
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

  it("tells people editing from people who only have the doc open", () => {
    const { url, docId } = ids();
    const session = acquireDocSession(url, docId, "u1", `doc:${docId}`);
    const awareness = providers.at(-1)!.awareness;
    const caret = { anchor: {}, head: {} };
    const moves = vi.fn();
    const stop = session.watchAwareness(moves);
    awareness.states = new Map<number, unknown>([
      [101, { user: { id: "a", name: "Ann", kind: "member" } }],
      // Bo in two tabs, typing in the second.
      [102, { user: { id: "b", name: "Bo", kind: "member" } }],
      [103, { user: { id: "b", name: "Bo", kind: "member" }, cursor: caret }],
    ]);
    awareness.emit();
    const people = () => session.getState().collaborators.map((c) => [c.name, c.editing, c.key]);
    expect(people()).toEqual([["Ann", false, 101], ["Bo", true, 103]]);
    expect(moves).toHaveBeenCalledTimes(1);

    awareness.states.set(103, { user: { id: "b", name: "Bo", kind: "member" }, cursor: null });
    awareness.emit();
    expect(people()).toEqual([["Ann", false, 101], ["Bo", false, 102]]);

    stop();
    awareness.emit();
    expect(moves).toHaveBeenCalledTimes(2);
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

  it("joins again with a fresh ticket when the server asks it to reauthenticate", async () => {
    vi.useFakeTimers();
    const { url, docId } = ids();
    const session = acquireDocSession(url, docId, "u1", `doc:${docId}`);
    const provider = providers.at(-1)!;
    provider.config.onSynced({ state: true });

    apiMock.createDocCollabTicket.mockRejectedValueOnce(new Error("offline"));
    provider.config.onClose({ event: { reason: "reauthenticate" } });
    // Unsynced until it is back in, so a page waiting too long goes solo.
    expect(session.getState().synced).toBe(false);
    await vi.advanceTimersByTimeAsync(1_000);
    expect(provider.tokensSent).toBe(0);

    // The next try, later, gets a ticket and joins with it, keeping the doc.
    apiMock.createDocCollabTicket.mockResolvedValueOnce(ticket(docId, { token: "fresh" }));
    await vi.advanceTimersByTimeAsync(2_000);
    expect([provider.tokensSent, provider.syncs]).toEqual([1, 1]);
    expect(JSON.parse(await provider.config.token()).ticket).toBe("fresh");
    expect(providers).toHaveLength(1);
    provider.config.onSynced({ state: true });
    expect(session.getState().synced).toBe(true);
    releaseDocSession(session, { discard: true });
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
