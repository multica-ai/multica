"use client";

import type { AgentRuntime } from "@multica/core/types";
import { runtimeComputerName } from "../runtime-computer-name";

// FIR-2669: runtime-list "Machine" column cell — the computer/hostname the
// runtime runs on. The name column shows only the "base" part of a
// "base (hostname)" runtime name, so without this the machine name is
// invisible. Opt-in via the column picker; renders an em-dash when the machine
// can't be resolved (e.g. a cloud runtime with an empty name).
export function RuntimeMachineCell({ runtime }: { runtime: AgentRuntime }) {
  const computer = runtimeComputerName(runtime);
  if (!computer) {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }
  return (
    <span className="min-w-0 truncate font-mono text-xs text-muted-foreground">
      {computer}
    </span>
  );
}
