import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enWorkspace from "../locales/en/workspace.json";
import { NewWorkspacePage } from "./new-workspace-page";

const TEST_RESOURCES = {
  en: { common: enCommon, workspace: enWorkspace },
};

let mockUser: { email: string } | null = { email: "wrong-account@gmail.com" };

vi.mock("../auth", () => ({
  useLogout: () => vi.fn(),
}));

vi.mock("../platform", () => ({
  DragStrip: () => null,
}));

vi.mock("./create-workspace-form", () => ({
  CreateWorkspaceForm: () => <div data-testid="create-workspace-form" />,
}));

vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (s: { workspaceCreationDisabled: boolean }) => unknown) =>
    selector({ workspaceCreationDisabled: false }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { email: string } | null }) => unknown) =>
    selector({ user: mockUser }),
}));

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderPage() {
  return render(<NewWorkspacePage onSuccess={vi.fn()} />, { wrapper: I18nWrapper });
}

describe("NewWorkspacePage", () => {
  it("shows the signed-in email next to Log out so a wrong-account user spots it", () => {
    mockUser = { email: "wrong-account@gmail.com" };
    renderPage();
    expect(screen.getByText("wrong-account@gmail.com")).toBeTruthy();
    expect(screen.getByRole("button", { name: /log out/i })).toBeTruthy();
  });

  it("renders the Log out escape even when no email is available", () => {
    mockUser = null;
    renderPage();
    expect(screen.queryByText("wrong-account@gmail.com")).toBeNull();
    expect(screen.getByRole("button", { name: /log out/i })).toBeTruthy();
  });
});
