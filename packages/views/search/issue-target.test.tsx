import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses/queries";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Issue } from "@multica/core/types";
import { RESOURCES } from "../locales";
import { useIssueTargetWriter } from "./issue-target";
import type { IssueTarget } from "./types";

const { mockUpdateIssue, mockToast, mockOpenModal } = vi.hoisted(() => ({
  mockUpdateIssue: vi.fn(),
  mockToast: Object.assign(vi.fn(), { success: vi.fn(), error: vi.fn(), warning: vi.fn() }),
  mockOpenModal: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { updateIssue: mockUpdateIssue },
}));
vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-test",
}));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog([]),
}));
vi.mock("@multica/core/modals", async (importOriginal) => {
  const state = { open: mockOpenModal };
  return {
    ...(await importOriginal<typeof import("@multica/core/modals")>()),
    useModalStore: Object.assign((selector?: (s: typeof state) => unknown) => (selector ? selector(state) : state), {
      getState: () => state,
    }),
  };
});
vi.mock("sonner", () => ({ toast: mockToast }));

const issue = {
  id: "issue-1",
  workspace_id: "ws-test",
  identifier: "MUL-1",
  title: "Demo",
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
} as unknown as Issue;

function renderWriter(target: IssueTarget) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={{ en: RESOURCES.en }}>
        {children}
      </I18nProvider>
    </QueryClientProvider>
  );
  return renderHook(() => useIssueTargetWriter(target), { wrapper });
}

describe("useIssueTargetWriter", () => {
  beforeEach(() => {
    mockUpdateIssue.mockReset();
    mockToast.error.mockReset();
    mockOpenModal.mockReset();
  });

  // The palette closes — unmounting the writer — as soon as a value is picked,
  // so a failed write must still reach the user after the unmount.
  it("reports a failed single-issue write after the palette has unmounted", async () => {
    let reject!: (err: Error) => void;
    mockUpdateIssue.mockReturnValue(new Promise((_, r) => (reject = r)));
    const { result, unmount } = renderWriter({ kind: "single", issue });

    result.current.apply({ priority: "high" });
    unmount();
    reject(new Error("Network down"));

    await waitFor(() => expect(mockToast.error).toHaveBeenCalledWith("Network down"));
    expect(mockUpdateIssue).toHaveBeenCalledWith("issue-1", { priority: "high" });
  });

  it("still asks before handing a live issue to an agent", () => {
    const { result } = renderWriter({ kind: "single", issue });

    result.current.assign("agent", "agent-1");

    expect(mockOpenModal).toHaveBeenCalledWith(
      "issue-run-confirm",
      expect.objectContaining({ issueIds: ["issue-1"], assigneeType: "agent", assigneeId: "agent-1" }),
    );
    expect(mockUpdateIssue).not.toHaveBeenCalled();
  });
});
