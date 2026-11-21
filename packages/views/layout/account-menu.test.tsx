import type { ReactNode } from "react";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DEFAULT_APPEARANCE, useAppearanceStore, useInstalledThemesStore } from "@multica/core/appearance";
import { renderWithI18n } from "../test/i18n";
import { AccountMenu } from "./account-menu";

const theme = vi.hoisted(() => ({ current: "light", setTheme: vi.fn() }));

vi.mock("@multica/ui/components/common/theme-provider", () => ({
  useTheme: () => ({
    theme: theme.current,
    resolvedTheme: theme.current,
    setTheme: theme.setTheme,
  }),
}));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "user-1", name: "Jiayuan", email: "jiayuan@example.com", avatar_url: null } };
  return {
    useAuthStore: Object.assign((selector: (s: typeof state) => unknown) => selector(state), {
      getState: () => state,
    }),
  };
});
vi.mock("@multica/core/workspace/avatar-url", () => ({ resolvePublicFileUrl: () => undefined }));
vi.mock("@multica/ui/components/common/tooltip-hint", () => ({
  TooltipHint: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("../navigation", () => ({
  AppLink: ({ children, href, ...props }: { children: ReactNode; href: string } & React.ComponentProps<"a">) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("../auth", () => ({ useLogout: () => vi.fn() }));
vi.mock("../common/use-language-switch", () => ({
  useLanguageSwitch: () => ({
    currentLocale: "en",
    options: [{ value: "en", label: "English" }],
    changeLanguage: vi.fn(),
  }),
}));

function openThemeMenu() {
  renderWithI18n(<AccountMenu />);
  fireEvent.click(screen.getByRole("button", { name: "Account" }));
  fireEvent.click(screen.getByRole("menuitem", { name: /Theme/ }));
}

beforeEach(() => {
  theme.current = "light";
  useAppearanceStore.setState(DEFAULT_APPEARANCE);
  useInstalledThemesStore.setState({ themes: [], folders: {}, fromStore: {} });
});

afterEach(() => {
  theme.setTheme.mockReset();
});

describe("AccountMenu theme submenu", () => {
  it("shows the mode and the active colour theme on the Theme row", () => {
    theme.current = "dark";
    useAppearanceStore.setState({ theme: "github" });
    renderWithI18n(<AccountMenu />);
    fireEvent.click(screen.getByRole("button", { name: "Account" }));

    expect(screen.getByRole("menuitem", { name: /Theme/ })).toHaveTextContent("Dark·GitHub");
  });

  it("truncates a long installed theme name instead of the row label", () => {
    const name = "A very long community theme name, forty!";
    useInstalledThemesStore.setState({
      themes: [{ apiVersion: 1, id: "long", name, css: "" }],
      folders: {},
      fromStore: {},
    });
    useAppearanceStore.setState({ theme: "long" });
    renderWithI18n(<AccountMenu />);
    fireEvent.click(screen.getByRole("button", { name: "Account" }));

    const row = screen.getByRole("menuitem", { name: /Theme/ });
    expect(within(row).getByText("Theme")).toHaveClass("shrink-0");
    expect(within(row).getByText(name)).toHaveClass("truncate");
  });

  it("switches the colour theme and keeps the menu open to compare", async () => {
    openThemeMenu();

    const pop = await screen.findByRole("menuitemradio", { name: "Pop" });
    expect(screen.getByRole("menuitemradio", { name: "Default" })).toHaveAttribute("aria-checked", "true");
    fireEvent.click(pop);

    expect(useAppearanceStore.getState().theme).toBe("pop");
    expect(screen.getByRole("menuitemradio", { name: "Pop" })).toHaveAttribute("aria-checked", "true");
  });

  it("switches between light, dark and system and closes like before", async () => {
    openThemeMenu();

    const mode = (await screen.findByText("Mode")).closest("[role=group]") as HTMLElement;
    expect(within(mode).getByRole("menuitemradio", { name: "Light" })).toHaveAttribute("aria-checked", "true");
    fireEvent.click(within(mode).getByRole("menuitemradio", { name: "Dark" }));

    expect(theme.setTheme).toHaveBeenCalledWith("dark");
    await waitFor(() => expect(screen.queryByRole("menuitemradio", { name: "Pop" })).not.toBeInTheDocument());
  });

  it("links to the appearance settings", async () => {
    openThemeMenu();

    expect(await screen.findByRole("menuitem", { name: "Appearance settings" })).toHaveAttribute(
      "href",
      "/acme/settings?tab=appearance",
    );
  });
});
