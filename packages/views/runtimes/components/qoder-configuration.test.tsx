import { vi, it, expect, beforeEach, afterEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enRuntimes from "../../locales/en/runtimes.json";
import { QoderConfiguration } from "./qoder-configuration";

const mocks = vi.hoisted(() => ({
  getQoderConnection: vi.fn(),
  listMembers: vi.fn(),
  listQoderEnvironments: vi.fn(),
  checkQoderConnection: vi.fn(),
  saveQoderConnection: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({ api: mocks }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "owner" } };
  return {
    useAuthStore: Object.assign(
      (selector: (s: typeof state) => unknown) => selector(state),
      { getState: () => state },
    ),
  };
});
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

beforeEach(() => {
  mocks.getQoderConnection.mockResolvedValue({
    available: true,
    configured: true,
    name: "QCA",
    baseUrl: "https://api.qoder.com/api/v1/cloud",
    environmentId: "",
    hasToken: true,
    repositories: [],
    enabled: true,
    status: "online",
    lastError: "",
    maxConcurrency: 3,
    runTimeoutMinutes: 60,
    activeRuns: 0,
    queuedRuns: 0,
  });
  mocks.listMembers.mockResolvedValue([{ user_id: "owner", role: "owner" }]);
  mocks.checkQoderConnection.mockResolvedValue(undefined);
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
function mount(onConnected?: () => void) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <I18nProvider locale="en" resources={{ en: { runtimes: enRuntimes } }}>
      <QueryClientProvider client={client}>
        <QoderConfiguration onConnected={onConnected} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return client;
}
it("explains missing server encryption setup without offering credential submission", async () => {
  mocks.getQoderConnection.mockResolvedValue({ available: false, configured: false });
  const client = mount();
  expect(
    await screen.findByText("Server setup is required: configure MULTICA_VCS_SECRET_KEY."),
  ).toBeInTheDocument();
  expect(screen.queryByPlaceholderText("pt-…")).not.toBeInTheDocument();
  expect(mocks.saveQoderConnection).not.toHaveBeenCalled();
  client.clear();
});
it("points an existing connection to its runtime page", async () => {
  const client = mount();
  expect(
    await screen.findByText("Qoder connected. It will be ready in a few seconds."),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Connect" })).not.toBeInTheDocument();
  expect(mocks.listQoderEnvironments).not.toHaveBeenCalled();
  client.clear();
});
it("connects a first-time workspace with the short form", async () => {
  mocks.getQoderConnection.mockResolvedValue({
    ...(await mocks.getQoderConnection()),
    configured: false,
    hasToken: false,
    enabled: false,
    status: "offline",
  });
  mocks.listQoderEnvironments.mockResolvedValue([]);
  const client = mount();
  expect(await screen.findByRole("button", { name: "Connect" })).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Test connection" })).not.toBeInTheDocument();
  client.clear();
});
it("reports a first-time connection so the caller can close", async () => {
  mocks.getQoderConnection.mockResolvedValue({
    ...(await mocks.getQoderConnection()),
    configured: false,
    hasToken: false,
    enabled: false,
    status: "offline",
  });
  mocks.listQoderEnvironments.mockResolvedValue([
    { id: "env_one", name: "Web environment" },
  ]);
  mocks.saveQoderConnection.mockResolvedValue(undefined);
  const onConnected = vi.fn();
  const client = mount(onConnected);
  fireEvent.change(await screen.findByPlaceholderText("pt-…"), {
    target: { value: "pt-test" },
  });
  const connect = screen.getByRole("button", { name: "Connect" });
  await waitFor(() => expect(connect).toBeEnabled(), { timeout: 3000 });
  fireEvent.click(connect);
  await waitFor(() => expect(onConnected).toHaveBeenCalledTimes(1));
  expect(mocks.saveQoderConnection).toHaveBeenCalledWith(
    "ws-test",
    expect.objectContaining({ environmentId: "env_one", qoderToken: "pt-test" }),
  );
  client.clear();
});
