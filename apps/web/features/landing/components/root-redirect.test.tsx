import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Workspace } from "@multica/core/types";

const { mockReplace, authState, queryState } = vi.hoisted(() => ({
  mockReplace: vi.fn(),
  authState: {
    user: null as null | {
      onboarded_at: string | null;
      preferences?: Record<string, unknown>;
    },
    isLoading: false,
  },
  queryState: {
    data: [] as Workspace[],
    isFetched: true,
  },
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: mockReplace }),
}));

vi.mock("@tanstack/react-query", () => ({
  queryOptions: (options: unknown) => options,
  useQuery: () => queryState,
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: typeof authState) => unknown) =>
    selector(authState),
}));

function workspace(slug: string): Workspace {
  return {
    id: "ws-1",
    name: "Workspace",
    slug,
    avatar_url: null,
    description: null,
    context: null,
    settings: {},
    repos: [],
    issue_prefix: "FIR",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

import { RootRedirect } from "./root-redirect";

describe("RootRedirect", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    authState.user = {
      onboarded_at: "2026-01-01T00:00:00Z",
      preferences: {},
    };
    authState.isLoading = false;
    queryState.data = [workspace("jeh-b0edd870")];
    queryState.isFetched = true;
    window.innerWidth = 1024;
  });

  it("redirects anonymous visitors to /login (FIR-2490: internal-tool root)", async () => {
    authState.user = null;

    render(<RootRedirect />);

    await waitFor(() => {
      expect(mockReplace).toHaveBeenCalledWith("/login");
    });
  });

  it("uses the desktop start page preference for authenticated root fallback", async () => {
    authState.user = {
      onboarded_at: "2026-01-01T00:00:00Z",
      preferences: { start_page_desktop: "projects" },
    };

    render(<RootRedirect />);

    await waitFor(() => {
      expect(mockReplace).toHaveBeenCalledWith("/jeh-b0edd870/projects");
    });
  });

  it("uses the mobile start page preference for authenticated root fallback", async () => {
    window.innerWidth = 390;
    authState.user = {
      onboarded_at: "2026-01-01T00:00:00Z",
      preferences: { start_page_mobile: "my-issues" },
    };

    render(<RootRedirect />);

    await waitFor(() => {
      expect(mockReplace).toHaveBeenCalledWith("/jeh-b0edd870/my-issues");
    });
  });

  it("falls back to issues when no start page preference is set", async () => {
    render(<RootRedirect />);

    await waitFor(() => {
      expect(mockReplace).toHaveBeenCalledWith("/jeh-b0edd870/issues");
    });
  });
});
