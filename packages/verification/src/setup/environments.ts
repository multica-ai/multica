import { execFile } from "node:child_process";
import { randomBytes, randomUUID } from "node:crypto";
import { createServer } from "node:net";
import {
  appendFile,
  lstat,
  mkdir,
  readFile,
  writeFile,
} from "node:fs/promises";
import { dirname, join } from "node:path";
import { homedir } from "node:os";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { z } from "zod";
import { environmentSchema, type Environment } from "../contracts/resources.js";
import {
  jsonSchema,
  resourceIdSchema,
  toJson,
  jobSchema,
} from "../contracts/index.js";
import type { HostStorage } from "../host/storage.js";
import { redactLogText } from "../host/redact.js";
import { digest } from "../host/storage.js";
import type { JobService, OperationContext } from "../host/jobs.js";
import {
  isMissing,
  problem,
  toProblem,
  VerificationError,
} from "../host/problem.js";
import {
  launchReadOnlyPages,
  type ReadOnlyPageRuntime,
} from "../page/read-only.js";
import { sourceIdentity } from "./source-identity.js";
import { awaitPrerequisites } from "./prerequisites.js";
import { BuildCache, manifestSchema, verifyBuildFiles } from "./build-cache.js";
import { probeTool } from "./inspect.js";
import { prepareToolchain } from "./toolchain.js";
import {
  inheritedEnvironment,
  launch,
  run,
  type RunningProcess,
} from "./process.js";
import { productionRuntimeDigest } from "./production-guard.js";
import type { Bindings } from "../sources/bindings.js";

export const prepareInput = z
  .object({
    /** A branch on the daemon's remote, or a commit SHA reachable from it. */
    branch: z
      .string()
      .min(1)
      .max(200)
      .regex(/^(?!-)(?!.*\.\.)[A-Za-z0-9._/-]+$/),
    mode: z.enum(["renderer", "desktop"]),
    dataMode: z.enum(["liveReadOnly", "realSnapshot", "fixture"]),
    apiOrigin: z.url().optional(),
    sourceRef: z.string().optional(),
    /** Who the environment is for; shown in the console. */
    owner: z.string().max(200).optional(),
    /** Reclaim the environment after this long without MCP calls naming it. */
    idleTimeoutMinutes: z.number().int().min(1).max(10080).optional(),
  })
  .strict();

export type EnvironmentPolicy = {
  /** Project-level directory shared by every host: toolchains, build cache, repository mirror. */
  sharedRoot: string;
  /** Remote the repository mirror fetches branches from. */
  repositoryUrl: string;
  maxEnvironments: number;
  defaultIdleMinutes: number;
  /** Runs before admitting a start so a crashed host's environments do not hold capacity. */
  beforeStart?: () => Promise<void>;
};
/** States that occupy one of the daemon's environment slots. */
export const slotStates = new Set([
  "preparing",
  "starting",
  "awaitingUser",
  "ready",
  "stopping",
]);
const sessionSchema = z
  .object({
    schemaVersion: z.literal(1),
    environmentId: resourceIdSchema,
    token: z.string().regex(/^[a-f0-9]{64}$/),
    directory: z.string(),
    profileDir: z.string(),
    commit: z.string(),
    sourceDigest: z.string(),
    apiOrigin: z.url(),
  })
  .strict();
export const descriptorSchema = z
  .object({
    schemaVersion: z.literal(1),
    environmentId: resourceIdSchema,
    pid: z.number().int().positive(),
    port: z.number().int().positive(),
    ownerId: z.string(),
    commit: z.string(),
    sourceDigest: z.string(),
    apiOrigin: z.url(),
    debugPort: z.number().int().nonnegative(),
  })
  .strict();
export type Descriptor = z.infer<typeof descriptorSchema>;
type Tools = Awaited<ReturnType<typeof prepareToolchain>>;

export const git = (
  cwd: string,
  args: string[],
  raw = false,
  timeout = 30_000,
) =>
  new Promise<string>((resolve, reject) =>
    execFile(
      "git",
      args,
      {
        cwd,
        maxBuffer: 32 * 1024 * 1024,
        timeout,
        env: { ...process.env, GIT_TERMINAL_PROMPT: "0" },
      },
      (error, stdout) =>
        error
          ? reject(
              new VerificationError(
                problem(
                  "invalidInput",
                  "The requested Git operation failed",
                  "inspect",
                ),
              ),
            )
          : resolve(raw ? stdout : stdout.trim()),
    ),
  );
async function freePort(): Promise<number> {
  const server = createServer();
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("Port allocation failed");
  const port = address.port;
  await new Promise<void>((resolve) => server.close(() => resolve()));
  return port;
}

