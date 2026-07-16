// @vitest-environment jsdom

// FIR-3212 Approval slice (mockup M3) — the queue is where the two halves meet:
// the field diff (what changed) and the consequences panel (what it means on
// this agent's engine). These cover the wiring between them, not either half's
// own behaviour.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent, AgentContextChangeRequest } from "@multica/core/types";
import type { ReactNode } from "react";

const mocks = vi.hoisted(() => ({
  requests: [] as AgentContextChangeRequest[],
  versions: [] as unknown[],
  featureFlag: true,
  impactSpy: vi.fn(),
}));

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: (opts: { queryKey: unknown[] }) => {
      const key = JSON.stringify(opts.queryKey);
      if (key.includes("change-requests")) return { data: mocks.requests };
      if (key.includes("versions")) return { data: mocks.versions };
      return actual.useQuery(opts as never);
    },
  };
});

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => mocks.featureFlag,
}));

vi.mock("../../core/mutations", () => ({
  useReviewAgentContextChangeRequest: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
}));

vi.mock("../use-skill-name-resolver", () => ({
  useSkillNameResolver: () => (id: string) => id,
}));

// The panel itself is covered in agent-context-approval-impact.test.tsx; here it
// is a spy that records the fields the queue decided to ask about.
vi.mock("./agent-context-approval-impact", () => ({
  AgentContextApprovalImpact: (props: { changedFields: string[] }) => {
    mocks.impactSpy(props.changedFields);
    return <div data-testid="approval-impact" />;
  },
}));

import { AgentContextChangeRequestQueue } from "./agent-context-change-request-queue";

const agent = { id: "agent-1", name: "Kathrine" } as unknown as Agent;

const baseSnapshot = {
  instructions: "old",
  description: "",
  model: "claude-opus-4-8",
  thinking_level: "",
  persona_sandbox: "",
  skill_ids: [],
  custom_env_keys: [],
};

function makeRequest(
  status: AgentContextChangeRequest["status"],
): AgentContextChangeRequest {
  return {
    id: `cr-${status}`,
    agent_id: "agent-1",
    title: `A ${status} proposal`,
    description: "",
    base_version: "1.4.0",
    proposed_version: "1.5.0",
    proposed_snapshot: { ...baseSnapshot, instructions: "new" },
    status,
    proposed_by: "user-1",
    created_at: "2026-07-16T00:00:00Z",
  } as unknown as AgentContextChangeRequest;
}

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

function renderQueue() {
  return render(
    <AgentContextChangeRequestQueue
      agent={agent}
      wsId="ws-1"
      members={[]}
      canReview
    />,
    { wrapper },
  );
}

beforeEach(() => {
  mocks.impactSpy.mockReset();
  mocks.featureFlag = true;
  mocks.versions = [{ version: "1.4.0", snapshot: baseSnapshot }];
  mocks.requests = [];
});

describe("AgentContextChangeRequestQueue — approval consequences", () => {
  it("asks the panel about exactly the fields the diff found changed", async () => {
    mocks.requests = [makeRequest("pending")];
    renderQueue();

    await waitFor(() => expect(mocks.impactSpy).toHaveBeenCalled());
    expect(mocks.impactSpy).toHaveBeenCalledWith(["instructions"]);
  });

  // "What will approving this do?" is a question about a decision not yet made.
  // On a merged request it is already settled, and answering it against the
  // engine's CURRENT matrix would describe the past with today's facts.
  it("does not explain consequences for a settled request", async () => {
    mocks.requests = [makeRequest("merged")];
    renderQueue();

    await screen.findByText("A merged proposal");
    expect(screen.queryByTestId("approval-impact")).toBeNull();
    expect(mocks.impactSpy).not.toHaveBeenCalled();
  });

  it("stays out of the way when the capability flag is off", async () => {
    mocks.featureFlag = false;
    mocks.requests = [makeRequest("pending")];
    renderQueue();

    await screen.findByText("A pending proposal");
    expect(screen.queryByTestId("approval-impact")).toBeNull();
  });
});
