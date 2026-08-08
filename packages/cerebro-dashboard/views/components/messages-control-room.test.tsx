// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DashboardOverview } from "../../core/api";
import { MessagesControlRoom } from "./messages-control-room";

const queryResult = vi.hoisted(() => ({
  data: {
    messages: [{
      id: "message-1",
      content: "Please verify the Dashboard",
      created_at: "2026-07-15T07:00:00Z",
      sender_id: "member-1",
      sender_name: "Jesper",
      agent_id: "agent-1",
      agent_name: "Lone",
      session_id: "session-1",
    }],
  },
  isLoading: false,
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => queryResult,
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "member-1" } }),
}));
vi.mock("./analytics-dashboard", () => ({ AnalyticsDashboard: () => null }));
vi.mock("./message-detail-sheet", () => ({
  MessageDetailSheet: ({ message }: { message: { id: string } | null }) =>
    message ? <div>Opened conversation {message.id}</div> : null,
}));

afterEach(cleanup);

const overview = {
  chat_messages: { value: 14, prior: 10 },
  channel_messages: { value: 8, prior: 6 },
  spend_cents: { value: 900, prior: 700 },
  timeline: [{ day: "2026-07-15", issues_created: 0, issues_done: 0, tasks_completed: 0, tasks_failed: 0, messages_sent: 6 }],
  top_message_senders: [{ id: "member-1", name: "Jesper", count: 12 }],
  top_message_recipients: [{ id: "agent-1", name: "Lone", count: 9 }],
  message_flow: [{ sender_id: "member-1", sender_name: "Jesper", recipient_id: "agent-1", recipient_name: "Lone", count: 8 }],
} as unknown as DashboardOverview;

describe("MessagesControlRoom", () => {
  it("renders the complete Messages control room", () => {
    render(
      <MessagesControlRoom
        workspaceId="workspace-1"
        workspaceSlug="firtal"
        data={overview}
        isLoading={false}
        filters={[]}
        onFiltersChange={vi.fn()}
        onNewVisual={vi.fn()}
        onSelectActor={vi.fn()}
      />,
    );

    expect(screen.getByRole("region", { name: "Message operations" })).toBeTruthy();
    expect(screen.getByText("Conversation flow")).toBeTruthy();
    expect(screen.getByText("All messages")).toBeTruthy();
    expect(screen.getByText("Please verify the Dashboard")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Open conversation with Lone" }));
    expect(screen.getByText("Opened conversation message-1")).toBeTruthy();
  });

  it("paginates the messages table", () => {
    const original = queryResult.data.messages;
    queryResult.data.messages = Array.from({ length: 12 }, (_, index) => ({
      ...original[0]!,
      id: `message-${index + 1}`,
      content: `Message ${index + 1}`,
    }));

    render(
      <MessagesControlRoom
        workspaceId="workspace-1"
        workspaceSlug="firtal"
        data={overview}
        isLoading={false}
        filters={[]}
        onFiltersChange={vi.fn()}
        onNewVisual={vi.fn()}
        onSelectActor={vi.fn()}
      />,
    );

    expect(screen.getByText("Message 10")).toBeTruthy();
    expect(screen.queryByText("Message 11")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Next messages page" }));
    expect(screen.getByText("Message 11")).toBeTruthy();
    queryResult.data.messages = original;
  });

  it("filters every Messages panel when a sender is selected", () => {
    const onSelectActor = vi.fn();
    render(
      <MessagesControlRoom
        workspaceId="workspace-1"
        workspaceSlug="firtal"
        data={overview}
        isLoading={false}
        filters={[]}
        onFiltersChange={vi.fn()}
        onNewVisual={vi.fn()}
        onSelectActor={onSelectActor}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Filter Dashboard by sender Jesper" }));
    expect(onSelectActor).toHaveBeenCalledWith("member-1", "Jesper", "member");
  });

  it("filters the messages table from an issue click", () => {
    const original = queryResult.data.messages;
    queryResult.data.messages = [
      { ...original[0]!, id: "message-1", content: "About the dashboard", issue_number: 2996, issue_title: "Dashboard i Multica" },
      { ...original[0]!, id: "message-2", content: "Unrelated message" },
    ] as typeof original;

    render(
      <MessagesControlRoom
        workspaceId="workspace-1"
        workspaceSlug="firtal"
        data={overview}
        isLoading={false}
        filters={[]}
        onFiltersChange={vi.fn()}
        onNewVisual={vi.fn()}
        onSelectActor={vi.fn()}
      />,
    );

    expect(screen.getByText("Unrelated message")).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "Filter messages by issue Dashboard i Multica" }),
    );
    expect(screen.queryByText("Unrelated message")).toBeNull();
    expect(screen.getByText("About the dashboard")).toBeTruthy();
    const search = screen.getByRole("textbox", { name: "Search all messages" }) as HTMLInputElement;
    expect(search.value).toBe("Dashboard i Multica");
    queryResult.data.messages = original;
  });

  it("treats nullable message aggregates as empty lists", () => {
    const sparseOverview = {
      ...overview,
      timeline: null,
      top_message_senders: null,
      top_message_recipients: null,
      message_flow: null,
    } as unknown as DashboardOverview;

    render(
      <MessagesControlRoom
        workspaceId="workspace-1"
        workspaceSlug="firtal"
        data={sparseOverview}
        isLoading={false}
        filters={[]}
        onFiltersChange={vi.fn()}
        onNewVisual={vi.fn()}
        onSelectActor={vi.fn()}
      />,
    );

    expect(screen.getByRole("region", { name: "Message operations" })).toBeTruthy();
    expect(screen.getByText("No message activity in the selected range.")).toBeTruthy();
    expect(screen.getByText("No message flows in the selected range.")).toBeTruthy();
  });

});
