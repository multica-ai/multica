import { describe, expect, it, vi } from "vitest";

vi.mock("expo", () => ({ requireOptionalNativeModule: () => null }));
vi.mock("expo-secure-store", () => ({ getItemAsync: vi.fn(async () => null), setItemAsync: vi.fn(async () => {}), deleteItemAsync: vi.fn(async () => {}) }));

const { createDraftUploads, sendFile } = await import("./uploads");
const { persistedAttachments } = await import("./drafts");

const flush = async () => { for (let i = 0; i < 20; i++) await new Promise((resolve) => setImmediate(resolve)); };
const asset = { uri: "file:///a.txt", name: "a.txt", mimeType: "text/plain" };

function setup(overrides: { hash?: () => Promise<string | null>; transfer?: (...args: never[]) => Promise<string>; link?: (body: unknown) => Promise<unknown> } = {}) {
  const updates: unknown[][] = [];
  const request = vi.fn(async (method: string, path: string, _schema?: unknown, body?: unknown) => {
    if (path === "/attachments/link") return overrides.link ? overrides.link(body) : Promise.reject(new Error("not_found"));
    return { method, path };
  });
  const client = { url: (path: string) => path, headers: () => ({}), request } as never;
  const transfer = overrides.transfer ?? vi.fn(async (_client: unknown, _asset: unknown, id: string) => id);
  const uploads = createDraftUploads({ hash: overrides.hash ?? (async () => null), transfer: transfer as never, update: (key, id, patch) => updates.push([key, id, patch]) });
  return { client, request, transfer, updates, uploads };
}

describe("draft uploads", () => {
  it("adds the attachment at once and starts moving it", async () => {
    const { client, transfer, updates, uploads } = setup();
    const id = uploads.start(client, "draft", asset);
    expect(id).toMatch(/^[0-9a-f]{32}$/);
    expect(updates[0]).toEqual(["draft", id, { id, name: "a.txt", status: "uploading" }]);
    await flush();
    expect(transfer).toHaveBeenCalledWith(client, asset, id, expect.any(AbortSignal));
    expect(updates.at(-1)).toEqual(["draft", id, { status: "uploaded" }]);
  });

  it("does not transfer content the device already has", async () => {
    const link = vi.fn(async () => ({ id: "x", name: "a.txt", mime_type: "text/plain", size: 1 }));
    const { client, transfer, uploads } = setup({ hash: async () => "abc", link });
    const id = uploads.start(client, "draft", asset);
    await flush();
    expect(link).toHaveBeenCalledWith({ id, sha256: "abc", name: "a.txt", mime_type: "text/plain" });
    expect(transfer).not.toHaveBeenCalled();
  });

  it("transfers when the content is not there yet", async () => {
    const { client, transfer, uploads } = setup({ hash: async () => "abc" });
    uploads.start(client, "draft", asset);
    await flush();
    expect(transfer).toHaveBeenCalledOnce();
  });

  it("moves several files at once, up to a limit", async () => {
    const releases: (() => void)[] = [];
    const transfer = vi.fn(() => new Promise<string>((resolve) => { releases.push(() => resolve("x")); }));
    const { client, uploads } = setup({ transfer });
    for (let i = 0; i < 5; i++) uploads.start(client, "draft", { ...asset, name: `${i}` });
    await flush();
    expect(transfer).toHaveBeenCalledTimes(3);
    releases[0]!();
    await flush();
    expect(transfer).toHaveBeenCalledTimes(4);
  });

  it("lets a send wait for a transfer", async () => {
    let finish!: (id: string) => void;
    const { client, uploads } = setup({ transfer: vi.fn(() => new Promise<string>((resolve) => { finish = resolve; })) });
    const id = uploads.start(client, "draft", asset);
    let done = false;
    const waiting = uploads.ensure([id, "already-stored"]).then(() => { done = true; });
    await flush();
    expect(done).toBe(false);
    finish(id);
    await waiting;
    expect(done).toBe(true);
  });

  it("uses the id an older daemon gave the file", async () => {
    const { client, updates, uploads } = setup({ transfer: vi.fn(async () => "named-by-daemon") });
    const id = uploads.start(client, "draft", asset);
    await expect(uploads.ensure([id, "already-stored"])).resolves.toEqual(["named-by-daemon", "already-stored"]);
    expect(updates.at(-1)).toEqual(["draft", id, { status: "uploaded", id: "named-by-daemon" }]);
  });

  it("fails a send on a failed transfer and retries on its next attempt", async () => {
    const transfer = vi.fn().mockRejectedValueOnce(new Error("upload_failed")).mockResolvedValueOnce("stored");
    const { client, updates, uploads } = setup({ transfer });
    const id = uploads.start(client, "draft", asset);
    await expect(uploads.ensure([id])).rejects.toThrow("upload_failed");
    expect(updates.at(-1)).toEqual(["draft", id, { status: "failed" }]);
    await expect(uploads.ensure([id])).resolves.toEqual(["stored"]);
    expect(transfer).toHaveBeenCalledTimes(2);
    expect(updates.at(-1)).toEqual(["draft", id, { status: "uploaded", id: "stored" }]);
  });

  it("removing stops the transfer, then discards what arrived, and reports nothing further", async () => {
    const order: string[] = [];
    const transfer = vi.fn((_client, _asset, _id, signal: AbortSignal) => new Promise<string>((_, reject) => {
      signal.addEventListener("abort", () => { order.push("aborted"); reject(new Error("aborted")); });
    }));
    const { client, request, updates, uploads } = setup({ transfer });
    const id = uploads.start(client, "draft", asset);
    await flush();
    const before = updates.length;
    request.mockImplementation(async (method: string) => { order.push(method); return {} as never; });
    uploads.remove(client, id);
    await flush();
    expect(order).toEqual(["aborted", "DELETE"]);
    expect(request).toHaveBeenCalledWith("DELETE", `/attachments/${id}`, expect.anything());
    expect(updates.length).toBe(before);
  });

  it("discards an attachment restored from a saved draft", async () => {
    const { client, request, uploads } = setup();
    uploads.remove(client, "from-last-launch");
    await flush();
    expect(request).toHaveBeenCalledWith("DELETE", "/attachments/from-last-launch", expect.anything());
  });
});

