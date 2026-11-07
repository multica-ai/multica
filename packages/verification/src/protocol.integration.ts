// @vitest-environment node
import { spawn, execFile } from "node:child_process";
import { mkdtemp, readFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "node:net";
import { promisify } from "node:util";
import { randomUUID } from "node:crypto";
import { chromium, type Browser } from "playwright";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { z } from "zod";
import { createVerificationHost, type VerificationHost } from "./host/host.js";
import { problem, VerificationError } from "./host/problem.js";
import { createVerificationMcpServer } from "./mcp/server.js";
import {
  bindingSchema,
  environmentSchema,
  type SourceBinding,
} from "./contracts/resources.js";
import { type Json } from "./contracts/index.js";

const exec = promisify(execFile);
const root = resolve(fileURLToPath(new URL("../../../", import.meta.url)));
let directory: string,
  host: VerificationHost,
  binding: SourceBinding,
  environmentId: string;
let harness: ReturnType<typeof spawn>,
  harnessExit: Promise<unknown>,
  browser: Browser | undefined;
const object = z.record(z.string(), z.unknown());
async function eventually<T>(
  read: () => Promise<T>,
  accept: (value: T) => boolean,
  attempts = 100,
): Promise<T> {
  let last: T | undefined;
  for (let index = 0; index < attempts; index++) {
    last = await read();
    if (accept(last)) return last;
    await new Promise((done) => setTimeout(done, 20));
  }
  throw new Error(
    `Expected protocol state was not reached: ${JSON.stringify(last)}`,
  );
}
async function operation(name: string, input: Json) {
  const receipt = await host.jobs.submit(name, input, randomUUID());
  if (receipt.kind !== "accepted")
    throw new Error("A new operation must return a receipt");
  const job = await eventually(
    () => host.jobs.get(receipt.jobId),
    (value) =>
      ["completed", "failed", "unknown", "interrupted"].includes(value.state),
  );
  if (job.state !== "completed") throw new Error(JSON.stringify(job.problem));
  return object.parse(job.output?.value);
}
async function request(path: string, body?: unknown) {
  return host.environments.bindings.request(
    binding.ref,
    path,
    body === undefined ? "GET" : "POST",
    body,
  );
}
async function snapshot(id: string) {
  return object.parse(await request(`/local/v2/sessions/${id}/snapshot`));
}
async function freePort() {
  const server = createServer();
  await new Promise<void>((done) => server.listen(0, "127.0.0.1", done));
  const value = server.address();
  if (!value || typeof value === "string") throw new Error("No port");
  await new Promise<void>((done) => server.close(() => done()));
  return value.port;
}

beforeAll(async () => {
  directory = await mkdtemp(join(tmpdir(), "verification-protocol-"));
  const binary = join(directory, "harness");
  await exec(
    "go",
    [
      "build",
      "-tags=verificationdev",
      "-o",
      binary,
      "./cmd/verification-harness",
    ],
    { cwd: join(root, "server"), timeout: 120000 },
  );
  const data = join(directory, "daemon");
  harness = spawn(binary, ["--directory", data], {
    stdio: ["ignore", "ignore", "pipe"],
  });
  let exitCode: number | null | undefined,
    diagnostics = "";
  harnessExit = new Promise((done) =>
    harness.once("exit", (code) => {
      exitCode = code;
      done(code);
    }),
  );
  harness.stderr?.on("data", (chunk) => {
    diagnostics = (diagnostics + chunk.toString()).slice(-8192);
  });
  const metadata = await eventually(
    async () => {
      if (exitCode !== undefined)
        throw new Error(
          `The test-created harness exited (${exitCode}): ${diagnostics}`,
        );
      return readFile(join(data, "harness.json"), "utf8").catch(() => "");
    },
    Boolean,
    750,
  );
  binding = bindingSchema.parse({
    ...JSON.parse(metadata),
    ref: "integration",
    kind: "daemon",
    access: "test",
    filesRoot: join(data, "files"),
  });
  host = await createVerificationHost({
    projectRoot: root,
    stateRoot: join(directory, "host"),
    bindings: [binding],
  });
  environmentId = randomUUID();
  await writeFile(
    join(host.stateDirectory, "environments", `${environmentId}.json`),
    JSON.stringify(
      environmentSchema.parse({
        schemaVersion: 1,
        environmentId,
        projectRef: host.projectRef,
        commit: "0".repeat(40),
        checkout: root,
        mode: "desktop",
        dataMode: "fixture",
        state: "ready",
        apiOrigin: binding.scope.apiOrigin,
        components: {},
      }),
    ),
    { mode: 0o600 },
  );
  // The harness supplies the same authenticated Local API contract as the bundled daemon.
  // Native Electron lifecycle is separately verified on a dedicated clean Mac.
  vi.spyOn(host.environments, "connection").mockResolvedValue(binding.ref);
});
afterAll(async () => {
  await host?.close();
  await browser?.close();
  harness?.kill("SIGTERM");
  await harnessExit;
  if (directory) await rm(directory, { recursive: true, force: true });
});

describe("manual verification protocol integration", () => {
  it("copies complete paginated history and attachment bytes, freezes copies and leaves originals unchanged", async () => {
    await request("/local/v2/sessions", {
      id: "original",
      provider: "claude",
      title: "Source history",
    });
    const token = (await readFile(binding.tokenFile, "utf8")).trim();
    const uploaded = await fetch(`${binding.baseUrl}/local/v2/attachments`, {
      method: "POST",
      headers: {
        ...(await host.environments.bindings.headers(binding.ref)),
        Authorization: `Bearer ${token}`,
        "Content-Type": "text/plain",
        "X-File-Name": "notes.txt",
        "X-Attachment-ID": "ffffffffffffffffffffffffffffffff",
      },
      body: "source attachment bytes",
    });
    expect(uploaded.status).toBe(201);
    for (let index = 0; index < 23; index++) {
      await request("/local/v2/sessions/original/commands", {
        type: "send",
        request_id: `source-${index}`,
        text: `History ${index}`,
        ...(index === 0
          ? { attachment_ids: ["ffffffffffffffffffffffffffffffff"] }
          : {}),
      });
      await request("/local/v2/verification/fixture", {
        conversationId: "original",
        action: "release",
        text: `Answer ${index}`,
        expectedRequestId: `source-${index}`,
      });
      await eventually(
        () => snapshot("original"),
        (value) =>
          object.parse(z.array(z.unknown()).parse(value.turns).at(-1))
            .status === "completed",
      );
    }
    const before = await snapshot("original");
    const apiBinding = {
      ...binding,
      ref: "api-only",
      databaseFile: undefined,
      filesRoot: undefined,
    };
    host.environments.bindings.add(apiBinding);
    const exported = await host.data.snapshot(apiBinding.ref, {
      conversationIds: ["original"],
    });
    const contents = object.parse(exported.content),
      conversations = z.array(object).parse(contents.conversations);
    expect(
      z
        .array(z.unknown())
        .parse(object.parse(conversations[0]!.snapshot).turns),
    ).toHaveLength(23);
    const consistent = await host.data.snapshot(binding.ref, {
      conversationIds: ["original"],
    });
    expect(
      z.array(object).parse(object.parse(consistent.content).conversations)[0],
    ).toMatchObject({
      attachments: [
        {
          originalId: "ffffffffffffffffffffffffffffffff",
          data: Buffer.from("source attachment bytes").toString("base64"),
        },
      ],
    });
    const restored = await operation("data.restore", {
      environmentId,
      content: { kind: "snapshot", dataSnapshotId: consistent.dataSnapshotId },
    });
    const refs = z.record(z.string(), z.string()).parse(restored.refs);
    expect(refs.original).not.toBe("original");
    const copy = await snapshot(refs.original!);
    expect(copy.queue).toMatchObject({ paused: true });
    expect(
      (
        await host.environments.bindings.bytes(
          binding.ref,
          `/local/v2/attachments/${refs["ffffffffffffffffffffffffffffffff"]}`,
        )
      ).toString(),
    ).toBe("source attachment bytes");
    const again = await operation("data.restore", {
      environmentId,
      content: { kind: "snapshot", dataSnapshotId: consistent.dataSnapshotId },
    });
    expect(
      z.record(z.string(), z.string()).parse(again.refs).original,
    ).not.toBe(refs.original);
    expect(await snapshot("original")).toEqual(before);
  });

  it("reads queue dispatch through raw state and durable event pages", async () => {
    const dataset = await host.data.fixture(environmentId, 3),
      subjectRef = dataset.refs.conversationId!;
    const start = await host.state.read({
      environmentId,
      source: "daemon",
      subjectRef,
    });
    const startSeq = z
      .object({ snapshot_seq: z.number() })
      .passthrough()
      .parse(start.payload).snapshot_seq;
    await request(`/local/v2/sessions/${subjectRef}/commands`, {
      type: "send",
      request_id: "queue-a",
      text: "A",
    });
    await request(`/local/v2/sessions/${subjectRef}/commands`, {
      type: "queue_enqueue",
      item: { requestId: "queue-b", content: "B", display: { id: "queue-b" } },
    });
    await host.data.control(
      environmentId,
      subjectRef,
      "release",
      "Answer A",
      "queue-a",
    );
    await eventually(
      () => host.data.control(environmentId, subjectRef, "inspect"),
      (value) =>
        z.array(z.string()).parse(object.parse(value).received).length === 2,
    );
    await host.data.control(
      environmentId,
      subjectRef,
      "release",
      "Answer B",
      "queue-b",
    );
    const final = await eventually(
      () => host.state.read({ environmentId, source: "daemon", subjectRef }),
      (value) =>
        object.parse(
          z.array(z.unknown()).parse(object.parse(value.payload).turns).at(-1),
        ).status === "completed",
    );
    expect(object.parse(object.parse(final.payload).queue).items).toEqual([]);
    expect(
      object.parse(
        await host.data.control(environmentId, subjectRef, "inspect"),
      ).received,
    ).toEqual(["queue-a", "queue-b"]);
    const page = z
      .object({
        generation: z.string(),
        cursor: z.number(),
        highWater: z.number(),
        events: z.array(z.object({ seq: z.number() }).passthrough()),
      })
      .passthrough()
      .parse(
        (
          await host.state.events({
            environmentId,
            source: "daemon",
            subjectRef,
            after: startSeq,
          })
        ).payload,
      );
    expect(page.generation).toBe(object.parse(final.payload).store_generation);
    expect(page.events.map((event) => event.seq)).toEqual(
      Array.from({ length: page.events.length }, (_, i) => startSeq + i + 1),
    );
    expect(page.events.length).toBeGreaterThan(3);
  });

  it("reports an unknown conversation as a read error instead of empty state", async () => {
    await expect(
      host.state.read({
        environmentId,
        source: "daemon",
        subjectRef: "missing",
      }),
    ).rejects.toBeInstanceOf(VerificationError);
  });

  it("leaves a lost import response unknown with its dataset id discoverable", async () => {
    const fixture = await host.data.fixture(environmentId, 2);
    const original = host.environments.bindings.request.bind(
      host.environments.bindings,
    );
    let copied: unknown;
    const interceptor = vi
      .spyOn(host.environments.bindings, "request")
      .mockImplementation(async (ref, path, method, body) => {
        const result = await original(ref, path, method, body);
        if (path === "/local/v2/verification/import") {
          copied = result;
          throw new VerificationError(
            problem("outcomeUnknown", "Test-created response loss", "inspect"),
          );
        }
        return result;
      });
    const receipt = await host.jobs.submit(
      "data.restore",
      {
        environmentId,
        content: { kind: "snapshot", dataSnapshotId: fixture.dataSnapshotId },
      },
      randomUUID(),
    );
    if (receipt.kind !== "accepted") throw new Error("No import receipt");
    const job = await eventually(
      () => host.jobs.get(receipt.jobId),
      (value) => value.state === "unknown",
    );
    interceptor.mockRestore();
    expect(job.resourceRefs).toContain(environmentId);
    expect(
      job.resourceRefs.filter((ref) => ref !== environmentId),
    ).toHaveLength(1);
    const mapping = z
      .record(z.string(), z.string())
      .parse(object.parse(copied).mapping);
    const copiedId = mapping["fixture-conversation"]!;
    expect(
      (
        await host.state.read({
          environmentId,
          source: "daemon",
          subjectRef: copiedId,
        })
      ).subjectRef,
    ).toBe(copiedId);
  });

  it("rejects stale fake-provider completions and returns event generations unchanged", async () => {
    const dataset = await host.data.fixture(environmentId),
      subjectRef = dataset.refs.conversationId!;
    await request(`/local/v2/sessions/${subjectRef}/commands`, {
      type: "send",
      request_id: "guard-a",
      text: "A",
    });
    await expect(
      host.data.control(
        environmentId,
        subjectRef,
        "release",
        "Wrong request",
        "guard-b",
      ),
    ).rejects.toBeInstanceOf(VerificationError);
    expect(
      await host.data.control(environmentId, subjectRef, "inspect"),
    ).toMatchObject({ pending: true, pendingRequestId: "guard-a" });
    const original = host.environments.bindings.request.bind(
      host.environments.bindings,
    );
    const interceptor = vi
      .spyOn(host.environments.bindings, "request")
      .mockImplementation(async (ref, path, method, body) => {
        const result = await original(ref, path, method, body);
        return path.includes("/verification/events?")
          ? { ...object.parse(result), generation: "replaced-database" }
          : result;
      });
    const page = await host.state.events({
      environmentId,
      source: "daemon",
      subjectRef,
    });
    interceptor.mockRestore();
    expect(page.payload).toMatchObject({ generation: "replaced-database" });
    await host.data.control(
      environmentId,
      subjectRef,
      "release",
      "Answer A",
      "guard-a",
    );
  });

  it("rejects an unsupported read-only page source before installing or launching, and protects its source", async () => {
    const readOnly = {
      ...binding,
      ref: "read-only-original",
      access: "readOnly" as const,
    };
    host.environments.bindings.add(readOnly);
    const receipt = await host.jobs.submit(
      "env.start",
      {
        branch: "main",
        mode: "desktop",
        dataMode: "liveReadOnly",
        sourceRef: readOnly.ref,
      },
      randomUUID(),
    );
    if (receipt.kind !== "accepted") throw new Error("Expected receipt");
    const job = await eventually(
      () => host.jobs.get(receipt.jobId),
      (job) => job.state === "failed",
    );
    expect(job.problem?.code).toBe("unsupportedCapability");
    await expect(
      host.environments.bindings.request(readOnly.ref, "/shutdown", "POST", {}),
    ).rejects.toMatchObject({ problem: { code: "forbidden" } });
    expect(await request("/health")).toMatchObject({ status: "running" });
  });

  it("lists Computer Use targets for desktop windows, operates read-only pages by locator and returns real screenshot bytes", async () => {
    const port = await freePort();
    browser = await chromium.launch({
      headless: true,
      args: [`--remote-debugging-port=${port}`],
    });
    const page = await browser.newPage();
    await page.setContent(
      '<button data-testid="save" onclick="this.textContent=\'Saved\'">Save</button><button>Twin</button><button>Twin</button><input type="password" value="private-password">',
    );
    vi.spyOn(host.environments, "descriptor").mockResolvedValue({
      schemaVersion: 1,
      environmentId,
      pid: process.pid,
      ownerId: "browser-fixture",
      port: 1,
      debugPort: port,
      commit: "0".repeat(40),
      sourceDigest: "fixture",
      apiOrigin: binding.scope.apiOrigin,
    });
    vi.spyOn(host.environments, "desktopRequest").mockImplementation(
      async (_id, path) =>
        path === "/snapshot?source=main"
          ? {
              payload: {
                computerUseAppId: "com.github.Electron",
                windows: [
                  {
                    title: await page.title(),
                    url: page.url(),
                    computerUseWindowId: "4321:77",
                  },
                ],
              },
            }
          : { environmentId },
    );
    const windows = await host.pages.windows(environmentId);
    expect(windows).toHaveLength(1);
    expect(windows[0]).toMatchObject({
      computerUse: { appId: "com.github.Electron", windowId: "4321:77" },
    });
    const windowId = windows[0]!.windowId;
    expect((await host.pages.windows(environmentId))[0]!.windowId).toBe(
      windowId,
    );
    const save = {
      windowId,
      locator: { kind: "testId" as const, value: "save" },
    };
    await expect(
      host.pages.perform(environmentId, save, { kind: "click" }),
    ).rejects.toMatchObject({ problem: { code: "unsupportedCapability" } });
    await page.evaluate(() => {
      document.body.style.height = "5000px";
    });
    expect(
      await host.pages.perform(
        environmentId,
        { windowId, x: 10, y: 10 },
        { kind: "scroll", deltaX: 0, deltaY: 400 },
      ),
    ).toEqual({ performed: true });
    await expect
      .poll(() => page.evaluate(() => scrollY))
      .toBeGreaterThan(0);
    await page.evaluate(() => scrollTo(0, 0));
    expect(
      await host.pages.perform(environmentId, save, { kind: "scrollIntoView" }),
    ).toEqual({ performed: true });
    const livePageId = randomUUID();
    await writeFile(
      join(host.stateDirectory, "environments", `${livePageId}.json`),
      JSON.stringify(
        environmentSchema.parse({
          ...(await host.environments.get(environmentId)),
          environmentId: livePageId,
          mode: "renderer",
          dataMode: "liveReadOnly",
        }),
      ),
      { mode: 0o600 },
    );
    expect(await host.pages.windows(livePageId)).toEqual([
      { windowId, title: await page.title(), url: page.url() },
    ]);
    const observed = await host.pages.observe(environmentId);
    expect(JSON.stringify(observed)).not.toContain("private-password");
    expect(observed.elements.some((item) => item.testId === "save")).toBe(true);
    expect(
      await host.pages.perform(livePageId, save, { kind: "click" }),
    ).toEqual({ performed: true });
    expect(await host.pages.read(environmentId, save)).toMatchObject({
      count: 1,
      items: [{ index: 0, visible: true, enabled: true, text: "Saved" }],
    });
    expect(
      await host.pages.read(environmentId, {
        locator: { kind: "testId", value: "absent" },
      }),
    ).toEqual({ count: 0, items: [] });
    const twin = {
      locator: {
        kind: "role" as const,
        role: "button",
        name: "Twin",
        exact: true,
      },
    };
    await expect(
      host.pages.perform(livePageId, twin, { kind: "click" }),
    ).rejects.toMatchObject({ problem: { code: "invalidInput" } });
    expect(
      await host.pages.perform(
        livePageId,
        { ...twin, index: 1 },
        {
          kind: "click",
        },
      ),
    ).toEqual({ performed: true });
    await page.emulateMedia({ colorScheme: "dark" });
    expect(
      (await host.pages.screenshot(environmentId, windowId)).colorScheme,
    ).toBe("dark");
    await page.emulateMedia({ colorScheme: "light" });
    const shot = await host.pages.screenshot(environmentId);
    expect(shot.colorScheme).toBe("light");
    expect(shot.png.subarray(0, 8)).toEqual(
      Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    );
    const server = createVerificationMcpServer(host),
      client = new Client({ name: "image-consumer", version: "1" });
    const [clientSide, serverSide] = InMemoryTransport.createLinkedPair();
    try {
      await server.connect(serverSide);
      await client.connect(clientSide);
      const result = await client.callTool({
        name: "page_screenshot",
        arguments: { environmentId, windowId },
      });
      const returned = z
        .object({
          type: z.literal("image"),
          data: z.string(),
          mimeType: z.literal("image/png"),
        })
        .parse(
          z
            .array(z.object({ type: z.string() }).passthrough())
            .parse(result.content)
            .find((item) => item.type === "image"),
        );
      expect(Buffer.from(returned.data, "base64").subarray(0, 8)).toEqual(
        Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
      );
    } finally {
      await client.close();
      await server.close();
    }
  });
});
