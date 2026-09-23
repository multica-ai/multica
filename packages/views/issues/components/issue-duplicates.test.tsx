// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses";
import type { Issue, IssueDuplicates } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const listIssueDuplicates = vi.fn<(id: string) => Promise<IssueDuplicates>>();
const navigate = vi.fn();

vi.mock("@multica/core/api", () => ({
  api: {
    listIssueDuplicates: (id: string) => listIssueDuplicates(id),
  },
}));
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog(undefined),
}));
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span data-testid="avatar">{actorId}</span>,
}));
vi.mock("../../navigation", () => ({
  AppLink: ({
    href,
    children,
    className,
    title,
    onClick,
    onAuxClick,
  }: {
    href: string;
    children: ReactNode;
    className?: string;
    title?: string;
    onClick?: (e: React.MouseEvent<HTMLAnchorElement>) => void;
    onAuxClick?: (e: React.MouseEvent<HTMLAnchorElement>) => void;
  }) => (
    <a href={href} className={className} title={title} onClick={onClick} onAuxClick={onAuxClick}>
      {children}
    </a>
  ),
  resolveClickIntent: () => "push",
  rowLinkInteractiveProps: {
    onClick: (e: React.MouseEvent) => e.stopPropagation(),
    onAuxClick: (e: React.MouseEvent) => e.stopPropagation(),
  },
  useIntentNavigate: () => (...args: unknown[]) => navigate(...args),
}));

import {
  IssueDuplicateBanner,
  IssueDuplicateOfMarker,
  IssueDuplicatesSection,
  isDuplicateIssue,
} from "./issue-duplicates";

function issue(overrides: Partial<Issue>): Issue {
  return {
    id: "issue",
    identifier: "MUL-1",
    title: "An issue",
    status: "todo",
    creator_type: "member",
    creator_id: "user-1",
    ...overrides,
  } as Issue;
}

const ORIGINAL = issue({ id: "original", identifier: "MUL-6980", title: "Tap targets are too small", status: "in_progress" });
const DUPLICATE = issue({
  id: "duplicate",
  identifier: "MUL-7412",
  title: "Status picker is hard to tap",
  status: "cancelled",
  creator_id: "user-2",
  duplicate_of: { id: "original", identifier: "MUL-6980", title: "Tap targets are too small", status: "in_progress" },
});

function renderWithQuery(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

afterEach(() => {
  cleanup();
  listIssueDuplicates.mockReset();
  navigate.mockReset();
});

describe("isDuplicateIssue", () => {
  it("only counts a pointer while the issue is cancelled", () => {
    expect(isDuplicateIssue(DUPLICATE)).toBe(true);
    expect(isDuplicateIssue({ ...DUPLICATE, status: "todo" })).toBe(false);
    expect(isDuplicateIssue({ ...DUPLICATE, duplicate_of: null })).toBe(false);
    expect(isDuplicateIssue({ status: "cancelled" } as Issue)).toBe(false);
  });
});

describe("IssueDuplicateBanner", () => {
  it("links the whole strip to the original and keeps unmark secondary", async () => {
    listIssueDuplicates.mockResolvedValue({ duplicate_of: ORIGINAL, duplicates: [] });
    const onUnmark = vi.fn();
    renderWithQuery(<IssueDuplicateBanner issue={DUPLICATE} onUnmark={onUnmark} />);

    // The original's identifier and title sit inside the link, after the prefix.
    const link = await screen.findByRole("link", { name: /Duplicate of MUL-6980 Tap targets are too small/ });
    expect(link.getAttribute("href")).toBe("/acme/issues/original");
    // The unmark control is not nested inside the link.
    const unmark = screen.getByRole("button", { name: "Not a duplicate" });
    expect(link.contains(unmark)).toBe(false);

    fireEvent.click(unmark);
    expect(onUnmark).toHaveBeenCalledTimes(1);
  });

  // The server clears the mark whenever the status leaves cancelled; a relation
  // fetched before that must not keep the banner up after an optimistic reopen.
  it("hides once the issue is no longer cancelled", async () => {
    listIssueDuplicates.mockResolvedValue({ duplicate_of: ORIGINAL, duplicates: [] });
    renderWithQuery(
      <IssueDuplicateBanner issue={{ ...DUPLICATE, status: "todo" }} onUnmark={() => {}} />,
    );

    await waitFor(() => expect(listIssueDuplicates).toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: "Not a duplicate" })).toBeNull();
  });
});

describe("IssueDuplicatesSection", () => {
  it("lists the duplicates with their reporters and a count", async () => {
    const second = issue({ id: "second", identifier: "MUL-7413", title: "Cannot tap priority", status: "cancelled", creator_id: "user-3" });
    listIssueDuplicates.mockResolvedValue({ duplicate_of: null, duplicates: [DUPLICATE, second] });
    renderWithQuery(<IssueDuplicatesSection issueId="original" />);

    const row = await screen.findByRole("link", { name: /MUL-7412/ });
    expect(row.getAttribute("href")).toBe("/acme/issues/duplicate");
    expect(screen.getByRole("button", { name: "Duplicates 2" })).toBeTruthy();
    // Every duplicate is cancelled, so the row shows who reported it instead
    // of a status icon that would be identical on each line.
    expect(screen.getAllByTestId("avatar").map((el) => el.textContent)).toEqual(["user-2", "user-3"]);
  });

  it("renders nothing when there are no duplicates", async () => {
    listIssueDuplicates.mockResolvedValue({ duplicate_of: null, duplicates: [] });
    const { container } = renderWithQuery(<IssueDuplicatesSection issueId="original" />);

    await waitFor(() => expect(listIssueDuplicates).toHaveBeenCalledWith("original"));
    expect(container.textContent).toBe("");
  });
});

describe("IssueDuplicateOfMarker", () => {
  it("is a real link to the original where the row is not one", () => {
    const rowClick = vi.fn();
    renderWithQuery(
      <div onClick={rowClick}>
        {DUPLICATE.title} <IssueDuplicateOfMarker issue={DUPLICATE} />
      </div>,
    );

    const marker = screen.getByRole("link", { name: "MUL-6980" });
    expect(marker.tagName).toBe("A");
    expect(marker.getAttribute("href")).toBe("/acme/issues/original");
    expect(marker.getAttribute("title")).toBe("Duplicate of MUL-6980 Tap targets are too small");

    fireEvent.click(marker);
    expect(rowClick).not.toHaveBeenCalled();
  });

  it("navigates without an anchor when nested inside the row's link", () => {
    const rowClick = vi.fn();
    renderWithQuery(
      <a href="/acme/issues/duplicate" onClick={rowClick}>
        {DUPLICATE.title} <IssueDuplicateOfMarker issue={DUPLICATE} insideLink />
      </a>,
    );

    const marker = screen.getByRole("link", { name: "MUL-6980" });
    expect(marker.tagName).toBe("SPAN");
    fireEvent.click(marker);
    expect(navigate).toHaveBeenCalledWith("/acme/issues/original", "push", "MUL-6980");
    expect(rowClick).not.toHaveBeenCalled();
  });

  it("renders nothing for an issue that is not a duplicate", () => {
    const { container } = renderWithQuery(<IssueDuplicateOfMarker issue={ORIGINAL} />);
    expect(container.textContent).toBe("");
  });
});
