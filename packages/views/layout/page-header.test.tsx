import { ListTodo, Plus, Zap } from "lucide-react";
import { describe, expect, it } from "vitest";

import { SidebarProvider } from "@multica/ui/components/ui/sidebar";
import { renderWithI18n } from "../test/i18n";
import {
  CollectionPageHeader,
  CollectionPageHeaderAction,
} from "./collection-page";
import { PageHeader } from "./page-header";

/**
 * Below `xl` the collapsed-nav trigger renders, so a header that reads as two
 * zones in source ("title left, actions right") is really three flex items.
 * `justify-between` then splits the free space on BOTH sides of the title and
 * parks it mid-header instead of beside the trigger — the desktop window is
 * narrower than `xl`, so that is where it shows up.
 *
 * The header stays left-aligned only while nothing distributes that space:
 * the content group grows and absorbs all of it.
 */
function expectTitleLeftOfFreeSpace(header: HTMLElement) {
  const trigger = header.querySelector("[data-slot='sidebar-trigger']");
  const items = Array.from(header.children);

  expect(trigger).not.toBeNull();
  expect(items[0]).toBe(trigger);
  expect(header).not.toHaveClass("justify-between");
}

describe("PageHeader title alignment", () => {
  it("keeps a collection title beside the nav trigger instead of centering it", () => {
    const { container } = renderWithI18n(
      <SidebarProvider>
        <CollectionPageHeader
          icon={Zap}
          title="Autopilot"
          count={2}
          actions={
            <CollectionPageHeaderAction icon={Plus} label="New autopilot" />
          }
        />
      </SidebarProvider>,
    );

    const header = container.querySelector<HTMLElement>("header")!;
    expectTitleLeftOfFreeSpace(header);

    const heading = header.querySelector("h1")!;
    expect(heading.textContent).toBe("Autopilot");
    expect(heading.parentElement).toHaveClass("flex-1");
  });

  it("keeps the issues-style inline title packed against the nav trigger", () => {
    const { container } = renderWithI18n(
      <SidebarProvider>
        <PageHeader className="gap-2">
          <ListTodo className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-body font-medium">Issues</h1>
        </PageHeader>
      </SidebarProvider>,
    );

    const header = container.querySelector<HTMLElement>("header")!;
    expectTitleLeftOfFreeSpace(header);
    expect(header.children).toHaveLength(3);
  });

  it("still reserves the leading slot for the nav trigger on compact widths", () => {
    const { container } = renderWithI18n(
      <SidebarProvider>
        <PageHeader>
          <h1>Issues</h1>
        </PageHeader>
      </SidebarProvider>,
    );

    const trigger = container.querySelector("[data-slot='sidebar-trigger']")!;
    expect(trigger).toHaveClass("xl:hidden");
  });
});

/**
 * The trigger is a flex item, so the header's own `gap` already separates it
 * from the title. When the trigger carried a margin as well, every header that
 * declared a gap paid both and its title started further right than the ones
 * that declared none — at 390px the Autopilot title sat 8px right of Issues.
 * One declared gap, one source.
 */
describe("PageHeader leading spacing", () => {
  it("gives the nav trigger no margin of its own", () => {
    const { container } = renderWithI18n(
      <SidebarProvider>
        <PageHeader>
          <h1>Inbox</h1>
        </PageHeader>
      </SidebarProvider>,
    );

    const trigger = container.querySelector("[data-slot='sidebar-trigger']")!;
    expect(trigger.className).not.toMatch(/(^|\s)-?m[rsxe]?-/);
  });

  it("spaces a header that declares no gap by the base gap", () => {
    const { container } = renderWithI18n(
      <SidebarProvider>
        <PageHeader>
          <h1>Inbox</h1>
        </PageHeader>
      </SidebarProvider>,
    );

    expect(container.querySelector("header")).toHaveClass("gap-2");
  });

  it("leads the collection header on the same gutter as the issues header", () => {
    const collection = renderWithI18n(
      <SidebarProvider>
        <CollectionPageHeader icon={Zap} title="Autopilot" count={2} />
      </SidebarProvider>,
    );
    const issues = renderWithI18n(
      <SidebarProvider>
        <PageHeader className="gap-2">
          <ListTodo className="h-4 w-4" />
          <h1>Issues</h1>
        </PageHeader>
      </SidebarProvider>,
    );

    // Whatever the base gutter is, a collection page must not override it —
    // that 4px is what made the Autopilot title sit right of Issues at every
    // width, including >= xl where the nav trigger is not even rendered.
    const gutterOf = (r: { container: HTMLElement }) =>
      Array.from(r.container.querySelector("header")!.classList).find((c) =>
        /^px-/.test(c),
      );

    expect(gutterOf(collection)).toBe("px-4");
    expect(gutterOf(collection)).toBe(gutterOf(issues));
  });

  it("leads the collection header on the same gap as the issues header", () => {
    const collection = renderWithI18n(
      <SidebarProvider>
        <CollectionPageHeader icon={Zap} title="Autopilot" count={2} />
      </SidebarProvider>,
    );
    const issues = renderWithI18n(
      <SidebarProvider>
        <PageHeader className="gap-2">
          <ListTodo className="h-4 w-4" />
          <h1>Issues</h1>
        </PageHeader>
      </SidebarProvider>,
    );

    const gapOf = (r: { container: HTMLElement }) =>
      Array.from(r.container.querySelector("header")!.classList).find((c) =>
        c.startsWith("gap-"),
      );

    expect(gapOf(collection)).toBe("gap-2");
    expect(gapOf(collection)).toBe(gapOf(issues));
  });
});
