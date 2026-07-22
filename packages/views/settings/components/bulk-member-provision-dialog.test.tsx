// @vitest-environment jsdom

import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockProvisionMembers = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { provisionMembers: mockProvisionMembers },
}));

import { BulkMemberProvisionDialog } from "./bulk-member-provision-dialog";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function renderDialog(onCompleted = vi.fn()) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <BulkMemberProvisionDialog
        workspaceId="workspace-1"
        open
        onOpenChange={vi.fn()}
        onCompleted={onCompleted}
      />
    </I18nProvider>,
  );
}

describe("BulkMemberProvisionDialog", () => {
  beforeEach(() => {
    mockProvisionMembers.mockReset();
  });

  it("previews a seed cohort and provisions normalized members without invitations", async () => {
    const onCompleted = vi.fn();
    mockProvisionMembers.mockResolvedValue({
      summary: {
        total: 2,
        created: 1,
        already_member: 1,
        duplicate: 0,
        invalid: 0,
        failed: 0,
      },
      results: [
        { email: "alice@company.com", role: "member", status: "created" },
        { email: "bob@company.com", role: "member", status: "already_member" },
      ],
    });

    renderDialog(onCompleted);
    await userEvent.type(
      screen.getByRole("textbox", { name: /email addresses/i }),
      "Alice@Company.com\nbob@company.com",
    );

    expect(screen.getByText(/2 valid emails/i)).toBeInTheDocument();
    expect(screen.getByText(/no invitation emails/i)).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /provision members/i }));

    await waitFor(() => {
      expect(mockProvisionMembers).toHaveBeenCalledWith("workspace-1", {
        entries: [
          { email: "alice@company.com", role: "member" },
          { email: "bob@company.com", role: "member" },
        ],
      });
    });
    expect(await screen.findByText(/2 members provisioned/i)).toBeInTheDocument();
    expect(onCompleted).toHaveBeenCalledTimes(1);
  });

  it("blocks submission when the list contains invalid emails", async () => {
    renderDialog();
    await userEvent.type(
      screen.getByRole("textbox", { name: /email addresses/i }),
      "valid@company.com\ninvalid",
    );

    expect(screen.getByText(/1 invalid email/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /provision members/i })).toBeDisabled();
    expect(mockProvisionMembers).not.toHaveBeenCalled();
  });
});
