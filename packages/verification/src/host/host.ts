import { execFile } from "node:child_process";
import { randomUUID } from "node:crypto";
import { join } from "node:path";
import { homedir, tmpdir } from "node:os";
import {
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  realpath,
  rm,
  stat,
} from "node:fs/promises";
import { HostStorage, digest } from "./storage.js";
import { JobService } from "./jobs.js";
import { SetupInspector, type Probe } from "../setup/inspect.js";
import { Bindings } from "../sources/bindings.js";
import { Environments } from "../setup/environments.js";
import { State } from "../state/service.js";
import { Pages } from "../page/service.js";
import { DataService } from "../data/service.js";
import { registerOperations } from "./operations.js";
import type { SourceBinding } from "../contracts/resources.js";

export const defaultMaxEnvironments = 10;

export interface HostOptions {
  projectRoot: string;
  /** Project-level directory; each host keeps its own state under `hosts/`. */
  stateRoot?: string;
  /** Remote branches are fetched from; defaults to the project checkout's origin. */
  repositoryUrl?: string;
  maxEnvironments?: number;
  defaultIdleMinutes?: number;
  /**
   * A stable host directory name. The resident daemon uses one so a restart
   * takes over its own state and keeps environments whose runtime survived.
   */
  hostId?: string;
  probe?: Probe;
  bindings?: SourceBinding[];
}

/**
 * Every host (one per MCP session) gets its own state directory, so any
 * number of agents can start hosts without coordinating. Before starting, and
 * again before taking the device lease, a host stops the environments of
 * hosts whose process is gone and removes their directories.
 */
export async function createVerificationHost(options: HostOptions) {
  const projectRoot = await realpath(options.projectRoot);
  const sharedRoot =
    options.stateRoot ??
    join(homedir(), ".multica", "verification", digest(projectRoot));
  await mkdir(join(sharedRoot, "hosts"), { recursive: true, mode: 0o700 });
  const hostRoot = join(sharedRoot, "hosts", options.hostId ?? randomUUID());
  await collectAbandonedHosts(options, projectRoot, sharedRoot, hostRoot);
  return openHost(options, projectRoot, sharedRoot, hostRoot, {
    adopt: !!options.hostId,
  });
}

async function openHost(
  options: HostOptions,
  projectRoot: string,
  sharedRoot: string,
  hostRoot: string,
  recovery: { adopt: boolean } = { adopt: false },
) {
  const storage = await HostStorage.open({ projectRoot, stateRoot: hostRoot });
  let snapshotRoot: string | undefined;
  const removeSnapshots = async () => {
    if (snapshotRoot) await rm(snapshotRoot, { recursive: true, force: true });
  };
  try {
    // Snapshots hold real business content; keep them only for this host session.
    snapshotRoot = await mkdtemp(
      join(tmpdir(), "multica-verification-snapshots-"),
    );
    const jobs = await JobService.open(storage);
    const bindings = new Bindings(options.bindings);
    const environments = new Environments(storage, jobs, bindings, {
      sharedRoot,
      repositoryUrl: options.repositoryUrl ?? (await originUrl(projectRoot)),
      maxEnvironments: options.maxEnvironments ?? defaultMaxEnvironments,
      defaultIdleMinutes: options.defaultIdleMinutes ?? 120,
      beforeStart: () =>
        collectAbandonedHosts(options, projectRoot, sharedRoot, hostRoot),
    });
    await environments.recover(recovery);
    const state = new State(environments);
    const pages = new Pages(environments);
    const data = new DataService(snapshotRoot, environments);
    registerOperations(jobs, data);
    let closing: Promise<void> | undefined;
    return {
      projectRef: storage.projectRef,
      /** This host's own state directory under `<stateRoot>/hosts/`. */
      stateDirectory: storage.root,
      setup: new SetupInspector(projectRoot, options.probe),
      jobs,
      environments,
      data,
      state,
      pages,
      /**
       * Stops the host. By default its environments are stopped and its
       * directory removed; `keepEnvironments` leaves them running for the
       * next process with the same `hostId`.
       */
      close(closeOptions: { keepEnvironments?: boolean } = {}): Promise<void> {
        closing ??= (async () => {
          await jobs.close();
          await pages.close();
          const clean = await environments.close(closeOptions);
          await removeSnapshots();
          await storage.close();
          if (clean) await rm(storage.root, { recursive: true, force: true });
        })();
        return closing;
      },
    };
  } catch (error) {
    await removeSnapshots();
    await storage.close();
    throw error;
  }
}

async function collectAbandonedHosts(
  options: HostOptions,
  projectRoot: string,
  sharedRoot: string,
  except?: string,
): Promise<void> {
  const hostsRoot = join(sharedRoot, "hosts");
  for (const name of await readdir(hostsRoot).catch(() => [])) {
    const hostRoot = join(hostsRoot, name);
    if (hostRoot === except || !(await abandoned(hostRoot))) continue;
    let host: Awaited<ReturnType<typeof openHost>>;
    try {
      // Taking over the lock fails when another host is collecting it, or its owner is alive.
      host = await openHost(options, projectRoot, sharedRoot, hostRoot);
    } catch {
      continue;
    }
    await host.close().catch(() => undefined);
  }
}

async function abandoned(hostRoot: string): Promise<boolean> {
  let pid: number;
  try {
    pid = (
      JSON.parse(
        await readFile(join(hostRoot, "host.lock", "owner.json"), "utf8"),
      ) as { pid: number }
    ).pid;
  } catch {
    // A directory without an owner is either being created or was left before
    // locking; only collect it once it is clearly stale.
    const info = await stat(hostRoot).catch(() => null);
    return !!info && Date.now() - info.mtimeMs > 10 * 60 * 1000;
  }
  try {
    process.kill(pid, 0);
    return false;
  } catch (error) {
    return error instanceof Error && "code" in error && error.code === "ESRCH";
  }
}

export type VerificationHost = Awaited<
  ReturnType<typeof createVerificationHost>
>;

function originUrl(projectRoot: string): Promise<string> {
  return new Promise((resolve) =>
    execFile(
      "git",
      ["remote", "get-url", "origin"],
      { cwd: projectRoot },
      (error, stdout) => resolve(error ? projectRoot : stdout.trim()),
    ),
  );
}
