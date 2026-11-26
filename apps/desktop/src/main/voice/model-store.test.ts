// @vitest-environment node
import { createHash } from "crypto";
import { existsSync } from "fs";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "fs/promises";
import { tmpdir } from "os";
import { join } from "path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CatalogModel } from "./catalog";
import { ModelStore } from "./model-store";

const encoder = new TextEncoder();
const FILES: Record<string, Uint8Array> = {
  "model.onnx": encoder.encode("weights ".repeat(4000)),
  "tokens.txt": encoder.encode("a b c"),
  "tokenizer/vocab.json": encoder.encode('{"a":1}'),
};

function sha256(data: Uint8Array) {
  return createHash("sha256").update(data).digest("hex");
}

const MODEL: CatalogModel = {
  id: "tiny",
  name: "Tiny",
  author: "Test",
  licenseUrl: "https://example.com/license",
  repo: "org/tiny",
  revision: "rev1",
  languages: ["en"],
  languageHint: false,
  hotwords: false,
  recognizer: "sense-voice",
  files: Object.entries(FILES).map(([name, data]) => ({ name, size: data.byteLength, sha256: sha256(data) })),
};

type Served = { url: string; range?: string };

/** Serves FILES, honouring Range; `failAfter` cuts a body short once. */
function server(options: { failHosts?: string[]; failAfter?: number; corrupt?: string } = {}) {
  const requests: Served[] = [];
  let cutOnce = options.failAfter;
  const fetch = vi.fn(async (url: string, init: { signal: AbortSignal; headers: Record<string, string> }) => {
    requests.push({ url, range: init.headers.Range });
    const host = new URL(url).host;
    if (options.failHosts?.includes(host)) return new Response("down", { status: 503 });
    const name = url.split("/resolve/rev1/")[1]!;
    let data = FILES[name]!;
    if (options.corrupt === name) data = encoder.encode("x".repeat(data.byteLength));
    const start = init.headers.Range ? Number(/bytes=(\d+)-/.exec(init.headers.Range)![1]) : 0;
    const slice = data.subarray(start);
    const cut = cutOnce;
    cutOnce = undefined;
    let sent = false;
    const body = new ReadableStream<Uint8Array>({
      pull(controller) {
        if (init.signal.aborted) controller.error(init.signal.reason);
        else if (sent) controller[cut !== undefined ? "error" : "close"](new Error("connection reset"));
        else controller.enqueue(cut !== undefined ? slice.subarray(0, cut) : slice);
        sent = true;
      },
    });
    return new Response(body, { status: start > 0 ? 206 : 200 });
  });
  return { fetch, requests };
}

let root: string;
const onChange = vi.fn();

function store(fetch: ReturnType<typeof server>["fetch"]) {
  return new ModelStore({
    root,
    fetch,
    onChange,
    catalog: [MODEL],
    mirrors: ["https://primary.test", "https://mirror.test"],
  });
}

beforeEach(async () => {
  root = await mkdtemp(join(tmpdir(), "voice-models-"));
  onChange.mockReset();
});

afterEach(async () => {
  await rm(root, { recursive: true, force: true });
});

describe("ModelStore", () => {
  it("downloads, verifies and installs a model", async () => {
    const { fetch, requests } = server();
    const models = store(fetch);
    await models.init();
    expect(models.list()[0]).toMatchObject({ status: "absent", sizeBytes: 32012 });

    await models.download("tiny");
    expect(models.isReady("tiny")).toBe(true);
    expect(models.list()[0]).toMatchObject({ status: "ready" });
    expect(await readFile(join(root, "tiny", "tokens.txt"), "utf8")).toBe("a b c");
    expect(await readFile(join(root, "tiny", "tokenizer", "vocab.json"), "utf8")).toBe('{"a":1}');
    expect(existsSync(join(root, ".partial", "tiny"))).toBe(false);
    expect(requests.map((r) => r.url)).toEqual([
      "https://primary.test/org/tiny/resolve/rev1/model.onnx",
      "https://primary.test/org/tiny/resolve/rev1/tokens.txt",
      "https://primary.test/org/tiny/resolve/rev1/tokenizer/vocab.json",
    ]);

    const reopened = store(fetch);
    await reopened.init();
    expect(reopened.isReady("tiny")).toBe(true);
  });

  it("falls back to the mirror", async () => {
    const { fetch, requests } = server({ failHosts: ["primary.test"] });
    const models = store(fetch);
    await models.download("tiny");
    expect(models.isReady("tiny")).toBe(true);
    expect(requests.filter((r) => r.url.startsWith("https://mirror.test"))).toHaveLength(3);
  });

  it("resumes a cut-off download with a Range request", async () => {
    const { fetch, requests } = server({ failHosts: ["mirror.test"], failAfter: 1000 });
    const models = store(fetch);
    await models.download("tiny");
    expect(models.isReady("tiny")).toBe(false);
    expect(models.list()[0]?.error).toBeTruthy();

    await models.download("tiny");
    expect(models.isReady("tiny")).toBe(true);
    expect(requests.some((r) => r.range === "bytes=1000-")).toBe(true);
    expect(await readFile(join(root, "tiny", "model.onnx"))).toEqual(Buffer.from(FILES["model.onnx"]!));
  });

  it("rejects a file that fails its checksum and throws it away", async () => {
    const { fetch } = server({ corrupt: "tokens.txt" });
    const models = store(fetch);
    await models.download("tiny");
    expect(models.isReady("tiny")).toBe(false);
    expect(models.list()[0]?.error).toMatch(/checksum/);
    expect(existsSync(join(root, ".partial", "tiny", "tokens.txt"))).toBe(false);
  });

  it("cancelling discards the partial download without an error", async () => {
    const { fetch } = server();
    const models = store(fetch);
    const pending = models.download("tiny");
    models.cancel("tiny");
    await pending;
    expect(models.list()[0]).toMatchObject({ status: "absent" });
    expect(models.list()[0]?.error).toBeUndefined();
    expect(existsSync(join(root, ".partial", "tiny"))).toBe(false);
  });

  it("ignores a model installed from another revision, and deletes", async () => {
    await mkdir(join(root, "tiny"), { recursive: true });
    await writeFile(join(root, "tiny", "model.json"), JSON.stringify({ revision: "old" }));
    const { fetch } = server();
    const models = store(fetch);
    await models.init();
    expect(models.isReady("tiny")).toBe(false);

    await models.download("tiny");
    expect(models.isReady("tiny")).toBe(true);
    await models.remove("tiny");
    expect(models.isReady("tiny")).toBe(false);
    expect(existsSync(join(root, "tiny"))).toBe(false);
  });
});
