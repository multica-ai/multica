import { join } from "node:path";
import { homedir, tmpdir } from "node:os";
import { mkdtemp, realpath, rm } from "node:fs/promises";
import { HostStorage, digest } from "./storage.js";
import { JobService } from "./jobs.js";
import { SetupInspector, type Probe } from "../setup/inspect.js";
import { Bindings } from "../sources/bindings.js";
import { DeviceLease } from "../setup/device-lease.js";
import { Environments } from "../setup/environments.js";
import { State } from "../state/service.js";
import { Pages } from "../page/service.js";
import { DataService } from "../data/service.js";
import { registerOperations } from "./operations.js";
import type { SourceBinding } from "../contracts/resources.js";

export interface HostOptions {
  projectRoot: string;
  stateRoot?: string;
  deviceLeaseRoot?: string;
  recoverLock?: boolean;
  probe?: Probe;
  bindings?: SourceBinding[];
}

export async function createVerificationHost(options: HostOptions) {
  const projectRoot = await realpath(options.projectRoot);
  const storage = await HostStorage.open({
    ...options,
    projectRoot,
    stateRoot:
      options.stateRoot ??
      join(homedir(), ".multica", "verification", digest(projectRoot)),
  });
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
    const environments = new Environments(
      storage,
      jobs,
      bindings,
      new DeviceLease(
        options.deviceLeaseRoot ??
          join(homedir(), ".multica", "verification-device.lock"),
        storage.root,
      ),
    );
    await environments.recover();
    const state = new State(environments);
    const pages = new Pages(environments);
    const data = new DataService(snapshotRoot, environments);
    registerOperations(jobs, data);
    let closing: Promise<void> | undefined;
    return {
      projectRef: storage.projectRef,
      setup: new SetupInspector(projectRoot, options.probe),
      jobs,
      environments,
      data,
      state,
      pages,
      close(): Promise<void> {
        closing ??= (async () => {
          await jobs.close();
          await pages.close();
          await environments.close();
          await removeSnapshots();
          await storage.close();
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
export type VerificationHost = Awaited<
  ReturnType<typeof createVerificationHost>
>;
