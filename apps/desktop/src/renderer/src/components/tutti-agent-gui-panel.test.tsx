import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useEffect, useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentGuiHostEvent, AgentGuiInitSnapshot, AgentGuiRpcClient } from "@multica/tutti-agent-gui-host/protocol";
import { NavigationProvider } from "@multica/views/navigation";
import { TuttiAgentGuiPanel } from "./tutti-agent-gui-panel";

type Service = { load: ReturnType<typeof vi.fn>; request: ReturnType<typeof vi.fn>; publish: ReturnType<typeof vi.fn<(event: AgentGuiHostEvent) => void>>; dispose: ReturnType<typeof vi.fn>; subscribe: (listener: (event: AgentGuiHostEvent) => void) => () => void; reconnect: ReturnType<typeof vi.fn> };
const mock = vi.hoisted(() => ({
  foreground: true,
  create: vi.fn(),
  services: [] as unknown[],
  mounts: [] as Array<{ client: unknown; initial: unknown; onRendered?: () => void; unmounted: boolean }>,
  renderOnMount: true,
  loadFails: false,
}));
vi.mock("@multica/core/auth", () => ({ useAuthStore: { getState: () => ({ user: { id: "user" } }) } }));
vi.mock("@multica/core/platform", () => ({ getCurrentSlug: () => "acme", getCurrentWsId: () => "ws" }));
vi.mock("@multica/core/realtime", () => ({ useWSReconnect: () => {} }));
vi.mock("@multica/views/common/use-app-foreground", () => ({ useAppForeground: () => mock.foreground }));
vi.mock("@multica/ui/components/common/theme-provider", () => ({ useTheme: () => ({ resolvedTheme: "dark" }) }));
vi.mock("@multica/views/i18n", () => ({ useLocale: () => "en", useT: () => ({ t: (selector: (value: unknown) => string) => selector({ workbench: { title: "Agent GUI", loading: "Loading", load_error: "Failed", retry: "Retry" } }) }) }));
vi.mock("@multica/tutti-agent-gui-host/mount", () => ({
  default: function FakeMount(props: { client: AgentGuiRpcClient; initial: AgentGuiInitSnapshot; onRendered?: () => void }) {
    if (mock.loadFails) throw new Error("chunk failed");
    useEffect(() => {
      const record = { ...props, unmounted: false };
      mock.mounts.push(record);
      if (mock.renderOnMount) props.onRendered?.();
      return () => { record.unmounted = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    return <div data-testid="gui" />;
  },
}));

const props = { workspaceId: "ws", workspaceSlug: "acme", userId: "user", activeSessionId: null,
  active: true, serviceFactory: mock.create, onRestoreDraftApplied: vi.fn(), onOpenTask: vi.fn(), onShowMessage: vi.fn() };
const clients: QueryClient[] = [];
function TestProvider({ children }: { children: React.ReactNode }) {
  const [qc] = useState(() => new QueryClient());
  clients.push(qc);
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}
const services = () => mock.services as Service[];
const settle = () => act(async () => { await vi.dynamicImportSettled(); await new Promise((resolve) => setTimeout(resolve)); });
const mounted = async () => { await screen.findByTestId("gui"); await settle(); return mock.mounts.at(-1)!; };
const client = (mount: { client: unknown }) => mount.client as AgentGuiRpcClient;

beforeEach(() => {
  mock.foreground = true;
  mock.renderOnMount = true;
  mock.loadFails = false;
  mock.mounts.length = 0;
  vi.spyOn(document, "hasFocus").mockImplementation(() => mock.foreground);
  mock.services.length = 0;
  mock.create.mockReset();
  mock.create.mockImplementation(() => {
    const listeners = new Set<(event: AgentGuiHostEvent) => void>();
    const service: Service = { load: vi.fn().mockResolvedValue({ agents: [], sessions: [] }), request: vi.fn().mockResolvedValue(undefined),
      publish: vi.fn((event: AgentGuiHostEvent) => listeners.forEach((listener) => listener(event))), dispose: vi.fn(),
      subscribe: (listener) => { listeners.add(listener); return () => { listeners.delete(listener); }; }, reconnect: vi.fn() };
    mock.services.push(service);
    return service;
  });
});
afterEach(() => { cleanup(); clients.splice(0).forEach((qc) => qc.clear()); vi.restoreAllMocks(); vi.useRealTimers(); });

describe("Agent GUI lifetime", () => {
  it("recovers a failed initial workspace read when the service becomes ready without replacing it", async () => {
    const create = mock.create.getMockImplementation()!;
    mock.create.mockImplementation(() => {
      const service = create() as Service;
      service.load.mockRejectedValueOnce(new Error("daemon_offline: Runtime starting"));
      return service;
    });
    render(<TuttiAgentGuiPanel {...props} />, { wrapper: TestProvider });
    expect(await screen.findByRole("button", { name: "Retry" })).toBeVisible();
    const service = services()[0]!;
    await act(async () => service.publish({ type: "invalidate", scope: "workspace" }));
    await mounted();
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    expect(mock.create).toHaveBeenCalledOnce();
    expect(service.dispose).not.toHaveBeenCalled();
    const reads = service.load.mock.calls.length;
    await act(async () => service.publish({ type: "invalidate", scope: "workspace" }));
    expect(service.load).toHaveBeenCalledTimes(reads);
    expect(mock.mounts).toHaveLength(1);
  });

  it("dismisses GUI popovers on pointer actions outside it without intercepting them or leaking listeners", async () => {
    const view = render(<TuttiAgentGuiPanel {...props} />, { wrapper: TestProvider });
    await mounted();
    const service = services()[0]!;
    service.publish.mockClear();
    expect(fireEvent.pointerDown(document.body)).toBe(true);
    expect(service.publish).toHaveBeenCalledWith({ type: "dismissPopovers" });
    service.publish.mockClear();
    fireEvent.pointerDown(screen.getByTestId("gui"));
    expect(service.publish).not.toHaveBeenCalled();
    view.unmount();
    fireEvent.pointerDown(document.body);
    expect(service.publish).not.toHaveBeenCalled();
    expect(service.dispose).toHaveBeenCalled();
  });

  it("keeps initialization covered until the GUI has rendered", async () => {
    mock.renderOnMount = false;
    render(<TuttiAgentGuiPanel {...props} />, { wrapper: TestProvider });
    const mount = await mounted();
    expect(screen.getByRole("status").textContent).toContain("Loading");
    await act(async () => mount.onRendered?.());
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("offers a retry if rendering stalls, and retrying starts a new service", async () => {
    mock.renderOnMount = false;
    vi.useFakeTimers({ shouldAdvanceTime: true });
    render(<TuttiAgentGuiPanel {...props} />, { wrapper: TestProvider });
    await mounted();
    await act(async () => { await vi.advanceTimersByTimeAsync(25_000); });
    mock.renderOnMount = true;
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await act(async () => { await vi.advanceTimersByTimeAsync(10); await vi.dynamicImportSettled(); });
    expect(mock.create).toHaveBeenCalledTimes(2);
    expect(services()[0]!.dispose).toHaveBeenCalled();
  });

  it("shows the retry when the GUI chunk fails to load", async () => {
    mock.loadFails = true;
    vi.spyOn(console, "error").mockImplementation(() => {});
    render(<TuttiAgentGuiPanel {...props} />, { wrapper: TestProvider });
    expect(await screen.findByRole("button", { name: "Retry" })).toBeTruthy();
    mock.loadFails = false;
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await mounted();
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    expect(mock.create).toHaveBeenCalledTimes(2);
  });

  it("forwards new-conversation intent without replacing the GUI", async () => {
    const view = render(<TuttiAgentGuiPanel {...props} composerFocusSequence={1} />, { wrapper: TestProvider });
    await mounted();
    const service = services()[0]!;
    service.publish.mockClear();
    view.rerender(<TuttiAgentGuiPanel {...props} composerFocusSequence={2} newConversationRequest={{ sequence: 5, projectPath: "/repo" }} />);
    await settle();
    expect(service.publish).toHaveBeenCalledWith(expect.objectContaining({ type: "init", snapshot: expect.objectContaining({ composerFocusSequence: 2, newConversationRequest: { sequence: 5, projectPath: "/repo" } }) }));
    expect(mock.mounts).toHaveLength(1);
    expect(mock.create).toHaveBeenCalledTimes(1);
  });
});

describe("Agent GUI requests", () => {
  it("rejects user actions while the window lacks focus, and allows reads and admitted queue items", async () => {
    render(<TuttiAgentGuiPanel {...props} />, { wrapper: TestProvider });
    const mount = await mounted();
    const service = services()[0]!;
    mock.foreground = false;
    await expect(client(mount).request("session.send", { sessionId: "s", content: "x" } as never)).rejects.toMatchObject({ code: "inactive_host", status: 409 });
    expect(service.request).not.toHaveBeenCalled();
    await client(mount).request("storage.read", { key: "k" } as never);
    expect(service.request).toHaveBeenCalledWith("storage.read", { key: "k" });
    await client(mount).request("session.send", { sessionId: "s", content: "x", queued: true } as never);
    expect(service.request).toHaveBeenCalledWith("session.send", expect.objectContaining({ queued: true }));
  });

  it("rejects user actions from a hidden instance even when the window has focus", async () => {
    const view = render(<TuttiAgentGuiPanel {...props} />, { wrapper: TestProvider });
    const mount = await mounted();
    view.rerender(<TuttiAgentGuiPanel {...props} active={false} />);
    await expect(client(mount).request("session.cancel", { sessionId: "s" } as never)).rejects.toMatchObject({ code: "inactive_host" });
  });

  it("stops serving the old GUI once its panel unmounts", async () => {
    const view = render(<TuttiAgentGuiPanel {...props} />, { wrapper: TestProvider });
    const mount = await mounted();
    view.unmount();
    await expect(client(mount).request("workspace.load", undefined as never)).rejects.toMatchObject({ name: "AbortError" });
  });

  it("keeps the GUI and its draft mounted across visibility changes", async () => {
    const view = render(<NavigationProvider value={{ pathname: "/", searchParams: new URLSearchParams(), push: vi.fn(), replace: vi.fn(), back: vi.fn() } as never}><TuttiAgentGuiPanel {...props} /></NavigationProvider>, { wrapper: TestProvider });
    const mount = await mounted();
    view.rerender(<NavigationProvider value={{ pathname: "/", searchParams: new URLSearchParams(), push: vi.fn(), replace: vi.fn(), back: vi.fn() } as never}><TuttiAgentGuiPanel {...props} active={false} /></NavigationProvider>);
    await settle();
    expect(mount.unmounted).toBe(false);
    expect(mock.mounts).toHaveLength(1);
  });
});
