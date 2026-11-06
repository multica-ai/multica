// @vitest-environment node
import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import { setTimeout as delay } from "node:timers/promises";
import { afterEach, describe, expect, it, vi } from "vitest";
import { z } from "zod";
import { HostStorage } from "./storage.js";
import { JobService } from "./jobs.js";

type Ledger = { jobs: JobService; close(): Promise<void> };
// Receipts and jobs persist in one host state directory; reopening it is a restart.
async function ledger(options: {
  projectRoot: string;
  stateRoot: string;
}): Promise<Ledger> {
  const storage = await HostStorage.open(options);
  const jobs = await JobService.open(storage);
  return {
    jobs,
    close: async () => {
      await jobs.close();
      await storage.close();
    },
  };
}
import type { CommandResult, Job } from "../contracts/index.js";

const cleanups: (() => Promise<unknown>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
});
async function fixture() {
  const root = await mkdtemp(join(tmpdir(), "verification-jobs-"));
  cleanups.push(() => rm(root, { recursive: true, force: true }));
  const options = { projectRoot: root, stateRoot: join(root, "state") };
  const host = await ledger(options);
  cleanups.push(() => host.close());
  return { root, options, host };
}
function accepted(result: CommandResult): string {
  if (result.kind !== "accepted")
    throw new Error(`Expected accepted, got ${result.kind}`);
  return result.jobId;
}
async function until(host: Ledger, jobId: string, state: Job["state"]) {
  for (let i = 0; i < 200; i++) {
    const job = await host.jobs.get(jobId);
    if (job.state === state) return job;
    await delay(5);
  }
  throw new Error(`Job did not reach ${state}`);
}

describe("durable verification commands", () => {
  it("deduplicates concurrent input regardless of object key order and rejects changed input", async () => {
    const { host } = await fixture();
    const run = vi.fn(async () => ({ value: "done" }));
    host.jobs.register("test.once", {
      input: z.object({ a: z.number(), b: z.number() }).strict(),
      output: z.object({ value: z.string() }).strict(),
      outputSchema: "test/v1",
      run,
    });
    const [first, second] = await Promise.all([
      host.jobs.submit("test.once", { a: 1, b: 2 }, "same-request"),
      host.jobs.submit("test.once", { b: 2, a: 1 }, "same-request"),
    ]);
    expect(first.receiptId).toBe(second.receiptId);
    await until(host, accepted(first), "completed");
    expect(run).toHaveBeenCalledTimes(1);
    await expect(
      host.jobs.submit("test.once", { a: 2, b: 2 }, "same-request"),
    ).rejects.toMatchObject({ problem: { code: "requestConflict" } });
    await expect(
      host.jobs.submit("host.cancel", { jobId: randomUUID() }, "same-request"),
    ).rejects.toMatchObject({ problem: { code: "requestConflict" } });
  });

  it("retains job resources and completed receipts across host restarts", async () => {
    const { host, options, root } = await fixture();
    const register = (target: Ledger) =>
      target.jobs.register("test.create", {
        input: z.object({ title: z.string() }).strict(),
        output: z.object({ id: z.uuid() }).strict(),
        outputSchema: "test/v1",
        run: async (_input, context) => {
          const id = randomUUID();
          await context.addResource(id);
          return { id };
        },
      });
    register(host);
    const command = await host.jobs.submit(
      "test.create",
      { title: "Queue regression" },
      "create-queue",
    );
    const job = await until(host, accepted(command), "completed");
    expect(job.resourceRefs).toHaveLength(1);
    const id = job.resourceRefs[0]!;
    expect(job.output?.value).toEqual({ id });
    expect(
      (await stat(join(root, "state", "jobs", `${job.jobId}.json`))).mode &
        0o777,
    ).toBe(0o600);
    await host.close();
    const restarted = await ledger(options);
    cleanups.push(() => restarted.close());
    register(restarted);
    const retry = await restarted.jobs.submit(
      "test.create",
      { title: "Queue regression" },
      "create-queue",
    );
    expect(retry).toMatchObject({
      kind: "completed",
      receiptId: command.receiptId,
      value: { id },
    });
    expect((await restarted.jobs.byRequest("create-queue")).job.state).toBe(
      "completed",
    );
    expect((await restarted.jobs.getReceipt(command.receiptId)).job.jobId).toBe(
      job.jobId,
    );
  });

  it("cancels a user-waiting operation and preserves already registered resources", async () => {
    const { host } = await fixture();
    const resource = randomUUID();
    let started!: () => void;
    const running = new Promise<void>((resolve) => {
      started = resolve;
    });
    host.jobs.register("test.wait", {
      input: z.object({}).strict(),
      output: z.null(),
      outputSchema: "test/v1",
      run: async (_input, context) => {
        await context.addResource(resource);
        await context.awaitingUser("systemPermission");
        started();
        await delay(10_000, undefined, { signal: context.signal });
        return null;
      },
    });
    const jobId = accepted(await host.jobs.submit("test.wait", {}, "wait"));
    await running;
    expect(await host.jobs.get(jobId)).toMatchObject({
      state: "awaitingUser",
      phase: "systemPermission",
    });
    const cancel = await host.jobs.cancel(jobId, "cancel-wait");
    await until(host, accepted(cancel), "completed");
    const stopped = await until(host, jobId, "cancelled");
    expect(stopped.resourceRefs).toEqual([resource]);
    expect(stopped.problem?.code).toBe("cancelled");
    expect(await host.jobs.cancel(jobId, "cancel-wait")).toMatchObject({
      kind: "completed",
    });
  });

  it("does not replay an interrupted operation or accept a late result after close", async () => {
    const { host, options } = await fixture();
    let release!: () => void;
    let started!: () => void;
    const running = new Promise<void>((resolve) => {
      started = resolve;
    });
    const wait = new Promise<void>((resolve) => {
      release = resolve;
    });
    const run = vi.fn(async () => {
      started();
      await wait;
      return "late";
    });
    const operation = {
      input: z.null(),
      output: z.string(),
      outputSchema: "test/v1",
      run,
    };
    host.jobs.register("test.interrupt", operation);
    const jobId = accepted(
      await host.jobs.submit("test.interrupt", null, "interrupt"),
    );
    await running;
    await host.close();
    const restarted = await ledger(options);
    cleanups.push(() => restarted.close());
    restarted.jobs.register("test.interrupt", operation);
    expect(
      await restarted.jobs.submit("test.interrupt", null, "interrupt"),
    ).toMatchObject({ kind: "unknown", problem: { code: "outcomeUnknown" } });
    release();
    await delay(20);
    expect((await restarted.jobs.get(jobId)).state).toBe("interrupted");
    expect(run).toHaveBeenCalledTimes(1);
  });

  it("rejects unknown operations and corrupt persisted jobs without overwriting evidence", async () => {
    const { host, options, root } = await fixture();
    await expect(
      host.jobs.submit("not-installed", null, "unknown"),
    ).rejects.toMatchObject({ problem: { code: "unsupportedCapability" } });
    await expect(host.jobs.get("../../other-account")).rejects.toThrow();
    await host.close();
    const path = join(root, "state", "jobs", `${randomUUID()}.json`);
    await writeFile(path, '{"state":"completed"}');
    await expect(ledger(options)).rejects.toThrow();
    expect(await readFile(path, "utf8")).toBe('{"state":"completed"}');
  });
});
