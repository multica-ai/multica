// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mkdtemp, mkdir, rm, symlink, writeFile } from "fs/promises";
import { tmpdir } from "os";
import { join } from "path";
import type { IpcMainInvokeEvent } from "electron";
import { readLocalFile, setupLocalFileReading } from "./local-file";
import { AuthSessionCoordinator } from "./auth-session-coordinator";

const electron = vi.hoisted(() => ({
  handle: vi.fn(),
  fromWebContents: vi.fn(),
  openPath: vi.fn(),
}));
vi.mock("electron", () => ({
  ipcMain: { handle: electron.handle },
  BrowserWindow: { fromWebContents: electron.fromWebContents },
  shell: { openPath: electron.openPath },
}));

const context = { apiUrl: "https://server.example.test", token: "synthetic-token", daemonId: "local-daemon" };
const fetchResources = vi.fn();
let base: string;
let root: string;
let document: string;

function resource(overrides = {}) {
  return {
    id: "resource",
    workspace_id: "workspace",
    project_id: "project",
    resource_type: "local_directory",
    resource_ref: { local_path: root, daemon_id: context.daemonId },
    ...overrides,
  };
}
function request(href = document) {
  return { workspaceId: "workspace", projectId: "project", href };
}
function reply(resources: unknown = [resource()]) {
  fetchResources.mockResolvedValue({ ok: true, json: async () => ({ resources }) });
}

beforeEach(async () => {
  vi.clearAllMocks();
  base = await mkdtemp(join(tmpdir(), "multica-file-link-"));
  root = join(base, "project");
  await mkdir(join(root, "openspec"), { recursive: true });
  document = join(root, "openspec", "proposal.md");
  await writeFile(document, "# Synthetic proposal\n");
  electron.openPath.mockResolvedValue("");
  electron.fromWebContents.mockReturnValue({});
  vi.stubGlobal("fetch", fetchResources);
  reply();
});

afterEach(async () => {
  expect(electron.openPath).not.toHaveBeenCalled();
  vi.unstubAllGlobals();
  await rm(base, { recursive: true, force: true });
});

