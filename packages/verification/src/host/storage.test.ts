// @vitest-environment node
import { spawn, type ChildProcess } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";
import { afterEach, describe, expect, it } from "vitest";
import { createVerificationHost } from "./host.js";
import { HostStorage } from "./storage.js";

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
  it("refuses a live owner of one state directory and rejects another project's binding", async () => {
    const options = await fixture();
    const first = await HostStorage.open(options);
    await expect(HostStorage.open(options)).rejects.toMatchObject({
      problem: { code: "instanceBusy" },
    });
    await first.close();
    const other = await fixture();
    await expect(
      HostStorage.open({ ...options, projectRoot: other.root }),
    ).rejects.toMatchObject({ problem: { code: "identityMismatch" } });
    const reopened = await HostStorage.open(options);
    cleanups.push(() => reopened.close());
  });

  it("lets concurrent hosts share a project and removes a closed host's directory", async () => {
    const options = await fixture();
    const [first, second] = await Promise.all([
      createVerificationHost(options),
      createVerificationHost(options),
    ]);
    expect(await readdir(join(options.stateRoot, "hosts"))).toHaveLength(2);
    await first.close();
    await second.close();
    expect(await readdir(join(options.stateRoot, "hosts"))).toEqual([]);
  });

  it("collects a killed host's directory once without replaying its operation", async () => {
    const options = await fixture();
    const builtEntry = new URL("../../dist/index.js", import.meta.url).href;
    const childPath = join(options.root, "crash.mjs");
    await writeFile(
      childPath,
      `
import { createVerificationHost, jsonSchema } from ${JSON.stringify(builtEntry)};
import { appendFile } from 'node:fs/promises';
const host = await createVerificationHost(${JSON.stringify({ projectRoot: options.projectRoot, stateRoot: options.stateRoot })});
host.jobs.register('test.crash', { input: jsonSchema, output: jsonSchema, outputSchema: 'test/v1', run: async () => {
  await appendFile(${JSON.stringify(join(options.root, "effect.log"))}, 'ran\\n');
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
    const [abandoned] = await readdir(join(options.stateRoot, "hosts"));
    await kill(child);
    const hosts = await Promise.all([
      createVerificationHost(options),
      createVerificationHost(options),
    ]);
    for (const host of hosts) cleanups.push(() => host.close());
    const remaining = await readdir(join(options.stateRoot, "hosts"));
    expect(remaining).toHaveLength(2);
    expect(remaining).not.toContain(abandoned);
    expect(await readFile(join(options.root, "effect.log"), "utf8")).toBe(
      "ran\n",
    );
  });
});
