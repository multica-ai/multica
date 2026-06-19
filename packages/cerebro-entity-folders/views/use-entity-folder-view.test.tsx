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
});
