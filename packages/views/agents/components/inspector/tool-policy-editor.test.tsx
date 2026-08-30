import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../../test/i18n";
import { ToolPolicyEditor } from "./tool-policy-editor";

describe("ToolPolicyEditor", () => {
  it("edits exact identities and saves with the expected revision", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn().mockResolvedValue(undefined);
    renderWithI18n(
      <ToolPolicyEditor
        canEdit
        policy={{
          configured: true,
          revision: 12,
          status: "active",
          policy_digest: `sha256:${"b".repeat(64)}`,
          default_effect: "deny",
          rules: [],
        }}
        onSave={onSave}
      />,
    );

    expect(screen.getByText("Default deny")).toBeTruthy();
    expect(screen.getByText(/metadata only/i)).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Add rule" }));
    await user.type(screen.getByLabelText("Server key"), "github");
    await user.type(screen.getByLabelText("Tool name"), "create_pull_request");
    await user.type(
      screen.getByLabelText("Schema digest"),
      `sha256:${"c".repeat(64)}`,
    );
    await user.selectOptions(screen.getByLabelText("Effect"), "require_approval");
    await user.click(screen.getByRole("button", { name: "Save policy" }));

    expect(onSave).toHaveBeenCalledWith({
      expected_revision: 12,
      rules: [
        {
          transport_kind: "managed_mcp",
          server_key: "github",
          tool_name: "create_pull_request",
          schema_digest: `sha256:${"c".repeat(64)}`,
          effect: "require_approval",
        },
      ],
    });
  });
});
