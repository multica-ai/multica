// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const getAppDetail = vi.fn();

vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => true }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ id: "ws-1", slug: "firtal" }) }));
vi.mock("../core/api", () => ({
  getAppDetail: (id: string) => getAppDetail(id),
  callAppSdk: vi.fn(),
}));

import { AppDetailPage } from "./app-detail-page";

const app = {
  id: "app-1",
  slug: "allergen-formatter",
  name: "Allergen Formatter",
  description: "Format ingredients",
  icon: "blocks",
  folder: "Operations",
  current_version: "1.0.0",
  status: "published" as const,
  versions: [{ version: "1.0.0", release_notes: "Initial fixture", grant_status: "approved", scopes: [] }],
};

describe("AppDetailPage", () => {
  beforeEach(() => {
    getAppDetail.mockReset();
    getAppDetail.mockResolvedValue(app);
  });

  it("opens the published app as the full workspace surface without workflow controls", async () => {
    render(<AppDetailPage appId="app-1" runtimeBaseUrl="/api/cerebro/apps-runtime" />);
    await screen.findByRole("heading", { name: "Allergen Formatter" });
    const frame = screen.getByTitle("Allergen Formatter");
    expect(frame).toHaveAttribute(
      "src",
      "/api/cerebro/apps-runtime/apps/app-1/1.0.0/index.html",
    );
    expect(frame).toHaveClass("h-full");
    expect(screen.queryByText("Workflow")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Test workflow/i })).not.toBeInTheDocument();
  });
});
