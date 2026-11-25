/**
 * CPU and memory of the Desktop app, sampled from outside the app (MUL-9612).
 *
 * `ps` reports every process's cumulative CPU time and resident set size; the
 * app is the Electron main process and its descendants. CPU % is the CPU time
 * a role used between two snapshots over the wall time between them, so 100%
 * is one core busy. Nothing runs inside the app to measure it.
 *
 * Electron tags its helpers with `--type=`: renderer, gpu-process, utility
 * (network, storage, audio). Anything else under the main process — the
 * local daemon Desktop starts and the agent CLIs under it — is `other` and is
 * kept out of the app's total.
 */
import { execFileSync } from "node:child_process";

export const ROLES = ["main", "renderer", "gpu", "utility", "other"] as const;
export type Role = (typeof ROLES)[number];
/** Roles that make up the app itself. */
export const APP_ROLES: Role[] = ["main", "renderer", "gpu", "utility"];

export interface ProcessSample {
  pid: number;
  role: Role;
  cpu_s: number;
  rss_mb: number;
}

export interface TreeSnapshot {
  /** Milliseconds since the epoch. */
  at: number;
  processes: ProcessSample[];
}

/** `ps` CPU time: `[[dd-]hh:]mm:ss[.ff]` (Linux) or `mm:ss.ff` (macOS), in seconds. */
export function parseCpuTime(value: string): number {
  const [days, clock] = value.includes("-") ? value.split("-") as [string, string] : ["0", value];
  const seconds = clock.split(":").reduce((sum, part) => sum * 60 + Number(part), 0);
  return Number(days) * 86_400 + seconds;
}

function roleOf(pid: number, root: number, command: string): Role {
  if (pid === root) return "main";
  const type = command.match(/--type=([\w-]+)/)?.[1];
  if (type === "renderer") return "renderer";
  if (type === "gpu-process") return "gpu";
  if (type === "utility" || type === "zygote") return "utility";
  return "other";
}

/** The process tree under `root`, from one `ps` call. */
export function snapshotTree(root: number): TreeSnapshot {
  const out = execFileSync("ps", ["-A", "-o", "pid=,ppid=,time=,rss=,command="], { encoding: "utf8", maxBuffer: 32 << 20 });
  const at = Date.now();
  const rows = out.split("\n").flatMap((line) => {
    const m = line.trim().match(/^(\d+)\s+(\d+)\s+(\S+)\s+(\d+)\s+(.*)$/);
    return m ? [{ pid: Number(m[1]), ppid: Number(m[2]), time: m[3]!, rss: Number(m[4]), command: m[5]! }] : [];
  });
  const children = new Map<number, typeof rows>();
  for (const row of rows) children.set(row.ppid, [...(children.get(row.ppid) ?? []), row]);
  const self = rows.find((row) => row.pid === root);
  if (!self) throw new Error(`process ${root} is not running`);
  const tree = [self];
  for (let i = 0; i < tree.length; i++) tree.push(...(children.get(tree[i]!.pid) ?? []));
  return {
    at,
    processes: tree.map((row) => ({
      pid: row.pid,
      role: roleOf(row.pid, root, row.command),
      cpu_s: parseCpuTime(row.time),
      rss_mb: row.rss / 1024,
    })),
  };
}

export type ByRole = Record<Role, number>;
const zero = (): ByRole => ({ main: 0, renderer: 0, gpu: 0, utility: 0, other: 0 });

export function rssByRole(snapshot: TreeSnapshot): ByRole & { app: number } {
  const sum = zero();
  for (const p of snapshot.processes) sum[p.role] += p.rss_mb;
  return { ...sum, app: APP_ROLES.reduce((total, role) => total + sum[role], 0) };
}

/**
 * CPU % per role between two snapshots. A process that exists only in the
 * later one counts from zero; one that exited is dropped, so a crash cannot
 * produce negative CPU.
 */
export function cpuByRole(before: TreeSnapshot, after: TreeSnapshot): ByRole & { app: number } {
  const seconds = (after.at - before.at) / 1000;
  const prior = new Map(before.processes.map((p) => [p.pid, p.cpu_s]));
  const used = zero();
  for (const p of after.processes) used[p.role] += Math.max(0, p.cpu_s - (prior.get(p.pid) ?? 0));
  const pct = zero();
  for (const role of ROLES) pct[role] = (used[role] / seconds) * 100;
  return { ...pct, app: APP_ROLES.reduce((total, role) => total + pct[role], 0) };
}

/** Least-squares slope of `values` against `minutes`, per minute. */
export function slopePerMinute(minutes: number[], values: number[]): number {
  const n = minutes.length;
  if (n < 2) return NaN;
  const mx = minutes.reduce((a, b) => a + b, 0) / n;
  const my = values.reduce((a, b) => a + b, 0) / n;
  let num = 0;
  let den = 0;
  for (let i = 0; i < n; i++) {
    num += (minutes[i]! - mx) * (values[i]! - my);
    den += (minutes[i]! - mx) ** 2;
  }
  return num / den;
}

/** The Electron main process that serves CDP on `cdpUrl`'s port (the browser process owns that listener). */
export function cdpOwnerPid(cdpUrl: string): number {
  const port = new URL(cdpUrl).port;
  const out = execFileSync("lsof", ["-nP", `-iTCP:${port}`, "-sTCP:LISTEN", "-t"], { encoding: "utf8" }).trim();
  const pid = Number(out.split("\n")[0]);
  if (!pid) throw new Error(`nothing listens on CDP port ${port}`);
  return pid;
}
