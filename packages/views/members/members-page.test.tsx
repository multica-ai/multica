import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MemberWithUser } from "@multica/core/types";
import { memberListOptions } from "@multica/core/workspace/queries";
import { renderWithI18n } from "../test/i18n";
import { MembersPage } from "./members-page";

const WS = "ws-test";
const push = vi.hoisted(() => vi.fn());
const currentUser = vi.hoisted(() => ({ id: "user-me" }));
const listMembers = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/api", () => ({ api: { listMembers } }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (s: { user: { id: string } }) => unknown) =>
    select({ user: currentUser }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    settings: () => "/acme/settings",
    memberDetail: (id: string) => `/acme/members/${id}`,
  }),
}));
vi.mock("../navigation", () => ({
  useNavigation: () => ({ push, pathname: "/acme/members" }),
}));
vi.mock("../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../layout/collection-list", () => ({
  CollectionNameCell: ({
    title,
    badges,
    description,
  }: {
    title: ReactNode;
    badges?: ReactNode;
    description?: ReactNode;
  }) => (
    <span>
      {title}
      {badges}
      {description}
    </span>
  ),
  CollectionList: ({
    rows,
    columns,
    getRowHref,
    getRowId,
    empty,
  }: {
    rows: MemberWithUser[];
    columns: { key: string; cell: (row: MemberWithUser) => ReactNode }[];
    getRowHref: (row: MemberWithUser) => string;
    getRowId: (row: MemberWithUser) => string;
    empty: ReactNode;
  }) =>
    rows.length === 0 ? (
      <>{empty}</>
    ) : (
      <ul>
        {rows.map((row) => (
          <li key={getRowId(row)} data-testid="member-row">
            <a href={getRowHref(row)}>
              {columns.map((col) => (
                <span key={col.key}>{col.cell(row)}</span>
              ))}
            </a>
          </li>
        ))}
      </ul>
    ),
}));

function member(
  userId: string,
  name: string,
  role: MemberWithUser["role"],
): MemberWithUser {
  return {
    id: `m-${userId}`,
    workspace_id: WS,
    user_id: userId,
    role,
    created_at: "2026-03-14T00:00:00Z",
    name,
    email: `${name.split(" ")[0]!.toLowerCase()}@example.com`,
    avatar_url: null,
  };
}

const MEMBERS = [
  member("user-3", "Grace Hopper", "member"),
  member("user-me", "Alan Turing", "member"),
  member("user-1", "Ada Lovelace", "owner"),
  member("user-2", "Charles Babbage", "admin"),
];

function renderPage(members: MemberWithUser[] | null = MEMBERS) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  if (members) qc.setQueryData(memberListOptions(WS).queryKey, members);
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <MembersPage />
    </QueryClientProvider>,
  );
  return qc;
}

describe("MembersPage", () => {
  beforeEach(() => {
    push.mockReset();
    listMembers.mockReset();
    currentUser.id = "user-me";
  });

  it("lists members by role, then name, each linking to their page", () => {
    renderPage();

    const rows = screen.getAllByTestId("member-row");
    expect(rows.map((row) => row.textContent)).toEqual([
      expect.stringContaining("Ada Lovelace"),
      expect.stringContaining("Charles Babbage"),
      expect.stringContaining("Alan Turing"),
      expect.stringContaining("Grace Hopper"),
    ]);
    expect(within(rows[0]!).getByRole("link")).toHaveAttribute("href", "/acme/members/user-1");
    expect(rows[0]).toHaveTextContent("Owner");
    expect(rows[1]).toHaveTextContent("Admin");
    expect(rows[2]).toHaveTextContent("You");
    expect(rows[3]).not.toHaveTextContent("You");
  });

  it("shows guests with their own role label", () => {
    renderPage([...MEMBERS, member("user-4", "Visiting Guest", "guest")]);

    const rows = screen.getAllByTestId("member-row");
    expect(rows.at(-1)).toHaveTextContent("Visiting Guest");
    expect(rows.at(-1)).toHaveTextContent("Guest");
  });

  it("filters by name or email and offers to clear a search with no matches", () => {
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Search members" }));
    const input = screen.getByRole("textbox", { name: "Search members" });
    fireEvent.change(input, { target: { value: "grace@" } });
    const rows = screen.getAllByTestId("member-row");
    expect(rows).toHaveLength(1);
    expect(rows[0]).toHaveTextContent("Grace Hopper");

    fireEvent.change(input, { target: { value: "nobody" } });
    expect(screen.queryAllByTestId("member-row")).toHaveLength(0);
    expect(screen.getByText("No matching members")).toBeInTheDocument();
    // The search field has its own clear button; the empty state's comes after it.
    const clearButtons = screen.getAllByRole("button", { name: "Clear search" });
    expect(clearButtons).toHaveLength(2);
    fireEvent.click(clearButtons[1]!);
    expect(screen.getAllByTestId("member-row")).toHaveLength(4);
  });

  it("gives admins the way to Settings to invite and manage members", () => {
    currentUser.id = "user-2";
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Manage members in Settings" }));
    expect(push).toHaveBeenLastCalledWith("/acme/settings?tab=members");
    fireEvent.click(screen.getByRole("button", { name: "Invite member" }));
    expect(push).toHaveBeenLastCalledWith("/acme/settings?tab=members&invite=1");
  });

  it("hides the management buttons from regular members", () => {
    renderPage();

    expect(
      screen.queryByRole("button", { name: "Manage members in Settings" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Invite member" })).not.toBeInTheDocument();
  });

  it("shows a load failure with retry instead of an empty search result", async () => {
    listMembers.mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce(MEMBERS);
    renderPage(null);

    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't load members");
    expect(screen.queryByText("No matching members")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(screen.getAllByTestId("member-row")).toHaveLength(4));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(listMembers).toHaveBeenCalledTimes(2);
  });

  it("keeps the list when a background refresh fails", async () => {
    listMembers.mockRejectedValue(new Error("offline"));
    const qc = renderPage();

    await qc.refetchQueries({ queryKey: memberListOptions(WS).queryKey });
    await waitFor(() =>
      expect(qc.getQueryState(memberListOptions(WS).queryKey)?.status).toBe("error"),
    );
    expect(screen.getAllByTestId("member-row")).toHaveLength(4);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
