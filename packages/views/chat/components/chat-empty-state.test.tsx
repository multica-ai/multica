// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent } from "@multica/core/types";
import enChat from "../../locales/en/chat.json";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <div data-testid="agent-avatar" />,
}));

import { EmptyState } from "./chat-empty-state";

const agent = (conversationStarters: Agent["conversation_starters"] = []): Agent =>
  ({
    id: "agent-1",
    name: "Reviewer",
    description: "Reviews changes before they merge.",
    conversation_starters: conversationStarters,
  }) as Agent;

const adapter = (): NavigationAdapter => ({
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/chat",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path: string) => `https://app.test${path}`,
});

function renderEmptyState(value: Agent, customizeHref: string | null = null) {
  const onPickPrompt = vi.fn();
  render(
    <I18nProvider locale="en" resources={{ en: { chat: enChat } }}>
      <NavigationProvider value={adapter()}>
        <EmptyState
          agent={value}
          hasSessions={false}
          onPickPrompt={onPickPrompt}
          customizeHref={customizeHref}
        />
      </NavigationProvider>
    </I18nProvider>,
  );
  return onPickPrompt;
}

describe("chat empty-state conversation starters", () => {
  afterEach(cleanup);

  it("prefers the selected agent's configured prompts", () => {
    const onPickPrompt = renderEmptyState(
      agent([
        {
          label: "Review the release PR",
          prompt: "Review the release PR and summarize its risks.",
        },
      ]),
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Review the release PR" }),
    );

    expect(onPickPrompt).toHaveBeenCalledWith(
      "Review the release PR and summarize its risks.",
    );
    expect(screen.queryByText("What can you help with?")).toBeNull();
  });

  it("shows localized fallbacks for agents without configuration", () => {
    const onPickPrompt = renderEmptyState(agent());

    fireEvent.click(
      screen.getByRole("button", { name: "Suggest a first task" }),
    );

    expect(onPickPrompt).toHaveBeenCalledWith(
      "Suggest three useful tasks I could delegate to you.",
    );
  });
});

// Who may see "customize" is decided by the container (chat-window reads the
// agent permission and the backend capability); this suite only proves the
// empty state renders the affordance exactly when it is handed a target.
describe("chat empty-state customize affordance", () => {
  afterEach(cleanup);

  const href = "/acme/agents/agent-1?view=instructions&focus=conversation_starters";

  it("links a viewer who may edit the agent to its conversation starters", () => {
    renderEmptyState(agent(), href);

    expect(
      screen.getByRole("link", { name: "Customize starters" }),
    ).toHaveAttribute("href", href);
  });

  it("stays out of the DOM for a viewer who may not edit the agent", () => {
    renderEmptyState(agent());

    expect(
      screen.queryByRole("link", { name: "Customize starters" }),
    ).toBeNull();
  });
});
