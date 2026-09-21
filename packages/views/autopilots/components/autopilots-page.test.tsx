import { useState } from "react";
import { expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { NavigationProvider } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";
import { AutopilotsPage } from "./autopilots-page";

vi.mock("@multica/core/api", () => ({ api: { listAutopilots: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ autopilots: () => "/ws/autopilots" }),
}));
vi.mock("./workspace-wakeups", () => ({
  WorkspaceWakeups: () => <div>Wakeup inventory</div>,
}));
vi.mock("./autopilot-dialog", () => ({ AutopilotDialog: () => null }));

function Harness() {
  const [path, setPath] = useState("/ws/autopilots?keep=yes");
  const url = new URL(path, "https://example.test");
  return (
    <NavigationProvider
      value={{
        pathname: url.pathname,
        searchParams: url.searchParams,
        hash: "",
        push: setPath,
        replace: setPath,
        back: () => {},
        getShareableUrl: (v) => v,
      }}
    >
      <AutopilotsPage />
    </NavigationProvider>
  );
}
it.each(["empty", "error"])(
  "keeps wakeups reachable when autopilots are %s",
  async (state) => {
    if (state === "empty")
      vi.mocked(api.listAutopilots).mockResolvedValue({ autopilots: [], total: 0 });
    else
      vi.mocked(api.listAutopilots).mockRejectedValue(
        new Error("Autopilots unavailable"),
      );
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    renderWithI18n(
      <QueryClientProvider client={client}>
        <Harness />
      </QueryClientProvider>,
    );
    await screen.findByText(
      state === "empty" ? "No autopilots yet" : "Autopilots unavailable",
    );
    fireEvent.click(screen.getByRole("tab", { name: "Issue wakeups" }));
    expect(await screen.findByText("Wakeup inventory")).toBeVisible();
    expect(screen.queryByRole("button", { name: "New autopilot" })).toBeNull();
    fireEvent.click(
      screen.getByRole("tab", { name: "Autopilot" }),
    );
    expect(
      await screen.findByRole("button", { name: "New autopilot" }),
    ).toBeVisible();
  },
);
