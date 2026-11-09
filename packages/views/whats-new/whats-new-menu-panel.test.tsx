import type { ReactNode } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useSearchStore } from "@multica/core/command-bar";
import { configStore } from "@multica/core/config";
import { Command, Globe, Terminal } from "@multica/ui/icons";
import type { User } from "@multica/core/types";
import enLayout from "../locales/en/layout.json";
import { HelpLauncher } from "../layout/help-launcher";
import type { WhatsNewRelease } from "./releases";

const mocks = vi.hoisted(() => ({
  push: vi.fn(),
  markWhatsNewSeen: vi.fn(),
}));

const authStore = vi.hoisted(() => {
  // A real zustand store so optimistic updates re-render the menu.
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

function openHelpMenu() {
  fireEvent.click(screen.getByRole("button", { name: "Help" }));
  act(() => {
    vi.runAllTimers();
  });
  return screen.getByRole("menuitem", { name: /What's new/ });
}

function openFromHelpMenu() {
  fireEvent.click(openHelpMenu());
  act(() => {
    vi.runAllTimers();
  });
}

function closePanel() {
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  act(() => {
    vi.runAllTimers();
  });
}

const release = (version: string) => screen.getByRole("region", { name: `v${version}` });

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
});

describe("What's new from the Help menu", () => {
  it("opens nothing until the user asks", () => {
    signIn({ whats_new_seen_version: "0.1.0" });
    render(<HelpLauncher />);
    act(() => {
      vi.runAllTimers();
    });

    expect(screen.queryByRole("heading", { name: "What's new" })).not.toBeInTheDocument();
    expect(mocks.markWhatsNewSeen).not.toHaveBeenCalled();
  });

  it("marks only unread releases as new and records the newest as seen", () => {
    signIn({ whats_new_seen_version: "0.2.0" });
    render(<HelpLauncher />);

    openFromHelpMenu();
    expect(screen.getByRole("heading", { name: "What's new" })).toBeInTheDocument();
    expect(mocks.markWhatsNewSeen).toHaveBeenCalledWith("0.3.0");
    expect(release("0.3.0")).toHaveTextContent("New");
    expect(release("0.2.0")).not.toHaveTextContent("New");
    expect(screen.getByRole("link", { name: /v0.2.0 release notes/ })).toHaveAttribute(
      "href",
      "https://multica.ai/changelog#release-0-2-0",
    );
  });

  it("tags the menu entry with the newest unread version until the panel is opened", () => {
    signIn({ whats_new_seen_version: "0.1.0" });
    render(<HelpLauncher />);

    expect(openHelpMenu()).toHaveTextContent("v0.3.0");
    fireEvent.click(screen.getByRole("menuitem", { name: /What's new/ }));
    act(() => {
      vi.runAllTimers();
    });
    closePanel();

    expect(openHelpMenu()).not.toHaveTextContent("v0.3.0");
  });

  it("shows the server version next to the changelog link when it is known", () => {
    configStore.getState().setServerVersion("v0.3.0");
    signIn({ whats_new_seen_version: "0.3.0" });
    render(<HelpLauncher />);

    openFromHelpMenu();
    expect(screen.getByText("Server version v0.3.0", { selector: "[data-slot=whats-new-panel] span" })).toBeInTheDocument();
  });

  it("marks nothing as new once everything is read", () => {
    signIn({ whats_new_seen_version: "0.3.0" });
    render(<HelpLauncher />);

    expect(openHelpMenu()).not.toHaveTextContent("v0.3.0");
    fireEvent.click(screen.getByRole("menuitem", { name: /What's new/ }));
    act(() => {
      vi.runAllTimers();
    });
    expect(release("0.3.0")).not.toHaveTextContent("New");
    expect(mocks.markWhatsNewSeen).not.toHaveBeenCalled();
  });

  it("counts only releases after signup before the first dismissal", () => {
    signIn({ whats_new_seen_version: null, created_at: "2026-02-10T08:00:00Z" });
    render(<HelpLauncher />);

    openFromHelpMenu();
    expect(release("0.3.0")).toHaveTextContent("New");
    expect(release("0.2.0")).not.toHaveTextContent("New");
  });

  it("does not announce releases newer than the connected server", () => {
    configStore.getState().setServerVersion("v0.2.0");
    signIn({ whats_new_seen_version: "0.1.0" });
    render(<HelpLauncher />);

    openFromHelpMenu();
    expect(screen.queryByText("Palette title")).not.toBeInTheDocument();
    expect(mocks.markWhatsNewSeen).toHaveBeenCalledWith("0.2.0");
  });

  it("records nothing against a server that cannot store dismissals", () => {
    signIn({});
    render(<HelpLauncher />);

    openFromHelpMenu();
    expect(screen.getByRole("heading", { name: "What's new" })).toBeInTheDocument();
    expect(release("0.3.0")).not.toHaveTextContent("New");
    expect(mocks.markWhatsNewSeen).not.toHaveBeenCalled();
  });

  it("runs a highlight's action and closes the panel", () => {
    signIn({ whats_new_seen_version: "0.3.0" });
    render(<HelpLauncher />);

    openFromHelpMenu();
    fireEvent.click(screen.getByRole("button", { name: /Open/ }));
    expect(mocks.push).toHaveBeenCalledWith("/acme/runtimes");

    openFromHelpMenu();
    fireEvent.click(screen.getByRole("button", { name: /Try it/ }));
    expect(useSearchStore.getState().open).toBe(true);
  });

  it("falls back to English copy for a locale without a translation", () => {
    i18nState.language = "fr";
    signIn({ whats_new_seen_version: "0.1.0" });
    render(<HelpLauncher />);

    openFromHelpMenu();
    expect(screen.getByRole("heading", { name: "Palette title" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Titre langue" })).toBeInTheDocument();
  });

  it("retries recording the dismissal on the next open when it fails", async () => {
    signIn({ whats_new_seen_version: "0.1.0" });
    mocks.markWhatsNewSeen.mockRejectedValueOnce(new Error("offline"));
    render(<HelpLauncher />);

    openFromHelpMenu();
    await act(async () => {});
    closePanel();

    openFromHelpMenu();
    expect(mocks.markWhatsNewSeen).toHaveBeenCalledTimes(2);
  });
});
