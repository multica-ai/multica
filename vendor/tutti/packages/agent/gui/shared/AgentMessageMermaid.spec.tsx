import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { getPortalContainer } from "@tutti-os/ui-system/portal-container";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi
} from "vitest";
import { AgentMessageMarkdown } from "./AgentMessageMarkdown";
import {
  AgentMessageMermaid,
  agentMessageMermaidLimits,
  resolveDraggedMermaidView,
  sanitizeMermaidSvg
} from "./AgentMessageMermaid";

const mermaidMocks = vi.hoisted(() => ({
  initialize: vi.fn(),
  render: vi.fn()
}));

vi.mock("mermaid", () => ({
  default: mermaidMocks
}));

const mermaidSource = `flowchart TD
    A["HTTP 已经取得正确状态<br/>曾探在线"] --> B{"响应时间戳是否<br/>大于缓存时间戳？"}
    B -->|"是"| C["更新缓存"]`;

describe("AgentMessageMermaid", () => {
  const nativePointerEvent = window.PointerEvent;

  beforeAll(() => {
    class TestPointerEvent extends MouseEvent {
      pointerId: number;

      constructor(
        type: string,
        init: PointerEventInit & { pointerId?: number } = {}
      ) {
        super(type, init);
        this.pointerId = init.pointerId ?? 0;
      }
    }
    Object.defineProperty(window, "PointerEvent", {
      configurable: true,
      value: TestPointerEvent
    });
    Object.defineProperties(HTMLElement.prototype, {
      hasPointerCapture: {
        configurable: true,
        value: () => false
      },
      releasePointerCapture: {
        configurable: true,
        value: () => undefined
      },
      setPointerCapture: {
        configurable: true,
        value: () => undefined
      }
    });
  });

  afterAll(() => {
    Object.defineProperty(window, "PointerEvent", {
      configurable: true,
      value: nativePointerEvent
    });
  });

  beforeEach(() => {
    mermaidMocks.initialize.mockReset();
    mermaidMocks.render.mockReset();
    mermaidMocks.render.mockResolvedValue({
      svg: '<svg viewBox="0 0 320 180"><text>Rendered diagram</text></svg>'
    });
    getPortalContainer().dataset.theme = "light";
  });

  afterEach(() => {
    getPortalContainer().removeAttribute("data-theme");
    vi.restoreAllMocks();
  });

  it("shows a placeholder during streaming and renders only after settlement", async () => {
    const { rerender } = render(
      <AgentMessageMermaid source={mermaidSource} streaming />
    );

    expect(screen.getByTestId("agent-mermaid-placeholder")).toBeInTheDocument();
    expect(screen.queryByText("flowchart TD")).not.toBeInTheDocument();
    expect(mermaidMocks.render).not.toHaveBeenCalled();

    rerender(<AgentMessageMermaid source={mermaidSource} streaming={false} />);

    await waitFor(() => {
      expect(screen.getByText("Rendered diagram")).toBeInTheDocument();
    });
    expect(mermaidMocks.initialize).toHaveBeenCalledWith(
      expect.objectContaining({
        htmlLabels: false,
        maxEdges: 500,
        maxTextSize: 50_000,
        secure: expect.arrayContaining(["htmlLabels", "securityLevel"]),
        securityLevel: "strict",
        startOnLoad: false,
        suppressErrorRendering: true,
        theme: "default"
      })
    );
    expect(mermaidMocks.render).toHaveBeenCalledWith(
      expect.stringMatching(/^agent-mermaid-/),
      mermaidSource
    );
  });

  it("hides Mermaid source while the streaming language marker is incomplete", () => {
    render(
      <AgentMessageMarkdown
        content={`\`\`\`merm\n${mermaidSource}\n\`\`\``}
        streaming
      />
    );

    expect(screen.getByTestId("agent-mermaid-placeholder")).toBeInTheDocument();
    expect(screen.queryByText(/flowchart TD/)).not.toBeInTheDocument();
    expect(mermaidMocks.render).not.toHaveBeenCalled();
  });

  it("keeps an unclosed Mermaid fence in the loading state", () => {
    render(
      <AgentMessageMarkdown
        content={`\`\`\`mermaid\n${mermaidSource}\n    P -->`}
      />
    );

    expect(screen.getByTestId("agent-mermaid-placeholder")).toBeInTheDocument();
    expect(screen.queryByText(/flowchart TD/)).not.toBeInTheDocument();
    expect(mermaidMocks.render).not.toHaveBeenCalled();
  });

  it("keeps inline markdown previews from turning Mermaid blocks into diagrams", () => {
    render(
      <AgentMessageMarkdown
        content={`\`\`\`mermaid\n${mermaidSource}\n\`\`\``}
        inline
      />
    );

    expect(screen.queryByTestId("agent-mermaid-placeholder")).toBeNull();
    expect(screen.getByText(/flowchart TD/)).toBeInTheDocument();
    expect(mermaidMocks.render).not.toHaveBeenCalled();
  });

  it("shows a compact failure state without exposing source code", async () => {
    mermaidMocks.render.mockRejectedValueOnce(new Error("invalid diagram"));

    render(
      <AgentMessageMarkdown
        content={`\`\`\`mermaid\n${mermaidSource}\n\`\`\``}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId("agent-mermaid-failure")).toBeInTheDocument();
    });
    expect(screen.queryByText(/flowchart TD/)).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Copy code" })
    ).toBeInTheDocument();
  });

  it("keeps the current SVG visible while refreshing it for a new theme", async () => {
    const content = `\`\`\`mermaid\n${mermaidSource}\n\`\`\``;
    const { rerender } = render(<AgentMessageMarkdown content={content} />);
    await screen.findByText("Rendered diagram");

    getPortalContainer().dataset.theme = "dark";
    rerender(<AgentMessageMarkdown content={content} />);

    expect(screen.getByText("Rendered diagram")).toBeInTheDocument();
    await waitFor(() => {
      expect(mermaidMocks.render).toHaveBeenCalledTimes(2);
    });
    expect(mermaidMocks.initialize).toHaveBeenLastCalledWith(
      expect.objectContaining({
        darkMode: true,
        theme: "dark"
      })
    );
  });

  it("does not invoke Mermaid when the source exceeds the text limit", () => {
    const source = "x".repeat(agentMessageMermaidLimits.maxTextSize + 1);
    render(<AgentMessageMermaid source={source} streaming={false} />);

    expect(screen.getByTestId("agent-mermaid-failure")).toBeInTheDocument();
    expect(screen.queryByText(source)).not.toBeInTheDocument();
    expect(mermaidMocks.render).not.toHaveBeenCalled();
  });

  it("opens a zoomable viewer and supports wheel zoom and Space-drag panning", async () => {
    render(
      <AgentMessageMarkdown
        content={`\`\`\`mermaid\n${mermaidSource}\n\`\`\``}
      />
    );
    const preview = await screen.findByRole("button", {
      name: "Expand Mermaid diagram"
    });

    fireEvent.click(preview);
    const viewer = screen.getByRole("dialog", {
      name: "Mermaid diagram viewer"
    });
    expect(viewer).toBeInTheDocument();
    expect(viewer).toHaveFocus();

    const stage = screen.getByTestId("agent-mermaid-viewer-stage");
    expect(
      fireEvent.wheel(stage, {
        cancelable: true,
        clientX: 300,
        clientY: 200,
        deltaY: -100
      })
    ).toBe(false);
    await waitFor(() => {
      expect(screen.getByRole("status")).toHaveTextContent("115%");
    });

    fireEvent.keyDown(window, { code: "Space", key: " " });
    expect(stage.closest(".agent-mermaid-viewer")).toHaveAttribute(
      "data-agent-mermaid-space-pressed",
      "true"
    );
    fireEvent.pointerDown(stage, {
      button: 0,
      clientX: 100,
      clientY: 100,
      pointerId: 1
    });
    expect(stage.closest(".agent-mermaid-viewer")).toHaveAttribute(
      "data-agent-mermaid-dragging",
      "true"
    );
    const draggedView = resolveDraggedMermaidView(
      { x: 0.39, y: 0.26, zoom: 1.149 },
      { originX: 0.39, originY: 0.26, startX: 100, startY: 100 },
      145,
      130
    );
    expect(draggedView.x).toBeCloseTo(45.39);
    expect(draggedView.y).toBeCloseTo(30.26);
    expect(draggedView.zoom).toBe(1.149);
    fireEvent.keyUp(window, { code: "Space", key: " " });

    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("sanitizes Mermaid SVG output before inserting it into the transcript", () => {
    const sanitized = sanitizeMermaidSvg(`
      <svg xmlns="http://www.w3.org/2000/svg">
        <script>alert("xss")</script>
        <a href="javascript:alert('xss')">
          <text onclick="alert('xss')">Safe label</text>
        </a>
        <foreignObject>
          <div xmlns="http://www.w3.org/1999/xhtml">
            <img src="invalid" onerror="alert('xss')" />
          </div>
        </foreignObject>
      </svg>
    `);
    const container = document.createElement("div");
    container.innerHTML = sanitized;

    expect(container.querySelector("script")).toBeNull();
    expect(container.querySelector("[onclick]")).toBeNull();
    expect(container.querySelector("[onerror]")).toBeNull();
    expect(container.querySelector('a[href^="javascript:"]')).toBeNull();
    expect(container.textContent).toContain("Safe label");
  });
});
