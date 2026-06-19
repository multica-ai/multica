import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent } from "@multica/core/types";
import enChat from "../../locales/en/chat.json";
import enIssues from "../../locales/en/issues.json";

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));

import { AgentDropdown } from "./chat-window";

const TEST_RESOURCES = { en: { chat: enChat, issues: enIssues } };

function makeAgent(overrides: Partial<Agent> & Pick<Agent, "id" | "name" | "owner_id">): Agent {
  return {
    workspace_id: "ws-1",
    runtime_id: "runtime-1",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    status: "idle",
    max_concurrent_tasks: 1,
    model: "sonnet",
    skills: [],
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    archived_at: null,
    archived_by: null,
    ...overrides,
    id: overrides.id,
    name: overrides.name,
    owner_id: overrides.owner_id,
  };
}

const agents = [
  makeAgent({ id: "mine-alpha", name: "Alpha", owner_id: "user-1" }),
  makeAgent({ id: "mine-zhang", name: "张三", owner_id: "user-1" }),
  makeAgent({ id: "other-beta", name: "Beta", owner_id: "user-2" }),
  makeAgent({ id: "other-gamma", name: "Gamma", owner_id: "user-2" }),
];

function renderDropdown(onSelect = vi.fn()) {
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <AgentDropdown
        agents={agents}
        activeAgent={agents[0]!}
        userId="user-1"
        onSelect={onSelect}
      />
    </I18nProvider>,
  );
  fireEvent.click(screen.getByText("Alpha"));
  return { onSelect };
}

describe("AgentDropdown", () => {
  it("opens the shared picker upward from the chat input", async () => {
    renderDropdown();

    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveAttribute("data-side", "top");
  });

  it("filters both My agents and Others by agent name", async () => {
    renderDropdown();

    const input = await screen.findByRole("textbox", { name: "Filter options" });
    fireEvent.change(input, { target: { value: "ta" } });
    const dialog = screen.getByRole("dialog");

    expect(within(dialog).queryByText("Alpha")).not.toBeInTheDocument();
    expect(within(dialog).queryByText("张三")).not.toBeInTheDocument();
    expect(within(dialog).getByText("Beta")).toBeInTheDocument();
    expect(within(dialog).queryByText("Gamma")).not.toBeInTheDocument();
    expect(within(dialog).getByText("Others")).toBeInTheDocument();
  });

  it("matches My agents by pinyin", async () => {
    renderDropdown();

    const input = await screen.findByRole("textbox", { name: "Filter options" });
    fireEvent.change(input, { target: { value: "zhang" } });
    const dialog = screen.getByRole("dialog");

    expect(within(dialog).getByText("张三")).toBeInTheDocument();
    expect(within(dialog).getByText("My agents")).toBeInTheDocument();
    expect(within(dialog).queryByText("Alpha")).not.toBeInTheDocument();
    expect(within(dialog).queryByText("Beta")).not.toBeInTheDocument();
  });

  it("shows the shared empty state when no agents match", async () => {
    renderDropdown();

    const input = await screen.findByRole("textbox", { name: "Filter options" });
    fireEvent.change(input, { target: { value: "missing" } });

    expect(screen.getByText("No results")).toBeInTheDocument();
    expect(screen.queryByText("My agents")).not.toBeInTheDocument();
    expect(screen.queryByText("Others")).not.toBeInTheDocument();
  });

  it("left-aligns agent picker rows", async () => {
    renderDropdown();

    const dialog = await screen.findByRole("dialog");
    const alphaRow = Array.from(
      dialog.querySelectorAll<HTMLButtonElement>("button[data-picker-item]"),
    ).find((row) => row.textContent?.includes("Alpha"));

    expect(alphaRow).toBeDefined();
    expect(alphaRow).toHaveClass("text-left");
  });

  it("keeps the current agent marked and selects another agent", async () => {
    const { onSelect } = renderDropdown();

    const dialog = screen.getByRole("dialog");
    const alphaRow = within(dialog).getByText("Alpha").closest("button");
    expect(alphaRow).not.toBeNull();
    expect(alphaRow!.querySelector("svg:not(.invisible)")).not.toBeNull();

    fireEvent.click(within(dialog).getByText("Beta"));

    expect(onSelect).toHaveBeenCalledWith(agents[2]);
    await waitFor(() => {
      expect(screen.queryByRole("textbox", { name: "Filter options" })).not.toBeInTheDocument();
    });
  });
});