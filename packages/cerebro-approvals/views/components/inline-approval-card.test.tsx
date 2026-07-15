import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { approvalSchema } from "../../core/types";

const approveMutate = vi.fn();
const rejectMutate = vi.fn();
const mutationState = { approvePending: false, rejectPending: false };

vi.mock("../../core/mutations", () => ({
  useApproveApproval: () => ({ mutate: approveMutate, isPending: mutationState.approvePending }),
  useRejectApproval: () => ({ mutate: rejectMutate, isPending: mutationState.rejectPending }),
}));

import { InlineApprovalCard } from "./inline-approval-card";

const approval = (status: string) =>
  approvalSchema.parse({
    id: "approval-1",
    capability: "publish_campaign",
    reason: "Needs owner review",
    status,
    requester_name: "Campaign agent",
    surface: "chat",
  });

beforeEach(() => {
  approveMutate.mockReset();
  rejectMutate.mockReset();
  mutationState.approvePending = false;
  mutationState.rejectPending = false;
});

describe("InlineApprovalCard", () => {
  it("renders loading state", () => {
    render(<InlineApprovalCard approval={null} loading />);
    expect(screen.getByText("Loading approval…")).toBeInTheDocument();
  });

  it("lets an owner approve or reject a pending request inline", () => {
    const first = render(<InlineApprovalCard approval={approval("pending")} canDecide />);
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    first.unmount();
    render(<InlineApprovalCard approval={approval("pending")} canDecide />);
    fireEvent.click(screen.getByRole("button", { name: "Reject" }));
    expect(approveMutate).toHaveBeenCalledWith({ id: "approval-1" });
    expect(rejectMutate).toHaveBeenCalledWith({ id: "approval-1" });
  });

  it("shows a pending request to a member without decision controls", () => {
    render(<InlineApprovalCard approval={approval("pending")} canDecide={false} />);
    expect(screen.getByText("Needs owner review")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reject" })).not.toBeInTheDocument();
  });

  it.each(["approved", "rejected"])("renders %s as terminal without decision buttons", (status) => {
    render(<InlineApprovalCard approval={approval(status)} />);
    expect(screen.getByText(new RegExp(status, "i"))).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reject" })).not.toBeInTheDocument();
  });

  it("locks both decisions while another decision is in flight", () => {
    mutationState.approvePending = true;
    render(<InlineApprovalCard approval={approval("pending")} canDecide />);
    expect(screen.getByRole("button", { name: "Approve" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Reject" })).toBeDisabled();
    expect(screen.getByText("Deciding…")).toBeInTheDocument();
  });
});
