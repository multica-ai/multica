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

import { getUsageBreakdown } from "./api";
import { SessionUsageSheet } from "./session-usage-sheet";

beforeEach(() => {
  mockCerebroRequest.mockReset();
});

function renderWithClient(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const wellFormed = {
  sessions: [
    {
      session_id: "s1",
      title: "First session",
      created_at: "2026-06-24T10:00:00Z",
      run_count: 3,
      input_tokens: 250000,
      output_tokens: 12000,
      cache_read_tokens: 800000,
      cache_write_tokens: 40000,
      cost_cents: 530,
      model: "claude-opus-4-8",
      used_percent: 42,
      context_tokens: 420000,
      max_context_tokens: 1000000,
      cache_share_percent: 90,
      approximate: false,
    },
  ],
  total_cost_cents: 530,
  total_input_tokens: 250000,
  total_output_tokens: 12000,
  total_cache_read_tokens: 800000,
  total_cache_write_tokens: 40000,
};

describe("usage-breakdown api compatibility", () => {
  it("downgrades a garbage response to an empty breakdown", async () => {
    mockCerebroRequest.mockResolvedValueOnce({ unexpected: true });
    const res = await getUsageBreakdown("issue-1");
    expect(res.sessions).toEqual([]);
    expect(res.total_cost_cents).toBe(0);
  });

  it("passes through a well-formed breakdown", async () => {
    mockCerebroRequest.mockResolvedValueOnce(wellFormed);
    const res = await getUsageBreakdown("issue-1");
    expect(res.sessions).toHaveLength(1);
    expect(res.sessions[0]?.input_tokens).toBe(250000);
    expect(res.total_cost_cents).toBe(530);
  });
});

describe("SessionUsageSheet", () => {
  it("renders the issue total and a row per session when open", async () => {
    mockCerebroRequest.mockResolvedValue(wellFormed);
    renderWithClient(
      <SessionUsageSheet issueId="issue-1" open={true} onOpenChange={() => {}} />,
    );
    // Issue total cost ($5.30) and the session title appear once data loads.
    expect(await screen.findByText("First session")).toBeInTheDocument();
    expect(screen.getAllByText("$5.30").length).toBeGreaterThan(0);
    expect(screen.getByText("42%")).toBeInTheDocument();
  });

  it("does not fetch while closed", () => {
    mockCerebroRequest.mockResolvedValue(wellFormed);
    renderWithClient(
      <SessionUsageSheet issueId="issue-1" open={false} onOpenChange={() => {}} />,
    );
    expect(mockCerebroRequest).not.toHaveBeenCalled();
  });
});
