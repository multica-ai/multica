import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import type { PersonalAccessToken } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockListTokens = vi.hoisted(() => vi.fn());
const mockCreateToken = vi.hoisted(() => vi.fn());
const workspacesRef = vi.hoisted(() => ({
  current: [] as { id: string; name: string }[],
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: workspacesRef.current }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  workspaceListOptions: () => ({ queryKey: ["workspaces"] }),
}));
vi.mock("@multica/core/api", () => ({
  api: {
    listPersonalAccessTokens: mockListTokens,
    createPersonalAccessToken: mockCreateToken,
    revokePersonalAccessToken: vi.fn(),
  },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { TokensTab } from "./tokens-tab";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function token(overrides: Partial<PersonalAccessToken>): PersonalAccessToken {
  return {
    id: "token-1",
    name: "CI",
    token_prefix: "mul_abc12345",
    expires_at: null,
    last_used_at: null,
    created_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  workspacesRef.current = [
    { id: "workspace-1", name: "Acme" },
    { id: "workspace-2", name: "Globex" },
  ];
  mockListTokens.mockResolvedValue([]);
  mockCreateToken.mockResolvedValue({ ...token({}), token: "mwt_secret" });
});

async function createToken(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.type(screen.getByRole("textbox", { name: "Token name (e.g. My CLI)" }), name);
  await user.click(screen.getByRole("button", { name: "Create" }));
  await waitFor(() => expect(mockCreateToken).toHaveBeenCalledTimes(1));
}

describe("TokensTab — workspace scope", () => {
  it("mints an account-wide token unless a workspace is picked", async () => {
    const user = userEvent.setup();
    render(<TokensTab />, { wrapper: Wrapper });

    expect(screen.getByRole("combobox", { name: "Workspace access" })).toHaveTextContent("All workspaces");
    await createToken(user, "My CLI");

    expect(mockCreateToken).toHaveBeenCalledWith({
      name: "My CLI",
      expires_in_days: 90,
      workspace_id: undefined,
    });
  });

  it("mints a workspace token for the picked workspace", async () => {
    const user = userEvent.setup();
    render(<TokensTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("combobox", { name: "Workspace access" }));
    await user.click(await screen.findByRole("option", { name: "Globex" }));
    await createToken(user, "Globex CI");

    expect(mockCreateToken).toHaveBeenCalledWith({
      name: "Globex CI",
      expires_in_days: 90,
      workspace_id: "workspace-2",
    });
    // The picker goes back to the default, so the next token is not confined
    // to this workspace by accident. The created-token dialog is open over
    // the form by now, which hides the form from the accessibility tree.
    await waitFor(() =>
      expect(
        screen.getByRole("combobox", { name: "Workspace access", hidden: true }),
      ).toHaveTextContent("All workspaces"),
    );
  });

  it("names the workspace a token is confined to", async () => {
    mockListTokens.mockResolvedValue([
      token({ id: "token-1", name: "Everywhere", workspace_id: null }),
      token({ id: "token-2", name: "Globex only", token_prefix: "mwt_abc12345", workspace_id: "workspace-2" }),
      token({ id: "token-3", name: "Gone", token_prefix: "mwt_def12345", workspace_id: "workspace-9" }),
      // A server that predates workspace tokens omits the field.
      token({ id: "token-4", name: "Legacy" }),
    ]);
    render(<TokensTab />, { wrapper: Wrapper });

    const row = async (name: string) => (await screen.findByText(name)).parentElement!;
    expect(await row("Globex only")).toHaveTextContent("Globex only");
    expect((await row("Globex only")).textContent).toContain("· Globex only");
    // A workspace the list does not have still reads as confined.
    expect((await row("Gone")).textContent).toContain("· One workspace only");
    for (const name of ["Everywhere", "Legacy"]) {
      expect((await row(name)).textContent).not.toMatch(/only/);
    }
  });
});
