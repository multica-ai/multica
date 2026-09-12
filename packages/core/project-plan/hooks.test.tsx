/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { ProjectPlanOverview } from "../types";
import { usePlanOverview } from "./hooks";

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

function makeOverview(): ProjectPlanOverview {
  return {
    plan: {
      id: "plan-1",
      workspace_id: "ws-1",
      project_id: "project-1",
      version: 1,
      kind: "prd",
      origin: "orchestrator",
      title: "Launch",
      description: "",
      attributes: null,
      source_issue_id: null,
      superseded: false,
      superseded_at: null,
      created_by_type: "agent",
      created_by_id: "agent-1",
      created_at: "",
      updated_at: "",
    },
    rollup: {
      tasks_done: 0, tasks_total: 0, percent: 0,
      parts_covered: 0, parts_total: 0, parts_without_tasks: 0,
    },
    phases: [],
    dependencies: [],
    uncovered_parts: [],
  };
}

// usePlanOverview must resolve to exactly one of four states — loading,
// error, no-plan (a genuine 404) or present — and error/no-plan must never
// collapse into each other: a failed request that silently reads as "no
// plan" reintroduces the false-empty-state bug this slice exists to close.
describe("usePlanOverview", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => {
    qc.clear();
  });

  it("starts loading before the request settles", () => {
    setApiInstance({
      getActiveProjectPlan: () => new Promise(() => {}),
    } as unknown as ApiClient);

    const { result } = renderHook(() => usePlanOverview("ws-1", "project-1"), {
      wrapper: createWrapper(qc),
    });

    expect(result.current.status).toBe("loading");
  });

  it("resolves to no-plan on a genuine 404 (null), not error", async () => {
    setApiInstance({
      getActiveProjectPlan: () => Promise.resolve(null),
    } as unknown as ApiClient);

    const { result } = renderHook(() => usePlanOverview("ws-1", "project-1"), {
      wrapper: createWrapper(qc),
    });

    await waitFor(() => expect(result.current.status).toBe("no-plan"));
  });

  it("resolves to error on a failed request, not no-plan", async () => {
    setApiInstance({
      getActiveProjectPlan: () => Promise.reject(new Error("network error")),
    } as unknown as ApiClient);

    const { result } = renderHook(() => usePlanOverview("ws-1", "project-1"), {
      wrapper: createWrapper(qc),
    });

    await waitFor(() => expect(result.current.status).toBe("error"));
  });

  it("resolves to present with the plan data when the API returns a plan", async () => {
    const overview = makeOverview();
    setApiInstance({
      getActiveProjectPlan: () => Promise.resolve(overview),
    } as unknown as ApiClient);

    const { result } = renderHook(() => usePlanOverview("ws-1", "project-1"), {
      wrapper: createWrapper(qc),
    });

    await waitFor(() => expect(result.current.status).toBe("present"));
    expect(result.current.status === "present" && result.current.data).toEqual(overview);
  });
});
