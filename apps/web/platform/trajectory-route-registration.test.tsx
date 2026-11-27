import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { issueViewKeys } from "@multica/core/issue-views/queries";
import { trajectoryCapabilityKeys } from "@multica/core/trajectory/capability";
import { useActiveIssueViewStore, issueViewContainerKey } from "@multica/core/issue-views/active-view-store";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../../../packages/views/test/i18n";
import { NavigationProvider, type NavigationAdapter } from "@multica/views/navigation";
import type { TrajectoryHeaderRenderer } from "@multica/views/issues/trajectory-entry";
import IssuesRoute from "../app/[workspaceSlug]/(dashboard)/issues/page";
import ProjectRoute from "../app/[workspaceSlug]/(dashboard)/projects/[id]/page";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("./use-issue-view-url-sync", () => ({ useIssueViewUrlSync: () => {} }));
vi.mock("@multica/views/issues/trajectory-entry", async original => {
  const actual = await original<typeof import("@multica/views/issues/trajectory-entry")>();
  return { ...actual, TrajectoryRouteHost: ({ children }: { children: (trailing: TrajectoryHeaderRenderer) => ReactNode }) => <actual.TrajectoryRouteHost loadPage={async () => ({ default: (props: { onNativeFallback: () => void }) => <><p>Activated trajectory</p><button type="button" onClick={props.onNativeFallback}>Back to list</button></> })}>{children}</actual.TrajectoryRouteHost> };
});
function NativePage({ renderHeaderTrailing, projectId }: { renderHeaderTrailing?: TrajectoryHeaderRenderer; projectId?: string }) {
  const query = { scope: projectId ? { kind: "project" as const, project_id: projectId } : { kind: "workspace" as const }, filters: {}, sort: { field: "created_at" as const, direction: "asc" as const } };
  return <><input aria-label="Native route search" defaultValue="A" />{renderHeaderTrailing?.({ projectId, tableQuerySpec: query, tableSearch: "A", viewMode: "table", isLoading: false, filterDependencyError: null } as Parameters<TrajectoryHeaderRenderer>[0])}</>;
}
vi.mock("@multica/views/issues/components", () => ({ IssuesPage: (props: Parameters<typeof NativePage>[0]) => <NativePage {...props} /> }));
vi.mock("@multica/views/projects/components", () => ({ ProjectDetail: (props: Parameters<typeof NativePage>[0]) => <NativePage {...props} /> }));
afterEach(cleanup);
const adapter: NavigationAdapter = { pathname: "/synthetic/issues", searchParams: new URLSearchParams("view=A"), hash: "", push: vi.fn(), replace: vi.fn(), back: vi.fn(), getShareableUrl: p => p };
it.each(["issues", "project"])("registers the %s route's existing header slot and preserves its native child", async kind => {
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  client.setQueryData(trajectoryCapabilityKeys.detail("workspace-1"), { enabled: true });
  const scope = kind === "project" ? { scope_type: "project" as const, scope_id: "project-1" } : { scope_type: "workspace" as const };
  useActiveIssueViewStore.setState({ active: { [issueViewContainerKey("workspace-1", scope)]: "A" } });
  client.setQueryData(issueViewKeys.list("workspace-1", scope), [{ id: "A" }]);
  const params = Promise.resolve({ id: "project-1" });
  await act(async () => {
    renderWithI18n(<QueryClientProvider client={client}><NavigationProvider value={adapter}>{kind === "issues" ? <IssuesRoute /> : <ProjectRoute params={params} />}</NavigationProvider></QueryClientProvider>);
  });
  const native = await screen.findByRole("textbox", { name: "Native route search" });
  fireEvent.click(screen.getByRole("button", { name: "Open trajectory" }));
  await screen.findByText("Activated trajectory");
  fireEvent.click(screen.getByRole("button", { name: "Back to list" }));
  expect(screen.getByRole("textbox", { name: "Native route search" })).toBe(native);
  expect(native).toHaveValue("A");
  client.clear();
});
