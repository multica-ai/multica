import { execFile } from "node:child_process";
import { randomBytes, randomUUID, createHash } from "node:crypto";
import { createServer } from "node:net";
import {
  copyFile,
  lstat,
  mkdir,
  readFile,
  realpath,
  writeFile,
} from "node:fs/promises";
import { dirname, isAbsolute, join, relative } from "node:path";
import { homedir } from "node:os";
import { createRequire } from "node:module";
import { z } from "zod";
import { environmentSchema, type Environment } from "../contracts/resources.js";
import { jsonSchema, resourceIdSchema, toJson } from "../contracts/index.js";
import type { HostStorage } from "../host/storage.js";
import { redactLogText } from "../host/redact.js";
import { digest } from "../host/storage.js";
import type { JobService, OperationContext } from "../host/jobs.js";
import { problem, toProblem, VerificationError } from "../host/problem.js";
import { prepareToolchain } from "./toolchain.js";
import { launch, run, type RunningProcess } from "./process.js";
import type { Bindings } from "../sources/bindings.js";

export const prepareInput = z
  .object({
    gitRef: z.string().min(1).max(200),
    mode: z.enum(["renderer", "desktop"]),
    dataMode: z.enum(["liveReadOnly", "realSnapshot", "fixture"]),
    apiOrigin: z.url().optional(),
    sourceRef: z.string().optional(),
    includeWorkingChanges: z.boolean().default(false),
  })
  .strict();
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

