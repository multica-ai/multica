// @vitest-environment jsdom

// FIR-2742 — the inbox skill change-request detail: renders in place (no
// navigation) and pops the skill page to a new window on demand.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ skillDetail: (id: string) => `/acme/skills/${id}` }),
}));

import { SkillChangeInboxDetail } from "./skill-change-inbox-detail";
import type { InboxItem } from "@multica/core/types";

const item = (over: Partial<InboxItem>): InboxItem =>
  ({
    id: "inbox-1",
    workspace_id: "ws",
    recipient_type: "member",
    recipient_id: "me",
    actor_type: "agent",
    actor_id: "a1",
    type: "skill_change_request_created",
    severity: "action_required",
    route: "inbox",
    issue_id: null,
    project_id: null,
    title: "Proposed change",
    body: "An agent proposed an edit",
    issue_status: null,
    read: false,
    archived: false,
    muted_until: null,
    created_at: "2026-02-01T00:00:00Z",
    details: {
      skill_id: "s1",
      change_request_id: "cr9",
      skill_name: "Alpha Skill",
      title: "Tighten alpha",
      base_version: "1.0.0",
      proposed_version: "1.1.0",
      base_content: "old body",
      proposed_content: "new body",
    },
    ...over,
  }) as InboxItem;

afterEach(() => cleanup());

let openSpy: ReturnType<typeof vi.fn>;
beforeEach(() => {
  openSpy = vi.fn();
  window.open = openSpy as unknown as typeof window.open;
});

describe("SkillChangeInboxDetail", () => {
  it("lists the change (skill name, version bump, diff)", () => {
    render(<SkillChangeInboxDetail item={item({})} />);
    expect(screen.getByText("Tighten alpha")).toBeTruthy();
    expect(screen.getByText("Alpha Skill")).toBeTruthy();
    expect(screen.getByText(/1\.0\.0 → 1\.1\.0/)).toBeTruthy();
  });

  it("opens the focused skill page in a new window", () => {
    render(<SkillChangeInboxDetail item={item({})} />);
    fireEvent.click(screen.getByText("Open in new window"));
    expect(openSpy).toHaveBeenCalledWith(
      "/acme/skills/s1?cr=cr9",
      "_blank",
      "noopener,noreferrer",
    );
  });

  it("omits the new-window action when the skill id is missing", () => {
    render(<SkillChangeInboxDetail item={item({ details: { title: "x" } })} />);
    expect(screen.queryByText("Open in new window")).toBeNull();
  });

  it("renders reviewed notifications as an inbox message", () => {
    render(
      <SkillChangeInboxDetail
        item={item({ type: "skill_change_request_reviewed" })}
      />,
    );
    expect(screen.getByText(/reviewed/)).toBeTruthy();
    expect(screen.getByText("Open in new window")).toBeTruthy();
  });
});
