import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { runQueueWaitingOn } from "@multica/core/issues/queries";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { RunQueueControl } from "./run-queue-control";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };
const setOne = vi.fn().mockResolvedValue({});
const preview = vi.fn();
const listIssues = vi.fn();

vi.mock("@multica/core/issues/mutations", () => ({
  useSetIssueRunQueue: () => ({ mutateAsync: setOne, isPending: false }),
  useBatchSetRunQueue: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    previewRunQueue: (...args: unknown[]) => preview(...args),
    listIssues: (...args: unknown[]) => listIssues(...args),
    listRunQueueWaits: () => Promise.resolve([]),
  },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

const candidates = [
  { id: "issue-2", identifier: "MUL-2", title: "Write the schema", status: "todo" },
  { id: "issue-3", identifier: "MUL-3", title: "Wire the API", status: "backlog" },
];

function renderControl(issue: { id: string; run_queued?: boolean; run_after_issue_ids?: string[] }) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <RunQueueControl issue={issue} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("runQueueWaitingOn", () => {
  it("lists the unfinished waits of a queued issue", () => {
    const waits = [{ issue_id: "issue-1", waiting_on_issue_ids: ["issue-2", "issue-3"] }];
    expect(runQueueWaitingOn("issue-1", waits)).toEqual(["issue-2", "issue-3"]);
    expect(runQueueWaitingOn("issue-9", waits)).toEqual([]);
    expect(runQueueWaitingOn("issue-1", undefined)).toEqual([]);
  });
});

describe("RunQueueControl", () => {
  beforeEach(() => {
    setOne.mockClear();
    preview.mockReset();
    preview.mockResolvedValue({ release_now: 0, run_count: 0 });
    listIssues.mockReset();
    listIssues.mockResolvedValue({ issues: candidates, total: candidates.length });
  });

  it("lists candidate issues as ID: Title and queues behind the selected ones", async () => {
    const user = userEvent.setup();
    renderControl({ id: "issue-1", run_queued: false, run_after_issue_ids: [] });

    await user.click(screen.getByRole("button", { name: "Queue" }));
    expect(await screen.findByRole("option", { name: "MUL-2: Write the schema" })).toBeInTheDocument();

    await user.click(screen.getByRole("option", { name: /MUL-2: Write the schema/ }));
    await user.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(setOne).toHaveBeenCalledWith({
        id: "issue-1",
        queue: { queued: true, afterIssueIds: ["issue-2"] },
      }),
    );
  });

  it("queues with no waits when nothing is selected", async () => {
    const user = userEvent.setup();
    renderControl({ id: "issue-1", run_queued: false, run_after_issue_ids: [] });

    await user.click(screen.getByRole("button", { name: "Queue" }));
    await user.click(await screen.findByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(setOne).toHaveBeenCalledWith({ id: "issue-1", queue: { queued: true, afterIssueIds: [] } }),
    );
  });

  it("takes a queued issue out of the queue", async () => {
    const user = userEvent.setup();
    renderControl({ id: "issue-1", run_queued: true, run_after_issue_ids: ["issue-2"] });

    await user.click(screen.getByRole("button", { name: "Queued" }));
    await user.click(await screen.findByRole("button", { name: "Take out" }));

    await waitFor(() =>
      expect(setOne).toHaveBeenCalledWith({ id: "issue-1", queue: { queued: false, afterIssueIds: [] } }),
    );
  });
});
