// @vitest-environment jsdom

// FIR-2580: WorkspaceAvatar renders the uploaded logo as an <img> when
// avatarUrl is set, and falls back to the workspace's first letter otherwise.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";

// resolvePublicFileUrl normally calls api.getBaseUrl(); stub it to identity so
// the component test stays free of the API client wiring.
vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (url: string | null | undefined) => (url ? url : null),
}));

// WorkspaceAvatar's module also exports WorkspaceBreadcrumbLogo, which imports
// these — stub them so importing the module needs no provider context. They
// are not exercised by the WorkspaceAvatar cases below.
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => false,
}));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => null,
}));

import { WorkspaceAvatar } from "./workspace-avatar";

afterEach(() => cleanup());

describe("WorkspaceAvatar", () => {
  it("renders the logo image when avatarUrl is set", () => {
    render(<WorkspaceAvatar name="Acme" avatarUrl="https://cdn.example/logo.png" />);
    const img = screen.getByRole("img");
    expect(img).toHaveAttribute("src", "https://cdn.example/logo.png");
    expect(img).toHaveAttribute("alt", "Acme");
  });

  it("falls back to the first letter when no avatarUrl is set", () => {
    render(<WorkspaceAvatar name="acme" />);
    expect(screen.queryByRole("img")).toBeNull();
    expect(screen.getByText("A")).toBeInTheDocument();
  });

  it("falls back to the first letter when avatarUrl is empty", () => {
    render(<WorkspaceAvatar name="Beta" avatarUrl="" />);
    expect(screen.queryByRole("img")).toBeNull();
    expect(screen.getByText("B")).toBeInTheDocument();
  });
});
