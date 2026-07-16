// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const getAppDetail = vi.fn();
const publishAppVersion = vi.fn();
const workspace = { id: "ws-1", slug: "firtal" };

vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => true }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => workspace }));
vi.mock("../core/api", () => ({
  getAppDetail: (...args: unknown[]) => getAppDetail(...args),
  publishAppVersion: (...args: unknown[]) => publishAppVersion(...args),
}));

import { AppEditorPage } from "./app-editor-page";

describe("AppEditorPage", () => {
  afterEach(cleanup);

  beforeEach(() => {
    getAppDetail.mockReset();
    getAppDetail.mockResolvedValue({ id: "app-1", name: "Returns helper", status: "draft", versions: [] });
    publishAppVersion.mockReset();
    publishAppVersion.mockResolvedValue({ deployment_status: "provisioning" });
  });

  it("edits the complete starter package and blocks publish when the manifest is invalid", async () => {
    render(<AppEditorPage appId="app-1" />);
    expect(await screen.findByRole("tab", { name: "app.json" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "frontend/index.html" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "frontend/app.js" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "backend/index.mjs" })).toBeInTheDocument();
    expect(screen.getByLabelText("Import files")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Export package" })).toBeInTheDocument();

    fireEvent.change(screen.getByRole("textbox", { name: "Source for app.json" }), { target: { value: "{{}" } });
    expect(await screen.findByRole("alert")).toHaveTextContent("app.json must contain valid JSON");
    expect(screen.getByRole("button", { name: "Publish" })).toBeDisabled();
  });

  it("publishes the immutable source package with release notes", async () => {
    render(<AppEditorPage appId="app-1" />);
    await screen.findByRole("tab", { name: "app.json" });
    await userEvent.click(screen.getByRole("button", { name: "Publish" }));
    await userEvent.type(screen.getByLabelText("Release notes"), "Initial release");
    await userEvent.click(screen.getByRole("button", { name: "Publish version" }));

    expect(publishAppVersion).toHaveBeenCalledWith("app-1", expect.objectContaining({ version: "0.1.0", release_notes: "Initial release" }), "firtal");
  });
});
