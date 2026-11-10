import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { useIssueSidebarLayoutStore } from "@multica/core/issues/stores";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { IssueSidebarTabs } from "./issue-sidebar-tabs";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

const panels = {
  overview: <div>Properties body</div>,
  delivery: <div>Pull requests body</div>,
  runs: null,
};

function renderTabs(props: Partial<Parameters<typeof IssueSidebarTabs>[0]> = {}) {
  return render(
    <I18nProvider resources={TEST_RESOURCES} locale="en">
      <IssueSidebarTabs panels={panels} {...props} />
    </I18nProvider>,
  );
}

const panelOf = (text: string) => screen.getByText(text).closest('[role="tabpanel"]');

async function chooseLayoutItem(name: string) {
  fireEvent.click(screen.getAllByRole("button", { name: "Sidebar layout" })[0]!);
  fireEvent.click(await screen.findByRole("menuitem", { name }));
}

describe("IssueSidebarTabs", () => {
  beforeEach(() => {
    useIssueSidebarLayoutStore.getState().reset();
  });

  it("opens on Overview and keeps the other tabs mounted", () => {
    renderTabs({ counts: { delivery: 3 } });

    expect(screen.getByRole("tab", { name: "Overview" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: /^Delivery/ })).toHaveTextContent("3");
    expect(panelOf("Properties body")).not.toHaveAttribute("hidden");
    expect(panelOf("Pull requests body")).toHaveAttribute("hidden");
  });

  it("remembers the selected tab for every issue", () => {
    const { unmount } = renderTabs();
    fireEvent.click(screen.getByRole("tab", { name: "Delivery" }));
    unmount();

    renderTabs();
    expect(screen.getByRole("tab", { name: "Delivery" })).toHaveAttribute("aria-selected", "true");
  });

  it("splits the active tab into a group below, then merges and resets", async () => {
    renderTabs();
    fireEvent.click(screen.getByRole("tab", { name: "Runs" }));

    await chooseLayoutItem("Move Runs to a new group below");
    expect(screen.getAllByRole("tablist")).toHaveLength(2);
    expect(useIssueSidebarLayoutStore.getState().layout).toEqual([
      { id: "g1", tabs: ["overview", "delivery"], active: "overview" },
      { id: "g2", tabs: ["runs"], active: "runs" },
    ]);

    await chooseLayoutItem("Merge all groups");
    expect(screen.getAllByRole("tablist")).toHaveLength(1);

    await chooseLayoutItem("Reset layout");
    expect(useIssueSidebarLayoutStore.getState().layout).toBeNull();
  });

  it("follows a tab chosen from outside in the compact sheet", async () => {
    useIssueSidebarLayoutStore.getState().setLayout([
      { id: "g1", tabs: ["overview", "delivery"], active: "overview" },
      { id: "g2", tabs: ["runs"], active: "runs" },
    ]);
    renderTabs({ compact: true });

    expect(screen.getAllByRole("tablist")).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Sidebar layout" })).toBeNull();
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveAttribute("aria-selected", "true");

    // Runs is already active in its own group, so only the reveal moves the sheet.
    act(() => useIssueSidebarLayoutStore.getState().selectTab("runs"));
    await waitFor(() => expect(screen.getByRole("tab", { name: "Runs" })).toHaveAttribute("aria-selected", "true"));
  });
});
