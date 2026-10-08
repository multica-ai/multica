import { AsyncLocalStorage } from "node:async_hooks";

import type { DaemonState } from "../shared/daemon-types";

// The normal poll runs every 5s. Requiring three misses gives a transient slow
// response two chances to recover before Desktop spends 10s confirming death.
export const RECOVERY_STOPPED_THRESHOLD = 3;
// A daemon reports "running" only after its startup preflight completes. Keep
// it there for another minute before forgiving failures, so a 20s crash loop
// cannot reset the retry ladder on every cycle.
export const RECOVERY_STABLE_RUNNING_MS = 60_000;
// Fast retries cover one-off process kills; repeated failures step up to two
// minutes and then cap at ten minutes so broken preflight cannot create a
// restart storm while still allowing unattended recovery.
export const RECOVERY_BACKOFF_MS = [15_000, 30_000, 120_000, 600_000] as const;
// Even a daemon that survives the stable-running threshold may crash again.
// Five starts in thirty minutes is the absolute automatic budget; the sixth
// pauses visibly until a member explicitly starts/restarts the daemon.
export const RECOVERY_ATTEMPT_BUDGET = 5;
export const RECOVERY_ATTEMPT_WINDOW_MS = 30 * 60_000;
// A force-killed daemon leaves daemon.pid behind and Windows may later reuse
// that PID. Three 30s-spaced deferrals provide a conservative process check,
// then allow the CLI's fresh health check and the OS port bind to decide: an
// unresponsive old listener makes the replacement fail into normal backoff.
export const RECOVERY_PID_DEFERRAL_LIMIT = 3;
export const RECOVERY_PID_RECHECK_MS = 30_000;

export type RecoveryDecision = "none" | "confirm" | "pause";

interface RecoveryObservation {
  desiredRunning: boolean;
  externalDaemonObserved: boolean;
  lifecycleBusy: boolean;
  state: DaemonState;
  now: number;
}

/**
 * Pure policy for deciding when the Desktop should confirm and recover a lost
 * daemon. Side effects stay in daemon-manager; this class owns consecutive
 * misses, sustained-health forgiveness, PID deferrals, backoff, and budget.
 */
export class DaemonRecoveryPolicy {
  private consecutiveStopped = 0;
  private runningSince: number | null = null;
  private failureCount = 0;
  private nextAttemptAt = 0;
  private pidDeferrals = 0;
  private attemptTimestamps: number[] = [];
  private paused = false;

  get isPaused(): boolean {
    return this.paused;
  }

  observe(observation: RecoveryObservation): RecoveryDecision {
    this.pruneAttemptBudget(observation.now);

    if (observation.state === "running") {
      this.consecutiveStopped = 0;
      if (this.runningSince === null) this.runningSince = observation.now;
      if (
        observation.now - this.runningSince >=
        RECOVERY_STABLE_RUNNING_MS
      ) {
        this.failureCount = 0;
        this.nextAttemptAt = 0;
        this.pidDeferrals = 0;
        this.paused = false;
      }
      return "none";
    }

    this.runningSince = null;
    const eligible =
      observation.desiredRunning &&
      !observation.externalDaemonObserved &&
      !observation.lifecycleBusy &&
      observation.state === "stopped";
    if (!eligible) {
      this.consecutiveStopped = 0;
      return "none";
    }
    if (this.paused) return "pause";

    this.consecutiveStopped += 1;
    if (this.consecutiveStopped < RECOVERY_STOPPED_THRESHOLD) return "none";

    this.consecutiveStopped = 0;
    if (observation.now < this.nextAttemptAt) return "none";
    if (this.attemptTimestamps.length >= RECOVERY_ATTEMPT_BUDGET) {
      this.paused = true;
      return "pause";
    }
    return "confirm";
  }

  recordRecoveryAttempt(now: number): void {
    this.pruneAttemptBudget(now);
    this.attemptTimestamps.push(now);
    const delay =
      RECOVERY_BACKOFF_MS[
        Math.min(this.failureCount, RECOVERY_BACKOFF_MS.length - 1)
      ];
    this.failureCount += 1;
    this.nextAttemptAt = now + delay;
    this.pidDeferrals = 0;
  }

  /** Returns true once a stale/reused PID must stop blocking recovery. */
  recordPidDeferral(now: number): boolean {
    this.pidDeferrals += 1;
    this.nextAttemptAt = now + RECOVERY_PID_RECHECK_MS;
    if (this.pidDeferrals < RECOVERY_PID_DEFERRAL_LIMIT) return false;
    this.pidDeferrals = 0;
    return true;
  }

  recordPidAbsent(): void {
    this.pidDeferrals = 0;
  }

  reset(): void {
    this.consecutiveStopped = 0;
    this.runningSince = null;
    this.failureCount = 0;
    this.nextAttemptAt = 0;
    this.pidDeferrals = 0;
    this.attemptTimestamps = [];
    this.paused = false;
  }

  private pruneAttemptBudget(now: number): void {
    const cutoff = now - RECOVERY_ATTEMPT_WINDOW_MS;
    this.attemptTimestamps = this.attemptTimestamps.filter(
      (attemptedAt) => attemptedAt > cutoff,
    );
  }
}

export interface RecoveryProfile {
  name: string;
  port: number;
}

export interface DaemonLifecycleResult {
  success: boolean;
  error?: string;
}

