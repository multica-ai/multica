// @vitest-environment node
import { randomUUID } from "node:crypto";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, expect, it, vi } from "vitest";
import { environmentSchema, type Environment } from "../contracts/resources.js";
import { HostStorage } from "../host/storage.js";
import { JobService, type OperationContext } from "../host/jobs.js";
import { Bindings } from "../sources/bindings.js";
import { Environments } from "./environments.js";
import { launch, run } from "./process.js";
import { prepareToolchain } from "./toolchain.js";
import { awaitPrerequisites } from "./prerequisites.js";

vi.mock("./source-identity.js", () => ({
  sourceIdentity: vi.fn(async () => "prepared-tree"),
}));
vi.mock("./prerequisites.js", () => ({ awaitPrerequisites: vi.fn() }));
vi.mock("./toolchain.js", () => ({ prepareToolchain: vi.fn() }));
vi.mock("./production-guard.js", () => ({
  productionRuntimeDigest: vi.fn(async () => "production-unchanged"),
}));
vi.mock("./process.js", () => ({ launch: vi.fn(), run: vi.fn() }));

const cleanups: (() => Promise<unknown>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
  vi.restoreAllMocks();
  vi.resetAllMocks();
  vi.unstubAllGlobals();
});

async function fixture(max = 2) {
  const directory = await mkdtemp(join(tmpdir(), "verification-start-"));
  cleanups.push(() => rm(directory, { recursive: true, force: true }));
  const storage = await HostStorage.open({
    projectRoot: directory,
    stateRoot: join(directory, "state"),
  });
  const jobs = await JobService.open(storage);
  const bindings = new Bindings();
  const environments = new Environments(storage, jobs, bindings, {
    sharedRoot: directory,
    repositoryUrl: directory,
    maxEnvironments: max,
    defaultIdleMinutes: 120,
  });
  const environmentDirectory = (id: string) =>
    join(storage.root, "environments", id);
  cleanups.push(async () => {
    await environments.close({ keepEnvironments: true });
    await jobs.close();
    await storage.close();
  });
  const context: OperationContext = {
    signal: new AbortController().signal,
    addResource: async () => {},
    progress: async () => {},
    awaitingUser: async () => {},
  };
  vi.mocked(prepareToolchain).mockResolvedValue({
    node: "/test/node",
    pnpm: "/test/pnpm.cjs",
    env: {},
  });
  vi.mocked(run).mockResolvedValue(undefined);
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({
        commit: "0".repeat(40),
        pid: 2147483647,
        started_at: new Date().toISOString(),
      }),
    ),
  );
  vi.spyOn(environments, "desktopRequest").mockImplementation(
    async (id, path) => {
      if (path === "/capabilities")
        return {
          schemaVersion: 1,
          environmentId: id,
          userId: "test-user",
          pid: 2147483647,
        };
      if (path === "/shutdown") return {};
      if (path === "/connection?kind=daemon") {
        const environment = await environments.get(id);
        return {
          baseUrl: "http://127.0.0.1:32000",
          tokenFile: join(environmentDirectory(id), "verification.token"),
          scope: {
            apiOrigin: environment.apiOrigin,
            userId: "test-user",
            workspaceId: "test-workspace",
          },
          identity: {
            ownerId: id,
            runId: id,
            commit: environment.commit,
            pid: 2147483647,
          },
        };
      }
      throw new Error(`Unexpected diagnostic request: ${path}`);
    },
  );
  vi.spyOn(bindings, "request").mockImplementation(async (ref, path) => {
    if (path === "/health") {
      const binding = bindings.get(ref);
      return {
        status: "running",
        instance_owner_id: binding.identity.ownerId,
        instance_run_id: binding.identity.runId,
        cli_commit: binding.identity.commit,
        pid: binding.identity.pid,
      };
    }
    if (path === "/local/v2/verification/capabilities")
      return { schemaVersion: 1, generation: "test-generation" };
    throw new Error(`Unexpected daemon request: ${path}`);
  });
  vi.mocked(launch).mockImplementation(async (_executable, _args, options) => {
    const session = JSON.parse(
      await readFile(options.env!.MULTICA_VERIFICATION_SESSION!, "utf8"),
    );
    await writeFile(
      join(session.directory, "desktop.json"),
      JSON.stringify({
        schemaVersion: 1,
        environmentId: session.environmentId,
        ownerId: session.environmentId,
        pid: 2147483647,
        port: 32000,
        debugPort: 0,
        commit: session.commit,
        sourceDigest: session.sourceDigest,
        apiOrigin: session.apiOrigin,
      }),
    );
    return {
      pid: 2147483647,
      exited: new Promise(() => {}),
      stop: vi.fn(async () => {}),
    };
  });
  let apiPort = 32001;
  const record = async (
    state: Environment["state"] = "prepared",
    owner = "test-owner",
  ) => {
    const id = randomUUID(),
      checkout = join(directory, "checkouts", id),
      folder = environmentDirectory(id),
      port = apiPort++;
    await mkdir(checkout, { recursive: true });
    await mkdir(folder, { recursive: true });
    await writeFile(
      join(checkout, ".env.worktree"),
      `PORT=${port}\nPOSTGRES_PORT=5432\nDATABASE_URL=postgres://test:test@localhost:5432/test\n`,
    );
    const environment = environmentSchema.parse({
      schemaVersion: 1,
      environmentId: id,
      projectRef: storage.projectRef,
      commit: "0".repeat(40),
      checkout,
      mode: "desktop",
      dataMode: "fixture",
      state,
      apiOrigin: `http://localhost:${port}`,
      components: {
        owner,
        sourceTreeDigest: "prepared-tree",
        sourceDigest: "test-source",
        runtimeAttempted: false,
      },
    });
    await storage.write(`environments/${id}.json`, environment);
    const token = "0".repeat(64);
    await writeFile(join(folder, "verification.token"), token, { mode: 0o600 });
    await writeFile(
      join(folder, "session.json"),
      JSON.stringify({
        schemaVersion: 1,
        environmentId: id,
        token,
        directory: folder,
        profileDir: join(folder, "profile"),
        commit: environment.commit,
        sourceDigest: "test-source",
        apiOrigin: environment.apiOrigin,
      }),
    );
    return id;
  };
  return { environments, environmentDirectory, context, record };
}

