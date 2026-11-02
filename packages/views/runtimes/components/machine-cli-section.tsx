import type { AgentRuntime } from "@multica/core/types";
import type { RuntimeMachine } from "./runtime-machines";
import { UpdateSection } from "./update-section";

/**
 * Pick one runtime the viewer may use as the command channel for a
 * machine-wide daemon update. The viewer may use their own runtime, or a
 * public runtime when they are a workspace owner/admin. An online runtime
 * wins so the daemon can receive the request immediately.
 */
export function machineUpdateRuntime(
  machine: RuntimeMachine,
  currentUserId: string | undefined,
  canManagePublicRuntimes: boolean,
): AgentRuntime | null {
  if (machine.mode !== "local") return null;

  const usable = currentUserId
    ? machine.runtimes.filter(
        (runtime) =>
          runtime.owner_id === currentUserId ||
          (canManagePublicRuntimes && runtime.visibility === "public"),
      )
    : [];
  return (
    usable.find((runtime) => runtime.status === "online") ??
    usable[0] ??
    null
  );
}

/**
 * Pick the runtime whose agent CLI status this machine page should show.
 * The viewer's own runtime wins so a local owner can upgrade. A workspace
 * admin otherwise sees any runtime on the machine. Everyone else sees a
 * public runtime. An online runtime wins so a command can be delivered.
 */
export function machineAgentCLIRuntime(
  machine: RuntimeMachine,
  currentUserId: string | undefined,
  isWorkspaceAdmin: boolean,
): AgentRuntime | null {
  const real = machine.runtimes.filter((runtime) => runtime.id);
  if (real.length === 0) return null;

  const owned = currentUserId
    ? real.filter((runtime) => runtime.owner_id === currentUserId)
    : [];
  const pool =
    owned.length > 0
      ? owned
      : isWorkspaceAdmin
        ? real
        : real.filter((runtime) => runtime.visibility === "public");
  if (pool.length === 0) return null;
  return pool.find((runtime) => runtime.status === "online") ?? pool[0] ?? null;
}

/**
 * Whether the machine has a CLI version or update channel to show.
 * A viewer's ability to send an update command must not gate the
 * machine-level version and manager information. The only local machine
 * without anything to report is Desktop's synthesized stopped-daemon row.
 */
export function machineHasCliInfo(
  machine: RuntimeMachine,
  updateRuntime: AgentRuntime | null,
): boolean {
  if (machine.mode !== "local") return !!machine.cliVersion;
  return (
    !!updateRuntime ||
    machine.runtimes.length > 0 ||
    !!machine.cliVersion ||
    !!machine.launchedBy
  );
}

export function MachineCliSection({
  machine,
  currentUserId,
  canManagePublicRuntimes = false,
}: {
  machine: RuntimeMachine;
  currentUserId: string | undefined;
  canManagePublicRuntimes?: boolean;
}) {
  const updateRuntime = machineUpdateRuntime(
    machine,
    currentUserId,
    canManagePublicRuntimes,
  );

  if (!machineHasCliInfo(machine, updateRuntime)) return null;

  if (machine.mode !== "local") {
    return <span className="font-mono">CLI {machine.cliVersion}</span>;
  }

  return (
    <UpdateSection
      runtimeId={updateRuntime?.id ?? null}
      currentVersion={machine.cliVersion}
      isOnline={updateRuntime?.status === "online"}
      launchedBy={machine.launchedBy}
    />
  );
}
