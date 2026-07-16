// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

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
  afterEach(cleanup);
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

  it("renders apps as a data table with collection, ownership, version, status, and health", async () => {
    listApps.mockResolvedValue({ apps: [{
      id: "app-1", slug: "allergen-formatter", name: "Allergen Formatter", description: "Format ingredients",
      icon: "blocks", folder: "Operations", current_version: "1.0.0", status: "published",
    }] });
    listAppAdminOverview.mockResolvedValue([{
      id: "app-1", name: "Allergen Formatter", owner: "Jesper Hvejsel", version: "1.0.0", status: "published",
      approved_scopes: [], spend_cents: 0, runs: 4, failed_runs: 0, health: "healthy", touched: [],
    }]);

    render(<AppsPage />);

    const table = await screen.findByRole("table", { name: "Workspace apps" });
    for (const heading of ["App", "Collection", "Owner", "Version", "Status", "Health"]) {
      expect(table).toHaveTextContent(heading);
    }
    expect(table).toHaveTextContent("Allergen Formatter");
    expect(table).toHaveTextContent("Operations");
    expect(table).toHaveTextContent("Jesper Hvejsel");
    expect(table).toHaveTextContent("1.0.0");
    expect(table).toHaveTextContent("Published");
    expect(table).toHaveTextContent("Healthy");

    await userEvent.click(screen.getByRole("link", { name: /Allergen Formatter/ }));
    expect(push).toHaveBeenCalledWith("/firtal/apps/app-1");
    expect(screen.queryByRole("region", { name: "App folder management" })).not.toBeInTheDocument();
  });

  it("shows a stable loading state while the catalog is being fetched", () => {
    listApps.mockReturnValue(new Promise(() => undefined));
    render(<AppsPage />);
    expect(screen.getByText("Loading apps…")).toBeInTheDocument();
    expect(screen.queryByText("No apps yet")).not.toBeInTheDocument();
  });
});
