import type { ReactNode } from "react";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDevActionsStore } from "@multica/core/dev-actions";
import { useDevPromptStore, useSessionPromptStore } from "@multica/core/session-prompt";
import { configureShortcutPlatform } from "@multica/core/shortcuts";
import type { User } from "@multica/core/types";
import { WHATS_NEW_RELEASES } from "../whats-new/releases";
import { deckCards } from "../whats-new/whats-new-deck";
import { ActionsPanel, DevActions } from "./actions-panel";

// Follow the release list, so adding a release doesn't break the deck status test.
const [latest, previous] = WHATS_NEW_RELEASES;

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
      whats_new_seen_version: latest!.version,
      onboarded_at: "2026-11-14T08:00:00Z",
      onboarding_questionnaire: { source: ["friend"] },
      star_prompt_shown_count: 0,
      star_prompt_last_shown_at: null,
      star_prompt_starred_at: null,
      ...user,
    },
  });
}

function wrap(node: ReactNode) {
  return render(<QueryClientProvider client={new QueryClient()}>{node}</QueryClientProvider>);
}

const feature = (title: string) => within(screen.getByRole("region", { name: title }));
const row = (title: string, label: string) => feature(title).getByText(label).nextElementSibling;
const status = (title: string) => feature(title).getByText(title).nextElementSibling;
const search = () => screen.getByRole("textbox", { name: "Search actions" });

beforeEach(() => {
  useSessionPromptStore.setState({ prompted: false, deckDealt: false, surveyPending: false, corner: null });
  useDevPromptStore.setState({ request: null });
  useDevActionsStore.setState({ pinned: [], recent: [], section: null });
  stores.chat.setState({ isOpen: false });
  signIn();
});

afterEach(() => configureShortcutPlatform(null));

describe("ActionsPanel", () => {
  it("opens on the Prompts group and says why the deck has nothing to deal", () => {
    wrap(<ActionsPanel wsId="ws-1" onClose={vi.fn()} />);
    expect(screen.getByRole("button", { name: /Prompts/ })).toHaveAttribute("aria-current", "true");
    act(() => search().blur());

    expect(status("What's new deck")).toHaveTextContent("nothing unread");
    fireEvent.change(screen.getByRole("combobox", { name: "Set seen version" }), {
      target: { value: previous!.version },
    });
    expect(stores.auth.getState().user?.whats_new_seen_version).toBe(previous!.version);
    expect(row("What's new deck", "Unread")).toHaveTextContent(latest!.version);
    expect(row("What's new deck", "Cards to deal")).toHaveTextContent(String(deckCards([latest!]).length));
    expect(status("What's new deck")).toHaveTextContent("deals at the next check");

    act(() => useSessionPromptStore.setState({ surveyPending: true }));
    expect(status("What's new deck")).toHaveTextContent("waiting · source survey open or deciding");
    act(() => {
      useSessionPromptStore.setState({ surveyPending: false });
      stores.chat.setState({ isOpen: true });
    });
    expect(status("What's new deck")).toHaveTextContent("waiting · chat window open");
  });

  it("asks each prompt to show itself, and remembers what ran", () => {
    wrap(<ActionsPanel wsId="ws-1" onClose={vi.fn()} />);
    for (const [title, kind] of [
      ["Show deck", "whats-new"],
      ["Run check now", "whats-new-check"],
      ["Show star card", "star"],
      ["Open survey", "survey"],
    ] as const) {
      fireEvent.click(screen.getByRole("button", { name: `Run ${title}` }));
      expect(useDevPromptStore.getState().request?.kind).toBe(kind);
    }
    expect(useDevActionsStore.getState().recent).toEqual([
      "source-survey/open",
      "star-card/show",
      "whats-new-deck/check",
      "whats-new-deck/show",
    ]);

    fireEvent.click(screen.getByRole("button", { name: /Recent/ }));
    expect(screen.getByRole("button", { name: "Run Open survey" })).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "What's new deck" })).not.toBeInTheDocument();
  });

  it("starts the prompt session over", () => {
    useSessionPromptStore.setState({ prompted: true, deckDealt: true, corner: "star" });
    wrap(<ActionsPanel wsId="ws-1" onClose={vi.fn()} />);
    expect(status("Prompt session")).toHaveTextContent("star holds the corner");
    fireEvent.click(screen.getByRole("button", { name: "Run Reset session" }));
    expect(useSessionPromptStore.getState()).toMatchObject({ prompted: false, deckDealt: false, corner: "star" });
  });

  it("disables workspace actions outside a workspace and says why", () => {
    wrap(<ActionsPanel wsId={null} onClose={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Run Show deck" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Run Open survey" })).toBeDisabled();
    expect(feature("Star card").getByText("needs a workspace")).toBeInTheDocument();
    expect(status("What's new deck")).toHaveTextContent("no workspace open");
  });

  it("searches every group, and runs the highlighted match on Enter", () => {
    const onClose = vi.fn();
    wrap(<ActionsPanel wsId="ws-1" onClose={onClose} />);
    fireEvent.change(search(), { target: { value: "seen 0.8.2" } });
    const results = within(screen.getByLabelText("Matching actions"));
    expect(results.getAllByRole("button", { name: /^Run / }).map((b) => b.getAttribute("aria-label"))).toEqual([
      "Run Set seen version → 0.8.2",
    ]);
    fireEvent.keyDown(search(), { key: "Enter" });
    expect(stores.auth.getState().user?.whats_new_seen_version).toBe("0.8.2");
    expect(onClose).toHaveBeenCalled();
    expect(useDevActionsStore.getState().recent).toEqual(["whats-new-deck/seen-version=0.8.2"]);
  });

  it("moves the highlight with the arrow keys", () => {
    wrap(<ActionsPanel wsId="ws-1" onClose={vi.fn()} />);
    fireEvent.change(search(), { target: { value: "show" } });
    fireEvent.keyDown(search(), { key: "ArrowDown" });
    fireEvent.keyDown(search(), { key: "Enter" });
    expect(useDevPromptStore.getState().request?.kind).toBe("star");
  });

  it("lists another group's features", () => {
    wrap(<ActionsPanel wsId="ws-1" onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: /Errors/ }));
    expect(screen.getByRole("region", { name: "Error capture" })).toBeInTheDocument();
    expect(useDevActionsStore.getState().section).toBe("Errors");
  });
});

describe("DevActions", () => {
  it("puts pinned actions on the toolbar", () => {
    wrap(<DevActions wsId="ws-1" />);
    fireEvent.click(screen.getByTestId("dev-toolbar-actions"));
    fireEvent.click(screen.getByRole("button", { name: "Pin Show star card" }));
    expect(useDevActionsStore.getState().pinned).toEqual(["star-card/show"]);

    fireEvent.click(screen.getByRole("button", { name: "Show star card" }));
    expect(useDevPromptStore.getState().request?.kind).toBe("star");
  });

  it("toggles the panel with the shortcut", () => {
    configureShortcutPlatform("macos");
    wrap(<DevActions wsId="ws-1" />);
    expect(screen.queryByRole("textbox", { name: "Search actions" })).not.toBeInTheDocument();
    fireEvent.keyDown(window, { key: "k", code: "KeyK", metaKey: true, shiftKey: true });
    expect(search()).toBeInTheDocument();
  });
});
