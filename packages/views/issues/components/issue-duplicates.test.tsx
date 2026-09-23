// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses";
import type { Issue, IssueDuplicates } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const listIssueDuplicates = vi.fn<(id: string) => Promise<IssueDuplicates>>();

vi.mock("@multica/core/api", () => ({
  api: { listIssueDuplicates: (id: string) => listIssueDuplicates(id) },
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
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, className }: { href: string; children: ReactNode; className?: string }) => (
    <a href={href} className={className}>
      {children}
    </a>
  ),
}));

import { IssueDuplicateBanner, IssueDuplicatesSection } from "./issue-duplicates";

function issue(overrides: Partial<Issue>): Issue {
  return {
    id: "issue",
    identifier: "MUL-1",
    title: "An issue",
    status: "todo",
    ...overrides,
  } as Issue;
}

const ORIGINAL = issue({ id: "original", identifier: "MUL-6980", title: "Tap targets are too small", status: "in_progress" });
const DUPLICATE = issue({ id: "duplicate", identifier: "MUL-7412", title: "Status picker is hard to tap", status: "cancelled" });

function renderWithQuery(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

afterEach(() => {
  cleanup();
  listIssueDuplicates.mockReset();
});

describe("IssueDuplicateBanner", () => {
  it("points a cancelled duplicate at its original and offers to unmark it", async () => {
    listIssueDuplicates.mockResolvedValue({ duplicate_of: ORIGINAL, duplicates: [] });
    const onUnmark = vi.fn();
    renderWithQuery(<IssueDuplicateBanner issue={DUPLICATE} onUnmark={onUnmark} />);

    const link = await screen.findByRole("link", { name: "MUL-6980 Tap targets are too small" });
    expect(link.getAttribute("href")).toBe("/acme/issues/original");

    fireEvent.click(screen.getByRole("button", { name: "Unmark and move to Todo" }));
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
    expect(screen.queryByRole("button", { name: "Unmark and move to Todo" })).toBeNull();
  });
});

describe("IssueDuplicatesSection", () => {
  it("lists the issues marked as duplicates of this one", async () => {
    listIssueDuplicates.mockResolvedValue({ duplicate_of: null, duplicates: [DUPLICATE] });
    renderWithQuery(<IssueDuplicatesSection issueId="original" />);

    const row = await screen.findByRole("link", { name: /MUL-7412/ });
    expect(row.getAttribute("href")).toBe("/acme/issues/duplicate");
    expect(screen.getByRole("button", { name: "Duplicates" })).toBeTruthy();
  });

  it("renders nothing when there are no duplicates", async () => {
    listIssueDuplicates.mockResolvedValue({ duplicate_of: null, duplicates: [] });
    const { container } = renderWithQuery(<IssueDuplicatesSection issueId="original" />);

    await waitFor(() => expect(listIssueDuplicates).toHaveBeenCalledWith("original"));
    expect(container.textContent).toBe("");
  });
});
