import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { WakeupsSection } from "./wakeups-section";
const mutate = vi.fn();
let enabled = true;
let status = "queued";
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ id: "ws" }) }));
vi.mock("@multica/core/issues", () => ({
  issueWakeupsOptions: () => ({ queryKey: ["wakeups"] }), issueTasksOptions: () => ({ queryKey: ["tasks"] }),
  useDisableIssueWakeup: () => ({ mutate, isPending: false }),
}));
vi.mock("@tanstack/react-query", () => ({ useQuery: ({ queryKey }: { queryKey: string[] }) => ({
  data: queryKey[0] === "wakeups" ? [{ id: "wake", agent_name: "Emacs", instruction: "Check CI", kind: "every", mode: "continuous", event_types: [], interval_seconds: 3600, enabled, disabled_at: null, last_task_id: "task" }] : [{ id: "task", status }],
}) }));
vi.mock("../../common/task-transcript", () => ({ TranscriptButton: () => <button>Transcript</button> }));
beforeEach(() => { mutate.mockReset(); enabled = true; status = "queued"; });
describe("Wakeups sidebar", () => {
  it("turns off a wakeup without stopping the ordinary run", () => {
    renderWithI18n(<WakeupsSection issueId="issue" />);
    fireEvent.click(screen.getByRole("switch"));
    expect(mutate).toHaveBeenCalledWith("wake", expect.any(Object));
    expect(screen.getByText("Check CI")).toBeTruthy();
  });
  it("allows withdrawing a consumed one-shot that is still queued", () => {
    enabled = false;
    renderWithI18n(<WakeupsSection issueId="issue" />);
    expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe("true");
    fireEvent.click(screen.getByRole("switch"));
    expect(mutate).toHaveBeenCalled();
  });
  it("does not offer an enable switch for a consumed run", () => {
    enabled = false; status = "completed";
    renderWithI18n(<WakeupsSection issueId="issue" />);
    expect(screen.getByRole("switch").getAttribute("aria-disabled")).toBe("true");
  });
});
