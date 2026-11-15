import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { useSessionPromptStore } from "@multica/core/session-prompt";
import type { User } from "@multica/core/types";
import { Command, Globe, Terminal } from "@multica/ui/icons";
import enCommon from "../locales/en/common.json";
import enLayout from "../locales/en/layout.json";
import type { WhatsNewRelease } from "./releases";
import { WHATS_NEW_DECK_DELAY_MS, WHATS_NEW_DECK_RETRY_MS, WhatsNewDeck } from "./whats-new-deck";

const mocks = vi.hoisted(() => ({ push: vi.fn(), markWhatsNewSeen: vi.fn() }));

const stores = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { create } = require("zustand") as typeof import("zustand");
  return {
    auth: create<{ user: Partial<User> | null }>(() => ({ user: null })),
    chat: create<{ isOpen: boolean; floatingChatEnabled: boolean }>(() => ({
      isOpen: false,
      floatingChatEnabled: true,
    })),
  };
});

vi.mock("@multica/core/auth", () => ({ useAuthStore: stores.auth }));
vi.mock("@multica/core/chat", () => ({ useChatStore: stores.chat }));
vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>("@multica/core/paths");
  return {
    ...actual,
    useWorkspacePaths: () => ({ chat: () => "/acme/chat", runtimes: () => "/acme/runtimes" }),
    useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme" }),
  };
});
vi.mock("@multica/core/whats-new", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/whats-new")>("@multica/core/whats-new");
  return { ...actual, markWhatsNewSeen: mocks.markWhatsNewSeen };
});
vi.mock("../navigation", () => ({ useNavigation: () => ({ pathname: "/acme/issues", push: mocks.push }) }));

const ART = { ground: "brand" as const, Art: () => <div data-testid="hero-art" /> };

const RELEASES: WhatsNewRelease[] = [
  {
    version: "0.3.0",
    date: "2026-03-01",
    highlights: [
      {
        id: "palette",
        icon: Command,
        hero: ART,
        action: { kind: "command-palette" },
        copy: { en: { title: "Palette title", body: "Palette body" } },
      },
      {
        id: "runtimes",
        icon: Terminal,
        hero: ART,
        action: { kind: "route", to: (p) => p.runtimes() },
        copy: { en: { title: "Runtimes title", body: "Runtimes body" } },
      },
      { id: "plain", icon: Globe, copy: { en: { title: "Plain title", body: "Plain body" } } },
    ],
  },
  {
    version: "0.2.0",
    date: "2026-02-01",
    highlights: [{ id: "language", icon: Globe, hero: ART, copy: { en: { title: "Language title", body: "Language body" } } }],
  },
  {
    version: "0.1.0",
    date: "2026-01-01",
    highlights: [{ id: "old", icon: Globe, copy: { en: { title: "Old title", body: "Old body" } } }],
  },
];

vi.mock("./releases", async () => {
  const actual = await vi.importActual<typeof import("./releases")>("./releases");
  return {
    ...actual,
    get WHATS_NEW_RELEASES() {
      return RELEASES;
    },
  };
});

function signIn(user: Partial<User> = {}) {
  stores.auth.setState({
    user: {
      id: "u1",
      created_at: "2026-01-15T08:00:00Z",
      whats_new_seen_version: null,
      // Already answered the source survey, so it does not claim the session.
      onboarded_at: "2026-01-01T00:00:00Z",
      onboarding_questionnaire: { source: ["friend"] },
      ...user,
    },
  });
}

function renderDeck() {
  return render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon, layout: enLayout } }}>
      <WhatsNewDeck />
    </I18nProvider>,
  );
}

async function wait(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

const title = (name: string) => screen.queryByRole("heading", { name });

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  mocks.markWhatsNewSeen.mockResolvedValue(undefined);
  stores.chat.setState({ isOpen: false });
  useSessionPromptStore.setState({ prompted: false });
  signIn();
});

afterEach(() => {
  window.localStorage.removeItem("multica.source_backfill.dismiss.u1");
  vi.useRealTimers();
  vi.clearAllMocks();
});

