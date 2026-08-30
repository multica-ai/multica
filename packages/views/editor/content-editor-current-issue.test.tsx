import { describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider } from "../navigation/context";
import type { NavigationAdapter } from "../navigation/types";
import { CurrentIssueRenderContextProvider } from "../issues/current-issue-render-context";

vi.mock("../issues/components/issue-chip", () => ({
  IssueChip: ({
    issueId,
    children,
  }: {
    issueId: string;
    children?: ReactNode;
  }) => (
    <span
      data-testid="issue-chip"
      data-issue-id={issueId}
      data-current={children !== undefined ? "true" : "false"}
    >
      {children}
    </span>
  ),
}));

vi.mock("../issues/components/issue-hover-card", () => ({
  IssueHoverCard: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock("./link-hover-card", () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/acme/issues/${id}`,
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
  useWorkspaceSlug: () => "acme",
}));

import { ContentEditor } from "./content-editor";

const CURRENT_ID = "11111111-1111-4111-8111-111111111111";
const OTHER_ID = "22222222-2222-4222-8222-222222222222";
const CONTENT = [
  `[MUL-7](mention://issue/${CURRENT_ID})`,
  `[MUL-8](mention://issue/${OTHER_ID})`,
].join(" and ");

function adapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => `https://app.example${path}`,
  };
}

function renderEditor() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return renderWithI18n(
    <NavigationProvider value={adapter()}>
      <QueryClientProvider client={queryClient}>
        <CurrentIssueRenderContextProvider
          value={{ id: CURRENT_ID, identifier: "MUL-7" }}
        >
          <ContentEditor
            defaultValue={CONTENT}
            showBubbleMenu={false}
          />
        </CurrentIssueRenderContextProvider>
      </QueryClientProvider>
    </NavigationProvider>,
  );
}

describe("ContentEditor current-issue context", () => {
  it("passes the provider through the Tiptap NodeView portal to only the current mention", async () => {
    renderEditor();

    await waitFor(() => {
      expect(screen.getAllByTestId("issue-chip")).toHaveLength(2);
    });

    const chips = screen.getAllByTestId("issue-chip");
    expect(chips.map((chip) => chip.getAttribute("data-current"))).toEqual([
      "true",
      "false",
    ]);
    expect(chips[0]).toHaveTextContent("This issue · MUL-7");
  });
});
