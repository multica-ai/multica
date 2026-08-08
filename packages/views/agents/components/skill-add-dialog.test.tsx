// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, cleanup, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent, SkillSummary } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mockListSkills = vi.hoisted(() => vi.fn());
const mockAddAgentSkills = vi.hoisted(() => vi.fn());
const mockSetAgentSkills = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listSkills: (...args: unknown[]) => mockListSkills(...args),
    addAgentSkills: (...args: unknown[]) => mockAddAgentSkills(...args),
    setAgentSkills: (...args: unknown[]) => mockSetAgentSkills(...args),
  },
}));

vi.mock("sonner", () => ({
  toast: {
    error: (...args: unknown[]) => mockToastError(...args),
    success: vi.fn(),
  },
}));

import { SkillAddDialog } from "./skill-add-dialog";

function makeSkill(
  id: string,
  name: string,
  description: string,
  enabled: boolean,
): SkillSummary {
  return {
    id,
    workspace_id: "ws-1",
    name,
    description,
    config: {},
    created_by: "user-1",
    created_at: "2026-07-28T00:00:00Z",
    updated_at: "2026-07-28T00:00:00Z",
    enabled,
  };
}

/** Already on the agent and deliberately switched off by the user. */
const DISABLED_ASSIGNED = makeSkill(
  "skill-refunds",
  "Refund handling",
  "Process refunds",
  false,
);

/** Already on the agent and left on. */
const ENABLED_ASSIGNED = makeSkill(
  "skill-kb",
  "Knowledge base",
  "Answer from the knowledge base",
  true,
);

const SHIPPING = makeSkill(
  "skill-shipping",
  "Shipping lookup",
  "Track a shipment",
  true,
);

const RETURNS = makeSkill(
  "skill-returns",
  "Returns policy",
  "Explain the returns policy",
  true,
);

const agent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Support agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: null }],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [ENABLED_ASSIGNED, DISABLED_ASSIGNED],
  created_at: "2026-07-28T00:00:00Z",
  updated_at: "2026-07-28T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function renderDialog(agentOverrides: Partial<Agent> = {}) {
  const onOpenChange = vi.fn();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <SkillAddDialog
          agent={{ ...agent, ...agentOverrides }}
          open
          onOpenChange={onOpenChange}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );

  return { onOpenChange };
}

describe("SkillAddDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListSkills.mockResolvedValue([
      ENABLED_ASSIGNED,
      DISABLED_ASSIGNED,
      SHIPPING,
      RETURNS,
    ]);
    mockAddAgentSkills.mockResolvedValue(undefined);
    mockSetAgentSkills.mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
  });

  it("lists only workspace skills that are not attached yet", async () => {
    renderDialog();

    expect(await screen.findByText("Shipping lookup")).toBeInTheDocument();
    expect(screen.getByText("Returns policy")).toBeInTheDocument();
    expect(screen.queryByText("Refund handling")).not.toBeInTheDocument();
    expect(screen.queryByText("Knowledge base")).not.toBeInTheDocument();
  });

  // MUL-5358: the dialog used to merge `agent.skills` into the payload and
  // PUT the whole set, which deleted and reinserted every binding — the
  // reinserted rows came back at the `enabled` default, so adding one skill
  // silently re-enabled the ones the user had turned off.
  it("adds only the newly selected skill and leaves existing bindings alone", async () => {
    const user = userEvent.setup();
    const { onOpenChange } = renderDialog();

    await user.click(await screen.findByText("Shipping lookup"));
    await user.click(screen.getByRole("button", { name: "Add 1 skill" }));

    await waitFor(() => {
      expect(mockAddAgentSkills).toHaveBeenCalledWith("agent-1", {
        skill_ids: ["skill-shipping"],
      });
    });
    // The disabled binding must never appear in the request — the payload is
    // exactly what the user picked.
    expect(mockAddAgentSkills).toHaveBeenCalledTimes(1);
    expect(mockSetAgentSkills).not.toHaveBeenCalled();
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it("sends every selected skill when several are added at once", async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(await screen.findByText("Shipping lookup"));
    await user.click(screen.getByText("Returns policy"));
    await user.click(screen.getByRole("button", { name: "Add 2 skills" }));

    await waitFor(() => {
      expect(mockAddAgentSkills).toHaveBeenCalledWith("agent-1", {
        skill_ids: ["skill-shipping", "skill-returns"],
      });
    });
    expect(mockSetAgentSkills).not.toHaveBeenCalled();
  });

  it("does not touch skills when the user cancels", async () => {
    const user = userEvent.setup();
    const { onOpenChange } = renderDialog();

    await user.click(await screen.findByText("Shipping lookup"));
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(mockAddAgentSkills).not.toHaveBeenCalled();
    expect(mockSetAgentSkills).not.toHaveBeenCalled();
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("keeps the dialog open and surfaces a toast when the add fails", async () => {
    const user = userEvent.setup();
    mockAddAgentSkills.mockRejectedValue(new Error("network down"));
    const { onOpenChange } = renderDialog();

    await user.click(await screen.findByText("Shipping lookup"));
    await user.click(screen.getByRole("button", { name: "Add 1 skill" }));

    await waitFor(() => expect(mockToastError).toHaveBeenCalledWith("network down"));
    expect(onOpenChange).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Add 1 skill" })).toBeEnabled();
  });
});
