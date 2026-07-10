import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { entityFolderKeys } from "../queries";
import {
  useEntityFolderView,
  type EntityFolderViewItem,
} from "./use-entity-folder-view";

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => true,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// Stable singletons so the hook's ref-captured navigation/paths don't change
// identity across renders (mirrors the real memoized adapter + a slug-bound
// path builder for the duration of a route).
const mockNavigation = {
  pathname: "/ws-1/skills",
  searchParams: new URLSearchParams(),
  replace: vi.fn(),
  replaceSilent: vi.fn(),
  push: vi.fn(),
  back: vi.fn(),
  getShareableUrl: (p: string) => p,
};
const mockWsPaths = {
  skills: () => "/ws-1/skills",
  autopilots: () => "/ws-1/autopilots",
  skillsFolder: (id: string) => `/ws-1/skills?folder=${id}`,
  autopilotsFolder: (id: string) => `/ws-1/autopilots?folder=${id}`,
};
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => mockWsPaths,
}));

vi.mock("../api", () => ({
  fetchEntityFolders: vi.fn(async () => []),
  fetchEntityFolderItems: vi.fn(async () => []),
}));

function wrapperWithClient(client: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe("useEntityFolderView", () => {
  it("keeps the returned view stable across unchanged rerenders", () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    });
    queryClient.setQueryData(entityFolderKeys.folders("ws-1", "skill"), []);
    queryClient.setQueryData(entityFolderKeys.items("ws-1", "skill"), []);

    const items: EntityFolderViewItem[] = [{ id: "skill-1", label: "Alpha" }];
    const { result, rerender } = renderHook(
      ({ hookItems }) =>
        useEntityFolderView({
          kind: "skill",
          items: hookItems,
          itemNoun: "skills",
          navigation: mockNavigation,
        }),
      {
        wrapper: wrapperWithClient(queryClient),
        initialProps: { hookItems: items },
      },
    );

    const first = result.current;
    rerender({ hookItems: items });

    expect(result.current).toBe(first);
    expect(result.current.includes).toBe(first.includes);
    expect(result.current.getDragProps).toBe(first.getDragProps);
    expect(result.current.sidebar).toBe(first.sidebar);
  });

  it("seeds the folder selection from the ?folder= query param (deep link)", () => {
    // FIR-2688: opening /skills?folder=folder-1 should scope the list to that
    // folder without any click.
    mockNavigation.searchParams = new URLSearchParams("folder=folder-1");
    try {
      const queryClient = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      queryClient.setQueryData(entityFolderKeys.folders("ws-1", "skill"), [
        {
          id: "folder-1",
          workspace_id: "ws-1",
          parent_id: null,
          kind: "skill",
          name: "Folder 1",
          created_at: "",
          updated_at: "",
        },
      ]);
      queryClient.setQueryData(entityFolderKeys.items("ws-1", "skill"), [
        { kind: "skill", item_id: "skill-1", folder_id: "folder-1" },
        { kind: "skill", item_id: "skill-2", folder_id: "folder-2" },
      ]);

      const items: EntityFolderViewItem[] = [
        { id: "skill-1", label: "Alpha" },
        { id: "skill-2", label: "Beta" },
      ];
      const { result } = renderHook(
        () =>
          useEntityFolderView({
            kind: "skill",
            items,
            itemNoun: "skills",
            navigation: mockNavigation,
          }),
        { wrapper: wrapperWithClient(queryClient) },
      );

      expect(result.current.includes("skill-1")).toBe(true);
      expect(result.current.includes("skill-2")).toBe(false);
    } finally {
      mockNavigation.searchParams = new URLSearchParams();
    }
  });
});