export const git = (cwd: string, args: string[], raw = false) =>
  new Promise<string>((resolve, reject) =>
    execFile(
      "git",
      args,
      {
        cwd,
        maxBuffer: 32 * 1024 * 1024,
        timeout: 30_000,
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
  private readonly processes = new Map<string, RunningProcess[]>();
  private readonly tools = new Map<string, Tools>();
  private lifecycle: Promise<unknown> = Promise.resolve();
  constructor(
    private readonly storage: HostStorage,
    jobs: JobService,
    readonly bindings: Bindings,
  ) {
    jobs.register("setup.prepare", {
      input: prepareInput,
      output: jsonSchema,
      outputSchema: "environment/v1",
      run: (input, context) =>
        this.prepare(prepareInput.parse(input), context).then(toJson),
    });
    for (const operation of ["start", "stop"] as const)
      jobs.register(`setup.${operation}`, {
        input: z.object({ environmentId: resourceIdSchema }).strict(),
        output: jsonSchema,
        outputSchema: "environment/v1",
        run: async (input, context) => {
          const id = z
            .object({ environmentId: resourceIdSchema })
            .parse(input).environmentId;
          await context.addResource(id);
          return this.serializeLifecycle(async () => {
            context.signal.throwIfAborted();
            return toJson(await this[operation](id, context));
          });
        },
      });
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
      coverage: "unknown",
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
    const commit = await git(this.storage.projectRoot, [
      "rev-parse",
      "--verify",
      "--end-of-options",
      `${input.gitRef}^{commit}`,
    ]);
    if (!/^[a-f0-9]{40}$/.test(commit))
      throw new VerificationError(
        problem("invalidInput", "A full commit is required"),
      );
    const id = randomUUID();
    await context.addResource(id);
    const checkout = join(this.storage.root, "worktrees", id),
      directory = this.directory(id);
    await mkdir(directory, { mode: 0o700 });
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
      components: {},
    };
    if (input.dataMode === "liveReadOnly") {
      environment = {
        ...environment,
        checkout: this.storage.projectRoot,
        state: "prepared",
        components: {
          referenceOnly: true,
          sourceIdentity: toJson(source!.identity),
          applicationReady: "notApplicable",
        },
      };
      await this.save(environment);
      return environment;
    }
    await this.save(environment);
    try {
      await context.progress("checkout");
      await git(this.storage.projectRoot, [
        "worktree",
        "add",
        "-b",
        `codex/verification-${id.slice(0, 8)}`,
        checkout,
        commit,
      ]);
      let overlay = "";
      if (input.includeWorkingChanges) {
        if (
          commit !==
          (await git(this.storage.projectRoot, ["rev-parse", "HEAD"]))
        )
          throw new VerificationError(
            problem(
              "invalidInput",
              "Working changes can only overlay the current HEAD",
            ),
          );
        const patch = await git(
          this.storage.projectRoot,
          [
            "diff",
            "--binary",
            "HEAD",
            "--",
            ".",
            ":(exclude).env*",
            ":(exclude)**/.env*",
            ":(exclude)**/*.pem",
            ":(exclude)**/*.key",
            ":(exclude)**/*.token",
          ],
          true,
        );
        if (patch) {
          const path = join(directory, "source.patch");
          await writeFile(path, patch, { mode: 0o600 });
          await git(checkout, ["apply", path]);
        }
        overlay = patch;
        const untracked = (
          await git(
            this.storage.projectRoot,
            ["ls-files", "--others", "--exclude-standard", "-z"],
            true,
          )
        )
          .split("\0")
          .filter(Boolean)
          .sort();
        for (const path of untracked) {
          if (
            !/^(apps|packages|server|scripts|verification|docs)\//.test(path) ||
            path
              .split("/")
              .some(
                (part) => part.startsWith(".env") || part === "node_modules",
              )
          )
            continue;
          const from = join(this.storage.projectRoot, path),
            resolved = await realpath(from),
            rel = relative(this.storage.projectRoot, resolved);
          if (
            rel.startsWith("..") ||
            isAbsolute(rel) ||
            !(await lstat(from)).isFile()
          )
            throw new VerificationError(
              problem("forbidden", "A working source file escapes the project"),
            );
          const to = join(checkout, path);
          await mkdir(dirname(to), { recursive: true });
          await copyFile(from, to);
          overlay += `${path}:${createHash("sha256")
            .update(await readFile(from))
            .digest("hex")}\n`;
        }
      }
      try {
        await lstat(
          join(checkout, "apps/desktop/src/main/verification-dev/index.ts"),
        );
      } catch {
        throw new VerificationError(
          problem(
            "unsupportedVersion",
            "This source version predates the dev verification protocol; select an instrumented version or explicitly overlay the current implementation",
          ),
        );
      }
      const sourceDigest = digest(commit + overlay);
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
      await writeFile(
        join(directory, "session.json"),
        JSON.stringify(session),
        { mode: 0o600 },
      );
      await context.progress("toolchain");
      const tools = await prepareToolchain(
        checkout,
        join(this.storage.root, "tools"),
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
        for (const script of [
          "brand-dev-electron.mjs",
          "build-computer-use.mjs",
        ])
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
      environment = {
        ...environment,
        state: "prepared",
        components: {
          sourceDigest,
          buildKind: "development",
          profile: "desktop-runtime",
        },
      };
      await this.save(environment);
      return environment;
    } catch (error) {
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

  private async start(
    id: string,
    context: OperationContext,
  ): Promise<Environment> {
    await this.assertTaskBoundary();
    let environment = await this.get(id);
    if (
      environment.components.referenceOnly === true &&
      environment.sourceRef
    ) {
      const binding = this.bindings.get(environment.sourceRef);
      await this.bindings.request(
        binding.ref,
        binding.kind === "daemon"
          ? "/health"
          : `/api/workspaces/${encodeURIComponent(binding.scope.workspaceId)}`,
      );
      environment = {
        ...environment,
        state: "ready",
        components: {
          ...environment.components,
          readiness: "readOnlyReferenceSourceReachable",
        },
      };
      await this.save(environment);
      return environment;
    }
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
        other &&
        other.environmentId !== id &&
        other.components.referenceOnly !== true &&
        ["starting", "ready", "awaitingUser", "stopping"].includes(other.state)
      ) {
        throw new VerificationError(
          problem(
            "instanceBusy",
            "Stop the previous verification environment before starting another",
            "inspect",
            [other.environmentId],
          ),
        );
      }
      if (
        !other ||
        other.environmentId === id ||
        other.components.referenceOnly === true
      )
        continue;
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
    try {
      const registered = JSON.parse(
        await readFile(
          join(homedir(), ".multica/desktop-runtime/runtime.json"),
          "utf8",
        ),
      ) as { pid?: number };
      if (registered.pid) {
        try {
          process.kill(registered.pid, 0);
          throw new VerificationError(
            problem(
              "instanceBusy",
              "Another Desktop runtime is still present; use the dedicated test Mac",
              "userAction",
            ),
          );
        } catch (error) {
          if (
            error instanceof VerificationError ||
            !(
              error instanceof Error &&
              "code" in error &&
              error.code === "ESRCH"
            )
          )
            throw error;
        }
      }
    } catch (error) {
      if (
        !(error instanceof Error && "code" in error && error.code === "ENOENT")
      )
        throw error;
    }
    environment = { ...environment, state: "starting" };
    await this.save(environment);
    const directory = this.directory(id),
      tools =
        this.tools.get(id) ??
        (await prepareToolchain(
          environment.checkout,
          join(this.storage.root, "tools"),
          context.signal,
        ));
    this.tools.set(id, tools);
    const components: RunningProcess[] = [];
    this.processes.set(id, components);
    try {
      if (environment.dataMode !== "liveReadOnly")
        await run("make", ["up", "C=api,web"], {
          cwd: environment.checkout,
          env: tools.env,
          log: join(directory, "backend.log"),
          signal: context.signal,
        });
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
            ...tools.env,
            MULTICA_VERIFICATION_SESSION: join(directory, "session.json"),
            MULTICA_VERIFICATION_MODE: "1",
            MULTICA_VERIFICATION_HOME: join(directory, "runtime-home"),
            MULTICA_WORKSPACES_ROOT: join(directory, "runtime-home/workspaces"),
            DESKTOP_RENDERER_PORT: String(await freePort()),
            MULTICA_VERIFICATION_TOKEN: sessionSchema.parse(
              JSON.parse(
                await readFile(join(directory, "session.json"), "utf8"),
              ),
            ).token,
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
      },
    };
    await this.save(current);
    return current;
  }
  private async stop(
    id: string,
    _context?: OperationContext,
  ): Promise<Environment> {
    let environment = await this.get(id);
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
      for (let i = 0; i < 50; i++) {
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
    let applicationAbsent = false;
    try {
      const descriptor = await this.descriptor(id);
      await this.desktopRequest(id, "/shutdown", {});
      for (let attempt = 0; attempt < 50; attempt++) {
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
    this.processes.delete(id);
    if (!applicationAbsent) {
      try {
        const descriptor = await this.descriptor(id);
        process.kill(descriptor.pid, 0);
      } catch (error) {
        if (error instanceof Error && "code" in error && error.code === "ESRCH")
          applicationAbsent = true;
      }
    }
    if (daemonPid) {
      try {
        process.kill(daemonPid, 0);
        daemonAbsent = false;
      } catch (error) {
        daemonAbsent =
          error instanceof Error && "code" in error && error.code === "ESRCH";
      }
    }
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
  async close(): Promise<void> {
    for (const id of this.processes.keys())
      await this.serializeLifecycle(() => this.stop(id)).catch(() => undefined);
  }
  private serializeLifecycle<T>(run: () => Promise<T>): Promise<T> {
    const next = this.lifecycle.catch(() => undefined).then(run);
    this.lifecycle = next;
    return next;
  }
}