it("runs two isolated environments within capacity and stops only the selected one", async () => {
  const { environments, environmentDirectory, context, record } =
    await fixture();
  const first = await record(),
    second = await record();
  expect((await environments["start"](first, context)).state).toBe("ready");
  expect((await environments["start"](second, context)).state).toBe("ready");
  const calls = vi.mocked(launch).mock.calls;
  expect(calls).toHaveLength(2);
  for (const [index, id] of [first, second].entries()) {
    const env = calls[index]![2].env!;
    expect(env.HOME).toBe(join(environmentDirectory(id), "runtime-home"));
    expect(env.MULTICA_DEV_USER_DATA).toBe(
      join(environmentDirectory(id), "profile"),
    );
    expect(env.MULTICA_DEV_HEALTH_PORT).toBeTruthy();
    const database = (await environments.get(id)).components.database;
    expect(database).toMatchObject({ container: `multica-verification-${id}` });
  }
  expect(calls[0]![2].env!.MULTICA_DEV_HEALTH_PORT).not.toBe(
    calls[1]![2].env!.MULTICA_DEV_HEALTH_PORT,
  );
  expect((await environments["stop"](second, context)).state).toBe("stopped");
  expect((await environments.get(first)).state).toBe("ready");
  const children = await Promise.all(
    vi.mocked(launch).mock.results.map((result) => result.value),
  );
  expect(children[0]!.stop).not.toHaveBeenCalled();
  expect(children[1]!.stop).toHaveBeenCalledOnce();
  expect(
    vi
      .mocked(run)
      .mock.calls.filter(([exe, args]) => exe === "make" && args[0] === "down"),
  ).toMatchObject([
    ["make", ["down"], { cwd: (await environments.get(second)).checkout }],
  ]);
});

it.each([1, 2])(
  "enforces the configured capacity of %i when a prepared environment starts",
  async (max) => {
    const { environments, context, record } = await fixture(max);
    const first = await record("ready", "alice");
    const holders = [first];
    if (max === 2) holders.push(await record("ready", "bob"));
    const third = await record();
    await expect(environments["start"](third, context)).rejects.toMatchObject({
      problem: {
        code: "instanceBusy",
        relatedRefs: holders.sort(),
        message: expect.stringContaining(`All ${max}`),
      },
    });
    expect(launch).not.toHaveBeenCalled();
  },
);

it.each([
  "preparing",
  "starting",
  "ready",
  "awaitingUser",
  "stopping",
] as const)(
  "counts a %s environment toward capacity without treating its process as abandoned",
  async (state) => {
    const { environments, environmentDirectory, context, record } =
      await fixture();
    const active = await record(state),
      next = await record();
    await writeFile(
      join(environmentDirectory(active), "desktop.json"),
      JSON.stringify({ pid: process.pid }),
    );
    expect((await environments["start"](next, context)).state).toBe("ready");
    expect((await environments.get(active)).state).toBe(state);
  },
);

it.each([
  ["failed", "desktop.json"],
  ["interrupted", "desktop.json"],
  ["stopped", "desktop.json"],
  ["failed", "runtime-home/.multica/desktop-runtime/runtime.json"],
] as const)(
  "still blocks a live retained process from a %s environment in %s",
  async (state, file) => {
    const { environments, environmentDirectory, context, record } =
      await fixture();
    const retained = await record(state),
      next = await record();
    const path = join(environmentDirectory(retained), file);
    await mkdir(join(path, ".."), { recursive: true });
    await writeFile(path, JSON.stringify({ pid: process.pid }));
    await expect(environments["start"](next, context)).rejects.toMatchObject({
      problem: {
        code: "instanceBusy",
        relatedRefs: [retained],
        message: expect.stringContaining("retained verification process"),
      },
    });
    expect(launch).not.toHaveBeenCalled();
  },
);

it("keeps the startup prerequisite checks after allowing another active environment", async () => {
  const { environments, context, record } = await fixture();
  await record("ready");
  const next = await record();
  vi.mocked(awaitPrerequisites).mockRejectedValueOnce(
    new Error("test prerequisite unavailable"),
  );
  await expect(environments["start"](next, context)).rejects.toThrow(
    "test prerequisite unavailable",
  );
  expect(launch).not.toHaveBeenCalled();
});
