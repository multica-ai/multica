// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const lifecycle = vi.hoisted(() => ({
  setup: undefined as (() => () => void) | undefined,
  onAppState: undefined as ((state: string) => void) | undefined,
}));

vi.mock("react", async (importOriginal) => ({
  ...await importOriginal<typeof import("react")>(),
  useEffect: (setup: () => () => void) => { lifecycle.setup = setup; },
  useRef: (current: unknown) => ({ current }),
  useState: () => [null, vi.fn()],
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
vi.mock("@/data/auth-store", () => ({ useAuthStore: () => "user-1" }));
vi.mock("@/data/workspace-store", () => ({ useWorkspaceStore: () => "workspace" }));
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
});
