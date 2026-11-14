// @vitest-environment node
import { existsSync } from "fs";
import { mkdtemp, readdir, rm, writeFile } from "fs/promises";
import { tmpdir } from "os";
import { join } from "path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MAX_HISTORY_ENTRIES, VoiceHistoryStore } from "./history-store";

const DAY = 24 * 60 * 60 * 1000;
const audio = new Uint8Array([1, 2, 3]);

let root: string;
let now: number;
let onChange: ReturnType<typeof vi.fn>;

async function open() {
  const store = new VoiceHistoryStore({ root, onChange, now: () => now });
  await store.init();
  return store;
}

beforeEach(async () => {
  root = await mkdtemp(join(tmpdir(), "voice-history-"));
  now = 10 * DAY;
  onChange = vi.fn();
});

afterEach(async () => {
  await rm(root, { recursive: true, force: true });
});

describe("VoiceHistoryStore", () => {
  it("keeps a recording before it is transcribed, then its text", async () => {
    const store = await open();
    const entry = await store.save(audio, "audio/webm;codecs=opus", 1500, null);
    expect(entry).toMatchObject({ status: "transcribing", text: "", durationMs: 1500 });
    await store.update(entry.id, { status: "done", text: "你好" });

    const reopened = await open();
    expect(reopened.list()).toEqual([{ ...entry, status: "done", text: "你好" }]);
    expect(await reopened.audio(entry.id)).toEqual({ audio, mimeType: "audio/webm;codecs=opus" });
    expect(onChange).toHaveBeenCalled();
  });

  it("reads a transcription the app quit during as interrupted", async () => {
    const entry = await (await open()).save(audio, "audio/webm", 900, null);
    expect((await open()).list()[0]).toMatchObject({ id: entry.id, status: "interrupted" });
  });

  it("lists newest first and deletes the audio with the entry", async () => {
    const store = await open();
    const first = await store.save(audio, "audio/webm", 1, null);
    now += 1000;
    const second = await store.save(audio, "audio/ogg", 1, null);
    expect(store.list().map((e) => e.id)).toEqual([second.id, first.id]);
    await store.remove(second.id);
    expect(store.list().map((e) => e.id)).toEqual([first.id]);
    expect((await readdir(root)).filter((f) => f.endsWith(".ogg"))).toEqual([]);
  });

  it("prunes by age when saving and on demand, never a recording in flight", async () => {
    const store = await open();
    const old = await store.save(audio, "audio/webm", 1, null);
    await store.update(old.id, { status: "done", text: "old" });
    now += 8 * DAY;
    const fresh = await store.save(audio, "audio/webm", 1, 7);
    expect(store.list().map((e) => e.id)).toEqual([fresh.id]);

    await store.prune(0);
    expect(store.list().map((e) => e.id)).toEqual([fresh.id]);
    await store.update(fresh.id, { status: "failed" });
    await store.prune(0);
    expect(store.list()).toEqual([]);
    expect((await readdir(root)).filter((f) => f !== "history.json")).toEqual([]);
  });

  it("caps the number of entries even when keeping forever", async () => {
    const store = await open();
    for (let i = 0; i < MAX_HISTORY_ENTRIES; i++) {
      const entry = await store.save(audio, "audio/webm", 1, null);
      await store.update(entry.id, { status: "done", text: String(i) });
    }
    await store.save(audio, "audio/webm", 1, null);
    expect(store.list()).toHaveLength(MAX_HISTORY_ENTRIES);
    expect(store.list().at(-1)?.text).toBe("1");
  });

  it("ignores a corrupt index and entries that point outside the folder", async () => {
    await writeFile(join(root, "history.json"), JSON.stringify([
      { id: "x", createdAt: 1, durationMs: 1, status: "done", text: "", file: "../escape", mimeType: "audio/webm" },
      "junk",
    ]));
    const store = await open();
    expect(store.list()).toEqual([]);
    expect(await store.audio("x")).toBeNull();
    await writeFile(join(root, "history.json"), "{not json");
    expect((await open()).list()).toEqual([]);
    expect(existsSync(join(root, "history.json"))).toBe(true);
  });
});
