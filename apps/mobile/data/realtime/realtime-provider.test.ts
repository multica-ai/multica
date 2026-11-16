// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const lifecycle = vi.hoisted(() => ({
  userId: "user-1" as string | null,
  wsSlug: "workspace" as string | null,
  clientState: null as unknown,
  setup: undefined as (() => () => void) | undefined,
  onAppState: undefined as ((state: string) => void) | undefined,
}));

vi.mock("react", async (importOriginal) => ({
  ...await importOriginal<typeof import("react")>(),
  useEffect: (setup: () => () => void) => { lifecycle.setup = setup; },
  useRef: (current: unknown) => ({ current }),
  useState: () => [lifecycle.clientState, (value: unknown) => { lifecycle.clientState = value; }],
}));
vi.mock("react-native", () => ({
  AppState: {
    addEventListener: (_event: string, handler: (state: string) => void) => {
      lifecycle.onAppState = handler;
      return { remove: vi.fn() };
    },
  },
}));
vi.mock("@react-native-community/netinfo", () => ({
  default: { addEventListener: () => vi.fn() },
}));
vi.mock("@/data/auth-store", () => ({ useAuthStore: () => lifecycle.userId }));
vi.mock("@/data/workspace-store", () => ({ useWorkspaceStore: () => lifecycle.wsSlug }));
vi.mock("@/data/secure-storage", () => ({ getToken: async () => "token" }));
vi.mock("@/data/api", () => ({ api: { getToken: () => "token" } }));

// Run the provider's effect and its actual WSClient, without rendering RN.
class MockWebSocket {
  static instances: MockWebSocket[] = [];
  constructor() { MockWebSocket.instances.push(this); }
  close() {}
}

describe("RealtimeProvider foreground recovery", () => {
  let cleanup: (() => void) | undefined;

  beforeEach(() => {
    lifecycle.userId = "user-1";
    lifecycle.wsSlug = "workspace";
    lifecycle.clientState = null;
    lifecycle.setup = undefined;
    lifecycle.onAppState = undefined;
    MockWebSocket.instances = [];
    vi.stubGlobal("WebSocket", MockWebSocket);
    vi.stubEnv("EXPO_PUBLIC_API_URL", "https://example.test");
    vi.spyOn(console, "info").mockImplementation(() => {});
  });

  afterEach(() => {
    cleanup?.();
    cleanup = undefined;
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
    vi.restoreAllMocks();
  });

  it("opens exactly one socket per background or inactive return", async () => {
    const { RealtimeProvider } = await import("./realtime-provider");
    RealtimeProvider({ children: null });
    cleanup = lifecycle.setup!();
    // Let the effect finish reading the token and install native listeners.
    await Promise.resolve();
    expect(MockWebSocket.instances).toHaveLength(1);

    lifecycle.onAppState!("background");
    expect(MockWebSocket.instances).toHaveLength(1);
    lifecycle.onAppState!("active");
    expect(MockWebSocket.instances).toHaveLength(2);

    lifecycle.onAppState!("inactive");
    expect(MockWebSocket.instances).toHaveLength(2);
    lifecycle.onAppState!("active");
    expect(MockWebSocket.instances).toHaveLength(3);
  });

  it("does not open a socket if unmounted while the token is loading", async () => {
    const { RealtimeProvider } = await import("./realtime-provider");
    RealtimeProvider({ children: null });
    const cancel = lifecycle.setup!();
    cancel();
    await Promise.resolve();
    expect(MockWebSocket.instances).toHaveLength(0);
    expect(lifecycle.onAppState).toBeUndefined();
  });

  it("preserves the children reconciliation identity across workspace changes", async () => {
    const { RealtimeProvider } = await import("./realtime-provider");
    const { createElement } = await import("react");
    const child = createElement("navigation-stack");
    const before = RealtimeProvider({ children: child });
    lifecycle.wsSlug = "other-workspace";
    const after = RealtimeProvider({ children: child });
    expect(after.type).toBe(before.type);
    expect(after.key).toBe(before.key);
    expect(after.props.children).toBe(before.props.children);
  });

  it("hides the previous client while switching workspace or signing out", async () => {
    const { RealtimeProvider } = await import("./realtime-provider");
    RealtimeProvider({ children: null });
    cleanup = lifecycle.setup!();
    await Promise.resolve();
    expect(RealtimeProvider({ children: null }).props.value).not.toBeNull();
    cleanup();
    cleanup = undefined;
    lifecycle.wsSlug = "other-workspace";
    expect(RealtimeProvider({ children: null }).props.value).toBeNull();
    cleanup = lifecycle.setup!();
    await Promise.resolve();
    expect(RealtimeProvider({ children: null }).props.value).not.toBeNull();
    lifecycle.userId = null;
    expect(RealtimeProvider({ children: null }).props.value).toBeNull();
  });

});
