// @vitest-environment node
import type { ReactElement } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import type { Project } from "@multica/core/types";
import { patchProjectsList } from "@/data/realtime/project-ws-updaters";
import { projectKeys } from "@/data/queries/projects";
import ProjectsPage from "./projects";

const state = vi.hoisted(() => ({
  lifecycle: "active", wsId: "ws-1", wsSlug: "acme",
  data: [] as Project[], push: vi.fn(), refetch: vi.fn(),
}));
vi.mock("react", async (original) => ({
  ...await original<typeof import("react")>(),
  useState: () => [state.lifecycle, (value: string) => { state.lifecycle = value; }],
  useMemo: (compute: () => unknown) => compute(),
  useCallback: (callback: unknown) => callback,
}));
vi.mock("react-native", () => ({
  ActivityIndicator: "ActivityIndicator", FlatList: "FlatList", RefreshControl: "RefreshControl", View: "View",
}));
vi.mock("react-native-safe-area-context", () => ({ SafeAreaView: "SafeAreaView" }));
vi.mock("@react-native-segmented-control/segmented-control", () => ({ default: "SegmentedControl" }));
vi.mock("expo-router", () => ({ Stack: { Screen: "Screen" }, router: { push: state.push } }));
vi.mock("@/components/ui/text", () => ({ Text: "Text" }));
vi.mock("@/components/ui/button", () => ({ Button: "Button" }));
vi.mock("@/components/ui/icon-button", () => ({ IconButton: "IconButton" }));
vi.mock("@/components/project/project-row", () => ({ ProjectRow: "ProjectRow" }));
vi.mock("@/data/api", () => ({ api: {} }));
vi.mock("@/lib/i18n", () => ({ useT: () => ({ t: (key: string) => key }) }));
vi.mock("@/data/workspace-store", () => ({ useWorkspaceStore: (selector: (s: unknown) => unknown) => selector({
  currentWorkspaceId: state.wsId, currentWorkspaceSlug: state.wsSlug,
}) }));
vi.mock("@tanstack/react-query", async (original) => ({
  ...await original<typeof import("@tanstack/react-query")>(),
  useQuery: () => ({ data: state.data, refetch: state.refetch, isRefetching: false }),
}));

// Like the run-cancel-button suite, exercise the actual screen's element
// props/callbacks with native hosts mocked. This does not verify native layout.
type Element = ReactElement<Record<string, any>>; // eslint-disable-line @typescript-eslint/no-explicit-any
function render() {
  const page = ProjectsPage();
  const tree = (page.type as (props: unknown) => Element)(page.props);
  const children = tree.props.children as Element[];
  return {
    page, segment: children[1]!.props.children as Element,
    list: children[2]!,
  };
}
const fixtures = ["planned", "in_progress", "paused", "completed", "cancelled"].map((status, index) => ({
  id: status, title: `Project ${status}`, status,
  updated_at: `2026-06-0${index + 1}T00:00:00Z`,
})) as Project[];

beforeEach(() => {
  state.lifecycle = "active"; state.wsId = "ws-1"; state.wsSlug = "acme";
  state.data = fixtures.map((p) => ({ ...p }));
  vi.clearAllMocks();
});

describe("native project browse screen wiring", () => {
  it("starts Active, switches using the native control and keeps all five states in the source list", () => {
    const first = render();
    expect(first.segment.props.selectedIndex).toBe(0);
    expect(first.segment.props.values).toEqual(["lifecycle.active (3)", "lifecycle.closed (2)", "lifecycle.all (5)"]);
    expect(first.list.props.data.map((p: Project) => p.status)).toEqual(["paused", "in_progress", "planned"]);
    first.segment.props.onChange({ nativeEvent: { selectedSegmentIndex: 1 } });
    expect(render().list.props.data.map((p: Project) => p.status)).toEqual(["cancelled", "completed"]);
    first.segment.props.onChange({ nativeEvent: { selectedSegmentIndex: 2 } });
    const all = render().list;
    expect(all.props.data).toHaveLength(5);
    const row = all.props.renderItem({ item: fixtures[4] }) as Element;
    row.props.onPress();
    expect(state.push).toHaveBeenCalledWith("/acme/project/cancelled");
    expect(state.data).toHaveLength(5);
  });

  it("reflects authoritative status patches and reopening without narrowing the canonical cache", () => {
    const qc = new QueryClient();
    qc.setQueryData(projectKeys.list("ws-1"), state.data);
    patchProjectsList(qc, "ws-1", { id: "planned", status: "completed" });
    state.data = qc.getQueryData<Project[]>(projectKeys.list("ws-1"))!;
    expect(render().list.props.data.map((p: Project) => p.id)).not.toContain("planned");
    render().segment.props.onChange({ nativeEvent: { selectedSegmentIndex: 1 } });
    expect(render().list.props.data.map((p: Project) => p.id)).toContain("planned");
    patchProjectsList(qc, "ws-1", { id: "planned", status: "in_progress" });
    state.data = qc.getQueryData<Project[]>(projectKeys.list("ws-1"))!;
    expect(render().list.props.data.map((p: Project) => p.id)).not.toContain("planned");
    render().segment.props.onChange({ nativeEvent: { selectedSegmentIndex: 0 } });
    expect(render().list.props.data.map((p: Project) => p.id)).toContain("planned");
    expect(qc.getQueryData<Project[]>(projectKeys.list("ws-1"))).toHaveLength(5);
    qc.clear();
  });

  it("separates workspace and lifecycle empty states and keys session state to the workspace", () => {
    state.data = [fixtures[3]!];
    const empty = render();
    expect(empty.list.type).toBe("FlatList");
    expect(empty.list.props.ListEmptyComponent.props.children.props.children).toBe("lifecycle.empty.active");
    expect(empty.list.props.refreshControl.props.onRefresh).toBe(state.refetch);
    state.data = [];
    expect(render().list.type).not.toBe("FlatList");
    state.wsId = "ws-2";
    expect(render().page.key).toBe("ws-2");
  });
});
