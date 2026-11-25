import type {
  AgentRuntime,
  DashboardRuntimeUsage,
  DashboardUsageByRuntime,
} from "@multica/core/types";
import { estimateCost } from "../runtimes/utils";
import {
  buildRuntimeMachines,
  type RuntimeMachine,
} from "../runtimes/components/runtime-machines";

// Per-machine usage (the dashboard's "By machine" tab).
//
// The server reports usage per runtime; machines are a client-side grouping
// (one daemon hosts several runtimes), so this groups the rows with the same
// buildRuntimeMachines the Runtimes page uses, which keeps names identical.

/**
 * Synthetic runtime_id the SERVER sends for the bucket of every runtime whose
 * usage the viewer may not read, or that no longer exists. Mirrors
 * `restrictedRuntimesRowID` in server/internal/handler/dashboard_runtime.go;
 * the two strings must stay in sync.
 */
export const RESTRICTED_RUNTIMES_ROW_ID = "__restricted_runtimes__";

export interface RuntimeUsageSummary {
  tokens: number;
  cost: number;
  /** Distinct runs with usage in the window, counted server-side. */
  taskCount: number;
}

export interface MachineRuntimeUsageRow extends RuntimeUsageSummary {
  runtime: AgentRuntime;
}

/**
 * One row of the tab. `machine` rows group the runtimes the viewer's runtime
 * list knows. `restricted` is the server's bucket. `deleted` collects runtime
 * ids the server named but the runtime list does not have, which happens when
 * a runtime is removed between the two reads.
 */
export type MachineUsageRow =
  | (RuntimeUsageSummary & {
      kind: "machine";
      id: string;
      machine: RuntimeMachine;
      runtimes: MachineRuntimeUsageRow[];
    })
  | (RuntimeUsageSummary & { kind: "restricted" | "deleted"; id: string });

/** Tokens and estimated cost of a list of (provider, model) usage rows. */
export function summarizeRuntimeUsage(usage: DashboardRuntimeUsage[]): {
  tokens: number;
  cost: number;
} {
  let tokens = 0;
  let cost = 0;
  for (const u of usage) {
    tokens += u.input_tokens + u.output_tokens + u.cache_read_tokens + u.cache_write_tokens;
    cost += estimateCost(u);
  }
  return { tokens, cost };
}

function addTo(target: RuntimeUsageSummary, add: RuntimeUsageSummary) {
  target.tokens += add.tokens;
  target.cost += add.cost;
  target.taskCount += add.taskCount;
}

const bySpend = (a: RuntimeUsageSummary, b: RuntimeUsageSummary) =>
  b.cost - a.cost || b.tokens - a.tokens;

/**
 * Groups per-runtime usage into machines, priced like the per-agent
 * leaderboard. Machines sort by cost, then tokens; the deleted and restricted
 * buckets always sort last. Every input row lands in exactly one output row,
 * so the rows add up to the page's Cost and Tokens KPIs.
 */
export function aggregateMachineUsage(
  rows: DashboardUsageByRuntime[],
  runtimes: AgentRuntime[],
): MachineUsageRow[] {
  // Health is not shown here, so the clock does not matter.
  const machines = buildRuntimeMachines(runtimes, { now: 0 });
  const machineByRuntime = new Map<string, RuntimeMachine>();
  const runtimeById = new Map<string, AgentRuntime>();
  for (const machine of machines) {
    for (const runtime of machine.runtimes) {
      machineByRuntime.set(runtime.id, machine);
      runtimeById.set(runtime.id, runtime);
    }
  }

  const machineRows = new Map<string, Extract<MachineUsageRow, { kind: "machine" }>>();
  const deleted: MachineUsageRow = { kind: "deleted", id: "deleted", tokens: 0, cost: 0, taskCount: 0 };
  const restricted: MachineUsageRow = {
    kind: "restricted",
    id: RESTRICTED_RUNTIMES_ROW_ID,
    tokens: 0,
    cost: 0,
    taskCount: 0,
  };
  let hasDeleted = false;
  let hasRestricted = false;

  for (const row of rows) {
    const summary = { ...summarizeRuntimeUsage(row.usage), taskCount: row.task_count };
    if (row.runtime_id === RESTRICTED_RUNTIMES_ROW_ID) {
      hasRestricted = true;
      addTo(restricted, summary);
      continue;
    }
    const machine = machineByRuntime.get(row.runtime_id);
    const runtime = runtimeById.get(row.runtime_id);
    if (!machine || !runtime) {
      hasDeleted = true;
      addTo(deleted, summary);
      continue;
    }
    let entry = machineRows.get(machine.id);
    if (!entry) {
      entry = { kind: "machine", id: machine.id, machine, runtimes: [], tokens: 0, cost: 0, taskCount: 0 };
      machineRows.set(machine.id, entry);
    }
    addTo(entry, summary);
    const runtimeRow = entry.runtimes.find((r) => r.runtime.id === runtime.id);
    if (runtimeRow) addTo(runtimeRow, summary);
    else entry.runtimes.push({ runtime, ...summary });
  }

  const out: MachineUsageRow[] = Array.from(machineRows.values())
    .map((entry) => ({ ...entry, runtimes: entry.runtimes.toSorted(bySpend) }))
    .toSorted((a, b) => bySpend(a, b) || a.machine.title.localeCompare(b.machine.title));
  if (hasDeleted) out.push(deleted);
  if (hasRestricted) out.push(restricted);
  return out;
}
