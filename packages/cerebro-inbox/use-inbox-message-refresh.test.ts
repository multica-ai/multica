import { describe, it, expect, vi } from "vitest";
import type { QueryClient } from "@tanstack/react-query";
import { issueKeys } from "@multica/core/issues/queries";
import { refreshInboxMessageQueries } from "./use-inbox-message-refresh";

function makeQc() {
  const invalidateQueries = vi.fn();
  return { invalidateQueries } as unknown as QueryClient & {
    invalidateQueries: ReturnType<typeof vi.fn>;
  };
}

describe("refreshInboxMessageQueries", () => {
  it("invalidates the opened issue's timeline and detail", () => {
    const qc = makeQc();
    refreshInboxMessageQueries(qc, "ws-1", "issue-9");

    expect(qc.invalidateQueries).toHaveBeenCalledTimes(2);
    expect(qc.invalidateQueries).toHaveBeenCalledWith({
      queryKey: issueKeys.timeline("issue-9"),
    });
    expect(qc.invalidateQueries).toHaveBeenCalledWith({
      queryKey: issueKeys.detail("ws-1", "issue-9"),
    });
  });

  it("no-ops when no issue is open (channel/DM/chat selected)", () => {
    const qc = makeQc();
    refreshInboxMessageQueries(qc, "ws-1", null);
    expect(qc.invalidateQueries).not.toHaveBeenCalled();
  });
});
