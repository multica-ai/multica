import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { Agent, ChatSession } from "@multica/core/types";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ i18n: { language: "en" } }),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFlagValue: () => true,
}));

import { RecentChatsList } from "./recent-chats-list";

const agents: Agent[] = [
  { id: "agent-gemma", name: "Gemma", archived_at: null } as Agent,
  { id: "agent-lone", name: "Lone", archived_at: null } as Agent,
];

function session(
  id: string,
  agentId: string,
  title: string,
  updatedAt: string,
): ChatSession {
  return {
    id,
    workspace_id: "ws-1",
    agent_id: agentId,
    creator_id: "member-1",
    title,
    status: "active",
    has_unread: false,
    created_at: updatedAt,
    updated_at: updatedAt,
  };
}

describe("RecentChatsList", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-06-17T09:05:00Z"));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows only recent chats for the selected agent, newest first", () => {
    const onOpenSession = vi.fn();

    render(
      <RecentChatsList
        sessions={[
          session(
            "old-gemma",
            "agent-gemma",
            "Older Gemma chat",
            "2026-06-15T08:00:00Z",
          ),
          session("new-lone", "agent-lone", "Lone chat", "2026-06-17T08:00:00Z"),
          session(
            "new-gemma",
            "agent-gemma",
            "Latest Gemma chat",
            "2026-06-17T09:00:00Z",
          ),
        ]}
        agents={agents}
        agentId="agent-gemma"
        onOpenSession={onOpenSession}
      />,
    );

    expect(screen.getByText("Recent chats")).toBeInTheDocument();
    expect(screen.queryByText("Lone chat")).not.toBeInTheDocument();
    expect(screen.getByText("Latest Gemma chat")).toBeInTheDocument();
    expect(screen.getByText("Older Gemma chat")).toBeInTheDocument();
    expect(screen.getAllByRole("button").map((button) => button.textContent)).toEqual([
      "Latest Gemma chatGemma5 min",
      "Older Gemma chatGemma2d",
    ]);

    fireEvent.click(screen.getByRole("button", { name: /Latest Gemma chat/i }));
    expect(onOpenSession).toHaveBeenCalledWith("new-gemma");
  });

  it("renders nothing when no selected-agent chats exist", () => {
    const { container } = render(
      <RecentChatsList
        sessions={[
          session("new-lone", "agent-lone", "Lone chat", "2026-06-17T08:00:00Z"),
        ]}
        agents={agents}
        agentId="agent-gemma"
        onOpenSession={vi.fn()}
      />,
    );

    expect(container).toBeEmptyDOMElement();
  });
});
