// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { IssueSubscriber } from "@multica/core/types";
import { SubscribersSection } from "./subscribers-section";

afterEach(cleanup);

const subscribers: IssueSubscriber[] = [
  {
    issue_id: "issue-1",
    user_type: "member",
    user_id: "member-1",
    reason: "assignee",
    created_at: "2026-07-16T12:00:00Z",
  },
  {
    issue_id: "issue-1",
    user_type: "agent",
    user_id: "agent-1",
    reason: "mentioned",
    created_at: "2026-07-16T12:01:00Z",
  },
];

describe("SubscribersSection", () => {
  it("shows every subscriber with a badge and unsubscribes from the subscriber row", () => {
    const onUnsubscribe = vi.fn();
    render(
      <SubscribersSection
        subscribers={subscribers}
        getActorName={(_, id) => (id === "member-1" ? "Jesper" : "Lone")}
        renderAvatar={(subscriber) => <span>{subscriber.user_type}</span>}
        ownerType="member"
        ownerId="member-1"
        onUnsubscribe={onUnsubscribe}
      />,
    );

    expect(screen.getByText("Owner")).toBeInTheDocument();
    expect(screen.getByText("Mentioned")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Unsubscribe Lone" }));
    expect(onUnsubscribe).toHaveBeenCalledWith(subscribers[1]);
  });

  it("collapses and expands the subscriber list", () => {
    render(
      <SubscribersSection
        subscribers={subscribers}
        getActorName={(_, id) => id}
        renderAvatar={() => null}
        onUnsubscribe={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Collapse Subscribers" }));
    expect(screen.queryByRole("button", { name: "Unsubscribe member-1" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Expand Subscribers" }));
    expect(screen.getByRole("button", { name: "Unsubscribe member-1" })).toBeInTheDocument();
  });
});
