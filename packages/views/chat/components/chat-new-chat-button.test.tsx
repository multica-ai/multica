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

import { NewChatButton } from "./new-chat-button";

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
    permission_mode: "public_to",
    invocation_targets: [{ target_type: "workspace", target_id: null }],
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

// The ⊕ button carries the localized "New chat" label as its accessible name.
const NEW_CHAT_LABEL = enChat.window.new_chat_tooltip;

function renderPicker(onStart = vi.fn()) {
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <NewChatButton agents={agents} userId="user-1" onStart={onStart} />
    </I18nProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: NEW_CHAT_LABEL }));
  return { onStart };
}

describe("NewChatButton", () => {
  it("opens the agent picker below the ⊕ trigger", async () => {
    renderPicker();

    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveAttribute("data-side", "bottom");
  });

  it("filters both My agents and Others by agent name", async () => {
    renderPicker();

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
    renderPicker();

    const input = await screen.findByRole("textbox", { name: "Filter options" });
    fireEvent.change(input, { target: { value: "zhang" } });
    const dialog = screen.getByRole("dialog");

    expect(within(dialog).getByText("张三")).toBeInTheDocument();
    expect(within(dialog).getByText("My agents")).toBeInTheDocument();
    expect(within(dialog).queryByText("Alpha")).not.toBeInTheDocument();
    expect(within(dialog).queryByText("Beta")).not.toBeInTheDocument();
  });

  it("shows the shared empty state when no agents match", async () => {
    renderPicker();

    const input = await screen.findByRole("textbox", { name: "Filter options" });
    fireEvent.change(input, { target: { value: "missing" } });

    expect(screen.getByText("No results")).toBeInTheDocument();
    expect(screen.queryByText("My agents")).not.toBeInTheDocument();
    expect(screen.queryByText("Others")).not.toBeInTheDocument();
  });

  it("pre-checks no agent (a new chat has no current) and reports the chosen agent", async () => {
    const { onStart } = renderPicker();

    const dialog = screen.getByRole("dialog");
    // No agent should carry a visible check mark for a fresh new chat.
    const alphaRow = within(dialog).getByText("Alpha").closest("button");
    expect(alphaRow).not.toBeNull();
    expect(alphaRow!.querySelector("svg:not(.invisible)")).toBeNull();

    fireEvent.click(within(dialog).getByText("Beta"));

    expect(onStart).toHaveBeenCalledWith(agents[2]);
    await waitFor(() => {
      expect(screen.queryByRole("textbox", { name: "Filter options" })).not.toBeInTheDocument();
    });
  });

  it("starts immediately without a picker when only one agent exists", () => {
    const onStart = vi.fn();
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <NewChatButton agents={[agents[0]!]} userId="user-1" onStart={onStart} />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: NEW_CHAT_LABEL }));

    expect(onStart).toHaveBeenCalledWith(agents[0]);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
