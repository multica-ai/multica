import type { ReactNode } from "react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const mockSetQueryData = vi.hoisted(() => vi.fn());
const workspaceRef = vi.hoisted(() => ({
  current: {
    id: "workspace-1",
    name: "Acme",
    slug: "acme",
    settings: {
      github_enabled: true,
      knowledge_curator: {
        enabled: false,
        provider: "openai",
        model: "gpt-test",
        embedding_model: "embed-test",
        runtime_mode: "external",
        base_url: "https://api.example.com/v1",
        secret_ref: "secret://workspace/curator",
      },
      knowledge_rag: {
        auto_inject: true,
        limit: 5,
        type_filters: [],
        confidence_threshold: "high",
        curator_runtime_policy: "workspace_default",
        token_budget: 2000,
      },
    } as Record<string, unknown>,
    repos: [] as { url: string }[],
  },
}));
const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "owner" as "owner" | "admin" | "member" }],
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: membersRef.current }),
  useQueryClient: () => ({
    setQueryData: mockSetQueryData,
  }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => workspaceRef.current,
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@multica/core/api", () => ({
  api: { updateWorkspace: mockUpdateWorkspace },
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (sel?: (s: { user: { id: string } }) => unknown) =>
      sel ? sel({ user: { id: "user-1" } }) : { user: { id: "user-1" } },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { CuratorTab } from "./curator-tab";

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

function resetFixtures() {
  vi.clearAllMocks();
  workspaceRef.current = {
    id: "workspace-1",
    name: "Acme",
    slug: "acme",
    settings: {
      github_enabled: true,
      knowledge_curator: {
        enabled: false,
        provider: "openai",
        model: "gpt-test",
        embedding_model: "embed-test",
        runtime_mode: "external",
        base_url: "https://api.example.com/v1",
        secret_ref: "secret://workspace/curator",
      },
      knowledge_rag: {
        auto_inject: true,
        limit: 5,
        type_filters: [],
        confidence_threshold: "high",
        curator_runtime_policy: "workspace_default",
        token_budget: 2000,
      },
    },
    repos: [],
  };
  membersRef.current = [{ user_id: "user-1", role: "owner" }];
  mockUpdateWorkspace.mockImplementation(async (_id: string, payload: { settings: Record<string, unknown> }) => ({
    ...workspaceRef.current,
    settings: payload.settings,
  }));
}

describe("CuratorTab", () => {
  beforeEach(resetFixtures);

  it("merges and saves knowledge_curator settings", async () => {
    const user = userEvent.setup();
    render(<CuratorTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("switch", { name: /enable knowledge curator/i }));
    const provider = screen.getByPlaceholderText("openai, anthropic, custom");
    await user.clear(provider);
    await user.type(provider, "custom");
    await user.click(screen.getByRole("button", { name: /^Save$/ }));

    await waitFor(() => expect(mockUpdateWorkspace).toHaveBeenCalledTimes(1));
    expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
      settings: expect.objectContaining({
        github_enabled: true,
        knowledge_curator: expect.objectContaining({
          enabled: true,
          provider: "custom",
          model: "gpt-test",
          secret_ref: "secret://workspace/curator",
        }),
      }),
    });
  });

  it("does not allow members to save curator settings", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    render(<CuratorTab />, { wrapper: I18nWrapper });

    expect(screen.getByRole("switch", { name: /enable knowledge curator/i })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    expect(screen.queryByRole("button", { name: /^Save$/ })).toBeNull();
    expect(screen.getByText("Only admins and owners can update Curator settings.")).toBeTruthy();
  });

  it("renders token budget input with default value", () => {
    render(<CuratorTab />, { wrapper: I18nWrapper });
    const tokenBudgetInput = screen.getByDisplayValue("2000");
    expect(tokenBudgetInput).toBeTruthy();
    expect(tokenBudgetInput).toHaveAttribute("type", "number");
  });

  it("shows rebuild hint when model or embedding model changes", async () => {
    const user = userEvent.setup();
    render(<CuratorTab />, { wrapper: I18nWrapper });

    const modelInput = screen.getByPlaceholderText("Model used to write drafts");
    await user.clear(modelInput);
    await user.type(modelInput, "gpt-4-new");

    expect(screen.getByText(/Model or embedding model has changed/)).toBeTruthy();
  });

  it("does not show rebuild hint when only non-model fields change", async () => {
    const user = userEvent.setup();
    render(<CuratorTab />, { wrapper: I18nWrapper });

    const providerInput = screen.getByPlaceholderText("openai, anthropic, custom");
    await user.clear(providerInput);
    await user.type(providerInput, "custom-v2");

    expect(screen.queryByText(/Model or embedding model has changed/)).toBeNull();
  });
});
