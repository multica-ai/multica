// @vitest-environment node
import { spawn, type ChildProcess } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";
import { afterEach, describe, expect, it } from "vitest";
import { createVerificationHost } from "./host.js";

const cleanups: (() => Promise<unknown>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
});
async function fixture() {
  const root = await mkdtemp(join(tmpdir(), "verification-storage-"));
  cleanups.push(() => rm(root, { recursive: true, force: true }));
  return { root, projectRoot: root, stateRoot: join(root, "state") };
}
async function kill(child: ChildProcess) {
  if (child.exitCode !== null || child.signalCode !== null) return;
  const exited = once(child, "exit");
  child.kill("SIGKILL");
  await exited;
}

describe("host ownership and crash recovery", () => {
  it("refuses live owners even with recovery, and rejects another project's binding", async () => {
    const options = await fixture();
    const first = await createVerificationHost(options);
    cleanups.push(() => first.close());
    await expect(createVerificationHost(options)).rejects.toMatchObject({
      problem: { code: "instanceBusy" },
    });
    await expect(
      createVerificationHost({ ...options, recoverLock: true }),
    ).rejects.toMatchObject({ problem: { code: "instanceBusy" } });
    await first.close();
    const other = await fixture();
    await expect(
      createVerificationHost({ ...options, projectRoot: other.root }),
    ).rejects.toMatchObject({ problem: { code: "identityMismatch" } });
    const reopened = await createVerificationHost(options);
    cleanups.push(() => reopened.close());
  });

  it("retains crash resources and receipts, admits one recovery owner, and never replays the operation", async () => {
    const options = await fixture();
    const builtEntry = new URL("../../dist/index.js", import.meta.url).href;
    const childPath = join(options.root, "crash.mjs");
    await writeFile(
      childPath,
      `
import { createVerificationHost, jsonSchema } from ${JSON.stringify(builtEntry)};
import { writeFile } from 'node:fs/promises';
import { randomUUID } from 'node:crypto';
const host = await createVerificationHost(${JSON.stringify({ projectRoot: options.projectRoot, stateRoot: options.stateRoot })});
host.jobs.register('test.crash', { input: jsonSchema, output: jsonSchema, outputSchema: 'test/v1', run: async (_input, ctx) => {
  const id = randomUUID();
  await ctx.addResource(id);
  await writeFile(${JSON.stringify(join(options.root, "effect.json"))}, JSON.stringify({ id }));
  process.stdout.write(JSON.stringify({ ready: true }) + '\\n');
  await new Promise(() => {});
  return null;
}});
await host.jobs.submit('test.crash', null, 'crash-request');
setInterval(() => {}, 1000);
`,
    );
    const child = spawn(process.execPath, [childPath], {
      stdio: ["ignore", "pipe", "pipe"],
    });
    cleanups.push(() => kill(child));
    let errors = "";
    child.stderr!.on("data", (chunk) => {
      errors += String(chunk);
    });
    const lines = createInterface({ input: child.stdout! });
    try {
      const ready = await Promise.race([
        once(lines, "line").then(([line]) => String(line)),
        once(child, "exit").then(() => {
          throw new Error(`Crash fixture exited: ${errors}`);
        }),
      ]);
      expect(JSON.parse(ready)).toEqual({ ready: true });
    } finally {
      lines.close();
    }
    await kill(child);
    await expect(createVerificationHost(options)).rejects.toMatchObject({
      problem: { code: "instanceBusy" },
    });
    const attempts = await Promise.allSettled([
      createVerificationHost({ ...options, recoverLock: true }),
      createVerificationHost({ ...options, recoverLock: true }),
    ]);
    const owners = attempts.filter((result) => result.status === "fulfilled");
    expect(owners).toHaveLength(1);
    const owner = owners[0]!;
    if (owner.status !== "fulfilled") throw new Error("Missing recovered host");
    const host = owner.value;
    cleanups.push(() => host.close());
    const { job } = await host.jobs.byRequest("crash-request");
    const effect = JSON.parse(
      await readFile(join(options.root, "effect.json"), "utf8"),
    );
    expect(job).toMatchObject({
      state: "interrupted",
      resourceRefs: [effect.id],
      problem: { code: "outcomeUnknown" },
    });
    expect(job.output).toBeUndefined();
    expect(
      attempts.filter((result) => result.status === "rejected"),
    ).toHaveLength(1);
  });
});
