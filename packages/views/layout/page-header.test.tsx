import { ListTodo, Zap } from "@multica/ui/icons";
import { act, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { SidebarProvider, useSidebar } from "@multica/ui/components/ui/sidebar";
import { renderWithI18n } from "../test/i18n";
import { AppShellProvider } from "./app-shell-context";
import {
  CollectionCreateButton,
  CollectionPageHeader,
} from "./collection-page";
import { CollapsedNavTrigger, PAGE_GUTTER, PageHeader } from "./page-header";

const navigation = vi.hoisted(() => ({ pathname: "/acme/issues" }));
vi.mock("../navigation", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../navigation")>()),
  useNavigation: () => ({ pathname: navigation.pathname }),
}));

// The layout rules under test are documented on `PageHeader` itself; each
// test here pins one of them against the rendered output.

function renderHeader(
  ui: React.ReactElement,
  providerProps?: { hasExternalTrigger?: boolean },
) {
  const { container } = renderWithI18n(
    <SidebarProvider {...providerProps}>{ui}</SidebarProvider>,
  );
  return within(container).getByRole("banner");
}

// Below `lg` the collapsed-nav trigger renders as a third flex item, so the
// title stays left only while nothing distributes the free space: the content
// group grows and the header never uses `justify-between`.
function expectTitleLeftOfFreeSpace(header: HTMLElement) {
  const trigger = header.querySelector("[data-slot='sidebar-trigger']");
  expect(trigger).not.toBeNull();
  expect(header.firstElementChild).toBe(trigger);
  expect(header).not.toHaveClass("justify-between");
}

describe("PageHeader title alignment", () => {
  it("keeps a collection title beside the nav trigger instead of centering it", () => {
    const header = renderHeader(
      <CollectionPageHeader
        icon={Zap}
        title="Autopilot"
        actions={<CollectionCreateButton label="New autopilot" />}
      />,
    );

    expectTitleLeftOfFreeSpace(header);

    const heading = within(header).getByRole("heading");
    expect(heading.textContent).toBe("Autopilot");
    expect(heading.parentElement).toHaveClass("flex-1");
  });

  it("keeps the issues-style inline title packed against the nav trigger", () => {
    const header = renderHeader(
      <PageHeader>
        <ListTodo className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-body font-medium">Issues</h1>
      </PageHeader>,
    );

    expectTitleLeftOfFreeSpace(header);
    expect(header.children).toHaveLength(3);
  });
});

describe("PageHeader base chrome", () => {

  it("does not let a call site override the shared gutter", () => {
    const header = renderHeader(
      <PageHeader className="px-8">
        <h1>Inbox</h1>
      </PageHeader>,
    );

    expect(header).toHaveClass(PAGE_GUTTER);
    expect(header).not.toHaveClass("px-8");
  });

  // A shell that keeps its own trigger on screen (the desktop window toolbar)
  // gets no fallback one: the header's copy is the same icon 50px below the
  // shell's, and a list/detail surface stacked a third alongside it (MUL-6218).
  it("drops its trigger under a shell that keeps its own on screen", () => {
    const header = renderHeader(
      <PageHeader>
        <h1>Inbox</h1>
      </PageHeader>,
      { hasExternalTrigger: true },
    );

    expect(header.querySelector("[data-slot='sidebar-trigger']")).toBeNull();
    expect(within(header).getByRole("heading")).toBe(header.firstElementChild);
  });

  // The pages that build their own chrome (settings) reach for the trigger
  // directly, so the rule has to live in the trigger and not in `PageHeader`.
  it("drops a directly rendered trigger under that shell too", () => {
    const { container } = renderWithI18n(
      <SidebarProvider hasExternalTrigger>
        <CollapsedNavTrigger />
      </SidebarProvider>,
    );

    expect(container.querySelector("[data-slot='sidebar-trigger']")).toBeNull();
  });

  // Outside a `SidebarProvider` there is no nav to reopen at all.
  it("renders nothing when no sidebar is mounted", () => {
    const { container } = renderWithI18n(<CollapsedNavTrigger />);

    expect(container.querySelector("[data-slot='sidebar-trigger']")).toBeNull();
  });
});

// In the app shell the rail is always on screen from `md` up, so the trigger
// only has a job there while the space panel is collapsed (and below `md`,
// where the rail lives in the sheet).
describe("CollapsedNavTrigger in the app shell", () => {
  let sidebar: ReturnType<typeof useSidebar> | null = null;
  function Reader() {
    sidebar = useSidebar();
    return null;
  }
  function renderInShell(pathname: string) {
    navigation.pathname = pathname;
    const { container } = renderWithI18n(
      <SidebarProvider>
        <AppShellProvider>
          <Reader />
          <CollapsedNavTrigger />
        </AppShellProvider>
      </SidebarProvider>,
    );
    return () => container.querySelector("[data-slot='sidebar-trigger']");
  }

  it("is a compact-only trigger while the panel is open", () => {
    const trigger = renderInShell("/acme/issues");
    expect(trigger()).toHaveClass("md:hidden");
  });

  it("shows at every width once the panel is collapsed", () => {
    const trigger = renderInShell("/acme/issues");
    act(() => sidebar!.setOpen(false));
    expect(trigger()).not.toHaveClass("md:hidden");
  });

  // Inbox brings its own list column, so the panel's open state is not what
  // the trigger reopens there; the collapsed list column is.
  it("follows the list column in spaces without a shell panel", () => {
    const trigger = renderInShell("/acme/inbox");
    act(() => sidebar!.setOpen(false));
    expect(trigger()).toHaveClass("md:hidden");

    act(() => (trigger() as HTMLButtonElement).click());
    expect(trigger()).not.toHaveClass("md:hidden");
    expect(sidebar!.state).toBe("collapsed");
  });
});
