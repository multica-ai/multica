import type { AgentRuntime } from "@multica/core/types";
import type { RuntimeMachine } from "./runtime-machines";
import { UpdateSection } from "./update-section";

/**
 * Pick one viewer-owned runtime as the command channel for a machine-wide
 * daemon update. An online runtime wins so the daemon can receive the request
 * immediately; an offline row still keeps version/managed-state display
 * available without enabling the update action.
 */
export function machineUpdateRuntime(
  machine: RuntimeMachine,
  currentUserId: string | undefined,
): AgentRuntime | null {
  if (machine.mode !== "local" || !currentUserId) return null;

  const owned = machine.runtimes.filter(
    (runtime) => runtime.owner_id === currentUserId,
  );
  return owned.find((runtime) => runtime.status === "online") ?? owned[0] ?? null;
}

export function MachineCliSection({
  machine,
  currentUserId,
}: {
  machine: RuntimeMachine;
  currentUserId: string | undefined;
}) {
  const updateRuntime = machineUpdateRuntime(machine, currentUserId);

  if (machine.mode !== "local") {
    return machine.cliVersion ? (
      <span className="font-mono">CLI {machine.cliVersion}</span>
    ) : null;
  }

  // A viewer's ability to send an update command must not gate the
  // machine-level version and manager information. The only local machine
  // without anything to report is Desktop's synthesized stopped-daemon row.
  if (
    !updateRuntime &&
    machine.runtimes.length === 0 &&
    !machine.cliVersion &&
    !machine.launchedBy
  ) {
    return null;
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
