import { beforeEach, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { renderWithI18n } from "../../test/i18n";
import { SystemWakeupDefaults } from "./system-wakeup-defaults";

vi.mock("@multica/core/api", () => ({
  api: { listWorkspaceSystemWakeups: vi.fn(), updateWorkspaceSystemWakeup: vi.fn() },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
let role = "admin";
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role, isLoading: false }),
}));

const list = vi.mocked(api.listWorkspaceSystemWakeups);
const update = vi.mocked(api.updateWorkspaceSystemWakeup);

function mount(onShowCustomized = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={client}>
      <SystemWakeupDefaults onShowCustomized={onShowCustomized} />
    </QueryClientProvider>,
  );
  return onShowCustomized;
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
  fireEvent.click(toggle);
  await waitFor(() => expect(update).toHaveBeenCalledWith("child_done", { enabled: false }));

  fireEvent.click(screen.getByRole("button", { name: "Edit default instruction" }));
  const input = await screen.findByRole("textbox", { name: "Default instruction" });
  expect(screen.getByText("Advance the next stage.")).toBeVisible();
  expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  fireEvent.change(input, { target: { value: "  Wrap up in the parent.  " } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(update).toHaveBeenCalledWith("child_done", { instruction: "Wrap up in the parent." }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
});

it("keeps the dialog and the draft open when saving fails", async () => {
  update.mockRejectedValue(new Error("nope"));
  mount();
  fireEvent.click(await screen.findByRole("button", { name: "Edit default instruction" }));
  const input = await screen.findByRole("textbox", { name: "Default instruction" });
  fireEvent.change(input, { target: { value: "Draft" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not save the default.");
  expect(screen.getByRole("textbox", { name: "Default instruction" })).toHaveValue("Draft");
});

it("lists the issues that use their own setting", async () => {
  const onShowCustomized = mount();
  fireEvent.click(
    await screen.findByRole("button", { name: /2 open issues have their own setting/ }),
  );
  expect(onShowCustomized).toHaveBeenCalledOnce();
});

it("shows the default read-only to members who cannot change it", async () => {
  role = "member";
  mount();
  expect(await screen.findByRole("switch", { name: "On by default" })).toHaveAttribute("aria-disabled", "true");
  fireEvent.click(screen.getByRole("button", { name: "View default instruction" }));
  expect(await screen.findByText("Only owners and admins can change these defaults.")).toBeVisible();
  expect(screen.getByRole("textbox", { name: "Default instruction" })).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
});
