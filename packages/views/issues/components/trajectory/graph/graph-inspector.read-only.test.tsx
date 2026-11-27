import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { paths } from "@multica/core/paths";
import { NavigationProvider, type NavigationAdapter } from "../../../../navigation";
import { renderWithI18n } from "../../../../test/i18n";
import { GraphInspector } from "./graph-inspector";
import { graphFixture, graphId, graphNode } from "./graph-fixture";
vi.mock("@multica/core/paths", async original => ({ ...await original<typeof import("@multica/core/paths")>(), useWorkspacePaths: () => paths.workspace("synthetic") }));
afterEach(cleanup);
it("views the owning task inside Graph instead of opening the standard issue detail route", () => {
  const fixture = graphFixture(), select = vi.fn(), push = vi.fn();
  const adapter: NavigationAdapter = { pathname: "/synthetic/issues", searchParams: new URLSearchParams(), hash: "", push, replace: vi.fn(), back: vi.fn(), getShareableUrl: p => p };
  renderWithI18n(<NavigationProvider value={adapter}><GraphInspector graph={fixture.list} node={fixture.run} select={select} openLog={() => {}} /></NavigationProvider>);
  fireEvent.click(screen.getByLabelText("Open task"));
  expect(push).not.toHaveBeenCalled();
  expect(select).toHaveBeenCalledWith(`issue:${graphId(10)}`);
});
it("views an available referenced comment inside Graph without native navigation", () => {
  const fixture = graphFixture(), select = vi.fn(), push = vi.fn();
  const activity = graphNode("platform_event", 50, 10, "Synthetic activity", { comment_id: graphId(40), content: "Activity body" });
  const graph = { ...fixture.list, nodes: [...fixture.list.nodes, fixture.comment, activity] };
  const adapter: NavigationAdapter = { pathname: "/synthetic/issues", searchParams: new URLSearchParams(), hash: "", push, replace: vi.fn(), back: vi.fn(), getShareableUrl: p => p };
  renderWithI18n(<NavigationProvider value={adapter}><GraphInspector graph={graph} node={activity} select={select} openLog={() => {}} /></NavigationProvider>);
  fireEvent.click(screen.getByText("Open comment"));
  expect(push).not.toHaveBeenCalled(); expect(select).toHaveBeenCalledWith(fixture.comment.id);
});
