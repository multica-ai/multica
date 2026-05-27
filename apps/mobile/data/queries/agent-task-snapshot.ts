import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

// Workspace agent task snapshot — every active task plus each agent's most
// recent terminal task. Feeds the workload dimension of presence. Mobile
// invalidates on task lifecycle events (queued/dispatch/completed/failed/
// cancelled) but DELIBERATELY skips task:progress and task:message — those
// fire many times per active task and would invalidate-storm cellular data.
// See data/realtime/use-presence-realtime.ts.
export const agentTaskSnapshotOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: ["agent-task-snapshot", wsId] as const,
    queryFn: ({ signal }) => api.listAgentTaskSnapshot({ signal }),
    enabled: !!wsId,
  });