describe("WhatsNewDeck", () => {
  it("deals the releases since signup, newest first, a moment after the app opens", async () => {
    renderDeck();
    await wait(WHATS_NEW_DECK_DELAY_MS - 100);
    expect(title("Palette title")).not.toBeInTheDocument();

    await wait(100);
    expect(title("Palette title")).toBeInTheDocument();
    expect(within(screen.getByRole("group", { name: "Palette title" })).getByText("v0.3.0 · Mar 1")).toBeInTheDocument();
    // 0.1.0 shipped before the account existed.
    expect(screen.getAllByRole("button", { name: /^Update \d of 3$/ })).toHaveLength(3);
    expect(useSessionPromptStore.getState().prompted).toBe(true);
  });

  it("walks the deck with Next and marks the newest release seen on Got it", async () => {
    renderDeck();
    await wait(WHATS_NEW_DECK_DELAY_MS);

    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(title("Runtimes title")).toBeInTheDocument();
    expect(title("Palette title")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(title("Language title")).toBeInTheDocument();
    expect(mocks.markWhatsNewSeen).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Got it" }));
    expect(mocks.markWhatsNewSeen).toHaveBeenCalledWith("0.3.0");
    await wait(300);
    expect(title("Language title")).not.toBeInTheDocument();
  });

  it("jumps to a card from its dot", async () => {
    renderDeck();
    await wait(WHATS_NEW_DECK_DELAY_MS);

    fireEvent.click(screen.getByRole("button", { name: "Update 3 of 3" }));
    expect(title("Language title")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Got it" })).toBeInTheDocument();
  });

  it("closes every card at once from the close button", async () => {
    renderDeck();
    await wait(WHATS_NEW_DECK_DELAY_MS);

    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(mocks.markWhatsNewSeen).toHaveBeenCalledWith("0.3.0");
    await wait(300);
    expect(screen.queryByRole("region", { name: "What's new" })).not.toBeInTheDocument();
  });

  it("follows a card's action and puts the deck away", async () => {
    renderDeck();
    await wait(WHATS_NEW_DECK_DELAY_MS);

    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    fireEvent.click(screen.getByRole("button", { name: /Open/ }));
    expect(mocks.push).toHaveBeenCalledWith("/acme/runtimes");
    expect(mocks.markWhatsNewSeen).toHaveBeenCalledWith("0.3.0");
  });

  it("answers arrow keys and Escape only while focus is inside", async () => {
    renderDeck();
    await wait(WHATS_NEW_DECK_DELAY_MS);

    fireEvent.keyDown(document.body, { key: "ArrowRight" });
    expect(title("Palette title")).toBeInTheDocument();

    const next = screen.getByRole("button", { name: "Next" });
    fireEvent.keyDown(next, { key: "ArrowRight" });
    expect(title("Runtimes title")).toBeInTheDocument();
    fireEvent.keyDown(screen.getByRole("button", { name: "Next" }), { key: "ArrowLeft" });
    expect(title("Palette title")).toBeInTheDocument();

    fireEvent.keyDown(screen.getByRole("button", { name: "Next" }), { key: "Escape" });
    expect(mocks.markWhatsNewSeen).toHaveBeenCalledWith("0.3.0");
  });

  it("waits for the chat window to close before dealing", async () => {
    stores.chat.setState({ isOpen: true });
    renderDeck();
    await wait(WHATS_NEW_DECK_DELAY_MS);
    expect(title("Palette title")).not.toBeInTheDocument();
    expect(useSessionPromptStore.getState().prompted).toBe(false);

    act(() => stores.chat.setState({ isOpen: false }));
    await wait(WHATS_NEW_DECK_RETRY_MS);
    expect(title("Palette title")).toBeInTheDocument();
  });

  it("steps behind the chat window and comes back on the same card", async () => {
    renderDeck();
    await wait(WHATS_NEW_DECK_DELAY_MS);
    fireEvent.click(screen.getByRole("button", { name: "Next" }));

    act(() => stores.chat.setState({ isOpen: true }));
    expect(title("Runtimes title")).not.toBeInTheDocument();
    act(() => stores.chat.setState({ isOpen: false }));
    expect(title("Runtimes title")).toBeInTheDocument();
  });

  it("stays away once the session has had its interruption", async () => {
    useSessionPromptStore.setState({ prompted: true });
    renderDeck();
    await wait(WHATS_NEW_DECK_DELAY_MS + WHATS_NEW_DECK_RETRY_MS);
    expect(title("Palette title")).not.toBeInTheDocument();
  });

  it("leaves the session to the source survey when it is due", async () => {
    signIn({ onboarding_questionnaire: undefined });
    renderDeck();
    await wait(WHATS_NEW_DECK_DELAY_MS);
    expect(title("Palette title")).not.toBeInTheDocument();
    expect(useSessionPromptStore.getState().prompted).toBe(false);
  });

  it("leaves highlights without hero art to the What's new panel", async () => {
    renderDeck();
    await wait(WHATS_NEW_DECK_DELAY_MS);
    expect(screen.queryByRole("heading", { name: "Plain title", hidden: true })).not.toBeInTheDocument();
    expect(screen.getAllByTestId("hero-art")).toHaveLength(3);
  });

  it("stays away when no unread highlight has hero art", async () => {
    RELEASES[0]!.highlights.forEach((h) => (h.hero = undefined));
    try {
      signIn({ whats_new_seen_version: "0.2.0" });
      renderDeck();
      await wait(WHATS_NEW_DECK_DELAY_MS);
      expect(screen.queryByRole("region", { name: "What's new" })).not.toBeInTheDocument();
      expect(useSessionPromptStore.getState().prompted).toBe(false);
    } finally {
      RELEASES[0]!.highlights[0]!.hero = ART;
      RELEASES[0]!.highlights[1]!.hero = ART;
    }
  });

  it("has nothing to deal once the newest release is seen", async () => {
    signIn({ whats_new_seen_version: "0.3.0" });
    renderDeck();
    await wait(WHATS_NEW_DECK_DELAY_MS);
    expect(screen.queryByRole("region", { name: "What's new" })).not.toBeInTheDocument();
    expect(useSessionPromptStore.getState().prompted).toBe(false);
  });

  it("goes away when the release is marked seen elsewhere", async () => {
    renderDeck();
    await wait(WHATS_NEW_DECK_DELAY_MS);
    expect(title("Palette title")).toBeInTheDocument();

    act(() => signIn({ whats_new_seen_version: "0.3.0" }));
    expect(title("Palette title")).not.toBeInTheDocument();
  });
});
