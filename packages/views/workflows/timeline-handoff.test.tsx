// @vitest-environment jsdom

import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";
import { TimelineHandoff } from "./timeline-handoff";

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Reviewer" }),
}));
vi.mock("../common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));

const base = { handoff_to_type: "agent", handoff_to_id: "reviewer", handoff_run: "false" };
afterEach(cleanup);

describe("TimelineHandoff", () => {
  it("shows the reason from the status activity, with no execution claim", () => {
    renderWithI18n(<TimelineHandoff details={{ ...base, handoff_run_reason: "pending_check_failed" }} />);
    expect(screen.getByText("run eligibility could not be checked")).toBeTruthy();
    expect(screen.queryByText(/started a run/)).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("keeps historical true as a prediction and still opens its brief", () => {
    renderWithI18n(<TimelineHandoff details={{ ...base, handoff_run: "true", handoff_note: "Review the patch." }} />);
    expect(screen.getByText("enqueue was allowed at handoff; check runs for execution")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "View handoff brief" }));
    expect(screen.getByText("Review the patch.")).toBeTruthy();
    expect(screen.getByRole("button", { expanded: true })).toBeTruthy();
    expect(screen.getByText("Workflow handoff brief")).toBeTruthy();
  });

  it("shows a localized reason and safely falls back for future codes", () => {
    const { unmount } = renderWithI18n(<TimelineHandoff details={{ ...base, handoff_run_reason: "no_runtime" }} />, { locale: "zh-Hans" });
    expect(screen.getByText("交接时未绑定运行时")).toBeTruthy();
    unmount();
    renderWithI18n(<TimelineHandoff details={{ ...base, handoff_run_reason: "private future details" }} />);
    expect(screen.getByText("run prediction unavailable; check runs")).toBeTruthy();
    expect(screen.queryByText("private future details")).toBeNull();
  });

  it("keeps member handoffs and ordinary status changes intact", () => {
    const { unmount } = renderWithI18n(<TimelineHandoff details={{ ...base, handoff_to_type: "member" }} />);
    expect(screen.getByText("(member, no run)")).toBeTruthy();
    unmount();
    const { container } = renderWithI18n(<TimelineHandoff details={{ from: "todo", to: "in_review" }} />);
    expect(container.innerHTML).toBe("");
  });
});
