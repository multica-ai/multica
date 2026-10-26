import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../test/i18n";
import { DropdownMenuItem } from "@multica/ui/components/ui/dropdown-menu";
import { IssueSidebarSettingsMenu } from "./issue-sidebar-settings-menu";

const push = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ settings: () => "/acme/settings" }) }));
vi.mock("../../navigation", () => ({ useNavigation: () => ({ push }) }));

beforeEach(() => push.mockReset());

describe("IssueSidebarSettingsMenu", () => {
  it.each(["quick-actions", "wakeups", "properties", "plugins", "code"] as const)("opens the workspace %s settings", async (tab) => {
    renderWithI18n(<IssueSidebarSettingsMenu title="Section" label="Section settings" tab={tab} />);
    fireEvent.click(screen.getByRole("button", { name: "Section options" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Section settings" }));
    expect(push).toHaveBeenCalledExactlyOnceWith(`/acme/settings?tab=${tab}`);
  });

  it("composes section actions before settings without navigating on action clicks", async () => {
    const action = vi.fn();
    renderWithI18n(
      <IssueSidebarSettingsMenu title="Pull requests" triggerLabel="Pull request automation" label="PR merge settings" tab="code">
        <DropdownMenuItem onClick={action}>Keep status when PRs merge</DropdownMenuItem>
      </IssueSidebarSettingsMenu>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Pull request automation" }));
    const item = await screen.findByRole("menuitem", { name: "Keep status when PRs merge" });
    expect(screen.getAllByRole("menuitem").map((node) => node.textContent)).toEqual(["Keep status when PRs merge", "PR merge settings"]);
    fireEvent.click(item);
    expect(action).toHaveBeenCalledOnce();
    expect(push).not.toHaveBeenCalled();
  });

  it("supports keyboard opening and Escape restores focus without navigating", async () => {
    const user = userEvent.setup();
    renderWithI18n(<IssueSidebarSettingsMenu title="Properties" label="Custom property settings" tab="properties" />);
    const trigger = screen.getByRole("button", { name: "Properties options" });
    trigger.focus();
    await user.keyboard("{Enter}");
    expect(await screen.findByRole("menuitem", { name: "Custom property settings" })).toBeVisible();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(trigger).toHaveFocus());
    expect(push).not.toHaveBeenCalled();
  });

  it("localizes the menu trigger", () => {
    renderWithI18n(<IssueSidebarSettingsMenu title="属性" label="自定义属性设置" tab="properties" />, { locale: "zh-Hans" });
    expect(screen.getByRole("button", { name: "属性选项" })).toBeVisible();
  });
});
