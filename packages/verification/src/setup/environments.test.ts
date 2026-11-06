// @vitest-environment node
import { execFile } from "node:child_process";
import { randomUUID } from "node:crypto";
import { mkdtemp, mkdir, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import { afterEach, expect, it } from "vitest";
import { HostStorage } from "../host/storage.js";
import { JobService } from "../host/jobs.js";
import { Bindings } from "../sources/bindings.js";
import { Environments } from "./environments.js";
import { environmentSchema } from "../contracts/resources.js";

const exec = promisify(execFile);
const cleanups: (() => Promise<unknown>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
});

async function fixture(options: { repositoryUrl?: string; max?: number } = {}) {
  const directory = await mkdtemp(join(tmpdir(), "verification-env-"));
  cleanups.push(() => rm(directory, { recursive: true, force: true }));
  const storage = await HostStorage.open({
    projectRoot: directory,
    stateRoot: join(directory, "state"),
  });
  const jobs = await JobService.open(storage);
  cleanups.push(async () => {
    await jobs.close();
    await storage.close();
  });
  const environments = new Environments(storage, jobs, new Bindings(), {
    sharedRoot: join(directory, "shared"),
    repositoryUrl: options.repositoryUrl ?? directory,
    maxEnvironments: options.max ?? 2,
    defaultIdleMinutes: 120,
  });
  const record = async (
    state: string,
    owner?: string,
    environmentId: string = randomUUID(),
    checkout: string = directory,
  ) => {
    await writeFile(
      join(directory, "state/environments", `${environmentId}.json`),
      JSON.stringify(
        environmentSchema.parse({
          schemaVersion: 1,
          environmentId,
          projectRef: storage.projectRef,
          commit: "0".repeat(40),
          checkout,
          mode: "desktop",
          dataMode: "fixture",
          state,
          apiOrigin: "http://test.invalid",
          components: {
            runtimeAttempted: false,
            ...(owner ? { owner } : {}),
          },
        }),
      ),
    );
    return environmentId;
  };
  return { directory, jobs, environments, record };
}

async function until<T>(read: () => Promise<T>, accept: (value: T) => boolean) {
  for (let i = 0; i < 200; i++) {
    const value = await read();
    if (accept(value)) return value;
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  throw new Error("Condition was not reached");
}

it("reads bounded, redacted environment logs and reports truncation", async () => {
  const { directory, environments, record } = await fixture();
  const environmentId = await record("prepared");
  const folder = join(directory, "state/environments", environmentId);
  await mkdir(folder);
  await writeFile(
    join(folder, "prepare.log"),
    'Authorization: Bearer test-created-token\npassword="test-password"\n',
  );
  await writeFile(join(folder, "prepare.log.truncated"), "truncated");
  const page = await environments.readLog(environmentId, "prepare", 0, 32000);
  expect(page).toMatchObject({ truncated: true, hasMore: false });
  expect(page.text).toContain("[REDACTED]");
  expect(page.text).not.toContain("test-created-token");
  expect(page.text).not.toContain("test-password");
  await expect(
    environments.readLog(environmentId, "../session" as "prepare"),
  ).rejects.toThrow();
});

it("pins a remote branch or commit in its own mirror and rejects unknown branches", async () => {
  const remote = await mkdtemp(join(tmpdir(), "verification-remote-"));
  cleanups.push(() => rm(remote, { recursive: true, force: true }));
  const git = (...args: string[]) =>
    exec("git", ["-c", "user.name=t", "-c", "user.email=t@t", ...args], {
      cwd: remote,
    }).then(({ stdout }) => stdout.trim());
  await git("init", "-b", "main");
  await writeFile(join(remote, "a.txt"), "a");
  await git("add", ".");
  await git("commit", "--no-gpg-sign", "-m", "a");
  await git("checkout", "-b", "feature/x");
  await writeFile(join(remote, "b.txt"), "b");
  await git("add", ".");
  await git("commit", "--no-gpg-sign", "-m", "b");
  const feature = await git("rev-parse", "HEAD");
  const main = await git("rev-parse", "main");
  const { environments } = await fixture({ repositoryUrl: remote });
  expect(await environments.fetchCommit("feature/x")).toBe(feature);
  expect(await environments.fetchCommit("main")).toBe(main);
  await expect(environments.fetchCommit("missing")).rejects.toMatchObject({
    problem: { code: "invalidInput" },
  });
});

it("fails a start when every environment slot is in use, naming the holders", async () => {
  const { jobs, record } = await fixture({ max: 2 });
  await record("ready", "alice");
  await record("starting", "bob");
  await record("stopped", "carol");
  const receipt = await jobs.submit(
    "env.start",
    { branch: "main", mode: "desktop", dataMode: "fixture" },
    "full",
  );
  if (receipt.kind !== "accepted") throw new Error("Expected a job");
  const job = await until(
    () => jobs.get(receipt.jobId),
    (value) => value.state === "failed",
  );
  expect(job.problem?.code).toBe("instanceBusy");
  expect(job.problem?.message).toContain("alice");
  expect(job.problem?.message).toContain("bob");
  expect(job.problem?.message).not.toContain("carol");
});

it("removes a stopped environment's worktree and branch from the mirror", async () => {
  const remote = await mkdtemp(join(tmpdir(), "verification-remote-"));
  cleanups.push(() => rm(remote, { recursive: true, force: true }));
  const run = (cwd: string, ...args: string[]) =>
    exec("git", ["-c", "user.name=t", "-c", "user.email=t@t", ...args], {
      cwd,
    }).then(({ stdout }) => stdout.trim());
  await run(remote, "init", "-b", "main");
  await writeFile(join(remote, "a.txt"), "a");
  await run(remote, "add", ".");
  await run(remote, "commit", "--no-gpg-sign", "-m", "a");
  const { directory, jobs, environments, record } = await fixture({
    repositoryUrl: remote,
  });
  const commit = await environments.fetchCommit("main");
  const environmentId = randomUUID(),
    branch = `codex/verification-${environmentId.slice(0, 8)}`,
    checkout = join(directory, "state/worktrees", environmentId),
    mirror = join(directory, "shared/repository.git");
  await run(mirror, "worktree", "add", "-b", branch, checkout, commit);
  await record("prepared", undefined, environmentId, checkout);
  const receipt = await jobs.submit("env.stop", { environmentId }, "stop");
  if (receipt.kind !== "accepted") throw new Error("Expected a job");
  await until(
    () => jobs.get(receipt.jobId),
    (job) => job.state === "completed",
  );
  await expect(stat(checkout)).rejects.toThrow();
  expect(await run(mirror, "branch", "--list", branch)).toBe("");
  expect(await environments.get(environmentId)).toMatchObject({
    state: "stopped",
  });
});
