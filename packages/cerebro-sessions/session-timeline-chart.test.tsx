import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const mockCerebroRequest = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return {
    ...actual,
    api: { cerebroRequest: mockCerebroRequest },
  };
});

import { getContextTimeline } from "./api";
import { SessionTimelineChart } from "./session-timeline-chart";

beforeEach(() => {
  mockCerebroRequest.mockReset();
});

function renderWithClient(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe("context-timeline api compatibility", () => {
  it("downgrades garbage to a no-data timeline", async () => {
    mockCerebroRequest.mockResolvedValueOnce({ unexpected: true });
    const res = await getContextTimeline("issue-1", "s1");
    expect(res.has_data).toBe(false);
    expect(res.points).toEqual([]);
  });

  it("passes through a well-formed timeline with a compaction flag", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      session_id: "s1",
      has_data: true,
      points: [
        { observed_at: "t1", context_tokens: 200000, max_context_tokens: 1000000, used_percent: 20, cache_share_percent: 0, is_compaction: false },
        { observed_at: "t2", context_tokens: 700000, max_context_tokens: 1000000, used_percent: 70, cache_share_percent: 0, is_compaction: false },
        { observed_at: "t3", context_tokens: 100000, max_context_tokens: 1000000, used_percent: 10, cache_share_percent: 0, is_compaction: true },
      ],
    });
    const res = await getContextTimeline("issue-1", "s1");
    expect(res.points).toHaveLength(3);
    expect(res.points[2]?.is_compaction).toBe(true);
  });
});

describe("SessionTimelineChart", () => {
  it("shows the forward-only empty state when a session has no history", async () => {
    mockCerebroRequest.mockResolvedValue({ session_id: "s1", has_data: false, points: [] });
    renderWithClient(
      <SessionTimelineChart issueId="issue-1" sessionId="s1" enabled={true} />,
    );
    expect(
      await screen.findByText(/collected from now on/i),
    ).toBeInTheDocument();
  });

  it("shows a single run's value inline, without needing a hover", async () => {
    mockCerebroRequest.mockResolvedValue({
      session_id: "s1",
      has_data: true,
      points: [
        { observed_at: "t1", context_tokens: 50000, max_context_tokens: 1000000, used_percent: 5, cache_share_percent: 0, is_compaction: false },
      ],
    });
    renderWithClient(
      <SessionTimelineChart issueId="issue-1" sessionId="s1" enabled={true} />,
    );
    // The value is in the DOM directly — not hidden in a hover-only tooltip.
    expect(await screen.findByText(/5% used/i)).toBeInTheDocument();
    expect(screen.getByText(/builds out as the session runs/i)).toBeInTheDocument();
  });

  it("does not fetch while disabled", () => {
    mockCerebroRequest.mockResolvedValue({ session_id: "s1", has_data: false, points: [] });
    renderWithClient(
      <SessionTimelineChart issueId="issue-1" sessionId="s1" enabled={false} />,
    );
    expect(mockCerebroRequest).not.toHaveBeenCalled();
  });
});
