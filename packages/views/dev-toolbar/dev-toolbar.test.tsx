import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiClient } from "@multica/core/api/client";
import { resetDevRecorder } from "@multica/core/diagnostics";
import { setCurrentWorkspace } from "@multica/core/platform";
import { DevToolbar, type DevToolbarProps } from "./dev-toolbar";

const mocks = vi.hoisted(() => ({
  wsStatus: "connected" as string,
  listMembers: vi.fn(),
  createIssue: vi.fn(),
  uploadFile: vi.fn(),
}));

vi.mock("@multica/core/realtime", () => ({
  useWSStatus: () => mocks.wsStatus,
}));

vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "user-1" } };
  return {
    useAuthStore: Object.assign(
      (selector: (s: typeof state) => unknown) => selector(state),
      { getState: () => state },
    ),
  };
});

vi.mock("@multica/core/api", () => ({
  api: {
    listMembers: mocks.listMembers,
    createIssue: mocks.createIssue,
    uploadFile: mocks.uploadFile,
  },
}));

function renderToolbar(props: Partial<DevToolbarProps> = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <DevToolbar
        build="web"
        apiUrl="http://localhost:18080"
        route="/acme/issues"
        openPath={() => {}}
        {...props}
      />
    </QueryClientProvider>,
  );
}

describe("DevToolbar", () => {
  beforeEach(() => {
    mocks.wsStatus = "connected";
    mocks.listMembers.mockReset().mockResolvedValue([
      { user_id: "user-1", role: "admin" },
      { user_id: "user-2", role: "owner" },
    ]);
    mocks.createIssue.mockReset().mockResolvedValue({ id: "issue-9", identifier: "ACME-9" });
    mocks.uploadFile.mockReset().mockResolvedValue({ id: "att-1", markdown_url: "https://files.test/shot.png" });
  });

  afterEach(() => {
    resetDevRecorder();
    vi.unstubAllGlobals();
    setCurrentWorkspace(null, null);
  });

  it("shows the client, backend, workspace role and connection state", async () => {
    setCurrentWorkspace("acme", "ws-1");
    mocks.wsStatus = "reconnecting";
    renderToolbar();

    const bar = screen.getByTestId("dev-toolbar");
    expect(bar).toHaveTextContent("web");
    expect(bar).toHaveTextContent("localhost:18080");
    expect(bar).toHaveTextContent("acme");
    expect(bar).toHaveTextContent("/acme/issues");
    expect(bar).toHaveTextContent("Syncreconnecting");
    expect(await screen.findByText("admin")).toBeInTheDocument();
    expect(mocks.listMembers).toHaveBeenCalledWith("ws-1");
  });

  it("does not look up a role outside a workspace", () => {
    renderToolbar();
    expect(screen.getByTestId("dev-toolbar")).toBeInTheDocument();
    expect(mocks.listMembers).not.toHaveBeenCalled();
  });

  it("reserves its height from viewport-sized shells only while expanded", () => {
    const reservesSpace = () =>
      [...document.querySelectorAll("style")].some((s) =>
        s.textContent?.includes(".h-svh{height:calc(100svh - 24px)}"),
      );
    renderToolbar();
    expect(reservesSpace()).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Hide dev toolbar" }));
    expect(screen.queryByTestId("dev-toolbar")).not.toBeInTheDocument();
    expect(reservesSpace()).toBe(false);

    fireEvent.click(screen.getByTestId("dev-toolbar-restore"));
    expect(screen.getByTestId("dev-toolbar")).toBeInTheDocument();
    expect(reservesSpace()).toBe(true);
  });

  it("counts a failed API request and shows it with its request ID", async () => {
    renderToolbar();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: "version conflict" }), {
          status: 409,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    await expect(new ApiClient("http://localhost:18080").listMembers("ws-1")).rejects.toThrow();

    await waitFor(() => expect(screen.getByTestId("dev-toolbar-net")).toHaveTextContent("· 1 failed"));
    fireEvent.click(screen.getByTestId("dev-toolbar-net"));
    fireEvent.click(await screen.findByText("/api/workspaces/ws-1/members"));
    expect(screen.getByText("version conflict")).toBeInTheDocument();
    expect(screen.getByText(/^X-Request-ID /)).toBeInTheDocument();
  });

  it("counts console errors and lists them with the route they happened on", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    renderToolbar();
    console.error(new Error("Cannot read properties of undefined"));

    await waitFor(() => expect(screen.getByTestId("dev-toolbar-errors")).toHaveTextContent("Errors1"));
    fireEvent.click(screen.getByTestId("dev-toolbar-errors"));
    expect(await screen.findByText("Error: Cannot read properties of undefined")).toBeInTheDocument();
  });

  it("files a bug report with a screenshot into the current workspace", async () => {
    setCurrentWorkspace("acme", "ws-1");
    const openPath = vi.fn();
    const capture = vi.fn().mockResolvedValue(new Blob(["png"], { type: "image/png" }));
    vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: () => "blob:shot", revokeObjectURL: () => {} }));
    renderToolbar({ openPath, screenshot: { capture, automatic: true } });

    fireEvent.click(screen.getByTestId("dev-toolbar-report"));
    const title = await screen.findByTestId("dev-toolbar-report-title");
    expect(capture).toHaveBeenCalledOnce();
    fireEvent.change(title, { target: { value: "Board drops a card" } });
    fireEvent.click(screen.getByTestId("dev-toolbar-report-create"));

    await screen.findByText("Created ACME-9");
    expect(mocks.uploadFile).toHaveBeenCalledOnce();
    const [request, slug] = mocks.createIssue.mock.calls[0]!;
    expect(slug).toBe("acme");
    expect(request).toMatchObject({ title: "Board drops a card", attachment_ids: ["att-1"] });
    expect(request.description).toContain("![Screenshot](https://files.test/shot.png)");
    expect(request.description).toContain("- Route: `/acme/issues`");

    fireEvent.click(screen.getByRole("button", { name: "Open" }));
    expect(openPath).toHaveBeenCalledWith("/acme/issues/issue-9");
  });
});