describe("project-scoped local document preview", () => {
  it("returns a document for the in-app preview without opening an external app", async () => {
    await expect(readLocalFile(request(), context)).resolves.toEqual({
      ok: true, filename: "proposal.md", contentType: "text/markdown",
      text: "# Synthetic proposal\n", sizeBytes: Buffer.byteLength("# Synthetic proposal\n"),
    });
    expect(electron.openPath).not.toHaveBeenCalled();
  });

  it.each(["absolute", "relative", "dot-relative"])("reads a %s Markdown reference for preview", async (kind) => {
    const href = kind === "absolute" ? document : kind === "relative" ? "openspec/proposal.md" : "./openspec/proposal.md";
    await expect(readLocalFile(request(href), context)).resolves.toMatchObject({ ok: true, text: expect.any(String) });
    expect(fetchResources).toHaveBeenCalledWith("https://server.example.test/api/projects/project/resources", {
      headers: { Authorization: "Bearer synthetic-token", "X-Workspace-ID": "workspace" },
      redirect: "error",
      signal: expect.any(AbortSignal),
    });
  });

  it("decodes a path once and ignores a Markdown fragment", async () => {
    const file = join(root, "proposal #1 100%.md");
    await writeFile(file, "synthetic");
    await expect(readLocalFile(request(encodeURI(file).replace("#", "%23") + "#section"), context))
      .resolves.toMatchObject({ ok: true, text: expect.any(String) });
  });

  it.each(["notes.txt", "notes.markdown", "notes.MD"])("allows the text document %s", async (name) => {
    const file = join(root, name);
    await writeFile(file, "synthetic");
    await expect(readLocalFile(request(file), context)).resolves.toMatchObject({ ok: true, text: expect.any(String) });
  });

  it.each([
    "../outside.md",
    "openspec/../proposal.md",
    "openspec/%2e%2e/proposal.md",
    "//remote.example/project/proposal.md",
    "\\\\server\\share\\proposal.md",
    "file:///project/proposal.md",
    "https://example.test/proposal.md",
    "javascript:alert(1)",
    "./proposal.md?download=1",
    "./proposal%00.md",
    "./proposal%0a.md",
    "./proposal%.md",
  ])("rejects unsafe or unsupported href %s", async (href) => {
    expect(await readLocalFile(request(href), context)).toMatchObject({ ok: false });
    expect(electron.openPath).not.toHaveBeenCalled();
    expect(fetchResources).not.toHaveBeenCalled();
  });

  it("rejects a sibling path sharing the project prefix", async () => {
    const sibling = join(base, "project-other");
    await mkdir(sibling);
    const file = join(sibling, "proposal.md");
    await writeFile(file, "synthetic");
    await expect(readLocalFile(request(file), context)).resolves.toEqual({ ok: false, reason: "outside_project" });
    expect(electron.openPath).not.toHaveBeenCalled();
  });

  it("rejects a symlink escaping the project", async () => {
    const file = join(base, "outside.md");
    await writeFile(file, "synthetic");
    await symlink(file, join(root, "escape.md"));
    await expect(readLocalFile(request("./escape.md"), context))
      .resolves.toEqual({ ok: false, reason: "outside_project" });
    expect(electron.openPath).not.toHaveBeenCalled();
  });

  it("rejects a Markdown symlink to an executable inside the project", async () => {
    await writeFile(join(root, "script.sh"), "synthetic");
    await symlink(join(root, "script.sh"), join(root, "script.md"));
    await expect(readLocalFile(request("./script.md"), context))
      .resolves.toEqual({ ok: false, reason: "unsupported_type" });
    expect(electron.openPath).not.toHaveBeenCalled();
  });

  it("supports a linked project root without relaxing the canonical boundary", async () => {
    const link = join(base, "linked-project");
    await symlink(root, link);
    reply([resource({ resource_ref: { local_path: link, daemon_id: context.daemonId } })]);
    await expect(readLocalFile(request(join(link, "openspec/proposal.md")), context)).resolves.toMatchObject({ ok: true, text: expect.any(String) });
  });

  it("reports missing files and rejects a directory named like a document", async () => {
    await expect(readLocalFile(request("./missing.md"), context))
      .resolves.toEqual({ ok: false, reason: "not_found" });
    await mkdir(join(root, "directory.md"));
    await expect(readLocalFile(request("./directory.md"), context))
      .resolves.toEqual({ ok: false, reason: "unsupported_type" });
    expect(electron.openPath).not.toHaveBeenCalled();
  });

  it.each([
    { workspace_id: "other-workspace" },
    { project_id: "other-project" },
    { resource_type: "github_repo" },
    { resource_ref: { local_path: "/untrusted", daemon_id: "other-daemon" } },
  ])("does not trust a mismatched resource %j", async (overrides) => {
    reply([resource(overrides)]);
    await expect(readLocalFile(request(), context)).resolves.toEqual({ ok: false, reason: "unavailable" });
    expect(electron.openPath).not.toHaveBeenCalled();
  });

  it("rejects unavailable or ambiguous project resource data", async () => {
    reply([resource(), resource()]);
    await expect(readLocalFile(request(), context)).resolves.toEqual({ ok: false, reason: "unavailable" });
    reply("not-an-array");
    await expect(readLocalFile(request(), context)).resolves.toEqual({ ok: false, reason: "unavailable" });
    expect(electron.openPath).not.toHaveBeenCalled();
  });

  it("reports unsupported document types before contacting the server", async () => {
    await expect(readLocalFile(request("./script.sh"), context))
      .resolves.toEqual({ ok: false, reason: "unsupported_type" });
    expect(fetchResources).not.toHaveBeenCalled();
  });

  it("does not access resources without a resolved local context", async () => {
    await expect(readLocalFile(request(), null)).resolves.toEqual({ ok: false, reason: "unavailable" });
    expect(fetchResources).not.toHaveBeenCalled();
  });

  it("reports a resource API failure without an external fallback", async () => {
    fetchResources.mockRejectedValueOnce(new Error("offline"));
    await expect(readLocalFile(request(), context)).resolves.toEqual({ ok: false, reason: "unavailable" });
    expect(electron.openPath).not.toHaveBeenCalled();
  });

  it.each(["", "# Unicode preview — café ☕\n"])("preserves empty and UTF-8 document text", async (text) => {
    await writeFile(document, text);
    await expect(readLocalFile(request(), context)).resolves.toEqual({
      ok: true, filename: "proposal.md", contentType: "text/markdown", text,
      sizeBytes: Buffer.byteLength(text),
    });
  });

  it("returns plain-text metadata for a text document", async () => {
    const file = join(root, "notes.txt");
    await writeFile(file, "# Literal heading");
    await expect(readLocalFile(request(file), context)).resolves.toEqual({
      ok: true, filename: "notes.txt", contentType: "text/plain",
      text: "# Literal heading", sizeBytes: 17,
    });
  });

  it("accepts the preview size boundary and rejects oversized files", async () => {
    await writeFile(document, "x".repeat(2 * 1024 * 1024));
    const result = await readLocalFile(request(), context);
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.sizeBytes).toBe(2 * 1024 * 1024);
    await writeFile(document, "x".repeat(2 * 1024 * 1024 + 1));
    await expect(readLocalFile(request(), context)).resolves.toEqual({ ok: false, reason: "too_large" });
  });

  it("rejects malformed identifiers and requests", async () => {
    for (const value of [null, {}, { ...request(), projectId: "../other" }, { ...request(), workspaceId: "bad\r\nheader" }]) {
      expect(await readLocalFile(value, context)).toMatchObject({ ok: false });
    }
    expect(fetchResources).not.toHaveBeenCalled();
  });
});

