import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  WorkspaceSlugProvider,
  useWorkspaceSlug,
  useCurrentWorkspace,
} from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";

// Hook tests for @multica/core/paths live here because packages/core/ runs
// Vitest in node environment (no jsdom). packages/views/ already has jsdom +
// @testing-library/react configured, so it's the correct home per CLAUDE.md
// testing rules ("shared UI components live in packages/views/*.test.tsx").

function makeWorkspace(over: Partial<Workspace>): Workspace {
  return {
    id: "id-default",
    name: "Default",
    slug: "default",
    description: null,
    context: null,
    settings: {},
    repos: [],
    issue_prefix: "DEF",
    created_at: "",
    updated_at: "",
    ...over,
  };
}

function setup(slug: string | null, wsList: Workspace[] = []) {
  const qc = new QueryClient();
  qc.setQueryData(workspaceKeys.list(), wsList);
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <WorkspaceSlugProvider slug={slug}>{children}</WorkspaceSlugProvider>
      </QueryClientProvider>
    );
  };
}

describe("useWorkspaceSlug", () => {
  it("returns the provided slug", () => {
    function Probe() {
      return <div data-testid="slug">{useWorkspaceSlug() ?? "null"}</div>;
    }
    render(<Probe />, { wrapper: setup("acme") });
    expect(screen.getByTestId("slug").textContent).toBe("acme");
  });

  it("returns null when no slug is provided", () => {
    function Probe() {
      return <div data-testid="slug">{useWorkspaceSlug() ?? "null"}</div>;
    }
    render(<Probe />, { wrapper: setup(null) });
    expect(screen.getByTestId("slug").textContent).toBe("null");
  });
});

describe("useCurrentWorkspace", () => {
  const acme = makeWorkspace({ id: "id-1", slug: "acme", name: "Acme" });

  it("resolves workspace from slug and list", () => {
    function Probe() {
      const ws = useCurrentWorkspace();
      return <div data-testid="name">{ws?.name ?? "none"}</div>;
    }
    render(<Probe />, { wrapper: setup("acme", [acme]) });
    expect(screen.getByTestId("name").textContent).toBe("Acme");
  });

  it("returns null when slug does not match any workspace", () => {
    function Probe() {
      const ws = useCurrentWorkspace();
      return <div data-testid="name">{ws?.name ?? "none"}</div>;
    }
    render(<Probe />, { wrapper: setup("bogus", [acme]) });
    expect(screen.getByTestId("name").textContent).toBe("none");
  });

  it("returns null when no slug is provided", () => {
    function Probe() {
      const ws = useCurrentWorkspace();
      return <div data-testid="name">{ws?.name ?? "none"}</div>;
    }
    render(<Probe />, { wrapper: setup(null, [acme]) });
    expect(screen.getByTestId("name").textContent).toBe("none");
  });
});
