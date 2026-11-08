import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { getActiveTab, useTabStore } from "@/stores/tab-store";
import { DesktopWorkspaceEntry } from "./workspace-entry";

vi.mock("@multica/views/links", () => ({
  WorkspaceEntry: ({ onResolve }: { onResolve: (path: string) => void }) => (
    <button onClick={() => onResolve("/acme/links/home")}>Resolve home</button>
  ),
}));

beforeEach(() => {
  useTabStore.getState().reset();
});

describe("desktop workspace entry", () => {
  it("replaces the tab session and virtual history, leaving router writes to the coordinator", () => {
    useTabStore.getState().switchWorkspace("acme", "/acme/start");
    expect(getActiveTab(useTabStore.getState())?.url).toBe("/acme/start");
    render(<DesktopWorkspaceEntry />);
    fireEvent.click(screen.getByRole("button", { name: "Resolve home" }));
    const tab = getActiveTab(useTabStore.getState());
    expect(tab?.url).toBe("/acme/links/home");
    expect(tab?.history.stack).toEqual(["/acme/links/home"]);
    expect(tab?.history.index).toBe(0);
  });
});
