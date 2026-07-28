import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { approvalSchema } from "../../core/types";

const useQueryMock = vi.fn();
const memberState = { role: "member" };
const featureFlagState = { inbox: true, gate: true };

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: (...args: unknown[]) => useQueryMock(...args),
}));

vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: memberState.role, isLoading: false }),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFlagValue: (key: string) =>
    key === "cerebro_approval_gate" ? featureFlagState.gate : featureFlagState.inbox,
}));

vi.mock("./inline-approval-card", () => ({
  InlineApprovalCard: ({
    approval,
    canDecide,
  }: {
    approval: { id: string };
    canDecide?: boolean;
  }) => (
    <div data-testid="inline-approval" data-can-decide={String(canDecide)}>
      {approval.id}
    </div>
  ),
}));

import { InlineApprovalCards } from "./inline-approval-cards";

const approval = (id: string, taskId: string) =>
  approvalSchema.parse({ id, status: "pending", task_id: taskId });

describe("InlineApprovalCards", () => {
  beforeEach(() => {
    useQueryMock.mockReset();
    featureFlagState.inbox = true;
    featureFlagState.gate = true;
    memberState.role = "member";
  });

  it("does not start the approvals query when the feature flag is off", () => {
    featureFlagState.inbox = false;
    featureFlagState.gate = false;

    const { container } = render(
      <InlineApprovalCards wsId="workspace-1" origin={{ issue_id: "issue-1" }} />,
    );

    expect(container).toBeEmptyDOMElement();
    expect(useQueryMock).not.toHaveBeenCalled();
  });

  it("stays visible when Ask enforcement is on even if the inbox flag is off", () => {
    featureFlagState.inbox = false;
    featureFlagState.gate = true;
    useQueryMock.mockReturnValue({
      data: { approvals: [approval("approval-a", "task-a")] },
      isLoading: false,
    });

    render(<InlineApprovalCards wsId="workspace-1" origin={{ issue_id: "issue-1" }} />);

    expect(screen.getByText("approval-a")).toBeInTheDocument();
  });

  it("queries once for the origin scope and renders only approvals matching the turn", () => {
    useQueryMock.mockReturnValue({
      data: { approvals: [approval("approval-a", "task-a"), approval("approval-b", "task-b")] },
      isLoading: false,
    });

    render(
      <InlineApprovalCards
        wsId="workspace-1"
        origin={{ chat_session_id: "chat-1", surface: "chat" }}
        match={{ task_id: "task-b" }}
      />,
    );

    expect(screen.queryByText("approval-a")).not.toBeInTheDocument();
    expect(screen.getByText("approval-b")).toBeInTheDocument();
  });

  it("renders nothing while the shared scope query is loading", () => {
    useQueryMock.mockReturnValue({ data: undefined, isLoading: true });
    const { container } = render(
      <InlineApprovalCards wsId="workspace-1" origin={{ issue_id: "issue-1" }} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it.each([
    ["member", "false"],
    ["admin", "true"],
    ["owner", "true"],
  ])("sets decision access from the %s workspace role", (role, expected) => {
    memberState.role = role;
    useQueryMock.mockReturnValue({
      data: { approvals: [approval("approval-a", "task-a")] },
      isLoading: false,
    });

    render(
      <InlineApprovalCards
        wsId="workspace-1"
        origin={{ chat_session_id: "chat-1", surface: "chat" }}
      />,
    );

    expect(screen.getByTestId("inline-approval")).toHaveAttribute(
      "data-can-decide",
      expected,
    );
  });
});
