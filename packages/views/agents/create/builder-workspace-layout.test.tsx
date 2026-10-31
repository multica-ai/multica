// @vitest-environment jsdom

import { useState } from "react";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../test/i18n";
import { BuilderWorkspaceLayout } from "./builder-workspace-layout";

const h = vi.hoisted(() => ({ keyboard: null as null | { occludedBottom: number; viewportHeight: number } }));
vi.mock("../../chat/components/use-visual-viewport-keyboard", () => ({
  useVisualViewportKeyboard: () => h.keyboard,
}));

function Editor({ label }: { label: string }) {
  const [value, setValue] = useState("");
  return <textarea aria-label={label} value={value} onChange={(e) => setValue(e.target.value)} />;
}
function mount(width: number) {
  Object.defineProperty(window, "innerWidth", { configurable: true, value: width });
  return renderWithI18n(<BuilderWorkspaceLayout
    conversation={<Editor label="Message" />}
    configuration={<Editor label="Name" />}
    footer={<button>Create agent</button>}
  />);
}

afterEach(() => { cleanup(); h.keyboard = null; });

describe("responsive builder workspace", () => {
  it.each([320, 390, 767])("shows one pane at %ipx and retains both editors across switches", (width) => {
    mount(width);
    expect(screen.getByRole("tab", { name: "Conversation" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getAllByRole("tabpanel")).toHaveLength(1);
    const message = screen.getByRole("textbox", { name: "Message" });
    fireEvent.change(message, { target: { value: "Unsent message" } });
    fireEvent.click(screen.getByRole("tab", { name: "Agent configuration" }));
    expect(screen.getAllByRole("tabpanel")).toHaveLength(1);
    const name = screen.getByRole("textbox", { name: "Name" });
    fireEvent.change(name, { target: { value: "My agent" } });
    expect(screen.getByRole("button", { name: "Create agent" })).toBeVisible();
    fireEvent.click(screen.getByRole("tab", { name: "Conversation" }));
    expect(screen.getByRole("textbox", { name: "Message" })).toBe(message);
    expect(message).toHaveValue("Unsent message");
    fireEvent.click(screen.getByRole("tab", { name: "Agent configuration" }));
    expect(screen.getByRole("textbox", { name: "Name" })).toBe(name);
    expect(name).toHaveValue("My agent");
  });

  it.each([768, 1280])("retains both desktop panels and the resize handle at %ipx", (width) => {
    const { container } = mount(width);
    expect(screen.queryByRole("tab")).toBeNull();
    expect(screen.getAllByRole("textbox")).toHaveLength(2);
    expect(container.querySelector('[data-slot="resizable-handle"]')).not.toBeNull();
  });

  it("fits the mobile workspace to the keyboard's visible viewport and clears the style on close", () => {
    h.keyboard = { occludedBottom: 300, viewportHeight: 420 };
    const view = mount(390);
    const layout = screen.getByRole("tablist").parentElement!.parentElement!;
    expect(layout).toHaveStyle({ position: "fixed", height: "420px", bottom: "300px" });
    h.keyboard = null;
    view.rerender(<BuilderWorkspaceLayout conversation={null} configuration={null} footer={null} />);
    expect(layout.style.position).toBe("");
    expect(layout.style.height).toBe("");
  });
});
