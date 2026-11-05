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
async function record(title: string) {
  const value = await operation("verification.create", {
    title,
    kind: "smoke",
  });
  const verificationId = z.uuid().parse(value.verificationId);
  await host.evidence.bindEnvironment(verificationId, environmentId, "smoke");
  return verificationId;
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
    join(directory, "host/environments", `${environmentId}.json`),
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
    const before = await snapshot("original"),
      source = await host.data.bind(binding.ref);
    const apiBinding = {
      ...binding,
      ref: "api-only",
      databaseFile: undefined,
      filesRoot: undefined,
    };
    host.environments.bindings.add(apiBinding);
    const apiSource = await host.data.bind(apiBinding.ref);
    const exported = await host.data.snapshot(apiSource.sourceId, {
      conversationIds: ["original"],
    });
    const contents = object.parse(exported.content),
      conversations = z.array(object).parse(contents.conversations);
    expect(
      z
        .array(z.unknown())
        .parse(object.parse(conversations[0]!.snapshot).turns),
    ).toHaveLength(23);
    const consistent = await host.data.snapshot(source.sourceId, {
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
    const restored = await host.data.restore(
      environmentId,
      consistent.dataSnapshotId,
    );
    expect(restored.state).toBe("ready");
    expect(restored.refs.original).not.toBe("original");
    const copy = await snapshot(restored.refs.original!);
    expect(copy.queue).toMatchObject({ paused: true });
    expect(
      (
        await host.environments.bindings.bytes(
          binding.ref,
          `/local/v2/attachments/${restored.refs["ffffffffffffffffffffffffffffffff"]}`,
        )
      ).toString(),
    ).toBe("source attachment bytes");
    expect(await snapshot("original")).toEqual(before);
    const reset = await host.data.reset(restored.datasetId);
    expect(reset.restoreVersion).toBe(2);
    expect(reset.refs.original).not.toBe(restored.refs.original);
    expect(
      object.parse((await snapshot(restored.refs.original!)).conversation)
        .archived,
    ).toBe(true);
    expect(await snapshot("original")).toEqual(before);
  });

  it("observes real queue dispatch over WS and durable cursors, checks exactly-once receipt, and archives reports", async () => {
    const verificationId = await record("Queue dispatch with a fake provider");
    const dataset = await host.data.fixture(environmentId, 3),
      subjectRef = dataset.refs.conversationId!;
    const step = await host.sources.begin({
      verificationId,
      environmentId,
      subjectRef,
      description: "A completes, B dispatches exactly once",
      driver: "api",
      sources: ["daemon"],
    });
    expect(step.coverage).toBe("complete");
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
      dataset.datasetId,
      "release",
      "Answer A",
      "queue-a",
    );
    await eventually(
      () => host.data.control(dataset.datasetId, "inspect"),
      (value) =>
        z.array(z.string()).parse(object.parse(value).received).length === 2,
    );
    await host.data.control(
      dataset.datasetId,
      "release",
      "Answer B",
      "queue-b",
    );
    await eventually(
      () => snapshot(subjectRef),
      (value) =>
        object.parse(z.array(z.unknown()).parse(value.turns).at(-1)).status ===
        "completed",
    );
    const ended = await host.sources.end(step.stepId);
    expect(ended.coverage, ended.gaps.join(",")).toBe("complete");
    expect(ended.events.length).toBeGreaterThan(3);
    const checked = await host.checks.check({
      verificationId,
      environmentId,
      subjectRef,
      predicate: {
        kind: "providerFixture.receivedCount",
        operator: "eq",
        expected: 1,
        target: "queue-b",
      },
      temporal: { mode: "throughout", stepId: step.stepId },
    });
    expect(checked).toMatchObject({ status: "passed", actual: 1 });
    const final = await host.checks.check({
      verificationId,
      environmentId,
      subjectRef,
      stepId: step.stepId,
      predicate: { kind: "queue.itemCount", operator: "eq", expected: 0 },
      temporal: { mode: "eventually", timeoutMs: 2000 },
    });
    expect(final.status).toBe("passed");
    const report = await host.evidence.finalize({
      verificationId,
      conclusion: "passed",
      explanation: "Observed the normal queue and two controlled completions.",
      uncovered: [],
    });
    const exported = await host.evidence.export(
      verificationId,
      report.reportRevision,
      "html",
    );
    expect(await readFile(exported.path, "utf8")).toContain(
      "providerFixture.receivedCount",
    );
    const artifact = report.evidence[0]!;
    await writeFile(join(directory, "host", artifact.path), "tampered");
    await expect(host.evidence.read(artifact.evidenceId)).rejects.toMatchObject(
      { problem: { code: "identityMismatch" } },
    );
    await expect(
      host.evidence.export(verificationId, report.reportRevision, "html"),
    ).rejects.toMatchObject({ problem: { code: "identityMismatch" } });
  });

  it("keeps missing observations unavailable and refuses a passing conclusion", async () => {
    const verificationId = await record("Missing subject");
    const step = await host.sources.begin({
      verificationId,
      environmentId,
      subjectRef: "missing",
      description: "Unavailable source",
      driver: "api",
      sources: ["daemon"],
    });
    expect(step.coverage).toBe("partial");
    await host.sources.end(step.stepId);
    const check = await host.checks.check({
      verificationId,
      environmentId,
      subjectRef: "missing",
      predicate: { kind: "queue.itemCount", operator: "eq", expected: 0 },
      temporal: { mode: "now" },
    });
    expect(check.status).toBe("unavailable");
    await expect(
      host.evidence.finalize({
        verificationId,
        conclusion: "passed",
        explanation: "",
        uncovered: [],
      }),
    ).rejects.toMatchObject({ problem: { code: "coverageIncomplete" } });
  });

  it("reconciles a lost import response using the original dataset and restore namespace", async () => {
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
      { environmentId, dataSnapshotId: fixture.dataSnapshotId! },
      randomUUID(),
    );
    if (receipt.kind !== "accepted") throw new Error("No import receipt");
    const job = await eventually(
      () => host.jobs.get(receipt.jobId),
      (value) => value.state === "unknown",
    );
    interceptor.mockRestore();
    expect(job.resourceRefs).toHaveLength(1);
    const datasetId = job.resourceRefs[0]!;
    expect(await host.data.get(datasetId)).toMatchObject({
      state: "partial",
      restoreVersion: 1,
    });
    const resumed = await host.data.resume(datasetId);
    expect(resumed).toMatchObject({
      datasetId,
      state: "ready",
      restoreVersion: 1,
    });
    expect(resumed.refs).toMatchObject(
      object.parse(object.parse(copied).mapping),
    );
    expect((await host.jobs.byRequest(receipt.requestId)).job.state).toBe(
      "unknown",
    );
  });

  it("records event generation gaps and rejects stale fake-provider completions", async () => {
    const verificationId = await record("Generation and dispatch guards");
    const dataset = await host.data.fixture(environmentId),
      subjectRef = dataset.refs.conversationId!;
    const step = await host.sources.begin({
      verificationId,
      environmentId,
      subjectRef,
      description: "Collect one request",
      driver: "api",
      sources: ["daemon"],
    });
    await request(`/local/v2/sessions/${subjectRef}/commands`, {
      type: "send",
      request_id: "guard-a",
      text: "A",
    });
    await expect(
      host.data.control(
        dataset.datasetId,
        "release",
        "Wrong request",
        "guard-b",
      ),
    ).rejects.toBeInstanceOf(VerificationError);
    expect(await host.data.control(dataset.datasetId, "inspect")).toMatchObject(
      { pending: true, pendingRequestId: "guard-a" },
    );
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
    const ended = await host.sources.end(step.stepId);
    interceptor.mockRestore();
    expect(ended.coverage).toBe("partial");
    const checked = await host.checks.check({
      verificationId,
      environmentId,
      subjectRef,
      predicate: {
        kind: "providerFixture.receivedCount",
        operator: "eq",
        expected: 1,
      },
      temporal: { mode: "throughout", stepId: step.stepId },
    });
    expect(checked.status).toBe("unavailable");
    await host.data.control(
      dataset.datasetId,
      "release",
      "Answer A",
      "guard-a",
    );
  });

  it("stops a read-only reference environment without stopping or mutating its source", async () => {
    const readOnly = {
      ...binding,
      ref: "read-only-original",
      access: "readOnly" as const,
    };
    host.environments.bindings.add(readOnly);
    const prepared = await operation("setup.prepare", {
      gitRef: "HEAD",
      mode: "desktop",
      dataMode: "liveReadOnly",
      sourceRef: readOnly.ref,
    });
    const id = z.uuid().parse(prepared.environmentId);
    expect(prepared.components).toMatchObject({
      referenceOnly: true,
      applicationReady: "notApplicable",
    });
    const original = host.environments.bindings.request.bind(
      host.environments.bindings,
    );
    const calls: string[] = [];
    const interceptor = vi
      .spyOn(host.environments.bindings, "request")
      .mockImplementation((ref, path, method, body) => {
        calls.push(`${method ?? "GET"} ${path}`);
        return original(ref, path, method, body);
      });
    expect(await operation("setup.start", { environmentId: id })).toMatchObject(
      { state: "ready" },
    );
    expect(await operation("setup.stop", { environmentId: id })).toMatchObject({
      state: "stopped",
    });
    interceptor.mockRestore();
    expect(calls).toEqual(["GET /health"]);
    expect(await request("/health")).toMatchObject({ status: "running" });
    await expect(host.pages.windows(id)).rejects.toMatchObject({
      problem: { code: "unsupportedCapability" },
    });
    await expect(
      host.environments.bindings.request(readOnly.ref, "/shutdown", "POST", {}),
    ).rejects.toMatchObject({ problem: { code: "forbidden" } });
  });

  it("operates actual CDP pages, rejects used references, waits for semantic locators and reads real screenshot bytes", async () => {
    const port = await freePort();
    browser = await chromium.launch({
      headless: true,
      args: [`--remote-debugging-port=${port}`],
    });
    const page = await browser.newPage();
    await page.setContent(
      '<button data-testid="save" onclick="this.textContent=\'Saved\'">Save</button><input type="password" value="private-password">',
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
    vi.spyOn(host.environments, "desktopRequest").mockResolvedValue({
      environmentId,
    });
    const verificationId = await record("Page adapter protocol smoke"),
      dataset = await host.data.fixture(environmentId),
      subjectRef = dataset.refs.conversationId!;
    const step = await host.sources.begin({
      verificationId,
      environmentId,
      subjectRef,
      description: "Click Save",
      driver: "playwright",
      sources: ["daemon"],
    });
    const windows = await host.pages.windows(environmentId);
    const windowRef = windows[0]!.windowRef;
    const observed = await host.pages.observe(environmentId, windowRef);
    expect(JSON.stringify(observed)).not.toContain("private-password");
    const targetRef = observed.elements.find(
      (item) => item.testId === "save",
    )!.ref;
    const action = {
      verificationId,
      environmentId,
      stepId: step.stepId,
      windowRef,
      observationId: observed.observationId,
      targetRef,
      action: { kind: "click" as const },
    };
    expect(await host.pages.perform(action)).toMatchObject({
      actionCompleted: true,
      businessOutcome: "notChecked",
    });
    await expect(host.pages.perform(action)).rejects.toMatchObject({
      problem: { code: "staleReference" },
    });
    const checked = await host.checks.check({
      verificationId,
      environmentId,
      subjectRef,
      stepId: step.stepId,
      predicate: {
        kind: "page.text",
        operator: "eq",
        expected: "Saved",
        target: { windowRef, locator: { kind: "testId", value: "save" } },
      },
      temporal: { mode: "eventually", timeoutMs: 1000 },
    });
    expect(checked.status).toBe("passed");
    const hidden = await host.pages.readCondition(
      environmentId,
      { windowRef, locator: { kind: "testId", value: "absent" } },
      "page.visible",
    );
    expect(hidden).toBe(false);
    const image = await host.pages.capture(
      verificationId,
      environmentId,
      step.stepId,
      windowRef,
    );
    expect(
      (await host.evidence.read(image.evidenceId)).bytes.subarray(0, 8),
    ).toEqual(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]));
    const server = createVerificationMcpServer(host),
      client = new Client({ name: "image-consumer", version: "1" });
    const [clientSide, serverSide] = InMemoryTransport.createLinkedPair();
    try {
      await server.connect(serverSide);
      await client.connect(clientSide);
      const result = await client.callTool({
        name: "verification_evidence",
        arguments: {
          action: { operation: "read", evidenceId: image.evidenceId },
        },
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
      expect(returned.data).toBe(
        (await host.evidence.read(image.evidenceId)).bytes.toString("base64"),
      );
    } finally {
      await client.close();
      await server.close();
    }
    await host.sources.end(step.stepId);
    const report = await host.evidence.finalize({
      verificationId,
      conclusion: "passed",
      explanation: "Actual page operation and screenshot",
      uncovered: [],
    });
    const bundle = await host.evidence.export(
      verificationId,
      report.reportRevision,
      "bundle",
    );
    expect(JSON.parse(await readFile(bundle.path, "utf8"))).toMatchObject({
      verificationId,
      reportRevision: report.reportRevision,
    });
  });
  it("prunes expired finalized archives while retaining pinned evidence and data snapshots", async () => {
    const fixture = await host.data.fixture(environmentId),
      entries = [];
    for (let index = 0; index < 2; index++) {
      const created = await operation("verification.create", {
        title: `Retention ${index}`,
        kind: "smoke",
      });
      const verificationId = z.uuid().parse(created.verificationId);
      const evidence = await host.evidence.attach({
        verificationId,
        kind: "log",
        source: "test-created-log",
        mediaType: "text/plain",
        bytes: Buffer.from("Retention fixture"),
      });
      await host.evidence.finalize({
        verificationId,
        conclusion: "inconclusive",
        explanation: "Retention fixture",
        uncovered: ["No product verification attempted"],
      });
      const path = join(
          directory,
          "host/verifications",
          `${verificationId}.json`,
        ),
        metadata = JSON.parse(await readFile(path, "utf8"));
      metadata.createdAt = new Date(Date.now() - 10 * 86400000).toISOString();
      await writeFile(path, JSON.stringify(metadata), { mode: 0o600 });
      entries.push({ verificationId, evidence });
    }
    await host.evidence.pin(entries[0]!.verificationId, true);
    expect((await host.evidence.prune()).removed).toEqual([
      entries[1]!.verificationId,
    ]);
    expect(
      (
        await host.evidence.read(entries[0]!.evidence.evidenceId)
      ).bytes.toString(),
    ).toBe("Retention fixture");
    expect(await host.records.read(entries[1]!.verificationId)).toMatchObject({
      state: "pruned",
      revisions: [],
    });
    await expect(
      host.evidence.read(entries[1]!.evidence.evidenceId),
    ).rejects.toMatchObject({ problem: { code: "notFound" } });
    expect(await host.data.getSnapshot(fixture.dataSnapshotId!)).toMatchObject({
      complete: true,
    });
  });
});
