import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useDevPromptStore, useSessionPromptStore } from "@multica/core/session-prompt";
import type { User } from "@multica/core/types";
import { PromptsPanel } from "./prompts-panel";

const stores = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { create } = require("zustand") as typeof import("zustand");
  const auth = create<{ user: Partial<User> | null; setUser: (user: Partial<User>) => void }>((set) => ({
    user: null,
    setUser: (user) => set({ user }),
  }));
  const chat = create<{ isOpen: boolean }>(() => ({ isOpen: false }));
  return { auth, chat };
});

vi.mock("@multica/core/auth", () => ({ useAuthStore: stores.auth }));
vi.mock("@multica/core/chat", () => ({ useChatStore: stores.chat }));

function signIn(user: Partial<User> = {}) {
  stores.auth.setState({
    user: {
      id: "u1",
      created_at: "2026-11-14T08:00:00Z",
      whats_new_seen_version: "0.9.0",
      onboarded_at: "2026-11-14T08:00:00Z",
      onboarding_questionnaire: { source: ["friend"] },
      star_prompt_shown_count: 0,
      star_prompt_last_shown_at: null,
      star_prompt_starred_at: null,
      ...user,
    },
  });
}

const field = (label: string) => screen.getByText(label).nextElementSibling;

beforeEach(() => {
  useSessionPromptStore.setState({ prompted: false, deckDealt: false, surveyPending: false, corner: null });
  useDevPromptStore.setState({ request: null });
  stores.chat.setState({ isOpen: false });
  signIn();
});

describe("PromptsPanel", () => {
  it("says why the deck has nothing to deal, and deals once the seen version moves back", () => {
    render(<PromptsPanel wsId="ws-1" now={0} />);
    expect(field("Status")).toHaveTextContent("nothing unread");

    fireEvent.change(screen.getByRole("combobox", { name: "Seen version" }), { target: { value: "0.8.2" } });
    expect(stores.auth.getState().user?.whats_new_seen_version).toBe("0.8.2");
    expect(field("Unread")).toHaveTextContent("0.9.0");
    expect(field("Cards to deal")).toHaveTextContent("3");
    expect(field("Status")).toHaveTextContent("deals at the next check");

    act(() => useSessionPromptStore.setState({ surveyPending: true }));
    expect(field("Status")).toHaveTextContent("waiting: source survey open or deciding");
    act(() => {
      useSessionPromptStore.setState({ surveyPending: false });
      stores.chat.setState({ isOpen: true });
    });
    expect(field("Status")).toHaveTextContent("waiting: chat window open");
  });

  it("asks each prompt to show itself", () => {
    render(<PromptsPanel wsId="ws-1" now={0} />);
    for (const [button, kind] of [
      ["Show deck", "whats-new"],
      ["Run check now", "whats-new-check"],
      ["Show star card", "star"],
      ["Open survey", "survey"],
    ] as const) {
      fireEvent.click(screen.getByRole("button", { name: button }));
      expect(useDevPromptStore.getState().request?.kind).toBe(kind);
    }
  });

  it("starts the session over", () => {
    useSessionPromptStore.setState({ prompted: true, deckDealt: true, corner: "star" });
    render(<PromptsPanel wsId="ws-1" now={0} />);
    fireEvent.click(screen.getByRole("button", { name: "Reset session" }));
    expect(useSessionPromptStore.getState()).toMatchObject({ prompted: false, deckDealt: false, corner: "star" });
  });

  it("disables the workspace prompts outside a workspace", () => {
    render(<PromptsPanel wsId={null} now={0} />);
    expect(screen.getByRole("button", { name: "Show deck" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Open survey" })).toBeDisabled();
    expect(field("Status")).toHaveTextContent("no workspace open");
  });
});
