import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockListPersonalAccessTokens = vi.hoisted(() => vi.fn());
const mockCreatePersonalAccessToken = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    listPersonalAccessTokens: mockListPersonalAccessTokens,
    createPersonalAccessToken: mockCreatePersonalAccessToken,
    revokePersonalAccessToken: vi.fn(),
  },
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children, open }: { children: ReactNode; open: boolean }) =>
    open ? <div>{children}</div> : null,
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
  DialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
  DialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

import { TokensTab } from "./tokens-tab";

const TEST_RESOURCES = {
  en: { common: enCommon, settings: enSettings },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderTokensTab() {
  return render(<TokensTab />, { wrapper: I18nWrapper });
}

function getDialogCreateButton() {
  return screen.getAllByRole("button", { name: "Create" }).at(-1)!;
}

describe("TokensTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListPersonalAccessTokens.mockResolvedValue([]);
  });

  it("opens the token creation form from an enabled page action", async () => {
    const user = userEvent.setup();
    renderTokensTab();

    const createButton = screen.getByRole("button", { name: "Create" });
    expect(createButton).toBeEnabled();

    await user.click(createButton);

    expect(screen.getByRole("heading", { name: "Create API Token" })).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toBeInTheDocument();
    expect(getDialogCreateButton()).toBeDisabled();
  });

  it("creates a token from the dialog after a name is entered", async () => {
    mockCreatePersonalAccessToken.mockResolvedValue({
      id: "tok-1",
      name: "CLI",
      token: "pat_secret",
      token_prefix: "pat",
      created_at: "2026-07-01T00:00:00Z",
      last_used_at: null,
      expires_at: null,
    });

    const user = userEvent.setup();
    renderTokensTab();

    await user.click(screen.getByRole("button", { name: "Create" }));
    await user.type(screen.getByLabelText("Name"), "CLI");
    await user.click(getDialogCreateButton());

    await waitFor(() => {
      expect(mockCreatePersonalAccessToken).toHaveBeenCalledWith({
        name: "CLI",
        expires_in_days: 90,
      });
    });
    expect(screen.getByRole("heading", { name: "Token created" })).toBeInTheDocument();
    expect(screen.getByText("pat_secret")).toBeInTheDocument();
  });
});