describe("saved drafts", () => {
  it("keep only attachments the device has stored", () => {
    expect(persistedAttachments({
      a: [{ id: "1", name: "kept", status: "uploaded" }, { id: "2", name: "moving", status: "uploading" }, { id: "3", name: "failed", status: "failed" }],
      b: [{ id: "4", name: "moving", status: "uploading" }],
    })).toEqual({ a: [{ id: "1", name: "kept", status: "uploaded" }] });
  });
});

describe("sendFile", () => {
  class FakeXHR {
    static last: FakeXHR;
    upload: { onprogress?: () => void } = {};
    status = 0; responseText = ""; headers: Record<string, string> = {}; aborted = false;
    onload?: () => void; onerror?: () => void;
    constructor() { FakeXHR.last = this; }
    open() {}
    setRequestHeader(name: string, value: string) { this.headers[name] = value; }
    send() {}
    abort() { this.aborted = true; }
  }
  const client = { url: (path: string) => path, headers: () => ({ Authorization: "Bearer t" }), request: vi.fn() } as never;
  const run = (signal = new AbortController().signal) => {
    const sending = sendFile(client, asset, {} as Blob, "abc", signal, FakeXHR as never);
    void sending.catch(() => undefined);
    return sending;
  };

  it("names the attachment and tolerates a slow file that keeps moving", async () => {
    vi.useFakeTimers();
    try {
      const sending = run();
      expect(FakeXHR.last.headers).toMatchObject({ "X-Attachment-ID": "abc", "X-File-Name": "a.txt", Authorization: "Bearer t" });
      for (let minute = 0; minute < 5; minute++) { vi.advanceTimersByTime(59_000); FakeXHR.last.upload.onprogress!(); }
      FakeXHR.last.status = 201;
      FakeXHR.last.responseText = JSON.stringify({ id: "abc", name: "a.txt", mime_type: "text/plain", size: 1 });
      FakeXHR.last.onload!();
      await expect(sending).resolves.toBe("abc");
    } finally { vi.useRealTimers(); }
  });

  it("gives up when the file stops moving", async () => {
    vi.useFakeTimers();
    try {
      const sending = run();
      vi.advanceTimersByTime(60_000);
      await expect(sending).rejects.toThrow("upload_stalled");
      expect(FakeXHR.last.aborted).toBe(true);
    } finally { vi.useRealTimers(); }
  });

  it("stops when cancelled", async () => {
    const controller = new AbortController();
    const sending = run(controller.signal);
    controller.abort();
    await expect(sending).rejects.toThrow("aborted");
    expect(FakeXHR.last.aborted).toBe(true);
  });
});
