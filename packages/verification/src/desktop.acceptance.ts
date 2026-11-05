import { randomUUID } from "node:crypto";
import { mkdir, writeFile, statfs } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium, type Browser, type Page } from "playwright";
import { z } from "zod";
import { ResourceSampler } from "./setup/resource-sampler.js";
import { createVerificationHost, type VerificationHost } from "./host/host.js";
import { type Json } from "./contracts/index.js";
import { datasetSchema, environmentSchema } from "./contracts/resources.js";
import { problem, toProblem, VerificationError } from "./host/problem.js";

const object = z.record(z.string(), z.unknown());
async function until<T>(
  read: () => Promise<T>,
  accept: (value: T) => boolean,
  timeoutMs: number,
): Promise<T> {
  const deadline = Date.now() + timeoutMs;
  do {
    const value = await read();
    if (accept(value)) return value;
    await new Promise((done) => setTimeout(done, 250));
  } while (Date.now() < deadline);
  throw new VerificationError(
    problem("deadlineExceeded", "The acceptance condition did not become true"),
  );
}
async function operation(
  host: VerificationHost,
  name: string,
  input: Json,
  timeoutMs = 120000,
) {
  const receipt = await host.jobs.submit(name, input, randomUUID());
  if (receipt.kind !== "accepted")
    throw new Error("A new operation did not return a receipt");
  let announced = false;
  const job = await until(
    async () => {
      const job = await host.jobs.get(receipt.jobId);
      if (job.state === "awaitingUser" && !announced) {
        announced = true;
        process.stderr.write(
          `The isolated test application is waiting for ${job.phase}. Complete normal login and workspace selection in that test application.\n`,
        );
      }
      return job;
    },
    (job) =>
      ["completed", "failed", "unknown", "cancelled", "interrupted"].includes(
        job.state,
      ),
    timeoutMs,
  );
  if (job.state !== "completed")
    throw new VerificationError(
      job.problem ??
        problem("interrupted", "The acceptance operation did not complete"),
    );
  return job.output!.value;
}
async function selectConversation(page: Page, conversationId: string) {
  const navigation = page.locator('a[href$="/conversations"]:visible');
  if ((await navigation.count()) === 1) await navigation.click();
  const link = page.locator(`a[href*="session=${conversationId}"]:visible`);
  await link.waitFor({ state: "visible", timeout: 30000 });
  if ((await link.count()) !== 1)
    throw new Error("The owned conversation link is ambiguous");
  await link.click();
  await page
    .locator('[contenteditable="true"]:visible')
    .waitFor({ state: "visible", timeout: 30000 });
}
async function send(page: Page, text: string) {
  const editor = page.locator('[contenteditable="true"]:visible');
  if ((await editor.count()) !== 1)
    throw new Error("The test conversation composer is ambiguous");
  await editor.fill(text);
  await editor.press("Enter");
}

