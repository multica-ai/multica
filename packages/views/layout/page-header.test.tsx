import { ListTodo, Plus, Zap } from "lucide-react";
import { within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { SidebarProvider } from "@multica/ui/components/ui/sidebar";
import { renderWithI18n } from "../test/i18n";
import {
  CollectionPageHeader,
  CollectionPageHeaderAction,
} from "./collection-page";
import { PAGE_GUTTER, PageHeader } from "./page-header";

// The layout rules under test are documented on `PageHeader` itself; each
// test here pins one of them against the rendered output.

function renderHeader(ui: React.ReactElement) {
  const { container } = renderWithI18n(<SidebarProvider>{ui}</SidebarProvider>);
  return within(container).getByRole("banner");
}

// Below `xl` the collapsed-nav trigger renders as a third flex item, so the
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
        count={2}
        actions={
          <CollectionPageHeaderAction icon={Plus} label="New autopilot" />
        }
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
  it("supplies the trigger, gap and gutter without per-page classes", () => {
    const header = renderHeader(
      <PageHeader>
        <h1>Inbox</h1>
      </PageHeader>,
    );

    const trigger = header.querySelector("[data-slot='sidebar-trigger']")!;
    expect(trigger).toHaveClass("xl:hidden");
    expect(trigger.className).not.toMatch(/(^|\s)-?m[rsxe]?-/);
    expect(header).toHaveClass("gap-2", PAGE_GUTTER);
  });

  it("does not let a call site override the shared gutter", () => {
    const header = renderHeader(
      <PageHeader className="px-8">
        <h1>Inbox</h1>
      </PageHeader>,
    );

    expect(header).toHaveClass(PAGE_GUTTER);
    expect(header).not.toHaveClass("px-8");
  });

  // Collection and issues headers drifted apart when each declared its own
  // spacing; both must resolve to the base gap and gutter.
  it("resolves the same gutter and gap for collection and issues headers", () => {
    const collection = renderHeader(
      <CollectionPageHeader icon={Zap} title="Autopilot" count={2} />,
    );
    const issues = renderHeader(
      <PageHeader>
        <ListTodo className="h-4 w-4" />
        <h1>Issues</h1>
      </PageHeader>,
    );

    for (const header of [collection, issues]) {
      expect(header).toHaveClass("gap-2", PAGE_GUTTER);
    }
  });
});
