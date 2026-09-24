import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { NeedsMeItem } from "@multica/core/home";
import { renderWithI18n } from "../../test/i18n";
import { NeedsMeSection } from "./needs-me-section";

const NAMES: Record<string, string> = { niko: "Niko", linus: "Linus", jiayuan: "Jiayuan" };

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) => selector({ user: { id: "me" } }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => NAMES[id] ?? id }),
}));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => ({ entryOf: () => undefined }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ homeQueue: (key: string) => `/acme/home/queue?item=${encodeURIComponent(key)}` }),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...props }: { href: string; children?: React.ReactNode }) => (
    <a href={href} {...props}>{children}</a>
  ),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));
vi.mock("./use-source-comment", () => ({
  useSourceComment: () => ({
    comment: { content: "Collapsed into three levels, default `mentions_assignments`." },
    isLoading: false,
  }),
}));

function item(overrides: Partial<NeedsMeItem>): NeedsMeItem {
  return {
    key: "issue:i1",
    kind: "in_review",
    issueId: "i1",
    issue: null,
    inbox: null,
    inboxIds: [],
    title: "Per-issue notification levels",
    identifier: "MUL-1",
    priority: "high",
    dueDate: null,
    since: new Date(Date.now() - 2 * 60 * 60 * 1000).toISOString(),
    actor: { type: "agent", id: "niko" },
    ...overrides,
  };
}

describe("NeedsMeSection", () => {
  it("features the first entry with its reason, the verbatim comment and one action", () => {
    renderWithI18n(
      <NeedsMeSection
        isLoading={false}
        items={[
          item({}),
          item({ key: "issue:i2", kind: "blocked", title: "Done-Gate event type", actor: { type: "agent", id: "linus" } }),
          item({ key: "issue:i3", kind: "mentioned", title: "Landing copy", actor: { type: "member", id: "jiayuan" } }),
        ]}
      />,
    );

    expect(screen.getByText("Suggested first")).toBeInTheDocument();
    expect(screen.getByText("High · Waiting 2h")).toBeInTheDocument();
    const featured = screen.getByRole("link", { name: "Review Niko's delivery: Per-issue notification levels" });
    expect(featured).toHaveAttribute("href", "/acme/home/queue?item=issue%3Ai1");
    expect(screen.getByText("Collapsed into three levels, default mentions_assignments.")).toBeInTheDocument();

    // The rest are one line each, verb first, with the waiting actor named.
    expect(screen.getByRole("link", { name: "Reply to Linus: Done-Gate event type" })).toHaveAttribute(
      "href",
      "/acme/home/queue?item=issue%3Ai2",
    );
    expect(screen.getByRole("link", { name: "Reply to Jiayuan: Landing copy" })).toBeInTheDocument();
    expect(screen.getByText("Mentioned you")).toBeInTheDocument();
  });

  it("does not name the viewer as the one waiting", () => {
    renderWithI18n(
      <NeedsMeSection isLoading={false} items={[item({ actor: { type: "member", id: "me" } })]} />,
    );
    expect(screen.getByRole("link", { name: "Review: Per-issue notification levels" })).toBeInTheDocument();
  });

  it("never suggests a request already replied to, and marks it as waiting on the agent", () => {
    renderWithI18n(
      <NeedsMeSection
        isLoading={false}
        replied={new Set(["issue:i1"])}
        items={[
          item({ kind: "blocked", title: "Done-Gate event type", actor: { type: "agent", id: "linus" } }),
          item({ key: "issue:i2", title: "Per-issue notification levels" }),
        ]}
      />,
    );
    expect(screen.getByRole("link", { name: "Review Niko's delivery: Per-issue notification levels" })).toBeInTheDocument();
    expect(screen.getByText("Replied, waiting for Linus")).toBeInTheDocument();
  });

  it("collapses to a single line when nothing is waiting", () => {
    renderWithI18n(<NeedsMeSection isLoading={false} items={[]} />);
    expect(screen.getByText("Nothing is waiting on you right now")).toBeInTheDocument();
    expect(screen.queryByText("Suggested first")).not.toBeInTheDocument();
  });
});
