import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";

import { issueKeys } from "@/data/queries/issue-keys";
import { invalidateIssueAfterReconnect } from "./issue-ws-updaters";

describe("invalidateIssueAfterReconnect", () => {
  it("invalidates attachments together with the issue and task caches", () => {
    const qc = new QueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    const wsId = "workspace-1";
    const issueId = "issue-1";

    invalidateIssueAfterReconnect(qc, wsId, issueId);

    expect(invalidate.mock.calls.map(([filters]) => filters?.queryKey)).toEqual([
      issueKeys.detail(wsId, issueId),
      issueKeys.timeline(wsId, issueId),
      issueKeys.attachments(wsId, issueId),
      issueKeys.activeTasks(wsId, issueId),
      issueKeys.tasks(wsId, issueId),
    ]);
  });
});
