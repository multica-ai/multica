#!/usr/bin/env node
import { execFile } from "node:child_process";
import { mkdir, rm, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";
import { createVerificationHost } from "../host/host.js";
import { toProblem } from "../host/problem.js";
import { readBindings } from "../sources/bindings.js";
import { HealthMonitor } from "./health.js";
import { serveDaemon } from "./server.js";

export const defaultPort = 47311;

function argumentsFrom(argv: string[]) {
  const values = new Map<string, string>();
  for (let i = 0; i < argv.length; i += 2) {
    const flag = argv[i]!,
      value = argv[i + 1];
    if (
      ![
        "--project",
        "--port",
        "--state-root",
        "--bindings-file",
        "--repository",
        "--max-environments",
        "--idle-minutes",
      ].includes(flag) ||
      !value ||
      value.startsWith("--") ||
      values.has(flag)
    )
      throw new Error("Invalid arguments");
    values.set(flag, value);
  }
  const projectRoot = values.get("--project");
  if (!projectRoot) throw new Error("--project is required");
  const port = Number(values.get("--port") ?? defaultPort),
    idleMinutes = Number(values.get("--idle-minutes") ?? 120),
    maxEnvironments = Number(values.get("--max-environments") ?? 2);
  if (!Number.isInteger(port) || port < 1024 || port > 65535)
    throw new Error("Invalid port");
  if (!Number.isInteger(idleMinutes) || idleMinutes < 1)
    throw new Error("Invalid idle timeout");
  if (!Number.isInteger(maxEnvironments) || maxEnvironments < 1)
    throw new Error("Invalid environment limit");
  return {
    projectRoot,
    port,
    idleMinutes,
    maxEnvironments,
    repositoryUrl: values.get("--repository"),
    stateRoot: values.get("--state-root"),
    bindingsFile: values.get("--bindings-file"),
  };
}

function headCommit(projectRoot: string): Promise<string> {
  return new Promise((resolve) =>
    execFile("git", ["rev-parse", "HEAD"], { cwd: projectRoot }, (error, out) =>
      resolve(error ? "unknown" : out.trim()),
    ),
  );
}

async function main() {
  let options: ReturnType<typeof argumentsFrom>;
  try {
    options = argumentsFrom(process.argv.slice(2));
  } catch {
    process.stderr.write(
      "usage: multica-verification-daemon --project <checkout> [--port <port>] [--repository <url>] [--max-environments <n>] [--idle-minutes <n>] [--state-root <dir>] [--bindings-file <private-json>]\n",
    );
    process.exitCode = 2;
    return;
  }
  const daemonDirectory = join(homedir(), ".multica", "verification", "daemon");
  await mkdir(daemonDirectory, { recursive: true, mode: 0o700 });
  const host = await createVerificationHost({
    projectRoot: options.projectRoot,
    hostId: "daemon",
    maxEnvironments: options.maxEnvironments,
    defaultIdleMinutes: options.idleMinutes,
    ...(options.repositoryUrl ? { repositoryUrl: options.repositoryUrl } : {}),
    ...(options.stateRoot ? { stateRoot: options.stateRoot } : {}),
    ...(options.bindingsFile
      ? { bindings: await readBindings(options.bindingsFile) }
      : {}),
  });
  const monitor = new HealthMonitor(host, {
    stateRoot: options.stateRoot ?? join(homedir(), ".multica", "verification"),
    intervalMs: 10_000,
    maxEnvironments: options.maxEnvironments,
  });
  let daemon: Awaited<ReturnType<typeof serveDaemon>>;
  try {
    daemon = await serveDaemon({
      host,
      monitor,
      port: options.port,
      info: {
        pid: process.pid,
        startedAt: new Date().toISOString(),
        commit: await headCommit(options.projectRoot),
      },
    });
  } catch (error) {
    await host.close({ keepEnvironments: true });
    throw error;
  }
  monitor.start();
  const record = join(daemonDirectory, "daemon.json");
  await writeFile(record, JSON.stringify(daemon.info), { mode: 0o600 });
  process.stderr.write(`Verification daemon ready at ${daemon.info.mcpUrl}\n`);
  let stopping: Promise<void> | undefined;
  const stop = () => {
    stopping ??= (async () => {
      await monitor.stop();
      await daemon.close();
      // Environments survive a daemon restart; the next start adopts them.
      await host.close({ keepEnvironments: true });
      await rm(record, { force: true });
    })().catch(() => {
      process.exitCode = 1;
    });
    return stopping;
  };
  process.once("SIGINT", stop);
  process.once("SIGTERM", stop);
}

void main().catch((error: unknown) => {
  const failure = toProblem(error);
  process.stderr.write(`${failure.code}: ${failure.message}\n`);
  process.exitCode = 1;
});
