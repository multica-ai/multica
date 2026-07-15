import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enSettings from "../../locales/en/settings.json";

const mockDelete = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    isLoading: false,
    data: [
      {
        id: "template-1",
        workspace_id: "workspace-1",
        name: "Daily digest",
        description: "Summarize the day",
        execution_mode: "create_issue",
        issue_title_template: "Digest {{date}}",
        trigger_kind: "schedule",
        cron_expression: "0 9 * * *",
        timezone: "UTC",
        created_by: "user-1",
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      },
    ],
  }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: "member" }),
}));
vi.mock("@multica/core/autopilots/queries", () => ({
  autopilotTemplateListOptions: () => ({ queryKey: ["autopilot-templates"] }),
}));
vi.mock("@multica/core/autopilots/mutations", () => ({
  useCreateAutopilotTemplate: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useUpdateAutopilotTemplate: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useDeleteAutopilotTemplate: () => ({ mutateAsync: mockDelete, isPending: false }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { AutopilotTemplatesTab } from "./autopilot-templates-tab";

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={{ en: { settings: enSettings } }}>
      {children}
    </I18nProvider>
  );
}

describe("AutopilotTemplatesTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockDelete.mockResolvedValue(undefined);
  });

  it("requires confirmation before deleting a template", async () => {
    const user = userEvent.setup();
    render(<AutopilotTemplatesTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: "Delete Daily digest" }));
    expect(mockDelete).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(screen.getByText("“Daily digest” will be removed. Existing Autopilots created from it are not affected.")).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Delete template" }));
    await waitFor(() => expect(mockDelete).toHaveBeenCalledWith("template-1"));
  });
});
