// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const getAppDetail = vi.fn();
const createWorkflow = vi.fn();
const testWorkflow = vi.fn();

vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => true }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ id: "ws-1", slug: "firtal" }) }));
vi.mock("../core/api", () => ({
  getAppDetail: (id: string) => getAppDetail(id),
  createWorkflow: (input: unknown) => createWorkflow(input),
  testWorkflow: (id: string, input: unknown) => testWorkflow(id, input),
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
  workflows: [],
};

describe("AppDetailPage", () => {
  beforeEach(() => {
    getAppDetail.mockReset();
    createWorkflow.mockReset();
    testWorkflow.mockReset();
    getAppDetail.mockResolvedValue(app);
    createWorkflow.mockResolvedValue({ id: "workflow-1" });
    testWorkflow.mockResolvedValue({ id: "run-1", status: "succeeded", step_log: [{ id: "read", status: "succeeded" }] });
  });

  it("opens the published app and runs the workflow fixture to a visible result", async () => {
    render(<AppDetailPage appId="app-1" runtimeBaseUrl="/api/cerebro/apps-runtime" />);
    await screen.findByRole("heading", { name: "Allergen Formatter" });
    expect(screen.getByTitle("Allergen Formatter")).toHaveAttribute(
      "src",
      "/api/cerebro/apps-runtime/apps/app-1/1.0.0/index.html",
    );

    await userEvent.click(screen.getByRole("button", { name: "Test workflow" }));
    await waitFor(() => expect(testWorkflow).toHaveBeenCalled());
    expect(await screen.findByText("Workflow test succeeded")).toBeInTheDocument();
  });
});
