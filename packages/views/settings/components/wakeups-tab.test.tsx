import { beforeEach, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { renderWithI18n } from "../../test/i18n";
import { WakeupsTab } from "./wakeups-tab";

vi.mock("@multica/core/api", () => ({
  api: { listWorkspaceSystemWakeups: vi.fn(), updateWorkspaceSystemWakeup: vi.fn() },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: unknown) => unknown) => selector({ user: { id: "u" } }),
}));
let role = "admin";
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members", "ws"], queryFn: async () => [{ user_id: "u", role }] }),
}));

const list = vi.mocked(api.listWorkspaceSystemWakeups);
const update = vi.mocked(api.updateWorkspaceSystemWakeup);

function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <WakeupsTab />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  role = "admin";
  list.mockReset().mockResolvedValue([
    { rule: "child_done", enabled: true, instruction: "", builtin_instruction: "Advance the next stage.", customized: 2 },
  ]);
  update.mockReset().mockResolvedValue(undefined);
});

it("turns the sub-issue rule's default off and saves a workspace instruction", async () => {
  mount();
  const toggle = await screen.findByRole("switch", { name: "On by default" });
  expect(screen.getByText("2 open issues have their own setting and ignore this default.")).toBeVisible();
  fireEvent.click(toggle);
  await waitFor(() => expect(update).toHaveBeenCalledWith("child_done", { enabled: false }));

  const input = screen.getByRole("textbox", { name: "Default instruction" });
  expect(screen.getByText("Advance the next stage.")).toBeVisible();
  expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  fireEvent.change(input, { target: { value: "  Wrap up in the parent.  " } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(update).toHaveBeenCalledWith("child_done", { instruction: "Wrap up in the parent." }));
});

it("shows the defaults read-only to members who cannot change them", async () => {
  role = "member";
  mount();
  expect(await screen.findByText("Only owners and admins can change these defaults.")).toBeVisible();
  expect(screen.getByRole("switch", { name: "On by default" })).toHaveAttribute("aria-disabled", "true");
  expect(screen.getByRole("textbox", { name: "Default instruction" })).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
});
