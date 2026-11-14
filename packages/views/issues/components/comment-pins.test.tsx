import { afterEach, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { CommentPins } from "./comment-pins";

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => `user-${id}` }),
}));

const visible = new Set<string>();
vi.mock("../hooks/use-visible-pin-bodies", () => ({ useVisiblePinBodies: () => visible }));

const entry = (id: string, pinned_at: string | null, content = id): TimelineEntry =>
  ({ type: "comment", id, actor_type: "member", actor_id: "u", created_at: "2026-01-01", pinned_at, content } as TimelineEntry);

afterEach(() => visible.clear());

it("stacks every pin in pin order and dims rows whose original is on screen instead of removing them", () => {
  const onJump = vi.fn();
  const entries = [entry("late", "2026-03-01", "Release checklist"), entry("none", null), entry("early", "2026-01-01", "Decision")];
  visible.add("late");
  renderWithI18n(<CommentPins entries={entries} container={document.body} onJump={onJump} />);
  const rows = screen.getAllByRole("listitem");
  expect(rows.map(row => row.textContent)).toEqual(["user-uDecision", "user-uRelease checklist"]);
  const late = document.querySelector('[data-pin-row="late"]')!;
  expect(late).toHaveAttribute("data-on-screen", "true");
  expect(document.querySelector('[data-pin-row="early"]')).not.toHaveAttribute("data-on-screen");
  fireEvent.click(late);
  expect(onJump).toHaveBeenCalledWith("late");
});

it("renders nothing without pins", () => {
  renderWithI18n(<CommentPins entries={[entry("a", null)]} container={document.body} onJump={() => {}} />);
  expect(document.querySelector("[data-comment-pins-overlay]")).toBeNull();
});
