import type { ReactNode } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, onTestFinished, vi } from "vitest";
import { configStore } from "@multica/core/config";
import { Command, Globe, Terminal } from "@multica/ui/icons";
import type { User } from "@multica/core/types";
import enLayout from "../locales/en/layout.json";
import { useSearchStore } from "../search/search-store";
import type { WhatsNewRelease } from "./releases";
import { useWhatsNewSessionStore } from "./session-store";
import { AUTO_OPEN_DELAY_MS, WhatsNewLauncher } from "./whats-new-launcher";

const mocks = vi.hoisted(() => ({
  push: vi.fn(),
  markWhatsNewSeen: vi.fn(),
}));

const authStore = vi.hoisted(() => {
  // A real zustand store so optimistic updates re-render the launcher.
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { create } = require("zustand") as typeof import("zustand");
  return create<{ user: Partial<User> | null }>(() => ({ user: null }));
});

vi.mock("@multica/core/auth", () => ({ useAuthStore: authStore }));

vi.mock("@multica/core/whats-new", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/whats-new")>("@multica/core/whats-new");
  return { ...actual, markWhatsNewSeen: mocks.markWhatsNewSeen };
});

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ runtimes: () => "/acme/runtimes", settings: () => "/acme/settings" }),
}));

vi.mock("../navigation", () => ({ useNavigation: () => ({ push: mocks.push }) }));

