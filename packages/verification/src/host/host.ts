import { join } from "node:path";
import { homedir } from "node:os";
import { realpath } from "node:fs/promises";
import { HostStorage, digest } from "./storage.js";
import { JobService } from "./jobs.js";
import { SetupInspector, type Probe } from "../setup/inspect.js";
import { VerificationRecords } from "../evidence/records.js";
import { Bindings } from "../sources/bindings.js";
import { Environments } from "../setup/environments.js";
import { EvidenceStore } from "../evidence/store.js";
import { Sources } from "../sources/service.js";
import { Pages } from "../page/service.js";
import { Checks } from "../checks/service.js";
import { DataService } from "../data/service.js";
import { registerOperations } from "./operations.js";
import type { SourceBinding } from "../contracts/resources.js";

export interface HostOptions {
  projectRoot: string;
  stateRoot?: string;
  recoverLock?: boolean;
  probe?: Probe;
  bindings?: SourceBinding[];
  attachmentRoots?: string[];
  userWaiverRefs?: string[];
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
  try {
    const jobs = await JobService.open(storage);
    const records = new VerificationRecords(storage, jobs);
    const bindings = new Bindings(options.bindings);
    const environments = new Environments(storage, jobs, bindings);
    const evidence = new EvidenceStore(
      storage,
      records,
      options.attachmentRoots,
      new Set(options.userWaiverRefs),
      (options.bindings ?? []).flatMap((binding) => [
        binding.tokenFile,
        ...(binding.databaseFile ? [binding.databaseFile] : []),
      ]),
    );
    const sources = new Sources(storage, environments, evidence, records);
    await sources.recover();
    const pages = new Pages(environments, evidence, sources);
    const checks = new Checks(storage, sources, pages, evidence);
    const data = new DataService(storage, environments);
    registerOperations(jobs, { data, sources, evidence, checks, pages });
    let closing: Promise<void> | undefined;
    return {
      projectRef: storage.projectRef,
      setup: new SetupInspector(projectRoot, options.probe),
      jobs,
      records,
      environments,
      data,
      sources,
      checks,
      evidence,
      pages,
      close(): Promise<void> {
        closing ??= (async () => {
          await jobs.close();
          await sources.close();
          await pages.close();
          await environments.close();
          await storage.close();
        })();
        return closing;
      },
    };
  } catch (error) {
    await storage.close();
    throw error;
  }
}
export type VerificationHost = Awaited<
  ReturnType<typeof createVerificationHost>
>;
