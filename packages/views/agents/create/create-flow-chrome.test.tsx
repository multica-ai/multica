// @vitest-environment jsdom

import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Button } from "@multica/ui/components/ui/button";
import { renderWithI18n } from "../../test/i18n";
import { AgentCreateShell } from "./create-shell";
import { CreateAgentFooter, CreateFlowFooter } from "./create-agent-footer";
import { BuilderWorkspaceLayout } from "./builder-workspace-layout";

const h = vi.hoisted(() => ({
  keyboard: null as null | { occludedBottom: number; viewportHeight: number },
}));
vi.mock("../../chat/components/use-visual-viewport-keyboard", () => ({
  useVisualViewportKeyboard: () => h.keyboard,
}));

afterEach(() => {
  cleanup();
  h.keyboard = null;
});

describe("AgentCreateShell", () => {
  it("puts close, the title and the step indicator in one header", () => {
    const onClose = vi.fn();
    renderWithI18n(
      <AgentCreateShell title="Create an agent" step="unused" stepIndex={2} onClose={onClose}>
        <main />
      </AgentCreateShell>,
    );

    const header = screen.getByRole("banner");
    expect(
      within(header).getByRole("heading", { name: "Create an agent" }),
    ).toBeInTheDocument();

    const steps = within(header).getByRole("list", { name: "Agent creation steps" });
    const current = within(steps)
      .getAllByRole("listitem")
      .find((item) => item.getAttribute("aria-current") === "step");
    expect(current).toHaveTextContent("Configure");
    expect(within(steps).getByText("Choose runtime")).toBeInTheDocument();
    expect(within(steps).getByText("Done")).toBeInTheDocument();

    fireEvent.click(within(header).getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("names the screen instead of showing steps off the step path", () => {
    renderWithI18n(
      <AgentCreateShell
        title="Create an agent"
        step="Build through conversation"
        onClose={vi.fn()}
      >
        <main />
      </AgentCreateShell>,
    );

    const header = screen.getByRole("banner");
    expect(within(header).getByText("Build through conversation")).toBeInTheDocument();
    expect(within(header).queryByRole("list")).not.toBeInTheDocument();
    expect(within(header).getByRole("button", { name: "Close" })).toBeInTheDocument();
  });

  it("keeps the close header and workspace together inside the mobile keyboard viewport", () => {
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 390 });
    h.keyboard = { occludedBottom: 300, viewportHeight: 420 };
    const onClose = vi.fn();
    const onCreate = vi.fn();
    const shell = () => (
      <AgentCreateShell
        title="Create an agent"
        step="Build through conversation"
        onClose={onClose}
        fitVisualViewport
      >
        <BuilderWorkspaceLayout
          conversation={<textarea aria-label="Message" />}
          configuration={<input aria-label="Name" />}
          footer={<button onClick={onCreate}>Create agent</button>}
        />
      </AgentCreateShell>
    );
    const view = renderWithI18n(shell());
    const layout = screen.getByTestId("agent-create-shell");
    expect(layout).toHaveStyle({ position: "fixed", height: "420px", bottom: "300px" });
    const header = within(layout).getByRole("banner");
    const workspace = within(layout).getByTestId("agent-builder-workspace");
    expect(workspace.style.position).toBe("");
    fireEvent.click(within(workspace).getByRole("tab", { name: "Agent configuration" }));
    expect(within(workspace).getByRole("textbox", { name: "Name" })).toBeVisible();
    fireEvent.click(within(workspace).getByRole("button", { name: "Create agent" }));
    expect(onCreate).toHaveBeenCalledTimes(1);
    fireEvent.click(within(header).getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(1);

    h.keyboard = { occludedBottom: 250, viewportHeight: 380 };
    view.rerender(shell());
    expect(layout).toHaveStyle({ height: "380px", bottom: "250px" });

    h.keyboard = null;
    view.rerender(shell());
    expect(layout.style.position).toBe("");
    expect(layout.style.height).toBe("");
    expect(layout.style.bottom).toBe("");
  });

  it.each([
    { width: 390, fitVisualViewport: false },
    { width: 1280, fitVisualViewport: true },
  ])("keeps normal positioning at $width px with fitVisualViewport=$fitVisualViewport", ({ width, fitVisualViewport }) => {
    Object.defineProperty(window, "innerWidth", { configurable: true, value: width });
    h.keyboard = { occludedBottom: 300, viewportHeight: 420 };
    renderWithI18n(
      <AgentCreateShell
        title="Create an agent"
        step="Configure"
        onClose={vi.fn()}
        fitVisualViewport={fitVisualViewport}
      >
        <main />
      </AgentCreateShell>,
    );
    expect(screen.getByTestId("agent-create-shell").style.position).toBe("");
  });
});

describe("create flow footer", () => {
  it("shows the step position next to Cancel and one Next", () => {
    renderWithI18n(
      <CreateFlowFooter stepIndex={1}>
        <Button variant="ghost">Cancel</Button>
        <Button>Next</Button>
      </CreateFlowFooter>,
    );

    const footer = screen.getByRole("contentinfo");
    expect(within(footer).getByText("Step 1 of 3")).toBeInTheDocument();
    expect(within(footer).getAllByRole("button").map((b) => b.textContent)).toEqual([
      "Cancel",
      "Next",
    ]);
  });

  it("ends the configure step with Cancel and Create agent", () => {
    const onCreate = vi.fn();
    const onCancel = vi.fn();
    renderWithI18n(
      <CreateAgentFooter
        stepIndex={2}
        canCreate
        creating={false}
        squad={false}
        error={null}
        onCreate={onCreate}
        onCancel={onCancel}
      />,
    );

    const footer = screen.getByRole("contentinfo");
    expect(within(footer).getByText("Step 2 of 3")).toBeInTheDocument();
    fireEvent.click(within(footer).getByRole("button", { name: "Cancel" }));
    fireEvent.click(within(footer).getByRole("button", { name: "Create agent" }));
    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onCreate).toHaveBeenCalledTimes(1);
  });

  it("leaves out the step position where the flow has none", () => {
    renderWithI18n(
      <CreateAgentFooter
        canCreate
        creating={false}
        squad={false}
        error={null}
        onCreate={vi.fn()}
        onDiscard={vi.fn()}
      />,
    );

    const footer = screen.getByRole("contentinfo");
    expect(within(footer).queryByText(/Step \d of 3/)).not.toBeInTheDocument();
    expect(within(footer).getByRole("button", { name: "Create agent" })).toBeInTheDocument();
  });
});