vi.mock("@multica/ui/components/common/tooltip-hint", () => ({
  TooltipHint: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

const RELEASES: WhatsNewRelease[] = [
  {
    version: "0.3.0",
    date: "2026-03-01",
    highlights: [
      {
        id: "palette",
        icon: Command,
        action: { kind: "command-palette" },
        copy: { en: { title: "Palette title", body: "Palette body" } },
      },
      {
        id: "runtimes",
        icon: Terminal,
        action: { kind: "route", to: (p) => p.runtimes() },
        copy: { en: { title: "Runtimes title", body: "Runtimes body" } },
      },
    ],
  },
  {
    version: "0.2.0",
    date: "2026-02-01",
    highlights: [
      {
        id: "language",
        icon: Globe,
        copy: { en: { title: "Language title", body: "Language body" }, fr: { title: "Titre langue", body: "Corps" } },
      },
    ],
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

const i18nState = vi.hoisted(() => ({ language: "en" }));

// Resolves selectors against the real en/layout.json, including i18next's
// `_one`/`_other` plural keys that the typed selector names without suffix.
vi.mock("../i18n", () => ({
  useT: () => ({
    i18n: i18nState,
    t: (sel: (r: unknown) => unknown, vars?: Record<string, string | number>) => {
      const path: string[] = [];
      const recorder: unknown = new Proxy({}, {
        get: (_, key) => {
          path.push(String(key));
          return recorder;
        },
      });
      sel(recorder);
      const leaf = path.pop()!;
      let node = enLayout as unknown as Record<string, unknown>;
      for (const key of path) node = node[key] as Record<string, unknown>;
      const plural = vars?.count === undefined ? leaf : `${leaf}_${vars.count === 1 ? "one" : "other"}`;
      const template = String(node[leaf] ?? node[plural]);
      return template.replace(/\{\{(\w+)\}\}/g, (_, key) => String(vars?.[key] ?? ""));
    },
  }),
}));

function signIn(user: Partial<User>) {
  authStore.setState({ user: { id: "u-1", created_at: "2026-01-15T08:00:00Z", ...user } });
}

function renderLauncher() {
  const view = render(<WhatsNewLauncher />);
  act(() => {
    vi.advanceTimersByTime(AUTO_OPEN_DELAY_MS);
  });
  return view;
}

const trigger = () => screen.getByRole("button", { name: "What's new" });
const dot = () => document.querySelector('[data-slot="whats-new-dot"]');

beforeEach(() => {
  vi.useFakeTimers();
  i18nState.language = "en";
  mocks.markWhatsNewSeen.mockImplementation(async (version: string) => {
    const user = authStore.getState().user;
    authStore.setState({ user: { ...user, whats_new_seen_version: version } });
  });
});

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
  authStore.setState({ user: null });
  configStore.getState().setServerVersion("");
  useSearchStore.getState().setOpen(false);
  useWhatsNewSessionStore.setState({ autoShown: false });
});

describe("WhatsNewLauncher", () => {
  it("opens the update card with every highlight released since the last dismissal", () => {
    signIn({ whats_new_seen_version: "0.1.0" });
    renderLauncher();

    expect(dot()).toBeInTheDocument();
    expect(screen.getByText("Multica updated · v0.2.0 → v0.3.0")).toBeInTheDocument();
    expect(screen.getByText("3 new features since your last visit")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Palette title" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Language title/ })).toBeInTheDocument();
    expect(screen.queryByText("Old title")).not.toBeInTheDocument();
  });

  it("expands a row when it is clicked", () => {
    signIn({ whats_new_seen_version: "0.1.0" });
    renderLauncher();

    fireEvent.click(screen.getByRole("button", { name: /Runtimes title/ }));
    expect(screen.getByRole("heading", { name: "Runtimes title" })).toBeInTheDocument();
    expect(screen.getByText("Runtimes body")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Palette title/ })).toBeInTheDocument();
  });

  it("records the newest release as seen when the card is acknowledged", () => {
    signIn({ whats_new_seen_version: "0.1.0" });
    renderLauncher();

    fireEvent.click(screen.getByRole("button", { name: "Got it" }));
    expect(mocks.markWhatsNewSeen).toHaveBeenCalledWith("0.3.0");
    expect(dot()).not.toBeInTheDocument();
  });

  it("runs the highlight's action and dismisses the card", () => {
    signIn({ whats_new_seen_version: "0.1.0" });
    renderLauncher();

    fireEvent.click(screen.getByRole("button", { name: /Try it/ }));
    expect(useSearchStore.getState().open).toBe(true);
    expect(mocks.markWhatsNewSeen).toHaveBeenCalledWith("0.3.0");
  });

  it("counts only releases after signup before the first dismissal", () => {
    signIn({ whats_new_seen_version: null, created_at: "2026-02-10T08:00:00Z" });
    renderLauncher();

    expect(screen.getByText("Multica updated · v0.3.0")).toBeInTheDocument();
    expect(screen.getByText("2 new features since your last visit")).toBeInTheDocument();
  });

  it("does not announce releases newer than the connected server", () => {
    configStore.getState().setServerVersion("v0.2.0");
    signIn({ whats_new_seen_version: "0.1.0" });
    renderLauncher();

    expect(screen.getByText("1 new feature since your last visit")).toBeInTheDocument();
    expect(screen.queryByText("Palette title")).not.toBeInTheDocument();
  });

  it("stays quiet against a server that cannot record dismissals", () => {
    signIn({});
    renderLauncher();

    expect(dot()).not.toBeInTheDocument();
    expect(screen.queryByText(/since your last visit/)).not.toBeInTheDocument();

    fireEvent.click(trigger());
    expect(screen.getByRole("heading", { name: "What's new" })).toBeInTheDocument();
    expect(mocks.markWhatsNewSeen).not.toHaveBeenCalled();
  });

  it("opens the full history from the rail and marks only unread releases as new", () => {
    signIn({ whats_new_seen_version: "0.2.0" });
    render(<WhatsNewLauncher />);

    fireEvent.click(trigger());
    expect(screen.getByRole("heading", { name: "What's new" })).toBeInTheDocument();
    expect(mocks.markWhatsNewSeen).toHaveBeenCalledWith("0.3.0");

    const newest = screen.getByRole("region", { name: "v0.3.0" });
    const older = screen.getByRole("region", { name: "v0.2.0" });
    expect(newest).toHaveTextContent("New");
    expect(older).not.toHaveTextContent("New");
    expect(screen.getByRole("link", { name: /v0.2.0 release notes/ })).toHaveAttribute(
      "href",
      "https://multica.ai/changelog#release-0-2-0",
    );

    fireEvent.click(screen.getByRole("button", { name: /Open/ }));
    expect(mocks.push).toHaveBeenCalledWith("/acme/runtimes");
  });

  it("falls back to English copy for a locale without a translation", () => {
    i18nState.language = "fr";
    signIn({ whats_new_seen_version: "0.1.0" });
    renderLauncher();

    expect(screen.getByRole("heading", { name: "Palette title" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Titre langue/ })).toBeInTheDocument();
  });

  it("auto-opens from the visible rail only, once per session", () => {
    signIn({ whats_new_seen_version: "0.1.0" });
    // Narrow layouts keep a display:none rail mounted next to the nav sheet's.
    // jsdom has no checkVisibility, so stand one in for this test.
    const visibility = vi.fn(() => false);
    Object.defineProperty(HTMLElement.prototype, "checkVisibility", { value: visibility, configurable: true });
    onTestFinished(() => {
      delete (HTMLElement.prototype as Partial<HTMLElement>).checkVisibility;
    });
    const hidden = renderLauncher();
    expect(screen.queryByText(/since your last visit/)).not.toBeInTheDocument();

    visibility.mockReturnValue(true);
    const sheet = renderLauncher();
    expect(screen.getByText("3 new features since your last visit")).toBeInTheDocument();

    // Clicking away and reopening the sheet remounts the launcher.
    sheet.unmount();
    hidden.unmount();
    renderLauncher();
    expect(screen.queryByText(/since your last visit/)).not.toBeInTheDocument();
    expect(dot()).toBeInTheDocument();
  });

  it("keeps the dot and retries when recording the dismissal fails", async () => {
    signIn({ whats_new_seen_version: "0.1.0" });
    mocks.markWhatsNewSeen.mockRejectedValueOnce(new Error("offline"));
    renderLauncher();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Got it" }));
    });
    expect(dot()).toBeInTheDocument();

    fireEvent.click(trigger());
    expect(mocks.markWhatsNewSeen).toHaveBeenCalledTimes(2);
    expect(dot()).not.toBeInTheDocument();
  });
});
