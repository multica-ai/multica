import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import {
  ISSUE_SIDEBAR_SECTIONS,
  useIssueSidebarSectionsStore,
  type IssueSidebarSection,
} from "@multica/core/issues/stores";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { IssueSidebarSections, letSidebarSettingsDragKeysThrough } from "./issue-sidebar-settings";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

// Every section renders its id, except Apps, which has nothing on this issue.
const sections = Object.fromEntries(
  ISSUE_SIDEBAR_SECTIONS.map((id) => [id, id === "apps" ? null : <section>{`body:${id}`}</section>]),
) as Record<IssueSidebarSection, React.ReactNode>;
const available = ISSUE_SIDEBAR_SECTIONS.filter((id) => id !== "plugins");

function renderSections(revealed?: IssueSidebarSection[]) {
  return render(
    <I18nProvider resources={TEST_RESOURCES} locale="en">
      <IssueSidebarSections sections={sections} available={available} revealed={revealed} />
    </I18nProvider>,
  );
}

const renderedOrder = () => screen.getAllByText(/^body:/).map((el) => el.textContent!.slice("body:".length));

async function openSettings() {
  fireEvent.click(screen.getByRole("button", { name: "Customize sidebar" }));
  return screen.findByRole("dialog");
}

describe("IssueSidebarSections", () => {
  beforeEach(() => useIssueSidebarSectionsStore.getState().reset());

  it("renders the available sections in the default order", () => {
    renderSections();
    expect(renderedOrder()).toEqual(available.filter((id) => id !== "apps"));
  });

  it("follows the saved order and leaves hidden sections out", () => {
    act(() => {
      useIssueSidebarSectionsStore.getState().setOrder(["execution_log", ...ISSUE_SIDEBAR_SECTIONS]);
      useIssueSidebarSectionsStore.getState().setHidden("details", true);
      useIssueSidebarSectionsStore.getState().setHidden("metadata", true);
    });
    renderSections();

    expect(renderedOrder().slice(0, 2)).toEqual(["execution_log", "properties"]);
    expect(screen.queryByText("body:details")).toBeNull();
    expect(screen.getByRole("button", { name: /2 sections hidden/ })).toBeInTheDocument();
  });

  it("shows a hidden section the reader jumped to", () => {
    act(() => useIssueSidebarSectionsStore.getState().setHidden("wakeups", true));
    renderSections(["wakeups"]);
    expect(screen.getByText("body:wakeups")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /hidden/ })).toBeNull();
  });

  it("lists the available sections, marks the empty and locked ones, and hides on toggle", async () => {
    renderSections();
    const dialog = await openSettings();

    expect(within(dialog).queryByRole("switch", { name: "Plugins" })).toBeNull();
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

describe("letSidebarSettingsDragKeysThrough", () => {
  const keyDownOn = (target: EventTarget) => {
    const preventBaseUIHandler = vi.fn();
    letSidebarSettingsDragKeysThrough({ target, preventBaseUIHandler } as unknown as Parameters<
      typeof letSidebarSettingsDragKeysThrough
    >[0]);
    return preventBaseUIHandler;
  };

  it("lets keys out of the dialog only while a settings row is held", () => {
    const handle = document.createElement("button");
    handle.setAttribute("data-sidebar-settings-handle", "");
    expect(keyDownOn(handle)).not.toHaveBeenCalled();

    handle.setAttribute("aria-pressed", "true");
    expect(keyDownOn(handle)).toHaveBeenCalledOnce();
    expect(keyDownOn(document.createElement("button"))).not.toHaveBeenCalled();
  });
});
