// @vitest-environment jsdom

// FIR-3212 — Quality tab: each config version measured on the runs that
// actually ran under it. Hard requirements under test: missing data renders
// as "—" (never 0), sample sizes are always visible, and the empty state is
// an honest explanation rather than a fabricated metric.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";

const mockCerebroRequest = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: { ...actual.api, cerebroRequest: mockCerebroRequest },
  };
});

import { CerebroAgentQualityTab } from "./quality-tab";

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Kathrine",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "cloud",
  runtime_config: {},
  custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
  archived_at: null,
  archived_by: null,
  persona_sandbox: "",
};

const measuredVersion = {
  agent_context_version: "v1.4.0",
  agent_context_version_id: "acv-1",
  runs: 6,
  first_run_at: "2026-07-10T08:00:00Z",
  last_run_at: "2026-07-15T07:32:00Z",
  solution: {
    measured_runs: 5,
    measurements: 5,
    passes: 3,
    scored: 5,
    avg_score: 0.72,
    confident: 5,
    avg_confidence: 0.9,
  },
  satisfaction: {
    measured_runs: 4,
    reactions: 2,
    approvals: 1,
    rejections: 1,
  },
};

const unmeasuredVersion = {
  agent_context_version: "v1.3.0",
  agent_context_version_id: "acv-0",
  runs: 2,
  first_run_at: "2026-07-01T08:00:00Z",
  last_run_at: "2026-07-02T08:00:00Z",
  solution: {
    measured_runs: 0,
    measurements: 0,
    passes: 0,
    scored: 0,
    avg_score: null,
    confident: 0,
    avg_confidence: null,
  },
  satisfaction: {
    measured_runs: 0,
    reactions: 0,
    approvals: 0,
    rejections: 0,
  },
};

function renderTab() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <CerebroAgentQualityTab agent={baseAgent} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  mockCerebroRequest.mockReset();
});

describe("CerebroAgentQualityTab", () => {
  it("renders one row per config version with run counts", async () => {
    mockCerebroRequest.mockResolvedValue({
      versions: [measuredVersion, unmeasuredVersion],
    });
    renderTab();

    expect(await screen.findByText("v1.4.0")).toBeInTheDocument();
    expect(screen.getByText("v1.3.0")).toBeInTheDocument();
    // Run counts with explicit sample sizes.
    expect(screen.getByText("6")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
  });

  it("shows solution score with its sample size when measured", async () => {
    mockCerebroRequest.mockResolvedValue({ versions: [measuredVersion] });
    renderTab();

    // avg 0.72 over 5 scored measurements; 3 of 5 passed.
    expect(await screen.findByText(/0\.72/)).toBeInTheDocument();
    expect(screen.getByText(/3\s*\/\s*5 passed/)).toBeInTheDocument();
  });

  it("renders missing measurements as an em dash, never zero", async () => {
    mockCerebroRequest.mockResolvedValue({ versions: [unmeasuredVersion] });
    renderTab();

    await screen.findByText("v1.3.0");
    // No fabricated 0.0 score: the solution cell shows a dash + honest note.
    expect(screen.queryByText(/0\.0/)).not.toBeInTheDocument();
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
    expect(screen.getByText(/no measurements yet/i)).toBeInTheDocument();
  });

  it("shows satisfaction as behavioral signals with sample size", async () => {
    mockCerebroRequest.mockResolvedValue({ versions: [measuredVersion] });
    renderTab();

    await screen.findByText("v1.4.0");
    expect(screen.getByText(/2 reactions/i)).toBeInTheDocument();
    expect(screen.getByText(/1 approval\b/i)).toBeInTheDocument();
    expect(screen.getByText(/1 rejection\b/i)).toBeInTheDocument();
  });

  it("renders an honest empty state when nothing has been measured", async () => {
    mockCerebroRequest.mockResolvedValue({ versions: [] });
    renderTab();

    expect(
      await screen.findByText(/no runs have recorded a config version yet/i),
    ).toBeInTheDocument();
  });
});
