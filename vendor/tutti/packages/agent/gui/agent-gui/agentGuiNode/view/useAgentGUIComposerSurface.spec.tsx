import { act, useEffect } from "react";
import { createRoot } from "react-dom/client";
import { expect, it, vi } from "vitest";
import type { AgentComposerProps } from "../AgentComposer";
import { useAgentGUIComposerSurface } from "./useAgentGUIComposerSurface";

const lifetime = vi.hoisted(() => ({ mounted: vi.fn(), unmounted: vi.fn() }));
vi.mock("../AgentComposer", () => ({
  AgentComposer: () => {
    useEffect(() => { lifetime.mounted(); return () => { lifetime.unmounted(); }; }, []);
    return <input aria-label="Draft" />;
  }
}));

it("moves one editor from home to the dock without losing its draft or focus", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const container = document.body.appendChild(document.createElement("div"));
  const root = createRoot(container);
  const props = {} as AgentComposerProps;
  function Detail({ active, replace = false }: { active: boolean; replace?: boolean }) {
    const surface = useAgentGUIComposerSurface(props);
    return <>{surface.portal}{replace ? null : active ? <section>{surface.slot}</section> : <div>{surface.slot}</div>}</>;
  }
  try {
    await act(async () => root.render(<Detail active={false} />));
    const editor = container.querySelector("input")!;
    editor.value = "Keep draft";
    editor.focus();
    await act(async () => root.render(<Detail active />));
    expect(container.querySelector("section input")).toBe(editor);
    expect(editor.value).toBe("Keep draft");
    expect(document.activeElement).toBe(editor);
    expect(lifetime.mounted).toHaveBeenCalledOnce();
    expect(lifetime.unmounted).not.toHaveBeenCalled();
    await act(async () => root.render(<Detail active={false} />));
    expect(container.querySelector("input")).toBe(editor);
    await act(async () => root.render(<Detail active replace />));
    expect(container.querySelector("input")).toBeNull();
    expect(lifetime.unmounted).toHaveBeenCalledOnce();
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
  }
});
