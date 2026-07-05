import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
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

import { SessionContextBar } from "./context-bar";

// FIR-2279: the prompt-cache countdown is driven purely by last_activity_at plus
// the 5-minute TTL measured against the wall clock, so we anchor Date.now() to a
// fixed instant and vary how long ago the last run finished. This exercises the
// real warm/urgent/cold render branches — the part PR2107 shipped without a
// render test (the added test only checked the last_activity_at data field).
const LAST_ACTIVITY = "2026-07-05T18:30:00Z";
const lastMs = new Date(LAST_ACTIVITY).getTime();

function freezeNow(msAfterLastActivity: number) {
  vi.spyOn(Date, "now").mockReturnValue(lastMs + msAfterLastActivity);
}

function usage(overrides: Record<string, unknown>) {
  return {
    has_data: true,
    used_percent: 40,
    max_context_tokens: 1_000_000,
    context_tokens: 400_000,
    ...overrides,
  };
}

function renderBar() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <SessionContextBar issueId="issue-1" groups={[]} active={true} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  mockCerebroRequest.mockReset();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("SessionContextBar prompt-cache countdown (FIR-2279)", () => {
  it("shows the warm countdown while the cache is still warm", async () => {
    // 200s after the last run → 100s (1:40) left in the 5-minute window.
    freezeNow(200 * 1000);
    mockCerebroRequest.mockResolvedValue(usage({ last_activity_at: LAST_ACTIVITY }));
    renderBar();

    expect(await screen.findByText("Cache warm · 1:40 left")).toBeInTheDocument();
    expect(screen.queryByText(/Cache cold/)).not.toBeInTheDocument();
  });

  it("ambers the timer in its final minute", async () => {
    // 260s after the last run → 40s left → inside the urgent (final-minute) band.
    freezeNow(260 * 1000);
    mockCerebroRequest.mockResolvedValue(usage({ last_activity_at: LAST_ACTIVITY }));
    renderBar();

    const label = await screen.findByText("Cache warm · 0:40 left");
    expect(label.className).toContain("text-amber");
  });

  it("flips to the cold state once the 5-minute window has passed", async () => {
    // 400s after the last run → past the 300s TTL → cache is cold.
    freezeNow(400 * 1000);
    mockCerebroRequest.mockResolvedValue(usage({ last_activity_at: LAST_ACTIVITY }));
    renderBar();

    expect(
      await screen.findByText("Cache cold · next run re-pays the full context"),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Cache warm/)).not.toBeInTheDocument();
  });

  it("renders no timer when the server omits last_activity_at (older server)", async () => {
    freezeNow(200 * 1000);
    mockCerebroRequest.mockResolvedValue(usage({ last_activity_at: null }));
    renderBar();

    // The context line still renders (percent label), but neither cache state shows.
    expect(await screen.findByText(/context used/)).toBeInTheDocument();
    expect(screen.queryByText(/Cache warm/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Cache cold/)).not.toBeInTheDocument();
  });
});
