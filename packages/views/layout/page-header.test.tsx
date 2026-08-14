import { ListTodo, Plus, Zap } from "lucide-react";
import { describe, expect, it } from "vitest";

import { SidebarProvider } from "@multica/ui/components/ui/sidebar";
import { renderWithI18n } from "../test/i18n";
import {
  CollectionPageHeader,
  CollectionPageHeaderAction,
} from "./collection-page";
import { PAGE_GUTTER, PageHeader } from "./page-header";

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

  // The collection header and the issues header have to resolve to the same
  // leading edge and the same gap. Each `it` below is one axis of that, read
  // off both renders at once rather than asserted per page — a per-page
  // assertion is what let the Agents toolbar drift while its header moved.
  it.each([
    ["gutter", /^px-/, PAGE_GUTTER],
    ["gap", /^gap-/, "gap-2"],
  ])("resolves the same %s for collection and issues headers", (_axis, pattern, expected) => {
    const headerClass = (ui: React.ReactElement) =>
      Array.from(
        renderWithI18n(<SidebarProvider>{ui}</SidebarProvider>)
          .container.querySelector("header")!
          .classList,
      ).find((c) => pattern.test(c));

    const collection = headerClass(
      <CollectionPageHeader icon={Zap} title="Autopilot" count={2} />,
    );
    const issues = headerClass(
      <PageHeader className="gap-2">
        <ListTodo className="h-4 w-4" />
        <h1>Issues</h1>
      </PageHeader>,
    );

    expect(collection).toBe(expected);
    expect(collection).toBe(issues);
  });
});

/**
 * The gutter is a constant so a page cannot spell its own and drift; the
 * toolbar under a header reads the same one. Asserting the header renders the
 * constant is what makes importing it elsewhere meaningful — the earlier
 * version of this test hardcoded `px-4` in both the component and the
 * expectation, so it could only ever agree with itself.
 */
describe("PAGE_GUTTER", () => {
  it("is what PageHeader renders", () => {
    const { container } = renderWithI18n(
      <SidebarProvider>
        <PageHeader>
          <h1>Inbox</h1>
        </PageHeader>
      </SidebarProvider>,
    );

    expect(container.querySelector("header")).toHaveClass(PAGE_GUTTER);
  });
});
