// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const getAppDetail = vi.fn();
const getAppVersionFiles = vi.fn();
const publishAppVersion = vi.fn();
const workspace = { id: "ws-1", slug: "firtal" };

vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => true }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => workspace }));
vi.mock("../core/api", () => ({
  getAppDetail: (...args: unknown[]) => getAppDetail(...args),
  getAppVersionFiles: (...args: unknown[]) => getAppVersionFiles(...args),
  publishAppVersion: (...args: unknown[]) => publishAppVersion(...args),
}));

import { AppEditorPage } from "./app-editor-page";

describe("AppEditorPage", () => {
  afterEach(cleanup);

  beforeEach(() => {
    getAppDetail.mockReset();
    getAppDetail.mockResolvedValue({ id: "app-1", name: "Returns helper", status: "draft", versions: [] });
    getAppVersionFiles.mockReset();
    publishAppVersion.mockReset();
    publishAppVersion.mockResolvedValue({ deployment_status: "provisioning" });
  });

  it("loads the exact current version and proposes the next patch version", async () => {
    getAppDetail.mockResolvedValue({ id: "app-1", name: "Published helper", status: "published", current_version: "1.2.3", versions: [{ version: "1.2.3" }] });
    getAppVersionFiles.mockResolvedValue([
      { path: "app.json", media_type: "application/json", content: '{"manifest":{"schema_version":"1","name":"Published helper","version":"1.2.3","scopes":[],"frontend":{"entry":"frontend/index.html"}}}' },
      { path: "frontend/index.html", media_type: "text/html", content: "<h1>Saved source</h1>" },
    ]);
    render(<AppEditorPage appId="app-1" />);
    await screen.findByRole("tab", { name: "app.json" });
    await userEvent.click(screen.getByRole("tab", { name: "frontend/index.html" }));
    expect(screen.getByRole("textbox", { name: "Source for frontend/index.html" })).toHaveValue("<h1>Saved source</h1>");
    await userEvent.click(screen.getByRole("button", { name: "Publish" }));
    expect(screen.getByLabelText("Version")).toHaveValue("1.2.4");
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

  it("rewrites app.json manifest.version to match the published version", async () => {
    getAppDetail.mockResolvedValue({ id: "app-1", name: "Published helper", status: "published", current_version: "1.2.3", versions: [{ version: "1.2.3" }] });
    getAppVersionFiles.mockResolvedValue([
      { path: "app.json", media_type: "application/json", content: '{"manifest":{"schema_version":"1","name":"Published helper","version":"1.2.3","scopes":[],"frontend":{"entry":"frontend/index.html"}}}' },
      { path: "frontend/index.html", media_type: "text/html", content: "<h1>Saved source</h1>" },
    ]);
    render(<AppEditorPage appId="app-1" />);
    await screen.findByRole("tab", { name: "app.json" });
    await userEvent.click(screen.getByRole("button", { name: "Publish" }));
    expect(screen.getByLabelText("Version")).toHaveValue("1.2.4");
    await userEvent.type(screen.getByLabelText("Release notes"), "Patch");
    await userEvent.click(screen.getByRole("button", { name: "Publish version" }));

    const [, payload] = publishAppVersion.mock.calls[0] as [string, { version: string; files: { path: string; content: string }[] }, string];
    expect(payload.version).toBe("1.2.4");
    const manifestFile = payload.files.find((file) => file.path === "app.json");
    expect(JSON.parse(manifestFile!.content).manifest.version).toBe("1.2.4");
  });
});
