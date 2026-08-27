// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent } from "@multica/core/types";
import enChat from "../../locales/en/chat.json";

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <div data-testid="agent-avatar" />,
}));

import { EmptyState } from "./chat-empty-state";

const agent = (starterPrompts: Agent["starter_prompts"] = []): Agent =>
  ({
    id: "agent-1",
    name: "Reviewer",
    description: "Reviews changes before they merge.",
    starter_prompts: starterPrompts,
  }) as Agent;

function renderEmptyState(value: Agent) {
  const onPickPrompt = vi.fn();
  render(
    <I18nProvider locale="en" resources={{ en: { chat: enChat } }}>
      <EmptyState
        agent={value}
        hasSessions={false}
        onPickPrompt={onPickPrompt}
      />
    </I18nProvider>,
  );
  return onPickPrompt;
}

describe("chat empty-state starter prompts", () => {
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
