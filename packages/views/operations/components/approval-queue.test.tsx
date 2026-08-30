import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../test/i18n";
import { ApprovalQueue } from "./approval-queue";

const approval = {
  id: "11111111-1111-4111-8111-111111111111",
  workspace_id: "22222222-2222-4222-8222-222222222222",
  agent_id: "33333333-3333-4333-8333-333333333333",
  task_id: "44444444-4444-4444-8444-444444444444",
  invocation_id: "55555555-5555-4555-8555-555555555555",
  transport_kind: "managed_mcp" as const,
  server_key: "github",
  tool_name: "create_pull_request",
  schema_digest: `sha256:${"a".repeat(64)}`,
  policy_revision: 7,
  schema_field_names: ["owner", "repository", "title"],
  argument_bytes: 512,
  status: "pending" as const,
  requested_at: "2026-08-29T12:00:00Z",
  expires_at: "2026-08-29T12:05:00Z",
};

describe("ApprovalQueue", () => {
  it("submits machine-readable approve and deny reason codes", async () => {
    const user = userEvent.setup();
    const onDecision = vi.fn().mockResolvedValue(undefined);
    renderWithI18n(
      <ApprovalQueue approvals={[approval]} onDecision={onDecision} />,
    );

    expect(screen.getByText("github:create_pull_request")).toBeTruthy();
    expect(screen.getByText(/owner, repository, title/)).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Approve" }));
    expect(onDecision).toHaveBeenLastCalledWith(
      approval.id,
      "approve",
      "human_approved",
    );

    await user.click(screen.getByRole("button", { name: "Deny" }));
    expect(onDecision).toHaveBeenLastCalledWith(
      approval.id,
      "deny",
      "human_denied",
    );
  });
});
