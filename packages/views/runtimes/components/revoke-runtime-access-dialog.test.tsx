import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError } from "@multica/core/api";
import type { AgentRuntime } from "@multica/core/types";
import enRuntimes from "../../locales/en/runtimes.json";
import { RevokeRuntimeAccessDialog, parseRuntimeAccessRevocationPlan } from "./revoke-runtime-access-dialog";

const revoke = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/core/api")>(),
  api: { revokeRuntimeWorkspaceAccess: revoke },
}));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

const agentId = "00000000-0000-4000-8000-000000000001";
const taskId = "00000000-0000-4000-8000-000000000002";
const nextTaskId = "00000000-0000-4000-8000-000000000003";
const body = { code: "runtime_has_nonowner_dependents", nonowner_agents: [{ id: agentId, name: "Teammate" }], active_task_ids: [taskId] };
const conflict = (value: unknown) => new ApiError("conflict", 409, "Conflict", value);

describe("runtime access revocation confirmation", () => {
  beforeEach(() => { revoke.mockReset(); });

  it("rejects missing, malformed, duplicate and unknown confirmation data", () => {
    for (const value of [
      null, {}, { ...body, code: "future_code" },
      { ...body, nonowner_agents: undefined }, { ...body, active_task_ids: undefined },
      { ...body, nonowner_agents: [{ id: agentId }] },
      { ...body, nonowner_agents: [null] }, { ...body, active_task_ids: ["not-a-uuid"] },
      { ...body, active_task_ids: [taskId, taskId] },
    ]) expect(parseRuntimeAccessRevocationPlan(conflict(value))).toBeNull();
    expect(parseRuntimeAccessRevocationPlan(new ApiError("error", 500, "Error", body))).toBeNull();
    expect(parseRuntimeAccessRevocationPlan(conflict(body))).toEqual({ agents: body.nonowner_agents, activeTaskIds: [taskId] });
  });

  it("requires renewed consent and submits the refreshed exact plan after a 409", async () => {
    const close = vi.fn();
    revoke.mockRejectedValueOnce(conflict({ ...body, code: "runtime_access_revocation_plan_changed", active_task_ids: [taskId, nextTaskId] })).mockResolvedValueOnce(undefined);
    const runtime: AgentRuntime = {
      id: "runtime-1", workspace_id: "ws-1", name: "Local Runtime", custom_name: "My machine",
      daemon_id: null, runtime_mode: "local", provider: "claude", launch_header: "", status: "online",
      device_info: "", metadata: {}, owner_id: "owner", visibility: "public", last_seen_at: null,
      created_at: "2026-09-05T00:00:00Z", updated_at: "2026-09-05T00:00:00Z",
    };
    render(<I18nProvider locale="en" resources={{ en: { runtimes: enRuntimes } }}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}>
        <RevokeRuntimeAccessDialog open onOpenChange={close} runtime={runtime} wsId="ws-1" initialPlan={{ agents: body.nonowner_agents, activeTaskIds: [taskId] }} />
      </QueryClientProvider>
    </I18nProvider>);
    expect(screen.getByRole("alertdialog", { name: "Make this Runtime private?" })).toHaveAccessibleDescription(/pause active autopilots/);
    const confirm = screen.getByRole("button", { name: "Make Runtime private" });
    expect(confirm).toBeDisabled();
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(confirm);
    expect(await screen.findByRole("status")).toHaveTextContent("Review it and confirm again");
    expect(confirm).toBeDisabled();
    expect(close).not.toHaveBeenCalled();
    expect(revoke).toHaveBeenNthCalledWith(1, runtime.id, [agentId], [taskId]);
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(confirm);
    await waitFor(() => expect(close).toHaveBeenCalledWith(false));
    expect(revoke).toHaveBeenNthCalledWith(2, runtime.id, [agentId], [taskId, nextTaskId]);
  });
});
