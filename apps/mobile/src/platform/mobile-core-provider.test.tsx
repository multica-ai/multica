import { act, useEffect } from "react";
import { afterAll, describe, expect, it, vi } from "vitest";

// Boots the real core under the globals React Native provides: `window` is
// the global object with no DOM events, and there is no document,
// localStorage or URL-driven routing.

vi.mock("react-native", () => ({
  Platform: { OS: "ios" },
  AppState: { currentState: "active", addEventListener: () => ({ remove: () => {} }) },
}));
vi.mock("@react-native-community/netinfo", () => ({
  default: { addEventListener: () => () => {} },
}));
vi.mock("expo-constants", () => ({ default: { expoConfig: { version: "0.2.0" } } }));
vi.mock("expo-localization", () => ({
  getLocales: () => [{ languageCode: "zh", scriptCode: "Hans", regionCode: "CN" }],
}));

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  static readonly OPEN = 1;
  readyState = 0;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this);
  }
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.readyState = 3;
  }
}

const requests: { path: string; headers: Record<string, string> }[] = [];
const responses: Record<string, unknown> = {
  "/api/me": {
    id: "user-1",
    email: "ada@example.com",
    name: "Ada",
    onboarded_at: "2026-01-01T00:00:00Z",
  },
  "/api/workspaces": [
    { id: "ws-acme", slug: "acme", name: "Acme", created_at: "2026-01-01T00:00:00Z" },
    { id: "ws-beta", slug: "beta", name: "Beta", created_at: "2026-01-01T00:00:00Z" },
  ],
};

vi.stubGlobal("window", globalThis);
vi.stubGlobal("localStorage", undefined);
vi.stubGlobal("sessionStorage", undefined);
vi.stubGlobal("WebSocket", FakeWebSocket);
vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
vi.stubGlobal(
  "fetch",
  vi.fn(async (input: string, init?: RequestInit) => {
    const url = new URL(input);
    requests.push({ path: url.pathname, headers: { ...(init?.headers as Record<string, string>) } });
    const body = responses[url.pathname] ?? {};
    return new Response(JSON.stringify(body), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }),
);
vi.stubEnv("EXPO_PUBLIC_API_URL", "https://api.example.test");

const memory = new Map<string, string>([
  ["multica_token", "session-token"],
  ["multica_mobile_last_workspace", "beta"],
]);
const { installDefaultStorageBackend } = await import("@multica/core/platform/storage");
installDefaultStorageBackend({
  getItem: (key) => memory.get(key) ?? null,
  setItem: (key, value) => void memory.set(key, value),
  removeItem: (key) => void memory.delete(key),
  keys: () => [...memory.keys()],
});

const { create } = await import("react-test-renderer");
const { useAuthStore } = await import("@multica/core/auth");
const { getCurrentSlug } = await import("@multica/core/platform");
const { MobileCoreProvider } = await import("./mobile-core-provider");
const { useWorkspaceSelection } = await import("./workspace");

let selection: ReturnType<typeof useWorkspaceSelection> | undefined;
function Probe() {
  const current = useWorkspaceSelection();
  useEffect(() => {
    selection = current;
  });
  return null;
}

afterAll(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("MobileCoreProvider", () => {
  it("restores the session, selects the remembered workspace and connects realtime", async () => {
    await act(async () => {
      create(
        <MobileCoreProvider>
          <Probe />
        </MobileCoreProvider>,
      );
    });
    await vi.waitFor(() => expect(selection?.status).toBe("ready"));

    expect(useAuthStore.getState().status).toBe("authenticated");
    expect(selection?.status === "ready" && selection.workspace.slug).toBe("beta");
    expect(getCurrentSlug()).toBe("beta");

    const me = requests.find((request) => request.path === "/api/me");
    expect(me?.headers.Authorization).toBe("Bearer session-token");
    expect(me?.headers["X-Client-Platform"]).toBe("mobile");
    expect(me?.headers["X-Client-OS"]).toBe("ios");

    await vi.waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0));
    const socket = FakeWebSocket.instances.at(-1)!;
    const url = new URL(socket.url);
    expect(`${url.origin}${url.pathname}`).toBe("wss://api.example.test/ws");
    expect(url.searchParams.get("workspace_slug")).toBe("beta");
    expect(url.searchParams.get("client_platform")).toBe("mobile");

    await act(async () => {
      socket.readyState = 1;
      socket.onopen?.();
    });
    expect(socket.sent.map((frame) => JSON.parse(frame))).toContainEqual({
      type: "auth",
      payload: { token: "session-token" },
    });
  });
});
