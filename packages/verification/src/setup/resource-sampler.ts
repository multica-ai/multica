import { execFile } from "node:child_process";
import { promisify } from "node:util";
const execute = promisify(execFile);
export function ownedRss(
  rows: { pid: number; ppid: number; rss: number }[],
  roots: ReadonlySet<number>,
) {
  const owned = new Set(roots);
  let changed = true;
  while (changed) {
    changed = false;
    for (const row of rows)
      if (owned.has(row.ppid) && !owned.has(row.pid)) {
        owned.add(row.pid);
        changed = true;
      }
  }
  return rows
    .filter((row) => owned.has(row.pid))
    .reduce((total, row) => total + row.rss, 0);
}
export class ResourceSampler {
  private timer: ReturnType<typeof setInterval> | undefined;
  private busy: Promise<void> = Promise.resolve();
  private sampling = false;
  private stopped = false;
  private roots = new Set([process.pid]);
  private samples = 0;
  private errors = 0;
  private peakRssKiB = 0;
  private peakDiskKiB = 0;
  constructor(private readonly stateRoot: string) {}
  addRoot(pid: number) {
    if (Number.isInteger(pid) && pid > 0) this.roots.add(pid);
  }
  start() {
    this.timer = setInterval(() => {
      if (!this.sampling) {
        this.sampling = true;
        this.busy = this.sample().finally(() => {
          this.sampling = false;
        });
      }
    }, 1000);
    this.timer.unref();
  }
  private async sample() {
    try {
      const [processes, disk] = await Promise.all([
        execute("ps", ["-axo", "pid=,ppid=,rss="], {
          timeout: 3000,
          maxBuffer: 4 * 1024 * 1024,
        }),
        execute("du", ["-sk", this.stateRoot], {
          timeout: 10000,
          maxBuffer: 16384,
        }),
      ]);
      const rows = processes.stdout
        .trim()
        .split("\n")
        .map((line) => line.trim().split(/\s+/).map(Number))
        .filter((row) => row.length === 3 && row.every(Number.isFinite))
        .map((row) => ({ pid: row[0]!, ppid: row[1]!, rss: row[2]! }));
      this.peakRssKiB = Math.max(this.peakRssKiB, ownedRss(rows, this.roots));
      this.peakDiskKiB = Math.max(
        this.peakDiskKiB,
        Number.parseInt(disk.stdout, 10) || 0,
      );
      this.samples++;
    } catch {
      this.errors++;
    }
  }
  async stop() {
    if (this.stopped) return this.result();
    this.stopped = true;
    if (this.timer) clearInterval(this.timer);
    await this.busy;
    await this.sample();
    return this.result();
  }
  result() {
    return {
      sampleIntervalMs: 1000,
      samples: this.samples,
      errors: this.errors,
      sampledPeakOwnedRssKiB: this.peakRssKiB,
      sampledPeakStateDiskKiB: this.peakDiskKiB,
      scope:
        "Verification host, observed descendants and explicitly discovered application/daemon/backend roots; private state directory disk usage. Shared package caches, undiscovered detached processes and between-sample peaks are excluded.",
    };
  }
}
