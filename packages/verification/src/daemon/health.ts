import { execFile } from "node:child_process";
import { readFile, statfs } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";
import type { Environment } from "../contracts/resources.js";
import type { VerificationHost } from "../host/host.js";
import { slotStates } from "../setup/environments.js";

type Probe = { status: "up" | "down"; latencyMs?: number; detail?: string };
export type HealthSample = {
  at: string;
  status: "healthy" | "degraded" | "down";
  components: Record<string, Probe>;
};
export type DeviceHealth = {
  at: string;
  diskFreeBytes: number;
  docker: Probe;
  installedMultica: { running: boolean; pid: number | null };
  capacity: { running: number; max: number };
};

const activeStates = new Set(["starting", "awaitingUser", "ready"]);
const historyLength = 90;

/**
 * Samples device and environment health for the console and reclaims
 * environments nobody has used within the idle timeout.
 */
export class HealthMonitor {
  private readonly history = new Map<string, HealthSample[]>();
  private device: DeviceHealth | null = null;
  private timer: ReturnType<typeof setInterval> | undefined;
  private running: Promise<void> | undefined;

  constructor(
    private readonly host: VerificationHost,
    private readonly options: {
      stateRoot: string;
      intervalMs: number;
      maxEnvironments: number;
    },
  ) {}

  start(): void {
    const run = () => {
      this.running ??= this.tick().finally(() => {
        this.running = undefined;
      });
    };
    run();
    this.timer = setInterval(run, this.options.intervalMs);
    this.timer.unref();
  }

  async stop(): Promise<void> {
    if (this.timer) clearInterval(this.timer);
    await this.running?.catch(() => undefined);
  }

  async snapshot() {
    const environments = await this.host.environments.list();
    return {
      device: this.device,
      environments: environments
        .sort((a, b) => (b.startedAt ?? "").localeCompare(a.startedAt ?? ""))
        .map((environment) => {
          const last = this.host.environments.lastActivity(
            environment.environmentId,
          );
          return {
            environment: summarize(environment),
            lastActivityAt: last ? new Date(last).toISOString() : null,
            idleRemainingMs:
              last && activeStates.has(environment.state)
                ? Math.max(0, last + idleMs(environment) - Date.now())
                : null,
            health: this.history.get(environment.environmentId) ?? [],
          };
        }),
    };
  }

  private async tick(): Promise<void> {
    const environments = await this.host.environments.list();
    this.device = await this.sampleDevice(
      environments.filter((item) => slotStates.has(item.state)).length,
    );
    for (const environment of environments) {
      const id = environment.environmentId;
      if (!activeStates.has(environment.state)) continue;
      const sample = await this.sampleEnvironment(environment);
      const samples = this.history.get(id) ?? [];
      samples.push(sample);
      this.history.set(id, samples.slice(-historyLength));
      const last = this.host.environments.lastActivity(id);
      if (
        environment.state !== "starting" &&
        last !== undefined &&
        Date.now() - last > idleMs(environment)
      )
        await this.host.jobs
          .submit("env.stop", { environmentId: id }, `idle-stop-${id}`)
          .catch(() => undefined);
    }
  }

  private async sampleDevice(running: number): Promise<DeviceHealth> {
    const disk = await statfs(this.options.stateRoot);
    let installedPid: number | null = null;
    try {
      installedPid = Number(
        (
          await readFile(
            join(homedir(), ".multica/desktop-runtime/daemon.pid"),
            "utf8",
          )
        ).trim(),
      );
    } catch {
      /* The installed Multica has no running daemon record. */
    }
    return {
      at: new Date().toISOString(),
      diskFreeBytes: disk.bavail * disk.bsize,
      docker: await timed(() =>
        command("docker", ["info", "--format", "{{.ServerVersion}}"]).then(
          (version) => `Docker ${version}`,
        ),
      ),
      installedMultica: {
        running: !!installedPid && alive(installedPid),
        pid: installedPid || null,
      },
      capacity: { running, max: this.options.maxEnvironments },
    };
  }

  private async sampleEnvironment(
    environment: Environment,
  ): Promise<HealthSample> {
    const components: Record<string, Probe> = {};
    const started = Date.now();
    try {
      const status = await this.host.environments.status(
        environment.environmentId,
      );
      const observed = (status.components.observed ?? {}) as Record<
        string,
        { status?: string; problem?: { message?: string } }
      >;
      for (const [name, value] of Object.entries(observed))
        components[name] = {
          status: value.status === "available" ? "up" : "down",
          ...(value.problem?.message ? { detail: value.problem.message } : {}),
        };
      components.status = { status: "up", latencyMs: Date.now() - started };
    } catch (error) {
      components.status = {
        status: "down",
        detail: error instanceof Error ? error.message : String(error),
      };
    }
    const database = environment.components.database as
      { container?: string } | undefined;
    if (database?.container)
      components.database = await timed(() =>
        command("docker", [
          "inspect",
          "--format",
          "{{.State.Running}}",
          database.container!,
        ]).then((running) => {
          if (running !== "true") throw new Error("Container is not running");
          return database.container!;
        }),
      );
    const values = Object.values(components);
    return {
      at: new Date().toISOString(),
      status: values.every((item) => item.status === "up")
        ? "healthy"
        : values.some((item) => item.status === "up")
          ? "degraded"
          : "down",
      components,
    };
  }
}

function idleMs(environment: Environment): number {
  return Number(environment.components.idleTimeoutMinutes ?? 120) * 60_000;
}

function summarize(environment: Environment) {
  const components = environment.components;
  return {
    environmentId: environment.environmentId,
    state: environment.state,
    mode: environment.mode,
    dataMode: environment.dataMode,
    commit: environment.commit,
    apiOrigin: environment.apiOrigin,
    startedAt: environment.startedAt ?? null,
    readiness: components.readiness ?? null,
    healthPort: components.healthPort ?? null,
    database: components.database ?? null,
    signedIn: components.signedIn ?? null,
    branch: components.branch ?? null,
    owner: components.owner ?? null,
    idleTimeoutMinutes: components.idleTimeoutMinutes ?? null,
    problem: environment.problem ?? null,
  };
}

async function timed(run: () => Promise<string>): Promise<Probe> {
  const started = Date.now();
  try {
    const detail = await run();
    return { status: "up", latencyMs: Date.now() - started, detail };
  } catch (error) {
    return {
      status: "down",
      detail: error instanceof Error ? error.message : String(error),
    };
  }
}

function command(executable: string, args: string[]): Promise<string> {
  return new Promise((resolve, reject) =>
    execFile(executable, args, { timeout: 5000 }, (error, stdout) =>
      error
        ? reject(new Error(`${executable} is unavailable`))
        : resolve(stdout.trim()),
    ),
  );
}

function alive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}
