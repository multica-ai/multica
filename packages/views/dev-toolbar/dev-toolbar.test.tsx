import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setCurrentWorkspace } from "@multica/core/platform";
import { DevToolbar } from "./dev-toolbar";

const mocks = vi.hoisted(() => ({
  wsStatus: "connected" as string,
  listMembers: vi.fn(),
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
  api: { listMembers: mocks.listMembers },
}));

function renderToolbar() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <DevToolbar build="web" apiUrl="http://localhost:18080" route="/acme/issues" />
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
  });

  afterEach(() => {
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
});
