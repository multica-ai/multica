import { beforeEach, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { IssueWakeup } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { WakeupInstructionEditor } from "./wakeup-instruction-editor";

vi.mock("@multica/core/api", () => ({ api: { listIssueWakeups: vi.fn(), editIssueWakeupInstruction: vi.fn() } }));
const list = vi.mocked(api.listIssueWakeups);
const edit = vi.mocked(api.editIssueWakeupInstruction);
let rule: IssueWakeup;
beforeEach(() => {
  rule = { id: "wake", issue_id: "issue", agent_id: "agent", agent_name: "Emacs", instruction: "Check CI", revision: 2, kind: "event", mode: "continuous", event_types: ["comment.created"], filter_agent_id: null, filter_task_id: null, interval_seconds: null, cron_expression: null, timezone: "UTC", next_fire_at: null, enabled: false, disabled_at: null, last_task_id: null, last_error: null };
  list.mockReset().mockImplementation(async () => [rule]);
  edit.mockReset().mockResolvedValue(undefined);
});
function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  renderWithI18n(<QueryClientProvider client={client}><WakeupInstructionEditor workspaceId="ws" issueId="issue" wakeupId="wake" /></QueryClientProvider>);
  return client;
}
async function open() {
  fireEvent.click(screen.getByRole("button", { name: "Edit prompt" }));
  return screen.findByRole("textbox", { name: "What to do when woken" });
}
it("loads instructions only on demand and saves only prompt fields with its editing snapshot", async () => {
  mount();
  expect(list).not.toHaveBeenCalled();
  const input = await open();
  expect(input).toHaveValue("Check CI");
  fireEvent.change(input, { target: { value: "Check deployment\nReport failure" } });
  fireEvent.keyDown(input, { key: "Enter", ctrlKey: true });
  await waitFor(() => expect(edit).toHaveBeenCalledWith("issue", "wake", { instruction: "Check deployment\nReport failure", expected_instruction: "Check CI", revision: 2 }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
});
it("cancels a draft without saving", async () => {
  mount();
  fireEvent.change(await open(), { target: { value: "discard this" } });
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(edit).not.toHaveBeenCalled();
  expect(await open()).toHaveValue("Check CI");
});
it("keeps the draft after a conflict and never silently uses the refreshed revision", async () => {
  const client = mount();
  const input = await open();
  fireEvent.change(input, { target: { value: "my draft" } });
  rule = { ...rule, instruction: "someone else's edit", revision: 3 };
  await client.invalidateQueries({ queryKey: ["issue-wakeups", "ws", "issue"] });
  edit.mockRejectedValue({ status: 409 });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("This rule has changed");
  expect(input).toHaveValue("my draft");
  expect(edit).toHaveBeenCalledWith("issue", "wake", expect.objectContaining({ expected_instruction: "Check CI", revision: 2 }));
});
it("validates empty and oversized multi-byte instructions, and retains a forbidden draft", async () => {
  mount();
  const input = await open();
  for (const value of ["  ", "中".repeat(4001)]) {
    fireEvent.change(input, { target: { value } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("12,000");
  }
  expect(edit).not.toHaveBeenCalled();
  edit.mockRejectedValue({ status: 403 });
  fireEvent.change(input, { target: { value: "keep this draft" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("You cannot manage this wakeup");
  expect(input).toHaveValue("keep this draft");
});
it("prevents duplicate saves and dismissal while saving", async () => {
  let resolve!: () => void;
  edit.mockImplementation(() => new Promise<void>((done) => { resolve = done; }));
  mount();
  const input = await open();
  fireEvent.change(input, { target: { value: "new prompt" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Saving..." })).toBeDisabled());
  fireEvent.keyDown(input, { key: "Enter", ctrlKey: true });
  expect(edit).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
  resolve();
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
});
