import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import {
  ISSUE_SIDEBAR_SECTIONS,
  useIssueSidebarSectionsStore,
  type IssueSidebarSection,
} from "@multica/core/issues/stores";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { DropdownMenuItem } from "@multica/ui/components/ui/dropdown-menu";
import { useIssueSidebarAddEntry } from "./issue-sidebar-add-menu";
import { IssueSidebarSections } from "./issue-sidebar-settings";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

// Every section renders its id, except Apps, which has nothing on this issue.
const sections = Object.fromEntries(
  ISSUE_SIDEBAR_SECTIONS.map((id) => [id, id === "apps" ? null : <section>{`body:${id}`}</section>]),
) as Record<IssueSidebarSection, React.ReactNode>;
const available = ISSUE_SIDEBAR_SECTIONS.filter((id) => id !== "plugins");

function renderSections(revealed?: IssueSidebarSection[]) {
  return render(
    <I18nProvider resources={TEST_RESOURCES} locale="en">
      <IssueSidebarSections sections={sections} available={available} revealed={revealed} overview={<p>overview</p>} />
    </I18nProvider>,
  );
}

const renderedOrder = () => screen.getAllByText(/^body:/).map((el) => el.textContent!.slice("body:".length));
const cardOf = (text: string) => screen.getByText(text).closest("[data-slot=issue-sidebar-card]");
const groupOf = (text: string) => screen.getByText(text).closest("[data-slot=issue-sidebar-group]");

async function openSettings() {
  fireEvent.click(screen.getByRole("button", { name: "Customize sidebar" }));
  return screen.findByRole("dialog");
}

describe("IssueSidebarSections", () => {
  beforeEach(() => useIssueSidebarSectionsStore.getState().reset());

  it("puts the overview on its own card, then every other section on one card in related groups", () => {
    renderSections();
    expect(renderedOrder()).toEqual(available.filter((id) => id !== "apps"));
    const cards = document.querySelectorAll("[data-slot=issue-sidebar-card]");
    expect(cards).toHaveLength(2);
    expect(cards[0]).toHaveTextContent("overview");
    expect(cardOf("body:properties")).toBe(cards[1]);
    expect(cardOf("body:wakeups")).toBe(cards[1]);
    expect(groupOf("body:pull_requests")).toBe(groupOf("body:environments"));
    expect(groupOf("body:parent")).toBe(groupOf("body:duplicates"));
    expect(groupOf("body:pull_requests")).not.toBe(groupOf("body:parent"));
    // Creator and timestamps are small print under the cards, not a card.
    expect(cardOf("body:details")).toBeNull();
  });

  it("leaves hidden sections out, and drops a group once all of its sections are hidden", () => {
    act(() => {
      useIssueSidebarSectionsStore.getState().setHidden("execution_log", true);
      useIssueSidebarSectionsStore.getState().setHidden("metadata", true);
    });
    const { container } = renderSections();

    expect(screen.queryByText("body:execution_log")).toBeNull();
    expect(container.querySelector('[data-issue-sidebar-section="execution_log"]')).toBeNull();
    expect(container.querySelectorAll("[data-slot=issue-sidebar-group]")).toHaveLength(4);
    expect(screen.getByRole("button", { name: /2 sections hidden/ })).toBeInTheDocument();
  });

  it("collapses the Output group while it has nothing to show, offering its actions from the strip's + menu", async () => {
    function EmptyOutput({ id, label }: { id: string; label: string }) {
      useIssueSidebarAddEntry(id, { order: 0, items: <DropdownMenuItem>{label}</DropdownMenuItem> });
      return null;
    }
    render(
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <IssueSidebarSections
          sections={{
            ...sections,
            pull_requests: <EmptyOutput id="pull_requests" label="Link pull request" />,
            environments: <EmptyOutput id="environments" label="Start an environment" />,
            deliverables: null,
          }}
          available={available}
          overview={<p>overview</p>}
        />
      </I18nProvider>,
    );
    const outputGroup = document.querySelector('[data-issue-sidebar-section="pull_requests"]')!.parentElement!;
    expect(outputGroup).toHaveAttribute("data-slot", "issue-sidebar-group");
    // The group hides itself once every section slot in it is empty.
    expect([...outputGroup.children].every((slot) => slot.childNodes.length === 0)).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(await screen.findByRole("menuitem", { name: "Link pull request" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Start an environment" })).toBeInTheDocument();
  });

  it("leaves the + menu out while no section has an action to offer", () => {
    renderSections();
    expect(screen.queryByRole("button", { name: "Add" })).toBeNull();
  });

  it("shows a hidden section the reader jumped to", () => {
    act(() => useIssueSidebarSectionsStore.getState().setHidden("wakeups", true));
    renderSections(["wakeups"]);
    expect(screen.getByText("body:wakeups")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /hidden/ })).toBeNull();
  });

  it("lists the available sections by card, marks the empty and locked ones, and hides on toggle", async () => {
    renderSections();
    const dialog = await openSettings();

    expect(within(dialog).queryByRole("switch", { name: "Plugins" })).toBeNull();
    expect(within(dialog).queryByRole("button", { name: /Reorder/ })).toBeNull();
    expect(within(dialog).getAllByRole("separator")).toHaveLength(5);
    expect(within(dialog).getByRole("switch", { name: "Properties" })).toHaveAttribute("aria-disabled", "true");
    expect(within(dialog).getByText("Always shown")).toBeInTheDocument();
    expect(within(dialog).getByRole("switch", { name: "Apps" }).closest("li")).toHaveTextContent("None on this issue");

    fireEvent.click(within(dialog).getByRole("switch", { name: "Details" }));
    expect(useIssueSidebarSectionsStore.getState().hidden).toEqual(["details"]);
    expect(screen.queryByText("body:details")).toBeNull();

    fireEvent.click(within(dialog).getByRole("button", { name: /Reset/ }));
    expect(useIssueSidebarSectionsStore.getState().hidden).toEqual([]);
    expect(screen.getByText("body:details")).toBeInTheDocument();
  });

  it("opens settings from the hidden-sections hint", async () => {
    act(() => useIssueSidebarSectionsStore.getState().setHidden("metadata", true));
    renderSections();

    fireEvent.click(screen.getByRole("button", { name: /1 section hidden/ }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("switch", { name: "Metadata" })).not.toBeChecked();
  });
});