export class Environments {
  private readonly readOnlyPages = new Map<string, ReadOnlyPageRuntime>();
  private readonly processes = new Map<string, RunningProcess[]>();
  private readonly tools = new Map<string, Tools>();
  private readonly guards = new Map<string, ReturnType<typeof setInterval>>();
  private readonly activity = new Map<string, number>();
  private lifecycle: Promise<unknown> = Promise.resolve();
  constructor(
    private readonly storage: HostStorage,
    jobs: JobService,
    readonly bindings: Bindings,
    private readonly policy: EnvironmentPolicy,
  ) {
    jobs.register("env.start", {
      input: prepareInput,
      output: jsonSchema,
      outputSchema: "environment/v1",
      run: async (input, context) => {
        await this.policy.beforeStart?.();
        await this.assertCapacity();
        const prepared = await this.prepare(prepareInput.parse(input), context);
        const id = prepared.environmentId;
        let result = await this.serializeLifecycle(async () => {
          context.signal.throwIfAborted();
          return this.start(id, context);
        });
        while (result.state === "awaitingUser") {
          await context.awaitingUser(
            String(result.components.readiness ?? "loginRequired"),
          );
          await new Promise((done) => setTimeout(done, 1000));
          context.signal.throwIfAborted();
          const retained = await this.get(id);
          if (!["ready", "awaitingUser"].includes(retained.state))
            throw new VerificationError(
              problem(
                "interrupted",
                "The environment was stopped while waiting for user action",
              ),
            );
          result = await this.readiness(retained);
        }
        this.touch(id);
        return toJson(result);
      },
    });
    jobs.register("env.stop", {
      input: z.object({ environmentId: resourceIdSchema }).strict(),
      output: jsonSchema,
      outputSchema: "environment/v1",
      run: async (input, context) => {
        const id = z
          .object({ environmentId: resourceIdSchema })
          .parse(input).environmentId;
        await context.addResource(id);
        for (const name of await this.storage.list("jobs")) {
          const pending = await this.storage.read(`jobs/${name}`, jobSchema);
          if (
            pending &&
            pending.operation === "env.start" &&
            pending.resourceRefs.includes(id) &&
            ["queued", "running", "awaitingUser"].includes(pending.state)
          )
            await jobs.cancel(pending.jobId, `stop.${id}.${pending.jobId}`);
        }
        const stopped = await this.serializeLifecycle(async () => {
          context.signal.throwIfAborted();
          return this.stop(id, context);
        });
        // A resident daemon keeps stopped environments' records and logs,
        // but not their worktrees.
        if (stopped.state === "stopped") await this.removeCheckout(stopped);
        return toJson(stopped);
      },
    });
  }
  private repository() {
    return join(this.policy.sharedRoot, "repository.git");
  }
  private repositoryLock: Promise<unknown> = Promise.resolve();
  /**
   * Fetches a branch (or reachable commit) from the remote into the daemon's
   * own partial mirror and pins the commit; nobody's working copy is used.
   */
  fetchCommit(branch: string): Promise<string> {
    const next = this.repositoryLock
      .catch(() => undefined)
      .then(async () => {
        const repository = this.repository();
        try {
          await lstat(join(repository, "HEAD"));
        } catch {
          await mkdir(this.policy.sharedRoot, { recursive: true, mode: 0o700 });
          await git(
            this.policy.sharedRoot,
            [
              "clone",
              "--bare",
              "--filter=blob:none",
              "--",
              this.policy.repositoryUrl,
              repository,
            ],
            false,
            900_000,
          );
        }
        try {
          await git(
            repository,
            ["fetch", "--no-tags", "origin", branch],
            false,
            600_000,
          );
        } catch {
          throw new VerificationError(
            problem(
              "invalidInput",
              `The branch or commit ${branch} could not be fetched from ${this.policy.repositoryUrl}`,
            ),
          );
        }
        const commit = await git(repository, [
          "rev-parse",
          "--verify",
          "FETCH_HEAD^{commit}",
        ]);
        if (!/^[a-f0-9]{40}$/.test(commit))
          throw new VerificationError(
            problem("invalidInput", "The fetched ref is not a commit"),
          );
        return commit;
      });
    this.repositoryLock = next;
    return next;
  }
  /** Fails when the configured number of environments is already running. */
  private async assertCapacity(except?: string): Promise<void> {
    const running = (await this.list()).filter(
      (item) =>
        item.environmentId !== except &&
        slotStates.has(item.state) &&
        item.components.referenceOnly !== true,
    );
    if (running.length >= this.policy.maxEnvironments)
      throw new VerificationError(
        problem(
          "instanceBusy",
          `All ${this.policy.maxEnvironments} verification environment slots are in use (${running
            .map(
              (item) =>
                `${item.environmentId.slice(0, 8)}${item.components.owner ? ` for ${String(item.components.owner)}` : ""}`,
            )
            .join(", ")}); stop one or wait for its idle timeout`,
          "inspect",
          running.map((item) => item.environmentId),
        ),
      );
  }
  /** Records use by a caller; idle environments are reclaimed by the daemon. */
  touch(id: string): void {
    this.activity.set(id, Date.now());
  }
  lastActivity(id: string): number | undefined {
    return this.activity.get(id);
  }
  async list(): Promise<Environment[]> {
    const environments: Environment[] = [];
    for (const name of await this.storage.list("environments")) {
      const environment = await this.storage.read(
        `environments/${name}`,
        environmentSchema,
      );
      if (environment) environments.push(environment);
    }
    return environments;
  }
  async get(id: string): Promise<Environment> {
    resourceIdSchema.parse(id);
    const environment = await this.storage.read(
      `environments/${id}.json`,
      environmentSchema,
    );
    if (!environment || environment.projectRef !== this.storage.projectRef)
      throw new VerificationError(
        problem("notFound", "The environment is not owned by this project"),
      );
    return environment;
  }
  /**
   * Reconciles environments left by a previous process of this host. With
   * `adopt`, running Desktop environments whose application still answers
   * are kept and guarded again; everything that was mid-transition, or whose
   * runtime died with the old process, is stopped and cleaned up.
   */
  async recover(options: { adopt?: boolean } = {}): Promise<void> {
    const reclaim: string[] = [];
    for (const environment of await this.list()) {
      const id = environment.environmentId;
      if (
        ![
          "preparing",
          "starting",
          "ready",
          "awaitingUser",
          "stopping",
        ].includes(environment.state)
      )
        continue;
      if (
        options.adopt &&
        ["ready", "awaitingUser"].includes(environment.state) &&
        environment.dataMode !== "liveReadOnly"
      ) {
        try {
          await this.descriptor(id);
          await this.desktopRequest(id, "/capabilities");
          const digest = environment.components.productionRuntimeDigest;
          if (typeof digest === "string")
            this.guardProductionRuntime(id, digest);
          this.touch(id);
          continue;
        } catch {
          /* The application did not survive; reclaim it below. */
        }
      }
      await this.save({
        ...environment,
        state: "interrupted",
        problem: problem(
          "interrupted",
          "The host restarted; inspect component identities and explicitly stop retained resources",
          "inspect",
        ),
      });
      if (options.adopt) reclaim.push(id);
    }
    for (const id of reclaim)
      await this.serializeLifecycle(() => this.stop(id)).catch(() => undefined);
  }
  async status(id: string): Promise<Environment> {
    let environment = await this.get(id);
    if (
      !["starting", "ready", "awaitingUser", "interrupted", "failed"].includes(
        environment.state,
      )
    )
      return environment;
    if (environment.dataMode === "liveReadOnly") {
      const pages = this.readOnlyPages.get(id);
      const available = Boolean(pages?.browser.isConnected());
      environment = {
        ...environment,
        state: available ? "ready" : "interrupted",
        components: {
          ...environment.components,
          observedAt: new Date().toISOString(),
          observed: {
            application: { status: available ? "available" : "unavailable" },
          },
        },
      };
      await this.save(environment);
      return environment;
    }
    const components: Record<string, import("../contracts/index.js").Json> = {};
    for (const source of ["application", "daemon", "backend"] as const) {
      try {
        if (source === "application") {
          const descriptor = await this.descriptor(id);
          process.kill(descriptor.pid, 0);
          await this.desktopRequest(id, "/capabilities");
          components[source] = {
            status: "available",
            pid: descriptor.pid,
            ownerId: descriptor.ownerId,
          };
        } else {
          const ref = await this.connection(id, source);
          const binding = this.bindings.get(ref);
          await this.bindings.request(
            ref,
            source === "daemon"
              ? "/health"
              : `/api/workspaces/${encodeURIComponent(binding.scope.workspaceId)}`,
          );
          components[source] = {
            status: "available",
            identity: toJson(binding.identity),
          };
        }
      } catch (error) {
        components[source] = {
          status: "unavailable",
          problem: toJson(toProblem(error)),
        };
      }
    }
    environment = {
      ...environment,
      components: {
        ...environment.components,
        observedAt: new Date().toISOString(),
        observed: components,
      },
    };
    if (["ready", "awaitingUser"].includes(environment.state)) {
      if ((components.application as { status: string }).status !== "available")
        environment = {
          ...environment,
          state: "interrupted",
          problem: problem(
            "sourceUnavailable",
            "The application is offline; retained sources are reported separately",
            "inspect",
          ),
        };
      else environment = await this.readiness(environment);
    }
    await this.save(environment);
    return environment;
  }
  private save(value: Environment) {
    return this.storage.write(
      `environments/${value.environmentId}.json`,
      environmentSchema.parse(value),
    );
  }
  private directory(id: string) {
    return join(this.storage.root, "environments", id);
  }
  async readLog(
    id: string,
    name: "prepare" | "backend" | "desktop" | "stop",
    offset = 0,
    limit = 8000,
  ) {
    await this.get(id);
    z.enum(["prepare", "backend", "desktop", "stop"]).parse(name);
    z.number().int().nonnegative().parse(offset);
    z.number().int().min(1).max(32000).parse(limit);
    const path = join(this.directory(id), `${name}.log`);
    let text: string;
    try {
      text = redactLogText(await readFile(path, "utf8"));
    } catch {
      throw new VerificationError(
        problem("notFound", "This environment log has not been produced"),
      );
    }
    const truncated = await lstat(`${path}.truncated`).then(
      () => true,
      () => false,
    );
    return {
      environmentId: id,
      name,
      text: text.slice(offset, offset + limit),
      nextOffset: Math.min(text.length, offset + limit),
      hasMore: offset + limit < text.length,
      truncated,
    };
  }
  async assertTaskBoundary(): Promise<void> {
    let cursor = this.storage.projectRoot;
    for (;;) {
      try {
        const text = await readFile(
          join(cursor, ".multica", "daemon_task_context.json"),
          "utf8",
        );
        if (text.includes("multica-daemon-task"))
          throw new VerificationError(
            problem(
              "forbidden",
              "The daemon task boundary forbids launching a competing application or daemon",
              "userAction",
            ),
          );
      } catch (error) {
        if (error instanceof VerificationError) throw error;
        if (
          !(
            error instanceof Error &&
            "code" in error &&
            error.code === "ENOENT"
          )
        )
          throw error;
      }
      const parent = dirname(cursor);
      if (parent === cursor) break;
      cursor = parent;
    }
  }
  private async prepare(
    input: z.infer<typeof prepareInput>,
    context: OperationContext,
  ): Promise<Environment> {
    await this.assertTaskBoundary();
    const source = input.sourceRef
      ? this.bindings.get(input.sourceRef)
      : undefined;
    if (input.dataMode === "liveReadOnly" && !source)
      throw new VerificationError(
        problem(
          "invalidInput",
          "A host-bound source is required for live data",
        ),
      );
    if (
      input.dataMode === "liveReadOnly" &&
      (input.mode !== "renderer" || source?.kind !== "backend")
    )
      throw new VerificationError(
        problem(
          "unsupportedCapability",
          "Read-only application pages require renderer mode and an authorized backend binding; daemon data remains readable through source snapshots",
        ),
      );
    await context.progress("fetch");
    const commit = await this.fetchCommit(input.branch);
    const id = randomUUID();
    await context.addResource(id);
    const checkout = join(this.storage.root, "worktrees", id),
      directory = this.directory(id);
    await mkdir(directory, { mode: 0o700 });
    const apiOrigin =
      source?.scope.apiOrigin ??
      input.apiOrigin ??
      `http://localhost:${await freePort()}`;
    if (
      input.dataMode !== "liveReadOnly" &&
      !["localhost", "127.0.0.1", "[::1]"].includes(new URL(apiOrigin).hostname)
    )
      throw new VerificationError(
        problem(
          "forbidden",
          "Copies and fixtures require a local test backend",
        ),
      );
    const identity = {
      branch: input.branch,
      owner: input.owner ?? null,
      idleTimeoutMinutes:
        input.idleTimeoutMinutes ?? this.policy.defaultIdleMinutes,
    };
    let environment: Environment = {
      schemaVersion: 1,
      environmentId: id,
      projectRef: this.storage.projectRef,
      commit,
      checkout,
      mode: input.mode,
      dataMode: input.dataMode,
      state: "preparing",
      apiOrigin,
      ...(input.sourceRef ? { sourceRef: input.sourceRef } : {}),
      components: { ...identity, runtimeAttempted: false },
    };
    await this.save(environment);
    try {
      await awaitPrerequisites(
        this.storage.projectRoot,
        input.mode,
        ["platform", "disk", "commandLineTools", "make"],
        context,
        async (reason) => {
          environment.components.waitingReason = reason;
          await this.save(environment);
        },
      );
      await context.progress("checkout");
      await git(
        this.repository(),
        [
          "worktree",
          "add",
          "-b",
          `codex/verification-${id.slice(0, 8)}`,
          checkout,
          commit,
        ],
        false,
        600_000,
      );
      try {
        await lstat(
          join(checkout, "apps/desktop/src/main/verification-dev/index.ts"),
        );
      } catch {
        throw new VerificationError(
          problem(
            "unsupportedVersion",
            "This branch predates the dev verification protocol; verify a branch that contains it",
          ),
        );
      }
      const sourceDigest = digest(commit);
      const sourceTreeDigest = await sourceIdentity(checkout);
      const session = sessionSchema.parse({
        schemaVersion: 1,
        environmentId: id,
        token: randomBytes(32).toString("hex"),
        directory,
        profileDir: join(directory, "profile"),
        commit,
        sourceDigest,
        apiOrigin,
      });
      await writeFile(join(directory, "verification.token"), session.token, {
        mode: 0o600,
      });
      await writeFile(
        join(directory, "session.json"),
        JSON.stringify(session),
        { mode: 0o600 },
      );
      await context.progress("toolchain");
      const tools = await prepareToolchain(
        checkout,
        join(this.policy.sharedRoot, "tools"),
        context.signal,
      );
      this.tools.set(id, tools);
      await writeFile(join(directory, "tools.json"), JSON.stringify(tools), {
        mode: 0o600,
      });
      await context.progress("dependencies");
      await run(
        tools.node,
        [
          tools.pnpm,
          "install",
          "--filter",
          "@multica/desktop...",
          "--filter",
          "@multica/web...",
          "--frozen-lockfile",
        ],
        {
          cwd: checkout,
          env: tools.env,
          log: join(directory, "prepare.log"),
          signal: context.signal,
        },
      );
      if (input.dataMode === "liveReadOnly") {
        const require = createRequire(import.meta.url);
        await run(
          tools.node,
          [
            join(dirname(require.resolve("playwright/package.json")), "cli.js"),
            "install",
            "chromium",
          ],
          {
            cwd: checkout,
            env: tools.env,
            log: join(directory, "prepare.log"),
            signal: context.signal,
          },
        );
        if (input.mode !== "renderer" || source?.kind !== "backend")
          throw new VerificationError(
            problem(
              "unsupportedCapability",
              "Live read-only pages use renderer mode with a backend binding; full Electron verification uses an isolated snapshot copy",
            ),
          );
        environment = {
          ...environment,
          state: "prepared",
          components: {
            ...identity,
            runtimeAttempted: false,
            sourceDigest,
            sourceTreeDigest,
            buildKind: "development",
            profile: "readOnlyWeb",
            readOnlyNetwork: true,
          },
        };
        await this.save(environment);
        return environment;
      }
      const require = createRequire(
        join(checkout, "apps/desktop/package.json"),
      );
      await run(tools.node, [require.resolve("electron/install.js")], {
        cwd: checkout,
        env: { ...tools.env, ELECTRON_SKIP_BINARY_DOWNLOAD: undefined },
        log: join(directory, "prepare.log"),
        signal: context.signal,
      });
      {
        await run("make", ["worktree-env"], {
          cwd: checkout,
          env: tools.env,
          log: join(directory, "prepare.log"),
          signal: context.signal,
        });
        const file = join(checkout, ".env.worktree"),
          text = await readFile(file, "utf8");
        const matched = /^PORT=(\d+)$/m.exec(text);
        if (!matched)
          throw new VerificationError(
            problem(
              "identityMismatch",
              "The managed environment has no API port",
            ),
          );
        environment.apiOrigin = `http://localhost:${matched[1]}`;
        session.apiOrigin = environment.apiOrigin;
        await writeFile(
          join(directory, "session.json"),
          JSON.stringify(session),
          { mode: 0o600 },
        );
      }
      await context.progress("build");
      const versions = await Promise.all([
        probeTool(tools.node, ["--version"], { cwd: checkout }),
        // The managed environment selects Go through PATH/GOTOOLCHAIN.
        new Promise<string>((resolve, reject) =>
          execFile(
            "go",
            ["version"],
            // Same environment as launch/run: Go needs HOME for its module cache.
            { cwd: checkout, env: { ...inheritedEnvironment(), ...tools.env } },
            (error, stdout) => (error ? reject(error) : resolve(stdout.trim())),
          ),
        ),
      ]);
      const electronVersion = z
        .object({ version: z.string() })
        .parse(
          JSON.parse(
            await readFile(require.resolve("electron/package.json"), "utf8"),
          ),
        ).version;
      const build = await new BuildCache(
        join(this.policy.sharedRoot, "build-cache"),
      ).materialize(
        checkout,
        {
          sourceDigest,
          sourceTreeDigest,
          lockfile: digest(
            await readFile(join(checkout, "pnpm-lock.yaml"), "utf8"),
          ),
          node:
            versions[0].kind === "available" ? versions[0].output : "unknown",
          go: versions[1],
          electron: electronVersion,
          platform: process.platform,
          arch: process.arch,
          GOFLAGS: tools.env.GOFLAGS ?? "",
          CGO_ENABLED: tools.env.CGO_ENABLED ?? "",
          SDKROOT: tools.env.SDKROOT ?? "",
          MACOSX_DEPLOYMENT_TARGET: tools.env.MACOSX_DEPLOYMENT_TARGET ?? "",
          buildKind: "development",
          verificationdev: true,
          mode: input.mode,
        },
        [
          "apps/desktop/resources/bin",
          ...(input.mode === "desktop" && process.platform === "darwin"
            ? ["apps/desktop/resources/native/cua-driver"]
            : []),
        ],
        async () => {
          await run(
            tools.node,
            [
              join(checkout, "apps/desktop/scripts/bundle-cli.mjs"),
              "--require-build",
              "--verification-dev",
            ],
            {
              cwd: join(checkout, "apps/desktop"),
              env: tools.env,
              log: join(directory, "prepare.log"),
              signal: context.signal,
            },
          );
          if (input.mode === "desktop")
            for (const script of ["stage-cua-driver.mjs"])
              await run(
                tools.node,
                [join(checkout, "apps/desktop/scripts", script)],
                {
                  cwd: join(checkout, "apps/desktop"),
                  env: tools.env,
                  log: join(directory, "prepare.log"),
                  signal: context.signal,
                },
              );
        },
        context.signal,
      );
      if (input.mode === "desktop")
        await run(
          tools.node,
          [join(checkout, "apps/desktop/scripts/brand-dev-electron.mjs")],
          {
            cwd: checkout,
            // Multica Computer Use allows this bundle ID prefix without a dialog.
            env: {
              ...tools.env,
              DESKTOP_APP_SUFFIX: `verification-${id.slice(0, 8)}`,
              DESKTOP_BUNDLE_ID: `ai.multica.verification.${id}`,
            },
            log: join(directory, "prepare.log"),
            signal: context.signal,
          },
        );
      environment = {
        ...environment,
        state: "prepared",
        components: {
          ...identity,
          runtimeAttempted: false,
          sourceDigest,
          sourceTreeDigest,
          build: toJson(build),
          buildKind: "development",
          profile: "desktop-runtime",
        },
      };
      await this.save(environment);
      return environment;
    } catch (error) {
      await this.recordFailure(id, "prepare", error);
      await this.save({
        ...environment,
        state: "failed",
        problem: toProblem(error),
      });
      throw error;
    }
  }
  async descriptor(id: string): Promise<Descriptor> {
    const environment = await this.get(id),
      descriptor = descriptorSchema.parse(
        JSON.parse(
          await readFile(join(this.directory(id), "desktop.json"), "utf8"),
        ),
      );
    if (
      descriptor.environmentId !== id ||
      descriptor.commit !== environment.commit ||
      descriptor.sourceDigest !== environment.components.sourceDigest ||
      descriptor.apiOrigin !== environment.apiOrigin
    )
      throw new VerificationError(
        problem(
          "identityMismatch",
          "The running application does not match its environment",
        ),
      );
    return descriptor;
  }
  async desktopRequest(
    id: string,
    path: string,
    body?: unknown,
  ): Promise<unknown> {
    const livePages = this.readOnlyPages.get(id);
    if (livePages && body === undefined) {
      if (!livePages.browser.isConnected())
        throw new VerificationError(
          problem("sourceUnavailable", "The owned browser source is offline"),
        );
      return livePages.diagnostic(path, id);
    }
    const descriptor = await this.descriptor(id),
      session = sessionSchema.parse(
        JSON.parse(
          await readFile(join(this.directory(id), "session.json"), "utf8"),
        ),
      );
    const response = await fetch(`http://127.0.0.1:${descriptor.port}${path}`, {
      method: body === undefined ? "GET" : "POST",
      headers: {
        Authorization: `Bearer ${session.token}`,
        "Content-Type": "application/json",
      },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      signal: AbortSignal.timeout(5000),
      redirect: "error",
    });
    if (!response.ok)
      throw new VerificationError(
        problem(
          "sourceUnavailable",
          "The dev application diagnostic source is unavailable",
          "inspect",
        ),
      );
    return response.json();
  }
  async diagnosticConnection(
    id: string,
  ): Promise<{ url: string; headers: Record<string, string> }> {
    const descriptor = await this.descriptor(id),
      session = sessionSchema.parse(
        JSON.parse(
          await readFile(join(this.directory(id), "session.json"), "utf8"),
        ),
      );
    return {
      url: `http://127.0.0.1:${descriptor.port}`,
      headers: { Authorization: `Bearer ${session.token}` },
    };
  }
  async connection(id: string, kind: "daemon" | "backend"): Promise<string> {
    const environment = await this.get(id);
    if (environment.dataMode === "liveReadOnly" && environment.sourceRef) {
      const source = this.bindings.get(environment.sourceRef);
      if (source.kind === kind) return source.ref;
    }
    const ref = `${id}:${kind}`;

    const wire = z.object({
      baseUrl: z.url(),
      tokenFile: z.string(),
      scope: z.object({
        apiOrigin: z.url(),
        userId: z.string(),
        workspaceId: z.string(),
      }),
      identity: z.object({
        ownerId: z.string(),
        runId: z.string(),
        commit: z.string(),
        pid: z.number(),
      }),
    });
    let value: z.infer<typeof wire>;
    try {
      value = wire.parse(
        await this.desktopRequest(id, `/connection?kind=${kind}`),
      );
    } catch (error) {
      if (
        kind !== "daemon" ||
        (error instanceof VerificationError &&
          error.problem.code !== "sourceUnavailable")
      )
        throw error;
      try {
        value = wire.parse(
          JSON.parse(
            await readFile(
              join(this.directory(id), "daemon-connection.json"),
              "utf8",
            ),
          ),
        );
      } catch {
        throw error;
      }
      const health = z
        .object({
          instance_owner_id: z.string(),
          instance_run_id: z.string(),
          pid: z.number(),
          server_url: z.string(),
        })
        .passthrough();
      this.bindings.replace({
        ref,
        kind,
        ...value,
        access: environment.dataMode === "liveReadOnly" ? "readOnly" : "test",
      });
      const current = health.parse(await this.bindings.request(ref, "/health"));
      if (
        current.instance_owner_id !== value.identity.ownerId ||
        current.instance_run_id !== value.identity.runId ||
        current.pid !== value.identity.pid ||
        current.server_url !== value.scope.apiOrigin
      )
        throw new VerificationError(
          problem(
            "identityMismatch",
            "The retained daemon discovery no longer identifies its process",
          ),
        );
    }
    if (value.scope.apiOrigin !== environment.apiOrigin)
      throw new VerificationError(
        problem(
          "identityMismatch",
          "The application is connected to another API",
        ),
      );
    this.bindings.replace({
      ref,
      kind,
      ...(kind === "backend"
        ? {
            verificationTokenFile: join(
              this.directory(id),
              "verification.token",
            ),
          }
        : {}),
      ...value,
      access: environment.dataMode === "liveReadOnly" ? "readOnly" : "test",
    });
    if (kind === "daemon")
      await writeFile(
        join(this.directory(id), "daemon-connection.json"),
        JSON.stringify(value),
        { mode: 0o600 },
      );
    return ref;
  }

