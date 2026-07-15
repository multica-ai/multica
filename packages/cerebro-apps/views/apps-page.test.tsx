// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const push = vi.fn();
const listApps = vi.fn();
const listAppAdminOverview = vi.fn();

vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => true }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ id: "ws-1", slug: "firtal" }) }));
vi.mock("@multica/views/navigation", () => ({
  useNavigation: () => ({ push }),
  AppLink: ({ href, children, ...props }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { href: string }) => (
    <a href={href} onClick={(event) => { event.preventDefault(); push(href); }} {...props}>{children}</a>
  ),
}));
vi.mock("../core/api", () => ({ listApps: () => listApps(), listAppAdminOverview: () => listAppAdminOverview(), listAppFolders: vi.fn().mockResolvedValue([]), installAllergenFormatter: vi.fn(), createAppFolder: vi.fn(), updateAppFolder: vi.fn(), deleteAppFolder: vi.fn(), moveAppToFolder: vi.fn() }));

import { AppsPage } from "./apps-page";

describe("AppsPage", () => {
  beforeEach(() => {
    push.mockReset();
    listApps.mockReset();
    listApps.mockResolvedValue({ apps: [] });
    listAppAdminOverview.mockResolvedValue([]);
  });

  it("opens the real app builder from Build app", async () => {
    render(<AppsPage />);
    await waitFor(() => expect(listApps).toHaveBeenCalled());
    await userEvent.click(screen.getByRole("link", { name: "Build app" }));
    expect(push).toHaveBeenCalledWith("/firtal/apps/new");
  });
});
