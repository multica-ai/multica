import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { Pin, Trash2, MessageSquare, Type } from "lucide-react";

import { EditorActionsMenu } from "./editor-actions-menu";

// Base UI menus don't reliably open in jsdom, so — matching the repo
// convention for these kebab menus (see runtime-row-menu.test.tsx) — these
// tests exercise the component's own contribution: the falsy-item filter and
// the trigger label. The menu-open + item-click behaviour is Base UI's.

describe("EditorActionsMenu", () => {
  it("renders nothing when no items survive the falsy filter", () => {
    const { container } = render(
      <EditorActionsMenu items={[false, null, undefined]} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("renders the trigger when at least one item survives", () => {
    render(
      <EditorActionsMenu
        triggerLabel="Note actions"
        items={[
          // Falsy conditional actions are skipped without leaving a hole.
          false,
          { key: "pin", label: "Pin", icon: Pin, onSelect: () => {} },
          null,
          {
            key: "comments",
            label: "Comments",
            icon: MessageSquare,
            onSelect: () => {},
          },
        ]}
      />,
    );
    expect(
      screen.getByRole("button", { name: /note actions/i }),
    ).toBeInTheDocument();
  });

  it("defaults the trigger label to 'Actions'", () => {
    render(
      <EditorActionsMenu
        items={[
          { key: "delete", label: "Delete", icon: Trash2, onSelect: () => {} },
        ]}
      />,
    );
    expect(
      screen.getByRole("button", { name: /actions/i }),
    ).toBeInTheDocument();
  });

  // FIR-4028 slice 8 — Text size leaves the action row for this menu, so the
  // menu needs a choice item: one label, several mutually exclusive values.
  it("keeps the trigger when the only surviving item is a choice", () => {
    render(
      <EditorActionsMenu
        triggerLabel="Note actions"
        items={[
          {
            key: "text-size",
            label: "Text size",
            icon: Type,
            value: "1",
            options: [
              { value: "0", label: "Small" },
              { value: "1", label: "Medium" },
              { value: "2", label: "Large" },
            ],
            onValueChange: () => {},
          },
        ]}
      />,
    );
    expect(
      screen.getByRole("button", { name: /note actions/i }),
    ).toBeInTheDocument();
  });

  it("ignores a choice item that has no options, rather than rendering an empty submenu", () => {
    const { container } = render(
      <EditorActionsMenu
        items={[
          {
            key: "text-size",
            label: "Text size",
            icon: Type,
            value: "1",
            options: [],
            onValueChange: () => {},
          },
        ]}
      />,
    );
    expect(container).toBeEmptyDOMElement();
  });
});
