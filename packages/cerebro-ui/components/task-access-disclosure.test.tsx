// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import type React from "react";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "@multica/core/api";
import type { TaskAccessSnapshot } from "@multica/core/types";

import { TaskAccessDisclosure } from "./task-access-disclosure";

function renderDisclosure(node: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
}

const snapshot = {
  enforcement_enabled: false,
  task_id: "task-1",
  agent_id: "agent-1",
  allowed_tools: ["get_issue"],
  issued_at: "2026-08-02T19:00:00Z",
  expires_at: "2026-08-02T21:00:00Z",
  status: "active" as const,
  claim_generation: 3,
  lifecycle_state: "finalized" as const,
  offered_count: 2,
  authorized_count: 1,
  diagnostics: [{
    code: "task_partial",
    state: "partial",
    title: "Partial task access",
    message: "One offered capability was removed before the Task Mandate was finalized.",
    affected_capability: "1 capability",
    source_policy: "Task Mandate",
    recovery_action: "Review Permissions and start a new task after changing access.",
  }],
} satisfies TaskAccessSnapshot;

describe("TaskAccessDisclosure", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("shows loading and then the observation-only frozen ceiling", async () => {
    let resolve!: (value: typeof snapshot) => void;
    const load = vi.fn(() => new Promise<typeof snapshot>((done) => { resolve = done; }));
    renderDisclosure(<TaskAccessDisclosure taskId="task-1" load={load} />);

    expect(screen.getByText("Loading Task access…")).toBeInTheDocument();
    resolve(snapshot);

    expect(await screen.findByText("Observation only")).toBeInTheDocument();
    expect(screen.getByText("Partial task access")).toBeInTheDocument();
    expect(screen.getByText(/a later Deny or safety ceiling can still tighten this run/)).toBeInTheDocument();
    expect(screen.getByText(/a later Allow cannot widen its frozen Task Mandate/)).toBeInTheDocument();
  });

  it("loads once after the default loader settles", async () => {
    const client = new ApiClient("https://api.example.test");
    const load = vi.spyOn(client, "getTaskAccess").mockResolvedValue(snapshot);
    setApiInstance(client);

    renderDisclosure(<TaskAccessDisclosure taskId="task-1" />);

    expect(await screen.findByText("Observation only")).toBeInTheDocument();
    await waitFor(() => expect(load).toHaveBeenCalledTimes(1));
  });

  it("shows a recoverable error instead of hiding Task access", async () => {
    renderDisclosure(
      <TaskAccessDisclosure
        taskId="task-1"
        load={vi.fn().mockRejectedValue(new Error("network unavailable"))}
      />,
    );

    expect(await screen.findByText("Task access unavailable")).toBeInTheDocument();
    expect(screen.getByText("task:task-1")).toBeInTheDocument();
    expect(screen.getByText("Task Mandate API")).toBeInTheDocument();
    expect(screen.getByText(/Retry the task claim/)).toBeInTheDocument();
  });

  it("renders the shared missing-snapshot diagnostic returned by the API", async () => {
    const error = Object.assign(new Error("task access snapshot not found"), {
      body: {
        diagnostics: [{
          code: "task_mandate_missing",
          state: "unavailable",
          title: "Task access snapshot missing",
          message: "No persisted Task Mandate exists for this task.",
          affected_capability: "task:task-1",
          source_policy: "Task Mandate",
          recovery_action: "Retry the task claim.",
        }],
      },
    });
    renderDisclosure(<TaskAccessDisclosure taskId="task-1" load={vi.fn().mockRejectedValue(error)} />);

    expect(await screen.findByText("Task access snapshot missing")).toBeInTheDocument();
    expect(screen.getByText("Task Mandate")).toBeInTheDocument();
    expect(screen.getByText("Retry the task claim.")).toBeInTheDocument();
  });

  it("shows the empty capability state explicitly", async () => {
    renderDisclosure(
      <TaskAccessDisclosure
        taskId="task-1"
        load={vi.fn().mockResolvedValue({
          ...snapshot,
          allowed_tools: [],
          offered_count: 0,
          authorized_count: 0,
          diagnostics: [],
        })}
      />,
    );

    expect(await screen.findByText("No capabilities were frozen for this run.")).toBeInTheDocument();
  });
});
