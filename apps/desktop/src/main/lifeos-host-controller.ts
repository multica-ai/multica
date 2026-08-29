import { execFile } from "node:child_process";
import { access } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import type { LifeOSHostStatus } from "../shared/lifeos-host";

const execFileAsync = promisify(execFile);
const READY_URL = "http://127.0.0.1:8080/readyz";
const ENSURE_TIMEOUT_MS = 120_000;

export interface LifeOSHostControllerOptions {
  fetchImpl?: typeof fetch;
  accessImpl?: typeof access;
  execFileImpl?: (
    file: string,
    args: string[],
    options: {
      timeout: number;
      maxBuffer: number;
      env: NodeJS.ProcessEnv;
    },
  ) => Promise<unknown>;
  homeDirectory?: string;
  now?: () => number;
}

export class LifeOSHostController {
  private readonly fetchImpl: typeof fetch;
  private readonly accessImpl: typeof access;
  private readonly execFileImpl: NonNullable<
    LifeOSHostControllerOptions["execFileImpl"]
  >;
  private readonly homeDirectory: string;
  private readonly now: () => number;
  private status: LifeOSHostStatus;
  private ensurePromise: Promise<LifeOSHostStatus> | null = null;
  private readonly listeners = new Set<(status: LifeOSHostStatus) => void>();

  constructor(options: LifeOSHostControllerOptions = {}) {
    this.fetchImpl = options.fetchImpl ?? fetch;
    this.accessImpl = options.accessImpl ?? access;
    this.execFileImpl =
      options.execFileImpl ??
      ((file, args, execOptions) => execFileAsync(file, args, execOptions));
    this.homeDirectory = options.homeDirectory ?? homedir();
    this.now = options.now ?? Date.now;
    this.status = { state: "offline", checkedAt: this.now() };
  }

  getStatus(): LifeOSHostStatus {
    return { ...this.status };
  }

  subscribe(listener: (status: LifeOSHostStatus) => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  async probe(): Promise<LifeOSHostStatus> {
    try {
      const response = await this.fetchImpl(READY_URL, {
        signal: AbortSignal.timeout(3_000),
      });
      if (!response.ok) return this.update("offline");
      const body = (await response.json()) as Record<string, unknown>;
      if (body.status !== "ok") return this.update("offline");
      return this.update("ready");
    } catch {
      return this.update("offline");
    }
  }

  ensure(): Promise<LifeOSHostStatus> {
    if (this.ensurePromise) return this.ensurePromise;
    this.ensurePromise = this.runEnsure().finally(() => {
      this.ensurePromise = null;
    });
    return this.ensurePromise;
  }

  private async runEnsure(): Promise<LifeOSHostStatus> {
    this.update("starting");
    const script = join(
      this.homeDirectory,
      "code",
      "lifeos-workbench",
      "scripts",
      "lifeos_workbench.py",
    );
    try {
      await this.accessImpl(script);
    } catch {
      return this.update(
        "error",
        "找不到 LifeOS 本机启动器，请确认工作台代码仍位于 ~/code/lifeos-workbench。",
      );
    }

    try {
      await this.execFileImpl("python3", [script, "ensure"], {
        timeout: ENSURE_TIMEOUT_MS,
        maxBuffer: 1024 * 1024,
        env: process.env,
      });
    } catch {
      return this.update(
        "error",
        "LifeOS 本机服务恢复失败，请打开运行日志查看原因后重试。",
      );
    }

    const probed = await this.probe();
    if (probed.state === "ready") return probed;
    return this.update(
      "error",
      "LifeOS 后端尚未就绪，请稍后重试或打开运行日志。",
    );
  }

  private update(
    state: LifeOSHostStatus["state"],
    message?: string,
  ): LifeOSHostStatus {
    this.status = {
      state,
      checkedAt: this.now(),
      ...(message ? { message } : {}),
    };
    const snapshot = this.getStatus();
    for (const listener of this.listeners) listener(snapshot);
    return snapshot;
  }
}
