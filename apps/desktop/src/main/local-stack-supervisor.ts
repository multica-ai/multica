import { EventEmitter } from "events";

import {
  LOCAL_STACK_COMPONENT_ORDER,
  type LocalStackComponentName,
  type LocalStackComponentState,
  type LocalStackComponentStatus,
  type LocalStackOverallState,
  type LocalStackStatus,
} from "../shared/local-stack-types";
import type { ManagedPostgres } from "./managed-postgres";

/**
 * Per-component runner. Returning the promise represents a single attempt;
 * resolved = ready, rejected = failing. Task 9 ships only the shell, so the
 * default runners report "ready" (database/api treated as externally managed
 * during dev) while the supervisor scaffolding is exercised.
 */
export interface LocalStackComponentRunner {
  name: LocalStackComponentName;
  start: () => Promise<void>;
  stop?: () => Promise<void>;
}

export type SupervisorOptions = {
  runners: LocalStackComponentRunner[];
  /** Emit progress to a sink — typically `(payload) => mainWindow.webContents.send("localStack:status", payload)`. */
  onStatusChange?: (status: LocalStackStatus) => void;
  /** Wall-clock now() — injectable for tests. */
  now?: () => number;
};

export class LocalStackSupervisor extends EventEmitter {
  private readonly runners: LocalStackComponentRunner[];
  private readonly onStatusChange?: (status: LocalStackStatus) => void;
  private readonly now: () => number;

  private statuses: Map<LocalStackComponentName, LocalStackComponentStatus>;
  private inFlight: Promise<void> | null = null;

  constructor(opts: SupervisorOptions) {
    super();
    this.runners = opts.runners;
    this.onStatusChange = opts.onStatusChange;
    this.now = opts.now ?? Date.now;

    const ts = this.now();
    this.statuses = new Map();
    for (const name of LOCAL_STACK_COMPONENT_ORDER) {
      this.statuses.set(name, {
        name,
        state: "pending",
        detail: null,
        updatedAt: ts,
      });
    }
  }

  getStatus(): LocalStackStatus {
    const components = LOCAL_STACK_COMPONENT_ORDER.map((name) => {
      const s = this.statuses.get(name);
      if (!s) {
        // Shouldn't happen — we seed every name in the constructor — but
        // staying defensive keeps the return type honest.
        return {
          name,
          state: "pending" as LocalStackComponentState,
          detail: null,
          updatedAt: this.now(),
        };
      }
      return { ...s };
    });
    return {
      overall: this.computeOverall(components),
      components,
    };
  }

  /** Sequentially run runner.start in LOCAL_STACK_COMPONENT_ORDER. */
  start(): Promise<void> {
    if (this.inFlight) return this.inFlight;
    this.inFlight = this.march().finally(() => {
      this.inFlight = null;
    });
    return this.inFlight;
  }

  /** Reset failing components to pending and re-run from the first non-ready. */
  retry(): Promise<void> {
    if (this.inFlight) return this.inFlight;
    const ts = this.now();
    for (const [name, s] of this.statuses) {
      if (s.state === "failing") {
        this.statuses.set(name, {
          name,
          state: "pending",
          detail: null,
          updatedAt: ts,
        });
      }
    }
    // Don't broadcast the "reset to pending" — the very next transition
    // (component going to "starting") will broadcast and is more meaningful.
    return this.start();
  }

  /** Stop runners in reverse order. Errors are swallowed to keep teardown best-effort. */
  async stop(): Promise<void> {
    for (let i = this.runners.length - 1; i >= 0; i--) {
      const runner = this.runners[i];
      if (!runner.stop) continue;
      try {
        await runner.stop();
      } catch (err) {
        // Best-effort teardown — surface as a log but don't abort.
        // eslint-disable-next-line no-console
        console.error(`[local-stack] stop(${runner.name}) failed`, err);
      }
    }
  }

  // ---------------------------------------------------------------------

  private async march(): Promise<void> {
    for (const name of LOCAL_STACK_COMPONENT_ORDER) {
      const current = this.statuses.get(name);
      if (current?.state === "ready") continue;

      const runner = this.runners.find((r) => r.name === name);
      if (!runner) {
        // No runner registered → treat as a no-op ready.
        this.transition(name, "ready", null);
        continue;
      }

      this.transition(name, "starting", null);
      try {
        await runner.start();
        this.transition(name, "ready", null);
      } catch (err) {
        const detail = err instanceof Error ? err.message : String(err);
        this.transition(name, "failing", detail);
        return;
      }
    }
  }

  private transition(
    name: LocalStackComponentName,
    state: LocalStackComponentState,
    detail: string | null,
  ): void {
    this.statuses.set(name, {
      name,
      state,
      detail,
      updatedAt: this.now(),
    });
    this.onStatusChange?.(this.getStatus());
  }

  private computeOverall(
    components: LocalStackComponentStatus[],
  ): LocalStackOverallState {
    if (components.some((c) => c.state === "failing")) return "failing";
    if (components.every((c) => c.state === "ready")) return "ready";
    return "starting";
  }
}

/**
 * Adapter: wrap a `ManagedPostgres` instance in the runner shape the
 * supervisor expects. Kept as a free function (not a method on
 * LocalStackSupervisor) so the supervisor stays decoupled from the
 * concrete database implementation.
 */
export function createManagedPostgresRunner(
  pg: ManagedPostgres,
): LocalStackComponentRunner {
  return {
    name: "database",
    start: () => pg.start(),
    stop: () => pg.stop(),
  };
}
