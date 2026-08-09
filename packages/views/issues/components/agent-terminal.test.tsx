// @vitest-environment jsdom

import { act, cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../test/i18n";
import { AgentTerminal } from "./agent-terminal";

const mocks = vi.hoisted(() => ({
  terminalOpen: vi.fn(),
  terminalWrite: vi.fn(),
  terminalDispose: vi.fn(),
  terminalLoadAddon: vi.fn(),
  fit: vi.fn(),
  clientConnect: vi.fn(),
  clientDisconnect: vi.fn(),
  clientClaim: vi.fn(),
  clientRelease: vi.fn(),
  clientResize: vi.fn(),
  clientCtrlC: vi.fn(),
  clientInput: vi.fn(),
  terminalOnData: null as ((data: string) => void) | null,
  terminalKeyHandler: null as ((event: KeyboardEvent) => boolean) | null,
  searchFindNext: vi.fn(() => true),
  searchFindPrevious: vi.fn(() => true),
  searchClear: vi.fn(),
  webglDispose: vi.fn(),
  terminalOptions: null as Record<string, unknown> | null,
  handlers: null as Record<string, (...args: unknown[]) => void> | null,
  resizeCallback: null as (() => void) | null,
  mobile: false,
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getTaskTerminalWebSocketConfig: () => ({ url: "ws://example.test", token: null }),
  },
}));

vi.mock("@multica/core/terminal", () => ({
  TerminalClient: class {
    constructor(_config: unknown, handlers: Record<string, (...args: unknown[]) => void>) {
      mocks.handlers = handlers;
    }
    connect = mocks.clientConnect;
    disconnect = mocks.clientDisconnect;
    claimControl = mocks.clientClaim;
    releaseControl = mocks.clientRelease;
    resize = mocks.clientResize;
    ctrlC = mocks.clientCtrlC;
    sendInput = mocks.clientInput;
  },
}));

vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    constructor(options: Record<string, unknown>) {
      mocks.terminalOptions = options;
    }
    cols = 120;
    rows = 32;
    unicode = { activeVersion: "6" };
    loadAddon = mocks.terminalLoadAddon;
    open = mocks.terminalOpen;
    write = mocks.terminalWrite;
    dispose = mocks.terminalDispose;
    attachCustomKeyEventHandler(callback: (event: KeyboardEvent) => boolean) {
      mocks.terminalKeyHandler = callback;
    }
    onData(callback: (data: string) => void) {
      mocks.terminalOnData = callback;
      return { dispose: vi.fn() };
    }
  },
}));

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit = mocks.fit;
  },
}));

vi.mock("@xterm/addon-search", () => ({
  SearchAddon: class {
    findNext = mocks.searchFindNext;
    findPrevious = mocks.searchFindPrevious;
    clearDecorations = mocks.searchClear;
  },
}));

vi.mock("@xterm/addon-unicode11", () => ({
  Unicode11Addon: class {},
}));

vi.mock("@xterm/addon-web-links", () => ({
  WebLinksAddon: class {},
}));

vi.mock("@xterm/addon-webgl", () => ({
  WebglAddon: class {
    dispose = mocks.webglDispose;
    onContextLoss() {
      return { dispose: vi.fn() };
    }
  },
}));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.handlers = null;
  mocks.terminalOnData = null;
  mocks.terminalKeyHandler = null;
  mocks.terminalOptions = null;
  mocks.resizeCallback = null;
  mocks.mobile = false;
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(callback: () => void) {
        mocks.resizeCallback = callback;
      }
      observe() {}
      disconnect() {}
    },
  );
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
    callback(0);
    return 1;
  });
  vi.stubGlobal("cancelAnimationFrame", vi.fn());
  vi.stubGlobal("matchMedia", () => ({
    matches: mocks.mobile,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const metadata = {
  available: true,
  protocol_version: 1,
  task_id: "task-1",
  session_id: "019fe469-33bc-75c2-9492-ca640a1788a4",
  status: "running" as const,
  structured_observation: "stale" as const,
};

describe("AgentTerminal", () => {
  it("mounts xterm, relays output/input/resize, and disconnects without stopping the task", async () => {
    const view = renderWithI18n(<AgentTerminal taskId="task-1" metadata={metadata} />);
    await act(async () => {});

    expect(mocks.terminalOpen).toHaveBeenCalledTimes(1);
    expect(mocks.terminalOptions?.["allowProposedApi"]).toBe(true);
    expect(mocks.clientConnect).toHaveBeenCalledTimes(1);
    expect(mocks.fit).toHaveBeenCalled();
    expect(mocks.clientResize).toHaveBeenCalledWith(120, 32);

    act(() => mocks.terminalOnData?.("中文"));
    expect(mocks.clientInput).toHaveBeenCalledWith("中文");
    act(() => mocks.handlers?.["onOutput"]?.(new TextEncoder().encode("ANSI"), 1));
    expect(Array.from(mocks.terminalWrite.mock.calls[0]?.[0] as Uint8Array)).toEqual([
      65, 78, 83, 73,
    ]);

    act(() => mocks.handlers?.["onConnectionState"]?.("connected"));
    fireEvent.click(screen.getByRole("button", { name: "Claim control" }));
    expect(mocks.clientClaim).toHaveBeenCalledTimes(1);
    act(() =>
      mocks.handlers?.["onControl"]?.({ controller: true, leaseToken: "lease" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Terminal actions" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Send Ctrl+C" }));
    expect(mocks.clientCtrlC).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: "Search terminal" }));
    const search = screen.getByRole("textbox", { name: "Search terminal" });
    fireEvent.change(search, { target: { value: "Codex" } });
    expect(mocks.searchFindNext).toHaveBeenCalledWith("Codex", { incremental: true });
    fireEvent.click(screen.getByRole("button", { name: "Previous match" }));
    expect(mocks.searchFindPrevious).toHaveBeenCalledWith("Codex");
    fireEvent.click(screen.getByRole("button", { name: "Close search" }));
    expect(mocks.searchClear).toHaveBeenCalled();

    let browserFindSuppressed = false;
    act(() => {
      browserFindSuppressed =
        mocks.terminalKeyHandler?.(
          new KeyboardEvent("keydown", { key: "f", metaKey: true }),
        ) === false;
    });
    expect(browserFindSuppressed).toBe(true);
    expect(screen.getByRole("textbox", { name: "Search terminal" })).toBeInTheDocument();

    view.unmount();
    expect(mocks.clientDisconnect).toHaveBeenCalledTimes(1);
    expect(mocks.terminalDispose).toHaveBeenCalledTimes(1);
  });

  it("keeps the mobile terminal read-only", async () => {
    mocks.mobile = true;
    renderWithI18n(<AgentTerminal taskId="task-1" metadata={metadata} />);
    await act(async () => {});

    expect(screen.getByText("Mobile is read-only")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Claim control" })).not.toBeInTheDocument();
  });
});
