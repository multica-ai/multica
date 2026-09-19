/** @vitest-environment jsdom */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { createStore } from "zustand/vanilla";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { viewStoreSlice, type IssueViewState } from "@multica/core/issues/stores/view-store";
import { renderWithI18n } from "../../test/i18n";
import type { IssueGroupBranches } from "../surface/use-issue-group-branches";
import { WorkflowBoardView } from "./workflow-board-view";

vi.mock("./board-view", () => ({ BoardView: ({ groupBranches, ownWorkflow, onCreateIssue }: any) => (
  <div data-testid="lane-board" data-own-workflow={ownWorkflow}>
    {groupBranches.descriptors.map((cell: any) => <button key={cell.key} onClick={() => onCreateIssue({ workflow_status_id: cell.value.workflow_status_id })}>{cell.value.name}</button>)}
  </div>
) }));
afterEach(cleanup);

const branches: IssueGroupBranches = {
  enabled: true, issues: [], pagination: {}, total: 57, isLoading: false, isRefreshing: false, isError: false,
  hasMoreGroups: false, isLoadingMoreGroups: false, loadMoreGroups: vi.fn(), retryGroups: vi.fn(),
  descriptors: ["Engineering", "Design"].map((name, i) => ({
    key: `workflow:${i}`, value: { kind: "workflow", workflow_id: String(i), name }, count: i ? 6 : 51,
    secondary_groups: [{ key: `opaque-${i}`, value: { kind: "workflow_status", workflow_id: String(i), workflow_status_id: `review-${i}`, name: `${name} review`, status: "in_review" }, count: i ? 6 : 51 }],
  })),
};
function renderBoard(data = branches) {
  const store = createStore<IssueViewState>()((set) => viewStoreSlice(set));
  const onCreateIssue = vi.fn();
  renderWithI18n(<ViewStoreProvider store={store}><WorkflowBoardView issues={[]} visibleStatuses={[]} hiddenStatuses={[]}
    onMoveIssue={vi.fn()} onCreateIssue={onCreateIssue} groupBranches={data} /> </ViewStoreProvider>);
  return { store, onCreateIssue };
}
describe("workflow board", () => {
  it("renders server lanes and counts before cards arrive, and collapses without changing filters", () => {
    const { store } = renderBoard();
    expect(screen.getAllByTestId("lane-board")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Design 6" })).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(screen.getByRole("button", { name: "Design 6" }));
    expect(store.getState().expandedWorkflowLanes).toContain("workflow:1");
    expect(screen.getAllByTestId("lane-board")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "Engineering 51" }));
    expect(screen.getAllByTestId("lane-board")).toHaveLength(1);
    expect(store.getState().collapsedWorkflowLanes).toEqual(["workflow:0"]);
    expect(store.getState().statusFilters).toEqual([]);
    expect(screen.getByRole("button", { name: "Engineering 51" })).toHaveAttribute("aria-expanded", "false");
  });
  it("requires an explicit project choice while retaining the exact target node", () => {
    const { onCreateIssue } = renderBoard();
    fireEvent.click(screen.getByRole("button", { name: "Design 6" }));
    fireEvent.click(screen.getByRole("button", { name: "Design review" }));
    expect(onCreateIssue).toHaveBeenCalledWith({ project_id: null, required_workflow_id: "1", require_project_choice: true, workflow_status_id: "review-1" });
    for (const board of screen.getAllByTestId("lane-board")) expect(board).toHaveAttribute("data-own-workflow", "true");
  });
  it("omits the lane header only after the server confirms there is a single workflow", () => {
    renderBoard({ ...branches, descriptors: branches.descriptors.slice(0, 1) });
    expect(screen.queryByRole("button", { name: "Engineering 51" })).toBeNull();
    expect(screen.getByTestId("lane-board")).toBeInTheDocument();
    expect(screen.queryByRole("separator")).toBeNull();
    cleanup();
    renderBoard({ ...branches, descriptors: branches.descriptors.slice(0, 1), hasMoreGroups: true });
    expect(screen.getByRole("button", { name: "Engineering 51" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Show more workflows" }));
    expect(branches.loadMoreGroups).toHaveBeenCalledOnce();
  });
  it("resizes one lane with the keyboard, retains it across collapse, and resets to automatic height", () => {
    const { store } = renderBoard();
    const handle = screen.getByRole("separator", { name: "Resize Engineering lane" });
    fireEvent.keyDown(handle, { key: "ArrowDown" });
    expect(store.getState().workflowLaneHeights).toEqual({ "workflow:0": 840 });
    expect(store.getState().statusFilters).toEqual([]);
    fireEvent.click(screen.getByRole("button", { name: "Engineering 51" }));
    fireEvent.click(screen.getByRole("button", { name: "Engineering 51" }));
    const body = document.getElementById(screen.getByRole("separator", { name: "Resize Engineering lane" }).getAttribute("aria-controls")!);
    expect(body).toHaveStyle({ height: "840px" });
    fireEvent.click(screen.getByRole("button", { name: "Reset Engineering to automatic height" }));
    expect(store.getState().workflowLaneHeights).toEqual({});
  });
  it("commits pointer resizing on release and cancels interrupted gestures", () => {
    const { store } = renderBoard();
    // jsdom has no PointerEvent; a MouseEvent subclass supplies pointerId.
    const pointer = (type: string, y: number) => {
      const event = new MouseEvent(type, { bubbles: true, clientY: y, button: 0 });
      Object.defineProperty(event, "pointerId", { value: 1 });
      return event;
    };
    const handle = screen.getByRole("separator", { name: "Resize Engineering lane" });
    handle.setPointerCapture = vi.fn();
    handle.releasePointerCapture = vi.fn();
    fireEvent(handle, pointer("pointerdown", 100));
    fireEvent(handle, pointer("pointermove", 200));
    expect(store.getState().workflowLaneHeights).toEqual({});
    fireEvent(handle, pointer("pointerup", 200));
    expect(store.getState().workflowLaneHeights).toEqual({ "workflow:0": 900 });
    fireEvent(handle, pointer("pointerdown", 200));
    fireEvent(handle, pointer("pointermove", 300));
    fireEvent(handle, pointer("pointercancel", 300));
    expect(store.getState().workflowLaneHeights).toEqual({ "workflow:0": 900 });
    fireEvent.doubleClick(handle);
    expect(store.getState().workflowLaneHeights).toEqual({});
  });
});
