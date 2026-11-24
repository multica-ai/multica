import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueSidebarFooter } from "./issue-sidebar-footer";

const names: Record<string, string> = { "agent-1": "Xeon", "user-1": "Jiayuan" };
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => names[id] ?? "Unknown" }),
}));
vi.mock("../../common/use-viewing-timezone", () => ({ useViewingTimezone: () => "Asia/Shanghai" }));
vi.mock("../../agents/components/agent-profile-card", () => ({ AgentProfileCard: () => <div>Agent card</div> }));
vi.mock("../../members/member-profile-card", () => ({ MemberProfileCard: () => <div>Member card</div> }));
vi.mock("../../squads/components/squad-profile-card", () => ({ SquadProfileCard: () => <div>Squad card</div> }));

const issue = (patch: Partial<Issue> = {}) =>
  ({
    creator_type: "agent",
    creator_id: "agent-1",
    requester_id: "user-1",
    created_at: "2026-11-23T20:30:00Z",
    updated_at: "2026-11-24T02:00:00Z",
    ...patch,
  }) as Issue;

describe("IssueSidebarFooter", () => {
  it("says who created the issue for whom, in the reader's own word order", () => {
    renderWithI18n(<IssueSidebarFooter issue={issue()} />, { locale: "zh-Hans" });
    expect(screen.getByTestId("issue-sidebar-footer")).toHaveTextContent("Xeon 代表 Jiayuan 创建于 11月24日");
  });

  it("leaves the requester out when a member created the issue", () => {
    renderWithI18n(<IssueSidebarFooter issue={issue({ creator_type: "member", creator_id: "user-1" })} />);
    expect(screen.getByTestId("issue-sidebar-footer")).toHaveTextContent(/^Created by Jiayuan · Nov 24$/);
  });

  it("dates the issue in the viewer's timezone and keeps the exact times in a tooltip", async () => {
    renderWithI18n(<IssueSidebarFooter issue={issue()} />);
    const date = screen.getByText("Nov 24");
    fireEvent.focus(date);
    expect(await screen.findByText(/^Created Nov 24, 2026, 4:30/)).toBeInTheDocument();
    expect(screen.getByText(/^Updated Nov 24, 2026, 10:00/)).toBeInTheDocument();
  });
});