  private async readOnlyControl(id: string, shutdown = false): Promise<void> {
    const environment = await this.get(id),
      directory = this.directory(id);
    const control = z
      .object({
        environmentId: z.string(),
        ownerId: z.string(),
        pid: z.number().int().positive(),
        port: z.number().int().positive(),
        commit: z.string(),
        sourceDigest: z.string(),
        apiOrigin: z.string(),
      })
      .parse(JSON.parse(await readFile(join(directory, "web.json"), "utf8")));
    if (
      control.environmentId !== id ||
      control.commit !== environment.commit ||
      control.sourceDigest !== environment.components.sourceDigest ||
      control.apiOrigin !== environment.apiOrigin
    )
      throw new VerificationError(
        problem(
          "identityMismatch",
          "The retained web runtime belongs to another environment",
        ),
      );
    const session = sessionSchema.parse(
      JSON.parse(await readFile(join(directory, "session.json"), "utf8")),
    );
    try {
      process.kill(control.pid, 0);
    } catch (error) {
      if (
        shutdown &&
        error instanceof Error &&
        "code" in error &&
        error.code === "ESRCH"
      )
        return;
      throw error;
    }
    const base = `http://127.0.0.1:${control.port}`,
      headers = { Authorization: `Bearer ${session.token}` };
    const response = await fetch(`${base}/health`, {
      headers,
      signal: AbortSignal.timeout(5000),
      redirect: "error",
    });
    const health = z
      .object({
        environmentId: z.string(),
        ownerId: z.string(),
        pid: z.number(),
        commit: z.string(),
        sourceDigest: z.string(),
        apiOrigin: z.string(),
      })
      .parse(await response.json());
    if (
      !response.ok ||
      Object.entries(health).some(
        ([key, value]) => value !== control[key as keyof typeof control],
      )
    )
      throw new VerificationError(
        problem(
          "identityMismatch",
          "Web runtime identity no longer matches its retained receipt",
        ),
      );
    if (shutdown) {
      const stopped = await fetch(`${base}/shutdown`, {
        method: "POST",
        headers,
        signal: AbortSignal.timeout(5000),
        redirect: "error",
      });
      if (!stopped.ok)
        throw new VerificationError(
          problem("cleanupIncomplete", "Owned web shutdown was not accepted"),
        );
      for (let index = 0; index < 100; index++) {
        try {
          process.kill(control.pid, 0);
        } catch (error) {
          if (
            error instanceof Error &&
            "code" in error &&
            error.code === "ESRCH"
          )
            return;
          throw error;
        }
        await new Promise((done) => setTimeout(done, 100));
      }
      throw new VerificationError(
        problem("cleanupIncomplete", "Owned web shutdown has not finished"),
      );
    }
  }
  private async startReadOnly(
    environment: Environment,
    context: OperationContext,
  ): Promise<Environment> {
    const id = environment.environmentId;
    if (this.readOnlyPages.has(id)) return this.status(id);
    if (!["prepared", "stopped"].includes(environment.state))
      throw new VerificationError(
        problem(
          "interrupted",
          "Inspect and stop the previous read-only runtime before starting again",
          "inspect",
        ),
      );
    const tools =
      this.tools.get(id) ??
      (await prepareToolchain(
        environment.checkout,
        join(this.policy.sharedRoot, "tools"),
        context.signal,
      ));
    const webPort = await freePort(),
      debugPort = await freePort();
    await this.save({ ...environment, state: "starting" });
    const child = await launch(
      tools.node,
      [
        fileURLToPath(new URL("./read-only-web.js", import.meta.url)),
        join(this.directory(id), "session.json"),
        String(webPort),
      ],
      {
        cwd: environment.checkout,
        env: tools.env,
        log: join(this.directory(id), "desktop.log"),
        signal: context.signal,
      },
    );
    this.processes.set(id, [child]);
    try {
      const webOrigin = `http://127.0.0.1:${webPort}`;
      let available = false;
      for (let index = 0; index < 120; index++) {
        context.signal.throwIfAborted();
        try {
          available =
            (
              await fetch(webOrigin, {
                signal: AbortSignal.timeout(2000),
                redirect: "manual",
              })
            ).status < 500;
        } catch {
          /* The dev server is compiling. */
        }
        if (available) break;
        await new Promise((done) => setTimeout(done, 500));
      }
      if (!available)
        throw new VerificationError(
          problem(
            "deadlineExceeded",
            "The read-only web runtime did not become available",
          ),
        );
      const pages = await launchReadOnlyPages(
        webOrigin,
        this.bindings,
        environment.sourceRef!,
        debugPort,
      );
      this.readOnlyPages.set(id, pages);
      const descriptor = {
        schemaVersion: 1,
        environmentId: id,
        pid: child.pid,
        ownerId: id,
        port: webPort,
        debugPort,
        commit: environment.commit,
        sourceDigest: String(environment.components.sourceDigest),
        apiOrigin: environment.apiOrigin,
      };
      await writeFile(
        join(this.directory(id), "desktop.json"),
        JSON.stringify(descriptor),
        { mode: 0o600 },
      );
      environment = {
        ...environment,
        state: "ready",
        components: {
          ...environment.components,
          applicationKind: "webBrowser",
          runtimeScope: "readOnlyWebWithAuthorizedBackend",
          webOrigin,
          debugPort,
          applicationPid: child.pid,
        },
      };
      await this.save(environment);
      return environment;
    } catch (error) {
      await this.recordFailure(id, "desktop", error);
      await child.stop();
      this.processes.delete(id);
      await this.save({
        ...environment,
        state: "failed",
        problem: toProblem(error),
      });
      throw error;
    }
  }
  private async start(
    id: string,
    context: OperationContext,
  ): Promise<Environment> {
    await this.assertTaskBoundary();
    let environment = await this.get(id);
    if (
      !environment.components.sourceTreeDigest ||
      environment.components.sourceTreeDigest !==
        (await sourceIdentity(environment.checkout))
    )
      throw new VerificationError(
        problem(
          "identityMismatch",
          "The prepared source tree changed or has no immutable build identity; prepare another environment",
          "inspect",
        ),
      );
    if (environment.components.build) {
      const build = z
        .object({ manifest: manifestSchema })
        .parse(environment.components.build);
      await verifyBuildFiles(environment.checkout, build.manifest);
    }
    if (environment.dataMode === "liveReadOnly")
      return this.startReadOnly(environment, context);
    if (environment.state === "ready" || environment.state === "awaitingUser")
      return this.readiness(environment);
    if (environment.state !== "prepared" && environment.state !== "stopped")
      throw new VerificationError(
        problem("invalidInput", "The environment is not prepared"),
      );
    for (const name of await this.storage.list("environments")) {
      const other = await this.storage.read(
        `environments/${name}`,
        environmentSchema,
      );
      if (
        !other ||
        other.environmentId === id ||
        other.components.referenceOnly === true ||
        slotStates.has(other.state)
      )
        continue;
      // Active environments share the configured capacity; only processes
      // retained outside that lifecycle require cleanup before another start.
      for (const file of [
        "desktop.json",
        "runtime-home/.multica/desktop-runtime/runtime.json",
      ]) {
        try {
          const registered = z
            .object({ pid: z.number().int().positive() })
            .passthrough()
            .parse(
              JSON.parse(
                await readFile(
                  join(this.directory(other.environmentId), file),
                  "utf8",
                ),
              ),
            );
          process.kill(registered.pid, 0);
          throw new VerificationError(
            problem(
              "instanceBusy",
              "A retained verification process still exists; confirm its cleanup first",
              "inspect",
              [other.environmentId],
            ),
          );
        } catch (error) {
          if (
            error instanceof Error &&
            "code" in error &&
            ["ENOENT", "ESRCH"].includes(String(error.code))
          )
            continue;
          throw error;
        }
      }
    }
    await awaitPrerequisites(
      environment.checkout,
      environment.mode,
      ["graphicalSession", "dockerCompose", "dockerEngine"],
      context,
      async (reason) => {
        environment.components.waitingReason = reason;
        await this.save(environment);
      },
    );
    await this.policy.beforeStart?.();
    await this.assertCapacity(id);
    environment = {
      ...environment,
      state: "starting",
      components: { ...environment.components, runtimeAttempted: true },
    };
    await this.save(environment);
    const directory = this.directory(id),
      tools =
        this.tools.get(id) ??
        (await prepareToolchain(
          environment.checkout,
          join(this.policy.sharedRoot, "tools"),
          context.signal,
        ));
    this.tools.set(id, tools);
    const components: RunningProcess[] = [];
    this.processes.set(id, components);
    try {
      if (environment.dataMode !== "liveReadOnly") {
        environment.components = {
          ...environment.components,
          database: await this.startDatabase(
            id,
            environment.checkout,
            tools.env,
            context.signal,
          ),
        };
        await this.save(environment);
        await run("make", ["up", "C=api,web"], {
          cwd: environment.checkout,
          env: {
            ...tools.env,
            ...this.managedEnvironment(id),
            GOFLAGS: `${tools.env.GOFLAGS ?? ""} -tags=verificationdev`,
            MULTICA_VERIFICATION_BACKEND: id,
            MULTICA_VERIFICATION_DIRECTORY: directory,
            MULTICA_VERIFICATION_TOKEN: (
              await readFile(join(directory, "verification.token"), "utf8")
            ).trim(),
          },
          log: join(directory, "backend.log"),
          signal: context.signal,
        });
      }
      if (environment.dataMode !== "liveReadOnly") {
        const configuration = await readFile(
          join(environment.checkout, ".env.worktree"),
          "utf8",
        );
        const port = /^PORT=(\d+)$/m.exec(configuration)?.[1];
        if (!port)
          throw new VerificationError(
            problem(
              "identityMismatch",
              "The managed runtime did not publish its actual API port",
            ),
          );
        environment.apiOrigin = `http://localhost:${port}`;
        const sessionFile = join(directory, "session.json");
        const session = sessionSchema.parse(
          JSON.parse(await readFile(sessionFile, "utf8")),
        );
        session.apiOrigin = environment.apiOrigin;
        await writeFile(sessionFile, JSON.stringify(session), { mode: 0o600 });
        await this.save(environment);
      }
      const session = sessionSchema.parse(
          JSON.parse(await readFile(join(directory, "session.json"), "utf8")),
        ),
        runtimeHome = join(directory, "runtime-home"),
        healthPort = await freePort(),
        productionDigest = await productionRuntimeDigest(homedir());
      await mkdir(runtimeHome, { recursive: true, mode: 0o700 });
      const runtimeEnv: Record<string, string | undefined> = {
        ...tools.env,
        // The whole runtime tree gets its own home so it never shares the
        // installed Multica's registry, CLI link, prefs, health port or agent configs.
        HOME: runtimeHome,
        USERPROFILE: runtimeHome,
        XDG_CONFIG_HOME: join(runtimeHome, ".config"),
        CODEX_HOME: join(runtimeHome, ".codex"),
        MULTICA_DEV_ISOLATED: "1",
        MULTICA_DEV_USER_DATA: session.profileDir,
        MULTICA_DEV_HEALTH_PORT: String(healthPort),
      };
      environment.components = {
        ...environment.components,
        runtimeHome,
        healthPort,
        productionRuntimeDigest: productionDigest,
      };
      await this.save(environment);
      const child = await launch(
        tools.node,
        [
          tools.pnpm,
          "--filter",
          "@multica/desktop",
          "exec",
          "electron-vite",
          "dev",
        ],
        {
          cwd: environment.checkout,
          env: {
            ...runtimeEnv,
            MULTICA_VERIFICATION_SESSION: join(directory, "session.json"),
            MULTICA_VERIFICATION_MODE: "1",
            MULTICA_VERIFICATION_HOME: runtimeHome,
            MULTICA_WORKSPACES_ROOT: join(runtimeHome, "workspaces"),
            DESKTOP_RENDERER_PORT: String(await freePort()),
            MULTICA_VERIFICATION_TOKEN: session.token,
            VITE_API_URL: environment.apiOrigin,
            VITE_WS_URL: environment.apiOrigin.replace(/^http/, "ws") + "/ws",
            DESKTOP_APP_SUFFIX: `verification-${id.slice(0, 8)}`,
            REMOTE_DEBUGGING_PORT: "0",
            MULTICA_DAEMON_AUTO_UPDATE: "false",
            MULTICA_DAEMON_AUTO_RELOAD: "false",
          },
          log: join(directory, "desktop.log"),
          signal: context.signal,
        },
      );
      components.push(child);
      // Recorded so a restarted host can stop the dev server's process group.
      environment.components = {
        ...environment.components,
        launcherPid: child.pid,
      };
      await this.save(environment);
      let exited = false;
      void child.exited.then(() => {
        exited = true;
      });
      for (let i = 0; i < 240; i++) {
        context.signal.throwIfAborted();
        if (exited)
          throw new VerificationError(
            problem(
              "sourceUnavailable",
              "The Electron development process exited",
              "inspect",
            ),
          );
        try {
          await this.descriptor(id);
          const snapshot = z
            .object({
              schemaVersion: z.literal(1),
              environmentId: z.string(),
              pid: z.number(),
              userId: z.string().nullable(),
            })
            .passthrough()
            .parse(await this.desktopRequest(id, "/capabilities"));
          if (snapshot.environmentId !== id)
            throw new Error("Wrong application");
          environment = {
            ...environment,
            state: "awaitingUser",
            startedAt: new Date().toISOString(),
            components: {
              ...environment.components,
              applicationPid: snapshot.pid,
            },
          };
          await this.save(environment);
          this.guardProductionRuntime(id, productionDigest);
          if (!snapshot.userId) await this.signIn(environment);
          return this.readiness(environment);
        } catch {
          await new Promise((resolve) => setTimeout(resolve, 500));
        }
      }
      throw new VerificationError(
        problem(
          "deadlineExceeded",
          "The dev application did not publish a matching diagnostic endpoint",
          "inspect",
        ),
      );
    } catch (error) {
      await this.recordFailure(id, "desktop", error);
      await Promise.allSettled(components.map((child) => child.stop()));
      await this.save({
        ...environment,
        state: "failed",
        problem: toProblem(error),
      });
      throw error;
    }
  }
  private async readiness(environment: Environment): Promise<Environment> {
    const status = z
      .object({
        environmentId: z.string(),
        userId: z.string().nullable(),
        pid: z.number(),
      })
      .passthrough()
      .parse(
        await this.desktopRequest(environment.environmentId, "/capabilities"),
      );
    if (status.environmentId !== environment.environmentId)
      throw new VerificationError(
        problem(
          "identityMismatch",
          "Application readiness belongs to another environment",
        ),
      );
    let state: Environment["state"] = "awaitingUser",
      readiness = "loginRequired";
    if (status.userId) {
      try {
        if (environment.dataMode !== "liveReadOnly") {
          const response = await fetch(`${environment.apiOrigin}/health`, {
            signal: AbortSignal.timeout(5000),
            redirect: "error",
          });
          const health = z
            .object({
              commit: z.string(),
              pid: z.number().int().positive(),
              started_at: z.string(),
            })
            .passthrough()
            .parse(await response.json());
          // The backend reports an abbreviated commit.
          if (
            !response.ok ||
            health.commit.length < 7 ||
            !environment.commit.startsWith(health.commit)
          )
            throw new VerificationError(
              problem(
                "identityMismatch",
                "The backend build does not match the environment",
              ),
            );
          environment.components.backend = toJson(health);
        }
        const ref = await this.connection(environment.environmentId, "daemon"),
          binding = this.bindings.get(ref);
        const health = z
          .object({
            status: z.literal("running"),
            instance_owner_id: z.string(),
            instance_run_id: z.string(),
            pid: z.number(),
            cli_commit: z.string(),
          })
          .passthrough()
          .parse(await this.bindings.request(ref, "/health"));
        if (
          health.instance_owner_id !== binding.identity.ownerId ||
          health.instance_run_id !== binding.identity.runId ||
          health.pid !== binding.identity.pid ||
          !environment.commit.startsWith(health.cli_commit)
        )
          throw new VerificationError(
            problem(
              "identityMismatch",
              "The ready daemon does not match its source environment",
            ),
          );
        z.object({ schemaVersion: z.literal(1), generation: z.string().min(1) })
          .passthrough()
          .parse(
            await this.bindings.request(
              ref,
              "/local/v2/verification/capabilities",
            ),
          );
        state = "ready";
        readiness = "authenticatedWorkspaceAndDaemon";
      } catch (error) {
        if (
          error instanceof VerificationError &&
          error.problem.code === "identityMismatch"
        )
          throw error;
        readiness = "selectWorkspaceOrWaitForDaemon";
      }
    }
    const current = {
      ...environment,
      state,
      components: {
        ...environment.components,
        readiness,
        applicationPid: status.pid,
        permissions: toJson(status.permissions ?? null),
      },
    };
    await this.save(current);
    return current;
  }
  private async stop(
    id: string,
    _context?: OperationContext,
  ): Promise<Environment> {
    this.unguard(id);
    let environment = await this.get(id);
    if (environment.dataMode === "liveReadOnly") {
      const pages = this.readOnlyPages.get(id);
      await pages?.close();
      this.readOnlyPages.delete(id);
      const children = this.processes.get(id) ?? [];
      if (
        ["ready", "interrupted", "starting", "failed"].includes(
          environment.state,
        )
      ) {
        try {
          await this.readOnlyControl(id, true);
        } catch (error) {
          if (!children.length)
            throw new VerificationError(
              problem(
                "cleanupIncomplete",
                "The retained web runtime could not confirm owned shutdown; inspect its private identity",
                "inspect",
              ),
            );
          if (pages) throw error;
        }
      }
      await Promise.all(children.map((child) => child.stop()));
      this.processes.delete(id);
      environment = { ...environment, state: "stopped" };
      await this.save(environment);
      return environment;
    }
    if (
      environment.components.runtimeAttempted === false &&
      !this.processes.has(id)
    ) {
      environment = { ...environment, state: "stopped" };
      await this.save(environment);
      return environment;
    }
    if (environment.state === "stopped") return environment;
    if (
      environment.state === "prepared" ||
      environment.components.referenceOnly === true
    ) {
      environment.state = "stopped";
      await this.save(environment);
      return environment;
    }
    environment = { ...environment, state: "stopping" };
    await this.save(environment);
    let daemonAbsent = true;
    let daemonPid: number | undefined;
    try {
      const ref = await this.connection(id, "daemon");
      const binding = this.bindings.get(ref);
      const health = z
        .object({
          instance_owner_id: z.string(),
          instance_run_id: z.string(),
          pid: z.number(),
        })
        .passthrough()
        .parse(await this.bindings.request(ref, "/health"));
      if (
        health.instance_owner_id !== binding.identity.ownerId ||
        health.instance_run_id !== binding.identity.runId ||
        health.pid !== binding.identity.pid
      )
        throw new Error("Wrong daemon");
      daemonPid = health.pid;
      const headers = await this.bindings.headers(ref);
      const shutdown = await fetch(`${binding.baseUrl}/shutdown`, {
        method: "POST",
        headers,
        signal: AbortSignal.timeout(5000),
        redirect: "error",
      });
      if (!shutdown.ok) throw new Error("Owned daemon shutdown failed");
      // Graceful daemon and Electron shutdowns can take several seconds.
      for (let i = 0; i < 150; i++) {
        try {
          process.kill(health.pid, 0);
          await new Promise((resolve) => setTimeout(resolve, 100));
        } catch {
          daemonAbsent = true;
          break;
        }
        daemonAbsent = false;
      }
    } catch {
      const registered = await readFile(
        join(
          this.directory(id),
          "runtime-home/.multica/desktop-runtime/runtime.json",
        ),
        "utf8",
      ).catch(() => "");
      if (registered) {
        daemonAbsent = false;
        try {
          daemonPid = z
            .object({ pid: z.number().int().positive() })
            .passthrough()
            .parse(JSON.parse(registered)).pid;
        } catch {
          /* An unreadable identity cannot authorize process termination. */
        }
      }
    }
    let applicationAbsent =
      this.processes.has(id) && this.processes.get(id)!.length === 0;
    try {
      const descriptor = await this.descriptor(id);
      await this.desktopRequest(id, "/shutdown", {});
      for (let attempt = 0; attempt < 150; attempt++) {
        try {
          process.kill(descriptor.pid, 0);
          await new Promise((resolve) => setTimeout(resolve, 100));
        } catch (error) {
          if (
            error instanceof Error &&
            "code" in error &&
            error.code === "ESRCH"
          ) {
            applicationAbsent = true;
            break;
          }
          throw error;
        }
      }
    } catch {
      /* An owned child can still be stopped through its retained process handle. */
    }
    await Promise.allSettled(
      (this.processes.get(id) ?? []).map((child) => child.stop()),
    );
    const launcherPid = environment.components.launcherPid;
    if (
      !this.processes.has(id) &&
      typeof launcherPid === "number" &&
      !(await processGone(launcherPid)) &&
      (await processDirectory(launcherPid)) === environment.checkout
    ) {
      // Adopted after a restart: no handle remains, so end the recorded group.
      try {
        process.kill(-launcherPid, "SIGTERM");
      } catch {
        /* The group already exited. */
      }
      for (let attempt = 0; attempt < 100; attempt++) {
        if (await processGone(launcherPid)) break;
        await new Promise((resolve) => setTimeout(resolve, 100));
      }
    }
    this.processes.delete(id);
    if (!applicationAbsent) {
      try {
        const descriptor = await this.descriptor(id);
        for (let attempt = 0; attempt < 100; attempt++) {
          applicationAbsent = await processGone(descriptor.pid);
          if (applicationAbsent) break;
          await new Promise((resolve) => setTimeout(resolve, 100));
        }
      } catch {
        /* Without a readable descriptor the application cannot be confirmed gone. */
      }
    }
    if (daemonPid && !(await processGone(daemonPid))) {
      // Without its Desktop the daemon's Local API is unreachable; terminate it
      // only when its executable belongs to this environment's checkout.
      if (
        (await processCommand(daemonPid)).startsWith(
          join(environment.checkout, "apps/desktop/resources/bin/"),
        )
      ) {
        process.kill(daemonPid, "SIGTERM");
        for (let attempt = 0; attempt < 150; attempt++) {
          if (await processGone(daemonPid)) break;
          await new Promise((resolve) => setTimeout(resolve, 100));
        }
      }
    }
    if (daemonPid) daemonAbsent = await processGone(daemonPid);
    if (environment.dataMode !== "liveReadOnly") {
      try {
        const tools =
          this.tools.get(id) ??
          z
            .object({
              node: z.string(),
              pnpm: z.string(),
              env: z.record(z.string(), z.string()),
            })
            .parse(
              JSON.parse(
                await readFile(join(this.directory(id), "tools.json"), "utf8"),
              ),
            );
        await run("make", ["down"], {
          cwd: environment.checkout,
          env: { ...tools.env, ...this.managedEnvironment(id) },
          log: join(this.directory(id), "stop.log"),
        });
        await removeContainer(databaseContainer(id), {
          cwd: environment.checkout,
          env: tools.env,
          log: join(this.directory(id), "stop.log"),
        });
      } catch {
        await this.save({
          ...environment,
          state: "failed",
          problem: problem(
            "cleanupIncomplete",
            "The managed backend shutdown could not be confirmed",
            "inspect",
          ),
        });
        throw new VerificationError(
          problem(
            "cleanupIncomplete",
            "The managed backend shutdown could not be confirmed",
            "inspect",
            [id],
          ),
        );
      }
    }
    environment = {
      ...environment,
      state: daemonAbsent && applicationAbsent ? "stopped" : "failed",
      ...(daemonAbsent && applicationAbsent
        ? {}
        : {
            problem: problem(
              "cleanupIncomplete",
              "Application or daemon shutdown could not be confirmed",
              "inspect",
            ),
          }),
    };
    await this.save(environment);
    return environment;
  }
  /**
   * Signs the test application in without a person: the local test backend
   * accepts its development verification code, and the token reaches the
   * renderer through the same handoff a browser login uses.
   */
  private async signIn(environment: Environment): Promise<void> {
    const text = await readFile(
        join(environment.checkout, ".env.worktree"),
        "utf8",
      ),
      code =
        /^MULTICA_DEV_VERIFICATION_CODE=(\d{6})$/m.exec(text)?.[1] ?? "888888",
      email = "verification@localhost",
      slug = "verification";
    const call = async (
      path: string,
      init: {
        method?: string;
        body?: unknown;
        token?: string;
        workspace?: string;
      },
    ) => {
      const response = await fetch(`${environment.apiOrigin}${path}`, {
        method: init.method ?? (init.body === undefined ? "GET" : "POST"),
        headers: {
          "Content-Type": "application/json",
          ...(init.token ? { Authorization: `Bearer ${init.token}` } : {}),
          ...(init.workspace ? { "X-Workspace-ID": init.workspace } : {}),
        },
        ...(init.body === undefined ? {} : { body: JSON.stringify(init.body) }),
        signal: AbortSignal.timeout(10000),
        redirect: "error",
      });
      return {
        ok: response.ok,
        value: (await response.json().catch(() => null)) as unknown,
      };
    };
    const signInFailed = () =>
      new VerificationError(
        problem(
          "dependencyMissing",
          "The test backend did not accept the development sign-in",
          "inspect",
        ),
      );
    if (!(await call("/auth/send-code", { body: { email } })).ok)
      throw signInFailed();
    const verified = await call("/auth/verify-code", { body: { email, code } });
    const token = z
      .object({ token: z.string().min(1) })
      .safeParse(verified.value);
    if (!verified.ok || !token.success) throw signInFailed();
    const jwt = token.data.token;
    const workspaceSchema = z.object({ id: z.string(), slug: z.string() });
    const created = await call("/api/workspaces", {
      body: { name: "Verification", slug },
      token: jwt,
    });
    const workspace = created.ok
      ? workspaceSchema.parse(created.value)
      : z
          .array(workspaceSchema.passthrough())
          .parse((await call("/api/workspaces", { token: jwt })).value)
          .find((item) => item.slug === slug);
    if (!workspace) throw signInFailed();
    // A new account is otherwise routed to onboarding instead of the workspace.
    await call("/api/me/onboarding/complete", {
      body: { exit: "existing" },
      token: jwt,
      workspace: workspace.id,
    });
    // The dev renderer can still reload (Vite re-optimizes dependencies on
    // first start) and drop a token delivered too early, so hand it off until
    // the application reports the signed-in user.
    for (let attempt = 0; attempt < 30; attempt++) {
      await this.desktopRequest(environment.environmentId, "/login", {
        token: jwt,
      });
      if (await this.signedInWithin(environment.environmentId, 5000)) break;
    }
    environment.components = {
      ...environment.components,
      signedIn: { email, workspaceSlug: slug },
    };
    await this.save(environment);
  }
  private async signedInWithin(id: string, timeoutMs: number) {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      await new Promise((resolve) => setTimeout(resolve, 500));
      const status = z
        .object({ userId: z.string().nullable() })
        .passthrough()
        .parse(await this.desktopRequest(id, "/capabilities"));
      if (status.userId) return true;
    }
    return false;
  }
  /** Keeps the original failure in the environment's private log; tool results stay generic. */
  private async recordFailure(
    id: string,
    log: "prepare" | "desktop",
    error: unknown,
  ) {
    const detail =
      error instanceof Error ? (error.stack ?? error.message) : String(error);
    await appendFile(
      join(this.directory(id), `${log}.log`),
      `\n[verification host] ${detail}\n`,
      { mode: 0o600 },
    ).catch(() => undefined);
  }
  /**
   * The managed environment script keeps its registry, profiles and scratch
   * space inside this environment, so its garbage collection never sees the
   * user's other development environments.
   */
  private managedEnvironment(id: string): Record<string, string> {
    const root = join(this.directory(id), "managed");
    return {
      MULTICA_DEV_HOME: root,
      MULTICA_DEV_PROFILES_HOME: join(root, "profiles"),
      MULTICA_DEV_TMPDIR: join(root, "tmp"),
      MULTICA_DEV_WORKSPACES_PARENT: root,
      MULTICA_DEV_DESKTOP_APP_DATA: join(root, "app-data"),
      MULTICA_DEV_POSTGRES_CONTAINER: databaseContainer(id),
    };
  }
  /** Starts this environment's own PostgreSQL on a free port and points the worktree at it. */
  private async startDatabase(
    id: string,
    checkout: string,
    env: NodeJS.ProcessEnv,
    signal: AbortSignal,
  ) {
    const file = join(checkout, ".env.worktree"),
      text = await readFile(file, "utf8"),
      value = (key: string) =>
        new RegExp(`^${key}=(.*)$`, "m").exec(text)?.[1]?.trim(),
      user = value("POSTGRES_USER") || "multica",
      password = value("POSTGRES_PASSWORD") || "multica",
      databaseUrl = value("DATABASE_URL"),
      port = await freePort(),
      container = databaseContainer(id);
    if (!databaseUrl)
      throw new VerificationError(
        problem(
          "identityMismatch",
          "The managed environment has no DATABASE_URL",
        ),
      );
    const url = new URL(databaseUrl);
    url.hostname = "127.0.0.1";
    url.port = String(port);
    await writeFile(
      file,
      text
        .replace(/^POSTGRES_PORT=.*$/m, `POSTGRES_PORT=${port}`)
        .replace(/^DATABASE_URL=.*$/m, `DATABASE_URL=${url.toString()}`),
      { mode: 0o600 },
    );
    const log = join(this.directory(id), "backend.log");
    await removeContainer(container, { cwd: checkout, env, log, signal });
    await run(
      "docker",
      [
        "run",
        "--detach",
        "--name",
        container,
        "--label",
        `multica.verification.environment=${id}`,
        "--env",
        `POSTGRES_USER=${user}`,
        "--env",
        `POSTGRES_PASSWORD=${password}`,
        "--env",
        "POSTGRES_DB=postgres",
        "--publish",
        `127.0.0.1:${port}:5432`,
        "pgvector/pgvector:pg17",
      ],
      { cwd: checkout, env, log, signal },
    );
    return { container, port };
  }
  /** Stops the environment if the installed Multica's runtime files change while it runs. */
  private guardProductionRuntime(id: string, expected: string) {
    this.unguard(id);
    let checking = false;
    const timer = setInterval(() => {
      if (checking) return;
      checking = true;
      void productionRuntimeDigest(homedir())
        .then(async (current) => {
          if (current === expected) return;
          this.unguard(id);
          await this.serializeLifecycle(() => this.stop(id)).catch(
            () => undefined,
          );
          const stopped = await this.get(id);
          await this.save({
            ...stopped,
            problem: problem(
              "identityMismatch",
              "The installed Multica runtime files changed while this environment ran; the environment was stopped",
              "inspect",
            ),
          });
        })
        .catch(() => undefined)
        .finally(() => {
          checking = false;
        });
    }, 1000);
    timer.unref();
    this.guards.set(id, timer);
  }
  private unguard(id: string) {
    const timer = this.guards.get(id);
    if (timer) clearInterval(timer);
    this.guards.delete(id);
  }
  /**
   * Stops every environment this host owns and removes its worktree and
   * branch. Returns false when any cleanup is unconfirmed, so the host
   * directory is kept for the next host to retry.
   */
  async close(options: { keepEnvironments?: boolean } = {}): Promise<boolean> {
    for (const id of [...this.guards.keys()]) this.unguard(id);
    if (options.keepEnvironments) return false;
    let clean = true;
    for (const name of await this.storage.list("environments")) {
      const environment = await this.storage.read(
        `environments/${name}`,
        environmentSchema,
      );
      if (!environment) continue;
      const id = environment.environmentId;
      if (environment.state !== "stopped") {
        const stopped = await this.serializeLifecycle(() =>
          this.stop(id),
        ).catch(() => null);
        if (stopped?.state !== "stopped") {
          clean = false;
          continue;
        }
      }
      if (!(await this.removeCheckout(environment))) clean = false;
    }
    return clean;
  }
  /** Removes a stopped environment's worktree and branch from the mirror. */
  private async removeCheckout(environment: Environment): Promise<boolean> {
    let removed = true;
    try {
      await lstat(environment.checkout);
      await git(this.repository(), [
        "worktree",
        "remove",
        "--force",
        environment.checkout,
      ]);
    } catch (error) {
      if (!isMissing(error)) removed = false;
    }
    await git(this.repository(), [
      "branch",
      "-D",
      `codex/verification-${environment.environmentId.slice(0, 8)}`,
    ]).catch(() => undefined);
    return removed;
  }
  private serializeLifecycle<T>(run: () => Promise<T>): Promise<T> {
    const next = this.lifecycle.catch(() => undefined).then(run);
    this.lifecycle = next;
    return next;
  }
}

