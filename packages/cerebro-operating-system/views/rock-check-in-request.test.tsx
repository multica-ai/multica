import "@testing-library/jest-dom/vitest";

import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Rock } from "../core/types";
import { RockCheckInRequest } from "./rock-check-in-request";

const state = { userId: "me", createChannel: vi.fn(), createComment: vi.fn() };

vi.mock("@multica/core/api", () => ({ api: { createComment: (...args: unknown[]) => state.createComment(...args) } }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: (selector: (s: { user: { id: string } }) => unknown) => selector({ user: { id: state.userId } }) }));
vi.mock("@multica/core/channels", () => ({ useCreateChannel: () => ({ mutateAsync: (...args: unknown[]) => state.createChannel(...args) }) }));
vi.mock("@multica/core/paths", () => ({ useWorkspaceSlug: () => "firtal" }));

const terminology = { strategy: "Strategy", rock: "Rock", rocks: "Rocks", vision_plan: "Vision Plan", meetings: "Meetings", org_chart: "Org Chart", scorecard: "Scorecard", issues_list: "Issues List", strategy_map: "Strategy Map" };

const rock = (over: Partial<Rock> = {}): Rock => ({
  id: "rock-1", workspace_id: "workspace-1", title: "Cut fulfilment cost 12%",
  owner_type: "member", owner_id: "owner-1", owner_name: "Mette",
  period_id: "period-1", period_name: "Q3 2026", period_start: "2026-07-01", period_end: "2026-09-30",
  confidence: 72, reported_health: "at_risk", derived_health: { state: "on_track", reason: "", calculated_at: "" },
  health_score: 80, issue_count: 4, done_issue_count: 1, blocked_issue_count: 0, project_count: 0,
  projects: [], issues: [], check_ins: [], created_at: "", updated_at: "", ...over,
});

beforeEach(() => {
  state.userId = "me";
  state.createChannel = vi.fn().mockResolvedValue({ id: "channel-1" });
  state.createComment = vi.fn().mockResolvedValue({ id: "comment-1" });
});

describe("RockCheckInRequest", () => {
  it("sends the check-in to the owner as a message in a direct conversation", async () => {
    render(<RockCheckInRequest rock={rock()} terminology={terminology} />);
    await userEvent.click(screen.getByRole("button", { name: "Ask Mette for a check-in" }));

    await waitFor(() => expect(state.createComment).toHaveBeenCalled());
    expect(state.createChannel).toHaveBeenCalledWith({ kind: "dm", name: "", member_ids: ["owner-1"], agent_ids: [] });
    const [channelId, content] = state.createComment.mock.calls[0] ?? [];
    expect(channelId).toBe("channel-1");
    expect(content).toContain("Check-in: Cut fulfilment cost 12%");
    expect(content).toContain("[Open Rocks](/firtal/rocks)");
    expect(screen.getByText(/They answer by replying to the message/)).toBeInTheDocument();
  });

  it("keeps the owner informed when sending fails and lets them try again", async () => {
    state.createChannel = vi.fn().mockRejectedValue(new Error("network down"));
    render(<RockCheckInRequest rock={rock()} terminology={terminology} />);
    await userEvent.click(screen.getByRole("button", { name: "Ask Mette for a check-in" }));

    await waitFor(() => expect(screen.getByText("network down")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Ask Mette for a check-in" })).toBeEnabled();
  });

  it("does not offer to message yourself", () => {
    render(<RockCheckInRequest rock={rock({ owner_id: "me" })} terminology={terminology} />);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.getByText(/You own this Rock/)).toBeInTheDocument();
  });

  it("explains why an agent owner is not messaged", () => {
    render(<RockCheckInRequest rock={rock({ owner_type: "agent", owner_name: "Sabine" })} terminology={terminology} />);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.getByText(/Sabine is an agent/)).toBeInTheDocument();
  });

  it("asks for an owner when the rock has none", () => {
    render(<RockCheckInRequest rock={rock({ owner_type: undefined, owner_id: undefined, owner_name: undefined })} terminology={terminology} />);
    expect(screen.getByText(/Give this Rock an owner/)).toBeInTheDocument();
  });
});