describe("local file IPC", () => {
  it("accepts only the owning window's main frame", async () => {
    const getContext = vi.fn().mockResolvedValue(context);
    const authorizeSender = vi.fn().mockReturnValue(true);
    setupLocalFileReading(getContext, authorizeSender);
    const handler = electron.handle.mock.calls.find(([channel]) => channel === "file:read-local")?.[1];
    expect(handler).toBeTypeOf("function");
    const mainFrame = {};
    const event = { sender: { mainFrame }, senderFrame: mainFrame } as IpcMainInvokeEvent;
    await expect(handler(event, request())).resolves.toMatchObject({ ok: true, text: expect.any(String) });
    expect(electron.openPath).not.toHaveBeenCalled();
    getContext.mockClear();
    electron.openPath.mockClear();
    await expect(handler({ ...event, senderFrame: {} }, request())).resolves.toEqual({ ok: false, reason: "unavailable" });
    expect(getContext).not.toHaveBeenCalled();
    expect(electron.openPath).not.toHaveBeenCalled();
    authorizeSender.mockReturnValue(false);
    await expect(handler(event, request())).resolves.toEqual({ ok: false, reason: "unavailable" });
    expect(getContext).not.toHaveBeenCalled();
    electron.fromWebContents.mockReturnValue(null);
    await expect(handler(event, request())).resolves.toEqual({ ok: false, reason: "unavailable" });
  });

  it.each(["main", "issue"])("requires the %s window's current authenticated session", async (kind) => {
    const mainWindow = {};
    const issueWindow = {};
    const sessions = new AuthSessionCoordinator<object>(() => {});
    sessions.registerMainWindow(mainWindow);
    sessions.registerIssueWindow(issueWindow);
    sessions.reportMain(mainWindow, "synthetic-account");
    sessions.reportIssue(issueWindow, "synthetic-account");
    const getContext = vi.fn().mockResolvedValue(context);
    setupLocalFileReading(getContext, (window) =>
      sessions.hasActiveMainSession(window) || sessions.isCurrentIssueSession(window),
    );
    const handler = electron.handle.mock.calls.find(([channel]) => channel === "file:read-local")?.[1];
    const mainFrame = {};
    const event = { sender: { mainFrame }, senderFrame: mainFrame } as IpcMainInvokeEvent;
    electron.fromWebContents.mockReturnValue(kind === "main" ? mainWindow : issueWindow);
    await expect(handler(event, request())).resolves.toMatchObject({ ok: true, text: expect.any(String) });
    expect(electron.openPath).not.toHaveBeenCalled();

    sessions.reportMain(mainWindow, null);
    getContext.mockClear();
    electron.openPath.mockClear();
    await expect(handler(event, request())).resolves.toEqual({ ok: false, reason: "unavailable" });
    expect(getContext).not.toHaveBeenCalled();
    expect(electron.openPath).not.toHaveBeenCalled();
  });
});
