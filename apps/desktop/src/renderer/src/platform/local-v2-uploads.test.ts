// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { ATTACHMENT_RESERVE_CAPABILITY, contentHash, createLocalV2Uploads, type UploadOutcome } from "./local-v2-uploads";

const file = (name = "a.txt") => new File(["hello"], name, { type: "text/plain" });
const flush = async () => { for (let i = 0; i < 20; i++) await new Promise((resolve) => setImmediate(resolve)); };
const stored = (id = "x", name = "a.txt") => ({ id, name, mime_type: "text/plain", size: 5 });

function setup(overrides: Partial<Parameters<typeof createLocalV2Uploads>[0]> = {}, capabilities = [ATTACHMENT_RESERVE_CAPABILITY]) {
  const client = {
    upload: vi.fn(async (_file: File, options?: { id?: string }) => stored(options?.id)),
    linkUpload: vi.fn(async () => { throw new Error("not_found"); }) as ReturnType<typeof vi.fn>,
    discardUpload: vi.fn(async () => undefined),
    ...overrides,
  };
  const outcomes: UploadOutcome[] = [];
  // A fixed hash keeps these tests off the crypto thread pool, whose timing varies.
  const uploads = createLocalV2Uploads(client as never, async () => capabilities, (outcome) => outcomes.push(outcome), async () => "hash-of-file");
  return { client, outcomes, uploads };
}

describe("local v2 uploads", () => {
  it("identifies content by its SHA-256", async () => {
    await expect(contentHash(file())).resolves.toBe("2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824");
  });

  it("answers with the final id before the bytes arrive", async () => {
    let finish!: (value: ReturnType<typeof stored>) => void;
    const { client, outcomes, uploads } = setup({ upload: vi.fn(() => new Promise((resolve) => { finish = resolve; })) as never });
    const started = await uploads.begin(file());
    expect(started).toMatchObject({ name: "a.txt", uploading: true });
    expect(started.id).toMatch(/^[0-9a-f]{32}$/);
    await flush();
    expect(client.upload).toHaveBeenCalledWith(expect.any(File), expect.objectContaining({ id: started.id }));
    expect(outcomes).toEqual([]);
    finish(stored(started.id));
    await flush();
    expect(outcomes).toEqual([{ attachmentId: started.id, state: "uploaded" }]);
  });

  it("does not transfer content the daemon already stores", async () => {
    const { client, outcomes, uploads } = setup({ linkUpload: vi.fn(async () => stored()) });
    const started = await uploads.begin(file());
    await flush();
    expect(client.linkUpload).toHaveBeenCalledWith({ id: started.id, sha256: "hash-of-file", name: "a.txt", mime_type: "text/plain" });
    expect(client.upload).not.toHaveBeenCalled();
    expect(outcomes).toEqual([{ attachmentId: started.id, state: "uploaded" }]);
  });

  it("uploads in the foreground when the daemon cannot name attachments up front", async () => {
    const { client, uploads } = setup({}, []);
    await expect(uploads.begin(file())).resolves.toMatchObject({ id: "x", uploading: false });
    expect(client.upload).toHaveBeenCalledWith(expect.any(File));
    expect(client.linkUpload).not.toHaveBeenCalled();
  });

  it("runs several transfers at once, up to a limit", async () => {
    const releases: (() => void)[] = [];
    const upload = vi.fn((_file: File, options?: { id?: string }) => new Promise((resolve) => { releases.push(() => resolve(stored(options?.id))); }));
    const { uploads } = setup({ upload: upload as never });
    await Promise.all(Array.from({ length: 6 }, (_, index) => uploads.begin(file(`${index}.txt`))));
    await flush();
    expect(upload).toHaveBeenCalledTimes(4);
    releases[0]!();
    await flush();
    expect(upload).toHaveBeenCalledTimes(5);
  });

  it("reports a failure and transfers again on retry", async () => {
    const upload = vi.fn().mockRejectedValueOnce(new Error("upload_failed: offline")).mockResolvedValueOnce(stored());
    const { outcomes, uploads } = setup({ upload });
    const { id } = await uploads.begin(file());
    await flush();
    expect(outcomes).toEqual([{ attachmentId: id, state: "failed", message: "upload_failed: offline" }]);
    uploads.retry(id);
    await flush();
    expect(outcomes.at(-1)).toEqual({ attachmentId: id, state: "uploaded" });
    expect(upload).toHaveBeenCalledTimes(2);
  });

  it("cancelling stops the transfer, then discards what arrived, without an outcome", async () => {
    let signal!: AbortSignal;
    const order: string[] = [];
    const upload = vi.fn((_file: File, options: { signal: AbortSignal }) => new Promise((_, reject) => {
      signal = options.signal;
      signal.addEventListener("abort", () => { order.push("aborted"); reject(new DOMException("Aborted", "AbortError")); });
    }));
    const discardUpload = vi.fn(async () => { order.push("discarded"); });
    const { outcomes, uploads } = setup({ upload: upload as never, discardUpload });
    const { id } = await uploads.begin(file());
    await flush();
    await uploads.cancel(id);
    expect(order).toEqual(["aborted", "discarded"]);
    expect(discardUpload).toHaveBeenCalledWith(id);
    expect(outcomes).toEqual([]);
  });

  it("leaves an attachment a message owns alone", async () => {
    const { client, uploads } = setup();
    const { id } = await uploads.begin(file());
    await flush();
    uploads.forget(id);
    await uploads.cancel(id);
    expect(client.discardUpload).not.toHaveBeenCalled();
  });
});
