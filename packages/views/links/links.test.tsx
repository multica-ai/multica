import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { WorkspaceLink } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { EmbeddedLinkPage, LINK_SANDBOX } from "./embedded-link-page";
import { LinksBar } from "./components/links-bar";
import { LinkEditor } from "./components/link-editor";
import { LinksSection } from "./components/links-section";

const state = vi.hoisted(() => ({
  role: "member",
  desktop: false,
  list: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
  external: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listWorkspaceLinks: (...args: unknown[]) => state.list(...args),
    createWorkspaceLink: (...args: unknown[]) => state.create(...args),
    updateWorkspaceLink: (...args: unknown[]) => state.update(...args),
    deleteWorkspaceLink: (...args: unknown[]) => state.remove(...args),
  },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/paths", async (original) => {
  const actual = await original<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws", slug: "acme" }),
    useWorkspacePaths: () => actual.paths.workspace("acme"),
  };
});
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: state.role }),
}));
vi.mock("../platform", () => ({
  openExternal: (...args: unknown[]) => state.external(...args),
  isDesktopShell: () => state.desktop,
}));

const link: WorkspaceLink = {
  id: "link",
  workspaceId: "ws",
  projectId: null,
  name: "Docs",
  icon: "book-open",
  urlTemplate: "https://example.com/{workspace_slug}",
  openMode: "embed",
  allowSameOrigin: false,
  position: 0,
};

function render(ui: React.ReactElement, search = "") {
  const navigation: NavigationAdapter = {
    pathname: "/acme/links/link",
    searchParams: new URLSearchParams(search),
    hash: "",
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    getShareableUrl: (path) => path,
  };
  return renderWithI18n(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}
    >
      <NavigationProvider value={navigation}>{ui}</NavigationProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  state.role = "member";
  state.desktop = false;
  state.list.mockResolvedValue([link]);
  state.create.mockResolvedValue(link);
});

describe("embedded links", () => {
  it.each([false, true])("uses the explicit sandbox policy (session=%s)", async (allowSameOrigin) => {
    state.list.mockResolvedValue([{ ...link, allowSameOrigin }]);
    render(<EmbeddedLinkPage id="link" />);
    const iframe = await screen.findByTitle("Docs");
    expect(iframe.getAttribute("sandbox")).toBe(LINK_SANDBOX + (allowSameOrigin ? " allow-same-origin" : ""));
    expect(iframe.getAttribute("referrerpolicy")).toBe("no-referrer");
    expect(iframe.getAttribute("src")).toBe("https://example.com/acme");
    await userEvent.click(screen.getAllByRole("button", { name: "Open in new tab" })[0]!);
    expect(state.external).toHaveBeenCalledWith("https://example.com/acme");
  });
  it("loads project scope from route context", async () => {
    state.list.mockResolvedValue([
      { ...link, projectId: "project", urlTemplate: "https://example.com/{project_id}" },
    ]);
    render(<EmbeddedLinkPage id="link" />, "project=project");
    expect((await screen.findByTitle("Docs")).getAttribute("src")).toBe("https://example.com/project");
    expect(state.list).toHaveBeenCalledWith("project");
  });
  it("shows the standard missing state for deleted links", async () => {
    state.list.mockResolvedValue([]);
    render(<EmbeddedLinkPage id="gone" />);
    expect(await screen.findByText("Link not found")).toBeTruthy();
  });
});
describe("links bar", () => {
  it("uses the existing desktop external opener", async () => {
    state.desktop = true;
    state.list.mockResolvedValue([{ ...link, openMode: "new_tab" }]);
    render(<LinksBar />);
    await userEvent.click(await screen.findByRole("link", { name: "Docs" }));
    expect(state.external).toHaveBeenCalledWith("https://example.com/acme");
  });
  it("puts project links before workspace links and keeps destinations safe", async () => {
    state.list.mockImplementation(async (projectId?: string) =>
      projectId
        ? [{ ...link, id: "project-link", projectId, name: "CI" }]
        : [{ ...link, openMode: "new_tab" }],
    );
    render(<LinksBar projectId="project" />);
    const ci = await screen.findByRole("link", { name: "CI" });
    expect(ci.getAttribute("href")).toBe("/acme/links/project-link?project=project");
    const docs = screen.getByRole("link", { name: "Docs" });
    expect(docs.getAttribute("target")).toBe("_blank");
    expect(docs.getAttribute("rel")).toBe("noopener noreferrer");
    expect(ci.compareDocumentPosition(docs) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
  it("renders nothing for members with no links", async () => {
    state.list.mockResolvedValue([]);
    const result = render(<LinksBar />);
    await waitFor(() => expect(state.list).toHaveBeenCalled());
    expect(result.container.textContent).toBe("");
  });
  it("offers the editor to admins with no links", async () => {
    state.role = "admin";
    state.list.mockResolvedValue([]);
    render(<LinksBar />);
    expect(await screen.findByRole("button", { name: "Add link" })).toBeTruthy();
  });
});
describe("link management", () => {
  it("hides management controls for members", async () => {
    render(<LinksSection />);
    expect(await screen.findByText("Docs")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Edit link" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete link" })).toBeNull();
  });
  it("persists admin reorder and delete mutations", async () => {
    state.role = "admin";
    state.list.mockResolvedValue([link, { ...link, id: "other", name: "CI" }]);
    state.update.mockResolvedValue(link);
    state.remove.mockResolvedValue(undefined);
    render(<LinksSection />);
    await screen.findByText("CI");
    await userEvent.click(screen.getAllByRole("button", { name: "Move down" })[0]!);
    await waitFor(() => expect(state.update).toHaveBeenCalledWith("link", { position: 1 }));
    expect(state.update).toHaveBeenCalledWith("other", { position: 0 });
    await userEvent.click(screen.getAllByRole("button", { name: "Delete link" })[0]!);
    await waitFor(() => expect(state.remove).toHaveBeenCalledWith("link"));
  });
});

describe("link editor", () => {
  it("previews expansion and preserves the form after a failed save", async () => {
    state.create.mockRejectedValue(new Error("offline"));
    render(<LinkEditor />);
    await userEvent.click(screen.getByRole("button", { name: "Add link" }));
    await userEvent.type(screen.getByLabelText("Name"), "Docs");
    await userEvent.type(screen.getByLabelText("URL"), "https://example.com/docs");
    expect(await screen.findByText("https://example.com/docs")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(state.create).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Docs", openMode: "embed", allowSameOrigin: false }),
    );
  });
});
