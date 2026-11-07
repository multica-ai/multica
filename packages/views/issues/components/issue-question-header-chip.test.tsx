// @vitest-environment jsdom

import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueQuestionHeaderChip, findPendingAgentQuestion } from "./issue-question-header-chip";

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) => (id === "agent-1" ? "Walt" : "Unknown"),
    getActorInitials: () => "WA",
    getActorAvatarUrl: () => null,
  }),
}));

const question: TimelineEntry = {
  type: "comment",
  id: "q-1",
  actor_type: "agent",
  actor_id: "agent-1",
  created_at: "2026-09-07T10:00:00Z",
  content: "Which flag name?",
  question_payload: { questions: [{ question: "Which flag name?", options: [{ label: "--dry-run" }] }] },
};

const olderQuestion: TimelineEntry = { ...question, id: "q-0", created_at: "2026-09-07T09:00:00Z" };

const agentReply: TimelineEntry = {
  type: "comment",
  id: "r-agent",
  actor_type: "agent",
  actor_id: "agent-1",
  created_at: "2026-09-07T10:01:00Z",
  parent_id: "q-1",
  content: "Waiting for your answer.",
};

const memberReply: TimelineEntry = { ...agentReply, id: "r-member", actor_type: "member", actor_id: "user-1" };

const nestedMemberReply: TimelineEntry = { ...memberReply, id: "r-nested", parent_id: "r-agent" };

afterEach(() => cleanup());

describe("findPendingAgentQuestion", () => {
  it("ignores answered questions, including answers nested under an agent reply", () => {
    expect(findPendingAgentQuestion([question, agentReply])?.id).toBe("q-1");
    expect(findPendingAgentQuestion([question, memberReply])).toBeNull();
    expect(findPendingAgentQuestion([question, agentReply, nestedMemberReply])).toBeNull();
  });

  it("returns the newest pending question", () => {
    expect(findPendingAgentQuestion([olderQuestion, question])?.id).toBe("q-1");
    expect(findPendingAgentQuestion([olderQuestion, question, memberReply])?.id).toBe("q-0");
  });

  it("skips ordinary comments and activities", () => {
    const activity: TimelineEntry = { type: "activity", id: "a-1", actor_type: "member", actor_id: "u", created_at: "2026-09-07T10:00:00Z", action: "status_changed" };
    const plain: TimelineEntry = { ...question, id: "c-1", question_payload: undefined };
    expect(findPendingAgentQuestion([activity, plain])).toBeNull();
  });
});

describe("IssueQuestionHeaderChip", () => {
  it("renders nothing without a pending question", () => {
    renderWithI18n(<IssueQuestionHeaderChip timeline={[question, memberReply]} />);
    expect(screen.queryByTestId("issue-question-header-chip")).toBeNull();
  });

  it("names the waiting agent and scrolls to the question card on click", () => {
    const target = document.createElement("div");
    target.id = "comment-q-1";
    const scrollIntoView = vi.fn();
    target.scrollIntoView = scrollIntoView;
    document.body.appendChild(target);

    renderWithI18n(<IssueQuestionHeaderChip timeline={[question]} />);
    const chip = screen.getByTestId("issue-question-header-chip");
    expect(chip.textContent).toContain("Walt is waiting for your answer");

    fireEvent.click(chip);
    expect(scrollIntoView).toHaveBeenCalled();
    target.remove();
  });
});
