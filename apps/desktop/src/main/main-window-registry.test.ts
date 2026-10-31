// @vitest-environment node
import { describe, expect, it } from "vitest";
import { MainWindowRegistry } from "./main-window-registry";

describe("MainWindowRegistry", () => {
  it("finds the window that shows a workspace", () => {
    const registry = new MainWindowRegistry<string>();
    registry.add("a", "work");
    registry.add("b", "personal");

    expect(registry.findByWorkspace("personal")).toBe("b");
    expect(registry.findByWorkspace("personal", "b")).toBeNull();
    expect(registry.findByWorkspace("side")).toBeNull();
  });

  it("follows workspace switches inside a window", () => {
    const registry = new MainWindowRegistry<string>();
    registry.add("a", "work");

    expect(registry.setWorkspace("a", "personal")).toBe(true);
    expect(registry.setWorkspace("a", "personal")).toBe(false);
    expect(registry.setWorkspace("missing", "work")).toBe(false);
    expect(registry.findByWorkspace("work")).toBeNull();
    expect(registry.workspaceOf("a")).toBe("personal");
  });

  it("orders app-level targets by focus and restore by creation", () => {
    const registry = new MainWindowRegistry<string>();
    registry.add("a", "work");
    registry.add("b", "personal");
    expect(registry.mostRecent()).toBe("b");

    registry.focus("a");
    expect(registry.mostRecent()).toBe("a");
    expect(registry.mostRecent("a")).toBe("b");
    expect(registry.all()).toEqual(["a", "b"]);

    registry.remove("a");
    expect(registry.mostRecent()).toBe("b");
    expect(registry.all()).toEqual(["b"]);
    expect(registry.size).toBe(1);
  });

  it("lists the workspaces held by the other windows", () => {
    const registry = new MainWindowRegistry<string>();
    registry.add("a", "work");
    registry.add("b", "personal");
    registry.add("c", null);

    expect(registry.otherWorkspaces("a")).toEqual(["personal"]);
    expect(registry.otherWorkspaces("c")).toEqual(["work", "personal"]);
  });
});
