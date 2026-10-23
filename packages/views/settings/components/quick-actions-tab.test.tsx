import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { renderWithI18n } from "../../test/i18n";
import { QuickActionsTab } from "./quick-actions-tab";

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [], isLoading: false }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/quick-actions", () => ({
  quickActionListOptions: () => ({}),
  useCreateQuickAction: () => ({ mutateAsync: vi.fn() }),
  useUpdateQuickAction: () => ({ mutateAsync: vi.fn() }),
  useDeleteQuickAction: () => ({ mutateAsync: vi.fn() }),
}));

vi.mock("../../autopilots/components/pickers/agent-picker", () => ({
  AgentPicker: () => <div />,
}));

describe("QuickActionsTab", () => {
  it("keeps a long prompt scrollable inside the dialog", () => {
    renderWithI18n(<QuickActionsTab />);

    fireEvent.click(screen.getByRole("button", { name: "New quick action" }));

    const prompt = screen.getByLabelText("Prompt");
    expect(prompt).toHaveClass("max-h-48", "resize-y");
  });
});

