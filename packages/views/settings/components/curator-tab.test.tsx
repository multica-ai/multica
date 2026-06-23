import type { ReactNode } from "react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const mockProbeKnowledgeCurator = vi.hoisted(() => vi.fn());
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
        chat: {
          provider: "openai",
          model: "gpt-test",
          base_url: "https://chat.example.com/v1",
        },
        embedding: {
          provider: "openai",
          model: "embed-test",
          base_url: "https://embedding.example.com/v1",
          dimensions: 1536,
        },
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
  api: {
    updateWorkspace: mockUpdateWorkspace,
    probeKnowledgeCurator: mockProbeKnowledgeCurator,
  },
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
        chat: {
          provider: "openai",
          model: "gpt-test",
          base_url: "https://chat.example.com/v1",
        },
        embedding: {
          provider: "openai",
          model: "embed-test",
          base_url: "https://embedding.example.com/v1",
          dimensions: 1536,
        },
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
  mockProbeKnowledgeCurator.mockResolvedValue({
    chat_status: {
      provider: "openai",
      model: "gpt-4.1-mini",
      supported: true,
    },
    embedding_status: {
      provider: "openai",
      model: "text-embedding-3-small",
      dimensions: 1536,
      supported: true,
    },
  });
}

describe("CuratorTab", () => {
  beforeEach(resetFixtures);

  it("merges and saves knowledge_curator settings", async () => {
    const user = userEvent.setup();
    render(<CuratorTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("switch", { name: /enable knowledge curator/i }));
    const provider = screen.getAllByPlaceholderText("openai, deepseek, ollama, custom")[0]!;
    await user.clear(provider);
    await user.type(provider, "custom");
    await user.click(screen.getByRole("button", { name: /^Save$/ }));

    await waitFor(() => expect(mockUpdateWorkspace).toHaveBeenCalledTimes(1));
    const call = mockUpdateWorkspace.mock.calls[0];
    if (!call) throw new Error("expected updateWorkspace call");
    const payload = call[1] as { settings: Record<string, unknown> };
    const curator = payload.settings.knowledge_curator as Record<string, unknown>;
    const chat = curator.chat as Record<string, unknown>;
    const rag = payload.settings.knowledge_rag as Record<string, unknown>;
    expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
      settings: expect.objectContaining({
        github_enabled: true,
        knowledge_curator: expect.objectContaining({
          enabled: true,
          chat: expect.objectContaining({
            provider: "custom",
            model: "gpt-test",
          }),
        }),
      }),
    });
    expect(chat.secret_ref).toBeUndefined();
    expect(curator.runtime_mode).toBeUndefined();
    expect(rag.curator_runtime_policy).toBeUndefined();
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

    const providerInput = screen.getAllByPlaceholderText("openai, deepseek, ollama, custom")[0]!;
    await user.clear(providerInput);
    await user.type(providerInput, "custom-v2");

    expect(screen.queryByText(/Model or embedding model has changed/)).toBeNull();
  });

  it("probes the base URL and fills recommended models without forcing manual fields", async () => {
    const user = userEvent.setup();
    render(<CuratorTab />, { wrapper: I18nWrapper });

    const modelInput = screen.getByPlaceholderText("Model used to write drafts");
    await user.clear(modelInput);
    await user.type(modelInput, "manual-model");

    const baseURL = screen.getAllByPlaceholderText("https://api.example.com/v1")[0]!;
    await user.clear(baseURL);
    await user.type(baseURL, "https://api.openai.com/v1");
    await user.tab();

    await waitFor(() => expect(mockProbeKnowledgeCurator).toHaveBeenCalledWith({
      chat_base_url: "https://api.openai.com/v1",
      chat_model: "manual-model",
      embedding_base_url: "https://embedding.example.com/v1",
      embedding_model: "embed-test",
      embedding_dimensions: 1536,
    }));
    expect(screen.getByDisplayValue("manual-model")).toBeTruthy();
    expect(screen.getByDisplayValue("text-embedding-3-small")).toBeTruthy();
    expect(screen.getByText(/Endpoint is compatible/)).toBeTruthy();
  });

  it("shows probe warnings when embeddings are unavailable", async () => {
    mockProbeKnowledgeCurator.mockResolvedValueOnce({
      chat_status: {
        provider: "ollama",
        model: "llama3.1",
        supported: true,
      },
      embedding_status: {
        provider: "ollama",
        model: "",
        dimensions: 1536,
        supported: false,
        error: "embedding base_url is unreachable",
      },
    });
    const user = userEvent.setup();
    render(<CuratorTab />, { wrapper: I18nWrapper });

    const baseURL = screen.getAllByPlaceholderText("https://api.example.com/v1")[0]!;
    await user.clear(baseURL);
    await user.type(baseURL, "http://localhost:11434/v1");
    await user.tab();

    await waitFor(() => expect(screen.getByText(/Endpoint is partially compatible/)).toBeTruthy());
    expect(screen.getByText(/embedding base_url is unreachable/)).toBeTruthy();
  });

  it("opens the knowledge type help dialog", async () => {
    const user = userEvent.setup();
    render(<CuratorTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: "Explain knowledge types" }));

    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(screen.getByText(/Lessons capture mistakes/)).toBeTruthy();
    expect(screen.getByText(/Playbooks capture a repeatable/)).toBeTruthy();
    expect(screen.getByText(/References capture stable facts/)).toBeTruthy();
  });
});
