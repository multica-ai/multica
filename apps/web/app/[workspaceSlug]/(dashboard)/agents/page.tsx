import { AgentsPage } from "@multica/views/agents";

// Web has no bundled daemon, so the runtime filter always groups
// local-mode runtimes under "Remote" (buildRuntimeMachines has no
// localDaemonId / localMachineName / ensureLocalMachine context
// here) — that's the expected web behavior, not a bug. The Desktop
// app wires those props through `DesktopAgentsPage` so the local
// section appears in the dropdown the same way it does on the
// Runtimes page.
export default function AgentsRoute() {
  return <AgentsPage />;
}
