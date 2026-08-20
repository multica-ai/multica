import { AgentsPage } from "@multica/views/agents";

// Browser hosts group local-mode runtimes under "Remote" because they do not
// manage a daemon process on the viewing device.
export default function AgentsRoute() {
  return <AgentsPage />;
}
