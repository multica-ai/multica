// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mockListPersonaSandboxes = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    listPersonaSandboxes: (...args: unknown[]) =>
      mockListPersonaSandboxes(...args),
  },
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { SandboxTab } from "./sandbox-tab";

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_env: {},
  custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-self",
  skills: [],
  created_at: "2026-05-13T00:00:00Z",
  updated_at: "2026-05-13T00:00:00Z",
  archived_at: null,
  archived_by: null,
  persona_sandbox: "",
};

function renderTab(opts: {
  agent?: Agent;
  onSave?: (updates: Partial<Agent>) => Promise<void>;
  canEdit?: boolean;
}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <SandboxTab
          agent={opts.agent ?? baseAgent}
          canEdit={opts.canEdit ?? true}
          onSave={opts.onSave ?? (async () => {})}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("SandboxTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListPersonaSandboxes.mockResolvedValue([]);
  });

  it("shows persona-not-configured note when list is empty and agent has no value", async () => {
    renderTab({});
    expect(
      await screen.findByText(/Persona is not configured/i),
    ).toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("renders sandbox options including the no-gating sentinel for admins", async () => {
    mockListPersonaSandboxes.mockResolvedValue([
      { name: "claude-developer", description: "Dev sandbox", system_owned: true },
      { name: "claude-readonly", description: "", system_owned: false },
    ]);
    renderTab({});
    const select = (await screen.findByLabelText(/Persona sandbox/i)) as HTMLSelectElement;
    await waitFor(
      () => {
        expect(mockListPersonaSandboxes).toHaveBeenCalled();
        expect(select).not.toBeDisabled();
      },
      { timeout: 5000 },
    );
    const optionValues = Array.from(select.options).map((o) => o.value);
    expect(optionValues).toEqual(["", "claude-developer", "claude-readonly"]);
  });

  it("calls onSave with the chosen sandbox name on change", async () => {
    mockListPersonaSandboxes.mockResolvedValue([
      { name: "claude-developer", description: "Dev", system_owned: true },
    ]);
    const onSave = vi.fn().mockResolvedValue(undefined);
    renderTab({ onSave });

    const select = (await screen.findByLabelText(/Persona sandbox/i)) as HTMLSelectElement;
    await waitFor(() => expect(select).not.toBeDisabled(), { timeout: 5000 });
    fireEvent.change(select, { target: { value: "claude-developer" } });

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith({ persona_sandbox: "claude-developer" });
    });
  });

  it("disables the dropdown when canEdit is false", async () => {
    mockListPersonaSandboxes.mockResolvedValue([
      { name: "claude-developer", description: "Dev", system_owned: true },
    ]);
    renderTab({ canEdit: false });
    const select = (await screen.findByLabelText(/Persona sandbox/i)) as HTMLSelectElement;
    expect(select).toBeDisabled();
  });

  it("preserves a stale sandbox value as a selectable option when the server list does not include it", async () => {
    mockListPersonaSandboxes.mockResolvedValue([
      { name: "claude-developer", description: "Dev", system_owned: true },
    ]);
    renderTab({
      agent: { ...baseAgent, persona_sandbox: "renamed-sandbox" },
    });
    const select = (await screen.findByLabelText(/Persona sandbox/i)) as HTMLSelectElement;
    const optionValues = Array.from(select.options).map((o) => o.value);
    expect(optionValues).toContain("renamed-sandbox");
    expect(select.value).toBe("renamed-sandbox");
  });
});