export async function desktopAcceptance() {
  if (process.env.MULTICA_RUN_DESKTOP_VERIFICATION !== "1")
    throw new VerificationError(
      problem(
        "forbidden",
        "Run the explicit test:desktop command on a dedicated clean Mac",
      ),
    );
  const projectRoot =
    process.env.MULTICA_VERIFICATION_PROJECT ??
    fileURLToPath(new URL("../../../", import.meta.url));
  const stateRoot =
    process.env.MULTICA_VERIFICATION_STATE ??
    join(homedir(), ".multica", "verification-acceptance", randomUUID());
  const host = await createVerificationHost({ projectRoot, stateRoot });
  let browser: Browser | undefined,
    environmentId: string | undefined,
    verificationId: string | undefined;
  const startedAt = new Date().toISOString(),
    started = performance.now(),
    timings: Record<string, number> = {};
  let stopped = false;
  let sampler: ResourceSampler | undefined;
  await mkdir(stateRoot, { recursive: true, mode: 0o700 });
  const initialDisk = await statfs(stateRoot);
  try {
    await host.environments.assertTaskBoundary();
    const inspection = await host.setup.inspect("desktop");
    const blockers = inspection.deviceChecks.filter(
      (item) =>
        ["platform", "graphicalSession", "desktopRuntime"].includes(
          item.name,
        ) && item.status !== "ready",
    );
    if (blockers.length)
      throw new VerificationError(
        problem(
          "instanceBusy",
          blockers.map((item) => `${item.name}: ${item.detail}`).join("; "),
          "userAction",
        ),
      );
    sampler = new ResourceSampler(stateRoot);
    sampler.start();
    let phase = performance.now();
    const environment = environmentSchema.parse(
      await operation(
        host,
        "setup.prepare",
        {
          gitRef: process.env.MULTICA_VERIFICATION_GIT_REF ?? "HEAD",
          mode: "desktop",
          dataMode: "fixture",
          includeWorkingChanges: true,
        },
        20 * 60 * 1000,
      ),
    );
    environmentId = environment.environmentId;
    timings.prepareMs = performance.now() - phase;
    phase = performance.now();
    await operation(host, "setup.start", { environmentId }, 20 * 60 * 1000);
    timings.startAndLoginMs = performance.now() - phase;
    const descriptor = await host.environments.descriptor(environmentId);
    sampler.addRoot(descriptor.pid);
    const daemon = host.environments.bindings.get(
      await host.environments.connection(environmentId, "daemon"),
    );
    if (daemon.identity.pid) sampler.addRoot(daemon.identity.pid);
    const runtime = await host.environments.get(environmentId);
    if (
      runtime.components.backend &&
      typeof runtime.components.backend === "object" &&
      !Array.isArray(runtime.components.backend) &&
      typeof runtime.components.backend.pid === "number"
    )
      sampler.addRoot(runtime.components.backend.pid);
    browser = await chromium.connectOverCDP(
      `http://127.0.0.1:${descriptor.debugPort}`,
    );
    const pages = browser
      .contexts()
      .flatMap((context) => context.pages())
      .filter(
        (page) => !page.isClosed() && !page.url().startsWith("devtools:"),
      );
    if (pages.length !== 1)
      throw new Error(
        "Select a dedicated test environment with one application window",
      );
    const page = pages[0]!;
    const dataset = datasetSchema.parse(
      await operation(host, "data.fixture", {
        environmentId,
        historyMessageCount: 2,
      }),
    );
    const subjectRef = z.string().parse(dataset.refs.conversationId);
    const created = object.parse(
      await operation(host, "verification.create", {
        title: "Native Electron queue and navigation acceptance",
        kind: "smoke",
        requireScreenshots: true,
        plan: {
          scenario: "electron.queue-once/v1",
          scope: [
            "Normal Desktop login and bundled daemon",
            "Playwright sends A and queued B",
            "Navigation does not repeat provider receipt",
          ],
          dataSnapshotId: dataset.dataSnapshotId!,
          providerScript: "controlled/v1",
          checks: [
            {
              key: "queued",
              purpose: "target",
              kind: "queue.itemCount",
              operator: "eq",
              expected: 1,
              temporal: "eventually",
            },
            {
              key: "drained",
              purpose: "target",
              kind: "queue.itemCount",
              operator: "eq",
              expected: 0,
              temporal: "eventually",
            },
            {
              key: "receivedOnce",
              purpose: "target",
              kind: "providerFixture.receivedCount",
              operator: "eq",
              expected: 1,
              temporal: "throughout",
            },
            {
              key: "navigationBoundary",
              purpose: "boundary",
              kind: "providerFixture.receivedCount",
              operator: "eq",
              expected: 0,
              temporal: "throughout",
            },
          ],
          ui: { required: true, colorSchemes: ["light"] },
        },
      }),
    );
    verificationId = z.uuid().parse(created.verificationId);
    await operation(host, "evidence.bindEnvironment", {
      verificationId,
      environmentId,
      role: "smoke",
    });
    await selectConversation(page, subjectRef);
    const windows = await host.pages.windows(environmentId);
    if (windows.length !== 1)
      throw new Error("Application window reference is ambiguous");
    const windowRef = windows[0]!.windowRef;
    const step = await host.sources.begin({
      verificationId,
      environmentId,
      subjectRef,
      description: "Send A and enqueue B through the real composer",
      driver: "playwright",
      sources: ["daemon", "main", "renderer"],
    });
    if (step.coverage !== "complete")
      throw new Error(
        `Incomplete native source coverage: ${step.gaps.join(",")}`,
      );
    await page.emulateMedia({ colorScheme: "light" });
    await host.pages.capture(
      verificationId,
      environmentId,
      step.stepId,
      windowRef,
    );
    phase = performance.now();
    await send(page, `Verification A ${randomUUID()}`);
    const received = () =>
      host.data
        .control(dataset.datasetId, "inspect")
        .then((value) =>
          z.array(z.string()).parse(object.parse(value).received),
        );
    const first = await until(received, (ids) => ids.length === 1, 30000);
    await send(page, `Verification B ${randomUUID()}`);
    const check = async (
      comparisonKey: string,
      kind: "queue.itemCount" | "providerFixture.receivedCount",
      expected: number,
      stepId: string,
      interval = false,
      target?: string,
    ) => {
      const result = await host.checks.check({
        verificationId: verificationId!,
        environmentId: environmentId!,
        subjectRef,
        comparisonKey,
        stepId,
        predicate: {
          kind,
          operator: "eq",
          expected,
          ...(target ? { target } : {}),
        },
        temporal: interval
          ? { mode: "throughout", stepId }
          : { mode: "eventually", timeoutMs: 30000 },
      });
      if (result.status !== "passed")
        throw new Error(
          `Acceptance check failed: ${comparisonKey} (${result.status})`,
        );
    };
    await check("queued", "queue.itemCount", 1, step.stepId);
    await host.pages.capture(
      verificationId,
      environmentId,
      step.stepId,
      windowRef,
    );
    await host.data.control(dataset.datasetId, "release", "Answer A", first[0]);
    const both = await until(received, (ids) => ids.length === 2, 30000);
    await host.data.control(dataset.datasetId, "release", "Answer B", both[1]);
    await check("drained", "queue.itemCount", 0, step.stepId);
    await host.pages.capture(
      verificationId,
      environmentId,
      step.stepId,
      windowRef,
    );
    await host.sources.end(step.stepId);
    await check(
      "receivedOnce",
      "providerFixture.receivedCount",
      1,
      step.stepId,
      true,
      both[1],
    );
    const boundary = await host.sources.begin({
      verificationId,
      environmentId,
      subjectRef,
      description: "Navigate away and return without dispatching B again",
      driver: "playwright",
      sources: ["daemon", "main", "renderer"],
    });
    const issueLink = page.locator('a[href$="/issues"]:visible');
    if ((await issueLink.count()) !== 1)
      throw new Error("The issues navigation link is ambiguous");
    await issueLink.click();
    await selectConversation(page, subjectRef);
    await host.pages.capture(
      verificationId,
      environmentId,
      boundary.stepId,
      windowRef,
    );
    await host.sources.end(boundary.stepId);
    await check(
      "navigationBoundary",
      "providerFixture.receivedCount",
      0,
      boundary.stepId,
      true,
      both[1],
    );
    timings.scenarioMs = performance.now() - phase;
    await browser.close();
    browser = undefined;
    phase = performance.now();
    await operation(host, "setup.stop", { environmentId });
    stopped = true;
    timings.stopMs = performance.now() - phase;
    const report = await host.evidence.finalize({
      verificationId,
      conclusion: "passed",
      explanation:
        "The real development Electron/main/preload/renderer and bundled CLI completed the declared queue and navigation scenario using a controlled provider. No real model was called.",
      uncovered: [],
    });
    const exported = await host.evidence.export(
      verificationId,
      report.reportRevision,
      "bundle",
    );
    const disk = await statfs(stateRoot);
    const result = {
      schemaVersion: 1,
      status: "passed",
      startedAt,
      environmentId,
      verificationId,
      reportRevision: report.reportRevision,
      artifact: exported,
      timings: { ...timings, totalMs: performance.now() - started },
      hostResourceUsage: process.resourceUsage(),
      hostHeap: process.memoryUsage(),
      availableDiskDeltaBytes: (initialDisk.bavail - disk.bavail) * disk.bsize,
      sampledResources: await sampler.stop(),
      measurementScope:
        "Sampled owned process RSS and state-directory disk usage; not a whole-device profiler",
    };
    await writeFile(
      join(stateRoot, "desktop-acceptance.json"),
      JSON.stringify(result, null, 2),
      { mode: 0o600 },
    );
    process.stdout.write(JSON.stringify(result, null, 2) + "\n");
  } catch (error) {
    const result = {
      schemaVersion: 1,
      status: "inconclusive",
      startedAt,
      environmentId,
      verificationId,
      problem: toProblem(error),
      timings,
      elapsedMs: performance.now() - started,
    };
    await writeFile(
      join(stateRoot, "desktop-acceptance.json"),
      JSON.stringify(result, null, 2),
      { mode: 0o600 },
    );
    process.stderr.write(JSON.stringify(result, null, 2) + "\n");
    throw error;
  } finally {
    await sampler?.stop();
    await browser?.close().catch(() => undefined);
    if (environmentId && !stopped)
      await operation(host, "setup.stop", { environmentId }).catch(
        () => undefined,
      );
    await host.close();
  }
}
void desktopAcceptance().catch(() => {
  process.exitCode = 1;
});
