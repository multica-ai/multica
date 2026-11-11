import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import type { Workspace } from "@multica/core/types";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { renderWithI18n } from "../../test/i18n";
import { WorkspaceOrderSection } from "./workspace-order-section";

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ searchParams: new URLSearchParams(), pathname: "/acme/settings", replace: vi.fn() }),
}));

const workspace = (id: string, workspace_position: number | null, pinned_at: string | null = null) => ({
  id, name: id, slug: id, created_at: "2026-01-01T00:00:00Z", avatar_url: null, workspace_position, pinned_at,
}) as Workspace;

afterEach(() => vi.restoreAllMocks());

it("renders the shared order, labels drag handles, and pins a workspace", async () => {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const rows = [workspace("Alpha", 2), workspace("Beta", 1), workspace("Pinned", 3, "2026-03-01T00:00:00Z")];
  qc.setQueryData(workspaceKeys.list(), rows);
  vi.spyOn(api, "listWorkspaces").mockResolvedValue(rows);
  const pin = vi.spyOn(api, "setWorkspacePinned").mockResolvedValue({ pinned_at: "2026-04-01T00:00:00Z" });
  renderWithI18n(<QueryClientProvider client={qc}><WorkspaceOrderSection /></QueryClientProvider>);
  const list = screen.getByRole("list", { name: "Other workspaces" });
  expect(within(list).getAllByRole("listitem").map((row) => row.textContent)).toEqual(["Beta", "Alpha"]);
  expect(screen.getByRole("button", { name: "Reorder Beta" })).toBeVisible();
  await userEvent.click(within(list).getAllByRole("button", { name: "Pin workspace" })[0]!);
  expect(pin).toHaveBeenCalledWith("Beta", true);
  qc.clear();
});
