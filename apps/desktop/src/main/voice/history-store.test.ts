// @vitest-environment node
import { existsSync } from "fs";
import { mkdtemp, readdir, rm, writeFile } from "fs/promises";
import { tmpdir } from "os";
import { join } from "path";
import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import { MAX_HISTORY_ENTRIES, VoiceHistoryStore } from "./history-store";

const DAY = 24 * 60 * 60 * 1000;
const audio = new Uint8Array([1, 2, 3]);

let root: string;
let now: number;
let onChange: Mock<() => void>;

async function open() {
  const store = new VoiceHistoryStore({ root, onChange, now: () => now });
  await store.init();
  return store;
}

beforeEach(async () => {
  root = await mkdtemp(join(tmpdir(), "voice-history-"));
  now = 10 * DAY;
  onChange = vi.fn<() => void>();
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

  it("expires by the retention given when saving or chosen in Settings", async () => {
    const store = await open();
    const old = await store.save(audio, "audio/webm", 1, null);
    await store.update(old.id, { status: "done", text: "old" });
    now += 8 * DAY;
    const fresh = await store.save(audio, "audio/webm", 1, 7);
    expect(store.list().map((e) => e.id)).toEqual([fresh.id]);

    await store.setRetention(0);
    expect(store.list()).toEqual([]);
    expect((await readdir(root)).filter((f) => f.endsWith(".webm"))).toEqual([]);
  });

  it("keeps expiring by the chosen retention with no new recordings", async () => {
    const store = await open();
    await store.setRetention(1);
    const entry = await store.save(audio, "audio/webm", 1, 1);
    await store.update(entry.id, { status: "done", text: "yesterday" });

    now += DAY / 2;
    expect(await store.expire()).toBe(false);
    expect(store.list()).toHaveLength(1);

    now += 2 * DAY;
    const reopened = await open();
    expect(reopened.list()).toEqual([]);
    expect((await readdir(root)).filter((f) => f.endsWith(".webm"))).toEqual([]);
  });

  it("expires on read once a recording passes the retention", async () => {
    const store = await open();
    await store.save(audio, "audio/webm", 1, 1);
    now += 2 * DAY;
    expect(store.list()).toHaveLength(1);
    expect(await store.expire()).toBe(true);
    expect(store.list()).toEqual([]);
  });

  it("clears a recording still being transcribed, and its late result stays gone", async () => {
    const store = await open();
    await store.setRetention(30);
    const done = await store.save(audio, "audio/webm", 1, 30);
    await store.update(done.id, { status: "done", text: "done" });
    const inFlight = await store.save(audio, "audio/webm", 1, 30);

    await store.clear();
    expect(store.list()).toEqual([]);
    expect((await readdir(root)).filter((f) => f.endsWith(".webm"))).toEqual([]);

    await store.update(inFlight.id, { status: "done", text: "late" });
    expect(store.list()).toEqual([]);
    expect((await open()).list()).toEqual([]);

    const next = await store.save(audio, "audio/webm", 1, 30);
    now += 10 * DAY;
    expect((await open()).list().map((e) => e.id)).toEqual([next.id]);
  });

  it("caps the number of entries even when keeping forever", async () => {
    // Newest first, as the store writes it; only the oldest needs its audio on disk.
    const full = Array.from({ length: MAX_HISTORY_ENTRIES }, (_, i) => ({
      id: `e${i}`, createdAt: now - i, durationMs: 1, status: "done", text: String(i),
      file: `e${i}.webm`, mimeType: "audio/webm",
    }));
    await writeFile(join(root, "history.json"), JSON.stringify(full));
    await writeFile(join(root, `e${MAX_HISTORY_ENTRIES - 1}.webm`), audio);

    const store = await open();
    const newest = await store.save(audio, "audio/webm", 1, null);
    const list = store.list();
    expect(list).toHaveLength(MAX_HISTORY_ENTRIES);
    expect(list[0]?.id).toBe(newest.id);
    expect(list.at(-1)?.id).toBe(`e${MAX_HISTORY_ENTRIES - 2}`);
    expect(existsSync(join(root, `e${MAX_HISTORY_ENTRIES - 1}.webm`))).toBe(false);
    expect((await open()).list()).toHaveLength(MAX_HISTORY_ENTRIES);
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
