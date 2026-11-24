import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { DocTaskRun } from "@multica/core/types";
import { DocThreadRun } from "./doc-thread-runs";
import enAgents from "../../locales/en/agents.json";
import enDocs from "../../locales/en/docs.json";
import enIssues from "../../locales/en/issues.json";

const { apiMock } = vi.hoisted(() => ({ apiMock: { cancelTaskById: vi.fn() } }));
vi.mock("@multica/core/api", () => ({ api: apiMock }));
vi.mock("@multica/core/hooks", async (original) => ({
  ...await original<object>(), useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Writer" }),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function run(status: string, extra: Partial<DocTaskRun> = {}): DocTaskRun {
  return {
    id: "t1", agentId: "a1", docThreadId: "c1", status, failureReason: "", error: null,
    createdAt: "", completedAt: null, canCancel: true, ...extra,
  };
}

function mount(task: DocTaskRun) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={{ en: { agents: enAgents, docs: enDocs, issues: enIssues } }}>
        <DocThreadRun docId="d1" run={task} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

it("stops a working run after confirmation", async () => {
  apiMock.cancelTaskById.mockResolvedValue({});
  mount(run("running"));
  expect(screen.getByText("Writer")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Stop" }));
  fireEvent.click(await screen.findByRole("button", { name: "Stop run" }));
  await waitFor(() => expect(apiMock.cancelTaskById).toHaveBeenCalledWith("t1"));
});

it("offers no Stop to a viewer who cannot see the agent", () => {
  mount(run("running", { canCancel: false }));
  expect(screen.queryByRole("button", { name: "Stop" })).toBeNull();
});

it("shows how a failed run ended, with no way to stop it", () => {
  mount(run("failed", { failureReason: "timeout", error: "deadline exceeded" }));
  expect(screen.getByRole("status").textContent).not.toBe("");
  expect(screen.queryByRole("button", { name: "Stop" })).toBeNull();
});

it("shows a pending retry as stoppable", () => {
  mount(run("deferred"));
  expect(screen.getByText("Waiting to retry")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Stop" })).toBeTruthy();
});
