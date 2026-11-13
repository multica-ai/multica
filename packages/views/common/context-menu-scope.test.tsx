import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { findContextMenuScope, useContextMenuScope, type ContextMenuScope } from "./context-menu-scope";

function Scope({
  read,
  content,
  children,
}: {
  read: () => Partial<ContextMenuScope> | null;
  content?: boolean;
  children: ReactNode;
}) {
  return <div ref={useContextMenuScope(read, { content })}>{children}</div>;
}

const item = (label: string) => ({ key: label, icon: null, label, onSelect: vi.fn() });

describe("findContextMenuScope", () => {
  it("names the submenu from the nearest scope with groups and gathers every scope's selection entries, outermost first", () => {
    render(
      <Scope read={() => ({ selectionActions: () => [item("Add to reply")] })}>
        <Scope
          read={() => ({
            label: "Comment actions",
            icon: null,
            groups: [[item("Reply")], []],
            selectionActions: (text) => [item(`Create from ${text}`)],
          })}
        >
          <Scope read={() => ({ contentActions: [item("Preview")] })} content>
            <span>inside</span>
          </Scope>
        </Scope>
      </Scope>,
    );

    const scope = findContextMenuScope(screen.getByText("inside"));
    expect(scope.submenu?.label).toBe("Comment actions");
    expect(scope.submenu?.groups.map((group) => group.map((action) => action.label))).toEqual([["Reply"]]);
    expect(scope.contentActions.map((action) => action.label)).toEqual(["Preview"]);
    expect(scope.selectionActions("x").map((action) => action.label)).toEqual(["Add to reply", "Create from x"]);
  });

  it("reads the latest actions when a menu opens and skips scopes that opt out", () => {
    let label = "First";
    render(
      <Scope read={() => ({ label, icon: null, groups: [[item("Reply")]] })}>
        <Scope read={() => null}>
          <span>inside</span>
        </Scope>
      </Scope>,
    );
    label = "Second";
    expect(findContextMenuScope(screen.getByText("inside")).submenu?.label).toBe("Second");
  });

  it("marks content scopes so an enclosing row defers to them", () => {
    render(
      <Scope read={() => null} content>
        <span>file</span>
      </Scope>,
    );
    expect(screen.getByText("file").parentElement).toHaveAttribute("data-context-menu-content");
  });

  it("finds nothing outside any scope", () => {
    render(<span>loose</span>);
    const scope = findContextMenuScope(screen.getByText("loose"));
    expect(scope.submenu).toBeNull();
    expect(scope.contentActions).toEqual([]);
    expect(scope.selectionActions("x")).toEqual([]);
  });
});