function databaseContainer(id: string) {
  return `multica-verification-${id}`;
}

/** Removes a container by name; a container that does not exist counts as removed. */
async function removeContainer(
  name: string,
  options: Parameters<typeof run>[2],
): Promise<void> {
  try {
    await run("docker", ["rm", "--force", name], options);
  } catch (error) {
    const exists = await run("docker", ["container", "inspect", name], options)
      .then(() => true)
      .catch(() => false);
    if (exists) throw error;
  }
}

async function processGone(pid: number): Promise<boolean> {
  try {
    process.kill(pid, 0);
    return false;
  } catch (error) {
    return error instanceof Error && "code" in error && error.code === "ESRCH";
  }
}

function processCommand(pid: number): Promise<string> {
  return new Promise((resolve) =>
    execFile("ps", ["-p", String(pid), "-o", "command="], (error, stdout) =>
      resolve(error ? "" : stdout.trim()),
    ),
  );
}

/** A process's working directory, used to confirm it belongs to an environment checkout. */
function processDirectory(pid: number): Promise<string> {
  return new Promise((resolve) =>
    execFile(
      "lsof",
      ["-a", "-p", String(pid), "-d", "cwd", "-Fn"],
      (error, stdout) =>
        resolve(
          error
            ? ""
            : (stdout
                .split("\n")
                .find((line) => line.startsWith("n"))
                ?.slice(1) ?? ""),
        ),
    ),
  );
}