export type RecoveryAttemptOutcome =
  | { kind: "superseded" }
  | { kind: "alive" }
  | { kind: "pid_deferred" }
  | { kind: "started" }
  | { kind: "start_failed"; error?: string }
  | { kind: "cancelled" };

interface RecoveryAttemptDependencies {
  startAllowed: () => boolean;
  confirmAlive: () => Promise<boolean>;
  pidConfirmedAbsent: () => Promise<boolean>;
  recordPidDeferral: () => boolean;
  recordPidAbsent: () => void;
  onPidFallback: () => void;
  start: () => Promise<DaemonLifecycleResult>;
  desiredRunning: () => boolean;
  stop: () => Promise<DaemonLifecycleResult>;
}

/**
 * Runs one recovery attempt with a fresh intent check after every await. This
 * is the race-sensitive orchestration boundary: profile switches and member
 * Stop actions can supersede background work while its health/PID/start calls
 * are in flight, and a successful late start is rolled back before release.
 */
export async function runDaemonRecoveryAttempt(
  dependencies: RecoveryAttemptDependencies,
): Promise<RecoveryAttemptOutcome> {
  if (!dependencies.startAllowed()) return { kind: "superseded" };
  if (await dependencies.confirmAlive()) return { kind: "alive" };
  if (!dependencies.startAllowed()) return { kind: "superseded" };

  if (!(await dependencies.pidConfirmedAbsent())) {
    if (!dependencies.recordPidDeferral()) return { kind: "pid_deferred" };
    dependencies.onPidFallback();
  } else {
    dependencies.recordPidAbsent();
  }
  if (!dependencies.startAllowed()) return { kind: "superseded" };

  const result = await dependencies.start();
  if (!dependencies.desiredRunning()) {
    if (result.success) await dependencies.stop();
    return { kind: "cancelled" };
  }
  return result.success
    ? { kind: "started" }
    : { kind: "start_failed", error: result.error };
}

/**
 * Named Desktop profiles and their derived ports are the ownership boundary;
 * recovery does not require a prior successful health observation. A known
 * foreign-OS daemon and profile/target changes remain explicit exclusions.
 */
export function recoveryStartAllowed(input: {
  desiredRunning: boolean;
  externalDaemonObserved: boolean;
  expected: RecoveryProfile;
  current: RecoveryProfile | null;
}): boolean {
  return Boolean(
    input.desiredRunning &&
      !input.externalDaemonObserved &&
      input.current &&
      input.current.name === input.expected.name &&
      input.current.port === input.expected.port,
  );
}

export interface OperationBusyResult {
  success: false;
  error: string;
  operationBusy: true;
}

const OPERATION_BUSY: OperationBusyResult = {
  success: false,
  error: "Another daemon operation is in progress",
  operationBusy: true,
};

/**
 * Serializes lifecycle work. One-shot member/login intents use runForeground:
 * they wait for already-running background maintenance instead of disappearing.
 * Only work driven by a recurring poll uses runBackground, whose busy result is
 * intentionally dropped because the next poll safely retries it. Calling
 * runForeground from inside either gated mode throws before it can self-wait.
 */
export class DaemonOperationGate {
  private busy = false;
  private foregroundQueued = 0;
  private completion: Promise<void> = Promise.resolve();
  private activeOperation: symbol | null = null;
  private readonly operationContext = new AsyncLocalStorage<symbol>();

  get inProgress(): boolean {
    return this.busy || this.foregroundQueued > 0;
  }

  async runForeground<T>(fn: () => Promise<T>): Promise<T> {
    const currentOperation = this.operationContext.getStore();
    if (
      currentOperation !== undefined &&
      currentOperation === this.activeOperation
    ) {
      throw new Error(
        "DaemonOperationGate.runForeground cannot be called from inside a gated operation",
      );
    }
    const previous = this.completion;
    let release: () => void = () => {};
    const current = new Promise<void>((resolve) => {
      release = resolve;
    });
    this.foregroundQueued += 1;
    this.completion = previous.then(
      () => current,
      () => current,
    );

    await previous;
    this.foregroundQueued -= 1;
    try {
      return await this.execute(fn);
    } finally {
      release();
    }
  }

  runBackground<T>(
    fn: () => Promise<T>,
  ): Promise<T | OperationBusyResult> {
    if (this.inProgress) return Promise.resolve(OPERATION_BUSY);
    const operation = this.execute(fn);
    const completion = operation.then(
      () => undefined,
      () => undefined,
    );
    this.completion = completion;
    return operation;
  }

  private async execute<T>(fn: () => Promise<T>): Promise<T> {
    const operation = Symbol("daemon-operation");
    this.busy = true;
    this.activeOperation = operation;
    try {
      return await this.operationContext.run(operation, fn);
    } finally {
      this.busy = false;
      if (this.activeOperation === operation) this.activeOperation = null;
    }
  }
}

export function parseDaemonPid(raw: string): number | null {
  const trimmed = raw.trim();
  if (!/^\d+$/.test(trimmed)) return null;
  const pid = Number(trimmed);
  return Number.isSafeInteger(pid) && pid > 0 ? pid : null;
}

/** Conservative process-liveness check: only ESRCH proves the PID is absent. */
export function daemonProcessExists(
  pid: number,
  signalProbe: (pid: number, signal: 0) => void = process.kill,
): boolean {
  try {
    signalProbe(pid, 0);
    return true;
  } catch (err) {
    return !(
      err &&
      typeof err === "object" &&
      "code" in err &&
      err.code === "ESRCH"
    );
  }
}
